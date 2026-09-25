package cli

import (
	"github.com/qunaxis/skenv/internal/harness"

	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needTool(t *testing.T, tool string) {
	t.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is required in CI", tool)
		}
		t.Skipf("%s not installed", tool)
	}
}

func publicSkill(name, extra string) string {
	return "---\nname: " + name + "\ndescription: Public skill.\nlicense: MIT\nmetadata:\n  source: original\n---\n# " + name + "\n" + extra
}

func TestLintPublish(t *testing.T) {
	needTool(t, "gitleaks")
	w, repo := harnessRepo(t)
	writeFile(t, filepath.Join(repo, "skills/ok/SKILL.md"), publicSkill("ok", ""))
	w.git(repo, "add", "-A")
	w.git(repo, "commit", "-q", "-m", "feat: ok")
	t.Chdir(repo)

	// No stop-list: the publication check refuses to run.
	_, errOut := w.mustRun(2, "lint", "--publish")
	if !strings.Contains(errOut, "stop-list") {
		t.Errorf("stderr = %s", errOut)
	}
	writeFile(t, w.path(".config/skenv/denylist.txt"), "secret-codename\n")
	w.mustRun(0, "lint", "--publish")

	writeFile(t, filepath.Join(repo, "skills/bad/SKILL.md"),
		strings.Replace(publicSkill("bad", "About Secret-Codename.\n"), "source: original", "source: book", 1))
	out, _ := w.mustRun(1, "lint", "--publish", "skills/bad")
	if !strings.Contains(out, `metadata.source is "book"`) || !strings.Contains(out, "matches stop-list entry at line 1") || strings.Contains(strings.ToLower(out), "codename") {
		t.Errorf("publish findings:\n%s", out)
	}
	w.mustRun(0, "lint", "skills/bad") // the base rules alone pass

	// $SKENV_DENYLIST overrides the default location.
	list := filepath.Join(t.TempDir(), "list")
	writeFile(t, list, "nothing-matches\n")
	t.Setenv("SKENV_DENYLIST", list)
	out, _ = w.mustRun(1, "lint", "--publish", "skills/bad")
	if strings.Contains(out, "stop-list") {
		t.Errorf("$SKENV_DENYLIST ignored:\n%s", out)
	}

	// gitleaks over the whole history: a token committed and removed later
	// still fails the check. Built at run time so no token sits in this file.
	buf := make([]byte, 18)
	_, _ = rand.Read(buf)
	token := "gh" + "p_" + hex.EncodeToString(buf)
	writeFile(t, filepath.Join(repo, "config.txt"), "token = "+token+"\n")
	w.git(repo, "add", "-A")
	w.git(repo, "commit", "-q", "-m", "chore: oops")
	w.git(repo, "rm", "-q", "config.txt")
	w.git(repo, "commit", "-q", "-m", "chore: remove")
	out, _ = w.mustRun(1, "lint", "--publish", "skills/ok")
	if !strings.Contains(out, "gitleaks found secrets in the history") || strings.Contains(out, token) {
		t.Errorf("gitleaks finding missing or unredacted:\n%s", out)
	}
}

func TestLintHook(t *testing.T) {
	w, repo := harnessRepo(t)
	skill := filepath.Join(repo, "skills/demo")
	writeFile(t, filepath.Join(skill, "SKILL.md"), skillMD("demo", ""))
	hook := func(event string) (int, string) {
		t.Helper()
		hookStdin = strings.NewReader(event)
		t.Cleanup(func() { hookStdin = os.Stdin })
		code, _, errOut := w.run("lint", "--hook")
		return code, errOut
	}
	event := `{"tool_name":"Edit","tool_input":{"file_path":"` + filepath.Join(skill, "SKILL.md") + `"}}`
	if code, errOut := hook(event); code != 0 || errOut != "" {
		t.Errorf("clean skill: %d %q", code, errOut)
	}
	writeFile(t, filepath.Join(skill, "SKILL.md"), skillMD("Demo", "[x](missing.md)"))
	code, errOut := hook(event)
	if code != 2 || !strings.Contains(errOut, "L2") || !strings.Contains(errOut, "L4") || !strings.Contains(errOut, "after Edit") {
		t.Errorf("broken skill: %d\n%s", code, errOut)
	}
	// --hook ignores paths and the other modes, so they are usage errors
	// rather than checks that silently do not run.
	for _, args := range [][]string{
		{"lint", "--hook", "--publish"},
		{"lint", "--hook", "--staged"},
		{"lint", "--hook", "/does-not-exist"},
	} {
		hookStdin = strings.NewReader("{}")
		code, out, errOut := w.run(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "--hook") {
			t.Errorf("%v: %d %q %q", args, code, out, errOut)
		}
	}
	for _, ev := range []string{
		`{"tool_name":"Write","tool_input":{"file_path":"` + filepath.Join(repo, "README.md") + `"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"ls"}}`,
		`not json`,
	} {
		if code, errOut := hook(ev); code != 0 || errOut != "" {
			t.Errorf("event %s: %d %q", ev, code, errOut)
		}
	}
}

func TestNewSkill(t *testing.T) {
	w := newWorld(t)
	w.standard("\n[repo]\nharness = \"" + harness.Latest + "\"\nvisibility = \"private\"\n")
	w.cloneSync("me/skills", "~/"+ownPath)

	out, _ := w.mustRun(0, "new", "my-skill")
	skill := w.path(ownPath + "/skills/my-skill")
	if !strings.Contains(out, "created ~/"+ownPath+"/skills/my-skill") || !strings.Contains(readFile(t, skill+"/SKILL.md"), "name: my-skill\n") {
		t.Errorf("new:\n%s", out)
	}
	if _, err := os.Stat(skill + "/references/notes.md"); err != nil {
		t.Error("references/ missing")
	}
	code, _, _ := w.run("lint", skill)
	if code != 0 {
		t.Error("scaffold must pass lint")
	}
	_, errOut := w.mustRun(2, "new", "my-skill")
	if !strings.Contains(errOut, "already exists") {
		t.Errorf("duplicate: %s", errOut)
	}
	// The only own repository is the target, unless it contradicts
	// --visibility.
	_, errOut = w.mustRun(2, "new", "x", "--visibility", "public")
	if !strings.Contains(errOut, "--visibility public, but ~/"+ownPath+" is private") {
		t.Errorf("no public repo: %s", errOut)
	}
	if !strings.Contains(out, "run `skenv link` to make it available to your agents") {
		t.Errorf("new without the link step:\n%s", out)
	}
	w.mustRun(2, "new", "Bad_Name")

	pub := filepath.Join(w.home, "pub")
	mustMkdir(t, pub)
	w.git(pub, "init", "-q")
	out, _ = w.mustRun(0, "new", "shared", "--visibility", "public", "--dir", pub)
	if !strings.Contains(out, "skenv lint --publish") || !strings.Contains(out, "~/pub is not an own repository of the manifest") {
		t.Errorf("public hint or install step missing:\n%s", out)
	}
	// --dir takes the visibility from the repository's skenv.toml.
	writeFile(t, filepath.Join(pub, "skenv.toml"), "[repo]\nharness = \"0.3.0\"\nvisibility = \"public\"\n")
	out, _ = w.mustRun(0, "new", "other", "--dir", pub)
	if !strings.Contains(out, "skenv lint --publish") {
		t.Errorf("visibility from skenv.toml ignored:\n%s", out)
	}
	w.mustRun(2, "new", "third", "--dir", pub, "--visibility", "private")
	if _, err := os.Stat(filepath.Join(pub, "skills/shared/SKILL.md")); err != nil {
		t.Error("--dir ignored")
	}
}

// Without --dir, new writes to the only own repository of the manifest,
// harness or not; among several, to the one whose [repo] has the
// visibility.
func TestNewSkillTarget(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	out, _ := w.mustRun(0, "new", "first")
	if !strings.Contains(out, "created ~/"+ownPath+"/skills/first") || !strings.Contains(out, "skenv link") {
		t.Fatalf("new in the only own repository:\n%s", out)
	}
	w.mustRun(0, "link")
	if !w.exists(".claude/skills/first") {
		t.Error("skenv link did not make the new skill available")
	}

	w.push("me/team", map[string]string{"skills/shared/SKILL.md": skillMD("shared", "")}, "feat: shared")
	manifest := w.path(ownPath + "/skenv.toml")
	writeFile(t, manifest, readFile(t, manifest)+"\n[[environment.own]]\nrepo = \"me/team\"\npath = \"~/src/team\"\n")
	w.mustRun(0, "sync")
	_, errOut := w.mustRun(2, "new", "second")
	if !strings.Contains(errOut, "no own repository of the manifest (me/skills, me/team) has visibility \"private\"") || !strings.Contains(errOut, "--dir") {
		t.Errorf("new with two own repositories:\n%s", errOut)
	}
	writeFile(t, w.path("src/team/skenv.toml"), "[repo]\nharness = \""+harness.Latest+"\"\nvisibility = \"private\"\n")
	if out, _ = w.mustRun(0, "new", "second"); !strings.Contains(out, "created ~/src/team/skills/second") {
		t.Errorf("new by visibility:\n%s", out)
	}
}

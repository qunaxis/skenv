package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/schemas"
)

// harnessRepo is a git repository inside a temporary $HOME.
func harnessRepo(t *testing.T) (*world, string) {
	t.Helper()
	w := newWorld(t)
	repo := filepath.Join(w.home, "skills-repo")
	mustMkdir(t, repo)
	w.git(repo, "init", "--quiet", "-b", "main")
	return w, repo
}

// Acceptance (PRD v0.2): init → check = 0; hand edit of a managed file →
// check = 1; apply → check = 0; local AGENTS.md text outside the block
// survives apply.
func TestRepoInitCheckApply(t *testing.T) {
	w, repo := harnessRepo(t)
	// Without lefthook, init still succeeds, with a warning.
	lookLefthook = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookLefthook = exec.LookPath })

	out, errOut := w.mustRun(0, "repo", "init", "--visibility", "private", "--dir", repo)
	if !strings.Contains(out, "create .github/workflows/check.yml") || !strings.Contains(errOut, "lefthook not found") {
		t.Errorf("init output:\n%s\n%s", out, errOut)
	}
	w.mustRun(0, "repo", "check", "--dir", repo)

	agents := filepath.Join(repo, "AGENTS.md")
	local := strings.Replace(readFile(t, agents), "Repository-specific instructions go here, outside the managed block.", "Local rule: be nice.", 1) + "\nMore local text.\n"
	writeFile(t, agents, local)
	w.mustRun(0, "repo", "check", "--dir", repo)

	workflow := filepath.Join(repo, ".github/workflows/check.yml")
	writeFile(t, workflow, strings.Replace(readFile(t, workflow), "timeout-minutes: 30", "timeout-minutes: 99", 1))
	writeFile(t, agents, strings.Replace(readFile(t, agents), "### Checking", "### Checking!", 1))
	out, _ = w.mustRun(1, "repo", "check", "--dir", repo)
	if !strings.Contains(out, ".github/workflows/check.yml: differs") || !strings.Contains(out, "AGENTS.md: managed block differs") {
		t.Errorf("check output:\n%s", out)
	}

	out, _ = w.mustRun(0, "repo", "apply", "--dir", repo, "--dry-run")
	if !strings.Contains(out, "would update .github/workflows/check.yml") {
		t.Errorf("apply --dry-run:\n%s", out)
	}
	w.mustRun(1, "repo", "check", "--dir", repo)
	w.mustRun(0, "repo", "apply", "--dir", repo)
	w.mustRun(0, "repo", "check", "--dir", repo)
	if got := readFile(t, agents); got != local {
		t.Errorf("apply must restore the block and keep local text:\n%s", got)
	}
	out, _ = w.mustRun(0, "repo", "apply", "--dir", repo)
	if !strings.Contains(out, "up to date") {
		t.Errorf("second apply: %s", out)
	}

	// The skenv file names the schema of its harness version. A missing or
	// outdated directive is a warning of check (exit code unchanged) that
	// apply fixes.
	cfg := filepath.Join(repo, "skenv.toml")
	directive := "#:schema " + schemas.URL(schemas.Skenv, harness.Latest) + "\n"
	text := readFile(t, cfg)
	if !strings.HasPrefix(text, directive) {
		t.Fatalf("skenv.toml has no directive:\n%s", text)
	}
	for _, c := range []struct{ content, warning string }{
		{strings.TrimPrefix(text, directive), "no schema directive"},
		{strings.Replace(text, harness.Latest+"/", "0.3.0/", 1), "the schema directive points at " + schemas.Base + "v0.3.0/" + schemas.Skenv},
	} {
		writeFile(t, cfg, c.content)
		_, errOut := w.mustRun(0, "repo", "check", "--dir", repo)
		if !strings.Contains(errOut, "warning: skenv.toml: "+c.warning) {
			t.Errorf("check with %q: %s", c.warning, errOut)
		}
		out, _ = w.mustRun(0, "repo", "apply", "--dir", repo)
		if !strings.Contains(out, "update skenv.toml") || readFile(t, cfg) != text {
			t.Errorf("apply must restore the directive:\n%s\n%s", out, readFile(t, cfg))
		}
		if _, errOut = w.mustRun(0, "repo", "check", "--dir", repo); errOut != "" {
			t.Errorf("check after apply: %s", errOut)
		}
	}
	// A URL of the user's choice is left alone.
	writeFile(t, cfg, strings.Replace(text, directive, "#:schema ./my-schema.json\n", 1))
	if _, errOut := w.mustRun(0, "repo", "check", "--dir", repo); errOut != "" {
		t.Errorf("check with a custom schema URL: %s", errOut)
	}
	writeFile(t, cfg, text)

	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "x\n")
	out, _ = w.mustRun(1, "repo", "check", "--dir", repo)
	if !strings.Contains(out, "CLAUDE.md: must not exist") {
		t.Errorf("CLAUDE.md not reported:\n%s", out)
	}
	_, errOut = w.mustRun(2, "repo", "init", "--visibility", "public", "--dir", repo)
	if !strings.Contains(errOut, "already has [repo]") {
		t.Errorf("second init: %s", errOut)
	}
}

func TestLintCommand(t *testing.T) {
	w, repo := harnessRepo(t)
	writeFile(t, filepath.Join(repo, "skills/good/SKILL.md"), skillMD("good", ""))
	writeFile(t, filepath.Join(repo, "skills/bad/SKILL.md"), skillMD("wrong", "[x](missing.md)"))
	t.Chdir(repo)
	out, errOut := w.mustRun(1, "lint")
	if !strings.Contains(out, "skills/bad/SKILL.md: L2:") || !strings.Contains(out, "skills/bad/SKILL.md: L4:") || strings.Contains(out, "good") {
		t.Errorf("lint:\n%s", out)
	}
	if !strings.Contains(errOut, "lint: 2 skills, 2 problems") {
		t.Errorf("summary: %s", errOut)
	}
	w.mustRun(0, "lint", "skills/good")
	// --staged: only skills with staged files.
	w.mustRun(0, "lint", "--staged")
	w.git(repo, "add", "skills/good")
	w.mustRun(0, "lint", "--staged")
	w.git(repo, "add", "skills/bad")
	w.mustRun(1, "lint", "--staged")
}

// Acceptance (PRD v0.2): lefthook rejects a commit of a skill that breaks
// L1-L6. Runs the real hook, so it needs lefthook and gitleaks.
func TestLefthookRejectsBadSkill(t *testing.T) {
	for _, tool := range []string{"lefthook", "gitleaks", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s is required in CI", tool)
			}
			t.Skipf("%s not installed", tool)
		}
	}
	// Build before $HOME moves, so the module cache stays where it is.
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "skenv"), "github.com/qunaxis/skenv/cmd/skenv")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	w, repo := harnessRepo(t)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	run := func(dir string, args ...string) (string, error) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run(repo, "skenv", "repo", "init", "--visibility", "private"); err != nil || !strings.Contains(out, "hooks active") {
		t.Fatalf("repo init: %v\n%s", err, out)
	}
	w.git(repo, "add", "-A")
	if out, err := run(repo, "git", "commit", "-q", "-m", "chore: harness"); err != nil {
		t.Fatalf("committing the harness: %v\n%s", err, out)
	}

	writeFile(t, filepath.Join(repo, "skills/demo/SKILL.md"), "---\nname: Demo\ndescription: x\n---\n")
	w.git(repo, "add", "-A")
	out, err := run(repo, "git", "commit", "-q", "-m", "feat: demo")
	if err == nil {
		t.Fatalf("commit of a broken skill was accepted:\n%s", out)
	}
	if !strings.Contains(out, "L2") {
		t.Errorf("hook output lacks the lint finding:\n%s", out)
	}

	writeFile(t, filepath.Join(repo, "skills/demo/SKILL.md"), skillMD("demo", ""))
	w.git(repo, "add", "-A")
	if out, err := run(repo, "git", "commit", "-q", "-m", "feat: demo"); err != nil {
		t.Fatalf("valid skill rejected: %v\n%s", err, out)
	}
}

// repo apply on an older harness moves the version only when it can
// regenerate the files; --dry-run says "would".
func TestRepoApplyOlderHarness(t *testing.T) {
	w, repo := harnessRepo(t)
	lookLefthook = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookLefthook = exec.LookPath })
	writeFile(t, filepath.Join(repo, "skenv.toml"), "[repo]\nharness = \"0.3.0\"\nvisibility = \"private\"\n")
	writeFile(t, filepath.Join(repo, "ruff.toml"), "# hand-written\n")
	_, errOut := w.mustRun(2, "repo", "apply", "--dir", repo)
	if !strings.Contains(errOut, "not managed by skenv") || !strings.Contains(readFile(t, filepath.Join(repo, "skenv.toml")), `"0.3.0"`) {
		t.Fatalf("refused apply moved the version or did not refuse: %s", errOut)
	}
	out, _ := w.mustRun(0, "repo", "apply", "--dir", repo, "--force", "--dry-run")
	if !strings.Contains(out, "would move harness 0.3.0 → "+harness.Latest) || !strings.Contains(readFile(t, filepath.Join(repo, "skenv.toml")), `"0.3.0"`) {
		t.Fatalf("dry-run: %s", out)
	}
	w.mustRun(0, "repo", "apply", "--dir", repo, "--force")
	w.mustRun(0, "repo", "check", "--dir", repo)
}

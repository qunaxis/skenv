package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/harness"
)

func assertStandardLayout(t *testing.T, w *world) {
	t.Helper()
	if got, want := w.readlink(".agents/skills/alpha"), w.path(ownPath+"/skills/alpha"); got != want {
		t.Errorf("store link alpha → %s, want %s", got, want)
	}
	if got := w.readlink(".claude/skills/alpha"); got != "../../.agents/skills/alpha" {
		t.Errorf("claude link alpha → %s", got)
	}
	if got := w.readlink(".pi/agent/skills/beta"); got != "../../../.agents/skills/beta" {
		t.Errorf("pi link beta → %s", got)
	}
	if got := w.readlink(".claude/skills/archify"); got != "../../.agents/skills/archify" {
		t.Errorf("claude link archify → %s", got)
	}
	if !strings.Contains(readFile(t, w.path(".agents/skills/archify/SKILL.md")), "name: archify") {
		t.Error("vendored archify has wrong SKILL.md")
	}
	if fi, err := os.Stat(w.path(".agents/skills/archify/scripts/run")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("vendored script lost its executable bit: %v", err)
	}
	if w.exists(".agents/skills/other") || w.exists(".agents/skills/README.md") {
		t.Error("only the pinned path must be vendored")
	}
	if !w.exists(".claude/skills/synced") {
		t.Error("~/.claude/skills/synced must never be touched")
	}
}

// T2 / acceptance: `skenv init` on a clean $HOME clones the manifest
// repository and brings the machine to doctor = 0.
func TestInitOnCleanHome(t *testing.T) {
	w := newWorld(t)
	rev := w.initStandard("")
	cfg := readFile(t, w.path(".config/skenv/config.toml"))
	if !strings.Contains(cfg, `manifest = "~/`+ownPath+`/env.toml"`) {
		t.Fatalf("config.toml does not record the manifest:\n%s", cfg)
	}
	assertStandardLayout(t, w)
	marker := readFile(t, w.path(".agents/skills/archify/.skenv"))
	if !strings.Contains(marker, rev) || !strings.Contains(marker, `repo = "ext/tools"`) {
		t.Errorf("marker = %q", marker)
	}
	out, _ := w.mustRun(0, "doctor")
	if !strings.HasPrefix(out, "ok: 3 skills") {
		t.Errorf("doctor output = %q", out)
	}
	// Already cloned: init only records the path.
	out, _ = w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
	if !strings.Contains(out, "already cloned") {
		t.Errorf("second init output = %q", out)
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	stateBefore := readFile(t, w.path(".local/state/skenv/state.json"))
	fiBefore, err := os.Stat(w.path(".agents/skills/archify/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	out, errOut := w.mustRun(0, "sync")
	if out != "sync: up to date\n" || errOut != "" {
		t.Errorf("second sync: stdout %q stderr %q", out, errOut)
	}
	if got := readFile(t, w.path(".local/state/skenv/state.json")); got != stateBefore {
		t.Error("state changed on a no-op sync")
	}
	fiAfter, _ := os.Stat(w.path(".agents/skills/archify/SKILL.md"))
	if !os.SameFile(fiBefore, fiAfter) {
		t.Error("vendored skill was rewritten on a no-op sync")
	}
	w.mustRun(0, "doctor")
}

func TestRemoveSkillsFromManifest(t *testing.T) {
	w := newWorld(t)
	rev := w.initStandard("")
	// Remove the vendor skill from the manifest and an own skill from the
	// repository, upstream; sync pulls both changes.
	text, _, _ := strings.Cut(manifestText(rev, ""), "# pinned")
	w.push("me/skills", map[string]string{"env.toml": text, "skills/beta": ""}, "chore: drop skills")
	out, errOut := w.mustRun(0, "sync")
	for _, p := range []string{".agents/skills/archify", ".claude/skills/archify", ".pi/agent/skills/archify",
		".agents/skills/beta", ".claude/skills/beta", ".pi/agent/skills/beta"} {
		if w.exists(p) {
			t.Errorf("%s still exists after sync\n%s%s", p, out, errOut)
		}
	}
	if !w.exists(".claude/skills/alpha") {
		t.Error("alpha must stay")
	}
	w.mustRun(0, "doctor")
}

func TestVendorBump(t *testing.T) {
	w := newWorld(t)
	oldRev := w.initStandard("")
	newRev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v2")}, "fix: archify v2")
	w.push("ext/tools", map[string]string{"README.md": "unrelated\n"}, "docs: readme")
	headRev := w.git(filepath.Join(w.work, "ext__tools"), "rev-parse", "HEAD")

	out, _ := w.mustRun(0, "vendor", "bump", "archify", "--rev", newRev)
	if !strings.Contains(out, "fix: archify v2") {
		t.Errorf("bump must show the log of the path:\n%s", out)
	}
	if strings.Contains(out, "docs: readme") {
		t.Error("log must be limited to the vendored path")
	}
	manifestFile := w.path(ownPath + "/env.toml")
	text := readFile(t, manifestFile)
	if !strings.Contains(text, `rev  = "`+newRev+`" # keep this comment`) || strings.Contains(text, oldRev) {
		t.Errorf("manifest not bumped in place:\n%s", text)
	}
	if !strings.Contains(readFile(t, w.path(".agents/skills/archify/SKILL.md")), "v2") {
		t.Error("store content not updated")
	}
	// Without --rev: HEAD of the default branch.
	w.mustRun(0, "vendor", "bump", "archify")
	if !strings.Contains(readFile(t, manifestFile), headRev) {
		t.Error("bump without --rev must pin HEAD")
	}
	if !strings.Contains(readFile(t, w.path(".agents/skills/archify/.skenv")), headRev) {
		t.Error("marker not updated")
	}
}

func TestConflictWithUnmanagedPath(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	manual := w.path(".claude/skills/alpha/SKILL.md")
	writeFile(t, manual, "hand-installed\n")

	code, _, errOut := w.run("init", "me/skills", "--path", "~/"+ownPath)
	if code != 1 || !strings.Contains(errOut, "conflict: ~/.claude/skills/alpha") || !strings.Contains(errOut, "--adopt") {
		t.Fatalf("init with conflict: exit %d, stderr:\n%s", code, errOut)
	}
	if readFile(t, manual) != "hand-installed\n" {
		t.Fatal("unmanaged path was modified without --adopt")
	}
	out, _ := w.mustRun(1, "doctor")
	if !strings.Contains(out, "conflict") {
		t.Errorf("doctor must report the conflict:\n%s", out)
	}
	w.mustRun(1, "link")

	out, _ = w.mustRun(0, "sync", "--adopt")
	if !strings.Contains(out, "adopt ~/.claude/skills/alpha") {
		t.Errorf("adopt not reported:\n%s", out)
	}
	if got := w.readlink(".claude/skills/alpha"); got != "../../.agents/skills/alpha" {
		t.Errorf("alpha after adopt → %s", got)
	}
	backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/.claude/skills/alpha/SKILL.md"))
	if len(backups) != 1 || readFile(t, backups[0]) != "hand-installed\n" {
		t.Errorf("backup not found: %v", backups)
	}
	w.mustRun(0, "doctor")
}

func TestDirtyOwnCopyIsNotOverwritten(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	local := w.path(ownPath + "/skills/alpha/SKILL.md")
	writeFile(t, local, "local edit\n")
	w.push("me/skills", map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", "upstream")}, "feat: upstream")

	_, errOut := w.mustRun(0, "sync")
	if !strings.Contains(errOut, "uncommitted changes; not pulling") {
		t.Errorf("expected a dirty warning, got:\n%s", errOut)
	}
	if readFile(t, local) != "local edit\n" {
		t.Fatal("dirty working copy was overwritten")
	}
	out, _ := w.mustRun(1, "doctor", "--json")
	classes := doctorClasses(t, out)
	if !classes["dirty"] || !classes["behind"] {
		t.Errorf("doctor classes = %v", classes)
	}
}

func doctorClasses(t *testing.T, out string) map[string]bool {
	t.Helper()
	var r struct {
		OK     bool `json:"ok"`
		Issues []struct {
			Class string `json:"class"`
			Path  string `json:"path"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	classes := map[string]bool{}
	for _, is := range r.Issues {
		classes[is.Class] = true
	}
	return classes
}

func TestDoctorFindsEveryClass(t *testing.T) {
	w := newWorld(t)
	rev := w.standard(`
[[vendor]]
name = "other"
repo = "ext/tools"
path = "tools/other"
rev  = "PLACEHOLDER"
`)
	// Pin "other" at the same commit.
	w.push("me/skills", map[string]string{"env.toml": strings.ReplaceAll(manifestText(rev, `
[[vendor]]
name = "other"
repo = "ext/tools"
path = "tools/other"
rev  = "`+rev+`"
`), "PLACEHOLDER", rev)}, "chore: fix manifest")
	w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
	w.mustRun(0, "doctor")
	own := w.path(ownPath)
	newRev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v2")}, "fix: v2")

	// wrong-rev (archify pinned to a newer commit) and extra-managed
	// ("other" dropped), edited locally without syncing.
	text := manifestText(newRev, "")
	writeFile(t, filepath.Join(own, "env.toml"), text)
	// unpushed: a local commit; behind: a new upstream commit.
	w.git(own, "commit", "--quiet", "-am", "chore: local")
	w.push("me/skills", map[string]string{"notes.txt": "x\n"}, "docs: upstream")
	// dirty + conflict: a new own skill whose store path is taken by hand.
	writeFile(t, filepath.Join(own, "skills/gamma/SKILL.md"), skillMD("gamma", ""))
	writeFile(t, w.path(".agents/skills/gamma/SKILL.md"), "manual\n")
	// missing + broken-link: the store link of beta disappeared, so the
	// agent links dangle.
	if err := os.Remove(w.path(".agents/skills/beta")); err != nil {
		t.Fatal(err)
	}
	// agent-mismatch: alpha is gone for pi only.
	if err := os.Remove(w.path(".pi/agent/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	// unmanaged: hand-installed skill in an agent directory.
	writeFile(t, w.path(".claude/skills/manual/SKILL.md"), "manual\n")

	out, errOut := w.mustRun(1, "doctor", "--json")
	classes := doctorClasses(t, out)
	for _, c := range []string{"missing", "extra-managed", "unmanaged", "wrong-rev", "broken-link", "conflict",
		"dirty", "unpushed", "behind", "agent-mismatch"} {
		if !classes[c] {
			t.Errorf("doctor did not report %s\n%s\n%s", c, out, errOut)
		}
	}
	if strings.Contains(out, "synced") {
		t.Error("~/.claude/skills/synced must not be reported")
	}
	table, _ := w.mustRun(1, "doctor")
	if !strings.HasPrefix(table, "CLASS") || !strings.Contains(table, "discrepancies") {
		t.Errorf("table output:\n%s", table)
	}
}

func TestHostSkip(t *testing.T) {
	w := newWorld(t)
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname")
	}
	w.initStandard("\n[host.\"" + host + "\"]\nskip = [\"beta\", \"archify\"]\n")
	for _, p := range []string{".agents/skills/beta", ".claude/skills/beta", ".agents/skills/archify", ".pi/agent/skills/archify"} {
		if w.exists(p) {
			t.Errorf("%s must be skipped on this host", p)
		}
	}
	if !w.exists(".claude/skills/alpha") {
		t.Error("alpha must be linked")
	}
	out, _ := w.mustRun(0, "doctor")
	if !strings.HasPrefix(out, "ok: 1 skills") {
		t.Errorf("doctor = %q", out)
	}
}

func TestVendorAddAndRemove(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	w.push("solo/one", map[string]string{"SKILL.md": skillMD("one", "")}, "feat: one")

	_, errOut := w.mustRun(2, "vendor", "add", "ext/tools", "--name", "x")
	if !strings.Contains(errOut, "tools/archify") || !strings.Contains(errOut, "tools/other") {
		t.Errorf("several skills must be listed:\n%s", errOut)
	}
	w.mustRun(2, "vendor", "add", "ext/tools", "--path", "tools/archify") // duplicate name

	out, _ := w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other")
	if !strings.Contains(out, "git -C ~/"+ownPath+" commit") {
		t.Errorf("commit command not printed:\n%s", out)
	}
	w.mustRun(0, "vendor", "add", "solo/one")
	text := readFile(t, w.path(ownPath+"/env.toml"))
	for _, s := range []string{"# test manifest", "# keep this comment", `name = "other"`, `path = "tools/other"`, `name = "one"`, `path = "."`} {
		if !strings.Contains(text, s) {
			t.Errorf("manifest lacks %q:\n%s", s, text)
		}
	}
	if w.readlink(".claude/skills/one") != "../../.agents/skills/one" || !w.exists(".agents/skills/other/SKILL.md") {
		t.Error("added vendors not synced")
	}
	w.git(w.path(ownPath), "commit", "--quiet", "-am", "chore: vendor")
	w.git(w.path(ownPath), "push", "--quiet")
	w.mustRun(0, "doctor")

	w.mustRun(0, "vendor", "remove", "other")
	if w.exists(".agents/skills/other") || w.exists(".claude/skills/other") {
		t.Error("removed vendor still present")
	}
	text = readFile(t, w.path(ownPath+"/env.toml"))
	if strings.Contains(text, `"other"`) || !strings.Contains(text, `name = "one"`) || !strings.Contains(text, "# keep this comment") {
		t.Errorf("manifest after remove:\n%s", text)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath, "--dry-run")
	if w.exists(ownPath) || w.exists(".config/skenv/config.toml") {
		t.Fatal("init --dry-run changed the machine")
	}
	w.git(w.home, "clone", "--quiet", "https://github.com/me/skills", w.path(ownPath))
	manifest := "--manifest=~/" + ownPath + "/env.toml"
	out, _ := w.mustRun(0, "sync", "--dry-run", manifest)
	if !strings.Contains(out, "would vendor archify") || !strings.Contains(out, "would link ~/.claude/skills/alpha") {
		t.Errorf("dry-run plan:\n%s", out)
	}
	for _, p := range []string{".agents", ".claude/skills/alpha", ".local/state/skenv"} {
		if w.exists(p) {
			t.Errorf("dry-run created %s", p)
		}
	}
	w.mustRun(0, "sync", manifest)
	w.mustRun(0, "vendor", "remove", "archify", "--dry-run", manifest)
	if !w.exists(".agents/skills/archify") || !strings.Contains(readFile(t, w.path(ownPath+"/env.toml")), "archify") {
		t.Error("vendor remove --dry-run changed something")
	}
}

func TestManifestFromEnvironment(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	w.git(w.home, "clone", "--quiet", "https://github.com/me/skills", w.path(ownPath))
	_, errOut := w.mustRun(2, "doctor")
	if !strings.Contains(errOut, "skenv init") {
		t.Errorf("missing manifest error must say how to fix it: %s", errOut)
	}
	t.Setenv("SKENV_MANIFEST", "~/"+ownPath+"/env.toml")
	w.mustRun(0, "sync", "--quiet")
}

// No manifest configured and no default guessed: commands that need a
// manifest fail and say how to fix it.
func TestNoManifestConfigured(t *testing.T) {
	w := newWorld(t)
	for _, args := range [][]string{{"doctor"}, {"sync"}, {"vendor", "bump", "archify"}} {
		_, errOut := w.mustRun(2, args...)
		if !strings.Contains(errOut, "no manifest configured") || !strings.Contains(errOut, "skenv init <owner/repo>") ||
			!strings.Contains(errOut, "--manifest") {
			t.Errorf("skenv %s: missing manifest error must say how to fix it: %s", strings.Join(args, " "), errOut)
		}
	}
	if w.exists(".local/state/skenv") || w.exists(".agents") {
		t.Error("a failed command without a manifest changed the machine")
	}
}

// Without --path, init clones into ./<repo> of the current directory, like
// git clone, and records the absolute manifest path.
func TestInitClonesIntoCurrentDirectory(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	mustMkdir(t, w.path("src"))
	t.Chdir(w.path("src"))
	out, _ := w.mustRun(0, "init", "me/skills")
	if !strings.Contains(out, "cloned me/skills into ~/"+ownPath) {
		t.Errorf("init output = %q", out)
	}
	cfg := readFile(t, w.path(".config/skenv/config.toml"))
	if !strings.Contains(cfg, `manifest = "~/`+ownPath+`/env.toml"`) {
		t.Fatalf("config.toml does not record the manifest:\n%s", cfg)
	}
	assertStandardLayout(t, w)
	w.mustRun(0, "doctor")
	// Already cloned: a second init from the same directory only records it.
	out, _ = w.mustRun(0, "init", "me/skills")
	if !strings.Contains(out, "already cloned") {
		t.Errorf("second init output = %q", out)
	}
}

// The skills repository name and location are the user's choice: a
// repository with any name at any path works end to end.
func TestInitCustomRepositoryAndPath(t *testing.T) {
	w := newWorld(t)
	rev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v1")}, "feat: initial")
	const kit = "work/team/kit"
	w.push("acme/agent-kit", map[string]string{
		"env.toml": `[[own]]
repo = "acme/agent-kit"
path = "~/` + kit + `"

[[vendor]]
name = "archify"
repo = "ext/tools"
path = "tools/archify"
rev  = "` + rev + `"
`,
		"skills/gamma/SKILL.md": skillMD("gamma", ""),
	}, "feat: initial")
	w.mustRun(0, "init", "acme/agent-kit", "--path", "~/"+kit)
	if cfg := readFile(t, w.path(".config/skenv/config.toml")); !strings.Contains(cfg, `manifest = "~/`+kit+`/env.toml"`) {
		t.Fatalf("config.toml does not record the manifest:\n%s", cfg)
	}
	if got, want := w.readlink(".agents/skills/gamma"), w.path(kit+"/skills/gamma"); got != want {
		t.Errorf("store link gamma -> %s, want %s", got, want)
	}
	w.mustRun(0, "sync", "--quiet")
	out, _ := w.mustRun(0, "doctor")
	if !strings.HasPrefix(out, "ok: 2 skills") {
		t.Errorf("doctor output = %q", out)
	}
}

func TestUsageErrors(t *testing.T) {
	w := newWorld(t)
	w.mustRun(2)
	w.mustRun(2, "bogus")
	w.mustRun(2, "vendor", "frobnicate")
	w.mustRun(0, "sync", "--help")
	out, _ := w.mustRun(0, "version")
	if !strings.HasPrefix(out, "skenv ") {
		t.Errorf("version = %q", out)
	}
}

// N1: a managed path that the user replaced with their own content since
// the last sync is no longer skenv's; it must never be deleted silently.
func TestReplacedManagedPathIsNotDeleted(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	for _, p := range []string{".claude/skills/alpha", ".agents/skills/archify"} {
		if err := os.RemoveAll(w.path(p)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, w.path(p+"/NOTES.md"), "mine\n")
	}

	_, errOut := w.mustRun(1, "sync")
	if !strings.Contains(errOut, "conflict: ~/.claude/skills/alpha") || !strings.Contains(errOut, "conflict: ~/.agents/skills/archify") {
		t.Errorf("expected conflicts:\n%s", errOut)
	}
	for _, p := range []string{".claude/skills/alpha", ".agents/skills/archify"} {
		if readFile(t, w.path(p+"/NOTES.md")) != "mine\n" {
			t.Fatalf("%s was overwritten", p)
		}
	}
	out, _ := w.mustRun(1, "doctor", "--json")
	if c := doctorClasses(t, out); !c["conflict"] || c["broken-link"] {
		t.Errorf("doctor classes = %v", c)
	}

	// Plan only: nothing moves.
	w.mustRun(0, "sync", "--adopt", "--dry-run")
	if readFile(t, w.path(".claude/skills/alpha/NOTES.md")) != "mine\n" {
		t.Fatal("sync --adopt --dry-run moved user content")
	}
	w.mustRun(0, "sync", "--adopt")
	backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/.claude/skills/alpha/NOTES.md"))
	if len(backups) != 1 {
		t.Errorf("backup missing: %v", backups)
	}
	w.mustRun(0, "doctor")
}

// M1: vendor add must not write a manifest whose names clash with own skills.
func TestVendorAddRejectsOwnNameClash(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	before := readFile(t, w.path(ownPath+"/env.toml"))
	_, errOut := w.mustRun(2, "vendor", "add", "ext/tools", "--path", "tools/other", "--name", "alpha")
	if !strings.Contains(errOut, `"alpha" is defined twice`) {
		t.Errorf("stderr = %s", errOut)
	}
	if readFile(t, w.path(ownPath+"/env.toml")) != before {
		t.Fatal("manifest was written despite the clash")
	}
	w.mustRun(0, "sync")
}

// Two runs at once (autostart + manual) must not interleave state writes.
func TestConcurrentRunIsRejected(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	e, err := engine.Open(context.Background(), engine.Env{Home: w.home, Getenv: os.Getenv, Stdout: io.Discard, Stderr: io.Discard}, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	_, errOut := w.mustRun(2, "sync")
	if !strings.Contains(errOut, "another skenv process is running") {
		t.Errorf("stderr = %s", errOut)
	}
	w.mustRun(0, "doctor") // read-only commands do not need the lock
}

// doctor warns about own repositories whose harness is older than the
// templates of the installed skenv (PRD v0.2).
func TestDoctorWarnsAboutOldHarness(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	w.push("me/skills", map[string]string{"skenv.toml": "harness = \"0.1.0\"\nvisibility = \"private\"\n"}, "chore: harness")
	w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
	_, errOut := w.mustRun(0, "doctor")
	if !strings.Contains(errOut, "harness 0.1.0 is older than "+harness.Latest) {
		t.Errorf("stderr = %q", errOut)
	}
	out, _ := w.mustRun(0, "doctor", "--json")
	if !strings.Contains(out, "harness 0.1.0 is older") {
		t.Errorf("json warnings: %s", out)
	}
}

// Coordinator decision beyond the PRD: layout.ignore hides paths of other
// tools (e.g. the peon-ping brew package) from doctor, and sync/link never
// touch them, not even with --adopt.
func TestLayoutIgnore(t *testing.T) {
	w := newWorld(t)
	rev := w.standard("")
	text := strings.Replace(manifestText(rev, ""), "# test manifest\n", "# test manifest\n[layout]\nignore = [\"peon-ping-*\"]\n\n", 1)
	w.push("me/skills", map[string]string{"env.toml": text}, "chore: ignore peon-ping")
	writeFile(t, w.path(".claude/skills/peon-ping-toggle/SKILL.md"), "brew\n")
	writeFile(t, w.path(".agents/skills/peon-ping-use/SKILL.md"), "brew\n")
	writeFile(t, w.path(".claude/skills/manual/SKILL.md"), "hand\n")

	w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
	out, _ := w.mustRun(1, "doctor", "--json")
	if strings.Contains(out, "peon-ping") || !strings.Contains(out, "manual") {
		t.Errorf("doctor must hide ignored paths only:\n%s", out)
	}
	w.mustRun(0, "sync", "--adopt")
	for _, p := range []string{".claude/skills/peon-ping-toggle/SKILL.md", ".agents/skills/peon-ping-use/SKILL.md"} {
		if readFile(t, w.path(p)) != "brew\n" {
			t.Errorf("%s was touched", p)
		}
	}
	backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/*/skills/peon-ping-*"))
	if len(backups) != 0 {
		t.Errorf("ignored paths were backed up: %v", backups)
	}
	if err := os.RemoveAll(w.path(".claude/skills/manual")); err != nil {
		t.Fatal(err)
	}
	w.mustRun(0, "doctor")
}

// The tool config may be YAML or JSON instead of TOML; init then updates
// that file in its own format instead of creating a second one.
func TestConfigFormats(t *testing.T) {
	for name, content := range map[string]string{
		"config.yaml": "manifest: ~/" + ownPath + "/env.toml\n",
		"config.yml":  "manifest: ~/" + ownPath + "/env.toml\n",
		"config.json": `{"manifest": "~/` + ownPath + `/env.toml"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.initStandard("")
			if err := os.Remove(w.path(".config/skenv/config.toml")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, w.path(".config/skenv/"+name), content)
			w.mustRun(0, "doctor")
			w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
			if w.exists(".config/skenv/config.toml") {
				t.Error("init created config.toml next to " + name)
			}
			w.mustRun(0, "sync", "--quiet")

			writeFile(t, w.path(".config/skenv/config.toml"), "manifest = \"/elsewhere/env.toml\"\n")
			for _, args := range [][]string{{"doctor"}, {"init", "me/skills", "--path", "~/" + ownPath, "--dry-run"}, {"init", "me/skills", "--path", "~/other"}} {
				_, errOut := w.mustRun(2, args...)
				if !strings.Contains(errOut, "several config files") {
					t.Errorf("skenv %s with two config files: %s", strings.Join(args, " "), errOut)
				}
			}
			if w.exists("other") {
				t.Error("init cloned although the config cannot be updated")
			}
			// --manifest wins over the config files and does not read them.
			w.mustRun(0, "doctor", "--manifest", "~/"+ownPath+"/env.toml")
		})
	}
}

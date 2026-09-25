package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/schemas"
)

// projectDir is the project repository of the project tests, under $HOME.
const projectDir = "src/app"

// project sets up the remotes of world.standard, a project repository at
// ~/src/app whose skenv.toml has [project] (text follows [project]), a
// project-own skill "local", and makes it the current directory. It
// returns the vendor HEAD and the HEAD of me/skills.
func (w *world) project(text string) (vendorRev, ownRev string) {
	w.t.Helper()
	vendorRev = w.standard("")
	ownRev = w.git(filepath.Join(w.work, "me__skills"), "rev-parse", "HEAD")
	dir := w.path(projectDir)
	mustMkdir(w.t, dir)
	w.git(dir, "init", "--quiet", "-b", "main")
	writeFile(w.t, filepath.Join(dir, "skenv.toml"), projectText(vendorRev, ownRev, text))
	writeFile(w.t, filepath.Join(dir, ".agents/skills/local/SKILL.md"), skillMD("local", "project-own"))
	w.t.Chdir(filepath.Join(dir, ".agents")) // any directory of the repository
	return vendorRev, ownRev
}

func projectText(vendorRev, ownRev, extra string) string {
	return `# my project
[project]
mirrors = [".claude/skills"] # Claude Code
` + extra + `
[[project.vendor]]
name = "archify"
repo = "ext/tools"
path = "tools/archify"
rev  = "` + vendorRev + `" # pinned

[[project.from]]
repo   = "me/skills"
skills = ["alpha"]
rev    = "` + ownRev + `"
`
}

// pp is a path in the project.
func (w *world) pp(p string) string { return w.path(projectDir + "/" + p) }

func (w *world) projectMarker(name string) string {
	return readFile(w.t, w.pp(".agents/skills/"+name+"/.skenv"))
}

func projectIssues(t *testing.T, out string) map[string][]string {
	t.Helper()
	var r struct {
		OK     bool `json:"ok"`
		Issues []struct {
			Class, Skill, Path string
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	got := map[string][]string{}
	for _, is := range r.Issues {
		got[is.Class] = append(got[is.Class], is.Path)
	}
	return got
}

// Acceptance (#25): sync from scratch copies and mirrors, a second sync is
// idempotent, project-own skills are mirrored and never touched, and
// nothing reaches the machine (no store, no state).
func TestProjectSync(t *testing.T) {
	w := newWorld(t)
	vendorRev, ownRev := w.project("")
	localBefore := snapshot(t, w.pp(".agents/skills/local"))

	out, _ := w.mustRun(0, "sync")
	for _, want := range []string{
		"copy .agents/skills/alpha from me/skills@" + ownRev[:12] + " (skills/alpha)",
		"copy .agents/skills/archify from ext/tools@" + vendorRev[:12] + " (tools/archify)",
		"link .claude/skills/local → ../../.agents/skills/local",
		"project sync: 5 changes, 0 warnings, 0 errors",
		"git -C ~/src/app add -- skenv.toml .agents/skills .claude/skills",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output lacks %q:\n%s", want, out)
		}
	}
	for _, name := range []string{"alpha", "archify", "local"} {
		if got := w.readlink(projectDir + "/.claude/skills/" + name); got != "../../.agents/skills/"+name {
			t.Errorf("mirror %s → %s", name, got)
		}
	}
	mk := w.projectMarker("archify")
	for _, want := range []string{`repo = "https://github.com/ext/tools.git"`, `path = "tools/archify"`, `rev = "` + vendorRev + `"`, `hash = "sha256:`} {
		if !strings.Contains(mk, want) {
			t.Errorf("marker lacks %q:\n%s", want, mk)
		}
	}
	if fi, err := os.Stat(w.pp(".agents/skills/archify/scripts/run")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("copied script lost its executable bit: %v", err)
	}
	if !strings.Contains(w.projectMarker("alpha"), `path = "skills/alpha"`) || w.exists(projectDir+"/.agents/skills/beta") {
		t.Error("from copies only the listed skills")
	}
	assertUnchanged(t, localBefore, w.pp(".agents/skills/local"))
	for _, p := range []string{".agents", ".local/state/skenv/state.json", ".claude/skills/archify"} {
		if w.exists(p) {
			t.Errorf("a project sync touched the machine: %s", p)
		}
	}

	before := snapshot(t, w.pp(""))
	out, _ = w.mustRun(0, "sync")
	if strings.TrimSpace(out) != "project sync: up to date" {
		t.Errorf("second sync:\n%s", out)
	}
	assertUnchanged(t, before, w.pp(""))
	out, _ = w.mustRun(0, "doctor")
	if !strings.Contains(out, "ok: 3 project skills match skenv.toml (dir .agents/skills, mirrors .claude/skills (symlink))") {
		t.Errorf("doctor:\n%s", out)
	}

	// A fresh clone of the committed project is in sync too: the hash does
	// not depend on the machine that copied.
	w.git(w.pp(""), "add", "-A")
	w.git(w.pp(""), "commit", "--quiet", "-m", "chore: skills")
	clone := w.path("src/app-clone")
	w.git(w.home, "clone", "--quiet", w.pp(""), clone)
	t.Chdir(clone)
	w.mustRun(0, "doctor", "--project")
}

// Acceptance (#25): update --project changes rev and the copy; remove
// --project removes the copy and its mirrors.
func TestProjectVendorUpdateAndRemove(t *testing.T) {
	w := newWorld(t)
	vendorRev, _ := w.project("")
	w.mustRun(0, "sync")
	next := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v2")}, "feat: archify v2")

	out, _ := w.mustRun(0, "vendor", "update", "--project", "archify")
	for _, want := range []string{
		"update vendor archify " + vendorRev[:12] + " → " + next[:12] + " in [project] of ~/src/app/skenv.toml",
		"update .agents/skills/archify " + vendorRev[:12] + " → " + next[:12] + " (ext/tools, tools/archify)",
		`chore(skills): update archify to ` + next[:12],
	} {
		if !strings.Contains(out, want) {
			t.Errorf("update output lacks %q:\n%s", want, out)
		}
	}
	text := readFile(t, w.pp("skenv.toml"))
	if !strings.Contains(text, `rev  = "`+next+`" # pinned`) || !strings.Contains(text, "# Claude Code") {
		t.Errorf("rev not updated in place:\n%s", text)
	}
	if !strings.Contains(readFile(t, w.pp(".agents/skills/archify/SKILL.md")), "v2") || !strings.Contains(w.projectMarker("archify"), next) {
		t.Error("copy not updated")
	}
	w.mustRun(0, "doctor")

	// A from entry moves as a whole.
	ownNext := w.push("me/skills", map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", "v2")}, "feat: alpha v2")
	w.mustRun(0, "vendor", "update", "--project", "alpha")
	if !strings.Contains(readFile(t, w.pp("skenv.toml")), ownNext) || !strings.Contains(readFile(t, w.pp(".agents/skills/alpha/SKILL.md")), "v2") {
		t.Error("from entry not updated")
	}
	if code, _, errOut := w.run("vendor", "remove", "--project", "alpha"); code != 2 || !strings.Contains(errOut, "comes from [[project.from]]") {
		t.Errorf("remove of a from skill: exit %d, %s", code, errOut)
	}

	out, _ = w.mustRun(0, "vendor", "remove", "--project", "archify")
	if !strings.Contains(out, "remove .agents/skills/archify (no longer in [project])") || !strings.Contains(out, "remove .claude/skills/archify") {
		t.Errorf("remove output:\n%s", out)
	}
	if w.exists(projectDir+"/.agents/skills/archify") || w.exists(projectDir+"/.claude/skills/archify") {
		t.Error("copy or mirror left behind")
	}
	if strings.Contains(readFile(t, w.pp("skenv.toml")), "archify") {
		t.Error("entry not removed")
	}
	w.mustRun(0, "doctor")

	// vendor add --project copies and mirrors, and refuses a project-own
	// name.
	out, _ = w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other", "--project")
	if !strings.Contains(out, "copy .agents/skills/other") || w.readlink(projectDir+"/.claude/skills/other") != "../../.agents/skills/other" {
		t.Errorf("vendor add --project:\n%s", out)
	}
	if code, _, errOut := w.run("vendor", "add", "ext/tools", "--path", "tools/other", "--name", "local", "--project"); code != 2 || !strings.Contains(errOut, "project-own skill") {
		t.Errorf("add over a project-own skill: exit %d, %s", code, errOut)
	}
	w.mustRun(0, "doctor")
}

// Acceptance (#25): a local edit gives modified, a missing mirror gives
// broken-mirror; doctor finds the other classes too, and sync repairs
// what it may.
func TestProjectDoctorClasses(t *testing.T) {
	w := newWorld(t)
	_, ownRev := w.project("")
	w.mustRun(0, "sync")

	writeFile(t, w.pp(".agents/skills/archify/SKILL.md"), skillMD("archify", "edited"))
	if err := os.Remove(w.pp(".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	// A mirror entry that is a copy, not a symlink.
	if err := os.Remove(w.pp(".claude/skills/local")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.pp(".claude/skills/local/SKILL.md"), skillMD("local", "project-own"))
	// A skill only in the mirror.
	writeFile(t, w.pp(".claude/skills/stray/SKILL.md"), skillMD("stray", ""))
	// An entry gone from [project], and one moved to another rev by hand.
	text := readFile(t, w.pp("skenv.toml"))
	text = strings.Replace(text, `skills = ["alpha"]`, `skills = ["beta"]`, 1)
	writeFile(t, w.pp("skenv.toml"), text)

	code, out, _ := w.run("doctor", "--json")
	if code != 1 {
		t.Fatalf("doctor exit %d:\n%s", code, out)
	}
	got := projectIssues(t, out)
	want := map[string][]string{
		"modified":      {".agents/skills/archify"},
		"missing":       {".agents/skills/beta"},
		"extra-managed": {".agents/skills/alpha"},
		"broken-mirror": {".claude/skills/alpha"},
		"mirror-drift":  {".claude/skills/local"},
		"unmanaged":     {".claude/skills/stray"},
	}
	for class, paths := range want {
		if strings.Join(got[class], ",") != strings.Join(paths, ",") {
			t.Errorf("%s = %v, want %v", class, got[class], paths)
		}
	}
	if len(got) != len(want) {
		t.Errorf("classes = %v", got)
	}

	// sync fixes everything but the edited copy, which it never overwrites.
	code, out, errOut := w.run("sync")
	if code != 1 || !strings.Contains(errOut, ".agents/skills/archify was edited locally") {
		t.Errorf("sync: exit %d\n%s%s", code, out, errOut)
	}
	if !strings.Contains(readFile(t, w.pp(".agents/skills/archify/SKILL.md")), "edited") {
		t.Error("sync overwrote a modified copy")
	}
	if w.exists(projectDir+"/.agents/skills/alpha") || !w.exists(projectDir+"/.agents/skills/beta/.skenv") {
		t.Error("from entry not followed")
	}
	if got := w.readlink(projectDir + "/.claude/skills/local"); got != "../../.agents/skills/local" {
		t.Errorf("a mirror copy with the same content becomes a symlink, got %s", got)
	}
	if !w.exists(projectDir + "/.claude/skills/stray/SKILL.md") {
		t.Error("sync touched a skill that is only in the mirror")
	}
	code, out, _ = w.run("doctor", "--json")
	if got := projectIssues(t, out); code != 1 || len(got) != 2 || len(got["modified"]) != 1 || len(got["unmanaged"]) != 1 {
		t.Errorf("after sync: exit %d, %v", code, got)
	}
	w.mustRun(0, "sync", "--adopt")
	if strings.Contains(readFile(t, w.pp(".agents/skills/archify/SKILL.md")), "edited") {
		t.Error("--adopt did not restore the copy")
	}
	if backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/src/app/.agents/skills/archify/SKILL.md")); len(backups) != 1 {
		t.Errorf("edited copy not backed up: %v", backups)
	}

	// wrong-rev: rev moved by hand.
	text = readFile(t, w.pp("skenv.toml"))
	older := w.push("me/skills", map[string]string{"skills/beta/SKILL.md": skillMD("beta", "v2")}, "feat: beta v2")
	writeFile(t, w.pp("skenv.toml"), strings.Replace(text, ownRev, older, 1))
	code, out, _ = w.run("doctor", "--json")
	if got := projectIssues(t, out); code != 1 || strings.Join(got["wrong-rev"], ",") != ".agents/skills/beta" {
		t.Errorf("wrong-rev: exit %d, %v", code, got)
	}
	w.mustRun(0, "sync")
	if !strings.Contains(readFile(t, w.pp(".agents/skills/beta/SKILL.md")), "v2") {
		t.Error("sync did not move the copy to the new rev")
	}
}

// Project-own skills are mirrored and never touched, not even by an
// entry of the same name without --adopt.
func TestProjectOwnSkillsAreNeverReplaced(t *testing.T) {
	w := newWorld(t)
	w.project("")
	writeFile(t, w.pp(".agents/skills/archify/SKILL.md"), skillMD("archify", "our own archify"))
	before := snapshot(t, w.pp(".agents/skills/archify"))
	code, _, errOut := w.run("sync")
	if code != 1 || !strings.Contains(errOut, "conflict: .agents/skills/archify exists without a .skenv marker") {
		t.Errorf("sync: exit %d\n%s", code, errOut)
	}
	assertUnchanged(t, before, w.pp(".agents/skills/archify"))
	code, out, _ := w.run("doctor", "--json")
	if got := projectIssues(t, out); code != 1 || strings.Join(got["conflict"], ",") != ".agents/skills/archify" {
		t.Errorf("doctor: %v", got)
	}
	if got := w.readlink(projectDir + "/.claude/skills/archify"); got != "../../.agents/skills/archify" {
		t.Errorf("the project-own skill is still mirrored, got %s", got)
	}
	// --adopt takes the name over, after a backup: how copies installed by
	// another tool become pinned ones.
	w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other", "--name", "local", "--project", "--adopt")
	if !strings.Contains(w.projectMarker("local"), `path = "tools/other"`) {
		t.Error("--adopt did not replace the project-own skill")
	}
	if backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/src/app/.agents/skills/local/SKILL.md")); len(backups) != 1 {
		t.Errorf("project-own skill not backed up: %v", backups)
	}
}

// mirrors_mode = "copy" gives each mirror a full copy with a marker, kept
// in line with dir.
func TestProjectCopyMirrors(t *testing.T) {
	w := newWorld(t)
	w.project(`mirrors_mode = "copy"` + "\n")
	w.mustRun(0, "sync")
	for _, name := range []string{"alpha", "archify", "local"} {
		p := w.pp(".claude/skills/" + name)
		if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
			t.Fatalf("mirror %s is not a directory: %v", name, err)
		}
		if mk := readFile(t, filepath.Join(p, ".skenv")); !strings.Contains(mk, `mirror = ".agents/skills/`+name+`"`) {
			t.Errorf("mirror marker of %s:\n%s", name, mk)
		}
	}
	w.mustRun(0, "doctor")
	writeFile(t, w.pp(".agents/skills/local/SKILL.md"), skillMD("local", "changed"))
	code, out, _ := w.run("doctor", "--json")
	if got := projectIssues(t, out); code != 1 || strings.Join(got["mirror-drift"], ",") != ".claude/skills/local" {
		t.Errorf("stale copy: %v", got)
	}
	w.mustRun(0, "sync")
	if !strings.Contains(readFile(t, w.pp(".claude/skills/local/SKILL.md")), "changed") {
		t.Error("stale mirror copy not updated")
	}
	// An edit in the mirror is not overwritten.
	writeFile(t, w.pp(".claude/skills/local/SKILL.md"), skillMD("local", "edited in the mirror"))
	writeFile(t, w.pp(".agents/skills/local/SKILL.md"), skillMD("local", "changed again"))
	if code, _, errOut := w.run("sync"); code != 1 || !strings.Contains(errOut, "conflict: .claude/skills/local differs") {
		t.Errorf("sync over an edited mirror: exit %d\n%s", code, errOut)
	}
	// Back to symlinks: the copies are replaced.
	text := strings.Replace(readFile(t, w.pp("skenv.toml")), `mirrors_mode = "copy"`, "", 1)
	writeFile(t, w.pp("skenv.toml"), text)
	w.mustRun(0, "sync", "--adopt")
	if got := w.readlink(projectDir + "/.claude/skills/alpha"); got != "../../.agents/skills/alpha" {
		t.Errorf("copy not replaced by a symlink: %s", got)
	}
	w.mustRun(0, "doctor")
}

func TestProjectDryRunChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.project("")
	before := snapshot(t, w.pp(""))
	out, _ := w.mustRun(0, "sync", "--dry-run")
	for _, want := range []string{"would copy .agents/skills/archify", "would link .claude/skills/alpha → ../../.agents/skills/alpha", "would link .claude/skills/local"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run lacks %q:\n%s", want, out)
		}
	}
	w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other", "--project", "--dry-run")
	assertUnchanged(t, before, w.pp(""))
	w.mustRun(0, "sync")
	before = snapshot(t, w.pp(""))
	w.mustRun(0, "vendor", "remove", "archify", "--project", "--dry-run")
	w.mustRun(0, "vendor", "update", "--project", "--dry-run")
	assertUnchanged(t, before, w.pp(""))
}

// Scope: sync and doctor pick the project inside it; --manifest picks
// the machine there; the user-level sync never reads [project], not even
// of an own repository; --project needs a project.
func TestProjectScope(t *testing.T) {
	w := newWorld(t)
	w.standard(`
[project]
mirrors = [".claude/skills"]

[[project.from]]
repo   = "me/skills"
skills = ["beta"]
rev    = "` + strings.Repeat("0", 40) + `"
`)
	w.cloneSync("me/skills", "~/"+ownPath)
	if w.exists(ownPath+"/.agents") || w.exists(ownPath+"/.claude") {
		t.Error("the user-level sync read [project] of an own repository")
	}
	w.mustRun(0, "doctor", "--manifest", "~/"+ownPath)

	t.Chdir(w.path(ownPath))
	if code, _, errOut := w.run("sync", "--project", "--manifest", "x"); code != 2 || !strings.Contains(errOut, "--project and --manifest exclude each other") {
		t.Errorf("--project --manifest: exit %d, %s", code, errOut)
	}
	out, _ := w.mustRun(0, "sync", "--manifest", "~/"+ownPath)
	if !strings.HasPrefix(strings.TrimSpace(out), "sync:") {
		t.Errorf("--manifest in a project syncs the machine:\n%s", out)
	}
	code, _, errOut := w.run("sync")
	if code != 1 || !strings.Contains(errOut, "commit "+strings.Repeat("0", 40)+" not found") {
		t.Errorf("sync in a project: exit %d, %s", code, errOut)
	}

	t.Chdir(w.home)
	if code, _, errOut := w.run("doctor", "--project"); code != 2 || !strings.Contains(errOut, "no skenv file with a [project] section") {
		t.Errorf("--project outside a project: exit %d, %s", code, errOut)
	}
	if code, _, errOut := w.run("vendor", "update", "--project"); code != 2 || !strings.Contains(errOut, "--project") {
		t.Errorf("vendor --project outside a project: exit %d, %s", code, errOut)
	}
}

// Invariant (#20) for [project]: vendor add|update|remove --project keep
// the format, comments and schema directive of the skenv file.
func TestProjectWritesKeepFormat(t *testing.T) {
	for _, format := range append(formats, "yml") {
		t.Run(format, func(t *testing.T) {
			w := newWorld(t)
			w.standard("")
			dir := w.path(projectDir)
			mustMkdir(t, dir)
			w.git(dir, "init", "--quiet", "-b", "main")
			url := schemas.URL(schemas.Skenv, "")
			text := directive(format, url) + "# my project\n[project]\nmirrors = [\".claude/skills\"] # Claude Code\n"
			keep := []string{"# my project", "# Claude Code"}
			switch fileformat.Of("x." + format) {
			case "yaml":
				text = directive(format, url) + "# my project\nproject:\n  mirrors: [.claude/skills] # Claude Code\n"
			case "json":
				text = "{" + directive(format, url) + `"project": {"mirrors": [".claude/skills"]}}` + "\n"
				keep = nil
			}
			writeFile(t, filepath.Join(dir, "skenv."+format), text)
			t.Chdir(dir)
			vendors := func(names ...string) func([]byte, string) error {
				return func(data []byte, ext string) error {
					p, err := manifest.ParseProject(data, ext)
					if err != nil {
						return err
					}
					var got []string
					for _, v := range p.Vendor {
						got = append(got, v.Name)
					}
					if strings.Join(got, ",") != strings.Join(names, ",") {
						return &mismatch{"project vendors", strings.Join(got, ","), strings.Join(names, ",")}
					}
					return nil
				}
			}
			w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other", "--project")
			assertKept(t, dir, "skenv", format, keep, vendors("other"))
			next := w.push("ext/tools", map[string]string{"tools/other/SKILL.md": skillMD("other", "v2")}, "fix: other v2")
			w.mustRun(0, "vendor", "update", "other", "--rev", next, "--project")
			if !strings.Contains(readFile(t, filepath.Join(dir, "skenv."+format)), next) {
				t.Error("rev not updated")
			}
			assertKept(t, dir, "skenv", format, keep, vendors("other"))
			w.mustRun(0, "doctor")
			w.mustRun(0, "vendor", "remove", "other", "--project")
			assertKept(t, dir, "skenv", format, keep, vendors())
			w.mustRun(0, "doctor")
		})
	}
}

// Review findings: a mirror that is dir through a symlink, a .skenv
// symlink shipped by a vendored skill, a symlink in a mirror that leads
// out of the repository, ignored files, and a broken skenv file in some
// repository.
func TestProjectSafety(t *testing.T) {
	w := newWorld(t)
	w.project("")
	w.mustRun(0, "sync")

	t.Run("mirror is dir through a symlink", func(t *testing.T) {
		mirror := w.pp(".claude/skills")
		if err := os.RemoveAll(mirror); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../.agents/skills", mirror); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, w.pp(".agents/skills"))
		if code, _, errOut := w.run("sync"); code != 2 || !strings.Contains(errOut, "the same directory") {
			t.Errorf("sync: exit %d, %s", code, errOut)
		}
		assertUnchanged(t, before, w.pp(".agents/skills"))
		if err := os.Remove(mirror); err != nil {
			t.Fatal(err)
		}
		w.mustRun(0, "sync")
	})

	t.Run("symlink out of the repository in a mirror", func(t *testing.T) {
		link := w.pp(".claude/skills/local")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(w.path("elsewhere/local"), link); err != nil {
			t.Fatal(err)
		}
		if code, _, errOut := w.run("sync"); code != 1 || !strings.Contains(errOut, "is a symlink out of the repository") {
			t.Errorf("sync: exit %d, %s", code, errOut)
		}
		if dest, _ := os.Readlink(link); dest != w.path("elsewhere/local") {
			t.Errorf("replaced without --adopt: %s", dest)
		}
		w.mustRun(0, "sync", "--adopt")
		w.mustRun(0, "doctor")
	})

	t.Run("ignored files", func(t *testing.T) {
		writeFile(t, w.pp(".gitignore"), "*.log\n")
		w.push("ext/tools", map[string]string{"tools/archify/debug.log": "x\n"}, "chore: a log file")
		w.mustRun(0, "vendor", "update", "--project", "archify")
		code, _, errOut := w.run("doctor")
		if code != 0 || !strings.Contains(errOut, "git ignores 1 files of the project skills (.agents/skills/archify/debug.log") {
			t.Errorf("doctor: exit %d, %s", code, errOut)
		}
		if err := os.Remove(w.pp(".gitignore")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("broken skenv file falls back to the manifest", func(t *testing.T) {
		writeFile(t, w.pp("skenv.json"), "{}\n")
		code, _, errOut := w.run("doctor")
		if code != 2 || !strings.Contains(errOut, "warning: not a project, using the manifest: several skenv files") {
			t.Errorf("doctor: exit %d, %s", code, errOut)
		}
		if code, _, errOut := w.run("doctor", "--project"); code != 2 || strings.Contains(errOut, "warning") {
			t.Errorf("doctor --project: exit %d, %s", code, errOut)
		}
		if err := os.Remove(w.pp("skenv.json")); err != nil {
			t.Fatal(err)
		}
	})
}

// A .skenv symlink in a vendored skill never redirects the marker write.
func TestProjectMarkerSymlink(t *testing.T) {
	w := newWorld(t)
	w.project("")
	target := w.path("victim")
	writeFile(t, target, "keep\n")
	clone := filepath.Join(w.work, "ext__tools")
	if err := os.Symlink(target, filepath.Join(clone, "tools/archify/.skenv")); err != nil {
		t.Fatal(err)
	}
	w.push("ext/tools", map[string]string{}, "feat: a marker link")
	w.mustRun(0, "vendor", "update", "--project", "archify")
	if got := readFile(t, target); got != "keep\n" {
		t.Errorf("the marker was written through a symlink: %q", got)
	}
	if fi, err := os.Lstat(w.pp(".agents/skills/archify/.skenv")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("marker is not a file: %v", err)
	}
	w.mustRun(0, "doctor")
}

// Under --dry-run every edit of one command is planned, not only the last.
func TestProjectDryRunPlansEveryUpdate(t *testing.T) {
	w := newWorld(t)
	vendorRev, _ := w.project("")
	w.mustRun(0, "sync")
	w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v2")}, "feat: archify v2")
	w.push("me/skills", map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", "v2")}, "feat: alpha v2")
	out, _ := w.mustRun(0, "vendor", "update", "--project", "--dry-run")
	for _, want := range []string{"would update .agents/skills/archify " + vendorRev[:12], "would update .agents/skills/alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
}

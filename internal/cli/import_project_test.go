package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// computeSkillFolderHash of skills 1.7.0, run with Node over the files of
// the skills below: archifyV1 (with metadata.json, which the hash counts
// and installed copies leave out) and notesV1.
const (
	archifyV1Hash = "da0cf8317c7a75dc7671b3a2f120ece147f49dd3b50bff3f93dab729043bdffe"
	notesV1Hash   = "7bc654c8392eb264472349c320f4b4f4265abcdaa2a560a5974aafc9b6936813"
)

var (
	archifyV1 = map[string]string{
		"SKILL.md":      skillMD("archify", "v1"),
		"metadata.json": "{}\n",
		"scripts/run":   "#!/bin/sh\necho run\n",
	}
	notesV1 = map[string]string{"SKILL.md": skillMD("notes", "v1")}
)

// under prefixes each path of files with dir.
func under(dir string, files map[string]string) map[string]string {
	out := map[string]string{}
	for p, c := range files {
		out[dir+"/"+p] = c
	}
	return out
}

// writeProjectLock writes skills-lock.json (version 1) into the project.
func (w *world) writeProjectLock(skills map[string]lockEntry) {
	w.t.Helper()
	data, err := json.MarshalIndent(map[string]any{"version": 1, "skills": skills}, "", "  ")
	if err != nil {
		w.t.Fatal(err)
	}
	writeFile(w.t, w.pp("skills-lock.json"), string(data)+"\n")
}

func (w *world) projectLockSkills() []string {
	w.t.Helper()
	var lock struct {
		Version int                        `json:"version"`
		Skills  map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal([]byte(readFile(w.t, w.pp("skills-lock.json"))), &lock); err != nil || lock.Version != 1 {
		w.t.Fatalf("skills-lock.json: %v %d", err, lock.Version)
	}
	var names []string
	for n := range lock.Skills {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// installedInProject copies files into .agents/skills/<name> of the
// project the way `npx skills add` does, with a link from .claude/skills.
func (w *world) installedInProject(name string, files map[string]string) {
	w.t.Helper()
	for p, c := range files {
		if p != "metadata.json" { // the skills CLI does not copy it
			writeFile(w.t, w.pp(".agents/skills/"+name+"/"+p), c)
		}
	}
	w.symlink("../../.agents/skills/"+name, projectDir+"/.claude/skills/"+name)
}

func projectEntry(source, sourceType, skillPath, hash string) lockEntry {
	e := lockEntry{"source": source, "sourceType": sourceType, "skillPath": skillPath, "computedHash": hash}
	if sourceType == "gitlab" || sourceType == "git" {
		e["sourceUrl"] = source
	}
	return e
}

// importProjectWorld is a project with skills installed by `npx skills
// add` (no skenv file yet) and project-own skills in its agent
// directories. It returns the commits the skills must be pinned to.
func importProjectWorld(t *testing.T) (*world, map[string]string) {
	w := newWorld(t)
	w.mapHost("https://gitlab.com/", "gitlab.com")
	// archify changes after v1; lost and drifted do not change.
	rev1 := w.push("ext/tools", under("tools/archify", archifyV1), "feat: archify v1")
	w.push("ext/tools", map[string]string{
		"tools/archify/SKILL.md": skillMD("archify", "v2"),
		"tools/lost/SKILL.md":    skillMD("lost", "v1"),
		"tools/drifted/SKILL.md": skillMD("drifted", "v1"),
	}, "feat: archify v2, lost, drifted")
	lost := w.git(filepath.Join(w.work, "ext__tools"), "rev-parse", "HEAD")
	head := w.push("ext/tools", map[string]string{"README.md": "tools\n"}, "docs: readme")
	// A partial clone in the cache, as from GitHub: the blobs of old
	// commits are fetched to recompute their hashes.
	w.git(filepath.Join(w.remotes, "ext/tools.git"), "config", "uploadpack.allowFilter", "true")
	notes := w.push("gitlab.com/grp/sub/tools", under("notes", notesV1), "feat: notes")
	w.push("gitlab.com/grp/sub/tools", map[string]string{"README.md": "x\n"}, "docs: readme")

	dir := w.path(projectDir)
	mustMkdir(t, dir)
	w.git(dir, "init", "--quiet", "-b", "main")
	w.installedInProject("archify", archifyV1)
	w.installedInProject("lost", map[string]string{"SKILL.md": skillMD("lost", "v1")})
	w.installedInProject("drifted", map[string]string{"SKILL.md": skillMD("drifted", "edited here")})
	w.installedInProject("notes", notesV1)
	w.installedInProject("local-one", map[string]string{"SKILL.md": skillMD("local-one", "")})
	w.writeProjectLock(map[string]lockEntry{
		"archify":   projectEntry("ext/tools", "github", "tools/archify/SKILL.md", archifyV1Hash),
		"lost":      projectEntry("ext/tools", "github", "tools/lost/SKILL.md", strings.Repeat("0", 64)),
		"drifted":   projectEntry("ext/tools", "github", "tools/drifted/SKILL.md", strings.Repeat("1", 64)),
		"notes":     projectEntry("https://gitlab.com/grp/sub/tools.git", "gitlab", "notes/SKILL.md", notesV1Hash),
		"local-one": projectEntry("./local-one", "local", "SKILL.md", strings.Repeat("2", 64)),
	})

	// Project-own skills: one in dir, one only in .claude/skills, one the
	// same in both, and one that differs between them.
	writeFile(t, w.pp(".agents/skills/deploy/SKILL.md"), skillMD("deploy", ""))
	writeFile(t, w.pp(".claude/skills/solo/SKILL.md"), skillMD("solo", ""))
	for _, d := range []string{".agents/skills", ".claude/skills"} {
		writeFile(t, w.pp(d+"/same/SKILL.md"), skillMD("same", ""))
		writeFile(t, w.pp(d+"/dup/SKILL.md"), skillMD("dup", d))
	}
	writeFile(t, w.pp(".agents/skills/dup/notes.md"), "notes\n")
	t.Chdir(dir)
	return w, map[string]string{"archify": rev1, "lost": lost, "drifted": head, "notes": notes}
}

// Acceptance (#26, project level): import --project pins each entry of
// skills-lock.json to the commit whose files have its computedHash (a
// partial clone and a GitLab source too), falls back to the installed copy
// and HEAD with a warning, reports project-own skills and duplicates
// without touching them, cleans the lock with a backup, does nothing
// twice, and --dry-run writes nothing.
func TestImportProject(t *testing.T) {
	w, revs := importProjectWorld(t)
	lockBefore := readFile(t, w.pp("skills-lock.json"))
	dirs := []string{w.path(projectDir), w.path(".local"), w.path(".config")}

	before := snapshot(t, dirs...)
	out, errOut := w.mustRun(0, "import", "--project", "--dry-run")
	assertUnchanged(t, before, dirs...)
	for _, want := range []string{
		"would import vendor archify from ext/tools (tools/archify) at " + revs["archify"][:12],
		"+[project]\n+mirrors = [\".claude/skills\"]\n",
		"would remove archify, drifted, lost, notes from skills-lock.json",
		"import: planned: 4 [project] entries, 4 removed from skills-lock.json, 4 project-own skills, 1 differing duplicates",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run lacks %q:\n%s%s", want, out, errOut)
		}
	}

	out, errOut = w.mustRun(0, "import", "--project")
	text := readFile(t, w.pp("skenv.toml"))
	for name, want := range map[string]string{
		"archify": "repo = \"ext/tools\"\npath = \"tools/archify\"\nrev  = \"" + revs["archify"] + "\"",
		"lost":    "repo = \"ext/tools\"\npath = \"tools/lost\"\nrev  = \"" + revs["lost"] + "\"",
		"drifted": "repo = \"ext/tools\"\npath = \"tools/drifted\"\nrev  = \"" + revs["drifted"] + "\"",
		"notes":   "repo = \"gitlab:grp/sub/tools\"\npath = \"notes\"\nrev  = \"" + revs["notes"] + "\"",
	} {
		if !strings.Contains(text, "[[project.vendor]]\nname = \""+name+"\"\n"+want+"\n") {
			t.Errorf("%s: want %q in\n%s", name, want, text)
		}
	}
	if !strings.HasPrefix(text, "#:schema ") || !strings.Contains(text, "[project]\nmirrors = [\".claude/skills\"]\n") || strings.Contains(text, "local-one") {
		t.Errorf("skenv.toml:\n%s", text)
	}
	for _, want := range []string{
		"import vendor archify from ext/tools (tools/archify) at " + revs["archify"][:12] +
			"\n  its computedHash da0cf8317c7a is the sha256 of the files of tools/archify at that commit",
		"import vendor notes from gitlab:grp/sub/tools (notes) at " + revs["notes"][:12] + "\n  its computedHash",
		`skip local-one of skills-lock.json: installed from a "local" source, not a git repository`,
		"project-own skill .agents/skills/deploy: kept as it is",
		"project-own skill same is in .agents/skills/same, .claude/skills/same with the same files",
		"add [project] to ~/src/app/skenv.toml",
		"next: `skenv sync --adopt`",
		"git -C ~/src/app add -- skenv.toml .agents/skills .claude/skills skills-lock.json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, want := range []string{
		"vendor lost: computedHash 000000000000 is not in the history of the default branch; pinned " + revs["lost"][:12] + ", whose files match the installed copy",
		"vendor drifted: computedHash 111111111111 is not in the history of the default branch, and no commit has the files of the installed copy; pinned HEAD " + revs["drifted"][:12],
		"project-own skill solo is only in .claude/skills/solo: move it to .agents/skills/solo",
		"project-own skill dup is in .agents/skills/dup, .claude/skills/dup, and the copies differ (.agents/skills/dup and .claude/skills/dup differ: " +
			"1 file (SKILL.md) changed, 1 file (notes.md) only in .agents/skills/dup)",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("missing warning %q:\n%s", want, errOut)
		}
	}
	if got := strings.Join(w.projectLockSkills(), ","); got != "local-one" {
		t.Errorf("lock skills after import: %s", got)
	}
	backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/" + projectDir + "/skills-lock.json"))
	if len(backups) != 1 || readFile(t, backups[0]) != lockBefore {
		t.Errorf("lock backup: %v", backups)
	}
	// Nothing of the project-own skills changed.
	if readFile(t, w.pp(".claude/skills/dup/SKILL.md")) != skillMD("dup", ".claude/skills") || !w.exists(projectDir+"/.claude/skills/solo") {
		t.Error("import touched a project-own skill")
	}

	// Idempotent: a second import changes nothing.
	before = snapshot(t, dirs...)
	out, _ = w.mustRun(0, "import", "--project")
	if !strings.Contains(out, "import: nothing to import, 4 project-own skills, 1 differing duplicates") {
		t.Errorf("second import:\n%s", out)
	}
	assertUnchanged(t, before, dirs...)

	// --sync refuses while the duplicate differs, and syncs once it is
	// resolved: the project then passes doctor.
	if _, errOut := w.mustRun(1, "import", "--project", "--sync"); w.exists(projectDir + "/.agents/skills/archify/.skenv") {
		t.Errorf("synced despite the differing duplicate:\n%s", errOut)
	}
	for _, p := range []string{".claude/skills/dup", ".claude/skills/solo", ".agents/skills/local-one", ".claude/skills/local-one", "skills-lock.json"} {
		if err := os.RemoveAll(w.pp(p)); err != nil {
			t.Fatal(err)
		}
	}
	w.mustRun(0, "import", "--project", "--sync")
	for name, rev := range revs {
		if !strings.Contains(w.projectMarker(name), `rev = "`+rev+`"`) {
			t.Errorf("%s is not copied at %.12s", name, rev)
		}
	}
	w.mustRun(0, "doctor", "--project")
}

// A skenv file without [project] gets one in its own format, comments
// kept; an entry already in [project] only leaves the lock, and a lock left
// empty is removed.
func TestImportProjectExistingFile(t *testing.T) {
	w := newWorld(t)
	w.push("ext/tools", under("tools/archify", archifyV1), "feat: archify v1")
	rev := w.git(filepath.Join(w.work, "ext__tools"), "rev-parse", "HEAD")
	dir := w.path(projectDir)
	mustMkdir(t, dir)
	w.git(dir, "init", "--quiet", "-b", "main")
	yaml := "# my project\nrepo:\n  harness: 0.4.0 # the harness\n  visibility: public\n"
	writeFile(t, w.pp("skenv.yaml"), yaml)
	w.installedInProject("archify", archifyV1)
	w.writeProjectLock(map[string]lockEntry{"archify": projectEntry("ext/tools", "github", "tools/archify/SKILL.md", archifyV1Hash)})
	t.Chdir(w.pp(".agents/skills"))

	out, _ := w.mustRun(0, "import", "--project")
	if !strings.Contains(out, "add -- skenv.yaml .agents/skills .claude/skills\n") {
		t.Errorf("the commit hint names the removed, never committed lock:\n%s", out)
	}
	text := readFile(t, w.pp("skenv.yaml"))
	if !strings.Contains(text, yaml) || !strings.Contains(text, "project:\n  mirrors: [.claude/skills]\n  vendor:\n    - name: archify\n") ||
		!strings.Contains(text, rev) {
		t.Errorf("skenv.yaml:\n%s", text)
	}
	if w.exists(projectDir + "/skills-lock.json") {
		t.Error("an empty lock stays")
	}

	// The skills CLI installs it again: the entry is in [project] already.
	w.writeProjectLock(map[string]lockEntry{"archify": projectEntry("ext/tools", "github", "tools/archify/SKILL.md", archifyV1Hash)})
	out, _ = w.mustRun(0, "import", "--project")
	if readFile(t, w.pp("skenv.yaml")) != text || w.exists(projectDir+"/skills-lock.json") ||
		!strings.Contains(out, "import: 0 [project] entries, 1 removed from skills-lock.json") {
		t.Errorf("second import:\n%s", out)
	}

	if _, errOut := w.mustRun(2, "import", "--project", "--manifest", dir); !strings.Contains(errOut, "--project and --manifest exclude each other") {
		t.Errorf("--manifest: %s", errOut)
	}
	t.Chdir(w.home)
	if _, errOut := w.mustRun(2, "import", "--project"); !strings.Contains(errOut, "not inside a git repository") {
		t.Errorf("outside a repository: %s", errOut)
	}
}

// #26 follow-up of #34: a global lock entry from GitLab (a sha256
// skillFolderHash, as the skills CLI records for non-GitHub sources) is
// imported under its gitlab: short form, pinned to the commit with that
// hash.
func TestImportGitLabSource(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://gitlab.com/", "gitlab.com")
	rev := w.push("gitlab.com/grp/sub/tools", under("notes", notesV1), "feat: notes")
	w.push("gitlab.com/grp/sub/tools", map[string]string{"notes/SKILL.md": skillMD("notes", "v2")}, "feat: notes v2")
	w.push("me/skills", map[string]string{"skenv.toml": "[environment]\n"}, "feat: manifest")
	w.mustRun(0, "init", "me/skills", "--path", "~/src/skills")
	w.installed("notes", map[string]string{"SKILL.md": skillMD("notes", "edited")})
	w.writeLock(map[string]lockEntry{"notes": {
		"source": "https://gitlab.com/grp/sub/tools.git", "sourceType": "gitlab", "sourceUrl": "https://gitlab.com/grp/sub/tools.git",
		"skillPath": "notes/SKILL.md", "skillFolderHash": notesV1Hash,
	}})
	out, errOut := w.mustRun(0, "import")
	text := readFile(t, w.path("src/skills/skenv.toml"))
	if !strings.Contains(text, "repo = \"gitlab:grp/sub/tools\"\npath = \"notes\"\nrev  = \""+rev+"\"") || errOut != "" ||
		!strings.Contains(out, "its skillFolderHash 7bc654c8392e is the sha256 of the files of notes at that commit") {
		t.Errorf("skenv.toml:\n%s\n%s%s", text, out, errOut)
	}
}

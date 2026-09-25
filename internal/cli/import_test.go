package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// installed copies files into ~/.agents/skills/<name> the way `npx skills
// add -g` does and links it for Claude Code.
func (w *world) installed(name string, files map[string]string) {
	w.t.Helper()
	for p, content := range files {
		writeFile(w.t, w.path(".agents/skills/"+name+"/"+p), content)
	}
	w.symlink("../../.agents/skills/"+name, ".claude/skills/"+name)
}

func (w *world) symlink(dest, p string) {
	w.t.Helper()
	mustMkdir(w.t, filepath.Dir(w.path(p)))
	if err := os.Symlink(dest, w.path(p)); err != nil {
		w.t.Fatal(err)
	}
}

// lockEntry is an entry of ~/.agents/.skill-lock.json.
type lockEntry map[string]string

func githubEntry(repo, skillPath, hash string) lockEntry {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return lockEntry{
		"source": repo, "sourceType": "github", "sourceUrl": "https://github.com/" + repo + ".git",
		"skillPath": skillPath, "skillFolderHash": hash, "installedAt": now, "updatedAt": now,
	}
}

func (w *world) writeLock(skills map[string]lockEntry) {
	w.t.Helper()
	data, err := json.MarshalIndent(map[string]any{
		"version": 3, "skills": skills, "dismissed": map[string]bool{"findSkillsPrompt": true}, "lastSelectedAgents": []string{"claude-code"},
	}, "", "  ")
	if err != nil {
		w.t.Fatal(err)
	}
	writeFile(w.t, w.path(".agents/.skill-lock.json"), string(data))
}

func (w *world) lockSkills() []string {
	w.t.Helper()
	var lock struct {
		Version int                        `json:"version"`
		Skills  map[string]json.RawMessage `json:"skills"`
		Dismiss map[string]bool            `json:"dismissed"`
	}
	if err := json.Unmarshal([]byte(readFile(w.t, w.path(".agents/.skill-lock.json"))), &lock); err != nil {
		w.t.Fatal(err)
	}
	if lock.Version != 3 || !lock.Dismiss["findSkillsPrompt"] {
		w.t.Errorf("the lock lost its other keys: %+v", lock)
	}
	var names []string
	for n := range lock.Skills {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// importWorld is a machine with skills installed by `npx skills` and by
// hand, and a manifest that has none of them yet. It returns the commits
// the vendored skills must be pinned to.
func importWorld(t *testing.T) (*world, map[string]string) {
	w := newWorld(t)
	// Third-party skills: archify changes after rev1, lost and drifted do not.
	rev1 := w.push("ext/tools", map[string]string{
		"tools/archify/SKILL.md":      skillMD("archify", "v1"),
		"tools/archify/metadata.json": "{}\n",
		"tools/other/SKILL.md":        skillMD("other", "v1"),
		"tools/lost/SKILL.md":         skillMD("lost", "v1"),
		"tools/drifted/SKILL.md":      skillMD("drifted", "v1"),
	}, "feat: initial")
	w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v2")}, "fix: archify v2")
	head := w.push("ext/tools", map[string]string{"README.md": "tools\n"}, "docs: readme")
	tools := filepath.Join(w.work, "ext__tools")
	// A branch with its own version of other.
	w.git(tools, "checkout", "--quiet", "-b", "stable", rev1)
	writeFile(t, filepath.Join(tools, "tools/other/SKILL.md"), skillMD("other", "stable"))
	w.git(tools, "commit", "--quiet", "-am", "fix: other on stable")
	stable := w.git(tools, "rev-parse", "HEAD")
	w.git(tools, "push", "--quiet", "origin", "stable")
	w.git(tools, "checkout", "--quiet", "main")
	tree := func(rev, p string) string { return w.git(tools, "rev-parse", rev+":"+p) }

	// The manifest: an own repository with alpha, and an ignore pattern.
	w.push("me/skills", map[string]string{
		"skenv.toml": `# my manifest
[environment]

[environment.layout]
ignore = ["peon-*"]

[[environment.own]]
repo = "me/skills"
path = "~/src/skills"
`,
		"skills/alpha/SKILL.md": skillMD("alpha", ""),
	}, "feat: manifest")
	w.mustRun(0, "init", "me/skills", "--path", "~/src/skills")

	// An own repository of which only a and b are linked.
	w.push("me/mine", map[string]string{
		"skills/a/SKILL.md": skillMD("a", ""), "skills/b/SKILL.md": skillMD("b", ""), "skills/c/SKILL.md": skillMD("c", ""),
	}, "feat: mine")
	mustMkdir(t, w.path("src"))
	w.git(w.path("src"), "clone", "--quiet", "https://github.com/me/mine.git")
	for _, n := range []string{"a", "b"} {
		w.symlink(w.path("src/mine/skills/"+n), ".agents/skills/"+n)
		w.symlink("../../.agents/skills/"+n, ".claude/skills/"+n)
	}

	// Copies installed by `npx skills`: archify edited locally since.
	w.installed("archify", map[string]string{"SKILL.md": skillMD("archify", "edited here")})
	w.installed("other", map[string]string{"SKILL.md": skillMD("other", "stable")})
	w.installed("lost", map[string]string{"SKILL.md": skillMD("lost", "v1")})
	w.installed("drifted", map[string]string{"SKILL.md": skillMD("drifted", "edited here")})
	w.installed("local-one", map[string]string{"SKILL.md": skillMD("local-one", "")})
	other := githubEntry("ext/tools", "tools/other/SKILL.md", tree(stable, "tools/other"))
	other["ref"] = "stable"
	local := githubEntry("", "SKILL.md", "")
	local["sourceType"], local["source"] = "local", "/somewhere/local-one"
	w.writeLock(map[string]lockEntry{
		"archify":   githubEntry("ext/tools", "tools/archify/SKILL.md", tree(rev1, "tools/archify")),
		"other":     other,
		"lost":      githubEntry("ext/tools", "tools/lost/SKILL.md", strings.Repeat("0", 40)),
		"drifted":   githubEntry("ext/tools", "tools/drifted/SKILL.md", strings.Repeat("1", 40)),
		"alpha":     githubEntry("me/skills", "skills/alpha/SKILL.md", ""),
		"removed":   githubEntry("ext/tools", "tools/removed/SKILL.md", ""),
		"local-one": local,
	})

	// Not skills to import: by hand, ignored, a Claude Code plugin's, synced.
	writeFile(t, w.path(".claude/skills/handmade/SKILL.md"), skillMD("handmade", ""))
	writeFile(t, w.path(".claude/skills/peon-ping/SKILL.md"), skillMD("peon-ping", ""))
	mustMkdir(t, w.path(".claude/plugins/cache/p"))
	w.git(w.path(".claude/plugins/cache/p"), "init", "--quiet")
	writeFile(t, w.path(".claude/plugins/cache/p/skills/plug/SKILL.md"), skillMD("plug", ""))
	w.symlink(w.path(".claude/plugins/cache/p/skills/plug"), ".claude/skills/plug")
	return w, map[string]string{"archify": rev1, "other": stable, "lost": rev1, "drifted": head}
}

// Acceptance (#26, user level): import pins each lock entry to the commit
// its skillFolderHash names (on its ref), falls back to the installed copy
// and then HEAD with a warning, adds a partly linked own repository with a
// selection, reports unknown directories, skips synced/, ignored and
// plugin skills, cleans the lock with a backup, and does nothing twice.
func TestImport(t *testing.T) {
	w, revs := importWorld(t)
	manifestFile := w.path("src/skills/skenv.toml")
	lockFile := w.path(".agents/.skill-lock.json")
	lockBefore := readFile(t, lockFile)
	dirs := []string{w.path(".agents"), w.path(".claude"), w.path(".pi"), w.path("src"), w.path(".local/state"), w.path(".config")}

	before := snapshot(t, dirs...)
	out, errOut := w.mustRun(0, "import", "--dry-run")
	assertUnchanged(t, before, dirs...)
	if !strings.Contains(out, "+++ ~/src/skills/skenv.toml\n") || !strings.Contains(out, "+[[environment.vendor]]\n") ||
		!strings.Contains(out, "would remove alpha, archify, drifted, lost, other from ~/.agents/.skill-lock.json") {
		t.Errorf("dry run:\n%s%s", out, errOut)
	}

	out, errOut = w.mustRun(0, "import")
	text := readFile(t, manifestFile)
	if !strings.HasPrefix(text, "# my manifest\n[environment]\n\n[environment.layout]\n") {
		t.Errorf("the manifest lost its comment:\n%s", text)
	}
	for name, rev := range revs {
		path := "tools/" + name
		if !strings.Contains(text, "name = \""+name+"\"\nrepo = \"ext/tools\"\npath = \""+path+"\"\nrev  = \""+rev+"\"\n") {
			t.Errorf("vendor %s is not pinned to %.12s:\n%s", name, rev, text)
		}
	}
	if !strings.Contains(text, "[[environment.own]]\nrepo = \"me/mine\"\npath = \"~/src/mine\"\nskills = [\"a\", \"b\"]\n") {
		t.Errorf("own me/mine with a selection is missing:\n%s", text)
	}
	for _, want := range []string{
		"vendor lost: skillFolderHash 000000000000 is not in the history of the default branch; pinned " + revs["lost"][:12] + ", whose files match the installed copy",
		"vendor drifted: neither skillFolderHash 111111111111 nor the installed copy matches a commit of the default branch; pinned its HEAD " + revs["drifted"][:12],
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("missing warning %q:\n%s", want, errOut)
		}
	}
	for _, want := range []string{
		"import own me/mine at ~/src/mine (skills a, b)",
		"import vendor archify from ext/tools (tools/archify) at " + revs["archify"][:12] + "\n  its skillFolderHash",
		"import vendor other from ext/tools (tools/other) at " + revs["other"][:12] + "\n  its skillFolderHash",
		"unmanaged ~/.claude/skills/handmade: not in ~/.agents/.skill-lock.json and not a link into a git working copy",
		`add "handmade" to layout.ignore`,
		`unmanaged ~/.agents/skills/local-one: in ~/.agents/.skill-lock.json, but installed from a "local" source`,
		"skip removed of ~/.agents/.skill-lock.json: not installed",
		"next: `skenv sync --adopt`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, skipped := range []string{"peon-ping", "plug", "synced", "skills/alpha"} {
		if strings.Contains(out, skipped) {
			t.Errorf("%s must be skipped:\n%s", skipped, out)
		}
	}
	if got := strings.Join(w.lockSkills(), ","); got != "local-one,removed" {
		t.Errorf("lock skills after import: %s", got)
	}
	backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/.agents/.skill-lock.json"))
	if len(backups) != 1 || readFile(t, backups[0]) != lockBefore {
		t.Errorf("lock backup: %v", backups)
	}

	// Idempotent: a second import changes nothing.
	lockAfter := readFile(t, lockFile)
	out, _ = w.mustRun(0, "import")
	if !strings.Contains(out, "import: nothing to import, 2 unmanaged") || readFile(t, manifestFile) != text || readFile(t, lockFile) != lockAfter {
		t.Errorf("second import:\n%s", out)
	}

	// --sync adopts the copies; without the unmanaged ones doctor passes.
	for _, p := range []string{".claude/skills/handmade", ".agents/skills/local-one", ".claude/skills/local-one", ".claude/skills/plug"} {
		if err := os.RemoveAll(w.path(p)); err != nil {
			t.Fatal(err)
		}
	}
	w.git(w.path("src/skills"), "commit", "--quiet", "-am", "chore(manifest): import")
	w.git(w.path("src/skills"), "push", "--quiet")
	w.mustRun(0, "import", "--sync")
	if !strings.Contains(readFile(t, w.path(".agents/skills/other/.skenv")), revs["other"]) {
		t.Error("other is not vendored at the stable commit")
	}
	if got := w.readlink(".agents/skills/a"); got != w.path("src/mine/skills/a") {
		t.Errorf("own link a → %s", got)
	}
	if w.exists(".agents/skills/c") {
		t.Error("c is not selected")
	}
	w.mustRun(0, "doctor")
}

// Acceptance (#26): init --import starts the manifest in the repository,
// imports and syncs, so the machine passes doctor in one command.
func TestInitImport(t *testing.T) {
	w := newWorld(t)
	rev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v1")}, "feat: archify")
	tools := filepath.Join(w.work, "ext__tools")
	w.push("me/skills", map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", ""), "skills/beta/SKILL.md": skillMD("beta", "")}, "feat: skills")
	mustMkdir(t, w.path("src"))
	w.git(w.path("src"), "clone", "--quiet", "https://github.com/me/skills.git")
	w.installed("archify", map[string]string{"SKILL.md": skillMD("archify", "v1")})
	w.writeLock(map[string]lockEntry{"archify": githubEntry("ext/tools", "tools/archify/SKILL.md", w.git(tools, "rev-parse", rev+":tools/archify"))})

	dirs := []string{w.path(".agents"), w.path(".claude"), w.path("src"), w.path(".config")}
	before := snapshot(t, dirs...)
	out, _ := w.mustRun(0, "init", "--import", "--dir", w.path("src/skills"), "--dry-run")
	assertUnchanged(t, before, dirs...)
	for _, want := range []string{"would create ~/src/skills/skenv.toml", "would add own me/skills", "would import vendor archify", "would run skenv sync --adopt"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run lacks %q:\n%s", want, out)
		}
	}

	out, _ = w.mustRun(0, "init", "--import", "--dir", w.path("src/skills"))
	text := readFile(t, w.path("src/skills/skenv.toml"))
	if !strings.Contains(text, "rev  = \""+rev+"\"") || !strings.Contains(text, "repo = \"me/skills\"\npath = \"~/src/skills\"\n") {
		t.Errorf("skenv.toml:\n%s\n%s", text, out)
	}
	if !strings.Contains(readFile(t, w.path(".config/skenv/config.toml")), "~/src/skills/skenv.toml") {
		t.Error("the manifest is not recorded")
	}
	if len(w.lockSkills()) != 0 {
		t.Error("archify stays in the lock")
	}
	w.git(w.path("src/skills"), "add", "-A")
	w.git(w.path("src/skills"), "commit", "--quiet", "-m", "chore(manifest): import")
	w.git(w.path("src/skills"), "push", "--quiet")
	w.mustRun(0, "doctor")

	for args, want := range map[string]string{
		"init --import me/skills":                           "--import starts a new manifest",
		"import --manifest " + w.path("nowhere/skenv.toml"): "nowhere/skenv.toml",
	} {
		if _, errOut := w.mustRun(2, strings.Fields(args)...); !strings.Contains(errOut, want) {
			t.Errorf("skenv %s: %s", args, errOut)
		}
	}
}

// Without a configured manifest import points at init --import.
func TestImportWithoutManifest(t *testing.T) {
	w := newWorld(t)
	if _, errOut := w.mustRun(2, "import"); !strings.Contains(errOut, "skenv init --import") {
		t.Errorf("import: %s", errOut)
	}
}

// A skenv file without [environment] gets one, as `skenv init` adds it; a
// public [repo] refuses and writes nothing.
func TestImportAddsEnvironment(t *testing.T) {
	noLefthook(t)
	w, repo := harnessRepo(t)
	w.mustRun(0, "repo", "init", "--visibility", "private", "--dir", repo)
	file := filepath.Join(repo, "skenv.toml")
	withRepo := readFile(t, file)
	out, _ := w.mustRun(0, "import", "--manifest", repo)
	text := readFile(t, file)
	if !strings.HasPrefix(text, withRepo) || !strings.Contains(text, "\n[environment]\n") || !strings.Contains(out, "add [environment] to ~/skills-repo/skenv.toml") {
		t.Errorf("skenv.toml:\n%s\n%s", text, out)
	}
	if out, _ := w.mustRun(0, "import", "--manifest", repo); !strings.Contains(out, "nothing to import") || readFile(t, file) != text {
		t.Errorf("second import:\n%s", out)
	}

	pub := filepath.Join(w.home, "public")
	mustMkdir(t, pub)
	w.git(pub, "init", "--quiet", "-b", "main")
	w.mustRun(0, "repo", "init", "--visibility", "public", "--dir", pub)
	before := snapshot(t, pub)
	if _, errOut := w.mustRun(2, "import", "--manifest", pub); !strings.Contains(errOut, `visibility = "public"`) {
		t.Errorf("public: %s", errOut)
	}
	assertUnchanged(t, before, pub)
}

// The other shapes of lock entries: a skill at the repository root, a ref
// that is a tag on no branch or a short SHA, a lock key that is not the
// directory name, and the lock under $XDG_STATE_HOME.
func TestImportLockVariants(t *testing.T) {
	w := newWorld(t)
	t.Setenv("XDG_STATE_HOME", w.path(".state"))
	rootV1 := w.push("ext/root", map[string]string{"SKILL.md": skillMD("rooty", "v1")}, "feat: v1")
	w.push("ext/root", map[string]string{"SKILL.md": skillMD("rooty", "v2")}, "feat: v2")
	root := filepath.Join(w.work, "ext__root")

	rev1 := w.push("ext/tools", map[string]string{
		"tools/other/SKILL.md":       skillMD("other", "v1"),
		"tools/pretty-name/SKILL.md": skillMD("Pretty Name", ""),
	}, "feat: initial")
	w.push("ext/tools", map[string]string{"tools/other/SKILL.md": skillMD("other", "v2")}, "fix: other v2")
	tools := filepath.Join(w.work, "ext__tools")
	// A tag on a commit of no branch.
	w.git(tools, "checkout", "--quiet", "-b", "side", rev1)
	writeFile(t, filepath.Join(tools, "tools/tagged/SKILL.md"), skillMD("tagged", "side"))
	w.git(tools, "add", "-A")
	w.git(tools, "commit", "--quiet", "-m", "feat: tagged")
	side := w.git(tools, "rev-parse", "HEAD")
	w.git(tools, "tag", "v1")
	w.git(tools, "push", "--quiet", "origin", "v1")
	w.git(tools, "checkout", "--quiet", "main")

	w.push("me/skills", map[string]string{"skenv.toml": "[environment]\n"}, "feat: manifest")
	w.mustRun(0, "init", "me/skills", "--path", "~/src/skills")

	w.installed("rooty", map[string]string{"SKILL.md": skillMD("rooty", "v1")})
	w.installed("tagged", map[string]string{"SKILL.md": skillMD("tagged", "side")})
	w.installed("other", map[string]string{"SKILL.md": skillMD("other", "v1")})
	w.installed("pretty-name", map[string]string{"SKILL.md": skillMD("Pretty Name", "")})
	tagged := githubEntry("ext/tools", "tools/tagged/SKILL.md", w.git(tools, "rev-parse", side+":tools/tagged"))
	tagged["ref"] = "v1"
	other := githubEntry("ext/tools", "tools/other/SKILL.md", w.git(tools, "rev-parse", rev1+":tools/other"))
	other["ref"] = rev1[:12]
	lock := map[string]lockEntry{
		"rooty":       githubEntry("ext/root", "SKILL.md", w.git(root, "rev-parse", rootV1+"^{tree}")),
		"tagged":      tagged,
		"other":       other,
		"Pretty Name": githubEntry("ext/tools", "tools/pretty-name/SKILL.md", w.git(tools, "rev-parse", rev1+":tools/pretty-name")),
	}
	data, err := json.MarshalIndent(map[string]any{"version": 3, "skills": lock, "dismissed": map[string]bool{"findSkillsPrompt": true}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lockFile := w.path(".state/skills/.skill-lock.json")
	writeFile(t, lockFile, string(data))

	out, errOut := w.mustRun(0, "import")
	text := readFile(t, w.path("src/skills/skenv.toml"))
	for name, want := range map[string]string{
		"rooty":       "repo = \"ext/root\"\npath = \".\"\nrev  = \"" + rootV1 + "\"",
		"tagged":      "path = \"tools/tagged\"\nrev  = \"" + side + "\"",
		"other":       "path = \"tools/other\"\nrev  = \"" + rev1 + "\"",
		"pretty-name": "path = \"tools/pretty-name\"\nrev  = \"" + rev1 + "\"",
	} {
		if !strings.Contains(text, "name = \""+name+"\"\n") || !strings.Contains(text, want) {
			t.Errorf("%s: want %q in\n%s\n%s%s", name, want, text, out, errOut)
		}
	}
	if errOut != "" {
		t.Errorf("every rev must match exactly:\n%s", errOut)
	}
	var got struct {
		Skills map[string]any `json:"skills"`
	}
	if err := json.Unmarshal([]byte(readFile(t, lockFile)), &got); err != nil || len(got.Skills) != 0 {
		t.Errorf("lock after import: %v %v", got.Skills, err)
	}
	if backups, _ := filepath.Glob(w.path(".local/state/skenv/backup/*/.state/skills/.skill-lock.json")); len(backups) != 1 {
		t.Errorf("backup of the XDG lock: %v", backups)
	}
}

// When a later commit reverts the skill to the same tree, the search from
// updatedAt back pins the commit that was installed, not the revert.
func TestImportSearchesFromUpdatedAt(t *testing.T) {
	w := newWorld(t)
	at := func(date string) { t.Setenv("GIT_COMMITTER_DATE", date) }
	at("2026-01-01T00:00:00Z")
	installed := w.push("ext/tools", map[string]string{"tools/x/SKILL.md": skillMD("x", "v1")}, "feat: x v1")
	at("2026-02-01T00:00:00Z")
	w.push("ext/tools", map[string]string{"tools/x/SKILL.md": skillMD("x", "v2")}, "feat: x v2")
	at("2026-03-01T00:00:00Z")
	w.push("ext/tools", map[string]string{"tools/x/SKILL.md": skillMD("x", "v1")}, "revert: x v1")
	tools := filepath.Join(w.work, "ext__tools")
	w.push("me/skills", map[string]string{"skenv.toml": "[environment]\n"}, "feat: manifest")
	w.mustRun(0, "init", "me/skills", "--path", "~/src/skills")

	w.installed("x", map[string]string{"SKILL.md": skillMD("x", "v1")})
	entry := githubEntry("ext/tools", "tools/x/SKILL.md", w.git(tools, "rev-parse", installed+":tools/x"))
	entry["installedAt"], entry["updatedAt"] = "2026-01-15T00:00:00.000Z", "2026-02-15T00:00:00.000Z"
	w.writeLock(map[string]lockEntry{"x": entry})
	w.mustRun(0, "import")
	if text := readFile(t, w.path("src/skills/skenv.toml")); !strings.Contains(text, installed) {
		t.Errorf("x must be pinned to %.12s:\n%s", installed, text)
	}
}

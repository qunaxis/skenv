package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/harness"
)

// The verification requirements of the declarative contract (issue #38,
// docs/adr/0002-config-format.md): these tests exercise the reconciliation
// end to end, in a temporary $HOME with local remotes.

// managedState is what a sync leaves behind: every link and copy in the
// store and the agent directories, and the state file.
func (w *world) managedState() string {
	w.t.Helper()
	var b strings.Builder
	for _, dir := range []string{".agents/skills", ".claude/skills", ".pi/agent/skills"} {
		entries, _ := os.ReadDir(w.path(dir))
		for _, de := range entries {
			p := w.path(dir + "/" + de.Name())
			if dest, err := os.Readlink(p); err == nil {
				b.WriteString(dir + "/" + de.Name() + " -> " + dest + "\n")
			} else {
				b.WriteString(dir + "/" + de.Name() + "\n")
			}
		}
	}
	b.WriteString(readFile(w.t, w.path(".local/state/skenv/state.json")))
	return b.String()
}

// With unchanged inputs a second reconciliation plans no changes and
// changes nothing.
func TestSecondSyncChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	before := w.managedState()
	out, _ := w.mustRun(0, "sync")
	if strings.TrimSpace(out) != "sync: up to date" {
		t.Errorf("second sync:\n%s", out)
	}
	if out, _ := w.mustRun(0, "sync", "--dry-run"); !strings.Contains(out, "sync: up to date") {
		t.Errorf("dry run after a sync plans changes:\n%s", out)
	}
	if w.managedState() != before {
		t.Error("the second sync changed the machine")
	}
	w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other")
	w.git(w.path(ownPath), "commit", "--quiet", "-am", "chore: other")
	w.git(w.path(ownPath), "push", "--quiet")
	w.mustRun(0, "sync")
	after := w.managedState()
	if out, _ := w.mustRun(0, "sync"); strings.TrimSpace(out) != "sync: up to date" || w.managedState() != after {
		t.Errorf("sync after vendor add is not idempotent:\n%s", out)
	}
	w.mustRun(0, "doctor")
}

// Reordering independent declarations does not change the result: the
// same tables in another order sync to the same links, copies and state.
func TestReorderedManifestSyncsTheSame(t *testing.T) {
	w := newWorld(t)
	w.push("me/team", map[string]string{"skills/deploy/SKILL.md": skillMD("deploy", "")}, "feat: team")
	rev := w.standard("")
	text := manifestText(rev, `
[user.dependencies.other]
repo      = "ext/tools"
skill_dir = "tools/other"
commit    = "`+rev+`"

[user.checkouts.team]
repo         = "me/team"
checkout_dir = "~/src/team"
exclude      = ["nothing-*", "deploy-old"]
`)
	w.push("me/skills", map[string]string{"skenv.toml": text}, "chore: more")
	w.cloneSync("me/skills", "~/"+ownPath)
	manifest := w.path(ownPath + "/skenv.toml")
	before := w.managedState()

	// The same declarations, tables and list items in reverse order.
	blocks := strings.SplitAfter(strings.TrimPrefix(text, "# test manifest\n"), "\n\n")
	for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
		blocks[i], blocks[j] = blocks[j], blocks[i]
	}
	reordered := strings.Replace(strings.Join(blocks, "\n"), `["nothing-*", "deploy-old"]`, `["deploy-old", "nothing-*"]`, 1)
	writeFile(t, manifest, reordered)
	out, _ := w.mustRun(0, "sync")
	if !strings.Contains(out, "sync: 0 changes, 1 unresolved") {
		t.Errorf("sync of the reordered manifest (uncommitted, so one unresolved) changed something:\n%s", out)
	}
	if w.managedState() != before {
		t.Error("the reordered manifest synced to another state")
	}
}

// Normal sync never rewrites intent: pins, selections, template versions
// and agents stay as written, byte for byte, whatever moved upstream.
func TestSyncNeverRewritesTheFile(t *testing.T) {
	noLefthook(t)
	w := newWorld(t)
	w.initStandard("\n[user.agents]\nenabled = [\"claude\"]\n\n[repository]\ntemplate_version = \"0.3.0\"\nvisibility = \"private\"\n")
	manifest := w.path(ownPath + "/skenv.toml")
	before := readFile(t, manifest)
	w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "v2")}, "fix: archify v2")
	w.push("me/skills", map[string]string{"skills/gamma/SKILL.md": skillMD("gamma", "")}, "feat: gamma")
	upstream := readFile(t, manifest)
	for _, args := range [][]string{{"sync"}, {"sync", "--adopt"}, {"link"}, {"doctor"}, {"list"}, {"config", "show"}, {"repo", "check", "--dir", w.path(ownPath)}} {
		w.run(args...)
		if got := readFile(t, manifest); got != upstream && got != before {
			t.Errorf("skenv %s rewrote the skenv file:\n%s", strings.Join(args, " "), got)
		}
	}
	if !strings.Contains(readFile(t, manifest), `template_version = "0.3.0"`) || strings.Contains(readFile(t, manifest), harness.Latest) {
		t.Error("the template version moved without repo upgrade")
	}
	if mk := readFile(t, w.path(".agents/skills/archify/.skenv")); strings.Contains(mk, w.git(filepath.Join(w.work, "ext__tools"), "rev-parse", "HEAD")) {
		t.Error("sync followed the branch of a pinned dependency")
	}
	// A successful repo apply leaves the file byte for byte.
	w.mustRun(0, "repo", "upgrade", "--dir", w.path(ownPath), "--force")
	upgraded := readFile(t, manifest)
	w.mustRun(0, "repo", "apply", "--dir", w.path(ownPath))
	if readFile(t, manifest) != upgraded {
		t.Error("repo apply rewrote the skenv file")
	}
}

// Removing a declared resource removes only its managed artifacts:
// unmanaged paths and the working copies of checkouts stay.
func TestRemovalRemovesOnlyManagedPaths(t *testing.T) {
	w := newWorld(t)
	w.push("me/team", map[string]string{"skills/deploy/SKILL.md": skillMD("deploy", "")}, "feat: team")
	w.initStandard("\n[user.checkouts.team]\nrepo = \"me/team\"\ncheckout_dir = \"~/src/team\"\n")
	writeFile(t, w.path(".claude/skills/handmade/SKILL.md"), "mine\n")
	manifest := w.path(ownPath + "/skenv.toml")
	text := readFile(t, manifest)
	writeFile(t, manifest, strings.Replace(text, "\n[user.checkouts.team]\nrepo = \"me/team\"\ncheckout_dir = \"~/src/team\"\n", "", 1))
	out, _ := w.mustRun(0, "sync")
	if !strings.Contains(out, "remove ~/.claude/skills/deploy") || w.exists(".agents/skills/deploy") {
		t.Errorf("the links of a removed checkout stayed:\n%s", out)
	}
	if !w.exists("src/team/skills/deploy/SKILL.md") || !w.exists("src/team/.git") {
		t.Error("the working copy of a removed checkout was deleted")
	}
	if readFile(t, w.path(".claude/skills/handmade/SKILL.md")) != "mine\n" {
		t.Error("an unmanaged skill was touched")
	}
	// A managed path replaced by hand is not skenv's any more.
	if err := os.Remove(w.path(".pi/agent/skills/archify")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.path(".pi/agent/skills/archify/SKILL.md"), "replaced by hand\n")
	w.mustRun(0, "vendor", "remove", "archify")
	if readFile(t, w.path(".pi/agent/skills/archify/SKILL.md")) != "replaced by hand\n" || w.exists(".agents/skills/archify") {
		t.Error("vendor remove touched a path replaced by hand, or kept its own")
	}
}

// Existing checkouts with a wrong origin or branch, uncommitted changes or
// diverged history are an explicit unresolved result, never corrected
// destructively.
func TestCheckoutPolicy(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	own := w.path(ownPath)
	head := func() string { return w.git(own, "rev-parse", "HEAD") }

	// Another branch: local development state, linked as checked out.
	w.git(own, "switch", "--quiet", "-c", "feature")
	writeFile(t, filepath.Join(own, "skills/gamma/SKILL.md"), skillMD("gamma", ""))
	w.git(own, "add", "-A")
	w.git(own, "commit", "--quiet", "-m", "feat: gamma on a branch")
	w.push("me/skills", map[string]string{"skills/delta/SKILL.md": skillMD("delta", "")}, "feat: delta upstream")
	featureHead := head()
	out, errOut := w.mustRun(0, "sync")
	if !strings.Contains(errOut, "unresolved: ~/"+ownPath+" is on branch feature, not main: local development state, not updated") ||
		!strings.Contains(out, "1 unresolved") {
		t.Errorf("sync on another branch:\n%s%s", out, errOut)
	}
	if head() != featureHead || w.git(own, "branch", "--show-current") != "feature" || w.exists(ownPath+"/skills/delta") {
		t.Error("sync switched, reset or pulled a working copy on another branch")
	}
	w.mustBeLinked("gamma")
	docOut, _ := w.mustRun(1, "doctor")
	if !strings.Contains(docOut, "wrong-branch") || !strings.Contains(docOut, "on branch feature; sync keeps it on main") {
		t.Errorf("doctor on another branch:\n%s", docOut)
	}

	// A detached HEAD is the same.
	w.git(own, "switch", "--quiet", "--detach", "HEAD~1")
	if _, errOut := w.mustRun(0, "sync"); !strings.Contains(errOut, "is on a detached HEAD, not main") {
		t.Errorf("detached HEAD:\n%s", errOut)
	}

	// Back on main with a local commit and a new upstream one: diverged.
	w.git(own, "switch", "--quiet", "main")
	writeFile(t, filepath.Join(own, "local.txt"), "local\n")
	w.git(own, "add", "-A")
	w.git(own, "commit", "--quiet", "-m", "chore: local")
	local := head()
	if _, errOut := w.mustRun(0, "sync"); !strings.Contains(errOut, "has diverged from origin/main") {
		t.Errorf("diverged:\n%s", errOut)
	}
	if head() != local {
		t.Error("sync reset a diverged working copy")
	}

	// Uncommitted changes.
	writeFile(t, filepath.Join(own, "local.txt"), "edited\n")
	if _, errOut := w.mustRun(0, "sync"); !strings.Contains(errOut, "has uncommitted changes: local development state") {
		t.Errorf("dirty:\n%s", errOut)
	}
	if readFile(t, filepath.Join(own, "local.txt")) != "edited\n" {
		t.Error("sync overwrote an uncommitted change")
	}

	// Another origin: an error; its skills are not used and its links are
	// not pruned; nothing in the directory changes.
	w.git(own, "remote", "set-url", "origin", "https://github.com/ext/tools.git")
	links := w.managedState()
	out, errOut = w.mustRun(1, "sync")
	if !strings.Contains(errOut, "unresolved: ~/"+ownPath+" is a working copy of https://github.com/ext/tools.git, not of https://github.com/me/skills.git") ||
		!strings.Contains(errOut, "some checkouts are not available; skipping removal") {
		t.Errorf("wrong origin:\n%s%s", out, errOut)
	}
	if w.managedState() != links || head() != local || readFile(t, filepath.Join(own, "local.txt")) != "edited\n" {
		t.Error("sync changed something because of a working copy of another repository")
	}
	docOut, _ = w.mustRun(1, "doctor")
	if !strings.Contains(docOut, "wrong-origin") {
		t.Errorf("doctor with another origin:\n%s", docOut)
	}
	w.git(own, "remote", "set-url", "origin", "https://user:secret-token@github.com/ext/tools.git")
	for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}, {"sync"}, {"list"}, {"config", "show"}} {
		_, out, errOut := w.run(args...)
		if strings.Contains(out+errOut, "secret-token") {
			t.Errorf("skenv %s printed the credentials of origin:\n%s%s", strings.Join(args, " "), out, errOut)
		}
	}
	w.git(own, "remote", "set-url", "origin", "https://github.com/ext/tools.git")
	listOut, _ := w.mustRun(0, "list")
	if !strings.Contains(listOut, "not used: skills: ~/"+ownPath+" is a working copy of") {
		t.Errorf("list with another origin:\n%s", listOut)
	}
	// Another spelling of the same repository (ssh instead of https) is the
	// same origin, also on a declared host whose ssh path differs.
	w.git(own, "remote", "set-url", "origin", "git@github.com:me/skills.git")
	if _, errOut := w.mustRun(0, "sync"); strings.Contains(errOut, "is a working copy of") {
		t.Errorf("an ssh origin of the same repository is not accepted:\n%s", errOut)
	}
}

// A declared branch is the one sync clones and keeps a checkout on.
func TestCheckoutBranch(t *testing.T) {
	w := newWorld(t)
	w.push("me/team", map[string]string{"skills/deploy/SKILL.md": skillMD("deploy", "")}, "feat: team")
	team := filepath.Join(w.work, "me__team")
	w.git(team, "switch", "--quiet", "-c", "stable")
	writeFile(t, filepath.Join(team, "skills/stable-only/SKILL.md"), skillMD("stable-only", ""))
	w.git(team, "add", "-A")
	w.git(team, "commit", "--quiet", "-m", "feat: stable")
	w.git(team, "push", "--quiet", "origin", "stable")
	w.initStandard("\n[user.checkouts.team]\nrepo = \"me/team\"\ncheckout_dir = \"~/src/team\"\nbranch = \"stable\"\n")
	if w.git(w.path("src/team"), "branch", "--show-current") != "stable" {
		t.Error("the checkout is not cloned on its branch")
	}
	w.mustBeLinked("stable-only")
	w.mustRun(0, "doctor")
	if _, errOut := w.mustRun(2, "doctor", "--manifest", writeManifestWith(t, w, "branch = \"-x\"")); !strings.Contains(errOut, `branch "-x" is not a branch name`) {
		t.Errorf("bad branch: %s", errOut)
	}
}

// writeManifestWith writes a copy of the manifest with extra lines in the
// team checkout and returns its path.
func writeManifestWith(t *testing.T, w *world, lines string) string {
	t.Helper()
	text := strings.Replace(readFile(t, w.path(ownPath+"/skenv.toml")), `branch = "stable"`, lines, 1)
	p := w.path("other-manifest/skenv.toml")
	writeFile(t, p, text)
	return p
}

// config show exposes the inputs from outside the file and explains the
// selection.
func TestConfigShow(t *testing.T) {
	w := newWorld(t)
	w.standard(`
[user.checkouts.later]
repo         = "me/later"
checkout_dir = "~/src/later"

[user.machines.laptop]
exclude = ["beta"]
`)
	w.mustRun(0, "clone", "me/skills", "~/"+ownPath)
	t.Setenv("SKENV_MACHINE", "laptop")
	t.Setenv("CLAUDE_CONFIG_DIR", w.path("cc"))
	mustMkdir(t, w.path("cc"))
	out, _ := w.mustRun(0, "config", "show")
	for _, want := range []string{
		"manifest  ~/" + ownPath + "/skenv.toml (tool config ~/.config/skenv/config.toml)",
		"machine   laptop ($SKENV_MACHINE; rules user.machines.laptop)",
		"claude    ~/cc ($CLAUDE_CONFIG_DIR)",
		"branch (default of origin)",
		"store     ~/.agents/skills (default)",
		"claude  ~/cc/skills", "detected: ~/cc exists",
		"pi      ~/.pi/agent/skills", "detected: ~/.pi/agent exists",
		"later   me/later", "not cloned: sync clones it",
		"branch main (default branch of origin)",
		`beta     checkout skills  not selected  include omitted: every skill; user.machines.laptop.include omitted: every skill, but excluded by user.machines.laptop.exclude "beta"`,
		"archify  dependency       selected      dependency ext/tools at",
		"note: dependencies are pinned to a commit; checkouts follow their branch",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("config show lacks %q:\n%s", want, out)
		}
	}
	out, _ = w.mustRun(0, "config", "show", "--json")
	var r struct {
		Machine struct{ Name, Source string }
		Agents  []struct {
			Agent string
			On    bool
			Why   string
		}
		Skills []struct {
			Name     string
			Selected bool
			Why      string
		}
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Machine.Name != "laptop" || len(r.Agents) != 2 || len(r.Skills) != 3 {
		t.Errorf("config show --json: %v\n%s", err, out)
	}
	if out, _ := w.mustRun(0, "config", "show", "--manifest", "~/"+ownPath); !strings.Contains(out, "(--manifest)") {
		t.Errorf("--manifest source:\n%s", out)
	}
	// A selection error is shown with the rest, exit code 1.
	manifest := w.path(ownPath + "/skenv.toml")
	writeFile(t, manifest, strings.Replace(readFile(t, manifest), "[user.checkouts.later]", "[user.checkouts.skills2]\nrepo = \"me/skills\"\ncheckout_dir = \"~/"+ownPath+"\"\n\n[user.checkouts.later]", 1))
	out, errOut := w.mustRun(1, "config", "show")
	if !strings.Contains(out, "machine   laptop") || !strings.Contains(errOut, `skill "alpha" is defined twice`) {
		t.Errorf("config show with a clash:\n%s%s", out, errOut)
	}
}

// The user and the project scope own separate resources: a project whose
// dir is a user-level directory is refused, the user-level sync never
// touches a project, and a skill in both scopes is reported, not ranked.
func TestScopeBoundaries(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")

	// $HOME as a git repository with [project] and the default dir.
	w.git(w.home, "init", "--quiet", "-b", "main")
	writeFile(t, w.path("skenv.toml"), "[project]\n")
	t.Chdir(w.home)
	before := w.managedState()
	if _, errOut := w.mustRun(2, "sync"); !strings.Contains(errOut, "[project] .agents/skills is the user-level skills directory ~/.agents/skills") {
		t.Errorf("project over the user store: %s", errOut)
	}
	if _, errOut := w.mustRun(2, "doctor", "--project"); !strings.Contains(errOut, "overlaps it") {
		t.Errorf("doctor --project over the user store: %s", errOut)
	}
	if w.managedState() != before {
		t.Error("a refused project changed the user scope")
	}
	if err := os.RemoveAll(w.path(".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(w.path("skenv.toml")); err != nil {
		t.Fatal(err)
	}

	// A project with a skill of the same name as a user-level one.
	vendorRev, _ := w.project("")
	_ = vendorRev
	w.mustRun(0, "sync")
	_, errOut := w.mustRun(0, "doctor")
	if !strings.Contains(errOut, `skill "archify" is also installed for your user (~/.agents/skills/archify)`) {
		t.Errorf("doctor in the project does not report the skill in both scopes:\n%s", errOut)
	}
	// The user-level sync neither reads nor prunes the project.
	projectBefore := snapshot(t, w.pp(""))
	w.mustRun(0, "sync", "--manifest", "~/"+ownPath)
	assertUnchanged(t, projectBefore, w.pp(""))
	if !w.exists(".agents/skills/archify/.skenv") {
		t.Error("the project sync touched the user scope")
	}
}

// The ssh form of a repository on a declared host is its origin too, even
// when the https base carries a path the ssh form does not.
func TestCheckoutOriginOnDeclaredHost(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://git.example.com/git/", "selfhosted")
	w.push("selfhosted/team/skills", map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", ""),
		"skenv.toml": "[user.git_hosts.work]\nbase_url = \"https://git.example.com/git\"\nprovider = \"gitlab\"\n\n[user.checkouts.skills]\nrepo = \"work:team/skills\"\ncheckout_dir = \".\"\n"}, "feat: skills")
	w.cloneSync("https://git.example.com/git/team/skills.git", "~/"+ownPath)
	w.git(w.path(ownPath), "remote", "set-url", "origin", "git@git.example.com:team/skills.git")
	out, _ := w.mustRun(0, "list")
	if strings.Contains(out, "not used") || !strings.Contains(out, "alpha") {
		t.Errorf("the ssh origin of the declared host is not accepted:\n%s", out)
	}
}

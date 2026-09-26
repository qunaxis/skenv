package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The format of skenv 0.5 is not read: every command stops with an error
// that lists each old key and its replacement.
func TestOldFormatIsRejected(t *testing.T) {
	w := newWorld(t)
	w.push("me/skills", map[string]string{"skenv.toml": `[repo]
harness = "0.5.0"
visibility = "private"

[environment.layout]
ignore = ["peon-*"]

[[environment.own]]
repo = "me/skills"
path = "~/src/skills"
skills = ["alpha"]
`}, "chore: 0.5 format")
	_, errOut := w.mustRun(2, "clone", "me/skills", "~/"+ownPath)
	for _, want := range []string{
		"uses keys of skenv before 0.6", "moving-to-the-0-6-format",
		"[repo] → [repository]", "repo.harness → repository.template_version",
		"[environment] → [user]", "environment.layout.ignore → user.unmanaged",
		"environment.own → user.checkouts.<id>", "environment.own.path → checkout_dir", "environment.own.skills → include",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("clone of an old manifest lacks %q:\n%s", want, errOut)
		}
	}
	for _, args := range [][]string{{"doctor", "--manifest", "~/" + ownPath}, {"repo", "check", "--dir", w.path(ownPath)}} {
		if _, errOut := w.mustRun(2, args...); !strings.Contains(errOut, "[environment] → [user]") && !strings.Contains(errOut, "[repo] → [repository]") {
			t.Errorf("skenv %s: %s", strings.Join(args, " "), errOut)
		}
	}
}

// user.agents chooses the agent directories: an explicit list whether or
// not the agent is detected, [] for none, extra directories added. Turning
// an agent off removes only the links skenv made there.
func TestAgentsSelection(t *testing.T) {
	w := newWorld(t)
	w.initStandard("\n[user.agents]\nenabled    = [\"pi\"]\nextra_dirs = [\"~/extra\"]\n")
	for _, p := range []string{".pi/agent/skills/alpha", "extra/alpha", "extra/archify"} {
		if !w.exists(p) {
			t.Errorf("%s not linked", p)
		}
	}
	if w.exists(".claude/skills/alpha") {
		t.Error("claude is not enabled but got links")
	}
	out, _ := w.mustRun(0, "list")
	if !strings.Contains(out, "agents    ~/.pi/agent/skills, ~/extra\n") {
		t.Errorf("list header:\n%s", out)
	}
	writeFile(t, w.path(".pi/agent/skills/handmade/SKILL.md"), "mine\n")
	manifest := w.path(ownPath + "/skenv.toml")
	writeFile(t, manifest, strings.Replace(readFile(t, manifest), `enabled    = ["pi"]`, `enabled    = []`, 1))
	out, _ = w.mustRun(0, "sync")
	if !strings.Contains(out, "remove ~/.pi/agent/skills/alpha") || w.exists(".pi/agent/skills/alpha") {
		t.Errorf("pi links not removed:\n%s", out)
	}
	if !w.exists(".pi/agent/skills/handmade/SKILL.md") || !w.exists("extra/alpha") || !w.exists(ownPath+"/skills/alpha/SKILL.md") {
		t.Error("an unmanaged path, an extra directory or a source was touched")
	}
	// A path override moves one agent only.
	writeFile(t, manifest, readFile(t, manifest)+"\n[user.agents.paths]\nclaude = \"~/cc/skills\"\n")
	writeFile(t, manifest, strings.Replace(readFile(t, manifest), `enabled    = []`, `enabled    = ["claude"]`, 1))
	w.mustRun(0, "sync", "--quiet")
	if !w.exists("cc/skills/alpha") || w.exists(".claude/skills/alpha") {
		t.Error("the path override of claude is not used")
	}
}

// A new storage.dir: sync creates the new store and removes only the
// managed entries of the old one; the old store can stay a destination.
func TestStorageMove(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	writeFile(t, w.path(".agents/skills/handmade/SKILL.md"), "mine\n")
	manifest := w.path(ownPath + "/skenv.toml")
	writeFile(t, manifest, readFile(t, manifest)+"\n[user.storage]\ndir = \"~/.local/share/skenv/skills\"\n")
	w.mustRun(0, "sync", "--quiet")
	if !w.exists(".local/share/skenv/skills/archify/.skenv") || w.readlink(".local/share/skenv/skills/alpha") != w.path(ownPath+"/skills/alpha") {
		t.Error("the new store is not complete")
	}
	if w.exists(".agents/skills/alpha") || w.exists(".agents/skills/archify") {
		t.Error("managed entries of the old store stayed")
	}
	if !w.exists(".agents/skills/handmade/SKILL.md") {
		t.Error("an unmanaged entry of the old store was removed")
	}
	if got := w.readlink(".claude/skills/archify"); got != "../../.local/share/skenv/skills/archify" {
		t.Errorf("claude link → %s", got)
	}
	// Codex keeps its skills when the old store is an extra directory.
	writeFile(t, manifest, readFile(t, manifest)+"\n[user.agents]\nextra_dirs = [\"~/.agents/skills\"]\n")
	w.mustRun(0, "sync", "--quiet")
	if w.readlink(".agents/skills/archify") != "../../.local/share/skenv/skills/archify" {
		t.Error("~/.agents/skills is not linked as an extra directory")
	}
	w.mustRun(1, "doctor") // the manifest edit is uncommitted: dirty

	// The store moved onto an agent directory: the links there are
	// replaced by the copies, and a second sync finds nothing to do.
	writeFile(t, manifest, strings.Replace(readFile(t, manifest), `dir = "~/.local/share/skenv/skills"`, `dir = "~/.claude/skills"`, 1))
	w.mustRun(0, "sync", "--quiet")
	if !w.exists(".claude/skills/archify/.skenv") {
		t.Error("the dependency is not copied into the new store")
	}
	if _, errOut := w.mustRun(0, "sync"); strings.Contains(errOut, "conflict") || strings.Contains(errOut, "error") {
		t.Errorf("second sync after moving the store onto an agent directory:\n%s", errOut)
	}
	if !w.exists(".claude/skills/archify/.skenv") || w.readlink(".agents/skills/archify") != "../../.claude/skills/archify" {
		t.Error("the store and its links do not survive a second sync")
	}
}

// Machine rules: an explicit machine ($SKENV_MACHINE or the tool config)
// may move a checkout and narrow the selection.
func TestMachineCheckoutDirs(t *testing.T) {
	w := newWorld(t)
	w.push("me/team", map[string]string{"skills/deploy/SKILL.md": skillMD("deploy", ""), "skills/review/SKILL.md": skillMD("review", "")}, "feat: team")
	w.initStandard(`
[user.checkouts.team]
repo         = "me/team"
checkout_dir = "~/src/team"

[user.machines.laptop]
exclude = ["review"]

[user.machines.laptop.checkout_dirs]
team = "~/work/team"
`)
	if !w.exists("src/team/skills/deploy") || !w.exists(".claude/skills/review") {
		t.Fatal("without a machine the rules of laptop apply")
	}
	t.Setenv("SKENV_MACHINE", "laptop")
	out, _ := w.mustRun(0, "sync")
	if !strings.Contains(out, "clone me/team") || !w.exists("work/team/skills/deploy") || w.readlink(".agents/skills/deploy") != w.path("work/team/skills/deploy") {
		t.Errorf("checkout_dirs of the machine:\n%s", out)
	}
	if w.exists(".claude/skills/review") || !w.exists("src/team/skills/review") {
		t.Error("the machine exclude removed more than the link")
	}
	t.Setenv("SKENV_MACHINE", "desktop")
	if _, errOut := w.mustRun(2, "sync"); !strings.Contains(errOut, `machine "desktop" ($SKENV_MACHINE) has no user.machines.desktop entry`) {
		t.Errorf("unknown machine: %s", errOut)
	}
}

// Relative paths follow the skenv file: sync gives the same result from
// any working directory, the root one of an autostart job included.
func TestRelativePathsFollowTheFile(t *testing.T) {
	w := newWorld(t)
	rev := w.standard("")
	text := strings.Replace(manifestText(rev, "\n[user.agents]\nextra_dirs = [\"../agent-links\"]\n"), `checkout_dir = "~/`+ownPath+`"`, `checkout_dir = "."`, 1)
	w.push("me/skills", map[string]string{"skenv.toml": text}, "chore: relative paths")
	for i, dir := range []string{"/", w.home, os.TempDir(), w.path(ownPath + "/skills")} {
		if i == 0 {
			w.mustRun(0, "clone", "me/skills", "~/"+ownPath)
		}
		t.Chdir(dir)
		out, _ := w.mustRun(0, "sync")
		if i > 0 && !strings.Contains(out, "sync: up to date") {
			t.Errorf("sync from %s changed something:\n%s", dir, out)
		}
		if w.readlink(".agents/skills/alpha") != w.path(ownPath+"/skills/alpha") || !w.exists("src/agent-links/alpha") {
			t.Errorf("from %s: links do not follow the skenv file", dir)
		}
	}
	w.mustRun(0, "doctor")
}

// base_url selects the transport: an ssh base clones over ssh; github:
// names github.com explicitly.
func TestGitTransport(t *testing.T) {
	w := newWorld(t)
	w.git(w.home, "config", "--global", "url.file://"+filepath.Join(w.remotes, "selfhosted")+"/.insteadOf", "ssh://git@git.example.com:2222/")
	w.pushTool("selfhosted/team/tools", "over-ssh")
	w.initStandard("\n[user.git_hosts.work]\nbase_url = \"ssh://git@git.example.com:2222\"\nprovider = \"gitlab\"\n")
	w.mustRun(0, "vendor", "add", "work:team/tools", "--path", "tool", "--name", "tool")
	if mk := readFile(t, w.path(".agents/skills/tool/.skenv")); !strings.Contains(mk, `repo = "ssh://git@git.example.com:2222/team/tools.git"`) {
		t.Errorf("not cloned over ssh:\n%s", mk)
	}
	w.mustRun(0, "vendor", "add", "github:ext/tools", "--path", "tools/other")
	if text := readFile(t, w.path(ownPath+"/skenv.toml")); !strings.Contains(text, `repo      = "github:ext/tools"`) || !w.exists(".agents/skills/other/SKILL.md") {
		t.Errorf("github: prefix:\n%s", text)
	}
}

package cli

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/harness"
)

// selectManifest is the manifest of the standard world with selection
// lines in its [user.checkouts.skills] entry.
func selectManifest(rev, ownLines, extra string) string {
	line := `checkout_dir = "~/` + ownPath + `"` + "\n"
	return strings.Replace(manifestText(rev, extra), line, line+ownLines, 1)
}

var allTargets = []string{".agents/skills/", ".claude/skills/", ".pi/agent/skills/"}

func (w *world) linked(name string) (all, none bool) {
	all, none = true, true
	for _, dir := range allTargets {
		if w.exists(dir + name) {
			none = false
		} else {
			all = false
		}
	}
	return all, none
}

func (w *world) mustBeLinked(name string) {
	w.t.Helper()
	if all, _ := w.linked(name); !all {
		w.t.Errorf("%s must be linked in the store and every agent directory", name)
	}
}

func (w *world) mustNotBeLinked(name string) {
	w.t.Helper()
	if _, none := w.linked(name); !none {
		w.t.Errorf("%s must not be linked anywhere", name)
	}
}

// Issue #9: include and exclude (names and globs) select skills from a
// checkout on every machine.
func TestOwnSelection(t *testing.T) {
	w := newWorld(t)
	rev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "")}, "feat: archify")
	skills := map[string]string{
		"skills/alpha/SKILL.md":        skillMD("alpha", ""),
		"skills/beta/SKILL.md":         skillMD("beta", ""),
		"skills/exp-one/SKILL.md":      skillMD("exp-one", ""),
		"skills/not.selected/SKILL.md": skillMD("not.selected", ""),
	}
	skills["skenv.toml"] = selectManifest(rev, "include = [\"alpha\", \"beta\", \"exp-*\"]\nexclude = [\"exp-*\", \"nothing-*\"]\n", "")
	w.push("me/skills", skills, "feat: selection")

	// The allowlist minus exclude: alpha and beta, in every target; a
	// directory with an invalid name that is not selected is not warned
	// about.
	_, errOut := w.cloneSync("me/skills", "~/"+ownPath)
	if strings.Contains(errOut, "not.selected") {
		t.Errorf("an unselected directory must not be warned about:\n%s", errOut)
	}
	w.mustBeLinked("alpha")
	w.mustBeLinked("beta")
	w.mustNotBeLinked("exp-one")
	w.mustNotBeLinked("not.selected")
	w.mustRun(0, "doctor")

	// A new skill in the repository is not linked while the allowlist is
	// set.
	w.push("me/skills", map[string]string{"skills/gamma/SKILL.md": skillMD("gamma", "")}, "feat: gamma")
	w.mustRun(0, "sync")
	w.mustNotBeLinked("gamma")
	w.mustRun(0, "doctor")

	// Removing a skill from the allowlist unlinks it; --dry-run shows the
	// plan first and doctor explains the leftover.
	w.push("me/skills", map[string]string{"skenv.toml": selectManifest(rev, "include = [\"alpha\", \"exp-one\"]\nexclude = [\"exp-*\"]\n", "")}, "chore: drop beta")
	w.git(w.path(ownPath), "pull", "--quiet", "--ff-only")
	out, _ := w.mustRun(1, "doctor")
	if !strings.Contains(out, "not selected by checkout skills") {
		t.Errorf("doctor must say beta is not selected:\n%s", out)
	}
	out, _ = w.mustRun(0, "sync", "--dry-run")
	if !strings.Contains(out, "would remove ~/.claude/skills/beta (skill \"beta\" is not selected by checkout skills") {
		t.Errorf("sync --dry-run must show the removal:\n%s", out)
	}
	w.mustBeLinked("beta")
	w.mustRun(0, "sync")
	w.mustNotBeLinked("beta")
	w.mustBeLinked("alpha")
	if !w.exists(ownPath + "/skills/beta/SKILL.md") {
		t.Error("the skill itself must stay in the repository")
	}
	w.mustRun(0, "doctor")

	// Machine rules apply after the selection and only narrow it.
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname")
	}
	w.push("me/skills", map[string]string{"skenv.toml": selectManifest(rev, "exclude = [\"exp-*\", \"gamma\"]\n",
		"\n[user.machines.\""+host+"\"]\ninclude = [\"*\"]\nexclude = [\"alpha\"]\n")}, "chore: exclude only")
	_, errOut = w.mustRun(0, "sync")
	if !strings.Contains(errOut, "not.selected") {
		t.Errorf("without an allowlist the invalid directory is warned about again:\n%s", errOut)
	}
	w.mustBeLinked("beta")
	w.mustNotBeLinked("alpha")
	w.mustNotBeLinked("gamma")
	w.mustNotBeLinked("exp-one")
	// A link left from before the exclusion is explained by the machine key.
	writeFile(t, w.path(ownPath+"/skenv.toml"), selectManifest(rev, "exclude = [\"exp-*\", \"gamma\"]\n", ""))
	w.mustRun(0, "link")
	writeFile(t, w.path(ownPath+"/skenv.toml"), selectManifest(rev, "exclude = [\"exp-*\", \"gamma\"]\n",
		"\n[user.machines.\""+host+"\"]\nexclude = [\"alpha\"]\n"))
	out, _ = w.mustRun(1, "doctor")
	key := host
	if strings.ContainsAny(host, ".") {
		key = strconv.Quote(host)
	}
	if !strings.Contains(out, "excluded on this machine (user.machines."+key+")") {
		t.Errorf("doctor must explain the skipped skill:\n%s", out)
	}
	w.git(w.path(ownPath), "checkout", "--quiet", "--", "skenv.toml")
	w.mustRun(0, "sync")
	out, _ = w.mustRun(0, "doctor", "--json")
	var report struct{ Skills int }
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.Skills != 2 { // beta, archify
		t.Errorf("doctor --json: %v\n%s", err, out)
	}
}

// A name in include that the repository does not have is an error that
// points at the entry; include = [] selects nothing, without an error.
func TestOwnSelectionErrors(t *testing.T) {
	w := newWorld(t)
	rev := w.initStandard("")
	w.push("me/skills", map[string]string{"skenv.toml": selectManifest(rev, "include = [\"alpha\", \"typo\", \"nothing-*\"]\n", "")}, "chore: typo")
	_, errOut := w.mustRun(2, "sync")
	if !strings.Contains(errOut, `user.checkouts.skills: include lists "typo", not found in ~/`+ownPath+"/skills") {
		t.Errorf("sync with an unknown name:\n%s", errOut)
	}
	_, errOut = w.mustRun(2, "doctor")
	if !strings.Contains(errOut, `"typo"`) {
		t.Errorf("doctor with an unknown name:\n%s", errOut)
	}
	// Nothing was removed by the failed sync.
	w.mustBeLinked("alpha")
	w.mustBeLinked("beta")

	writeFile(t, w.path(ownPath+"/skenv.toml"), selectManifest(rev, "include = []\n", ""))
	out, _ := w.mustRun(1, "doctor")
	if !strings.Contains(out, "not selected by checkout skills (include/exclude)") {
		t.Errorf("doctor with include = []:\n%s", out)
	}
	w.mustRun(0, "sync", "--quiet")
	w.mustNotBeLinked("alpha")
	w.mustNotBeLinked("beta")
	if !w.exists(ownPath + "/skills/alpha/SKILL.md") {
		t.Error("the source files of the checkout must stay")
	}
	// Only the uncommitted edit of the manifest is left to report.
	if out, _ = w.mustRun(1, "doctor"); !strings.Contains(out, "dirty") || strings.Contains(out, "extra-managed") || strings.Contains(out, "missing") {
		t.Errorf("doctor after sync with include = []:\n%s", out)
	}
}

// Two checkouts may hold a skill with the same name as long as only one of
// them selects it.
func TestOwnSelectionSameName(t *testing.T) {
	w := newWorld(t)
	rev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "")}, "feat: archify")
	w.push("team/shared", map[string]string{
		"skills/alpha/SKILL.md": skillMD("alpha", "team"),
		"skills/delta/SKILL.md": skillMD("delta", "team"),
	}, "feat: team skills")
	second := "\n[user.checkouts.shared]\nrepo = \"team/shared\"\ncheckout_dir = \"~/src/shared\"\n"
	w.push("me/skills", map[string]string{
		"skenv.toml":            selectManifest(rev, "", second+"include = [\"delta\"]\n"),
		"skills/alpha/SKILL.md": skillMD("alpha", "mine"),
	}, "feat: two repositories")
	w.cloneSync("me/skills", "~/"+ownPath)
	if got, want := w.readlink(".agents/skills/alpha"), w.path(ownPath+"/skills/alpha"); got != want {
		t.Errorf("alpha → %s, want %s", got, want)
	}
	w.mustBeLinked("delta")
	w.mustRun(0, "doctor")

	// Selecting it in both is the usual name clash.
	w.push("me/skills", map[string]string{"skenv.toml": selectManifest(rev, "", second+"include = [\"delta\", \"alpha\"]\n")}, "chore: clash")
	_, errOut := w.mustRun(2, "sync")
	if !strings.Contains(errOut, `skill "alpha" is defined twice`) {
		t.Errorf("sync with a clash:\n%s", errOut)
	}
	// Names are unique before the machine rules: excluding one side on a
	// machine does not help.
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname")
	}
	w.push("me/skills", map[string]string{"skenv.toml": selectManifest(rev, "",
		second+"include = [\"delta\", \"alpha\"]\n\n[user.machines.\""+host+"\"]\nexclude = [\"alpha\"]\n")}, "chore: clash with a machine rule")
	if _, errOut = w.mustRun(2, "sync"); !strings.Contains(errOut, `skill "alpha" is defined twice`) {
		t.Errorf("sync with a clash and a machine rule:\n%s", errOut)
	}
}

// skenv new in a checkout with an include list says that the new skill is
// not installed until it is listed.
func TestNewSkillNotSelected(t *testing.T) {
	w := newWorld(t)
	rev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "")}, "feat: archify")
	w.push("me/skills", map[string]string{
		"skenv.toml":            selectManifest(rev, "include = [\"alpha\"]\n", "\n[repository]\ntemplate_version = \""+harness.Latest+"\"\nvisibility = \"private\"\n"),
		"skills/alpha/SKILL.md": skillMD("alpha", ""),
	}, "feat: allowlist")
	w.cloneSync("me/skills", "~/"+ownPath)
	out, _ := w.mustRun(0, "new", "fresh")
	if !strings.Contains(out, "selects its skills with include, which does not match fresh; add it there") || strings.Contains(out, "skenv link") {
		t.Errorf("new without the hint:\n%s", out)
	}
	// Excluded by a pattern: adding it to include would not help.
	writeFile(t, w.path(ownPath+"/skenv.toml"), selectManifest(rev, "exclude = [\"exp-*\"]\n", "\n[repository]\ntemplate_version = \""+harness.Latest+"\"\nvisibility = \"private\"\n"))
	out, _ = w.mustRun(0, "new", "exp-one")
	if !strings.Contains(out, "exp-one matches exclude") || strings.Contains(out, "add it there") {
		t.Errorf("new of an excluded name:\n%s", out)
	}
	if out, _ = w.mustRun(0, "new", "plain"); !strings.Contains(out, "skenv link") {
		t.Errorf("new of a selected name:\n%s", out)
	}
}

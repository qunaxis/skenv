package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nameLineContains reports whether the line of out that starts with name
// (after leading whitespace) contains want; config show lists one skill per
// line, and the tabwriter column widths shift with the names.
func nameLineContains(out, name, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name+" ") && strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// selectionWhy explains a checkout's include/exclude (#56): TestConfigShow
// already pins the include-omitted case (with and without a machine-rule
// exclude); this pins the other branches, an explicit include that selects
// nothing, that a name does not match and that it does.
func TestConfigShowSelectionPatterns(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	manifestPath := w.path(ownPath + "/skenv.toml")
	base := readFile(t, manifestPath)
	patch := func(includeLine string) {
		t.Helper()
		text := strings.Replace(base, `checkout_dir = "~/`+ownPath+`"`, `checkout_dir = "~/`+ownPath+`"`+"\n"+includeLine, 1)
		writeFile(t, manifestPath, text)
	}

	patch(`include = []`)
	out, _ := w.mustRun(0, "config", "show")
	for _, name := range []string{"alpha", "beta"} {
		if !nameLineContains(out, name, "not selected") || !nameLineContains(out, name, "include = [] selects nothing") {
			t.Errorf("include = []: %s line:\n%s", name, out)
		}
	}

	patch(`include = ["al*"]`)
	out, _ = w.mustRun(0, "config", "show")
	if !nameLineContains(out, "alpha", "selected") || !nameLineContains(out, "alpha", `matched by include "al*"`) {
		t.Errorf("include = [\"al*\"]: alpha line:\n%s", out)
	}
	if !nameLineContains(out, "beta", "not selected") || !nameLineContains(out, "beta", "not matched by include") {
		t.Errorf("include = [\"al*\"]: beta line:\n%s", out)
	}
}

// targetBranch falls back to `git ls-remote --symref` when the working copy
// has no refs/remotes/origin/HEAD (TestCheckoutBranch already pins the
// explicit c.Branch and the normal origin/HEAD cases) (#56): it resolves
// the default branch from the remote when that succeeds, and reports the
// checkout as unresolved, not an error, when the remote cannot be reached.
func TestCheckoutBranchFallsBackToLsRemote(t *testing.T) {
	w := newWorld(t)
	w.push("me/team", map[string]string{"skills/deploy/SKILL.md": skillMD("deploy", "")}, "feat: team")
	w.initStandard("\n[user.checkouts.team]\nrepo = \"me/team\"\ncheckout_dir = \"~/src/team\"\n")
	dir := w.path("src/team")
	w.git(dir, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")

	out, _ := w.mustRun(0, "sync")
	if !strings.Contains(out, "up to date") {
		t.Errorf("sync without origin/HEAD should still resolve main via ls-remote:\n%s", out)
	}
	w.mustBeLinked("deploy")

	// The remote is unreachable: ls-remote itself fails.
	bare := filepath.Join(w.remotes, "me/team.git")
	if err := os.Rename(bare, bare+".moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(bare+".moved", bare) })
	_, errOut := w.mustRun(0, "sync")
	if !strings.Contains(errOut, "the default branch of origin is unknown (offline?): not updated; set branch of checkout team") {
		t.Errorf("unreachable origin:\n%s", errOut)
	}
}

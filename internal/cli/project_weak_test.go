package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectIssueDetails runs `doctor --json` and returns the detail of each
// issue keyed by "<class> <skill>".
func (w *world) projectIssueDetails() map[string]string {
	w.t.Helper()
	_, out, _ := w.run("doctor", "--json")
	var r struct {
		Issues []struct{ Class, Skill, Detail string }
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		w.t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, is := range r.Issues {
		got[is.Class+" "+is.Skill] = is.Detail
	}
	return got
}

// warnLinks pins the boundary check on symlinks inside a copied skill
// (#56): one that stays inside the skill is silent, one that escapes it
// (relative or absolute) is warned about, since it is committed with the
// project and followed by agents and CI.
func TestProjectWarnLinksOutOfSkill(t *testing.T) {
	w := newWorld(t)
	w.project("")
	clone := filepath.Join(w.work, "ext__tools")
	// Escapes the skill (into a sibling skill's directory).
	if err := os.Symlink("../other", filepath.Join(clone, "tools/archify/link")); err != nil {
		t.Fatal(err)
	}
	// Stays inside the skill: no warning wanted.
	if err := os.Symlink("scripts/run", filepath.Join(clone, "tools/archify/inside")); err != nil {
		t.Fatal(err)
	}
	w.push("ext/tools", map[string]string{}, "feat: links in and out of the skill")
	_, errOut := w.mustRun(0, "vendor", "update", "--project", "archify")
	if !strings.Contains(errOut, ".agents/skills/archify/link is a symlink out of the skill (to ../other); it is committed with the project as it is") {
		t.Errorf("warnLinks did not warn about the escaping symlink:\n%s", errOut)
	}
	if strings.Contains(errOut, "archify/inside is a symlink out of the skill") {
		t.Errorf("warnLinks warned about a symlink that stays inside the skill:\n%s", errOut)
	}
	if got := w.readlink(projectDir + "/.agents/skills/archify/inside"); got != "scripts/run" {
		t.Errorf("the in-skill symlink was not copied as it is: %s", got)
	}
}

// inRepo pins the check syncMirror and doctor use to tell a mirror symlink
// that merely points to the wrong place inside the repository (repointed
// by sync without --adopt) from one that escapes the repository (kept as
// it is unless --adopt says otherwise) (#56).
func TestProjectMirrorSymlinkBoundary(t *testing.T) {
	w := newWorld(t)
	w.project("")
	w.mustRun(0, "sync")

	// Repointed to a different, but still in-repo, target: inRepo is true.
	if err := os.Remove(w.pp(".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.agents/skills/archify", w.pp(".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	if got := w.projectIssueDetails()["broken-mirror alpha"]; got != "points to ../../.agents/skills/archify, "+
		"expected ../../.agents/skills/alpha; run `skenv sync`" {
		t.Errorf("in-repo mismatch: %q", got)
	}
	w.mustRun(0, "sync")
	if got := w.readlink(projectDir + "/.claude/skills/alpha"); got != "../../.agents/skills/alpha" {
		t.Errorf("sync did not repoint an in-repo symlink without --adopt: %s", got)
	}

	// Repointed out of the repository (relative, so this exercises inRepo
	// itself, not the filepath.IsAbs half of the check): inRepo is false.
	if err := os.Remove(w.pp(".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.path("src/victim"), "keep\n")
	if err := os.Symlink("../../../victim", w.pp(".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	if got := w.projectIssueDetails()["broken-mirror alpha"]; got != "points out of the repository to ../../../victim, "+
		"expected ../../.agents/skills/alpha; `skenv sync --adopt` backs it up and replaces it" {
		t.Errorf("out-of-repo mismatch: %q", got)
	}
	_, errOut := w.mustRun(1, "sync")
	if !strings.Contains(errOut, "conflict: .claude/skills/alpha is a symlink out of the repository (to ../../../victim), "+
		"or rerun with --adopt to move it to") {
		t.Errorf("sync without --adopt:\n%s", errOut)
	}
	if got := w.readlink(projectDir + "/.claude/skills/alpha"); got != "../../../victim" {
		t.Errorf("sync touched an out-of-repo symlink without --adopt: %s", got)
	}
	w.mustRun(0, "sync", "--adopt")
	if got := w.readlink(projectDir + "/.claude/skills/alpha"); got != "../../.agents/skills/alpha" {
		t.Errorf("--adopt did not replace the out-of-repo symlink: %s", got)
	}
	if got := readFile(t, w.path("src/victim")); got != "keep\n" {
		t.Errorf("--adopt must back up, not touch, the victim file: %q", got)
	}
}

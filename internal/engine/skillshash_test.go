package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/gitx"
)

// The order of localeCompare in Node 24, recorded from
// ["…"].sort((a, b) => a.localeCompare(b)).
func TestPathOrderIsLocaleCompare(t *testing.T) {
	want := []string{"10.md", "9.md", "a-b", "a/b", "ab", "alpha.md", "references/a_b.md", "references/a-b.md", "references/a.b.md",
		"scripts/run.sh", "skill.md", "Skill.md", "SKILL.md", "sub/kept.md", "Zeta.md"}
	got := slices.Clone(want)
	slices.Reverse(got)
	slices.SortFunc(got, pathOrder.CompareString)
	if !slices.Equal(got, want) {
		t.Errorf("order:\n got %q\nwant %q", got, want)
	}
}

// The hashes of skills 1.7.0 (its code, run with Node) over the folder
// that TestMatchFolderHash commits: computeSkillFolderHash of a clone, and
// computeSnapshotHash over the files a blob install keeps.
const (
	fixtureHash         = "9963dd2feec24e995558b5f13ab20dfa8ed5feece441ef1f94e0ca61b9341714"
	fixtureSnapshotHash = "c8edb92ea52ecfa1a9e184113ce6674d08f3e314fc415f0a131766b5cfec0128"
)

// Both hashes recomputed from a commit: every regular file in
// localeCompare order, without symlinks; the clone hash without
// node_modules, the snapshot hash without metadata.json and __pycache__.
func TestMatchFolderHash(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	files := map[string]string{
		"SKILL.md":                  "---\nname: fixture\ndescription: a test skill\n---\nBody.\n",
		"scripts/run.sh":            "#!/bin/sh\necho run\n",
		"references/a-b.md":         "dash\n",
		"references/a_b.md":         "under\n",
		"references/a.b.md":         "dot\n",
		"Zeta.md":                   "upper\n",
		"alpha.md":                  "lower\n",
		"10.md":                     "ten\n",
		"9.md":                      "nine\n",
		"node_modules/pkg/index.js": "skip\n",
		"sub/node_modules/x.js":     "skip2\n",
		"sub/kept.md":               "kept\n",
		"metadata.json":             "{\"a\":1}\n",
		"__pycache__/c.pyc":         "pyc\n",
	}
	for p, content := range files {
		full := filepath.Join(repo, "skills/fixture", p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(content, "#!") {
			mode = 0o755
		}
		if err := os.WriteFile(full, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("SKILL.md", filepath.Join(repo, "skills/fixture/link.md")); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("add", "-A")
	git("commit", "--quiet", "-m", "fixture")
	commit := git("rev-parse", "HEAD")

	e := &base{env: Env{Git: gitx.Git{}}, ctx: context.Background()}
	for hash, want := range map[string]string{fixtureHash: commit, fixtureSnapshotHash: commit, strings.Repeat("0", 64): ""} {
		got, err := e.matchFolderHash(repo, []string{commit}, "skills/fixture", hash)
		if err != nil || got != want {
			t.Errorf("matchFolderHash(%.12s) = %q, %v; want %q", hash, got, err, want)
		}
	}
	if got, err := e.matchFolderHash(repo, []string{commit}, "skills/none", fixtureHash); got != "" || err != nil {
		t.Errorf("missing folder: %q, %v", got, err)
	}
	// The same files in memory, in any order.
	var list []hashFile
	for p, content := range files {
		if !hashedOut(p) {
			list = append(list, hashFile{path: p, content: []byte(content)})
		}
	}
	if got := skillsFolderHash(list); got != fixtureHash {
		t.Errorf("skillsFolderHash = %s, want %s", got, fixtureHash)
	}
}

func TestReadBatch(t *testing.T) {
	got := map[string][]byte{}
	out := []byte("aa blob 3\nx\ny\nbb blob 0\n\n")
	if err := readBatch(out, got); err != nil {
		t.Fatal(err)
	}
	if string(got["aa"]) != "x\ny" || len(got["bb"]) != 0 || len(got) != 2 {
		t.Errorf("readBatch = %q", got)
	}
	if err := readBatch([]byte("cc missing\n"), got); err == nil {
		t.Error("a missing object must fail")
	}
}

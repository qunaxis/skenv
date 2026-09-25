package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockFolder(t *testing.T) {
	for in, want := range map[string]string{
		"skills/archify/SKILL.md": "skills/archify",
		"skills\\win\\skill.md":   "skills/win",
		"SKILL.md":                "",
		"":                        "",
	} {
		if got := lockFolder(in); got != want {
			t.Errorf("lockFolder(%q) = %q, want %q", in, got, want)
		}
	}
}

// The blob ids of an installed copy are git's, without the files the
// skills CLI does not copy.
func TestCopyBlobs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for p, content := range map[string]string{
		"SKILL.md": "---\nname: x\n---\n", "scripts/run.sh": "#!/bin/sh\n", "metadata.json": "{}", "__pycache__/a.pyc": "x",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := copyBlobs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("copyBlobs = %v, want SKILL.md and scripts/run.sh", got)
	}
	out, err := exec.Command("git", "hash-object", filepath.Join(dir, "scripts/run.sh")).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(string(out)); got["scripts/run.sh"] != want {
		t.Errorf("blob id %s, git says %s", got["scripts/run.sh"], want)
	}
	if !copiedOut("a/metadata.json") || !copiedOut(".git/x") || copiedOut("git/x") {
		t.Error("copiedOut")
	}
}

func TestLineDiff(t *testing.T) {
	if d := lineDiff("f", []byte("a\n"), []byte("a\n")); d != "" {
		t.Errorf("equal: %q", d)
	}
	got := lineDiff("f", []byte("1\n2\n3\n4\n"), []byte("1\n2\n3\nnew\n4\n"))
	if want := "--- f\n+++ f\n 2\n 3\n+new\n 4\n"; got != want {
		t.Errorf("diff:\n%s\nwant:\n%s", got, want)
	}
	if got := lineDiff("f", nil, []byte("x")); got != "--- f\n+++ f\n+x\n" {
		t.Errorf("new file: %q", got)
	}
}

// Rewriting the lock keeps the other entries and keys as they were.
func TestLockWithout(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".skill-lock.json")
	in := `{"version": 3, "skills": {"a": {"source": "o/a", "sourceType": "github", "skillPath": "SKILL.md"}, "b": {"source": "x<y>&z"}}, "dismissed": {"p": true}}`
	if err := os.WriteFile(p, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := readSkillsLock(p, skillsLockVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.without([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want := "{\n  \"dismissed\": {\n    \"p\": true\n  },\n  \"skills\": {\n    \"b\": {\n      \"source\": \"x<y>&z\"\n    }\n  },\n  \"version\": 3\n}"
	if string(got) != want {
		t.Errorf("lock:\n%s\nwant:\n%s", got, want)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if err := os.WriteFile(p, []byte(`{"version": 2, "skills": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSkillsLock(p, skillsLockVersion); err == nil || !strings.Contains(err.Error(), "version 2 is not supported") {
		t.Errorf("version 2: %v", err)
	}
}

// A project lock is written as the skills CLI writes it, and removed once
// it has no skills left.
func TestProjectLockWithout(t *testing.T) {
	p := filepath.Join(t.TempDir(), projectLockName)
	in := `{"version": 1, "skills": {"a": {"source": "o/a", "computedHash": "x"}, "b": {"source": "o/b"}}}`
	if err := os.WriteFile(p, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := readSkillsLock(p, projectLockVersion)
	if err != nil {
		t.Fatal(err)
	}
	if l.entries["a"].ComputedHash != "x" {
		t.Errorf("entry a = %+v", l.entries["a"])
	}
	if err := l.without([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if want := "{\n  \"version\": 1,\n  \"skills\": {\n    \"b\": {\n      \"source\": \"o/b\"\n    }\n  }\n}\n"; string(got) != want {
		t.Errorf("lock:\n%s\nwant:\n%s", got, want)
	}
	if err := l.without([]string{"b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("an empty project lock stays: %v", err)
	}
}

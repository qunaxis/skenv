package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceNewFileDefaultsTo0644(t *testing.T) {
	file := filepath.Join(t.TempDir(), "new.txt")
	if err := Replace(file, []byte("a")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, want 0644", got)
	}
	if got, _ := os.ReadFile(file); string(got) != "a" {
		t.Errorf("content = %q, want %q", got, "a")
	}
}

func TestReplaceKeepsExistingMode(t *testing.T) {
	file := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(file, []byte("new")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 0600", got)
	}
	if got, _ := os.ReadFile(file); string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

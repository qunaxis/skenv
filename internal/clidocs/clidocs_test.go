package clidocs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The committed reference must match the command definitions; CI runs
// this, so a flag or help change without `make docs` fails the build.
func TestReferenceUpToDate(t *testing.T) {
	want := filepath.Join("..", "..", "docs", "commands")
	got := t.TempDir()
	if err := Generate(got, want); err != nil {
		t.Fatal(err)
	}
	names := func(dir string) []string {
		m, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		for i := range m {
			m[i] = filepath.Base(m[i])
		}
		return m
	}
	if g, w := names(got), names(want); !slices.Equal(g, w) {
		t.Fatalf("docs/commands is stale: run `make examples docs`\ngenerated: %v\ncommitted: %v", g, w)
	}
	for _, n := range names(got) {
		g, _ := os.ReadFile(filepath.Join(got, n))
		w, _ := os.ReadFile(filepath.Join(want, n))
		if string(g) != string(w) {
			t.Errorf("docs/commands/%s is stale: run `make examples docs`", n)
		}
	}
}

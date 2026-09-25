package clidocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Man pages are generated at release time (goreleaser before hook) and
// not committed; this keeps the generation working and complete.
func TestGenerateMan(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "skenv-gone.1")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := GenerateMan(dir, "1.2.3", date); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale page %s kept", stale)
	}
	pages, err := filepath.Glob(filepath.Join(dir, "*.1"))
	if err != nil {
		t.Fatal(err)
	}
	md, err := filepath.Glob(filepath.Join("..", "..", "docs", "commands", "skenv*.md"))
	if err != nil {
		t.Fatal(err)
	}
	// One page per command in the Markdown reference.
	if len(pages) != len(md) {
		t.Errorf("%d man pages for %d commands", len(pages), len(md))
	}
	for _, name := range []string{"skenv.1", "skenv-vendor-add.1", "skenv-completion-zsh.1"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Error(err)
			continue
		}
		s := string(b)
		for _, want := range []string{".TH ", `"1" "Sep 2026" "skenv 1.2.3" "skenv manual"`, ".SH NAME", ".SH SYNOPSIS"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
	// Examples without the recorded output, as in --help.
	if b, err := os.ReadFile(filepath.Join(dir, "skenv-doctor.1")); err != nil || !strings.Contains(string(b), ".SH EXAMPLE") || strings.Contains(string(b), "ok: 3 skills") {
		t.Errorf("skenv-doctor.1: examples missing or with output (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skenv-completion-powershell.1")); !os.IsNotExist(err) {
		t.Error("man page for completion powershell")
	}
}

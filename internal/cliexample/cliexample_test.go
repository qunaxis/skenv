package cliexample

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	got := Parse("  # Show the plan\n  # first\n  skenv sync --dry-run\n\n  skenv sync\n  source <(skenv completion bash)")
	want := []Example{
		{Comment: "Show the plan first", Line: "skenv sync --dry-run"},
		{Line: "skenv sync"},
		{Line: "source <(skenv completion bash)"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Parse = %#v", got)
	}
	if args, ok := got[0].Args(); !ok || !slices.Equal(args, []string{"sync", "--dry-run"}) {
		t.Errorf("Args = %q, %v", args, ok)
	}
	for _, line := range []string{"source <(skenv completion bash)", "skenv schema > x.json", "skenv lint | less", "git status", `skenv lint "a b"`} {
		if _, ok := (Example{Line: line}).Args(); ok {
			t.Errorf("%q is runnable", line)
		}
	}
}

func TestOutputRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := File("skenv vendor add", 2); got != filepath.Join("examples", "skenv_vendor_add", "2.txt") {
		t.Errorf("File = %s", got)
	}
	p := filepath.Join(dir, "1.txt")
	want := Output{Code: 1, Text: "a\nb\n"}
	if err := os.WriteFile(p, []byte(want.Format()), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := Read(p); err != nil || !ok || got != want {
		t.Errorf("Read = %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := Read(filepath.Join(dir, "none.txt")); ok || err != nil {
		t.Errorf("missing file: %v, %v", ok, err)
	}
	if err := os.WriteFile(p, []byte("no header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(p); err == nil {
		t.Error("a file without the exit line is accepted")
	}
}

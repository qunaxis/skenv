package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestTargets(t *testing.T) {
	home := t.TempDir()
	store := filepath.Join(home, ".agents", "skills")
	none := env(nil)

	// A2: no agent base directories → no targets.
	if got := Targets(home, none, nil, store); len(got) != 0 {
		t.Errorf("targets without agents = %v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := Targets(home, none, nil, store), []string{filepath.Join(home, ".claude", "skills")}; !reflect.DeepEqual(got, want) {
		t.Errorf("claude only: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Targets(home, none, nil, store); len(got) != 2 || got[1] != filepath.Join(home, ".pi", "agent", "skills") {
		t.Errorf("claude+pi: %v", got)
	}

	// A1: $CLAUDE_CONFIG_DIR replaces ~/.claude.
	cfg := filepath.Join(home, "cc")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Targets(home, env(map[string]string{"CLAUDE_CONFIG_DIR": cfg}), nil, store); got[0] != filepath.Join(cfg, "skills") {
		t.Errorf("CLAUDE_CONFIG_DIR: %v", got)
	}

	// A3: layout.targets overrides the table; the store is never a target
	// (Codex reads it directly, A0).
	got := Targets(home, none, []string{"~/x", "~/.agents/skills", "~/x"}, store)
	if !reflect.DeepEqual(got, []string{filepath.Join(home, "x")}) {
		t.Errorf("override: %v", got)
	}
	if got := Targets(home, none, []string{}, store); len(got) != 0 {
		t.Errorf("empty override: %v", got)
	}
}

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
	if got := Targets(home, none, Selection{}, store); len(got) != 0 {
		t.Errorf("targets without agents = %v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := Targets(home, none, Selection{}, store), []string{filepath.Join(home, ".claude", "skills")}; !reflect.DeepEqual(got, want) {
		t.Errorf("claude only: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Targets(home, none, Selection{}, store); len(got) != 2 || got[1] != filepath.Join(home, ".pi", "agent", "skills") {
		t.Errorf("claude+pi: %v", got)
	}

	// A1: $CLAUDE_CONFIG_DIR replaces ~/.claude.
	cfg := filepath.Join(home, "cc")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Targets(home, env(map[string]string{"CLAUDE_CONFIG_DIR": cfg}), Selection{}, store); got[0] != filepath.Join(cfg, "skills") {
		t.Errorf("CLAUDE_CONFIG_DIR: %v", got)
	}

	// An explicit list selects exactly those agents, detected or not;
	// [] selects none.
	if got := Targets(home, none, Selection{Enabled: []string{"pi"}}, store); !reflect.DeepEqual(got, []string{filepath.Join(home, ".pi", "agent", "skills")}) {
		t.Errorf("enabled pi: %v", got)
	}
	if got := Targets(home, none, Selection{Enabled: []string{}}, store); len(got) != 0 {
		t.Errorf("enabled []: %v", got)
	}
	// A path override moves one agent; extra directories add, and the
	// store and duplicates are never destinations (Codex reads the store
	// directly, A0).
	x := filepath.Join(home, "x")
	got := Targets(home, none, Selection{Paths: map[string]string{"pi": x}, ExtraDirs: []string{x, store, filepath.Join(home, "y")}}, store)
	want := []string{filepath.Join(home, ".claude", "skills"), x, filepath.Join(home, "y")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paths and extra_dirs: %v, want %v", got, want)
	}
}

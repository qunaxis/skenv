// Package agents holds the built-in table of agent skill directories.
package agents

import (
	"os"
	"path/filepath"

	"github.com/qunaxis/skenv/internal/paths"
)

// Agent describes where one agent reads global skills from.
type Agent struct {
	Name string
	// Base must exist for the agent to be considered installed (A2).
	Base string
	// Skills is the agent's global skills directory.
	Skills string
}

// Table returns the built-in agents for home (A1). getenv is used for
// $CLAUDE_CONFIG_DIR. Codex reads the store (~/.agents/skills) directly, so
// it has no entry of its own (A0).
func Table(home string, getenv func(string) string) []Agent {
	claude := filepath.Join(home, ".claude")
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		claude = paths.Expand(home, dir)
	}
	pi := filepath.Join(home, ".pi", "agent")
	return []Agent{
		{Name: "claude", Base: claude, Skills: filepath.Join(claude, "skills")},
		{Name: "pi", Base: pi, Skills: filepath.Join(pi, "skills")},
	}
}

// Targets resolves the directories skills are linked into. A non-nil
// override (layout.targets) replaces the table entirely (A3); otherwise an
// agent is included only when its base directory exists (A2). The store
// itself is never a target.
func Targets(home string, getenv func(string) string, override []string, store string) []string {
	var out []string
	add := func(p string) {
		if p == store {
			return
		}
		for _, q := range out {
			if q == p {
				return
			}
		}
		out = append(out, p)
	}
	if override != nil {
		for _, t := range override {
			add(paths.Expand(home, t))
		}
		return out
	}
	for _, a := range Table(home, getenv) {
		if fi, err := os.Stat(a.Base); err == nil && fi.IsDir() {
			add(a.Skills)
		}
	}
	return out
}

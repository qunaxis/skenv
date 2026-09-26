// Package agents holds the built-in table of agent skill directories and
// resolves user.agents into the directories skills are linked into.
package agents

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/qunaxis/skenv/internal/paths"
)

// Names are the built-in agents, the values of user.agents.enabled.
var Names = []string{"claude", "pi"}

// Agent describes where one agent reads global skills from.
type Agent struct {
	Name string
	// Base must exist for the agent to be detected (A2).
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

// Selection is user.agents with its paths resolved.
type Selection struct {
	// Enabled lists the agents to link for; nil detects them.
	Enabled []string
	// Paths overrides the skills directory of an agent.
	Paths map[string]string
	// ExtraDirs are added directories.
	ExtraDirs []string
}

// Destination is one agent, or an extra directory, and whether skills are
// linked into Dir.
type Destination struct {
	Agent string // "" for an extra directory
	Dir   string
	On    bool
	// Why says how On was decided, for `skenv config show`.
	Why string
}

// Resolve returns every built-in agent, on or off, then the extra
// directories. An explicit Enabled selects exactly those agents whether
// or not they are detected; nil selects the agents whose base directory
// exists (A2). The store is never a destination, and a directory appears
// once.
func Resolve(home string, getenv func(string) string, sel Selection, store string) []Destination {
	var out []Destination
	seen := map[string]bool{store: true}
	for _, a := range Table(home, getenv) {
		d := Destination{Agent: a.Name, Dir: a.Skills}
		if p, ok := sel.Paths[a.Name]; ok {
			d.Dir = p
		}
		switch {
		case sel.Enabled != nil && slices.Contains(sel.Enabled, a.Name):
			d.On, d.Why = true, "listed in user.agents.enabled"
		case sel.Enabled != nil:
			d.Why = "not listed in user.agents.enabled"
		case isDir(a.Base):
			d.On, d.Why = true, "detected: "+paths.Collapse(home, a.Base)+" exists"
		default:
			d.Why = "not detected: " + paths.Collapse(home, a.Base) + " does not exist"
		}
		if d.On && seen[d.Dir] {
			d.On, d.Why = false, "same directory as the store or another destination"
		}
		if d.On {
			seen[d.Dir] = true
		}
		out = append(out, d)
	}
	for _, dir := range sel.ExtraDirs {
		d := Destination{Dir: dir, On: true, Why: "listed in user.agents.extra_dirs"}
		if seen[dir] {
			d.On, d.Why = false, "same directory as the store or another destination"
		}
		seen[dir] = true
		out = append(out, d)
	}
	return out
}

// Targets returns the directories of Resolve that are on.
func Targets(home string, getenv func(string) string, sel Selection, store string) []string {
	var out []string
	for _, d := range Resolve(home, getenv, sel, store) {
		if d.On {
			out = append(out, d.Dir)
		}
	}
	return out
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

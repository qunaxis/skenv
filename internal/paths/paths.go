// Package paths resolves the locations skenv reads and writes.
package paths

import (
	"path/filepath"
	"strings"
)

// Expand replaces a leading "~" with home and cleans the result.
func Expand(home, p string) string {
	switch {
	case p == "~":
		return filepath.Clean(home)
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return filepath.Clean(p)
}

// Collapse is the inverse of Expand for display: paths under home start
// with "~".
func Collapse(home, p string) string {
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		if rel == "." {
			return "~"
		}
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

// Layout holds the fixed skenv locations under a home directory.
type Layout struct{ Home string }

// State is ~/.local/state/skenv.
func (l Layout) State() string { return filepath.Join(l.Home, ".local", "state", "skenv") }

// StateFile is ~/.local/state/skenv/state.json.
func (l Layout) StateFile() string { return filepath.Join(l.State(), "state.json") }

// Backup is ~/.local/state/skenv/backup.
func (l Layout) Backup() string { return filepath.Join(l.State(), "backup") }

// AutostartLog is ~/.local/state/skenv/autostart.log.
func (l Layout) AutostartLog() string { return filepath.Join(l.State(), "autostart.log") }

// Cache is ~/.cache/skenv/repos.
func (l Layout) Cache() string { return filepath.Join(l.Home, ".cache", "skenv", "repos") }

// ConfigFile is ~/.config/skenv/config.toml.
func (l Layout) ConfigFile() string { return filepath.Join(l.Home, ".config", "skenv", "config.toml") }

// DefaultManifest is ~/Personal/lab/skills-private/env.toml.
func (l Layout) DefaultManifest() string {
	return filepath.Join(l.Home, "Personal", "lab", "skills-private", "env.toml")
}

// DefaultStore is ~/.agents/skills.
func (l Layout) DefaultStore() string { return filepath.Join(l.Home, ".agents", "skills") }

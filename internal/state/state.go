// Package state records which paths skenv created, so that it only ever
// removes or replaces its own paths.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/qunaxis/skenv/internal/atomicfile"
)

// Kind of a managed path.
type Kind string

const (
	// Link is a symlink created by skenv (store → own repo, target → store).
	Link Kind = "link"
	// VendorDir is a vendored skill directory copied into the store.
	VendorDir Kind = "vendor"
)

// Entry describes one managed path.
type Entry struct {
	Kind  Kind   `json:"kind"`
	Skill string `json:"skill"`
}

// State is the content of state.json.
type State struct {
	Version int              `json:"version"`
	Managed map[string]Entry `json:"managed"`
}

// Load reads file; a missing file is an empty state.
func Load(file string) (*State, error) {
	s := &State{Version: 1, Managed: map[string]Entry{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("state %s is corrupt: %w (move it away to start over; unmanaged paths are never deleted)", file, err)
	}
	if s.Managed == nil {
		s.Managed = map[string]Entry{}
	}
	return s, nil
}

// Save writes the state atomically.
func (s *State) Save(file string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(file, append(data, '\n'), 0o644)
}

// Is reports whether path is managed.
func (s *State) Is(path string) bool { _, ok := s.Managed[path]; return ok }

// Paths returns managed paths in sorted order.
func (s *State) Paths() []string {
	out := make([]string, 0, len(s.Managed))
	for p := range s.Managed {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

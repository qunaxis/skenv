package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/manifest"
)

// The user scope (the manifest) and the project scope ([project]) own
// separate resources (docs/adr/0002-config-format.md, gap 7): the store and
// agent directories recorded in state.json, and a project's dir and
// mirrors with their .skenv markers. These helpers keep them apart.

// userDirs are the directories the user scope manages: the default store
// and the built-in agent directories, and, when a manifest is configured
// and reads, its store, agent directories and extra directories.
func userDirs(ctx context.Context, env Env) []string {
	dirs := []string{filepath.Join(env.Home, ".agents", "skills")}
	for _, a := range agents.Table(env.Home, env.Getenv) {
		dirs = append(dirs, a.Skills)
	}
	if mp, err := ResolveManifest(ctx, env, ""); err == nil {
		if m, err := manifest.Load(mp); err == nil {
			if m.Storage.Dir != "" {
				dirs = append(dirs, m.Path(env.Home, m.Storage.Dir))
			}
			for _, p := range m.Agents.Paths {
				dirs = append(dirs, m.Path(env.Home, p))
			}
			for _, d := range m.Agents.ExtraDirs {
				dirs = append(dirs, m.Path(env.Home, d))
			}
		}
	}
	return slices.Compact(dirs)
}

// checkScope refuses a project whose dir or a mirror is, contains or lies
// inside a directory of the user scope: both would manage the same
// entries. A home directory that is itself a git repository with
// [project] and dir ".agents/skills" is the usual case.
func (e *ProjectEngine) checkScope(user []string) error {
	for _, rel := range append([]string{e.p.Dir}, e.p.Mirrors...) {
		p := resolveExisting(e.abs(rel))
		for _, u := range user {
			ru := resolveExisting(u)
			if p == ru || strings.HasPrefix(p, ru+string(filepath.Separator)) || strings.HasPrefix(ru, p+string(filepath.Separator)) {
				return fmt.Errorf("[project] %s is the user-level skills directory %s, or overlaps it: the project and the user scope "+
					"would manage the same entries; choose another dir or mirror in [project]", rel, e.show(u))
			}
		}
	}
	return nil
}

// shadowed describes each skill of the project that is also installed for
// the user: skenv installs both, and the agent decides which it uses.
func (e *ProjectEngine) shadowed(user []string) []string {
	var out []string
	for _, name := range e.skillNames() {
		for _, u := range user {
			p := filepath.Join(u, name)
			if _, err := os.Lstat(p); err == nil {
				out = append(out, fmt.Sprintf("skill %q is also installed for your user (%s); skenv keeps both, "+
					"and the agent decides which one it uses", name, e.show(p)))
				break
			}
		}
	}
	return out
}

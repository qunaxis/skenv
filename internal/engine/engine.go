// Package engine implements the skenv commands: sync, link, doctor, vendor,
// init and import.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
	"github.com/qunaxis/skenv/internal/state"
)

// Env is everything the engine takes from the outside world.
type Env struct {
	Home     string
	Getenv   func(string) string
	Hostname string
	Stdout   io.Writer
	Stderr   io.Writer
	Git      gitx.Git
	Now      func() time.Time
}

// Options are the flags shared by the mutating commands.
type Options struct {
	Manifest string // --manifest
	DryRun   bool   // N3
	Adopt    bool   // B2
	Quiet    bool   // sync --quiet
	ReadOnly bool   // doctor: no lock, no writes
}

// Exit codes shared by all commands.
const (
	ExitOK       = 0
	ExitProblems = 1 // discrepancies (doctor) or conflicts/errors in the report
	ExitFatal    = 2 // the command could not run
)

// Engine is one command invocation over a loaded manifest and state.
type Engine struct {
	env          Env
	opts         Options
	layout       paths.Layout
	manifestPath string
	m            *manifest.Manifest
	store        string
	targets      []string
	st           *state.State
	stateDirty   bool
	backupDir    string
	ctx          context.Context
	lockFile     *os.File

	changes  int
	warnings int
	errs     int
	// ownUnavailable is set when an own repository could not be listed;
	// pruning is skipped then so its links are not mistaken for stale ones.
	ownUnavailable bool
	// unselected says why a skill that exists is not installed here: not
	// selected by skills/exclude of its own repository, or skipped on this
	// host. Set by skills.
	unselected map[string]string
	// fetched maps a repository to its clone cache, fetched once per
	// import.
	fetched map[string]string
}

// ErrNoManifest means no manifest location is configured. skenv does not
// guess one: the skills repository can live anywhere and have any name.
var ErrNoManifest = errors.New("no manifest configured: run `skenv init <owner/repo>` to clone your skills repository " +
	"and record its skenv.toml, or pass --manifest FILE (or set $SKENV_MANIFEST)")

// ResolveManifest picks the manifest, the skenv file with [environment]:
// --manifest, $SKENV_MANIFEST, then `manifest` in the config file
// (~/.config/skenv/config.{toml,yaml,yml,json}). Each may name the file or
// the directory that holds it. Without any of them it returns ErrNoManifest.
func ResolveManifest(env Env, flag string) (string, error) {
	m, _, err := config.Resolve(env.Home, env.Getenv, "manifest", flag, "")
	if err != nil {
		return "", err
	}
	if m == "" {
		return "", ErrNoManifest
	}
	return manifest.Locate(paths.Expand(env.Home, m))
}

// Open loads the manifest and state.
func Open(ctx context.Context, env Env, opts Options) (*Engine, error) {
	if err := gitx.Available(); err != nil {
		return nil, err
	}
	mp, err := ResolveManifest(env, opts.Manifest)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(mp)
	if err != nil {
		return nil, err
	}
	return open(ctx, env, opts, mp, m)
}

// open takes the lock and loads the state for the manifest m of the skenv
// file mp, which need not exist yet (`init --import`).
func open(ctx context.Context, env Env, opts Options, mp string, m *manifest.Manifest) (*Engine, error) {
	if env.Now == nil {
		env.Now = time.Now
	}
	layout := paths.Layout{Home: env.Home}
	e := &Engine{env: env, opts: opts, layout: layout, manifestPath: mp, ctx: ctx}
	if !opts.ReadOnly && !opts.DryRun {
		if err := e.lock(); err != nil {
			return nil, err
		}
	}
	// Load the state under the lock so a concurrent run cannot be lost.
	st, err := state.Load(layout.StateFile())
	if err != nil {
		e.Close()
		return nil, err
	}
	e.st = st
	e.setManifest(m)
	return e, nil
}

func (e *Engine) setManifest(m *manifest.Manifest) {
	e.m = m
	e.store = e.layout.DefaultStore()
	if m.Layout.Store != "" {
		e.store = paths.Expand(e.env.Home, m.Layout.Store)
	}
	e.targets = agents.Targets(e.env.Home, e.env.Getenv, m.Layout.Targets, e.store)
}

// Skill is a manifest skill resolved against the file system.
type Skill struct {
	Name   string
	OwnDir string // for own skills: <own.path>/<skills_dir>/<name>
	Vendor *manifest.Vendor
}

// skipped maps the skills skipped on this host to the host key that skips
// them: the full hostname, or the short one.
func (e *Engine) skipped() map[string]string {
	skip := map[string]string{}
	if short, _, ok := strings.Cut(e.env.Hostname, "."); ok {
		for k := range e.m.Skipped(short) {
			skip[k] = short
		}
	}
	for k := range e.m.Skipped(e.env.Hostname) {
		skip[k] = e.env.Hostname
	}
	return skip
}

// ownPath is the expanded working copy path of o.
func (e *Engine) ownPath(o *manifest.Own) string { return paths.Expand(e.env.Home, o.Path) }

// skills lists every skill of the manifest that applies to this host.
// Own repositories that are not cloned yet contribute no skills.
func (e *Engine) skills() ([]Skill, error) {
	own := map[int][]string{}
	e.ownUnavailable = false
	e.unselected = map[string]string{}
	for i := range e.m.Own {
		o := &e.m.Own[i]
		dir := filepath.Join(e.ownPath(o), filepath.FromSlash(o.SkillsDir))
		entries, err := os.ReadDir(dir)
		if err != nil {
			e.ownUnavailable = true
			continue
		}
		var found []string
		for _, de := range entries {
			if strings.HasPrefix(de.Name(), ".") || !de.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, de.Name(), "SKILL.md")); err != nil {
				continue
			}
			found = append(found, de.Name())
		}
		// skills and exclude select from the repository; M1 is checked on
		// the selection, before host.skip, so the manifest is valid or not
		// the same way on every host.
		selected, err := o.Select(found)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", e.show(e.manifestPath), err)
		}
		for _, name := range selected {
			if err := manifest.ValidName(name); err != nil {
				e.warnf("skipping %s: %v", e.show(filepath.Join(dir, name)), err)
				continue
			}
			own[i] = append(own[i], name)
		}
		for _, name := range found {
			if !o.Selects(name) {
				e.unselected[name] = fmt.Sprintf("not selected by own %s (skills/exclude)", o.Repo)
			}
		}
	}
	refs, err := e.m.CheckNames(own)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", e.show(e.manifestPath), err)
	}
	skip := e.skipped()
	var out []Skill
	for _, r := range refs {
		// A name that another own repository or a vendor entry installs is
		// not "unselected".
		delete(e.unselected, r.Name)
	}
	for _, r := range refs {
		if host, ok := skip[r.Name]; ok {
			e.unselected[r.Name] = fmt.Sprintf("skipped on this host (host.%q.skip)", host)
			continue
		}
		s := Skill{Name: r.Name, Vendor: r.Vendor}
		if r.Own != nil {
			s.OwnDir = filepath.Join(e.ownPath(r.Own), filepath.FromSlash(r.Own.SkillsDir), r.Name)
		}
		out = append(out, s)
	}
	return out, nil
}

// storePath is store/<name>.
func (e *Engine) storePath(name string) string { return filepath.Join(e.store, name) }

// linkDest is the relative symlink destination from target/<name> to the
// store entry.
func (e *Engine) linkDest(target, name string) string {
	rel, err := filepath.Rel(target, e.storePath(name))
	if err != nil {
		return e.storePath(name)
	}
	return rel
}

// desired maps each path skenv should manage to its kind.
func (e *Engine) desired(skills []Skill) map[string]state.Entry {
	out := map[string]state.Entry{}
	for _, s := range skills {
		kind := state.Link
		if s.Vendor != nil {
			kind = state.VendorDir
		}
		out[e.storePath(s.Name)] = state.Entry{Kind: kind, Skill: s.Name}
		for _, t := range e.targets {
			out[filepath.Join(t, s.Name)] = state.Entry{Kind: state.Link, Skill: s.Name}
		}
	}
	return out
}

// Output helpers. Changes go to stdout (suppressed by --quiet); warnings and
// errors go to stderr.

func (e *Engine) show(p string) string { return paths.Collapse(e.env.Home, p) }

func (e *Engine) changef(format string, args ...any) {
	e.changes++
	if e.opts.Quiet {
		return
	}
	prefix := ""
	if e.opts.DryRun {
		prefix = "would "
	}
	fmt.Fprintf(e.env.Stdout, "%s%s\n", prefix, gitx.Mask(fmt.Sprintf(format, args...)))
}

func (e *Engine) infof(format string, args ...any) {
	if e.opts.Quiet {
		return
	}
	fmt.Fprintf(e.env.Stdout, "%s\n", gitx.Mask(fmt.Sprintf(format, args...)))
}

func (e *Engine) warnf(format string, args ...any) {
	e.warnings++
	fmt.Fprintf(e.env.Stderr, "warning: %s\n", gitx.Mask(fmt.Sprintf(format, args...)))
}

func (e *Engine) errorf(format string, args ...any) {
	e.errs++
	fmt.Fprintf(e.env.Stderr, "error: %s\n", gitx.Mask(fmt.Sprintf(format, args...)))
}

func (e *Engine) saveState() error {
	if e.opts.DryRun || !e.stateDirty {
		return nil
	}
	return e.st.Save(e.layout.StateFile())
}

func (e *Engine) manage(p string, entry state.Entry) {
	if cur, ok := e.st.Managed[p]; ok && cur == entry {
		return
	}
	e.st.Managed[p] = entry
	e.stateDirty = true
}

func (e *Engine) unmanage(p string) {
	if _, ok := e.st.Managed[p]; ok {
		delete(e.st.Managed, p)
		e.stateDirty = true
	}
}

// finish saves state and prints the summary; it returns the exit code.
func (e *Engine) finish(cmd string) (int, error) {
	if err := e.saveState(); err != nil {
		return ExitFatal, fmt.Errorf("save state %s: %w", e.show(e.layout.StateFile()), err)
	}
	if !e.opts.Quiet {
		switch {
		case e.changes == 0 && e.warnings == 0 && e.errs == 0:
			fmt.Fprintf(e.env.Stdout, "%s: up to date\n", cmd)
		default:
			verb := "changes"
			if e.opts.DryRun {
				verb = "planned changes"
			}
			fmt.Fprintf(e.env.Stdout, "%s: %d %s, %d warnings, %d errors\n", cmd, e.changes, verb, e.warnings, e.errs)
		}
	}
	if e.errs > 0 {
		return ExitProblems, nil
	}
	return ExitOK, nil
}

// OwnDir is an own repository of the manifest resolved on this machine.
type OwnDir struct {
	Repo      string
	Path      string // expanded working copy path
	SkillsDir string
	Own       manifest.Own
}

// OwnDirs lists the own repositories of the manifest.
func (e *Engine) OwnDirs() []OwnDir {
	out := make([]OwnDir, 0, len(e.m.Own))
	for i := range e.m.Own {
		o := &e.m.Own[i]
		out = append(out, OwnDir{Repo: o.Repo, Path: e.ownPath(o), SkillsDir: o.SkillsDir, Own: *o})
	}
	return out
}

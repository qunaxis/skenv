// Package engine implements the skenv commands: sync, link, doctor, vendor
// and init.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/qunaxis/skenv/internal/agents"
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
}

// ErrNoManifest means no manifest location is configured. skenv does not
// guess one: the skills repository can live anywhere and have any name.
var ErrNoManifest = errors.New("no manifest configured: run `skenv init <owner/repo>` to clone your skills repository " +
	"and record its env.toml, or pass --manifest FILE (or set $SKENV_MANIFEST)")

// ResolveManifest picks the manifest path: --manifest, $SKENV_MANIFEST,
// then `manifest` in ~/.config/skenv/config.toml. Without any of them it
// returns ErrNoManifest.
func ResolveManifest(env Env, flag string) (string, error) {
	if flag != "" {
		return paths.Expand(env.Home, flag), nil
	}
	if v := env.Getenv("SKENV_MANIFEST"); v != "" {
		return paths.Expand(env.Home, v), nil
	}
	layout := paths.Layout{Home: env.Home}
	cfg, err := readConfig(layout.ConfigFile())
	if err != nil {
		return "", err
	}
	if m, ok := cfg["manifest"].(string); ok && m != "" {
		return paths.Expand(env.Home, m), nil
	}
	return "", ErrNoManifest
}

func readConfig(file string) (map[string]any, error) {
	cfg := map[string]any{}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", file, err)
	}
	return cfg, nil
}

// writeConfig sets the manifest key in config.toml, keeping other keys.
func writeConfig(file, manifestPath string) error {
	cfg, err := readConfig(file)
	if err != nil {
		return err
	}
	cfg["manifest"] = manifestPath
	var b strings.Builder
	b.WriteString("# skenv configuration, written by `skenv init`\n")
	if err := toml.NewEncoder(&b).Encode(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return manifest.WriteFile(file, []byte(b.String()))
}

// Open loads the manifest and state.
func Open(ctx context.Context, env Env, opts Options) (*Engine, error) {
	if err := gitx.Available(); err != nil {
		return nil, err
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	mp, err := ResolveManifest(env, opts.Manifest)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(mp)
	if err != nil {
		return nil, err
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

func (e *Engine) skipped() map[string]bool {
	skip := e.m.Skipped(e.env.Hostname)
	if short, _, ok := strings.Cut(e.env.Hostname, "."); ok {
		for k := range e.m.Skipped(short) {
			skip[k] = true
		}
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
	for i := range e.m.Own {
		o := &e.m.Own[i]
		dir := filepath.Join(e.ownPath(o), filepath.FromSlash(o.SkillsDir))
		entries, err := os.ReadDir(dir)
		if err != nil {
			e.ownUnavailable = true
			continue
		}
		for _, de := range entries {
			if strings.HasPrefix(de.Name(), ".") || !de.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, de.Name(), "SKILL.md")); err != nil {
				continue
			}
			if err := manifest.ValidName(de.Name()); err != nil {
				e.warnf("skipping %s: %v", e.show(filepath.Join(dir, de.Name())), err)
				continue
			}
			own[i] = append(own[i], de.Name())
		}
	}
	refs, err := e.m.CheckNames(own)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", e.show(e.manifestPath), err)
	}
	skip := e.skipped()
	var out []Skill
	for _, r := range refs {
		if skip[r.Name] {
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
}

// OwnDirs lists the own repositories of the manifest.
func (e *Engine) OwnDirs() []OwnDir {
	out := make([]OwnDir, 0, len(e.m.Own))
	for i := range e.m.Own {
		o := &e.m.Own[i]
		out = append(out, OwnDir{Repo: o.Repo, Path: e.ownPath(o), SkillsDir: o.SkillsDir})
	}
	return out
}

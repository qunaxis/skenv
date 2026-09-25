// Package engine implements the skenv commands: sync, link, doctor, list,
// vendor, init, clone, use and import.
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

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
	"github.com/qunaxis/skenv/internal/skenvfile"
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
	// Keep names skills whose installed copies sync leaves as they are,
	// even under --adopt: the import --sync of skills pinned without a
	// matching commit.
	Keep []string
}

// Exit codes shared by all commands.
const (
	ExitOK       = 0
	ExitProblems = 1 // discrepancies (doctor) or conflicts/errors in the report
	ExitFatal    = 2 // the command could not run
)

// base is what every command shares: the outside world, the flags, the
// output with its counters and the vendor cache.
type base struct {
	env       Env
	opts      Options
	layout    paths.Layout
	ctx       context.Context
	lockFile  *os.File
	backupDir string
	// hosts resolve the repo values of the skenv file: the declared hosts
	// of the manifest, or of [project].
	hosts manifest.Hosts
	// pending is the edited skenv file under --dry-run, which is never
	// written: the next edit of the same command builds on it.
	pending []byte

	// fetched maps a repository to its clone cache, fetched once per
	// import.
	fetched map[string]string

	changes  int
	warnings int
	errs     int
}

func newBase(ctx context.Context, env Env, opts Options) base {
	if env.Now == nil {
		env.Now = time.Now
	}
	return base{env: env, opts: opts, layout: paths.Layout{Home: env.Home}, ctx: ctx}
}

// readSkenvFile returns the skenv file at file as the command sees it: with
// the edits it made so far under --dry-run.
func (e *base) readSkenvFile(file string) ([]byte, error) {
	if e.pending != nil {
		return e.pending, nil
	}
	return os.ReadFile(file)
}

// writeSkenvFile writes the edited skenv file, or keeps it in memory
// under --dry-run.
func (e *base) writeSkenvFile(file string, data []byte) error {
	if e.opts.DryRun {
		e.pending = data
		return nil
	}
	return manifest.WriteFile(file, data)
}

// Engine is one command invocation over a loaded manifest and state.
type Engine struct {
	base
	manifestPath string
	m            *manifest.Manifest
	store        string
	targets      []string
	st           *state.State
	stateDirty   bool

	// ownUnavailable is set when an own repository could not be listed;
	// pruning is skipped then so its links are not mistaken for stale ones.
	ownUnavailable bool
	// unselected says why a skill that exists is not installed here: not
	// selected by skills/exclude of its own repository, or skipped on this
	// host. Set by skills.
	unselected map[string]string
}

// ErrNoManifest means no manifest location is configured. skenv does not
// guess one: the skills repository can live anywhere and have any name.
var ErrNoManifest = errors.New("no manifest configured: start one with `skenv init` in a git repository, " +
	"connect an existing one with `skenv clone <repo>` or, for a checkout you already have, `skenv use <path>`; " +
	"or pass --manifest FILE (or set $SKENV_MANIFEST)")

// ResolveManifest picks the manifest, the skenv file with [environment]:
// --manifest, $SKENV_MANIFEST, then `manifest` in the config file
// (~/.config/skenv/config.{toml,yaml,yml,json}). Each may name the file or
// the directory that holds it. Without any of them it returns ErrNoManifest.
func ResolveManifest(ctx context.Context, env Env, flag string) (string, error) {
	m, _, err := config.Resolve(env.Home, env.Getenv, "manifest", flag, "")
	if err != nil {
		return "", err
	}
	if m == "" {
		return "", noManifest(ctx, env)
	}
	return manifest.Locate(paths.Expand(env.Home, m))
}

// noManifest is ErrNoManifest, pointing at `skenv use .` when the git
// repository of the current directory holds a manifest: the likely case
// of a checkout that was never recorded.
func noManifest(ctx context.Context, env Env) error {
	cwd, err := os.Getwd()
	if err != nil {
		return ErrNoManifest
	}
	root, err := env.Git.Run(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return ErrNoManifest
	}
	file, err := skenvfile.Find(root)
	if err != nil || file == "" {
		return ErrNoManifest
	}
	if doc, err := skenvfile.Read(file); err != nil || !doc.Has(skenvfile.Environment) {
		return ErrNoManifest
	}
	return fmt.Errorf("%w\nthis repository has a manifest (%s): run `skenv use .` to use it on this machine", ErrNoManifest, filepath.Base(file))
}

// Open loads the manifest and state.
func Open(ctx context.Context, env Env, opts Options) (*Engine, error) {
	if err := gitx.Available(); err != nil {
		return nil, err
	}
	mp, err := ResolveManifest(ctx, env, opts.Manifest)
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
	e := &Engine{base: newBase(ctx, env, opts), manifestPath: mp}
	if !opts.ReadOnly && !opts.DryRun {
		if err := e.lock(); err != nil {
			return nil, err
		}
	}
	// Load the state under the lock so a concurrent run cannot be lost.
	st, err := state.Load(e.layout.StateFile())
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
	e.hosts = m.Hosts
	e.store = e.layout.DefaultStore()
	if m.Layout.Store != "" {
		e.store = paths.Expand(e.env.Home, m.Layout.Store)
	}
	e.targets = agents.Targets(e.env.Home, e.env.Getenv, m.Layout.Targets, e.store)
}

// Skill is a manifest skill resolved against the file system: Own and
// OwnDir for an own skill, Vendor for a vendored one.
type Skill struct {
	Name   string
	Own    *manifest.Own
	OwnDir string // <own.path>/<skills_dir>/<name>
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

// ownSkillsDir is the expanded <own.path>/<skills_dir> of o.
func (e *Engine) ownSkillsDir(o *manifest.Own) string {
	return filepath.Join(e.ownPath(o), filepath.FromSlash(o.SkillsDir))
}

// ownFound lists the skills in dir, the skills directory of an own
// repository: its subdirectories with a SKILL.md. It fails when dir cannot
// be read, as before the repository is cloned.
func ownFound(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
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
	return found, nil
}

// ownSkills lists the skills of the own repository o. A working copy
// without its skills directory has no skills yet; only a working copy that
// is missing (not cloned yet) or unreadable is an error.
func (e *Engine) ownSkills(o *manifest.Own) ([]string, error) {
	found, err := ownFound(e.ownSkillsDir(o))
	if errors.Is(err, fs.ErrNotExist) {
		if fi, serr := os.Stat(e.ownPath(o)); serr == nil && fi.IsDir() {
			return nil, nil
		}
	}
	return found, err
}

// skills lists every skill of the manifest that applies to this host.
// Own repositories that are not cloned yet contribute no skills.
func (e *Engine) skills() ([]Skill, error) {
	own := map[int][]string{}
	e.ownUnavailable = false
	e.unselected = map[string]string{}
	for i := range e.m.Own {
		o := &e.m.Own[i]
		dir := e.ownSkillsDir(o)
		found, err := e.ownSkills(o)
		if err != nil {
			e.ownUnavailable = true
			continue
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
		s := Skill{Name: r.Name, Vendor: r.Vendor, Own: r.Own}
		if r.Own != nil {
			s.OwnDir = filepath.Join(e.ownSkillsDir(r.Own), r.Name)
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

func (e *base) show(p string) string { return paths.Collapse(e.env.Home, p) }

func (e *base) changef(format string, args ...any) {
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

func (e *base) infof(format string, args ...any) {
	if e.opts.Quiet {
		return
	}
	fmt.Fprintf(e.env.Stdout, "%s\n", gitx.Mask(fmt.Sprintf(format, args...)))
}

func (e *base) warnf(format string, args ...any) {
	e.warnings++
	fmt.Fprintf(e.env.Stderr, "warning: %s\n", gitx.Mask(fmt.Sprintf(format, args...)))
}

func (e *base) errorf(format string, args ...any) {
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
	return e.summary(cmd), nil
}

// summary prints the one-line result of cmd and returns its exit code.
func (e *base) summary(cmd string) int {
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
		return ExitProblems
	}
	return ExitOK
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

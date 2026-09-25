package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/agents"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// Import runs `skenv import`: it adds the skills installed on this machine
// that the manifest does not have yet, vendored ones from the global lock
// of the `skills` CLI and own ones from links into git working copies, and
// removes the imported entries from that lock. A manifest file without
// [environment] gets one first. With sync, `skenv sync --adopt` follows.
func Import(ctx context.Context, env Env, opts Options, sync bool) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	mp, err := ResolveManifest(env, opts.Manifest)
	if errors.Is(err, ErrNoManifest) {
		return ExitFatal, errors.New("no manifest configured: run `skenv init --import` in your skills repository to start one " +
			"and import into it, or pass --manifest FILE (or set $SKENV_MANIFEST)")
	}
	if err != nil {
		return ExitFatal, err
	}
	data, err := os.ReadFile(mp)
	if err != nil {
		return ExitFatal, fmt.Errorf("read manifest %s: %w", mp, err)
	}
	ext := filepath.Ext(mp)
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return ExitFatal, fmt.Errorf("manifest %s: %w", mp, err)
	}
	start, fresh := data, !doc.Has(skenvfile.Environment)
	if fresh {
		// `skenv init` semantics: the same skeleton and the same refusal.
		if err := refusePublic(data, ext, mp); err != nil {
			return ExitFatal, err
		}
		if start, err = manifest.AddEnvironment(data, ext, nil); err != nil {
			return ExitFatal, fmt.Errorf("manifest %s: %w", mp, err)
		}
	}
	m, err := manifest.Parse(start, ext)
	if err != nil {
		return ExitFatal, fmt.Errorf("manifest %s: %w", mp, err)
	}
	e, err := open(ctx, env, opts, mp, m)
	if err != nil {
		return ExitFatal, err
	}
	defer e.Close()
	r, err := e.importUser(data, start, fresh, nil)
	if err != nil {
		return ExitFatal, err
	}
	if r.changed() && !opts.DryRun {
		if fresh {
			e.infof("add [environment] to %s", e.show(mp))
		}
		if err := manifest.WriteFile(mp, r.out); err != nil {
			return ExitFatal, fmt.Errorf("write manifest %s: %w", e.show(mp), err)
		}
	}
	code, err := e.finishImport(r, sync)
	if err != nil || code != ExitOK || !sync || opts.DryRun {
		return code, err
	}
	e.Close()
	return syncAdopt(ctx, env, mp)
}

// InitImport runs `skenv init --import`: start a manifest in the git
// repository of dir, import into it and run `skenv sync --adopt`. The
// repository itself becomes an own repository (from origin, or remote when
// it has none) unless the import added it already.
func InitImport(ctx context.Context, env Env, dir, format, remote string, dryRun bool) (int, error) {
	p, err := planManifest(ctx, env, dir, format, remote)
	if err != nil {
		return ExitFatal, err
	}
	ext := filepath.Ext(p.file)
	start, err := manifest.AddEnvironment(p.data, ext, nil)
	if err != nil {
		return ExitFatal, fmt.Errorf("%s: %w", p.show(p.file), err)
	}
	m, err := manifest.Parse(start, ext)
	if err != nil {
		return ExitFatal, err
	}
	e, err := open(ctx, env, Options{DryRun: dryRun, Manifest: p.file}, p.file, m)
	if err != nil {
		return ExitFatal, err
	}
	defer e.Close()
	if dryRun {
		p.printPlan(env, nil)
	}
	r, err := e.importUser(p.data, start, true, p.own)
	if err != nil {
		return ExitFatal, err
	}
	if !dryRun {
		if err := p.write(env, r.out, nil); err != nil {
			return ExitFatal, err
		}
	}
	code, err := e.finishImport(r, true)
	if err != nil || code != ExitOK || dryRun {
		return code, err
	}
	e.Close()
	return syncAdopt(ctx, env, p.file)
}

// syncAdopt runs `skenv sync --adopt` on the manifest mp.
func syncAdopt(ctx context.Context, env Env, mp string) (int, error) {
	e, err := Open(ctx, env, Options{Manifest: mp, Adopt: true})
	if err != nil {
		return ExitFatal, err
	}
	defer e.Close()
	return e.Sync()
}

// imported is the outcome of importUser.
type imported struct {
	before, out []byte
	entries     int
	unmanaged   int
	lock        *skillsLock
	unlock      []string // lock entries to remove
}

func (r *imported) changed() bool { return string(r.before) != string(r.out) }

// found is a skill installed on the machine.
type found struct {
	name     string
	path     string // where it was found first
	resolved string // the directory it resolves to
	link     bool   // path is a symlink
}

// ownGroup is the skills linked from one skills directory of a working
// copy.
type ownGroup struct {
	own   manifest.Own
	top   string // the working copy
	names []string
}

// importUser plans the import into the manifest text start (before is the
// file as it is on disk; fresh when [environment] was just added), prints
// the report and the manifest diff, and returns the new text and the lock
// entries to remove. extraOwn is added as an own repository unless the
// import adds its working copy already.
func (e *Engine) importUser(before, start []byte, fresh bool, extraOwn *manifest.Own) (*imported, error) {
	r := &imported{before: before, out: start}
	e.fetched = map[string]string{}
	lock, err := readSkillsLock(e.skillsLockPath())
	if err != nil {
		return nil, err
	}
	r.lock = lock
	skills, err := e.skills()
	if err != nil {
		return nil, err
	}
	inManifest := map[string]bool{}
	for _, s := range skills {
		inManifest[s.Name] = true
	}
	// Unmanaged paths are reported after the imports.
	var report []string
	unmanaged := func(p, why string) {
		r.unmanaged++
		report = append(report, fmt.Sprintf("unmanaged %s: %s", e.show(p), why))
	}

	// Everything installed in the store and the agent directories, once per
	// name.
	var list []found
	seen := map[string]found{}
	plugins := e.claudePlugins()
	for _, dir := range append([]string{e.store}, e.targets...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, de := range entries {
			name := de.Name()
			p := filepath.Join(dir, name)
			if strings.HasPrefix(name, ".") || e.isClaudeSynced(p) || e.m.Layout.Ignored(name) || e.owned(p) || inManifest[name] {
				continue
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				unmanaged(p, "a broken link; remove it")
				continue
			}
			if plugins != "" && strings.HasPrefix(resolved, plugins+string(filepath.Separator)) {
				continue // a Claude Code plugin's
			}
			if why, ok := e.unselected[name]; ok {
				unmanaged(p, why+"; change the manifest to install it")
				continue
			}
			if prev, ok := seen[name]; ok {
				if prev.resolved != resolved {
					unmanaged(p, fmt.Sprintf("%s is also installed from %s; keep one", name, e.show(prev.resolved)))
				}
				continue
			}
			fi, err := os.Lstat(p)
			if err != nil {
				continue
			}
			f := found{name: name, path: p, resolved: resolved, link: fi.Mode()&fs.ModeSymlink != 0}
			seen[name] = f
			list = append(list, f)
		}
	}

	groups := map[string]*ownGroup{}
	vendors := map[string]found{}
	for _, f := range list {
		if g, why := e.ownOf(f); why != "" {
			unmanaged(f.path, why)
			continue
		} else if g != nil {
			key := g.top + "\x00" + g.own.SkillsDir
			if groups[key] == nil {
				groups[key] = g
			}
			groups[key].names = append(groups[key].names, f.name)
			continue
		}
		if le, ok := lock.entries[f.name]; ok {
			if _, err := lockRepo(le); err != nil {
				unmanaged(f.path, fmt.Sprintf("in %s, but %v", e.show(lock.path), err))
				continue
			}
			vendors[f.name] = f
			continue
		}
		unmanaged(f.path, fmt.Sprintf("not in %s and not a link into a git working copy; move it into an own repository, "+
			"or add %q to layout.ignore", e.show(lock.path), f.name))
	}

	var own []ownGroup
	for _, k := range sortedKeys(groups) {
		g := groups[k]
		all := skillDirs(filepath.Join(g.top, filepath.FromSlash(g.own.SkillsDir)))
		slices.Sort(g.names)
		if len(g.names) < len(all) {
			g.own.Skills = g.names
		}
		own = append(own, *g)
	}
	for _, g := range own {
		detail := "every skill"
		if g.own.Skills != nil {
			detail = "skills " + strings.Join(g.own.Skills, ", ")
		}
		e.changef("import own %s at %s (%s)", g.own.Repo, g.own.Path, detail)
	}
	var vend []manifest.Vendor
	for _, name := range sortedKeys(vendors) {
		v, ok := e.lockVendor(name, lock.entries[name], vendors[name].resolved)
		if !ok {
			continue
		}
		vend = append(vend, v)
	}
	for _, name := range sortedKeys(lock.entries) {
		if _, ok := seen[name]; !ok && !inManifest[name] {
			e.infof("skip %s of %s: not installed in %s or an agent directory", name, e.show(lock.path), e.show(e.store))
		}
	}
	for _, line := range report {
		e.infof("%s", line)
	}

	// The manifest text, validated with the own repositories listed.
	ext := filepath.Ext(e.manifestPath)
	out := start
	for _, g := range own {
		var err error
		if out, err = manifest.AppendOwn(out, ext, g.own); err != nil {
			return nil, err
		}
		r.entries++
	}
	for _, v := range vend {
		var err error
		if out, err = manifest.AppendVendor(out, ext, skenvfile.Environment, v); err != nil {
			return nil, err
		}
		r.entries++
	}
	if extraOwn != nil && !slices.ContainsFunc(own, func(g ownGroup) bool { return g.own.Path == extraOwn.Path }) {
		withExtra, err := manifest.AppendOwn(out, ext, *extraOwn)
		if err == nil {
			err = e.checkManifest(withExtra)
		}
		if err != nil {
			e.warnf("not adding %s as an own repository: %v", extraOwn.Repo, err)
		} else {
			out = withExtra
			e.changef("add own %s at %s (the repository of the manifest)", extraOwn.Repo, extraOwn.Path)
		}
	}
	if string(out) != string(start) || fresh {
		var err error
		if out, err = skenvfile.Stamp(out, ext, fresh); err != nil {
			return nil, err
		}
	}
	if err := e.checkManifest(out); err != nil {
		return nil, fmt.Errorf("the imported manifest is invalid, nothing was written: %w", err)
	}
	r.out = out

	// Lock entries of every skill the manifest now has.
	skills, err = e.skills()
	if err != nil {
		return nil, err
	}
	for _, s := range skills {
		if _, ok := lock.entries[s.Name]; ok {
			r.unlock = append(r.unlock, s.Name)
		}
	}
	slices.Sort(r.unlock)
	if d := lineDiff(e.show(e.manifestPath), before, out); d != "" {
		e.infof("%s", strings.TrimSuffix(d, "\n"))
	}
	return r, nil
}

// checkManifest parses the manifest text and makes it the engine's
// manifest when its skills, own repositories listed, are valid.
func (e *Engine) checkManifest(data []byte) error {
	m, err := manifest.Parse(data, filepath.Ext(e.manifestPath))
	if err != nil {
		return err
	}
	prev := e.m
	e.setManifest(m)
	if _, err := e.skills(); err != nil {
		e.setManifest(prev)
		return err
	}
	return nil
}

// claudePlugins is the resolved plugins directory of Claude Code, whose
// skills belong to their plugins; "" when it does not exist.
func (e *Engine) claudePlugins() string {
	for _, a := range agents.Table(e.env.Home, e.env.Getenv) {
		if a.Name == "claude" {
			if p, err := filepath.EvalSymlinks(filepath.Join(a.Base, "plugins")); err == nil {
				return p
			}
		}
	}
	return ""
}

// ownOf returns the own repository of an installed skill that links into a
// git working copy: nil when it does not, why when such a skill cannot be
// imported.
func (e *Engine) ownOf(f found) (g *ownGroup, why string) {
	if !f.link {
		return nil, ""
	}
	fi, err := os.Stat(f.resolved)
	if err != nil || !fi.IsDir() {
		return nil, ""
	}
	top, err := e.env.Git.Run(e.ctx, f.resolved, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ""
	}
	if top, err = filepath.EvalSymlinks(top); err != nil {
		return nil, ""
	}
	where := "a link into the working copy " + e.show(top)
	rel, err := filepath.Rel(top, f.resolved)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, where + ", at its root; skenv links skills from a directory of the repository (skills_dir)"
	}
	if filepath.Base(f.resolved) != f.name {
		return nil, fmt.Sprintf("%s under another name (%s); skenv links a skill under its directory name", where, filepath.Base(f.resolved))
	}
	if !fileExists(filepath.Join(f.resolved, "SKILL.md")) {
		return nil, where + " without SKILL.md"
	}
	if err := manifest.ValidName(f.name); err != nil {
		return nil, fmt.Sprintf("%s: %v", where, err)
	}
	remote, err := e.env.Git.Run(e.ctx, top, "config", "--get", "remote.origin.url")
	if err != nil || remote == "" {
		return nil, where + " without an origin remote; push it somewhere, then import again"
	}
	repo, ok := e.m.Hosts.ShortForm(remote)
	if !ok {
		if hasCredentials(remote) {
			return nil, where + " whose origin URL carries credentials; add the own repository by hand"
		}
		repo = remote
	}
	for i := range e.m.Own {
		if p, err := filepath.EvalSymlinks(e.ownPath(&e.m.Own[i])); err == nil && p == top {
			return nil, fmt.Sprintf("%s, already own %s in the manifest; add %q to its skills or skills_dir", where, e.m.Own[i].Repo, f.name)
		}
	}
	return &ownGroup{
		own: manifest.Own{Repo: repo, Path: homeShow(e.env.Home)(top), SkillsDir: filepath.ToSlash(filepath.Dir(rel))},
		top: top,
	}, ""
}

// lockVendor turns a lock entry into a vendor entry, reporting how its rev
// was found; ok is false when it cannot be imported.
func (e *Engine) lockVendor(name string, le lockEntry, dir string) (manifest.Vendor, bool) {
	fail := func(format string, args ...any) (manifest.Vendor, bool) {
		e.errorf("cannot import %s: %s", name, fmt.Sprintf(format, args...))
		return manifest.Vendor{}, false
	}
	if err := manifest.ValidName(name); err != nil {
		return fail("%v", err)
	}
	repo, err := lockRepo(le)
	if err != nil {
		return fail("%v", err)
	}
	// One fetch per repository, however many of its skills are installed.
	cache, ok := e.fetched[repo]
	if !ok {
		var err error
		if cache, err = e.ensureCache(repo, ""); err != nil {
			return fail("%v", err)
		}
		e.fetched[repo] = cache
	}
	rev, how, tip, err := e.lockRev(cache, repo, le, dir)
	if err != nil {
		return fail("%v", err)
	}
	folder := lockFolder(le.SkillPath)
	v := manifest.Vendor{Name: name, Repo: repo, Path: vendorPath(folder), Rev: rev}
	file := "SKILL.md"
	if folder != "" {
		file = folder + "/SKILL.md"
	}
	if !e.env.Git.OK(e.ctx, cache, "cat-file", "-e", rev+":"+file) {
		return fail("no %s in %s at %.12s", file, repo, rev)
	}
	e.changef("import vendor %s from %s (%s) at %.12s", name, repo, v.Path, rev)
	switch how {
	case revByHash:
		e.infof("  its skillFolderHash %.12s is the tree of %s at that commit", le.SkillFolderHash, v.Path)
	case revByCopy:
		why := fmt.Sprintf("skillFolderHash %.12s is not in the history of %s", le.SkillFolderHash, tip)
		if le.SourceType != "github" {
			why = fmt.Sprintf("the skills CLI records no git tree for %s sources", le.SourceType)
		}
		e.warnf("vendor %s: %s; pinned %.12s, whose files match the installed copy", name, why, rev)
	case revByHead:
		e.warnf("vendor %s: neither skillFolderHash %.12s nor the installed copy matches a commit of %s; pinned its HEAD %.12s, "+
			"check the skill before syncing", name, le.SkillFolderHash, tip, rev)
	}
	return v, true
}

// finishImport cleans the lock, prints the summary and the next step, and
// returns the exit code. sync says `skenv sync --adopt` follows.
func (e *Engine) finishImport(r *imported, sync bool) (int, error) {
	if len(r.unlock) > 0 {
		if err := e.cleanLock(r); err != nil {
			return ExitFatal, err
		}
	}
	switch {
	case r.entries == 0 && len(r.unlock) == 0 && !r.changed():
		fmt.Fprintf(e.env.Stdout, "import: nothing to import")
	default:
		verb := ""
		if e.opts.DryRun {
			verb = "planned: "
		}
		entries := "entries"
		if r.entries == 1 {
			entries = "entry"
		}
		fmt.Fprintf(e.env.Stdout, "import: %s%d manifest %s, %d removed from the skills lock", verb, r.entries, entries, len(r.unlock))
	}
	fmt.Fprintf(e.env.Stdout, ", %d unmanaged, %d warnings, %d errors\n", r.unmanaged, e.warnings, e.errs)
	if r.changed() || len(r.unlock) > 0 {
		switch {
		case e.opts.DryRun && sync:
			e.infof("would run skenv sync --adopt")
		case sync && !e.opts.DryRun && e.errs > 0:
			e.infof("not running skenv sync --adopt: fix the errors above, then run it")
		case !sync && !e.opts.DryRun:
			e.infof("next: `skenv sync --adopt` replaces the copies and links found with managed ones (the old ones go to %s)",
				e.show(e.layout.Backup()))
		}
		if r.changed() {
			e.commitHint("import installed skills")
		}
	}
	if e.errs > 0 {
		return ExitProblems, nil
	}
	return ExitOK, nil
}

// cleanLock removes the imported entries from the lock of the `skills`
// CLI, so `npx skills update` does not fight skenv over them, after a copy
// to the backup directory.
func (e *Engine) cleanLock(r *imported) error {
	backup := e.backupPath(r.lock.path)
	e.changef("remove %s from %s (a copy goes to %s)", strings.Join(r.unlock, ", "), e.show(r.lock.path), e.show(backup))
	if e.opts.DryRun {
		return nil
	}
	data, err := os.ReadFile(r.lock.path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(backup, data, r.lock.mode); err != nil {
		return fmt.Errorf("back up %s: %w", e.show(r.lock.path), err)
	}
	if err := r.lock.without(r.unlock); err != nil {
		return fmt.Errorf("write %s: %w", e.show(r.lock.path), err)
	}
	return nil
}

// backupPath is where p goes in the backup directory of this run:
// backup/<ts>/<path relative to home>.
func (e *base) backupPath(p string) string {
	if e.backupDir == "" {
		e.backupDir = filepath.Join(e.layout.Backup(), e.env.Now().UTC().Format("20060102T150405Z"))
	}
	rel, err := filepath.Rel(e.env.Home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = strings.TrimPrefix(p, string(filepath.Separator))
	}
	return filepath.Join(e.backupDir, rel)
}

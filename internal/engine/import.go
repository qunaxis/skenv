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
// [user] gets one first. With sync, `skenv sync --adopt` follows,
// leaving the skills pinned without a matching commit as installed.
func Import(ctx context.Context, env Env, opts Options, sync bool) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	mp, err := ResolveManifest(ctx, env, opts.Manifest)
	if errors.Is(err, ErrNoManifest) {
		return ExitFatal, errors.New("no manifest configured: run `skenv init --import` in your skills repository to start one " +
			"and import into it; to import into an existing one, connect it first (`skenv clone <repo>` or `skenv use <path>`) " +
			"or pass --manifest FILE (or set $SKENV_MANIFEST)")
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
	start, fresh := data, !doc.Has(skenvfile.User)
	if fresh {
		// `skenv init` semantics: the same skeleton and the same refusal.
		if err := refusePublic(data, ext, mp); err != nil {
			return ExitFatal, err
		}
		if start, err = manifest.AddUser(data, ext, nil); err != nil {
			return ExitFatal, fmt.Errorf("manifest %s: %w", mp, err)
		}
	}
	m, err := manifest.ParseIn(start, ext, filepath.Dir(mp))
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
			e.infof("add [user] to %s", e.show(mp))
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
	return syncAdopt(ctx, env, mp, r)
}

// InitImport runs `skenv init --import`: start a manifest in the git
// repository of dir, import into it and run `skenv sync --adopt` as
// Import does with sync. The repository itself becomes a checkout (from
// origin, or remote when it has none) unless the import added it already.
func InitImport(ctx context.Context, env Env, dir, format, remote string, dryRun bool) (int, error) {
	p, err := planManifest(ctx, env, dir, format, remote)
	if err != nil {
		return ExitFatal, err
	}
	ext := filepath.Ext(p.file)
	start, err := manifest.AddUser(p.data, ext, nil)
	if err != nil {
		return ExitFatal, fmt.Errorf("%s: %w", p.show(p.file), err)
	}
	m, err := manifest.ParseIn(start, ext, filepath.Dir(p.file))
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
	return syncAdopt(ctx, env, p.file, r)
}

// syncAdopt runs `skenv sync --adopt` on the manifest mp after the import
// r, leaving the unmatched skills as installed, and reports which skills
// it took over.
func syncAdopt(ctx context.Context, env Env, mp string, r *imported) (int, error) {
	e, err := Open(ctx, env, Options{Manifest: mp, Adopt: true, Keep: r.unmatched})
	if err != nil {
		return ExitFatal, err
	}
	defer e.Close()
	code, err := e.Sync()
	if err != nil {
		return code, err
	}
	e.reportTakeover(r, e.show(mp), "", func(name string) bool { return e.owned(e.storePath(name)) })
	return code, nil
}

// The groups of imported entries: revByHash, revByCopy and revByHead for
// dependencies, then checkouts.
const byOwn = revByHead + 1

var groupNames = [...]string{revByHash: "exact", revByCopy: "same files", revByHead: "unmatched", byOwn: "checkout"}

var groupTitles = [...]string{
	revByHash: "the commit has the hash recorded in the lock",
	revByCopy: "the commit has the files of the installed copy (no commit has the hash in the lock)",
	revByHead: "no commit matched, pinned to the tip of the branch; the installed copy may differ",
	byOwn:     "repositories kept as git working copies",
}

// imported is the outcome of importUser.
type imported struct {
	before, out []byte
	entries     int
	unmanaged   int
	lock        *skillsLock
	unlock      []string // lock entries to remove
	diff        string   // of the skenv file

	managed   [byOwn + 1][]string // report lines of the entries, by group
	names     []string            // the installed skills the entries cover
	unmatched []string            // dependencies of the revByHead group
	skipped   []string            // what is not imported, and why
	failed    []string            // lock entries that could not be imported
}

// addVendor records the dependency v, found by how.
func (r *imported) addVendor(v manifest.Dependency, how int, note string) {
	line := fmt.Sprintf("%s from %s (%s) at %.12s", v.Name, v.Repo, v.SkillDir, v.Commit)
	if note != "" {
		line += ": " + note
	}
	r.managed[how] = append(r.managed[how], line)
	r.names = append(r.names, v.Name)
	if how == revByHead {
		r.unmatched = append(r.unmatched, v.Name)
	}
}

// groups counts the entries per group, "(2 exact, 1 checkout)"; "" without
// any.
func (r *imported) groups() string {
	var parts []string
	for i, g := range r.managed {
		if len(g) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", len(g), groupNames[i]))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
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
	own   manifest.Checkout
	top   string // the working copy
	names []string
}

// importUser plans the import into the manifest text start (before is the
// file as it is on disk; fresh when [user] was just added), prints the
// report and the manifest diff, and returns the new text and the lock
// entries to remove. extraOwn is added as a checkout unless the import
// adds its working copy already.
func (e *Engine) importUser(before, start []byte, fresh bool, extraOwn *manifest.Checkout) (*imported, error) {
	r := &imported{before: before, out: start}
	lock, err := readSkillsLock(e.skillsLockPath(), skillsLockVersion)
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
	unmanaged := func(p, why string) {
		r.unmanaged++
		r.skipped = append(r.skipped, fmt.Sprintf("%s: %s", e.show(p), why))
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
			if strings.HasPrefix(name, ".") || e.isClaudeSynced(p) || e.m.IsUnmanaged(name) || e.owned(p) || inManifest[name] {
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
			if _, err := lockRepo(le, e.hosts); err != nil {
				unmanaged(f.path, fmt.Sprintf("in %s, but %v", e.show(lock.path), err))
				continue
			}
			vendors[f.name] = f
			continue
		}
		unmanaged(f.path, fmt.Sprintf("not in %s and not a link into a git working copy; move it into a checkout, "+
			"or add %q to user.unmanaged", e.show(lock.path), f.name))
	}

	var own []ownGroup
	taken := map[string]bool{}
	for id := range e.m.Checkouts {
		taken[id] = true
	}
	for _, k := range sortedKeys(groups) {
		g := groups[k]
		all := skillDirs(filepath.Join(g.top, filepath.FromSlash(g.own.SkillsDir)))
		slices.Sort(g.names)
		if len(g.names) < len(all) {
			g.own.Include = g.names
		}
		g.own.ID = manifest.NewID(g.own.Repo, taken)
		taken[g.own.ID] = true
		own = append(own, *g)
	}
	var vend []manifest.Dependency
	for _, name := range sortedKeys(vendors) {
		v, how, note, err := e.lockVendor(name, lock.entries[name], vendors[name].resolved)
		if err != nil {
			r.failed = append(r.failed, fmt.Sprintf("cannot import %s: %v", name, err))
			continue
		}
		r.addVendor(v, how, note)
		vend = append(vend, v)
	}
	for _, g := range own {
		detail := "every skill"
		if g.own.Include != nil {
			detail = "include " + strings.Join(g.own.Include, ", ")
		}
		r.managed[byOwn] = append(r.managed[byOwn], fmt.Sprintf("%s: %s at %s (%s)", g.own.ID, g.own.Repo, e.show(g.top), detail))
		r.names = append(r.names, g.names...)
	}
	slices.Sort(r.names)
	for _, name := range sortedKeys(lock.entries) {
		if _, ok := seen[name]; !ok && !inManifest[name] {
			r.skipped = append(r.skipped, fmt.Sprintf("%s, in %s: not installed in %s or an agent directory; it stays in the lock",
				name, e.show(lock.path), e.show(e.store)))
		}
	}

	// The manifest text, validated with the checkouts listed.
	ext := filepath.Ext(e.manifestPath)
	out := start
	for _, g := range own {
		var err error
		if out, err = manifest.AppendCheckout(out, ext, g.own); err != nil {
			return nil, err
		}
		r.entries++
	}
	for _, v := range vend {
		var err error
		if out, err = manifest.AppendDependency(out, ext, skenvfile.User, v); err != nil {
			return nil, err
		}
		r.entries++
	}
	resolved := func(c manifest.Checkout) string { return e.m.Path(e.env.Home, c.CheckoutDir) }
	if extraOwn != nil && !slices.ContainsFunc(own, func(g ownGroup) bool { return samePath(resolved(g.own), resolved(*extraOwn)) }) {
		extra := *extraOwn
		extra.ID = manifest.NewID(extra.Repo, taken)
		withExtra, err := manifest.AppendCheckout(out, ext, extra)
		if err == nil {
			err = e.checkManifest(withExtra)
		}
		if err != nil {
			e.warnf("not adding %s as a checkout: %v", extra.Repo, err)
		} else {
			out = withExtra
			r.entries++
			r.managed[byOwn] = append(r.managed[byOwn], fmt.Sprintf("%s: %s at %s (the repository of the manifest)", extra.ID, extra.Repo, e.show(resolved(extra))))
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
	r.diff = strings.TrimSuffix(lineDiff(e.show(e.manifestPath), before, out), "\n")
	e.report(r, e.show(e.manifestPath))
	return r, nil
}

// checkManifest parses the manifest text and makes it the engine's
// manifest when its skills, checkouts listed, are valid.
func (e *Engine) checkManifest(data []byte) error {
	m, err := manifest.ParseIn(data, filepath.Ext(e.manifestPath), filepath.Dir(e.manifestPath))
	if err != nil {
		return err
	}
	prev := e.m
	if err := e.setManifest(m); err != nil {
		_ = e.setManifest(prev)
		return err
	}
	if _, err := e.skills(); err != nil {
		_ = e.setManifest(prev)
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

// ownOf returns the checkout of an installed skill that links into a git
// working copy: nil when it does not, why when such a skill cannot be
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
	repo, ok := e.m.GitHosts.ShortForm(remote)
	if !ok {
		if hasCredentials(remote) {
			return nil, where + " whose origin URL carries credentials; add the checkout by hand"
		}
		repo = remote
	}
	for _, c := range e.m.CheckoutList() {
		if p, err := filepath.EvalSymlinks(e.checkoutPath(c)); err == nil && p == top {
			return nil, fmt.Sprintf("%s, already checkout %s in the manifest; add %q to its include or skills_dir", where, c.ID, f.name)
		}
	}
	dir := homeShow(e.env.Home)(top)
	if samePath(top, filepath.Dir(e.manifestPath)) {
		dir = "." // the repository of the manifest, wherever it is cloned
	}
	return &ownGroup{
		own: manifest.Checkout{Repo: repo, CheckoutDir: dir, SkillsDir: filepath.ToSlash(filepath.Dir(rel))},
		top: top,
	}, ""
}

// lockVendor turns a lock entry into a dependency: how says how its commit
// was found, and note why it is not an exact match. dir is the installed
// copy, "" when there is none.
func (e *base) lockVendor(name string, le lockEntry, dir string) (v manifest.Dependency, how int, note string, err error) {
	if err := manifest.ValidName(name); err != nil {
		return v, 0, "", err
	}
	repo, err := lockRepo(le, e.hosts)
	if err != nil {
		return v, 0, "", err
	}
	// One fetch per repository, however many of its skills are installed.
	if e.fetched == nil {
		e.fetched = map[string]string{}
	}
	cache, ok := e.fetched[repo]
	if !ok {
		if cache, err = e.ensureCache(repo, ""); err != nil {
			return v, 0, "", err
		}
		e.fetched[repo] = cache
	}
	rev, how, tip, err := e.lockRev(cache, repo, le, dir)
	if err != nil {
		return v, 0, "", err
	}
	folder := lockFolder(le.SkillPath)
	v = manifest.Dependency{Name: name, Repo: repo, SkillDir: vendorPath(folder), Commit: rev}
	file := "SKILL.md"
	if folder != "" {
		file = folder + "/SKILL.md"
	}
	if !e.env.Git.OK(e.ctx, cache, "cat-file", "-e", rev+":"+file) {
		return v, 0, "", fmt.Errorf("no %s in %s at %.12s", file, repo, rev)
	}
	hash, field := le.hash()
	note = fmt.Sprintf("%s %.12s is not in the history of %s", field, hash, tip)
	if hash == "" {
		note = "the lock records no hash"
	}
	switch {
	case how == revByHash:
		note = ""
	case how == revByHead && dir == "":
		note += fmt.Sprintf(", and there is no installed copy to compare; HEAD of %s", tip)
	case how == revByHead:
		note += fmt.Sprintf(", and no commit has the files of the installed copy; HEAD of %s", tip)
	}
	return v, how, note, nil
}

// report prints what the import makes managed in file, grouped by how
// the entries match what is installed, and what it does not import.
func (e *base) report(r *imported, file string) {
	n := 0
	for _, g := range r.managed {
		n += len(g)
	}
	if n > 0 {
		verb := "becomes managed"
		if e.opts.DryRun {
			verb = "would become managed"
		}
		entries := "entries"
		if n == 1 {
			entries = "entry"
		}
		e.infof("%s: %d %s in %s", verb, n, entries, file)
		for i, g := range r.managed {
			if len(g) == 0 {
				continue
			}
			e.infof("  %s: %s", groupNames[i], groupTitles[i])
			for _, line := range g {
				e.infof("    %s", line)
			}
		}
	}
	if n := len(r.skipped) + len(r.failed); n > 0 {
		e.infof("not imported: %d", n)
		for _, line := range r.skipped {
			e.infof("  %s", line)
		}
		for _, line := range r.failed {
			e.errorf("%s", line)
		}
	}
}

// decide tells how to settle each unmatched skill that sync left as
// installed; flag is the --project flag of vendor remove, or "".
func (e *base) decide(names []string, flag string) {
	for _, name := range names {
		e.infof("  %s: `skenv sync --adopt` replaces it with the pinned commit (the copy goes to %s), "+
			"or `skenv vendor remove%s %s` drops the entry and leaves the copy to neither skenv nor the skills CLI "+
			"(its lock entry is in the backup of the lock)", name, e.show(e.layout.Backup()), flag, name)
	}
}

// reportTakeover prints, after the sync of the import r into file, which
// of the recorded skills sync took over (managed says whether skenv
// manages a skill now) and which it left as installed.
func (e *base) reportTakeover(r *imported, file, flag string, managed func(string) bool) {
	if len(r.names) == 0 {
		return
	}
	var took, kept, failed []string
	for _, name := range r.names {
		switch {
		case managed(name):
			took = append(took, name)
		case slices.Contains(r.unmatched, name):
			kept = append(kept, name)
		default:
			failed = append(failed, name)
		}
	}
	list := func(names []string) string {
		if len(names) == 0 {
			return "none"
		}
		return strings.Join(names, ", ")
	}
	e.infof("recorded in %s: %s", file, list(r.names))
	e.infof("taken over: %s", list(took))
	if len(kept) > 0 {
		e.infof("left as installed (unmatched): %s; decide for each:", list(kept))
		e.decide(kept, flag)
	}
	if len(failed) > 0 {
		e.infof("not taken over: %s (see the errors above)", list(failed))
	}
}

// finishImport cleans the lock, prints the diff, the summary and the next
// step, and returns the exit code. sync says `skenv sync --adopt` follows.
func (e *Engine) finishImport(r *imported, sync bool) (int, error) {
	if len(r.unlock) > 0 {
		if err := e.cleanLock(r.lock, r.unlock, e.show); err != nil {
			return ExitFatal, err
		}
	}
	if r.diff != "" {
		e.infof("%s", r.diff)
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
		fmt.Fprintf(e.env.Stdout, "import: %s%d manifest %s%s, %d removed from the skills lock", verb, r.entries, entries, r.groups(), len(r.unlock))
	}
	fmt.Fprintf(e.env.Stdout, ", %d unmanaged, %d warnings, %d errors\n", r.unmanaged, e.warnings, e.errs)
	if r.changed() || len(r.unlock) > 0 {
		e.nextAfterImport(r, sync, "")
		if r.changed() {
			e.commitHint("import installed skills")
		}
	}
	if e.errs > 0 {
		return ExitProblems, nil
	}
	return ExitOK, nil
}

// nextAfterImport prints what follows the import r: the sync of --sync
// under --dry-run, or the next step without --sync. flag is the --project
// flag of vendor remove, or "".
func (e *base) nextAfterImport(r *imported, sync bool, flag string) {
	switch {
	case sync && e.errs > 0:
		e.infof("not running skenv sync --adopt: fix the errors above, then run it")
	case sync && e.opts.DryRun && len(r.unmatched) > 0:
		e.infof("would run skenv sync --adopt, leaving the unmatched as installed: %s; then decide for each:", strings.Join(r.unmatched, ", "))
		e.decide(r.unmatched, flag)
	case sync && e.opts.DryRun:
		e.infof("would run skenv sync --adopt")
	case !sync && !e.opts.DryRun && len(r.unmatched) > 0:
		e.infof("next: `skenv sync --adopt` replaces the installed copies with managed ones, the unmatched %s included "+
			"(the old ones go to %s); compare the unmatched ones first", strings.Join(r.unmatched, ", "), e.show(e.layout.Backup()))
	case !sync && !e.opts.DryRun:
		e.infof("next: `skenv sync --adopt` replaces the installed copies with managed ones (the old ones go to %s)", e.show(e.layout.Backup()))
	}
}

// cleanLock removes the imported entries names from a lock of the
// `skills` CLI, so `npx skills update` does not fight skenv over them,
// after a copy to the backup directory. show shows the lock path.
func (e *base) cleanLock(lock *skillsLock, names []string, show func(string) string) error {
	backup := e.backupPath(lock.path)
	e.changef("remove %s from %s, so that the skills CLI no longer updates them; skenv manages each once sync takes its installed copy over (a copy of the lock goes to %s)",
		strings.Join(names, ", "), show(lock.path), e.show(backup))
	if e.opts.DryRun {
		return nil
	}
	data, err := os.ReadFile(lock.path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(backup, data, lock.mode); err != nil {
		return fmt.Errorf("back up %s: %w", show(lock.path), err)
	}
	if err := lock.without(names); err != nil {
		return fmt.Errorf("write %s: %w", show(lock.path), err)
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

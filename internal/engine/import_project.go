package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// agentDirs are the project directories of agents that the `skills` CLI
// installs into (dir first) and project-own skills are looked for in, besides dir and
// the mirrors of [project].
var agentDirs = []string{manifest.DefaultProjectDir, ".claude/skills", ".pi/skills"}

// ImportProject runs `skenv import --project` in the git repository of
// dir: it pins every skill of the project's skills-lock.json (the `skills`
// CLI) in [[project.vendor]], reports the project-own skills and the
// duplicates among the agent directories, and removes the imported
// entries from the lock. A skenv file without [project] gets one, a
// repository without a skenv file a skenv.toml. With sync, `skenv sync
// --adopt` follows in the project, leaving the skills pinned without a
// matching commit as installed.
func ImportProject(ctx context.Context, env Env, dir string, dryRun, sync bool) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return ExitFatal, fmt.Errorf("--project: %s is not inside a git repository", homeShow(env.Home)(dir))
	}
	file, err := skenvfile.Find(root)
	if err != nil {
		return ExitFatal, err
	}
	var data []byte
	if file == "" {
		file = filepath.Join(root, skenvfile.Names[0])
	} else if data, err = os.ReadFile(file); err != nil {
		return ExitFatal, err
	}
	ext := filepath.Ext(file)
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return ExitFatal, fmt.Errorf("%s: %w", homeShow(env.Home)(file), err)
	}
	start, fresh := data, !doc.Has(skenvfile.Project)
	if fresh {
		// The skills CLI links its copies into the other agent directories:
		// mirrors.
		var mirrors []string
		for _, d := range agentDirs[1:] {
			if _, err := os.Stat(filepath.Join(root, d)); err == nil {
				mirrors = append(mirrors, d)
			}
		}
		if start, err = manifest.AddProject(data, ext, mirrors); err != nil {
			return ExitFatal, fmt.Errorf("%s: %w", homeShow(env.Home)(file), err)
		}
	}
	p, err := manifest.ParseProject(start, ext)
	if err != nil {
		return ExitFatal, fmt.Errorf("%s: %w", homeShow(env.Home)(file), err)
	}
	e := &ProjectEngine{base: newBase(ctx, env, Options{DryRun: dryRun}), root: root, file: file, p: p, removed: map[string]bool{}}
	e.hosts = p.Hosts
	if err := e.checkDirs(); err != nil {
		return ExitFatal, fmt.Errorf("%s: %w", e.show(file), err)
	}
	if !dryRun {
		if err := e.lock(); err != nil {
			return ExitFatal, err
		}
	}
	defer e.Close()

	r, err := e.importLock(data, start, fresh)
	if err != nil {
		return ExitFatal, err
	}
	if r.changed() && !dryRun {
		if fresh {
			e.infof("add [project] to %s", e.show(file))
		}
		if err := manifest.WriteFile(file, r.out); err != nil {
			return ExitFatal, fmt.Errorf("write %s: %w", e.show(file), err)
		}
	}
	if len(r.unlock) > 0 {
		if err := e.cleanLock(r.lock, r.unlock, e.rel); err != nil {
			return ExitFatal, err
		}
		// A lock that is gone and was never committed has nothing to add.
		if fileExists(r.lock.path) || e.env.Git.OK(ctx, root, "ls-files", "--error-unmatch", "--", projectLockName) {
			e.hintPaths = []string{projectLockName}
		}
	}
	code := e.finishProjectImport(r, sync)
	if code != ExitOK || !sync || dryRun {
		return code, nil
	}
	e.Close()
	s, err := OpenProject(ctx, env, Options{Adopt: true, Keep: r.unmatched}, file)
	if err != nil {
		return ExitFatal, err
	}
	defer s.Close()
	s.hintPaths = e.hintPaths
	code, err = s.Sync()
	if err != nil {
		return code, err
	}
	s.reportTakeover(&r.imported, s.show(file), " --project", func(name string) bool {
		_, err := readMarker(s.abs(s.p.Dir, name))
		return err == nil
	})
	return code, nil
}

// projectImport is the outcome of importLock.
type projectImport struct {
	imported
	own        int // project-own skills found
	duplicates int // project-own skills with different content in several directories
}

// importLock plans the import of skills-lock.json into the skenv file text
// start (before is the file as it is on disk; fresh when [project] was
// just added), prints the report and the diff, and returns the new text
// and the lock entries to remove.
func (e *ProjectEngine) importLock(before, start []byte, fresh bool) (*projectImport, error) {
	r := &projectImport{imported: imported{before: before, out: start}}
	lock, err := readSkillsLock(filepath.Join(e.root, projectLockName), projectLockVersion)
	if err != nil {
		return nil, err
	}
	r.lock = lock
	inProject := map[string]bool{}
	for _, s := range e.p.Skills() {
		inProject[s.Name] = true
	}
	ext := filepath.Ext(e.file)
	out := start
	for _, name := range sortedKeys(lock.entries) {
		le := lock.entries[name]
		if inProject[name] {
			r.unlock = append(r.unlock, name)
			continue
		}
		if _, err := lockRepo(le, e.hosts); err != nil {
			r.skipped = append(r.skipped, fmt.Sprintf("%s, in %s: %v; it stays in the lock", name, projectLockName, err))
			continue
		}
		v, how, note, err := e.lockVendor(name, le, e.installedCopy(name))
		if err != nil {
			r.failed = append(r.failed, fmt.Sprintf("cannot import %s: %v", name, err))
			continue
		}
		r.addVendor(v, how, note)
		if out, err = manifest.AppendVendor(out, ext, skenvfile.Project, v); err != nil {
			return nil, err
		}
		r.entries++
		r.unlock = append(r.unlock, name)
	}
	if string(out) != string(start) || fresh {
		if out, err = skenvfile.Stamp(out, ext, fresh); err != nil {
			return nil, err
		}
	}
	p, err := manifest.ParseProject(out, ext)
	if err != nil {
		return nil, fmt.Errorf("the imported [project] is invalid, nothing was written: %w", err)
	}
	e.p = p
	r.out = out
	r.diff = strings.TrimSuffix(lineDiff(e.show(e.file), before, out), "\n")
	e.report(&r.imported, e.show(e.file))
	e.reportOwn(r, lock)
	return r, nil
}

// scanDirs are the directories import looks at: dir, the mirrors and the
// agent directories, each once.
func (e *ProjectEngine) scanDirs() []string {
	var dirs []string
	for _, d := range append(append([]string{e.p.Dir}, e.p.Mirrors...), agentDirs...) {
		if !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// installedCopy is the copy of a lock skill that the skills CLI installed,
// the first skill directory of that name among scanDirs; "" when there is
// none.
func (e *ProjectEngine) installedCopy(name string) string {
	for _, d := range e.scanDirs() {
		if p := e.abs(d, name); fileExists(filepath.Join(p, "SKILL.md")) {
			return p
		}
	}
	return ""
}

// ownCopy is a project-own skill directory found by reportOwn.
type ownCopy struct {
	dir  string // the directory of the project it was found in
	path string // dir/name
	real string // path with symlinks resolved
}

// reportOwn reports the project-own skills of the scanned directories
// (neither in the lock nor in [project], no skenv marker): kept in dir,
// only in another directory, or in several directories, with a summary
// of how their files differ so the owner picks one before sync mirrors
// dir. Nothing is changed or removed.
func (e *ProjectEngine) reportOwn(r *projectImport, lock *skillsLock) {
	managed := map[string]bool{}
	for name := range lock.entries {
		managed[name] = true
	}
	for _, s := range e.p.Skills() {
		managed[s.Name] = true
	}
	found := map[string][]ownCopy{}
	for _, d := range e.scanDirs() {
		entries, err := os.ReadDir(e.abs(d))
		if err != nil {
			continue
		}
		for _, de := range entries {
			name := de.Name()
			p := e.abs(d, name)
			if strings.HasPrefix(name, ".") || managed[name] || !fileExists(filepath.Join(p, "SKILL.md")) {
				continue
			}
			if _, err := readMarker(p); err == nil {
				continue // a copy skenv made
			}
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				continue
			}
			found[name] = append(found[name], ownCopy{dir: d, path: p, real: real})
		}
	}
	for _, name := range sortedKeys(found) {
		r.own++
		copies := distinct(found[name])
		home := e.abs(e.p.Dir, name)
		if len(copies) == 1 {
			if c := copies[0]; c.dir != e.p.Dir {
				e.warnf("project-own skill %s is only in %s: move it to %s/%s, which skenv mirrors (sync leaves it where it is, doctor reports it unmanaged)",
					name, e.rel(c.path), e.p.Dir, name)
			} else {
				e.infof("project-own skill %s: kept as it is; sync mirrors it", e.rel(home))
			}
			continue
		}
		var diffs []string
		for _, c := range copies[1:] {
			if d := e.dirDiff(copies[0].path, c.path); d != "" {
				diffs = append(diffs, fmt.Sprintf("%s and %s differ: %s", e.rel(copies[0].path), e.rel(c.path), d))
			}
		}
		where := make([]string, len(copies))
		for i, c := range copies {
			where[i] = e.rel(c.path)
		}
		inDir := copies[0].dir == e.p.Dir
		switch {
		case len(diffs) > 0:
			r.duplicates++
			e.warnf("project-own skill %s is in %s, and the copies differ (%s); pick the version to keep, put it in %s/%s "+
				"and remove the others: skenv never removes a project-own skill, and sync --adopt would replace a differing mirror with a link to %s",
				name, strings.Join(where, ", "), strings.Join(diffs, "; "), e.p.Dir, name, e.p.Dir)
		case inDir:
			e.infof("project-own skill %s is in %s with the same files; %s is the one skenv mirrors", name, strings.Join(where, ", "), e.rel(home))
		default:
			e.warnf("project-own skill %s is in %s with the same files, but not in %s: move one there, skenv mirrors it", name, strings.Join(where, ", "), e.p.Dir)
		}
	}
}

// distinct keeps one of the copies that resolve to the same directory
// (mirror links), the one in dir first.
func distinct(copies []ownCopy) []ownCopy {
	var out []ownCopy
	for _, c := range copies {
		if !slices.ContainsFunc(out, func(o ownCopy) bool { return o.real == c.real }) {
			out = append(out, c)
		}
	}
	return out
}

// dirDiff summarizes how the files of the skill directories a and b
// differ: "" when they are the same.
func (e *ProjectEngine) dirDiff(a, b string) string {
	skipGit := func(name string, dir bool) bool { return dir && name == ".git" }
	fa, errA := dirBlobs(a, skipGit)
	fb, errB := dirBlobs(b, skipGit)
	if err := errors.Join(errA, errB); err != nil {
		return err.Error()
	}
	var changed, onlyA, onlyB []string
	for _, p := range sortedKeys(fa) {
		switch id, ok := fb[p]; {
		case !ok:
			onlyA = append(onlyA, p)
		case id != fa[p]:
			changed = append(changed, p)
		}
	}
	for _, p := range sortedKeys(fb) {
		if _, ok := fa[p]; !ok {
			onlyB = append(onlyB, p)
		}
	}
	var parts []string
	if len(changed) > 0 {
		parts = append(parts, fmt.Sprintf("%s changed", fileList(changed)))
	}
	if len(onlyA) > 0 {
		parts = append(parts, fmt.Sprintf("%s only in %s", fileList(onlyA), e.rel(a)))
	}
	if len(onlyB) > 0 {
		parts = append(parts, fmt.Sprintf("%s only in %s", fileList(onlyB), e.rel(b)))
	}
	return strings.Join(parts, ", ")
}

// fileList shows a few paths and how many there are.
func fileList(paths []string) string {
	const shown = 3
	n := "1 file"
	if len(paths) != 1 {
		n = fmt.Sprintf("%d files", len(paths))
	}
	list := strings.Join(paths[:min(shown, len(paths))], ", ")
	if len(paths) > shown {
		list += ", ..."
	}
	return fmt.Sprintf("%s (%s)", n, list)
}

// finishProjectImport prints the diff, the summary and the next step and
// returns the exit code. sync says `skenv sync --adopt` follows.
func (e *ProjectEngine) finishProjectImport(r *projectImport, sync bool) int {
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
		fmt.Fprintf(e.env.Stdout, "import: %s%d [project] %s%s, %d removed from %s", verb, r.entries, entries, r.groups(), len(r.unlock), projectLockName)
	}
	fmt.Fprintf(e.env.Stdout, ", %d project-own skills, %d differing duplicates, %d warnings, %d errors\n", r.own, r.duplicates, e.warnings, e.errs)
	pending := r.changed() || len(r.unlock) > 0
	blocked := sync && (e.errs > 0 || r.duplicates > 0)
	switch {
	case sync && e.errs == 0 && r.duplicates > 0:
		e.infof("not running skenv sync --adopt: resolve the differing duplicates above, then run it")
	case sync || pending:
		e.nextAfterImport(&r.imported, sync, " --project")
	}
	if pending && (!sync || blocked) {
		e.commitHint("import project skills")
	}
	if e.errs > 0 || blocked {
		return ExitProblems
	}
	return ExitOK
}

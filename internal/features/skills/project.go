package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/model/skenvfile"
	"github.com/qunaxis/skenv/internal/platform/gitx"
)

// ProjectScope runs a command on the [project] section of a repository:
// the skills committed with a project. Nothing of it lives on the machine
// (no state.json, no store): a copy is skenv's when it carries a .skenv
// marker, and everything else in dir is the project's own and never
// changed.
type ProjectScope struct {
	scope
	root    string // the repository root
	file    string // its skenv file
	project *skenvfile.Project

	// removed are the copies sync removes (or would, under --dry-run), so
	// that mirrors follow the plan.
	removed map[string]bool
	// kept are the skills of opts.Keep that sync leaves as installed.
	kept map[string]bool
	// user are the directories of the user scope (userDirs).
	user []string
}

// FindProject returns the skenv file at the root of the git repository
// that holds dir when it has a [project] section, "" otherwise (dir is
// not in a repository, the root has no skenv file, or the file has no
// [project]).
func FindProject(ctx context.Context, env Env, dir string) (string, error) {
	if err := gitx.Available(); err != nil {
		return "", err
	}
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", nil //nolint:nilerr // not in a git repository: not a project
	}
	file, err := skenvfile.Find(root)
	if err != nil || file == "" {
		return "", err
	}
	doc, err := skenvfile.Read(file)
	if err != nil {
		return "", err
	}
	if !doc.Has(skenvfile.SectionProject) {
		return "", nil
	}
	return file, nil
}

// OpenProject loads the [project] section of file, the skenv file at the
// root of a repository. Commands that write take the skenv lock: the
// vendor cache is shared with the user-level commands.
func OpenProject(ctx context.Context, env Env, opts Options, file string) (*ProjectScope, error) {
	if err := gitx.Available(); err != nil {
		return nil, err
	}
	p, err := skenvfile.LoadProject(file)
	if err != nil {
		return nil, err
	}
	e := &ProjectScope{scope: newBase(ctx, env, opts), root: filepath.Dir(file), file: file, project: p, removed: map[string]bool{}}
	e.hosts, e.hostsDir = p.GitHosts, e.root
	if err := e.checkDirs(); err != nil {
		return nil, fmt.Errorf("%s: %w", e.displayPath(file), err)
	}
	e.user = userDirs(ctx, env)
	if err := e.checkScope(e.user); err != nil {
		return nil, fmt.Errorf("%s: %w", e.displayPath(file), err)
	}
	if !opts.ReadOnly && !opts.DryRun {
		if err := e.acquireLock(); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// checkDirs rejects a dir or mirror that leads out of the repository, or
// onto dir or another mirror, through a symlink: the checks of
// skenvfile.Project.Validate on the paths as the file system resolves them.
// sync would otherwise replace the skills of dir through a mirror.
func (e *ProjectScope) checkDirs() error {
	root, err := filepath.EvalSymlinks(e.root)
	if err != nil {
		return err
	}
	seen := map[string]string{}
	for _, rel := range append([]string{e.project.Dir}, e.project.Mirrors...) {
		real := resolveExisting(e.abs(rel))
		in, err := filepath.Rel(root, real)
		if err != nil || in == "." || in == ".." || strings.HasPrefix(in, ".."+string(filepath.Separator)) {
			return fmt.Errorf("[project] %s leads out of the repository through a symlink (to %s)", rel, real)
		}
		for other, r := range seen {
			if real == r || strings.HasPrefix(real, r+string(filepath.Separator)) || strings.HasPrefix(r, real+string(filepath.Separator)) {
				return fmt.Errorf("[project] %s and %s are the same directory, or one is inside the other, through a symlink", other, rel)
			}
		}
		seen[rel] = real
	}
	return nil
}

// resolveExisting resolves the symlinks of the longest existing prefix of
// p and appends the rest.
func resolveExisting(p string) string {
	for cur := p; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			rest, _ := filepath.Rel(cur, p)
			return filepath.Join(real, rest)
		}
		if cur == filepath.Dir(cur) {
			return p
		}
	}
}

// inRepo reports whether p, after cleaning, is inside the repository.
func (e *ProjectScope) inRepo(p string) bool {
	in, err := filepath.Rel(e.root, filepath.Clean(p))
	return err == nil && in != ".." && !strings.HasPrefix(in, ".."+string(filepath.Separator))
}

// abs is the path of rel, a slash path relative to the repository root.
func (e *ProjectScope) abs(rel ...string) string {
	return filepath.Join(append([]string{e.root}, rel...)...)
}

// rel shows p relative to the repository root.
func (e *ProjectScope) rel(p string) string {
	if r, err := filepath.Rel(e.root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return e.displayPath(p)
}

// Sync runs `skenv sync` in a project: copy every pinned skill into dir at
// its rev, remove copies whose entry is gone, and give every skill of dir
// to each mirror.
func (e *ProjectScope) Sync() (int, error) {
	e.computeKept()
	e.syncCopies()
	e.syncMirrors()
	e.warnIgnored()
	e.commitHint("")
	return e.summary("project sync"), nil
}

// computeKept sets kept: the skills of opts.Keep whose copy in dir has no
// marker, or whose mirror entry skenv did not place.
func (e *ProjectScope) computeKept() {
	e.kept = map[string]bool{}
	for _, name := range e.opts.Keep {
		if _, ok := e.project.Skill(name); !ok {
			continue
		}
		var found []string
		p := e.abs(e.project.Dir, name)
		if _, err := os.Lstat(p); err == nil {
			if _, err := readMarker(p); err != nil {
				found = append(found, p)
			}
		}
		for _, m := range e.project.Mirrors {
			p := e.abs(m, name)
			if _, err := os.Lstat(p); err == nil && !e.isMirrorEntry(p, name) {
				found = append(found, p)
			}
		}
		if len(found) > 0 {
			e.infof("leave %s as it is: %s was pinned without a matching commit, so it is not taken over", e.rel(found[0]), name)
			e.kept[name] = true
		}
	}
}

// syncCopies brings the copies in dir in line with the entries of
// [project].
func (e *ProjectScope) syncCopies() {
	dir := e.abs(e.project.Dir)
	want := map[string]bool{}
	for _, s := range e.project.Skills() {
		want[s.Name] = true
		if !e.kept[s.Name] {
			e.syncCopy(s, filepath.Join(dir, s.Name))
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, de := range entries {
		name := de.Name()
		p := filepath.Join(dir, name)
		if want[name] || strings.HasPrefix(name, ".") || !de.IsDir() {
			continue
		}
		mk, err := readMarker(p)
		if err != nil {
			continue // a project-own skill
		}
		if modified, _ := e.modified(p, mk); modified {
			if !e.opts.Adopt {
				e.errorf("%s is no longer in [project] but was edited locally; not removed: keep it as a project-own skill "+
					"by deleting its %s, or rerun with --adopt to move it to %s", e.rel(p), markerName, e.displayPath(e.layout.Backup()))
				continue
			}
			if err := e.backup(p); err != nil {
				e.errorf("adopt %s: %v", e.rel(p), err)
				continue
			}
			e.removed[name] = true
			continue
		}
		e.changef("remove %s (no longer in [project])", e.rel(p))
		e.removed[name] = true
		if e.opts.DryRun {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			e.errorf("remove %s: %v", e.rel(p), err)
		}
	}
}

// modified reports whether the copy at p differs from what skenv copied:
// its content hash is not the one in the marker. A marker without a hash
// cannot vouch for the content, so it counts as modified.
func (e *ProjectScope) modified(p string, mk marker) (bool, error) {
	if mk.Hash == "" {
		return true, nil
	}
	h, err := treeHash(p)
	if err != nil {
		return true, err
	}
	return h != mk.Hash, nil
}

// syncCopy makes dst the copy of s at its rev. A directory without a marker
// (a project-own skill) and a copy edited locally are replaced only with
// --adopt, after a backup.
func (e *ProjectScope) syncCopy(s skenvfile.ProjectSkill, dst string) {
	from := fmt.Sprintf("%s@%.12s (%s)", s.Repo, s.Commit, s.Path)
	fi, err := os.Lstat(dst)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		e.changef("copy %s from %s", e.rel(dst), from)
	case err != nil:
		e.errorf("%s: %v", e.rel(dst), err)
		return
	default:
		mk, merr := readMarker(dst)
		if !fi.IsDir() || merr != nil {
			switch {
			case e.opts.Adopt:
			case fi.IsDir() && e.hasPinnedFiles(s, dst):
				e.errorf("conflict: %s has the files of %s but no %s marker: installed another way, as by the skills CLI before "+
					"`skenv import --project`; run `skenv sync --adopt` to take it over (the copy goes to %s)", e.rel(dst), from, markerName, e.displayPath(e.layout.Backup()))
				return
			default:
				e.errorf("conflict: %s exists without a %s marker (a project-own skill?) and is never replaced; "+
					"rename it or the entry, or rerun with --adopt to move it to %s and copy %s", e.rel(dst), markerName, e.displayPath(e.layout.Backup()), s.Name)
				return
			}
			if err := e.backup(dst); err != nil {
				e.errorf("adopt %s: %v", e.rel(dst), err)
				return
			}
			e.changef("copy %s from %s", e.rel(dst), from)
			break
		}
		modified, err := e.modified(dst, mk)
		if err != nil {
			e.errorf("%s: %v", e.rel(dst), err)
			return
		}
		remote, _ := e.hosts.Resolve(s.Repo) // Validate resolved it already
		same := mk.matches(remote, s.Path, s.Commit)
		if same && !modified {
			return
		}
		if modified {
			if !e.opts.Adopt {
				e.errorf("%s was edited locally (it differs from %s@%.12s) and is not overwritten; move the change upstream "+
					"or into a project-own skill, or rerun with --adopt to move it to %s and copy %s", e.rel(dst), mk.Repo, mk.Rev, e.displayPath(e.layout.Backup()), from)
				return
			}
			if err := e.backup(dst); err != nil {
				e.errorf("adopt %s: %v", e.rel(dst), err)
				return
			}
		}
		switch {
		case same:
			e.changef("restore %s from %s", e.rel(dst), from)
		case mk.matches(remote, s.Path, mk.Rev):
			e.changef("update %s %.12s → %.12s (%s, %s)", e.rel(dst), mk.Rev, s.Commit, s.Repo, s.Path)
		default:
			e.changef("update %s from %s@%.12s (%s) to %s", e.rel(dst), mk.Repo, mk.Rev, mk.Path, from)
		}
	}
	if e.opts.DryRun {
		return
	}
	remote, _ := e.hosts.Resolve(s.Repo) // Validate resolved it already
	if err := e.copySkill(s.Repo, remote.URL, s.Path, s.Commit, dst, true); err != nil {
		e.errorf("copy %s: %v", s.Name, err)
		return
	}
	e.warnLinks(dst)
}

// hasPinnedFiles reports whether the unmarked copy dir has the files of
// s at its commit, as the skills CLI installs them: a copy that sync
// --adopt only takes over.
func (e *ProjectScope) hasPinnedFiles(s skenvfile.ProjectSkill, dir string) bool {
	cache, err := e.ensureCache(s.Repo, s.Commit)
	if err != nil {
		return false
	}
	want, err := e.commitBlobs(cache, s.Commit, s.Path)
	if err != nil {
		return false
	}
	got, err := copyBlobs(dir)
	return err == nil && sameBlobs(got, want)
}

// skillNames lists the skills of dir that mirrors get: its directories with
// a SKILL.md, plus the copies sync is about to make under --dry-run, minus
// the ones it removes.
func (e *ProjectScope) skillNames() []string {
	dir := e.abs(e.project.Dir)
	set := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, de := range entries {
		name := de.Name()
		if strings.HasPrefix(name, ".") || e.removed[name] {
			continue
		}
		if fileExists(filepath.Join(dir, name, "SKILL.md")) {
			set[name] = true
		}
	}
	if e.opts.DryRun {
		for _, s := range e.project.Skills() {
			set[s.Name] = true
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// syncMirrors gives each mirror every skill of dir and removes the mirror
// entries of skills that left it.
func (e *ProjectScope) syncMirrors() {
	names := e.skillNames()
	for _, m := range e.project.Mirrors {
		mdir := e.abs(m)
		in := map[string]bool{}
		for _, name := range names {
			in[name] = true
			if !e.kept[name] {
				e.syncMirror(mdir, name)
			}
		}
		entries, _ := os.ReadDir(mdir)
		for _, de := range entries {
			name := de.Name()
			p := filepath.Join(mdir, name)
			if in[name] || strings.HasPrefix(name, ".") || !e.isMirrorEntry(p, name) {
				continue
			}
			e.changef("remove %s (no skill %s in %s)", e.rel(p), name, e.project.Dir)
			if e.opts.DryRun {
				continue
			}
			if err := os.RemoveAll(p); err != nil {
				e.errorf("remove %s: %v", e.rel(p), err)
			}
		}
	}
}

// linkDest is the relative symlink destination from <mirror>/<name> to
// <dir>/<name>.
func (e *ProjectScope) linkDest(mdir, name string) string {
	rel, err := filepath.Rel(mdir, e.abs(e.project.Dir, name))
	if err != nil {
		return e.abs(e.project.Dir, name)
	}
	return rel
}

// isMirrorEntry reports whether p, an entry of a mirror, is one skenv
// placed there for the skill name: a symlink to <dir>/<name>, or an
// unedited copy with a mirror marker.
func (e *ProjectScope) isMirrorEntry(p, name string) bool {
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		dest, _ := os.Readlink(p)
		return dest == e.linkDest(filepath.Dir(p), name)
	}
	mk, err := readMarker(p)
	if err != nil || mk.Mirror == "" {
		return false
	}
	modified, _ := e.modified(p, mk)
	return !modified
}

// syncMirror makes <mirror>/<name> a symlink to or a copy of <dir>/<name>.
// It replaces a symlink, a copy skenv made, or a directory with the same
// content freely; anything else only with --adopt, after a backup.
func (e *ProjectScope) syncMirror(mdir, name string) {
	p := filepath.Join(mdir, name)
	src := e.abs(e.project.Dir, name)
	copyMode := e.project.MirrorsMode == skenvfile.MirrorCopy
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		e.errorf("%s: %v", e.rel(p), err)
		return
	case fi.Mode()&fs.ModeSymlink != 0:
		dest, _ := os.Readlink(p)
		if !copyMode && dest == e.linkDest(mdir, name) {
			return
		}
		// A symlink into the repository is skenv's to repoint; one that
		// leads out of it (to a personal checkout, say) is not.
		if (filepath.IsAbs(dest) || !e.inRepo(filepath.Join(mdir, dest))) && !e.adoptMirror(p, src,
			fmt.Sprintf("is a symlink out of the repository (to %s)", dest)) {
			return
		}
	default:
		srcHash, _ := treeHash(src)
		h, err := treeHash(p)
		if err != nil {
			e.errorf("%s: %v", e.rel(p), err)
			return
		}
		mk, _ := readMarker(p)
		ours := mk.Mirror != "" && mk.Hash == h
		if copyMode && ours && h == srcHash {
			return
		}
		// The hash leaves out .git: a working copy there is never the same.
		if !ours && (h != srcHash || fileExists(filepath.Join(p, ".git"))) && !e.adoptMirror(p, src,
			fmt.Sprintf("differs from %s and was not made by skenv (edited in the mirror?); move the change to %s", e.rel(src), e.rel(src))) {
			return
		}
	}
	if copyMode {
		e.changef("copy %s to %s", e.rel(src), e.rel(p))
		if !e.opts.DryRun {
			if err := e.copyMirror(src, p); err != nil {
				e.errorf("copy %s: %v", e.rel(p), err)
			}
		}
		return
	}
	dest := e.linkDest(mdir, name)
	e.changef("link %s → %s", e.rel(p), dest)
	if !e.opts.DryRun {
		if err := symlinkAtomic(p, dest); err != nil {
			e.errorf("link %s: %v", e.rel(p), err)
		}
	}
}

// adoptMirror handles a mirror entry p that skenv may not replace on its
// own: a conflict, or under --adopt a backup. It reports whether p may be
// replaced now.
func (e *ProjectScope) adoptMirror(p, src, why string) bool {
	if !e.opts.Adopt {
		e.errorf("conflict: %s %s, or rerun with --adopt to move it to %s and replace it", e.rel(p), why, e.displayPath(e.layout.Backup()))
		return false
	}
	if err := e.backup(p); err != nil {
		e.errorf("adopt %s: %v", e.rel(p), err)
		return false
	}
	return true
}

// copyMirror makes dst a copy of the skill directory src (without its
// marker) with a mirror marker.
func (e *ProjectScope) copyMirror(src, dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".skenv-tmp-"+filepath.Base(dst)+"-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	content := filepath.Join(tmp, "c")
	if err := copyTree(src, content); err != nil {
		return err
	}
	h, err := treeHash(content)
	if err != nil {
		return err
	}
	if err := writeMarker(content, marker{Mirror: e.project.Dir + "/" + filepath.Base(dst), Hash: h}); err != nil {
		return err
	}
	return replace(content, dst)
}

// ignored lists the files of dir and the mirrors that .gitignore keeps
// out of the commit: a clone would not have them, and doctor there would
// report the copy as modified.
func (e *ProjectScope) ignored() []string {
	args := append([]string{"ls-files", "--others", "--ignored", "--exclude-standard", "--", e.project.Dir}, e.project.Mirrors...)
	out, err := e.env.Git.Run(e.ctx, e.root, args...)
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func (e *ProjectScope) ignoredWarning() string {
	files := e.ignored()
	if len(files) == 0 {
		return ""
	}
	return fmt.Sprintf("git ignores %d files of the project skills (%s ...), so they are not committed and a clone reports "+
		"the copy as modified; exclude them from .gitignore, for example with !%s/**", len(files), files[0], e.project.Dir)
}

func (e *ProjectScope) warnIgnored() {
	if e.opts.DryRun {
		return
	}
	if w := e.ignoredWarning(); w != "" {
		e.warnf("%s", w)
	}
}

// warnLinks warns about symlinks in the copy dst that point out of it:
// they are committed with the project, and agents and CI follow them.
func (e *ProjectScope) warnLinks(dst string) {
	_ = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return nil //nolint:nilerr // a warning only
		}
		target, err := os.Readlink(p)
		if err != nil {
			return nil //nolint:nilerr // a warning only
		}
		in, err := filepath.Rel(dst, filepath.Join(filepath.Dir(p), target))
		if filepath.IsAbs(target) || err != nil || in == ".." || strings.HasPrefix(in, ".."+string(filepath.Separator)) {
			e.warnf("%s is a symlink out of the skill (to %s); it is committed with the project as it is", e.rel(p), target)
		}
		return nil
	})
}

// commitHint tells how to commit what changed: the copies and mirrors are
// part of the project, and so is the lock of the skills CLI while git sees
// it edited or removed (by an import, which a later sync --adopt completes).
func (e *ProjectScope) commitHint(msg string) {
	if e.opts.DryRun || e.changes == 0 {
		return
	}
	paths := append([]string{filepath.Base(e.file), e.project.Dir}, e.project.Mirrors...)
	if st, err := e.env.Git.Run(e.ctx, e.root, "status", "--porcelain", "--", projectLockName); err == nil && st != "" {
		paths = append(paths, projectLockName)
	}
	if msg == "" {
		msg = "sync project skills"
	}
	e.infof("the project skills changed; to commit them:\n  git -C %s add -- %s\n  git -C %s commit -m %q",
		e.displayPath(e.root), strings.Join(paths, " "), e.displayPath(e.root), "chore(skills): "+msg)
}

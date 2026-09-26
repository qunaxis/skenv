package engine

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// VendorAddOptions are the arguments of `skenv vendor add`.
type VendorAddOptions struct {
	Repo string
	Path string
	Name string
	Rev  string
}

// VendorAdd pins a third-party skill in the manifest and syncs it.
func (e *Engine) VendorAdd(o VendorAddOptions) (int, error) {
	d, err := e.resolveDependency(o)
	if err != nil {
		return ExitFatal, err
	}
	if _, ok := e.m.Dependencies[d.Name]; ok {
		return ExitFatal, fmt.Errorf("dependency %q is already in the manifest; use `skenv vendor update %s`", d.Name, d.Name)
	}
	if err := e.editManifest(func(data []byte) ([]byte, error) {
		return manifest.AppendDependency(data, filepath.Ext(e.manifestPath), skenvfile.User, d)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("add dependency %s (%s@%.12s, %s) to %s", d.Name, d.Repo, d.Commit, d.SkillDir, e.show(e.manifestPath))
	return e.syncNames([]string{d.Name}, fmt.Sprintf("add dependency %s", d.Name))
}

// resolveDependency turns the arguments of `vendor add` into an entry: the
// commit (HEAD of the default branch unless o.Rev), the skill directory
// (the only one in the repository unless o.Path) and the name (the last
// element of the path unless o.Name). A relative local path in o.Repo is
// made absolute against the working directory: the skenv file does not
// live there.
func (e *base) resolveDependency(o VendorAddOptions) (manifest.Dependency, error) {
	if o.Repo == "" {
		return manifest.Dependency{}, fmt.Errorf("usage: skenv vendor add <repo> [--path P] [--name N] [--rev SHA]")
	}
	remote, err := e.hosts.Resolve(o.Repo)
	if err != nil {
		return manifest.Dependency{}, err
	}
	if isLocalPath(o.Repo) && !filepath.IsAbs(o.Repo) {
		o.Repo = remote.URL
	}
	cache, err := e.ensureCache(o.Repo, "")
	if err != nil {
		return manifest.Dependency{}, err
	}
	rev, err := e.resolveRev(cache, o.Repo, o.Rev)
	if err != nil {
		return manifest.Dependency{}, err
	}
	skillPath, err := e.findSkillPath(cache, rev, o.Path)
	if err != nil {
		return manifest.Dependency{}, err
	}
	name := o.Name
	if name == "" {
		name = path.Base(skillPath)
		if skillPath == "." {
			name = manifest.RepoName(remote.URL)
		}
		name = strings.ToLower(name)
	}
	if err := manifest.ValidName(name); err != nil {
		return manifest.Dependency{}, fmt.Errorf("%w; pass --name", err)
	}
	return manifest.Dependency{Name: name, Repo: o.Repo, SkillDir: skillPath, Commit: rev}, nil
}

// isLocalPath reports whether a repo value is a local path rather than a
// URL, an scp-like address or a short form.
func isLocalPath(repo string) bool {
	return strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "./") || strings.HasPrefix(repo, "../") || repo == "." || repo == ".."
}

// VendorUpdate moves dependencies to a new commit and syncs them: the
// named ones, or every dependency when names is empty. Each goes to HEAD
// of its default branch; rev pins a single named skill instead.
func (e *Engine) VendorUpdate(names []string, rev string) (int, error) {
	if len(names) == 0 {
		for _, d := range e.m.DependencyList() {
			names = append(names, d.Name)
		}
		if len(names) == 0 {
			e.infof("no dependencies in %s", e.show(e.manifestPath))
			return e.finish("vendor")
		}
	}
	for _, name := range names {
		if _, ok := e.m.Dependencies[name]; !ok {
			return ExitFatal, fmt.Errorf("dependency %q is not in the manifest %s", name, e.show(e.manifestPath))
		}
	}
	names = slices.Compact(slices.Sorted(slices.Values(names)))
	var updated []string
	for _, name := range names {
		newRev, err := e.updateRev(name, rev)
		switch {
		case err != nil:
			e.errorf("update dependency %s: %v", name, err)
		case newRev != "":
			updated = append(updated, fmt.Sprintf("%s to %.12s", name, newRev))
		}
	}
	hint := ""
	switch len(updated) {
	case 0:
	case 1:
		hint = "update dependency " + updated[0]
	default:
		hint = "update dependencies " + strings.Join(updated, ", ")
	}
	return e.syncNames(names, hint)
}

// updateRev moves the dependency name to rev (default: HEAD of the
// default branch), shows the log of its skill directory and writes the new
// commit into the manifest. It returns the new commit, or "" when the
// skill is already there.
func (e *Engine) updateRev(name, rev string) (string, error) {
	d := e.m.Dependencies[name]
	newRev, err := e.nextRev("dependency "+name, name, d.Repo, d.SkillDir, d.Commit, rev)
	if err != nil || newRev == "" {
		return "", err
	}
	old := d.Commit
	if err := e.editManifest(func(data []byte) ([]byte, error) {
		return manifest.SetDependencyCommit(data, filepath.Ext(e.manifestPath), skenvfile.User, name, newRev)
	}); err != nil {
		return "", err
	}
	e.changef("update dependency %s %.12s → %.12s in %s", name, old, newRev, e.show(e.manifestPath))
	return newRev, nil
}

// nextRev resolves rev (default: HEAD of the default branch) of repo for
// the entry what, pinned at old, and shows the log of dir between them
// under the heading name. It returns "" when the entry is already there.
func (e *base) nextRev(what, name, repo, dir, old, rev string) (string, error) {
	cache, err := e.ensureCache(repo, "")
	if err != nil {
		return "", err
	}
	newRev, err := e.resolveRev(cache, repo, rev)
	if err != nil {
		return "", err
	}
	if newRev == old {
		e.infof("%s is already at %.12s", what, newRev)
		return "", nil
	}
	if e.hasCommit(cache, old) {
		args := []string{"log", "--oneline", old + ".." + newRev}
		if dir != "." {
			args = append(args, "--", dir)
		}
		if log, err := e.env.Git.Run(e.ctx, cache, args...); err == nil {
			if log == "" {
				log = "(no commits touch " + dir + ")"
			}
			e.infof("%s %.12s..%.12s:\n%s", name, old, newRev, log)
		}
	}
	return newRev, nil
}

// VendorRemove drops a dependency from the manifest and removes its
// managed paths.
func (e *Engine) VendorRemove(name string) (int, error) {
	if _, ok := e.m.Dependencies[name]; !ok {
		return ExitFatal, fmt.Errorf("dependency %q is not in the manifest %s", name, e.show(e.manifestPath))
	}
	if err := e.editManifest(func(data []byte) ([]byte, error) {
		return manifest.RemoveDependency(data, filepath.Ext(e.manifestPath), skenvfile.User, name)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("remove dependency %s from %s", name, e.show(e.manifestPath))
	for _, p := range e.st.Paths() {
		if entry := e.st.Managed[p]; entry.Skill == name {
			e.removeManaged(p, entry)
		}
	}
	e.commitHint(fmt.Sprintf("remove dependency %s", name))
	return e.finish("vendor remove")
}

// editManifest applies edit to the manifest text, validates the result,
// writes it (unless --dry-run) and reloads it.
func (e *Engine) editManifest(edit func([]byte) ([]byte, error)) error {
	data, err := e.readSkenvFile(e.manifestPath)
	if err != nil {
		return err
	}
	out, err := edit(data)
	if err != nil {
		return err
	}
	// A skenv schema directive moves to the version of this skenv.
	if out, err = skenvfile.Stamp(out, filepath.Ext(e.manifestPath), false); err != nil {
		return err
	}
	m, err := manifest.ParseIn(out, filepath.Ext(e.manifestPath), filepath.Dir(e.manifestPath))
	if err != nil {
		return err
	}
	// Name clashes with the skills of checkouts (M1) are only visible with
	// the checkouts listed; check before writing so a bad edit never lands.
	prev := e.m
	restore := func() { _ = e.setManifest(prev) }
	if err := e.setManifest(m); err != nil {
		restore()
		return err
	}
	if _, err := e.skills(); err != nil {
		restore()
		return err
	}
	if err := e.writeSkenvFile(e.manifestPath, out); err != nil {
		restore()
		return fmt.Errorf("write manifest %s: %w", e.show(e.manifestPath), err)
	}
	return nil
}

// syncNames vendors and links the named skills after a manifest edit.
func (e *Engine) syncNames(names []string, hint string) (int, error) {
	skills, err := e.skills()
	if err != nil {
		return ExitFatal, err
	}
	var sel []Skill
	for _, name := range names {
		found := false
		for _, s := range skills {
			if s.Name == name {
				sel = append(sel, s)
				found = true
			}
		}
		if !found {
			e.infof("skill %s is not installed on this machine (%s)", name, e.unselected[name])
		}
	}
	for _, s := range sel {
		if s.Dependency != nil {
			e.syncDependency(s)
		}
	}
	e.linkAll(sel)
	if hint != "" {
		e.commitHint(hint)
	}
	return e.finish("vendor")
}

func (e *Engine) commitHint(msg string) {
	if e.opts.DryRun {
		return
	}
	dir := filepath.Dir(e.manifestPath)
	name := filepath.Base(e.manifestPath)
	// `git commit -- <file>` fails on a file git does not track yet.
	add := ""
	if !e.env.Git.OK(e.ctx, dir, "ls-files", "--error-unmatch", "--", name) {
		add = fmt.Sprintf("  git -C %s add -- %s\n", e.show(dir), name)
	}
	e.infof("manifest changed but not committed; to commit:\n%s  git -C %s commit -m %q -- %s",
		add, e.show(dir), "chore(manifest): "+msg, name)
}

// resolveRev returns the full SHA for rev, or the HEAD of the remote's
// default branch when rev is empty.
func (e *base) resolveRev(cache, repo, rev string) (string, error) {
	if rev == "" {
		out, err := e.env.Git.Run(e.ctx, cache, "ls-remote", "origin", "HEAD")
		if err != nil {
			return "", err
		}
		f := strings.Fields(out)
		if len(f) == 0 {
			return "", fmt.Errorf("%s has no HEAD (empty repository?); pass --rev", repo)
		}
		rev = f[0]
	}
	if !e.hasCommit(cache, rev) {
		_, _ = e.env.Git.Run(e.ctx, cache, "fetch", "--quiet", "origin", rev)
	}
	full, err := e.env.Git.Run(e.ctx, cache, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil || full == "" {
		return "", fmt.Errorf("commit %q not found in %s", rev, repo)
	}
	return full, nil
}

// findSkillPath returns the directory holding SKILL.md at rev: the given
// path (verified) or the only such directory in the repository.
func (e *base) findSkillPath(cache, rev, want string) (string, error) {
	if want != "" {
		want = path.Clean(strings.Trim(want, "/"))
		if want == "" {
			want = "."
		}
		file := "SKILL.md"
		if want != "." {
			file = want + "/SKILL.md"
		}
		if !e.env.Git.OK(e.ctx, cache, "cat-file", "-e", rev+":"+file) {
			return "", fmt.Errorf("no SKILL.md at %s in %.12s", want, rev)
		}
		return want, nil
	}
	out, err := e.env.Git.Run(e.ctx, cache, "ls-tree", "-r", "--name-only", rev)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, f := range strings.Split(out, "\n") {
		if path.Base(f) == "SKILL.md" {
			dirs = append(dirs, path.Dir(f))
		}
	}
	sort.Strings(dirs)
	switch len(dirs) {
	case 0:
		return "", fmt.Errorf("no SKILL.md found at %.12s", rev)
	case 1:
		return dirs[0], nil
	}
	return "", fmt.Errorf("several skills found, pick one with --path:\n  %s", strings.Join(dirs, "\n  "))
}

package engine

import (
	"fmt"
	"os"
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
	if o.Repo == "" {
		return ExitFatal, fmt.Errorf("usage: skenv vendor add <owner/repo> [--path P] [--name N] [--rev SHA]")
	}
	cache, err := e.ensureCache(o.Repo, "")
	if err != nil {
		return ExitFatal, err
	}
	rev, err := e.resolveRev(cache, o.Repo, o.Rev)
	if err != nil {
		return ExitFatal, err
	}
	skillPath, err := e.findSkillPath(cache, rev, o.Path)
	if err != nil {
		return ExitFatal, err
	}
	name := o.Name
	if name == "" {
		name = path.Base(skillPath)
		if skillPath == "." {
			name = manifest.RepoName(o.Repo)
		}
		name = strings.ToLower(name)
	}
	if err := manifest.ValidName(name); err != nil {
		return ExitFatal, fmt.Errorf("%w; pass --name", err)
	}
	if _, ok := e.m.FindVendor(name); ok {
		return ExitFatal, fmt.Errorf("vendor %q is already in the manifest; use `skenv vendor update %s`", name, name)
	}
	v := manifest.Vendor{Name: name, Repo: o.Repo, Path: skillPath, Rev: rev}
	if err := e.editManifest(func(data []byte) ([]byte, error) { return manifest.AppendVendor(data, filepath.Ext(e.manifestPath), v) }); err != nil {
		return ExitFatal, err
	}
	e.changef("add vendor %s (%s@%.12s, %s) to %s", name, o.Repo, rev, skillPath, e.show(e.manifestPath))
	return e.syncNames([]string{name}, fmt.Sprintf("add vendor skill %s", name))
}

// VendorUpdate moves vendored skills to a new commit and syncs them: the
// named ones, or every vendored skill when names is empty. Each goes to
// HEAD of its default branch; rev pins a single named skill instead.
func (e *Engine) VendorUpdate(names []string, rev string) (int, error) {
	if len(names) == 0 {
		for _, v := range e.m.Vendor {
			names = append(names, v.Name)
		}
		if len(names) == 0 {
			e.infof("no vendored skills in %s", e.show(e.manifestPath))
			return e.finish("vendor")
		}
	}
	for _, name := range names {
		if _, ok := e.m.FindVendor(name); !ok {
			return ExitFatal, fmt.Errorf("vendor %q is not in the manifest %s", name, e.show(e.manifestPath))
		}
	}
	names = slices.Compact(slices.Sorted(slices.Values(names)))
	var updated []string
	for _, name := range names {
		newRev, err := e.updateRev(name, rev)
		switch {
		case err != nil:
			e.errorf("update vendor %s: %v", name, err)
		case newRev != "":
			updated = append(updated, fmt.Sprintf("%s to %.12s", name, newRev))
		}
	}
	hint := ""
	switch len(updated) {
	case 0:
	case 1:
		hint = "update vendor skill " + updated[0]
	default:
		hint = "update vendor skills " + strings.Join(updated, ", ")
	}
	return e.syncNames(names, hint)
}

// updateRev moves the vendored skill name to rev (default: HEAD of the
// default branch), shows the log of its path and writes the new rev into
// the manifest. It returns the new rev, or "" when the skill is already
// there.
func (e *Engine) updateRev(name, rev string) (string, error) {
	v, _ := e.m.FindVendor(name)
	cache, err := e.ensureCache(v.Repo, "")
	if err != nil {
		return "", err
	}
	newRev, err := e.resolveRev(cache, v.Repo, rev)
	if err != nil {
		return "", err
	}
	if newRev == v.Rev {
		e.infof("vendor %s is already at %.12s", name, newRev)
		return "", nil
	}
	if e.hasCommit(cache, v.Rev) {
		args := []string{"log", "--oneline", v.Rev + ".." + newRev}
		if v.Path != "." {
			args = append(args, "--", v.Path)
		}
		if log, err := e.env.Git.Run(e.ctx, cache, args...); err == nil {
			if log == "" {
				log = "(no commits touch " + v.Path + ")"
			}
			e.infof("%s %.12s..%.12s:\n%s", name, v.Rev, newRev, log)
		}
	}
	old := v.Rev
	if err := e.editManifest(func(data []byte) ([]byte, error) {
		return manifest.SetVendorRev(data, filepath.Ext(e.manifestPath), name, newRev)
	}); err != nil {
		return "", err
	}
	e.changef("update vendor %s %.12s → %.12s in %s", name, old, newRev, e.show(e.manifestPath))
	return newRev, nil
}

// VendorRemove drops a vendored skill from the manifest and removes its
// managed paths.
func (e *Engine) VendorRemove(name string) (int, error) {
	if _, ok := e.m.FindVendor(name); !ok {
		return ExitFatal, fmt.Errorf("vendor %q is not in the manifest %s", name, e.show(e.manifestPath))
	}
	if err := e.editManifest(func(data []byte) ([]byte, error) {
		return manifest.RemoveVendor(data, filepath.Ext(e.manifestPath), name)
	}); err != nil {
		return ExitFatal, err
	}
	e.changef("remove vendor %s from %s", name, e.show(e.manifestPath))
	for _, p := range e.st.Paths() {
		if entry := e.st.Managed[p]; entry.Skill == name {
			e.removeManaged(p, entry)
		}
	}
	e.commitHint(fmt.Sprintf("remove vendor skill %s", name))
	return e.finish("vendor remove")
}

// editManifest applies edit to the manifest text, validates the result,
// writes it (unless --dry-run) and reloads it.
func (e *Engine) editManifest(edit func([]byte) ([]byte, error)) error {
	data, err := os.ReadFile(e.manifestPath)
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
	m, err := manifest.Parse(out, filepath.Ext(e.manifestPath))
	if err != nil {
		return err
	}
	// Name clashes with own skills (M1) are only visible with the own
	// repositories listed; check before writing so a bad edit never lands.
	prev := e.m
	e.setManifest(m)
	if _, err := e.skills(); err != nil {
		e.setManifest(prev)
		return err
	}
	if !e.opts.DryRun {
		if err := manifest.WriteFile(e.manifestPath, out); err != nil {
			e.setManifest(prev)
			return fmt.Errorf("write manifest %s: %w", e.show(e.manifestPath), err)
		}
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
			e.infof("skill %s is skipped on host %s", name, e.env.Hostname)
		}
	}
	for _, s := range sel {
		if s.Vendor != nil {
			e.syncVendor(s)
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
	e.infof("manifest changed but not committed; to commit:\n  git -C %s commit -m %q -- %s",
		e.show(dir), "chore(manifest): "+msg, filepath.Base(e.manifestPath))
}

// resolveRev returns the full SHA for rev, or the HEAD of the remote's
// default branch when rev is empty.
func (e *Engine) resolveRev(cache, repo, rev string) (string, error) {
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
func (e *Engine) findSkillPath(cache, rev, want string) (string, error) {
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

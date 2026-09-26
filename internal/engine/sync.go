package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/state"
)

const markerName = ".skenv"

// marker is the content of the .skenv file in a directory skenv copied:
// a dependency in the store or in a project (repo, path, rev; hash in a
// project), or a copy in a project mirror (mirror, hash). Repo is the
// canonical clone URL (manifest.Remote.URL), not the value written in the
// skenv file. The marker is observed state, so its keys keep their names.
type marker struct {
	Repo   string `toml:"repo,omitempty"`
	Path   string `toml:"path,omitempty"`
	Rev    string `toml:"rev,omitempty"`
	Mirror string `toml:"mirror,omitempty"` // <dir>/<name> of the project
	Hash   string `toml:"hash,omitempty"`   // treeHash of the content
}

// matches reports whether the marker records the directory path of the
// repository remote at rev.
func (mk marker) matches(remote manifest.Remote, path, rev string) bool {
	return manifest.NormalizeURL(mk.Repo) == manifest.NormalizeURL(remote.URL) && mk.Path == path && mk.Rev == rev
}

// showRepo is how output names a repository: the value of the skenv file,
// followed by its canonical URL when that differs, credentials masked.
func (e *Engine) showRepo(repo string, remote manifest.Remote) string {
	if remote.URL == "" || remote.URL == repo {
		return gitx.Mask(repo)
	}
	return gitx.Mask(repo + " (" + remote.URL + ")")
}

func readMarker(dir string) (marker, error) {
	var mk marker
	_, err := toml.DecodeFile(filepath.Join(dir, markerName), &mk)
	return mk, err
}

// writeMarker writes the marker of dir, a copy skenv just made. A .skenv
// that came with the copied content (a file, or a symlink that would
// redirect the write) is replaced, never written through.
func writeMarker(dir string, mk marker) error {
	b := []byte("# managed by skenv, do not edit\n")
	for _, kv := range [][2]string{{"repo", mk.Repo}, {"path", mk.Path}, {"rev", mk.Rev}, {"mirror", mk.Mirror}, {"hash", mk.Hash}} {
		if kv[1] != "" {
			b = fmt.Appendf(b, "%s = %q\n", kv[0], kv[1])
		}
	}
	p := filepath.Join(dir, markerName)
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Sync runs `skenv sync`: update the checkouts, materialize the
// dependencies, link everything and prune managed paths that left the
// manifest.
func (e *Engine) Sync() (int, error) {
	e.syncCheckouts()
	// The manifest usually lives in a checkout that was just pulled.
	m, err := manifest.Load(e.manifestPath)
	if err != nil {
		return ExitFatal, err
	}
	if err := e.setManifest(m); err != nil {
		return ExitFatal, err
	}
	if root, details := e.manifestElsewhere(); len(details) > 0 {
		for _, d := range details {
			e.warnf("%s: %s", e.show(root), d)
		}
	}
	skills, err := e.skills()
	if err != nil {
		return ExitFatal, err
	}
	kept := e.kept(skills)
	take := slices.DeleteFunc(slices.Clone(skills), func(s Skill) bool { return kept[s.Name] })
	for _, s := range take {
		if s.Dependency != nil {
			e.syncDependency(s)
		}
	}
	e.linkAll(take)
	e.prune(skills)
	return e.finish("sync")
}

// kept is the skills of opts.Keep that have an unmanaged path in the way;
// sync leaves each of them as it is installed.
func (e *Engine) kept(skills []Skill) map[string]bool {
	out := map[string]bool{}
	for _, s := range skills {
		if !slices.Contains(e.opts.Keep, s.Name) {
			continue
		}
		paths := []string{e.storePath(s.Name)}
		for _, t := range e.targets {
			paths = append(paths, filepath.Join(t, s.Name))
		}
		for _, p := range paths {
			if _, err := os.Lstat(p); err == nil && !e.owned(p) {
				e.infof("leave %s as it is: %s was pinned without a matching commit, so it is not taken over", e.show(p), s.Name)
				out[s.Name] = true
				break
			}
		}
	}
	return out
}

// Link runs `skenv link`.
func (e *Engine) Link() (int, error) {
	skills, err := e.skills()
	if err != nil {
		return ExitFatal, err
	}
	e.linkAll(skills)
	return e.finish("link")
}

// syncCheckouts clones missing checkouts and fast-forwards clean ones.
// Dirty or diverged working copies are left alone with a warning.
func (e *Engine) syncCheckouts() {
	for _, c := range e.m.CheckoutList() {
		dir := e.checkoutPath(c)
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			// Validate resolved every repo of the manifest already.
			remote, _ := e.m.Remote(c.Repo)
			e.changef("clone %s into %s", e.showRepo(c.Repo, remote), e.show(dir))
			if e.opts.DryRun {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				e.errorf("clone %s: %v", c.Repo, err)
				continue
			}
			if _, err := e.env.Git.Run(e.ctx, "", "clone", "--quiet", remote.URL, dir); err != nil {
				e.errorf("clone %s: %v (%s)", c.Repo, err, remote.AccessHint())
			}
			continue
		}
		if !e.env.Git.OK(e.ctx, dir, "rev-parse", "--is-inside-work-tree") {
			e.warnf("%s exists but is not a git working copy; leaving it alone (checkout %s)", e.show(dir), c.ID)
			continue
		}
		if out, err := e.env.Git.Run(e.ctx, dir, "status", "--porcelain"); err != nil {
			e.warnf("%s: %v", e.show(dir), err)
			continue
		} else if out != "" {
			e.warnf("%s has uncommitted changes; not pulling (commit or stash, then rerun sync)", e.show(dir))
			continue
		}
		before, _ := e.env.Git.Run(e.ctx, dir, "rev-parse", "HEAD")
		if e.opts.DryRun {
			// The preview cannot see upstream changes: say so, since the
			// manifest itself often lives in this working copy.
			e.infof("would pull --ff-only %s (not pulled by --dry-run: the plan uses its current commit)", e.show(dir))
			continue
		}
		if _, err := e.env.Git.Run(e.ctx, dir, "pull", "--ff-only", "--quiet"); err != nil {
			e.warnf("%s: not updated, pull --ff-only failed (diverged from upstream or offline?): %v", e.show(dir), err)
			continue
		}
		if after, _ := e.env.Git.Run(e.ctx, dir, "rev-parse", "HEAD"); after != before {
			e.changef("pull %s (%.7s → %.7s)", e.show(dir), before, after)
		}
	}
}

// ensureCache clones or refreshes the partial clone for repo and makes sure
// rev (when given) is present. The cache directory is keyed by the URL git
// really fetches from (after url.<base>.insteadOf), and a cache whose origin
// is another repository is cloned again, so two repositories never share one.
func (e *base) ensureCache(repo, rev string) (string, error) {
	remote, err := e.hosts.ResolveIn(e.hostsDir, repo)
	if err != nil {
		return "", err
	}
	url := remote.URL
	root := e.layout.Cache()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	// Resolve and clone from the cache root with discovery stopped there,
	// so the config of a repository skenv happens to run in (a local
	// insteadOf, an includeIf) cannot change the key or the clone, and the
	// result matches `remote get-url` inside the cache.
	git := e.env.Git
	git.Env = append(slices.Clip(git.Env), "GIT_CEILING_DIRECTORIES="+root)
	resolved, err := git.Run(e.ctx, root, "ls-remote", "--get-url", url)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, manifest.CacheKey(resolved))
	if !e.cacheIsFor(dir, resolved) {
		if err := e.cloneCache(git, url, dir); err != nil {
			return "", fmt.Errorf("%w (%s)", err, remote.AccessHint())
		}
	} else if rev == "" || !e.hasCommit(dir, rev) {
		if _, err := e.env.Git.Run(e.ctx, dir, "fetch", "--quiet", "--force", "origin"); err != nil {
			return "", err
		}
	}
	if rev != "" && !e.hasCommit(dir, rev) {
		// Commits that are not on any branch can still be fetched by SHA.
		_, _ = e.env.Git.Run(e.ctx, dir, "fetch", "--quiet", "origin", rev)
		if !e.hasCommit(dir, rev) {
			return "", fmt.Errorf("commit %s not found in %s (force-pushed away, or commit and repo in the skenv file do not match?)", rev, repo)
		}
	}
	return dir, nil
}

// cacheIsFor reports whether dir is a clone whose origin is resolved,
// compared after manifest.NormalizeURL. A clone of another repository is
// reported (credentials masked) so the caller clones again.
func (e *base) cacheIsFor(dir, resolved string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	origin, err := e.env.Git.Run(e.ctx, dir, "remote", "get-url", "origin")
	if err == nil && manifest.NormalizeURL(origin) == manifest.NormalizeURL(resolved) {
		return true
	}
	if err != nil {
		e.infof("vendor cache %s has no origin; cloning %s again", e.show(dir), resolved)
		return false
	}
	e.infof("vendor cache %s is a clone of %s, not %s; cloning again", e.show(dir), origin, resolved)
	return false
}

// cloneCache makes dir a fresh partial clone of url. The clone goes into a
// temporary sibling first and replaces dir only when it is complete, so an
// interrupted clone never leaves a broken cache behind.
func (e *base) cloneCache(git gitx.Git, url, dir string) error {
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".skenv-tmp-"+filepath.Base(dir)+"-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if _, err := git.Run(e.ctx, filepath.Dir(dir), "clone", "--quiet", "--filter=blob:none", "--no-checkout", url, tmp); err != nil {
		return err
	}
	return replace(tmp, dir)
}

func (e *base) hasCommit(dir, rev string) bool {
	return e.env.Git.OK(e.ctx, dir, "cat-file", "-e", rev+"^{commit}")
}

// syncDependency makes store/<name> a copy of the skill directory of the
// dependency at its commit.
func (e *Engine) syncDependency(s Skill) {
	d := s.Dependency
	dst := e.storePath(s.Name)
	remote, _ := e.m.Remote(d.Repo) // Validate resolved it already
	// Only a copy skenv made there counts: after a change of storage.dir a
	// link of an agent directory may stand where the store now is.
	if e.owned(dst) && e.st.Managed[dst].Kind == state.VendorDir {
		if mk, err := readMarker(dst); err == nil && mk.matches(remote, d.SkillDir, d.Commit) {
			e.manage(dst, state.Entry{Kind: state.VendorDir, Skill: s.Name})
			return
		}
	}
	if !e.claim(dst, s.Name) {
		return
	}
	e.changef("vendor %s from %s@%.12s (%s)", s.Name, d.Repo, d.Commit, d.SkillDir)
	if e.opts.DryRun {
		return
	}
	if err := e.copySkill(d.Repo, remote.URL, d.SkillDir, d.Commit, dst, false); err != nil {
		e.errorf("vendor %s: %v", s.Name, err)
		return
	}
	e.manage(dst, state.Entry{Kind: state.VendorDir, Skill: s.Name})
}

// copySkill makes dst a copy of the directory skillPath of repo at rev
// with a .skenv marker that records url, the canonical clone URL of repo,
// replacing what is there only once the copy is complete. withHash records
// the content hash in the marker, for copies that are committed to a
// project and checked by doctor there.
func (e *base) copySkill(repo, url, skillPath, rev, dst string, withHash bool) error {
	cache, err := e.ensureCache(repo, rev)
	if err != nil {
		return err
	}
	if _, err := e.env.Git.Run(e.ctx, cache, "checkout", "--quiet", "--force", "--detach", rev); err != nil {
		return err
	}
	src := filepath.Join(cache, filepath.FromSlash(skillPath))
	if !fileExists(filepath.Join(src, "SKILL.md")) {
		return fmt.Errorf("%s has no SKILL.md at %s@%.12s; fix skill_dir in the skenv file", skillPath, repo, rev)
	}
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".skenv-tmp-"+filepath.Base(dst)+"-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// MkdirTemp created tmp; copy into a fresh child so copyTree owns it.
	content := filepath.Join(tmp, "c")
	if err := copyTree(src, content); err != nil {
		return err
	}
	mk := marker{Repo: url, Path: skillPath, Rev: rev}
	if withHash {
		if mk.Hash, err = treeHash(content); err != nil {
			return err
		}
	}
	if err := writeMarker(content, mk); err != nil {
		return err
	}
	return replace(content, dst)
}

// linkAll creates store links for the skills of checkouts and target links
// for every skill.
func (e *Engine) linkAll(skills []Skill) {
	for _, s := range skills {
		if s.CheckoutDir == "" {
			continue
		}
		p := e.storePath(s.Name)
		if !e.claim(p, s.Name) {
			continue
		}
		if err := e.placeSymlink(p, s.CheckoutDir, s.Name); err != nil {
			e.errorf("link %s: %v", e.show(p), err)
		}
	}
	for _, s := range skills {
		if _, err := os.Lstat(e.storePath(s.Name)); err != nil && !e.opts.DryRun {
			if s.Dependency != nil {
				e.warnf("skill %q is not in the store yet; run `skenv sync`", s.Name)
			}
			continue
		}
		for _, t := range e.targets {
			p := filepath.Join(t, s.Name)
			if !e.claim(p, s.Name) {
				continue
			}
			if err := e.placeSymlink(p, e.linkDest(t, s.Name), s.Name); err != nil {
				e.errorf("link %s: %v", e.show(p), err)
			}
		}
	}
}

// prune removes managed paths that are no longer wanted.
func (e *Engine) prune(skills []Skill) {
	if e.checkoutUnavailable {
		e.warnf("some checkouts are not available; skipping removal of stale managed paths")
		return
	}
	want := e.desired(skills)
	for _, p := range e.st.Paths() {
		if _, ok := want[p]; ok {
			continue
		}
		e.removeManaged(p, e.st.Managed[p])
	}
}

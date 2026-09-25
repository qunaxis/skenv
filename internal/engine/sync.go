package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/state"
)

const markerName = ".skenv"

// marker is the content of store/<name>/.skenv for vendored skills.
type marker struct {
	Repo string `toml:"repo"`
	Path string `toml:"path"`
	Rev  string `toml:"rev"`
}

func readMarker(dir string) (marker, error) {
	var mk marker
	_, err := toml.DecodeFile(filepath.Join(dir, markerName), &mk)
	return mk, err
}

// Sync runs `skenv sync`: update own working copies, materialize vendored
// skills, link everything and prune managed paths that left the manifest.
func (e *Engine) Sync() (int, error) {
	e.syncOwn()
	// The manifest usually lives in an own repository that was just pulled.
	m, err := manifest.Load(e.manifestPath)
	if err != nil {
		return ExitFatal, err
	}
	e.setManifest(m)
	skills, err := e.skills()
	if err != nil {
		return ExitFatal, err
	}
	for _, s := range skills {
		if s.Vendor != nil {
			e.syncVendor(s)
		}
	}
	e.linkAll(skills)
	e.prune(skills)
	return e.finish("sync")
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

// syncOwn clones missing own repositories and fast-forwards clean ones.
// Dirty or diverged working copies are left alone with a warning.
func (e *Engine) syncOwn() {
	for i := range e.m.Own {
		o := &e.m.Own[i]
		dir := e.ownPath(o)
		url := manifest.RepoURL(o.Repo)
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			e.changef("clone %s into %s", o.Repo, e.show(dir))
			if e.opts.DryRun {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				e.errorf("clone %s: %v", o.Repo, err)
				continue
			}
			if _, err := e.env.Git.Run(e.ctx, "", "clone", "--quiet", url, dir); err != nil {
				e.errorf("clone %s: %v (check access to the repository: ssh key or git credential helper)", o.Repo, err)
			}
			continue
		}
		if !e.env.Git.OK(e.ctx, dir, "rev-parse", "--is-inside-work-tree") {
			e.warnf("%s exists but is not a git working copy; leaving it alone (own repo %s)", e.show(dir), o.Repo)
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
			e.infof("would pull --ff-only %s", e.show(dir))
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
func (e *Engine) ensureCache(repo, rev string) (string, error) {
	url := manifest.RepoURL(repo)
	resolved, err := e.env.Git.Run(e.ctx, "", "ls-remote", "--get-url", url)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(e.layout.Cache(), manifest.CacheKey(resolved))
	if !e.cacheIsFor(dir, resolved) {
		if err := e.cloneCache(url, dir); err != nil {
			return "", err
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
			return "", fmt.Errorf("commit %s not found in %s (force-pushed away, or rev and repo in the manifest do not match?)", rev, repo)
		}
	}
	return dir, nil
}

// cacheIsFor reports whether dir is a clone whose origin is resolved,
// compared after manifest.NormalizeURL. A clone of another repository is
// reported (credentials masked) so the caller clones again.
func (e *Engine) cacheIsFor(dir, resolved string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	origin, err := e.env.Git.Run(e.ctx, dir, "remote", "get-url", "origin")
	if err == nil && manifest.NormalizeURL(origin) == manifest.NormalizeURL(resolved) {
		return true
	}
	if err != nil {
		origin = "no origin"
	}
	e.infof("vendor cache %s is a clone of %s, not %s; cloning again", e.show(dir), origin, resolved)
	return false
}

// cloneCache makes dir a fresh partial clone of url. The clone goes into a
// temporary sibling first and replaces dir only when it is complete, so an
// interrupted clone never leaves a broken cache behind.
func (e *Engine) cloneCache(url, dir string) error {
	root := filepath.Dir(dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, ".skenv-tmp-"+filepath.Base(dir)+"-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if _, err := e.env.Git.Run(e.ctx, "", "clone", "--quiet", "--filter=blob:none", "--no-checkout", url, tmp); err != nil {
		return err
	}
	return replace(tmp, dir)
}

func (e *Engine) hasCommit(dir, rev string) bool {
	return e.env.Git.OK(e.ctx, dir, "cat-file", "-e", rev+"^{commit}")
}

// syncVendor makes store/<name> a copy of the vendored path at rev.
func (e *Engine) syncVendor(s Skill) {
	v := s.Vendor
	dst := e.storePath(s.Name)
	want := marker{Repo: v.Repo, Path: v.Path, Rev: v.Rev}
	if e.st.Is(dst) {
		if mk, err := readMarker(dst); err == nil && mk == want {
			e.manage(dst, state.Entry{Kind: state.VendorDir, Skill: s.Name})
			return
		}
	}
	if !e.claim(dst, s.Name) {
		return
	}
	e.changef("vendor %s from %s@%.12s (%s)", s.Name, v.Repo, v.Rev, v.Path)
	if e.opts.DryRun {
		return
	}
	if err := e.materialize(v, dst); err != nil {
		e.errorf("vendor %s: %v", s.Name, err)
		return
	}
	e.manage(dst, state.Entry{Kind: state.VendorDir, Skill: s.Name})
}

func (e *Engine) materialize(v *manifest.Vendor, dst string) error {
	cache, err := e.ensureCache(v.Repo, v.Rev)
	if err != nil {
		return err
	}
	if _, err := e.env.Git.Run(e.ctx, cache, "checkout", "--quiet", "--force", "--detach", v.Rev); err != nil {
		return err
	}
	src := filepath.Join(cache, filepath.FromSlash(v.Path))
	if !fileExists(filepath.Join(src, "SKILL.md")) {
		return fmt.Errorf("%s has no SKILL.md at %s@%.12s; fix path in the manifest", v.Path, v.Repo, v.Rev)
	}
	if err := os.MkdirAll(e.store, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(e.store, ".skenv-tmp-"+v.Name+"-")
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
	var b []byte
	b = fmt.Appendf(b, "# managed by skenv, do not edit\nrepo = %q\npath = %q\nrev = %q\n", v.Repo, v.Path, v.Rev)
	if err := os.WriteFile(filepath.Join(content, markerName), b, 0o644); err != nil {
		return err
	}
	return replace(content, dst)
}

// linkAll creates store links for own skills and target links for every
// skill.
func (e *Engine) linkAll(skills []Skill) {
	for _, s := range skills {
		if s.OwnDir == "" {
			continue
		}
		p := e.storePath(s.Name)
		if !e.claim(p, s.Name) {
			continue
		}
		if err := e.placeSymlink(p, s.OwnDir, s.Name); err != nil {
			e.errorf("link %s: %v", e.show(p), err)
		}
	}
	for _, s := range skills {
		if _, err := os.Lstat(e.storePath(s.Name)); err != nil && !e.opts.DryRun {
			if s.Vendor != nil {
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
	if e.ownUnavailable {
		e.warnf("some own repositories are not available; skipping removal of stale managed paths")
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

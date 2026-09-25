package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
)

// Clone connects this machine to an existing manifest repository: it
// clones repo into dir (default ./<repo name>, like git clone) and records
// its skenv file as the manifest, as Use does. A dir that is already a
// working copy of repo is used as it is. It never syncs, for a new clone
// or an existing one: applying the manifest stays an explicit `skenv sync`.
// format is the format of a new config file ("" for TOML).
func Clone(ctx context.Context, env Env, repo, dir, format string, dryRun bool) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	// The manifest, with its declared hosts, is not cloned yet: only the
	// built-in forms resolve here.
	remote, err := manifest.Hosts(nil).Resolve(repo)
	if err != nil {
		return ExitFatal, fmt.Errorf("%w; a host declared in the manifest is not known before it is cloned, so pass the full URL", err)
	}
	if dir == "" {
		dir = manifest.RepoName(remote.URL)
	}
	// The config must hold an absolute path: skenv runs from any directory.
	dir, err = filepath.Abs(paths.Expand(env.Home, dir))
	if err != nil {
		return ExitFatal, err
	}
	show := homeShow(env.Home)
	// A config that cannot be updated fails before anything is cloned.
	cfgPath, err := config.Target(env.Home, format)
	if err != nil {
		return ExitFatal, err
	}
	switch _, err := os.Stat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		if dryRun {
			fmt.Fprintf(env.Stdout, "would clone %s into %s and record its skenv file in %s\n", gitx.Mask(repo), show(dir), show(cfgPath))
			return ExitOK, nil
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return ExitFatal, err
		}
		if _, err := env.Git.Run(ctx, "", "clone", "--quiet", remote.URL, dir); err != nil {
			return ExitFatal, fmt.Errorf("clone %s: %w (%s)", gitx.Mask(repo), err, remote.AccessHint())
		}
		fmt.Fprintf(env.Stdout, "cloned %s into %s\n", gitx.Mask(repo), show(dir))
	case err != nil:
		return ExitFatal, err
	default:
		if err := checkoutOf(ctx, env, dir, remote); err != nil {
			return ExitFatal, err
		}
		fmt.Fprintf(env.Stdout, "%s is a working copy of %s already; using it\n", show(dir), gitx.Mask(repo))
	}
	return use(ctx, env, dir, format, dryRun)
}

// checkoutOf refuses dir unless it is the root of a git working copy whose
// origin is remote.
func checkoutOf(ctx context.Context, env Env, dir string, remote manifest.Remote) error {
	show := homeShow(env.Home)
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || !samePath(root, dir) {
		return fmt.Errorf("%s exists and is not a git working copy; pass another <dir>", show(dir))
	}
	origin, err := env.Git.Run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil || manifest.NormalizeURL(origin) != manifest.NormalizeURL(remote.URL) {
		return fmt.Errorf("%s exists and is a working copy of another repository (origin %q); pass another <dir>", show(dir), gitx.Mask(origin))
	}
	return nil
}

// Use records the manifest at path, a skenv file or a directory holding
// one, as "manifest" in the tool config. The working directory never
// selects a manifest by itself: Use is how a checkout becomes the one.
func Use(ctx context.Context, env Env, path, format string, dryRun bool) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	abs, err := filepath.Abs(paths.Expand(env.Home, path))
	if err != nil {
		return ExitFatal, err
	}
	return use(ctx, env, abs, format, dryRun)
}

func use(ctx context.Context, env Env, path, format string, dryRun bool) (int, error) {
	show := homeShow(env.Home)
	file, err := manifest.Locate(path)
	if err != nil {
		return ExitFatal, err
	}
	m, err := manifest.Load(file)
	if err != nil {
		return ExitFatal, err
	}
	cfg, err := config.Load(env.Home)
	if err != nil {
		return ExitFatal, err
	}
	cfgPath, err := config.Target(env.Home, format)
	if err != nil {
		return ExitFatal, err
	}
	replaced := ""
	if prev, ok, _ := cfg.String("manifest"); ok {
		if p, err := manifest.Locate(paths.Expand(env.Home, prev)); err != nil || !samePath(p, file) {
			replaced = prev
		}
	}
	warnOwnPath(ctx, env, m, filepath.Dir(file))
	if dryRun {
		fmt.Fprintf(env.Stdout, "would record %s in %s\n", show(file), show(cfgPath))
		if replaced != "" {
			fmt.Fprintf(env.Stdout, "would replace manifest %s there\n", replaced)
		}
		return ExitOK, nil
	}
	cfgFile, err := config.SetFormat(env.Home, format, "manifest", show(file))
	if err != nil {
		return ExitFatal, fmt.Errorf("record the manifest in %s: %w", show(config.Dir(env.Home)), err)
	}
	fmt.Fprintf(env.Stdout, "manifest %s recorded in %s\n", show(file), show(cfgFile))
	if replaced != "" {
		fmt.Fprintf(env.Stdout, "it replaces manifest %s\n", replaced)
	}
	fmt.Fprintln(env.Stdout, "next steps:")
	fmt.Fprintln(env.Stdout, "  - skenv sync --dry-run    show what sync would change on this machine")
	fmt.Fprintln(env.Stdout, "  - skenv sync              apply it; --adopt backs up and replaces skills installed another way")
	return ExitOK, nil
}

// warnOwnPath warns when an own repository of m is the repository checked
// out at dir but names another path: sync would clone a second working
// copy there, and the skills would be linked from that one.
func warnOwnPath(ctx context.Context, env Env, m *manifest.Manifest, dir string) {
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return
	}
	origin, err := env.Git.Run(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil {
		return
	}
	show := homeShow(env.Home)
	for _, o := range m.Own {
		r, err := m.Hosts.Resolve(o.Repo)
		if err != nil || manifest.NormalizeURL(r.URL) != manifest.NormalizeURL(origin) {
			continue
		}
		if p := paths.Expand(env.Home, o.Path); !samePath(p, root) {
			fmt.Fprintf(env.Stderr, "warning: own %s has path %q, but its checkout is %s: sync would clone a second working copy at %s; set path = %q\n",
				gitx.Mask(o.Repo), o.Path, show(root), show(p), show(root))
		}
	}
}

// samePath reports whether a and b name the same file once symlinks are
// resolved, as git reports them.
func samePath(a, b string) bool {
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return resolve(a) == resolve(b)
}

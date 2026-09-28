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
	defaultDir := dir == ""
	if defaultDir {
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
	switch fi, err := os.Stat(dir); {
	case errors.Is(err, fs.ErrNotExist), err == nil && fi.IsDir() && isEmptyDir(dir):
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
		if defaultDir {
			dir = moveToOwnPath(ctx, env, dir)
		}
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

// moveToOwnPath moves a fresh clone at dir, made without an explicit
// <dir>, to the checkout_dir its manifest names for this repository, when
// nothing is there yet: sync keeps that path up to date, so the manifest
// must live in it. It returns where the clone is now.
func moveToOwnPath(ctx context.Context, env Env, dir string) string {
	file, err := manifest.Locate(dir)
	if err != nil {
		return dir
	}
	m, err := manifest.Load(file)
	if err != nil {
		return dir
	}
	_, elsewhere, own := ownElsewhere(ctx, env, m, dir)
	if len(elsewhere) != 1 {
		return dir
	}
	p := elsewhere[0]
	if _, err := os.Lstat(p); err == nil {
		return dir
	}
	show := homeShow(env.Home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return dir
	}
	if err := os.Rename(dir, p); err != nil {
		return dir
	}
	fmt.Fprintf(env.Stdout, "moved it to %s, the checkout_dir of checkout %s in its manifest\n", show(p), own[0])
	return p
}

// isEmptyDir reports whether dir is a directory without entries; git
// clones into one.
func isEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 0
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

// ownElsewhere returns the checkouts of m that are the repository checked
// out at dir (the same origin) but name another path, resolved, their IDs,
// and the root of that checkout. sync then keeps a second working copy at
// that path up to date and never pulls the checkout at dir, which holds
// the manifest: changes pushed from other machines would not arrive.
func ownElsewhere(ctx context.Context, env Env, m *manifest.Manifest, dir string) (root string, elsewhere []string, own []string) {
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, nil
	}
	origin, err := env.Git.Run(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", nil, nil
	}
	mc, err := machineOf(env, m)
	if err != nil {
		fmt.Fprintf(env.Stderr, "warning: %s; checkout_dir overrides of the machine are not applied\n", err)
		mc = machine{}
	}
	pathOf := checkoutPathOf(env, m, mc)
	for _, c := range m.CheckoutList() {
		r, err := m.Remote(c.Repo)
		if err != nil || manifest.NormalizeURL(r.URL) != manifest.NormalizeURL(origin) {
			continue
		}
		if p := pathOf(c); !samePath(p, root) {
			elsewhere = append(elsewhere, p)
			own = append(own, c.ID)
		}
	}
	return root, elsewhere, own
}

// elsewhereAdvice says what to do about a manifest checkout at root whose
// checkout names path p instead: use the working copy at p when it is
// there, else clone the repository to p.
func elsewhereAdvice(env Env, repo, p string) string {
	show := homeShow(env.Home)
	if _, err := os.Stat(p); err == nil {
		return fmt.Sprintf("use the manifest of that working copy: `skenv use %s`", show(p))
	}
	return fmt.Sprintf("clone the repository there instead: `skenv clone %s %s`", gitx.Mask(repo), show(p))
}

// warnOwnPath warns when the manifest checkout at dir is not the working
// copy that its checkout names.
func warnOwnPath(ctx context.Context, env Env, m *manifest.Manifest, dir string) {
	root, elsewhere, own := ownElsewhere(ctx, env, m, dir)
	show := homeShow(env.Home)
	for i, p := range elsewhere {
		repo := m.Checkouts[own[i]].Repo
		fmt.Fprintf(env.Stderr, "warning: checkout %s has checkout_dir %s, but the manifest is in %s: sync would keep a second working copy at %s "+
			"and never pull this one, so manifest changes from other machines would not arrive; %s\n",
			own[i], show(p), show(root), show(p), elsewhereAdvice(env, repo, p))
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

// manifestElsewhere describes each checkout of the manifest that is the
// repository holding the manifest but names another working copy: sync
// never pulls the manifest checkout then. It returns that checkout and one
// detail per entry.
func (e *Engine) manifestElsewhere() (root string, details []string) {
	root, elsewhere, own := ownElsewhere(e.ctx, e.env, e.m, filepath.Dir(e.manifestPath))
	for i, p := range elsewhere {
		details = append(details, fmt.Sprintf("the manifest is not in the working copy of checkout %s (%s): sync pulls that one and never this checkout, "+
			"so manifest changes from other machines do not arrive; %s", own[i], e.show(p), elsewhereAdvice(e.env, e.m.Checkouts[own[i]].Repo, p)))
	}
	return root, details
}

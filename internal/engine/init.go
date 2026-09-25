package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
)

// ManifestFile is the manifest file name inside the manifest repository.
const ManifestFile = "env.toml"

// Init bootstraps a machine (B1): clone the manifest repository, record the
// manifest path in ~/.config/skenv/config.toml (or the existing config file) and run sync. Without dir the
// repository is cloned into ./<repo> of the current directory, like git
// clone. When the repository is already cloned it only records the path.
func Init(ctx context.Context, env Env, repo, dir string, opts Options) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if repo == "" {
		return ExitFatal, errors.New("usage: skenv init <owner/repo> [--path P]")
	}
	if dir == "" {
		dir = manifest.RepoName(repo)
	}
	// config.toml must hold an absolute path: skenv runs from any directory.
	dir, err := filepath.Abs(paths.Expand(env.Home, dir))
	if err != nil {
		return ExitFatal, err
	}
	manifestPath := filepath.Join(dir, ManifestFile)
	show := func(p string) string { return paths.Collapse(env.Home, p) }
	// An existing config.{yaml,yml,json} is updated in its own format;
	// otherwise config.toml is written.
	cfgFile, err := config.Find(env.Home)
	if err != nil {
		return ExitFatal, err
	}
	if cfgFile == "" {
		cfgFile = config.DefaultFile(env.Home)
	}

	cloned := false
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if opts.DryRun {
			fmt.Fprintf(env.Stdout, "would clone %s into %s, record %s in %s and run sync\n",
				repo, show(dir), show(manifestPath), show(cfgFile))
			return ExitOK, nil
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return ExitFatal, err
		}
		if _, err := env.Git.Run(ctx, "", "clone", "--quiet", manifest.RepoURL(repo), dir); err != nil {
			return ExitFatal, fmt.Errorf("clone %s: %w (check access: ssh key or git credential helper)", repo, err)
		}
		fmt.Fprintf(env.Stdout, "cloned %s into %s\n", repo, show(dir))
		cloned = true
	} else if !env.Git.OK(ctx, dir, "rev-parse", "--is-inside-work-tree") {
		return ExitFatal, fmt.Errorf("%s exists but is not a git working copy; pass --path", show(dir))
	}
	if _, err := manifest.Load(manifestPath); err != nil {
		return ExitFatal, err
	}
	if opts.DryRun {
		fmt.Fprintf(env.Stdout, "would record %s in %s\n", show(manifestPath), show(cfgFile))
		return ExitOK, nil
	}
	if _, err := config.Set(env.Home, config.Manifest, show(manifestPath)); err != nil {
		return ExitFatal, fmt.Errorf("write %s: %w", show(cfgFile), err)
	}
	fmt.Fprintf(env.Stdout, "manifest %s recorded in %s\n", show(manifestPath), show(cfgFile))
	if !cloned {
		fmt.Fprintf(env.Stdout, "repository was already cloned; run `skenv sync` to apply the manifest\n")
		return ExitOK, nil
	}
	opts.Manifest = manifestPath
	e, err := Open(ctx, env, opts)
	if err != nil {
		return ExitFatal, err
	}
	defer e.Close()
	return e.Sync()
}

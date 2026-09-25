package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// NewManifest starts a manifest in the git repository that contains dir
// (`skenv init` without a repository): it adds an [environment] section to
// the skenv file of the repository, or creates skenv.<format> with one,
// and records the file as "manifest" in the tool config. The repository
// itself becomes the first [[environment.own]] entry when its origin is on
// GitHub. An empty manifest is not synced.
//
// It refuses, before writing anything, when the file has [environment]
// already, when its [repo] is public (the manifest is personal), and when
// format disagrees with the existing file. A new tool config takes the
// format of the skenv file; an existing one keeps its own.
func NewManifest(ctx context.Context, env Env, dir, format string, dryRun bool) (int, error) {
	if err := gitx.Available(); err != nil {
		return ExitFatal, err
	}
	if dir == "" {
		dir = "."
	}
	dir = paths.Expand(env.Home, dir)
	if _, err := os.Stat(dir); err != nil {
		return ExitFatal, err
	}
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ExitFatal, fmt.Errorf("%s is not inside a git repository; run `git init` first or pass --dir", dir)
	}
	// git prints the resolved path: collapse it against the resolved home
	// too, so a symlinked home still gives "~/..." in the manifest.
	realHome, err := filepath.EvalSymlinks(env.Home)
	if err != nil {
		realHome = env.Home
	}
	show := func(p string) string {
		if c := paths.Collapse(env.Home, p); c != p {
			return c
		}
		return paths.Collapse(realHome, p)
	}

	existing, err := skenvfile.Find(root)
	if err != nil {
		return ExitFatal, err
	}
	file, err := fileformat.Choose(root, "skenv", existing, format)
	if err != nil {
		return ExitFatal, err
	}
	var data []byte
	if existing != "" {
		if data, err = os.ReadFile(existing); err != nil {
			return ExitFatal, err
		}
		doc, err := skenvfile.Parse(data, filepath.Ext(existing))
		if err != nil {
			return ExitFatal, fmt.Errorf("%s: %w", show(existing), err)
		}
		if doc.Has(skenvfile.Environment) {
			return ExitFatal, fmt.Errorf("%s has [environment] already; `skenv init` without <owner/repo> only starts a new manifest "+
				"(to use this one on a machine: `skenv init <owner>/<repo>`, or --manifest)", show(existing))
		}
		if c, ok, err := harness.ReadRaw(root); err != nil {
			return ExitFatal, err
		} else if ok && c.Visibility == "public" {
			return ExitFatal, fmt.Errorf("%s: [repo] says visibility = \"public\", and a public repository must not carry [environment]: "+
				"the manifest is personal (home paths, host names, which skills you use); start it in a private repository", show(existing))
		}
	}

	// The repository itself is the first own entry when it is on GitHub.
	var own *manifest.Own
	if remote, err := env.Git.Run(ctx, root, "config", "--get", "remote.origin.url"); err == nil {
		if repo, ok := manifest.GitHubRepo(remote); ok {
			own = &manifest.Own{Repo: repo, Path: show(root)}
		}
	}
	out, err := manifest.AddEnvironment(data, filepath.Ext(file), own)
	if err != nil {
		return ExitFatal, fmt.Errorf("%s: %w", show(file), err)
	}
	if out, err = skenvfile.Stamp(out, filepath.Ext(file), true); err != nil {
		return ExitFatal, err
	}

	// A config that cannot be updated fails before the skenv file is
	// written.
	cfg, err := config.Load(env.Home)
	if err != nil {
		return ExitFatal, err
	}
	cfgFormat := ""
	if cfg.Path == "" {
		cfgFormat = fileformat.Of(file)
	}
	cfgPath, err := config.Target(env.Home, cfgFormat)
	if err != nil {
		return ExitFatal, err
	}

	msg := "create " + show(file) + " with [environment]"
	if existing != "" {
		msg = "add [environment] to " + show(file)
	}
	if own != nil {
		msg += ", " + own.Repo + " as its first own repository"
	}
	// The config may name another manifest, which the new one replaces.
	replaced := ""
	if prev, ok, _ := cfg.String("manifest"); ok && paths.Expand(env.Home, prev) != file && paths.Expand(env.Home, prev) != root {
		replaced = prev
	}
	if dryRun {
		fmt.Fprintf(env.Stdout, "would %s\n", msg)
		fmt.Fprintf(env.Stdout, "would record %s in %s\n", show(file), show(cfgPath))
		if replaced != "" {
			fmt.Fprintf(env.Stdout, "would replace manifest %s there\n", replaced)
		}
		return ExitOK, nil
	}
	if err := manifest.WriteFile(file, out); err != nil {
		return ExitFatal, err
	}
	fmt.Fprintln(env.Stdout, msg)
	if replaced != "" {
		fmt.Fprintf(env.Stderr, "note: the config pointed at %s; it now names the new manifest\n", replaced)
	}
	cfgFile, err := config.SetFormat(env.Home, cfgFormat, "manifest", show(file))
	if err != nil {
		return ExitFatal, fmt.Errorf("record the manifest in %s: %w", show(config.Dir(env.Home)), err)
	}
	fmt.Fprintf(env.Stdout, "manifest %s recorded in %s\n", show(file), show(cfgFile))
	fmt.Fprintf(env.Stdout, "next steps:\n")
	if own == nil {
		fmt.Fprintf(env.Stdout, "  - list your skills repositories under environment.own in %s\n", filepath.Base(file))
	}
	fmt.Fprintf(env.Stdout, "  - skenv vendor add <owner/repo> --path <dir>   pin a third-party skill\n")
	fmt.Fprintf(env.Stdout, "  - skenv sync                                  link the skills of the manifest\n")
	fmt.Fprintf(env.Stdout, "  - commit %s; on another machine: skenv init <owner>/<repo>\n", filepath.Base(file))
	return ExitOK, nil
}

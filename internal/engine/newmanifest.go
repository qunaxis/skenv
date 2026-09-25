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
	p, err := planManifest(ctx, env, dir, format)
	if err != nil {
		return ExitFatal, err
	}
	out, err := p.build(p.own)
	if err != nil {
		return ExitFatal, err
	}
	if dryRun {
		p.printPlan(env, p.own)
		return ExitOK, nil
	}
	if err := p.write(env, out, p.own); err != nil {
		return ExitFatal, err
	}
	fmt.Fprintf(env.Stdout, "next steps:\n")
	if p.own == nil {
		fmt.Fprintf(env.Stdout, "  - list your skills repositories under environment.own in %s\n", filepath.Base(p.file))
	}
	fmt.Fprintf(env.Stdout, "  - skenv vendor add <owner/repo> --path <dir>   pin a third-party skill\n")
	fmt.Fprintf(env.Stdout, "  - skenv sync                                  link the skills of the manifest\n")
	fmt.Fprintf(env.Stdout, "  - commit %s; on another machine: skenv init <owner>/<repo>\n", filepath.Base(p.file))
	return ExitOK, nil
}

// manifestPlan is a new manifest checked by planManifest and not written
// yet.
type manifestPlan struct {
	file     string // the skenv file to write
	existing bool   // the file exists (without [environment])
	data     []byte // its content, empty for a new file
	// own is the repository itself when its origin is on GitHub.
	own       *manifest.Own
	cfgFormat string // format of a new tool config, "" to keep the existing one
	cfgPath   string
	replaced  string // the manifest the config named before, if another one
	show      func(string) string
}

// planManifest checks everything NewManifest needs before writing: the git
// repository of dir, its skenv file and the tool config.
func planManifest(ctx context.Context, env Env, dir, format string) (*manifestPlan, error) {
	if err := gitx.Available(); err != nil {
		return nil, err
	}
	if dir == "" {
		dir = "."
	}
	dir = paths.Expand(env.Home, dir)
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	root, err := env.Git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository; run `git init` first or pass --dir", dir)
	}
	p := &manifestPlan{show: homeShow(env.Home)}
	show := p.show

	existing, err := skenvfile.Find(root)
	if err != nil {
		return nil, err
	}
	if p.file, err = fileformat.Choose(root, "skenv", existing, format); err != nil {
		return nil, err
	}
	if existing != "" {
		p.existing = true
		if p.data, err = os.ReadFile(existing); err != nil {
			return nil, err
		}
		doc, err := skenvfile.Parse(p.data, filepath.Ext(existing))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", show(existing), err)
		}
		if doc.Has(skenvfile.Environment) {
			return nil, fmt.Errorf("%s has [environment] already; `skenv init` without <owner/repo> only starts a new manifest "+
				"(to use this one on a machine: `skenv init <owner>/<repo>`, or --manifest)", show(existing))
		}
		if err := refusePublic(p.data, filepath.Ext(existing), show(existing)); err != nil {
			return nil, err
		}
	}

	// The repository itself is the first own entry when it is on GitHub.
	if remote, err := env.Git.Run(ctx, root, "config", "--get", "remote.origin.url"); err == nil {
		if repo, ok := manifest.GitHubRepo(remote); ok {
			p.own = &manifest.Own{Repo: repo, Path: show(root)}
		}
	}

	// A config that cannot be updated fails before the skenv file is
	// written.
	cfg, err := config.Load(env.Home)
	if err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		p.cfgFormat = fileformat.Of(p.file)
	}
	if p.cfgPath, err = config.Target(env.Home, p.cfgFormat); err != nil {
		return nil, err
	}
	// The config may name another manifest, which the new one replaces.
	if prev, ok, _ := cfg.String("manifest"); ok && paths.Expand(env.Home, prev) != p.file && paths.Expand(env.Home, prev) != root {
		p.replaced = prev
	}
	return p, nil
}

// homeShow collapses paths against home and, since git prints resolved
// paths, against the resolved home too: a symlinked home still gives
// "~/..." in the manifest.
func homeShow(home string) func(string) string {
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		realHome = home
	}
	return func(p string) string {
		if c := paths.Collapse(home, p); c != p {
			return c
		}
		return paths.Collapse(realHome, p)
	}
}

// refusePublic refuses to add [environment] to a skenv file whose [repo]
// is public: the manifest is personal.
func refusePublic(data []byte, ext, name string) error {
	c, ok, err := harness.Parse(data, ext)
	if err != nil {
		return err
	}
	if ok && c.Visibility == "public" {
		return fmt.Errorf("%s: [repo] says visibility = \"public\", and a public repository must not carry [environment]: "+
			"the manifest is personal (home paths, host names, which skills you use); start it in a private repository", name)
	}
	return nil
}

// build returns the skenv file with [environment] added, own as its first
// own repository when not nil.
func (p *manifestPlan) build(own *manifest.Own) ([]byte, error) {
	out, err := manifest.AddEnvironment(p.data, filepath.Ext(p.file), own)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.show(p.file), err)
	}
	return skenvfile.Stamp(out, filepath.Ext(p.file), true)
}

func (p *manifestPlan) message(own *manifest.Own) string {
	msg := "create " + p.show(p.file) + " with [environment]"
	if p.existing {
		msg = "add [environment] to " + p.show(p.file)
	}
	if own != nil {
		msg += ", " + own.Repo + " as its first own repository"
	}
	return msg
}

func (p *manifestPlan) printPlan(env Env, own *manifest.Own) {
	fmt.Fprintf(env.Stdout, "would %s\n", p.message(own))
	fmt.Fprintf(env.Stdout, "would record %s in %s\n", p.show(p.file), p.show(p.cfgPath))
	if p.replaced != "" {
		fmt.Fprintf(env.Stdout, "would replace manifest %s there\n", p.replaced)
	}
}

// write writes the skenv file and records it as "manifest" in the tool
// config.
func (p *manifestPlan) write(env Env, out []byte, own *manifest.Own) error {
	if err := manifest.WriteFile(p.file, out); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, p.message(own))
	if p.replaced != "" {
		fmt.Fprintf(env.Stderr, "note: the config pointed at %s; it now names the new manifest\n", p.replaced)
	}
	cfgFile, err := config.SetFormat(env.Home, p.cfgFormat, "manifest", p.show(p.file))
	if err != nil {
		return fmt.Errorf("record the manifest in %s: %w", p.show(config.Dir(env.Home)), err)
	}
	fmt.Fprintf(env.Stdout, "manifest %s recorded in %s\n", p.show(p.file), p.show(cfgFile))
	return nil
}

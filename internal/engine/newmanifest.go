package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// NewManifest starts a manifest in the git repository that contains dir
// (`skenv init`): it adds a [user] section to the skenv file of the
// repository, or creates skenv.<format> with one, and records the file as
// "manifest" in the tool config. The repository itself becomes the first
// checkout, with checkout_dir ".": from its origin, or from remote
// (`--remote`) when it has no origin yet. An empty manifest is not synced.
//
// It refuses, before writing anything, when the file has [user] already,
// when its [repository] is public (the manifest is personal), and when
// format disagrees with the existing file. A new tool config takes the
// format of the skenv file; an existing one keeps its own.
func NewManifest(ctx context.Context, env Env, dir, format, remote string, dryRun bool) (int, error) {
	p, err := planManifest(ctx, env, dir, format, remote)
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
		fmt.Fprintf(env.Stdout, "  - list your skills repositories under user.checkouts in %s\n", filepath.Base(p.file))
	}
	fmt.Fprintf(env.Stdout, "  - skenv vendor add <repo> --path <dir>         pin a third-party skill\n")
	fmt.Fprintf(env.Stdout, "  - skenv sync                                  link the skills of the manifest\n")
	// The checkout is a built-in short form or a URL: both work there.
	again := "<repo>"
	if p.own != nil {
		again = p.own.Repo
	}
	fmt.Fprintf(env.Stdout, "  - commit and push %s; on another machine: skenv clone %s\n", filepath.Base(p.file), again)
	return ExitOK, nil
}

// manifestPlan is a new manifest checked by planManifest and not written
// yet.
type manifestPlan struct {
	file     string // the skenv file to write
	existing bool   // the file exists (without [user])
	data     []byte // its content, empty for a new file
	// own is the repository itself: from origin, or from --remote.
	own       *manifest.Checkout
	cfgFormat string // format of a new tool config, "" to keep the existing one
	cfgPath   string
	replaced  string // the manifest the config named before, if another one
	show      func(string) string
}

// planManifest checks everything NewManifest needs before writing: the git
// repository of dir, its skenv file, remote and the tool config.
func planManifest(ctx context.Context, env Env, dir, format, remote string) (*manifestPlan, error) {
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
		if doc.Has(skenvfile.User) {
			return nil, fmt.Errorf("%s has [user] already; `skenv init` only starts a new manifest "+
				"(to use this one on this machine: `skenv use %s`)", show(existing), show(root))
		}
		if err := refusePublic(p.data, filepath.Ext(existing), show(existing)); err != nil {
			return nil, err
		}
	}

	// The repository itself is the first checkout when its origin, or
	// --remote for a repository without one, is on a network host. Its
	// checkout_dir is ".": the repository that holds the manifest, wherever
	// it is cloned.
	origin, err := env.Git.Run(ctx, root, "config", "--get", "remote.origin.url")
	hasOrigin := err == nil && strings.TrimSpace(origin) != ""
	switch {
	case remote != "" && hasOrigin:
		return nil, errors.New("--remote is for a repository without an origin remote; this one has one, and its own entry comes from it")
	case remote != "":
		repo, err := remoteRepo(remote)
		if err != nil {
			return nil, err
		}
		p.own = &manifest.Checkout{ID: manifest.NewID(repo, nil), Repo: repo, CheckoutDir: "."}
	case hasOrigin:
		if repo, ok := ownRepo(origin); ok {
			p.own = &manifest.Checkout{ID: manifest.NewID(repo, nil), Repo: repo, CheckoutDir: "."}
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

// refusePublic refuses to add [user] to a skenv file whose [repository]
// is public: the manifest is personal.
func refusePublic(data []byte, ext, name string) error {
	c, ok, err := harness.Parse(data, ext)
	if err != nil {
		return err
	}
	if ok && c.Visibility == "public" {
		return fmt.Errorf("%s: [repository] says visibility = \"public\", and a public repository must not carry [user]: "+
			"the manifest is personal (home paths, machine names, which skills you use); start it in a private repository", name)
	}
	return nil
}

// build returns the skenv file with [user] added, own as its first
// checkout when not nil.
func (p *manifestPlan) build(own *manifest.Checkout) ([]byte, error) {
	out, err := manifest.AddUser(p.data, filepath.Ext(p.file), own)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.show(p.file), err)
	}
	return skenvfile.Stamp(out, filepath.Ext(p.file), true)
}

func (p *manifestPlan) message(own *manifest.Checkout) string {
	msg := "create " + p.show(p.file) + " with [user]"
	if p.existing {
		msg = "add [user] to " + p.show(p.file)
	}
	if own != nil {
		msg += ", " + own.Repo + " as its first checkout (" + own.ID + ")"
	}
	return msg
}

func (p *manifestPlan) printPlan(env Env, own *manifest.Checkout) {
	fmt.Fprintf(env.Stdout, "would %s\n", p.message(own))
	fmt.Fprintf(env.Stdout, "would record %s in %s\n", p.show(p.file), p.show(p.cfgPath))
	if p.replaced != "" {
		fmt.Fprintf(env.Stdout, "would replace manifest %s there\n", p.replaced)
	}
}

// write writes the skenv file and records it as "manifest" in the tool
// config.
func (p *manifestPlan) write(env Env, out []byte, own *manifest.Checkout) error {
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

// remoteRepo is the repo value for --remote: a repository on a network
// host in any form a manifest accepts except a declared alias (the new
// manifest declares no hosts yet). Like an origin, it is written in the
// short form on a built-in host and without credentials otherwise.
func remoteRepo(value string) (string, error) {
	r, err := manifest.Hosts(nil).Resolve(value)
	if err != nil {
		return "", fmt.Errorf("--remote: %w", err)
	}
	repo, ok := ownRepo(r.URL)
	if !ok {
		return "", errors.New("--remote must name a repository on a network host, not a local path")
	}
	return repo, nil
}

// ownRepo is the repo value for the origin remote of a new manifest: the
// short form on github.com, gitlab.com and codeberg.org, the URL without
// credentials on any other network host, ok false for a local path. A new
// manifest declares no hosts yet.
func ownRepo(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	if repo, ok := manifest.Hosts(nil).ShortForm(remote); ok {
		return repo, true
	}
	if strings.HasPrefix(manifest.NormalizeURL(remote), "/") {
		return "", false
	}
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", false // it may hold credentials that cannot be removed
		}
		if _, pw := u.User.Password(); u.User != nil && (pw || u.Scheme != "ssh") {
			u.User = nil
		}
		remote = u.String()
	}
	return remote, true
}

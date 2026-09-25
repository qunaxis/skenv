package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/alecthomas/kong"

	"github.com/qunaxis/skenv/internal/buildinfo"
	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/harness"
)

// SPIKE (issue #6): sync, link, vendor and repo are declared with kong; the
// other commands pass their raw arguments to the stdlib flag code.

type manifestOpt struct {
	Manifest string `placeholder:"FILE" help:"path to env.toml"`
}

type dryRunOpt struct {
	DryRun bool `help:"print the plan, change nothing"`
}

type kongCLI struct {
	Version kong.VersionFlag `help:"print the version and exit"`

	Init      legacyCmd  `cmd:"" passthrough:"" help:"clone the manifest repository and sync"`
	Sync      syncCmd    `cmd:"" help:"pull, vendor, link and prune according to the manifest"`
	Link      linkCmd    `cmd:"" help:"create store and agent links"`
	Doctor    legacyCmd  `cmd:"" passthrough:"" help:"compare the machine with the manifest (exit 0/1/2)"`
	Vendor    vendorCmd  `cmd:"" help:"pin, bump or remove third-party skills"`
	Autostart legacyCmd  `cmd:"" passthrough:"" help:"enable|disable|status of the hourly sync"`
	Lint      legacyCmd  `cmd:"" passthrough:"" help:"check skills (L1-L6, P1)"`
	New       legacyCmd  `cmd:"" passthrough:"" help:"scaffold a skill"`
	Repo      repoCmd    `cmd:"" help:"set up and check the harness of a skills repository"`
	VersionC  versionCmd `cmd:"" name:"version" help:"print the version"`
	Help      helpCmd    `cmd:"" hidden:""`
}

// legacyCmd hands everything after the command name to the old dispatch.
type legacyCmd struct {
	Args []string `arg:"" optional:""`
}

func (c *legacyCmd) Run(ctx context.Context, kctx *kong.Context, env engine.Env) error {
	name := kctx.Selected().Name
	cmds := map[string]func(context.Context, engine.Env, []string) (int, error){
		"init": cmdInit, "doctor": cmdDoctor, "autostart": cmdAutostart, "lint": cmdLint, "new": cmdNew,
	}
	return exit(cmds[name](ctx, env, c.Args))
}

type versionCmd struct{}

func (versionCmd) Run(kctx *kong.Context) error {
	fmt.Fprintln(kctx.Stdout, buildinfo.Get())
	return nil
}

type helpCmd struct{}

func (helpCmd) Run(kctx *kong.Context) error {
	root, err := kong.Trace(kctx.Kong, nil)
	if err != nil {
		return err
	}
	return root.PrintUsage(false)
}

type syncCmd struct {
	manifestOpt
	dryRunOpt
	Adopt bool `help:"move conflicting unmanaged paths to ~/.local/state/skenv/backup/<ts>/ and replace them"`
	Quiet bool `help:"print only warnings and errors"`
}

func (syncCmd) Help() string {
	return "Pull own repositories, vendor pinned skills, link everything into the store\nand agent directories, and remove managed paths that left the manifest."
}

func (c *syncCmd) Run(ctx context.Context, env engine.Env) error {
	return withEngine(ctx, env, engine.Options{Manifest: c.Manifest, DryRun: c.DryRun, Adopt: c.Adopt, Quiet: c.Quiet},
		func(e *engine.Engine) (int, error) { return e.Sync() })
}

type linkCmd struct {
	manifestOpt
	dryRunOpt
	Adopt bool `help:"move conflicting unmanaged paths to ~/.local/state/skenv/backup/<ts>/ and replace them"`
}

func (linkCmd) Help() string {
	return "Create store links for own skills and agent links for every skill."
}

func (c *linkCmd) Run(ctx context.Context, env engine.Env) error {
	return withEngine(ctx, env, engine.Options{Manifest: c.Manifest, DryRun: c.DryRun, Adopt: c.Adopt},
		func(e *engine.Engine) (int, error) { return e.Link() })
}

type vendorOpts struct {
	manifestOpt
	dryRunOpt
	Adopt bool `help:"move conflicting unmanaged paths to the backup directory and replace them"`
}

func (o vendorOpts) options() engine.Options {
	return engine.Options{Manifest: o.Manifest, DryRun: o.DryRun, Adopt: o.Adopt}
}

type vendorCmd struct {
	Add    vendorAddCmd    `cmd:"" help:"pin a third-party skill and sync it"`
	Bump   vendorBumpCmd   `cmd:"" help:"move a vendored skill to a new commit"`
	Remove vendorRemoveCmd `cmd:"" help:"remove a vendored skill"`
}

type vendorAddCmd struct {
	vendorOpts
	Repo string `arg:"" name:"owner/repo" help:"GitHub repository"`
	Path string `placeholder:"P" help:"directory with SKILL.md inside the repository (\".\" for the root)"`
	Name string `placeholder:"N" help:"skill name (default: last element of --path)"`
	Rev  string `placeholder:"SHA" help:"commit to pin (default: HEAD of the default branch)"`
}

func (vendorAddCmd) Help() string {
	return "Pin a third-party skill in the manifest (HEAD of the default branch unless\n--rev) and sync it. The manifest change is not committed."
}

func (c *vendorAddCmd) Run(ctx context.Context, env engine.Env) error {
	return withEngine(ctx, env, c.options(), func(e *engine.Engine) (int, error) {
		return e.VendorAdd(engine.VendorAddOptions{Repo: c.Repo, Path: c.Path, Name: c.Name, Rev: c.Rev})
	})
}

type vendorBumpCmd struct {
	vendorOpts
	Name string `arg:"" help:"vendored skill"`
	Rev  string `placeholder:"SHA" help:"commit to pin (default: HEAD of the default branch)"`
}

func (vendorBumpCmd) Help() string {
	return "Move a vendored skill to a new commit (default: HEAD), show the log, sync."
}

func (c *vendorBumpCmd) Run(ctx context.Context, env engine.Env) error {
	return withEngine(ctx, env, c.options(), func(e *engine.Engine) (int, error) { return e.VendorBump(c.Name, c.Rev) })
}

type vendorRemoveCmd struct {
	vendorOpts
	Name string `arg:"" help:"vendored skill"`
}

func (vendorRemoveCmd) Help() string {
	return "Remove a vendored skill from the manifest and its managed paths."
}

func (c *vendorRemoveCmd) Run(ctx context.Context, env engine.Env) error {
	return withEngine(ctx, env, c.options(), func(e *engine.Engine) (int, error) { return e.VendorRemove(c.Name) })
}

type repoDir struct {
	Dir string `default:"." placeholder:"D" help:"repository (any directory inside it)"`
}

type repoWrite struct {
	dryRunOpt
	Force bool `help:"replace existing files that skenv does not manage yet"`
}

type repoCmd struct {
	Init  repoInitCmd  `cmd:"" help:"set up the harness of a skills repository"`
	Apply repoApplyCmd `cmd:"" help:"regenerate the managed files and blocks"`
	Check repoCheckCmd `cmd:"" help:"compare the managed files with the templates (exit 0/1/2)"`
}

type repoInitCmd struct {
	repoDir
	repoWrite
	Visibility string `required:"" enum:"private,public" placeholder:"private|public" help:"private or public"`
}

func (repoInitCmd) Help() string {
	return "Set up the harness of a skills repository: skenv.toml, lefthook.yml, CI\nworkflow, linter configs and the managed blocks of AGENTS.md and .gitignore;\nthen `lefthook install`. Refuses if skenv.toml exists."
}

func (c *repoInitCmd) Run(ctx context.Context, env engine.Env) error {
	return exit(repoInit(ctx, env, c.Dir, c.Visibility, c.DryRun, c.Force))
}

type repoApplyCmd struct {
	repoDir
	repoWrite
	Upgrade bool `help:"move harness to the templates of this skenv"`
}

func (repoApplyCmd) Help() string {
	return "Regenerate the managed files and blocks for the harness version in skenv.toml\n(--upgrade moves it to " + harness.Latest + " first); then `lefthook install`."
}

func (c *repoApplyCmd) Run(ctx context.Context, env engine.Env) error {
	return exit(repoApply(ctx, env, c.Dir, c.Upgrade, c.DryRun, c.Force))
}

type repoCheckCmd struct{ repoDir }

func (repoCheckCmd) Help() string {
	return "Compare the managed files and blocks with the templates of the harness version.\nExit code 0: in sync, 1: drift (files listed), 2: error."
}

func (c *repoCheckCmd) Run(ctx context.Context, env engine.Env) error {
	return exit(repoCheck(ctx, env, c.Dir))
}

// exitError carries a command's exit code through kong's error-only Run.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func exit(code int, err error) error { return exitError{code, err} }

func withEngine(ctx context.Context, env engine.Env, o engine.Options, f func(*engine.Engine) (int, error)) error {
	e, err := engine.Open(ctx, env, o)
	if err != nil {
		return exit(engine.ExitFatal, err)
	}
	defer e.Close()
	return exit(f(e))
}

// kongExit is panicked by kong's Exit hook (--help, --version) and
// recovered in runKong, so Main returns instead of calling os.Exit.
type kongExit int

func runKong(ctx context.Context, args []string, stdout, stderr io.Writer) (code int, err error) {
	var cli kongCLI
	parser, err := kong.New(&cli,
		kong.Name("skenv"),
		kong.Description("skenv keeps agent skills (Claude Code, Codex, pi) in sync with a manifest."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(c int) { panic(kongExit(c)) }),
		kong.Vars{"version": buildinfo.Get().String()},
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.BindToProvider(func() (engine.Env, error) { return newEnv(stdout, stderr) }),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	if err != nil {
		return engine.ExitFatal, err
	}
	defer func() {
		if r := recover(); r != nil {
			c, ok := r.(kongExit)
			if !ok {
				panic(r)
			}
			code, err = int(c), nil
		}
	}()
	if len(args) == 0 {
		parser.Stdout = stderr
		root, _ := kong.Trace(parser, nil)
		_ = root.PrintUsage(false)
		return engine.ExitFatal, nil
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return engine.ExitFatal, err
	}
	err = kctx.Run()
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code, ee.err
	}
	if err != nil {
		return engine.ExitFatal, err
	}
	return engine.ExitOK, nil
}

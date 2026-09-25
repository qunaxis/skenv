package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	ucli "github.com/urfave/cli/v3"

	"github.com/qunaxis/skenv/internal/buildinfo"
	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/harness"
)

// SPIKE (issue #6): sync, link, vendor and repo are declared with
// urfave/cli v3; the other commands pass their raw arguments to the stdlib
// flag code.

// Flags keep parsed state, so every command gets fresh instances.
func manifestFlag3() ucli.Flag {
	return &ucli.StringFlag{Name: "manifest", Usage: "path to the manifest `FILE` (env.toml)"}
}
func dryRunFlag() ucli.Flag {
	return &ucli.BoolFlag{Name: "dry-run", Usage: "print the plan, change nothing"}
}
func forceFlag() ucli.Flag {
	return &ucli.BoolFlag{Name: "force", Usage: "replace existing files that skenv does not manage yet"}
}
func dirFlag() ucli.Flag {
	return &ucli.StringFlag{Name: "dir", Value: ".", Usage: "repository `D` (any directory inside it)"}
}
func revFlag() ucli.Flag {
	return &ucli.StringFlag{Name: "rev", Usage: "commit `SHA` to pin (default: HEAD of the default branch)"}
}

func adoptFlag(usage string) *ucli.BoolFlag { return &ucli.BoolFlag{Name: "adopt", Usage: usage} }

const adoptBackup = "move conflicting unmanaged paths to the backup directory and replace them"

func engineOpts(cmd *ucli.Command) engine.Options {
	return engine.Options{Manifest: cmd.String("manifest"), DryRun: cmd.Bool("dry-run"), Adopt: cmd.Bool("adopt")}
}

// withEngine3 opens the engine and runs f; env comes from the root's Metadata.
func withEngine3(ctx context.Context, cmd *ucli.Command, o engine.Options, f func(*engine.Engine) (int, error)) error {
	if cmd.Args().Len() > 0 {
		return usageErr(cmd, fmt.Sprintf("%s: unexpected argument %q", cmd.FullName(), cmd.Args().First()))
	}
	env, err := envOf(cmd)
	if err != nil {
		return exit(engine.ExitFatal, err)
	}
	e, err := engine.Open(ctx, env, o)
	if err != nil {
		return exit(engine.ExitFatal, err)
	}
	defer e.Close()
	return exit(f(e))
}

func envOf(cmd *ucli.Command) (engine.Env, error) {
	root := cmd.Root()
	return newEnv(root.Writer, root.ErrWriter)
}

func legacy(name, usage string, f func(context.Context, engine.Env, []string) (int, error)) *ucli.Command {
	return &ucli.Command{
		Name: name, Usage: usage, SkipFlagParsing: true, HideHelp: true,
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			env, err := envOf(cmd)
			if err != nil {
				return exit(engine.ExitFatal, err)
			}
			return exit(f(ctx, env, cmd.Args().Slice()))
		},
	}
}

func syncCommand(name string) *ucli.Command {
	c := &ucli.Command{
		Name:        name,
		Usage:       "pull, vendor, link and prune according to the manifest",
		Description: "Pull own repositories, vendor pinned skills, link everything into the store\nand agent directories, and remove managed paths that left the manifest.",
		Flags: []ucli.Flag{manifestFlag3(), dryRunFlag(),
			adoptFlag("move conflicting unmanaged paths to ~/.local/state/skenv/backup/<ts>/ and replace them")},
		Action: func(ctx context.Context, cmd *ucli.Command) error {
			o := engineOpts(cmd)
			o.Quiet = cmd.Bool("quiet")
			return withEngine3(ctx, cmd, o, func(e *engine.Engine) (int, error) {
				if name == "link" {
					return e.Link()
				}
				return e.Sync()
			})
		},
	}
	if name == "sync" {
		c.Flags = append(c.Flags, &ucli.BoolFlag{Name: "quiet", Usage: "print only warnings and errors"})
	} else {
		c.Usage = "create store and agent links"
		c.Description = "Create store links for own skills and agent links for every skill."
	}
	return c
}

func vendorCommand() *ucli.Command {
	common := func() []ucli.Flag { return []ucli.Flag{manifestFlag3(), dryRunFlag(), adoptFlag(adoptBackup)} }
	var repo, name string
	return &ucli.Command{
		Name:  "vendor",
		Usage: "pin, bump or remove third-party skills",
		Commands: []*ucli.Command{
			{
				Name:        "add",
				Usage:       "pin a third-party skill and sync it",
				Description: "Pin a third-party skill in the manifest (HEAD of the default branch unless\n--rev) and sync it. The manifest change is not committed.",
				Arguments:   []ucli.Argument{&ucli.StringArg{Name: "owner/repo", Required: true, Destination: &repo}},
				Flags: append(common(), revFlag(),
					&ucli.StringFlag{Name: "path", Usage: "directory with SKILL.md inside the repository (\".\" for the root)"},
					&ucli.StringFlag{Name: "name", Usage: "skill name (default: last element of --path)"}),
				Action: func(ctx context.Context, cmd *ucli.Command) error {
					return withEngine3(ctx, cmd, engineOpts(cmd), func(e *engine.Engine) (int, error) {
						return e.VendorAdd(engine.VendorAddOptions{Repo: repo, Path: cmd.String("path"), Name: cmd.String("name"), Rev: cmd.String("rev")})
					})
				},
			},
			{
				Name:        "bump",
				Usage:       "move a vendored skill to a new commit",
				Description: "Move a vendored skill to a new commit (default: HEAD), show the log, sync.",
				Arguments:   []ucli.Argument{&ucli.StringArg{Name: "name", Required: true, Destination: &name}},
				Flags:       append(common(), revFlag()),
				Action: func(ctx context.Context, cmd *ucli.Command) error {
					return withEngine3(ctx, cmd, engineOpts(cmd), func(e *engine.Engine) (int, error) { return e.VendorBump(name, cmd.String("rev")) })
				},
			},
			{
				Name:        "remove",
				Usage:       "remove a vendored skill",
				Description: "Remove a vendored skill from the manifest and its managed paths.",
				Arguments:   []ucli.Argument{&ucli.StringArg{Name: "name", Required: true, Destination: &name}},
				Flags:       common(),
				Action: func(ctx context.Context, cmd *ucli.Command) error {
					return withEngine3(ctx, cmd, engineOpts(cmd), func(e *engine.Engine) (int, error) { return e.VendorRemove(name) })
				},
			},
		},
	}
}

func noArgs(cmd *ucli.Command) error {
	if cmd.Args().Len() > 0 {
		return usageErr(cmd, fmt.Sprintf("%s: unexpected argument %q", cmd.FullName(), cmd.Args().First()))
	}
	return nil
}

func repoCommand() *ucli.Command {
	envAnd := func(f func(context.Context, *ucli.Command, engine.Env) (int, error)) ucli.ActionFunc {
		return func(ctx context.Context, cmd *ucli.Command) error {
			if err := noArgs(cmd); err != nil {
				return err
			}
			env, err := envOf(cmd)
			if err != nil {
				return exit(engine.ExitFatal, err)
			}
			return exit(f(ctx, cmd, env))
		}
	}
	return &ucli.Command{
		Name:  "repo",
		Usage: "set up and check the harness of a skills repository",
		Commands: []*ucli.Command{
			{
				Name:        "init",
				Usage:       "set up the harness of a skills repository",
				Description: "Set up the harness of a skills repository: skenv.toml, lefthook.yml, CI\nworkflow, linter configs and the managed blocks of AGENTS.md and .gitignore;\nthen `lefthook install`. Refuses if skenv.toml exists.",
				Flags: []ucli.Flag{dirFlag(), dryRunFlag(), forceFlag(),
					&ucli.StringFlag{Name: "visibility", Required: true, Usage: "`private|public` (required)",
						Validator: func(v string) error {
							if v != "private" && v != "public" {
								return fmt.Errorf("must be private or public, got %q", v)
							}
							return nil
						}}},
				Action: envAnd(func(ctx context.Context, cmd *ucli.Command, env engine.Env) (int, error) {
					return repoInit(ctx, env, cmd.String("dir"), cmd.String("visibility"), cmd.Bool("dry-run"), cmd.Bool("force"))
				}),
			},
			{
				Name:        "apply",
				Usage:       "regenerate the managed files and blocks",
				Description: "Regenerate the managed files and blocks for the harness version in skenv.toml\n(--upgrade moves it to " + harness.Latest + " first); then `lefthook install`.",
				Flags: []ucli.Flag{dirFlag(), dryRunFlag(), forceFlag(),
					&ucli.BoolFlag{Name: "upgrade", Usage: "move harness to " + harness.Latest + " (the templates of this skenv)"}},
				Action: envAnd(func(ctx context.Context, cmd *ucli.Command, env engine.Env) (int, error) {
					return repoApply(ctx, env, cmd.String("dir"), cmd.Bool("upgrade"), cmd.Bool("dry-run"), cmd.Bool("force"))
				}),
			},
			{
				Name:        "check",
				Usage:       "compare the managed files with the templates (exit 0/1/2)",
				Description: "Compare the managed files and blocks with the templates of the harness version.\nExit code 0: in sync, 1: drift (files listed), 2: error.",
				Flags:       []ucli.Flag{dirFlag()},
				Action: envAnd(func(ctx context.Context, cmd *ucli.Command, env engine.Env) (int, error) {
					return repoCheck(ctx, env, cmd.String("dir"))
				}),
			},
		},
	}
}

// onUsageError is per command in urfave (not inherited), so setUsageErrors
// installs it on the whole tree.
func onUsageError(_ context.Context, cmd *ucli.Command, err error, _ bool) error {
	return usageErr(cmd, err.Error())
}

func setUsageErrors(c *ucli.Command) *ucli.Command {
	c.OnUsageError = onUsageError
	for _, sub := range c.Commands {
		setUsageErrors(sub)
	}
	return c
}

func init() {
	// A package-level hook: urfave has no per-command version printer.
	ucli.VersionPrinter = func(cmd *ucli.Command) { fmt.Fprintln(cmd.Root().Writer, buildinfo.Get()) }
}

func newRoot(stdout, stderr io.Writer) *ucli.Command {
	return setUsageErrors(&ucli.Command{
		Name:                  "skenv",
		Usage:                 "keep agent skills (Claude Code, Codex, pi) in sync with a manifest",
		Version:               buildinfo.Get().String(),
		Writer:                stdout,
		ErrWriter:             stderr,
		EnableShellCompletion: true,
		ExitErrHandler:        func(context.Context, *ucli.Command, error) {}, // never os.Exit
		Action: func(_ context.Context, cmd *ucli.Command) error {
			if cmd.Args().Len() > 0 {
				return usageErr(cmd, fmt.Sprintf("unknown command %q", cmd.Args().First()))
			}
			cmd.Writer = cmd.ErrWriter // bare `skenv`: usage on stderr, exit 2
			_ = ucli.ShowAppHelp(cmd)
			return exit(engine.ExitFatal, nil)
		},
		Commands: []*ucli.Command{
			legacy("init", "clone the manifest repository and sync", cmdInit),
			syncCommand("sync"),
			syncCommand("link"),
			legacy("doctor", "compare the machine with the manifest (exit 0/1/2)", cmdDoctor),
			vendorCommand(),
			legacy("autostart", "enable|disable|status of the hourly sync", cmdAutostart),
			legacy("lint", "check skills (L1-L6, P1)", cmdLint),
			legacy("new", "scaffold a skill", cmdNew),
			repoCommand(),
			{Name: "version", Usage: "print the version", Action: func(_ context.Context, cmd *ucli.Command) error {
				fmt.Fprintln(cmd.Root().Writer, buildinfo.Get())
				return nil
			}},
		},
	})
}

// exitError carries a command's exit code through urfave's error-only Action.
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

func usageErr(_ *ucli.Command, msg string) error { return exit(engine.ExitFatal, usageError{msg}) }

func runUrfave(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	root := newRoot(stdout, stderr)
	err := root.Run(ctx, append([]string{"skenv"}, args...))
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code, ee.err
	}
	if err != nil {
		return engine.ExitFatal, err
	}
	return engine.ExitOK, nil
}

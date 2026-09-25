// Package cli parses the skenv command line.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/qunaxis/skenv/internal/autostart"
	"github.com/qunaxis/skenv/internal/buildinfo"
	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/paths"
)

// Main runs skenv with args (without the program name) and returns the
// exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return execute(ctx, &app{stdout: stdout, stderr: stderr}, args)
}

func execute(ctx context.Context, a *app, args []string) int {
	root := newRoot(a)
	root.SetArgs(legacyDashes(root, args))
	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintf(a.stderr, "skenv: %s\n", gitx.Mask(err.Error()))
		if !a.ran {
			// Parse and usage errors never reach a command.
			a.code = engine.ExitFatal
		}
	}
	return a.code
}

// app carries the exit code of the command that ran: cobra only knows
// about errors, skenv has three exit codes.
type app struct {
	stdout, stderr io.Writer
	code           int
	ran            bool
	// probe, when set, replaces every command action: the compatibility
	// test uses it to see what an invocation parses to without running it.
	probe func(cmd *cobra.Command, args []string)
}

// action adapts a skenv command to cobra's RunE.
func (a *app) action(fn func(ctx context.Context, env engine.Env, args []string) (int, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a.ran = true
		if a.probe != nil {
			a.probe(cmd, args)
			return nil
		}
		env, err := newEnv(a.stdout, a.stderr)
		if err != nil {
			a.code = engine.ExitFatal
			return err
		}
		a.code, err = fn(cmd.Context(), env, args)
		return err
	}
}

// Command returns the skenv command tree for the reference generator.
func Command() *cobra.Command {
	root := newRoot(&app{stdout: io.Discard, stderr: io.Discard})
	root.InitDefaultCompletionCmd()
	return root
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "skenv",
		Short: "Keep agent skills (Claude Code, Codex, pi) in sync with a manifest",
		Long: `skenv keeps agent skills (Claude Code, Codex, pi) in sync with a manifest.

Every command that reads the manifest accepts --manifest FILE (also
$SKENV_MANIFEST or "manifest" in ~/.config/skenv/config.toml).
Exit codes: 0 success, 1 problems found, 2 error.`,
		Version:           buildinfo.Get().String(),
		SilenceErrors:     true,
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Args:              cobra.NoArgs,
		// Without a command: usage on stderr and exit 2, as before.
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.ran = true
			a.code = engine.ExitFatal
			cmd.SetOut(a.stderr)
			return cmd.Help()
		},
	}
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError{fmt.Sprintf("%s (see `%s --help`)", err, cmd.CommandPath())}
	})
	root.AddCommand(
		initCmd(a), syncCmd(a, "sync"), syncCmd(a, "link"), doctorCmd(a),
		vendorCmd(a), autostartCmd(a), lintCmd(a), newCmd(a), repoCmd(a),
		&cobra.Command{
			Use:   "version",
			Short: "Print the skenv version",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				a.ran = true
				fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Get())
				return nil
			},
		},
	)
	return root
}

// legacyDashes rewrites single-dash long flags (-quiet, -path=x), which
// Go's flag package accepted, into the double-dash form pflag expects.
// Only names of real flags are rewritten; everything after "--" is left
// alone.
func legacyDashes(root *cobra.Command, args []string) []string {
	long := map[string]bool{"help": true, "version": true}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		visit := func(f *pflag.Flag) { long[f.Name] = true }
		c.LocalFlags().VisitAll(visit)
		c.PersistentFlags().VisitAll(visit)
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i:]...)
		}
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			name, _, _ := strings.Cut(arg[1:], "=")
			if long[name] {
				arg = "-" + arg
			}
		}
		out = append(out, arg)
	}
	return out
}

func newEnv(stdout, stderr io.Writer) (engine.Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return engine.Env{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	host, _ := os.Hostname()
	return engine.Env{Home: home, Getenv: os.Getenv, Hostname: host, Stdout: stdout, Stderr: stderr}, nil
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

// nArgs requires exactly n positional arguments.
func nArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usageError{fmt.Sprintf("%s: expected %d argument(s), got %d (see `%s --help`)", cmd.Name(), n, len(args), cmd.CommandPath())}
		}
		return nil
	}
}

// group is a command that only holds subcommands.
func group(use, short string, subs ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				var names []string
				for _, s := range cmd.Commands() {
					names = append(names, s.Name())
				}
				return usageError{fmt.Sprintf("%s: unknown subcommand %q (%s)", cmd.Name(), args[0], strings.Join(names, ", "))}
			}
			return usageError{fmt.Sprintf("%s: missing subcommand (see `%s --help`)", cmd.Name(), cmd.CommandPath())}
		},
	}
	c.AddCommand(subs...)
	return c
}

func manifestFlag(fs *pflag.FlagSet, o *engine.Options) {
	fs.StringVar(&o.Manifest, "manifest", "", "path to env.toml")
}

func dryRunFlag(fs *pflag.FlagSet, p *bool) {
	fs.BoolVar(p, "dry-run", false, "print the plan, change nothing")
}

func initCmd(a *app) *cobra.Command {
	var o engine.Options
	var dir string
	c := &cobra.Command{
		Use:   "init <owner/repo>",
		Short: "Clone the manifest repository and sync",
		Long: `Clone the manifest repository into --path (default ./<repo> in the current
directory, like git clone), record its env.toml in ~/.config/skenv/config.toml
and run sync. If the repository is already cloned, only the path is recorded.`,
		Args: nArgs(1),
		RunE: a.action(func(ctx context.Context, env engine.Env, args []string) (int, error) {
			return engine.Init(ctx, env, args[0], dir, o)
		}),
	}
	c.Flags().StringVar(&dir, "path", "", "where to clone the repository (default ./<repo>)")
	dryRunFlag(c.Flags(), &o.DryRun)
	c.Flags().BoolVar(&o.Adopt, "adopt", false, "back up and replace unmanaged paths that conflict with the manifest")
	return c
}

func syncCmd(a *app, name string) *cobra.Command {
	var o engine.Options
	c := &cobra.Command{
		Use:   name,
		Short: "Pull, vendor and link every skill of the manifest",
		Long: `Pull own repositories, vendor pinned skills, link everything into the store
and agent directories, and remove managed paths that left the manifest.`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
			e, err := engine.Open(ctx, env, o)
			if err != nil {
				return engine.ExitFatal, err
			}
			defer e.Close()
			if name == "link" {
				return e.Link()
			}
			return e.Sync()
		}),
	}
	if name == "link" {
		c.Short = "Create store and agent links without pulling"
		c.Long = "Create store links for own skills and agent links for every skill."
	}
	manifestFlag(c.Flags(), &o)
	dryRunFlag(c.Flags(), &o.DryRun)
	c.Flags().BoolVar(&o.Adopt, "adopt", false, "move conflicting unmanaged paths to ~/.local/state/skenv/backup/<ts>/ and replace them")
	if name == "sync" {
		c.Flags().BoolVar(&o.Quiet, "quiet", false, "print only warnings and errors")
	}
	return c
}

func doctorCmd(a *app) *cobra.Command {
	o := engine.Options{ReadOnly: true}
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Compare the machine with the manifest",
		Long: `Compare the machine with the manifest without changing it.
Classes: missing, extra-managed, unmanaged, wrong-rev, broken-link, conflict,
dirty, unpushed, behind, agent-mismatch.
Exit code: 0 in sync, 1 discrepancies, 2 error.`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
			e, err := engine.Open(ctx, env, o)
			if err != nil {
				return engine.ExitFatal, err
			}
			defer e.Close()
			return e.Doctor(asJSON)
		}),
	}
	manifestFlag(c.Flags(), &o)
	c.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return c
}

func vendorCmd(a *app) *cobra.Command {
	// open wires the flags every vendor subcommand shares.
	shared := func(c *cobra.Command, o *engine.Options) {
		manifestFlag(c.Flags(), o)
		dryRunFlag(c.Flags(), &o.DryRun)
		c.Flags().BoolVar(&o.Adopt, "adopt", false, "move conflicting unmanaged paths to the backup directory and replace them")
	}
	withEngine := func(o *engine.Options, fn func(e *engine.Engine, args []string) (int, error)) func(*cobra.Command, []string) error {
		return a.action(func(ctx context.Context, env engine.Env, args []string) (int, error) {
			e, err := engine.Open(ctx, env, *o)
			if err != nil {
				return engine.ExitFatal, err
			}
			defer e.Close()
			return fn(e, args)
		})
	}
	revUsage := "commit to pin (default: HEAD of the default branch)"

	var addO engine.Options
	var va engine.VendorAddOptions
	add := &cobra.Command{
		Use:   "add <owner/repo>",
		Short: "Pin a third-party skill in the manifest and sync it",
		Long: `Pin a third-party skill in the manifest (HEAD of the default branch unless
--rev) and sync it. The manifest change is not committed.`,
		Args: nArgs(1),
		RunE: withEngine(&addO, func(e *engine.Engine, args []string) (int, error) {
			va.Repo = args[0]
			return e.VendorAdd(va)
		}),
	}
	shared(add, &addO)
	add.Flags().StringVar(&va.Path, "path", "", `directory with SKILL.md inside the repository ("." for the root)`)
	add.Flags().StringVar(&va.Name, "name", "", "skill name (default: last element of --path)")
	add.Flags().StringVar(&va.Rev, "rev", "", revUsage)

	var bumpO engine.Options
	var rev string
	bump := &cobra.Command{
		Use:   "bump <name>",
		Short: "Move a vendored skill to a new commit",
		Long:  "Move a vendored skill to a new commit (default: HEAD), show the log, sync.",
		Args:  nArgs(1),
		RunE: withEngine(&bumpO, func(e *engine.Engine, args []string) (int, error) {
			return e.VendorBump(args[0], rev)
		}),
	}
	shared(bump, &bumpO)
	bump.Flags().StringVar(&rev, "rev", "", revUsage)

	var rmO engine.Options
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a vendored skill and its managed paths",
		Long:  "Remove a vendored skill from the manifest and its managed paths.",
		Args:  nArgs(1),
		RunE: withEngine(&rmO, func(e *engine.Engine, args []string) (int, error) {
			return e.VendorRemove(args[0])
		}),
	}
	shared(remove, &rmO)

	return group("vendor", "Pin, bump and remove third-party skills", add, bump, remove)
}

func autostartCmd(a *app) *cobra.Command {
	sub := func(action, short string) *cobra.Command {
		return &cobra.Command{
			Use:   action,
			Short: short,
			Args:  nArgs(0),
			RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
				return runAutostart(ctx, env, action)
			}),
		}
	}
	c := group("autostart", "Run `skenv sync --quiet` at login and every hour",
		sub("enable", "Install and load the autostart job"),
		sub("disable", "Unload and remove the autostart job"),
		sub("status", "Show whether the autostart job is installed and loaded (exit 1 if not)"),
	)
	c.Long = "Run `skenv sync --quiet` at login and every hour (macOS LaunchAgent\n" +
		autostart.Label + ", Linux systemd user timer). Log: ~/.local/state/skenv/autostart.log."
	return c
}

func runAutostart(ctx context.Context, env engine.Env, action string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return engine.ExitFatal, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cfg := autostart.Config{
		GOOS: runtime.GOOS,
		Home: env.Home,
		Exe:  exe,
		Log:  paths.Layout{Home: env.Home}.AutostartLog(),
		Path: jobPath(),
		UID:  os.Getuid(),
		Run:  autostart.ExecRunner,
	}
	switch action {
	case "enable":
		if err := cfg.Enable(ctx); err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintf(env.Stdout, "autostart enabled: %s sync --quiet at load and hourly; log %s\n", exe, paths.Collapse(env.Home, cfg.Log))
	case "disable":
		if err := cfg.Disable(ctx); err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintln(env.Stdout, "autostart disabled")
	default:
		st, err := cfg.Status(ctx)
		if err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintf(env.Stdout, "autostart: %s\n", st.Detail)
		if !st.Installed || !st.Loaded {
			return engine.ExitProblems, nil
		}
	}
	return engine.ExitOK, nil
}

// jobPath is the PATH for the autostart job: the directory of the current
// git plus the usual system locations.
func jobPath() string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if git, err := execLookPath("git"); err == nil {
		add(filepath.Dir(git))
	}
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		add(d)
	}
	return strings.Join(dirs, ":")
}

var execLookPath = exec.LookPath

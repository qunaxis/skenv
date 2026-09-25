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
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/paths"
	"github.com/qunaxis/skenv/schemas"
)

// Main runs skenv with args (without the program name) and returns the
// exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr}
	root := newRoot(a)
	root.SetArgs(args)
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
}

// action adapts a skenv command to cobra's RunE.
func (a *app) action(fn func(ctx context.Context, env engine.Env, args []string) (int, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		a.ran = true
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
	return newRoot(&app{stdout: io.Discard, stderr: io.Discard})
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "skenv",
		Short: "Keep agent skills (Claude Code, Codex, pi) in sync with a manifest",
		Long: `skenv keeps agent skills (Claude Code, Codex, pi) in sync with a manifest.

Configuration: a setting comes from, highest first, its flag, the SKENV_<KEY>
environment variable, the config file, the default. The config file is
~/.config/skenv/config.toml, config.yaml, config.yml or config.json (only
one of them); its one key today is "manifest", which --manifest and
$SKENV_MANIFEST override. "skenv init" records it.

The manifest is the [environment] section of a skenv file: skenv.toml (or
skenv.yaml, skenv.yml, skenv.json) in the root of a repository. The same
file holds the harness of a skills repository in its [repo] section.
"manifest" names that file or the directory that holds it.

Exit codes: 0 success, 1 problems found, 2 error.`,
		Example: `# Set up a machine from the manifest repository
skenv init example-org/skills
# Compare the machine with the manifest, then bring it in line
skenv doctor
skenv sync`,
		Version:           buildinfo.Get().String(),
		SilenceErrors:     true,
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Args:              cobra.NoArgs,
		// Without a command: usage on stderr and exit 2.
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
		vendorCmd(a), importCmd(a), autostartCmd(a), lintCmd(a), newCmd(a), repoCmd(a), schemaCmd(a),
		&cobra.Command{
			Use:     "version",
			Short:   "Print the skenv version",
			Example: "skenv version",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				a.ran = true
				fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Get())
				return nil
			},
		},
	)
	addCompletion(root)
	indentExamples(root)
	return root
}

// indentExamples indents every Example by two spaces, like the Usage
// line above it in --help.
func indentExamples(c *cobra.Command) {
	if c.Example != "" {
		c.Example = "  " + strings.ReplaceAll(c.Example, "\n", "\n  ")
	}
	for _, s := range c.Commands() {
		indentExamples(s)
	}
}

// addCompletion adds cobra's `completion` command for the shells skenv
// supports. skenv runs on darwin and linux only, so the PowerShell
// script cobra also generates is dropped rather than offered and left
// untested.
func addCompletion(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() != "completion" {
			continue
		}
		c.Short = "Generate the autocompletion script for bash, zsh or fish"
		c.Long = `Generate the autocompletion script for skenv for bash, zsh or fish.
See each sub-command's help for details on how to use the generated script.`
		c.Args, c.RunE = nil, groupRun
		for _, s := range c.Commands() {
			if s.Name() == "powershell" {
				c.RemoveCommand(s)
				continue
			}
			s.Example = completionExamples[s.Name()]
			c.Example += s.Example + "\n"
		}
		c.Example = strings.TrimSuffix(c.Example, "\n")
	}
}

// completionExamples load the completion script of each shell.
var completionExamples = map[string]string{
	"bash": "source <(skenv completion bash)",
	"zsh":  `skenv completion zsh > "${fpath[1]}/_skenv"`,
	"fish": "skenv completion fish > ~/.config/fish/completions/skenv.fish",
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

// cmdName is the command path without "skenv ", as in "vendor add".
func cmdName(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
}

// nArgs requires exactly n positional arguments.
func nArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return usageError{fmt.Sprintf("%s: expected %d argument(s), got %d (see `%s --help`)", cmdName(cmd), n, len(args), cmd.CommandPath())}
		}
		return nil
	}
}

// group is a command that only holds subcommands.
func group(use, short string, subs ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short, RunE: groupRun}
	c.AddCommand(subs...)
	return c
}

// groupRun rejects a group command run without a known subcommand.
func groupRun(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		var names []string
		for _, s := range cmd.Commands() {
			names = append(names, s.Name())
		}
		return usageError{fmt.Sprintf("%s: unknown subcommand %q (%s)", cmdName(cmd), args[0], strings.Join(names, ", "))}
	}
	return usageError{fmt.Sprintf("%s: missing subcommand (see `%s --help`)", cmdName(cmd), cmd.CommandPath())}
}

// formatFlag adds --format with the completion of its values.
func formatFlag(c *cobra.Command, p *string, usage string) {
	c.Flags().StringVar(p, "format", "", usage)
	_ = c.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(fileformat.Names, cobra.ShellCompDirectiveNoFileComp))
}

func manifestFlag(fs *pflag.FlagSet, o *engine.Options) {
	fs.StringVar(&o.Manifest, "manifest", "", "skenv file with the [environment] section, or its directory")
}

func dryRunFlag(fs *pflag.FlagSet, p *bool) {
	fs.BoolVar(p, "dry-run", false, "print the plan, change nothing")
}

func initCmd(a *app) *cobra.Command {
	var o engine.Options
	var dir, here, format string
	var imp bool
	c := &cobra.Command{
		Use:   "init [<repo>]",
		Short: "Clone the manifest repository and sync, or start a manifest",
		Long: `With <repo>: clone the manifest repository into --path (default
./<repo> in the current directory, like git clone), record its skenv file as
"manifest" in the config file (~/.config/skenv/config.toml unless a YAML or
JSON one exists; a new one is YAML or JSON with --format) and run sync. If the
repository is already cloned, only the path is recorded. <repo> is
owner/repo on github.com, gitlab:group/sub/repo, codeberg:owner/repo or a
full git URL; hosts declared in the manifest are not known before it is
cloned, so a self-hosted repository takes its URL.

Without <repo>: start a manifest in the git repository of the current
directory (or --dir). Its skenv file gets an [environment] section with a
commented skeleton, or skenv.toml is created with one (skenv.yaml or
skenv.json with --format); the repository itself becomes its first own
repository: owner/repo for an origin on github.com, gitlab:... on
gitlab.com, codeberg:... on codeberg.org, the URL (without credentials) on
any other host; a local origin is left out. The file is recorded
as "manifest" in the config file (a new one in the same format), and nothing
is synced. It refuses when the file has [environment] already or its [repo]
is public.

With --import (no <owner/repo>): start the manifest, import the skills
already installed on this machine into it (see "skenv import") and run
"skenv sync --adopt": one command to adopt an existing setup.

An existing file keeps its format: --format that disagrees with it is an
error (exit code 2), and nothing is written.`,
		Example: `# Clone the manifest repository into ./skills, record it and sync
skenv init example-org/skills
# Start a manifest in the git repository of the current directory
skenv init
# Start one with the skills already installed here, and take them over
skenv init --import`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError{fmt.Sprintf("init: expected at most 1 argument, got %d (see `%s --help`)", len(args), cmd.CommandPath())}
			}
			return nil
		},
		RunE: a.action(func(ctx context.Context, env engine.Env, args []string) (int, error) {
			if err := fileformat.Valid(format); err != nil {
				return engine.ExitFatal, usageError{"init: " + err.Error()}
			}
			if len(args) == 0 {
				if dir != "" || o.Adopt {
					return engine.ExitFatal, usageError{"init: --path and --adopt need <repo>; without it, --dir names the repository"}
				}
				if imp {
					return engine.InitImport(ctx, env, here, format, o.DryRun)
				}
				return engine.NewManifest(ctx, env, here, format, o.DryRun)
			}
			if imp {
				return engine.ExitFatal, usageError{"init: --import starts a new manifest, without <owner/repo>; after cloning one, run `skenv import`"}
			}
			if here != "" {
				return engine.ExitFatal, usageError{"init: --dir is for starting a manifest without <repo>; use --path"}
			}
			return engine.Init(ctx, env, args[0], dir, format, o)
		}),
	}
	c.Flags().StringVar(&dir, "path", "", "where to clone the repository (default ./<repo>)")
	c.Flags().StringVar(&here, "dir", "", "without <repo>: the repository to start the manifest in (default: the current one)")
	formatFlag(c, &format, "format of a new file: toml, yaml or json (default toml; an existing file keeps its format)")
	dryRunFlag(c.Flags(), &o.DryRun)
	c.Flags().BoolVar(&o.Adopt, "adopt", false, "back up and replace unmanaged paths that conflict with the manifest")
	c.Flags().BoolVar(&imp, "import", false, "without <owner/repo>: import the installed skills into the new manifest and run sync --adopt")
	return c
}

func importCmd(a *app) *cobra.Command {
	var o engine.Options
	var sync bool
	c := &cobra.Command{
		Use:   "import",
		Short: "Add the skills already installed on this machine to the manifest",
		Long: `Add the skills installed on this machine that the manifest does not have
yet, so adopting skenv on a machine with skills is one command. It reads the
store (~/.agents/skills), the agent directories and the global lock of the
vercel skills CLI, ~/.agents/.skill-lock.json (or
` + "`$XDG_STATE_HOME/skills/.skill-lock.json`" + `):

- A skill of the lock becomes ` + "`[[environment.vendor]]`" + `: repo from source,
  path from skillPath. Its rev is the commit whose tree of that path is the
  skillFolderHash of the lock (a git tree id for GitHub installs), searched
  on the ref of the lock (or the default branch) from updatedAt back. When no commit matches: the commit
  whose files match the installed copy, else HEAD; both are warnings.
- A link into a git working copy becomes ` + "`[[environment.own]]`" + ` (repo from
  its origin, path of the working copy), with ` + "`skills = [...]`" + ` when only
  some of its skills are linked.
- Anything else is reported as unmanaged, with a hint.

~/.claude/skills/synced, skills of Claude Code plugins, layout.ignore
matches and skills in the manifest are skipped. It prints the manifest
diff and writes the manifest in place, keeping comments; a skenv file
without [environment] gets one, as "skenv init" adds it. The skills now in
the manifest leave the lock, so ` + "`npx skills update`" + ` does not overwrite them;
a copy of the lock goes to ~/.local/state/skenv/backup/<ts>/. A second run
imports nothing.

The installed copies stay until ` + "`skenv sync --adopt`" + ` (or --sync) backs them up
and replaces them. The manifest change is not committed.`,
		Example: `# Show the manifest diff and the lock changes, write nothing
skenv import --dry-run
# Write the manifest and clean the lock of the skills CLI
skenv import`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
			return engine.Import(ctx, env, o, sync)
		}),
	}
	manifestFlag(c.Flags(), &o)
	dryRunFlag(c.Flags(), &o.DryRun)
	c.Flags().BoolVar(&sync, "sync", false, "run skenv sync --adopt after the import")
	return c
}

func syncCmd(a *app, name string) *cobra.Command {
	var o engine.Options
	c := &cobra.Command{
		Use:   name,
		Short: "Pull, vendor and link every skill of the manifest",
		Long: `Pull own repositories, vendor pinned skills, link everything into the store
and agent directories, and remove managed paths that left the manifest.`,
		Example: `# Show what a sync would change
skenv sync --dry-run
# Pull, vendor and link
skenv sync`,
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
		c.Example = "# Recreate a link removed by hand, without pulling\nskenv link"
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
		Example: `# The machine matches the manifest
skenv doctor
# A skill link was removed by hand
skenv doctor`,
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
		Use:   "add <repo>",
		Short: "Pin a third-party skill in the manifest and sync it",
		Long: `Pin a third-party skill in the manifest (HEAD of the default branch unless
--rev) and sync it. The manifest change is not committed.

<repo> is written to the manifest as given: owner/repo on github.com,
gitlab:group/sub/repo, codeberg:owner/repo, <alias>:path of a host declared
under [environment.hosts.<alias>], or a full git URL. An unknown prefix is
an error. See https://qunaxis.github.io/skenv/git-hosts`,
		Example: `# Pin the skill in tools/release-notes/ at HEAD of the default branch
skenv vendor add example-vendor/tools --path tools/release-notes
# A skill from a GitLab subgroup
skenv vendor add gitlab:example-org/team/tools --path release-notes
# A skill from a self-hosted host declared as "work" in the manifest
skenv vendor add work:platform/skills --path deploy`,
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

	var updateO engine.Options
	var rev string
	update := &cobra.Command{
		Use:     "update [name...]",
		Aliases: []string{"upgrade"},
		Short:   "Move vendored skills to a new commit",
		Long: `Move vendored skills to a new commit and sync them: the named ones, or every
vendored skill without names. Each goes to HEAD of its default branch;
--rev pins a single named skill. Shows the log of the skill's path.`,
		Example: `# Move diagrams to HEAD of the default branch of its repository
skenv vendor update diagrams
# Update every vendored skill
skenv vendor update`,
		Args: func(cmd *cobra.Command, args []string) error {
			if rev != "" && len(args) != 1 {
				return usageError{fmt.Sprintf("%s: --rev needs exactly 1 name, got %d (see `%s --help`)", cmdName(cmd), len(args), cmd.CommandPath())}
			}
			return nil
		},
		RunE: withEngine(&updateO, func(e *engine.Engine, args []string) (int, error) {
			return e.VendorUpdate(args, rev)
		}),
	}
	shared(update, &updateO)
	update.Flags().StringVar(&rev, "rev", "", revUsage)

	var rmO engine.Options
	remove := &cobra.Command{
		Use:     "remove <name>",
		Short:   "Remove a vendored skill and its managed paths",
		Long:    "Remove a vendored skill from the manifest and its managed paths.",
		Example: "skenv vendor remove diagrams",
		Args:    nArgs(1),
		RunE: withEngine(&rmO, func(e *engine.Engine, args []string) (int, error) {
			return e.VendorRemove(args[0])
		}),
	}
	shared(remove, &rmO)

	c := group("vendor", "Pin, update and remove third-party skills", add, update, remove)
	c.Example = "skenv vendor add example-vendor/tools --path tools/release-notes\n" +
		"skenv vendor update\n" +
		"skenv vendor remove diagrams"
	return c
}

func schemaCmd(a *app) *cobra.Command {
	kinds := map[string]string{"skenv": schemas.Skenv, "config": schemas.Config}
	return &cobra.Command{
		Use:   "schema [skenv|config]",
		Short: "Print the JSON Schema of the skenv file or the tool config",
		Long: `Print the JSON Schema of this skenv version to stdout: "skenv" (default) for
the skenv file (skenv.toml, .yaml, .yml or .json with [repo] and
[environment]), "config" for the tool config ~/.config/skenv/config.*.

Files that skenv writes name their schema in a directive, so most editors
need no setup. Use this for offline work or a custom mapping, for example a
JSON Schema mapping in JetBrains IDEs or a rule in .taplo.toml. The same
schemas are published at ` + schemas.Base + `.`,
		Example:   "skenv schema > skenv.schema.json\nskenv schema config",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"skenv", "config"},
		RunE: a.action(func(_ context.Context, env engine.Env, args []string) (int, error) {
			kind := "skenv"
			if len(args) == 1 {
				kind = args[0]
			}
			name, ok := kinds[kind]
			if !ok {
				return engine.ExitFatal, usageError{fmt.Sprintf("schema: unknown schema %q (skenv or config)", kind)}
			}
			data, _ := schemas.Stamped(name, schemas.Running())
			_, err := env.Stdout.Write(data)
			return engine.ExitOK, err
		}),
	}
}

func autostartCmd(a *app) *cobra.Command {
	sub := func(action, short string) *cobra.Command {
		return &cobra.Command{
			Use:     action,
			Short:   short,
			Example: "skenv autostart " + action,
			Args:    nArgs(0),
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
	c.Example = "skenv autostart enable\nskenv autostart status\nskenv autostart disable"
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

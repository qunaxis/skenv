// Package cli parses the skenv command line.
package cli

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/qunaxis/skenv/internal/features/skills"
	"github.com/qunaxis/skenv/internal/platform/buildinfo"
	"github.com/qunaxis/skenv/internal/platform/docedit"
	"github.com/qunaxis/skenv/internal/platform/gitx"
	"github.com/qunaxis/skenv/internal/platform/paths"
)

//go:embed help/*.md
var helpFS embed.FS

// help is the Long text of a command, from help/<name>.md (the command path
// without "skenv", spaces replaced by "_"; "root" for skenv itself). It
// panics on a missing file: every test that builds the command tree
// (Command, Main) catches that at startup, before the text ever ships.
func help(name string) string {
	b, err := helpFS.ReadFile("help/" + name + ".md")
	if err != nil {
		panic("cli: " + err.Error())
	}
	return strings.TrimSuffix(string(b), "\n")
}

// The help lists commands in the order they are added, by task, not
// alphabetically.
func init() { cobra.EnableCommandSorting = false }

// Main runs skenv with args (without the program name) and returns the
// exit code.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr}
	root := newRoot(a)
	root.SetIn(stdin)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintf(a.stderr, "skenv: %s\n", gitx.Mask(err.Error()))
		// The one place an error becomes an exit code: commands return
		// 0 with an error, parse and usage errors never set a code, and
		// both exit 2. A command that found problems keeps its 1.
		if a.code == skills.ExitOK {
			a.code = skills.ExitFatal
		}
	}
	return a.code
}

// app carries the exit code of the command that ran: cobra only knows
// about errors, skenv has three exit codes.
type app struct {
	stdout, stderr io.Writer
	code           int
}

// action adapts a skenv command to cobra's RunE.
func (a *app) action(fn func(ctx context.Context, env skills.Env, args []string) (int, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		env, err := newEnv(a.stdout, a.stderr)
		if err != nil {
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
		Short: "Install agent skills (Claude Code, Codex, pi) from a manifest in git",
		Long:  help("root"),
		Example: `# Set up a machine from the manifest repository
skenv clone example-org/skills
skenv sync
# See what is installed, then check the machine
skenv list
skenv doctor`,
		Version:           buildinfo.Get().String(),
		SilenceErrors:     true,
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Args:              cobra.NoArgs,
		RunE:              groupRun,
	}
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w (see `%s --help`)", err, cmd.CommandPath())
	})
	root.AddGroup(
		&cobra.Group{ID: groupStart, Title: "Get started:"},
		&cobra.Group{ID: groupEveryday, Title: "Everyday:"},
		&cobra.Group{ID: groupAuthor, Title: "Write skills:"},
		&cobra.Group{ID: groupMachine, Title: "Machine:"},
	)
	addTo(root, groupStart, initCmd(a), cloneCmd(a), useCmd(a), importCmd(a))
	addTo(root, groupEveryday, syncCmd(a, "sync"), listCmd(a), doctorCmd(a), vendorCmd(a), syncCmd(a, "link"))
	addTo(root, groupAuthor, newCmd(a), lintCmd(a), repoCmd(a))
	addTo(root, groupMachine, configCmd(a), autostartCmd(a), schemaCmd(a), &cobra.Command{
		Use:     "version",
		Short:   "Print the skenv version",
		Example: "skenv version",
		Args:    nArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Get())
			return nil
		},
	})
	root.SetHelpCommandGroupID(groupMachine)
	root.SetCompletionCommandGroupID(groupMachine)
	addCompletion(root)
	indentExamples(root)
	return root
}

// The command groups of the root help, by task.
const (
	groupStart    = "start"
	groupEveryday = "everyday"
	groupAuthor   = "author"
	groupMachine  = "machine"
)

// addTo adds cmds to root in the help group id.
func addTo(root *cobra.Command, id string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = id
		root.AddCommand(c)
	}
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
		c.Long = help("completion")
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

func newEnv(stdout, stderr io.Writer) (skills.Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return skills.Env{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	host, _ := os.Hostname()
	return skills.Env{Home: home, Getenv: os.Getenv, Hostname: host, Stdout: stdout, Stderr: stderr}, nil
}

// cmdName is the command path without "skenv ", as in "vendor add".
func cmdName(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
}

// nArgs requires exactly n positional arguments.
func nArgs(n int) cobra.PositionalArgs { return rangeArgs(n, n) }

// rangeArgs requires min to max positional arguments. The error names the
// missing or the unexpected argument, with the usage line and an example:
// a count alone does not say what to type.
func rangeArgs(minimum, maximum int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case len(args) < minimum:
			// The Use line names the arguments in order: "add <repo>".
			return argError(cmd, "missing "+strings.Fields(cmd.Use)[1+len(args)])
		case len(args) > maximum:
			return argError(cmd, fmt.Sprintf("unexpected argument %q", args[maximum]))
		}
		return nil
	}
}

func argError(cmd *cobra.Command, problem string) error {
	msg := fmt.Sprintf("%s: %s\nUsage: %s", cmdName(cmd), problem, cmd.UseLine())
	for line := range strings.SplitSeq(cmd.Example, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			msg += "\nExample: " + line //nolint:modernize // loop breaks after one append
			break
		}
	}
	return errors.New(msg)
}

// group is a command that only holds subcommands.
func group(use, short string, subs ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short, RunE: groupRun}
	c.AddCommand(subs...)
	return c
}

// groupRun prints the help of a group run without a subcommand, which
// lists its subcommands, and rejects an unknown one.
func groupRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	var names []string
	for _, s := range cmd.Commands() {
		names = append(names, s.Name())
	}
	return fmt.Errorf("%s: unknown subcommand %q (%s)", cmdName(cmd), args[0], strings.Join(names, ", "))
}

// formatFlag adds --format with the completion of its values.
func formatFlag(c *cobra.Command, p *string, usage string) {
	c.Flags().StringVar(p, "format", "", usage)
	_ = c.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(docedit.Formats, cobra.ShellCompDirectiveNoFileComp))
}

// projectFlag adds --project, the scope of a command: the [project]
// section of the current repository instead of the manifest.
func projectFlag(fs *pflag.FlagSet, p *bool, usage string) {
	fs.BoolVar(p, "project", false, usage)
}

// projectScope returns the skenv file whose [project] section the command
// works on, "" for the manifest. --project requires one; --manifest rules
// one out; auto (sync and doctor) picks the project when the git root of
// the current directory has a skenv file with [project].
func projectScope(ctx context.Context, env skills.Env, cmd string, project, auto bool, manifestFlag string) (string, error) {
	if project && manifestFlag != "" {
		return "", errors.New(cmd + ": --project and --manifest exclude each other")
	}
	if manifestFlag != "" || (!project && !auto) {
		return "", nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	file, err := skills.FindProject(ctx, env, cwd)
	if err != nil {
		if project {
			return "", err
		}
		// A broken skenv file in some repository must not stop the
		// machine's sync; say why the project was not used.
		fmt.Fprintf(env.Stderr, "warning: not a project, using the manifest: %s\n", gitx.Mask(err.Error()))
		return "", nil
	}
	if project && file == "" {
		return "", fmt.Errorf("--project: no skenv file with a [project] section at the root of the git repository of %s", paths.Collapse(env.Home, cwd))
	}
	return file, nil
}

func manifestFlag(fs *pflag.FlagSet, o *skills.Options) {
	fs.StringVar(&o.Manifest, "manifest", "", "skenv file with the [user] section, or its directory")
}

// Usages of --dry-run: what a preview still does differs by command, and
// the flag says so rather than promising that nothing happens.
const (
	dryRunPlain = "print the plan; write nothing"
	dryRunFetch = "print the plan; write nothing except the clone cache ~/.cache/skenv/repos, fetched to resolve commits"
	dryRunSync  = "print the plan; write and pull nothing, so the plan uses the working copies as they are now"
)

func dryRunFlag(fs *pflag.FlagSet, p *bool, usage string) {
	fs.BoolVar(p, "dry-run", false, usage)
}

// scope is the manifest or a project: what sync, doctor and the vendor
// commands call on either.
type scope interface {
	Sync() (int, error)
	Doctor(asJSON bool) (int, error)
	VendorAdd(skills.VendorAddOptions) (int, error)
	VendorUpdate(names []string, rev string) (int, error)
	VendorRemove(name string) (int, error)
	Close()
}

// withScope opens the scope of cmd (see projectScope), runs fn on it and
// closes it.
func withScope(ctx context.Context, env skills.Env, o skills.Options, cmd string, project, auto bool, fn func(scope) (int, error)) (int, error) {
	file, err := projectScope(ctx, env, cmd, project, auto, o.Manifest)
	if err != nil {
		return 0, err
	}
	if file == "" {
		return withUser(ctx, env, o, func(e *skills.UserScope) (int, error) { return fn(e) })
	}
	e, err := skills.OpenProject(ctx, env, o, file)
	if err != nil {
		return 0, err
	}
	defer e.Close()
	return fn(e)
}

// withUser opens the manifest, runs fn on it and closes it.
func withUser(ctx context.Context, env skills.Env, o skills.Options, fn func(*skills.UserScope) (int, error)) (int, error) {
	e, err := skills.OpenUser(ctx, env, o)
	if err != nil {
		return 0, err
	}
	defer e.Close()
	return fn(e)
}

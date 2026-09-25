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

// The help lists commands in the order they are added, by task, not
// alphabetically.
func init() { cobra.EnableCommandSorting = false }

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
		Short: "Install agent skills (Claude Code, Codex, pi) from a manifest in git",
		Long: `skenv installs agent skills (Claude Code, Codex, pi) from a manifest you
keep in git, and keeps every machine in line with it. The manifest is the
[environment] section of skenv.toml in a git repository: your own skills
come from editable git working copies ("own" repositories), third-party
skills are copies pinned to a commit ("vendor" entries).

First steps, by situation:
- No manifest yet: ` + "`skenv init`" + ` in a git repository starts one.
- Skills already installed (npx skills, copies): ` + "`skenv init --import`" + `
  starts one and takes them over.
- Another machine: ` + "`skenv init <repo>`" + ` clones your manifest repository
  and syncs it.

Then ` + "`skenv vendor add <repo>`" + ` installs a third-party skill, ` + "`skenv sync`" + `
applies the manifest and ` + "`skenv doctor`" + ` checks the machine.

In a project repository whose skenv file has a [project] section, sync and
doctor work on the skills of the project instead. The manifest location and
the config file: https://qunaxis.github.io/skenv/configuration and
"skenv schema config".

Exit codes: 0 success, 1 problems found, 2 error. Warnings do not change
the exit code; "skenv doctor" exits 0 only when the machine matches.`,
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
		RunE:              groupRun,
	}
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError{fmt.Sprintf("%s (see `%s --help`)", err, cmd.CommandPath())}
	})
	root.AddGroup(
		&cobra.Group{ID: groupStart, Title: "Get started:"},
		&cobra.Group{ID: groupEveryday, Title: "Everyday:"},
		&cobra.Group{ID: groupAuthor, Title: "Write skills:"},
		&cobra.Group{ID: groupMachine, Title: "Machine:"},
	)
	addTo(root, groupStart, initCmd(a), importCmd(a))
	addTo(root, groupEveryday, syncCmd(a, "sync"), doctorCmd(a), vendorCmd(a), syncCmd(a, "link"))
	addTo(root, groupAuthor, newCmd(a), lintCmd(a), repoCmd(a))
	addTo(root, groupMachine, autostartCmd(a), schemaCmd(a), &cobra.Command{
		Use:     "version",
		Short:   "Print the skenv version",
		Example: "skenv version",
		Args:    nArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.ran = true
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
	for _, line := range strings.Split(cmd.Example, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			msg += "\nExample: " + line
			break
		}
	}
	return usageError{msg}
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
	return usageError{fmt.Sprintf("%s: unknown subcommand %q (%s)", cmdName(cmd), args[0], strings.Join(names, ", "))}
}

// formatFlag adds --format with the completion of its values.
func formatFlag(c *cobra.Command, p *string, usage string) {
	c.Flags().StringVar(p, "format", "", usage)
	_ = c.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(fileformat.Names, cobra.ShellCompDirectiveNoFileComp))
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
func projectScope(ctx context.Context, env engine.Env, cmd string, project, auto bool, manifestFlag string) (string, error) {
	if project && manifestFlag != "" {
		return "", usageError{cmd + ": --project and --manifest exclude each other"}
	}
	if manifestFlag != "" || (!project && !auto) {
		return "", nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	file, err := engine.FindProject(ctx, env, cwd)
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

func manifestFlag(fs *pflag.FlagSet, o *engine.Options) {
	fs.StringVar(&o.Manifest, "manifest", "", "skenv file with the [environment] section, or its directory")
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

func initCmd(a *app) *cobra.Command {
	var o engine.Options
	var dir, here, format, remote string
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
any other host; a local origin is left out. A repository without an origin
yet names its future remote with --remote (owner/repo, gitlab:group/repo,
codeberg:owner/repo or a full URL), written the same way; with an origin,
--remote is an error. It does not set up [repo]: "skenv repo init" does, and
picks the CI system from the host of origin. The file is recorded
as "manifest" in the config file (a new one in the same format), and nothing
is synced. It refuses when the file has [environment] already or its [repo]
is public.

With --import (no <owner/repo>): start the manifest, import the skills
already installed on this machine into it (see "skenv import") and run
"skenv sync --adopt": one command to adopt an existing setup.

An existing file keeps its format: --format that disagrees with it is an
error (exit code 2), and nothing is written.

- Reads: with <repo>, the repository; without, the git repository of the
  current directory (or --dir), its origin and skenv file; with --import,
  the installed skills and the lock of the skills CLI.
- Changes: the skenv file and "manifest" in the config file; with <repo>,
  the new working copy and what sync changes; with --import, the lock of the
  skills CLI and what ` + "`skenv sync --adopt`" + ` changes.
- Network: with <repo>, git clone and the fetches of sync; without, none;
  --import fetches the repositories of the installed skills.
- Conflicts: sync reports an unmanaged path in the way as an error; --adopt
  and --import move it to ~/.local/state/skenv/backup/<ts>/ and replace it.
- Preview: --dry-run clones nothing, so with <repo> it cannot show what sync
  would change.
- Next: "skenv doctor"; commit and push the skenv file so that your other
  machines get it.`,
		Example: `# Clone the manifest repository into ./skills, record it and sync
skenv init example-org/skills
# Start a manifest in the git repository of the current directory
skenv init
# Start one with the skills already installed here, and take them over
skenv init --import
# Start one in a repository without an origin yet, to be pushed to gitlab.com
skenv init --remote gitlab:example-group/my-skills`,
		Args: rangeArgs(0, 1),
		RunE: a.action(func(ctx context.Context, env engine.Env, args []string) (int, error) {
			if err := fileformat.Valid(format); err != nil {
				return engine.ExitFatal, usageError{"init: " + err.Error()}
			}
			if len(args) == 0 {
				if dir != "" || o.Adopt {
					return engine.ExitFatal, usageError{"init: --path and --adopt need <repo>; without it, --dir names the repository"}
				}
				if imp {
					return engine.InitImport(ctx, env, here, format, remote, o.DryRun)
				}
				return engine.NewManifest(ctx, env, here, format, remote, o.DryRun)
			}
			if remote != "" {
				return engine.ExitFatal, usageError{"init: --remote is for starting a manifest without <repo>; <repo> is the remote"}
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
	c.Flags().StringVar(&remote, "remote", "", "without <repo>, for a repository without origin: its future remote, recorded as its own entry (owner/repo, gitlab:group/repo, codeberg:owner/repo or a URL)")
	formatFlag(c, &format, "format of a new file: toml, yaml or json (default toml; an existing file keeps its format)")
	dryRunFlag(c.Flags(), &o.DryRun, dryRunFetch)
	c.Flags().BoolVar(&o.Adopt, "adopt", false, "back up and replace unmanaged paths that conflict with the manifest")
	c.Flags().BoolVar(&imp, "import", false, "without <owner/repo>: import the installed skills into the new manifest and run sync --adopt")
	return c
}

func importCmd(a *app) *cobra.Command {
	var o engine.Options
	var sync, project bool
	c := &cobra.Command{
		Use:   "import",
		Short: "Add the skills already installed on this machine, or in a project, to the skenv file",
		Long: `Add the skills installed on this machine that the manifest does not have
yet, so adopting skenv on a machine with skills is one command. It reads the
store (~/.agents/skills), the agent directories and the global lock of the
vercel skills CLI, ~/.agents/.skill-lock.json (or
` + "`$XDG_STATE_HOME/skills/.skill-lock.json`" + `):

- A skill of the lock becomes ` + "`[[environment.vendor]]`" + `: repo from source,
  path from skillPath. Its rev is the commit whose tree of that path is the
  skillFolderHash of the lock (a git tree id for GitHub installs, a sha256
  of its files otherwise), searched
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
and replaces them. The manifest change is not committed.

With --project: the same for the git repository of the current directory
and the skills-lock.json of the skills CLI in its root. Each skill of the
lock becomes ` + "`[[project.vendor]]`" + ` of the repository's skenv file (a
skenv file without [project] gets one, a repository without a skenv file a
skenv.toml). Its rev is the newest commit whose skill folder has the
computedHash of the lock (a sha256 of the folder's files, recomputed per
commit); when none does, the commit whose files match the installed copy,
else HEAD, both warnings. Project-own skills in dir, the mirrors,
.agents/skills, .claude/skills and .pi/skills are reported, and one that is in several
of them with different files is a warning: pick the version to keep before
sync mirrors dir; import never removes one. The imported entries leave
skills-lock.json (the file goes when none are left), after a copy to the
backup directory.

- Reads: the manifest (or [project]), the store and agent directories, the
  lock of the skills CLI.
- Changes: the manifest (or [project]) and the lock, after a backup; with
  --sync, what ` + "`skenv sync --adopt`" + ` changes.
- Network: fetches the repository of each skill of the lock into the clone
  cache ~/.cache/skenv/repos to find its commit.
- Conflicts: none without --sync; with it, installed copies are backed up to
  ~/.local/state/skenv/backup/<ts>/ and replaced.
- Preview: --dry-run writes nothing except the clone cache.
- Next: ` + "`skenv sync --adopt`" + ` to take the installed copies over, then commit
  the skenv file.`,
		Example: `# Show the manifest diff and the lock changes, write nothing
skenv import --dry-run
# Write the manifest and clean the lock of the skills CLI
skenv import
# In a project: pin the skills of its skills-lock.json in [project]
skenv import --project`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
			if project {
				if o.Manifest != "" {
					return engine.ExitFatal, usageError{"import: --project and --manifest exclude each other"}
				}
				cwd, err := os.Getwd()
				if err != nil {
					return engine.ExitFatal, err
				}
				return engine.ImportProject(ctx, env, cwd, o.DryRun, sync)
			}
			return engine.Import(ctx, env, o, sync)
		}),
	}
	manifestFlag(c.Flags(), &o)
	dryRunFlag(c.Flags(), &o.DryRun, dryRunFetch)
	projectFlag(c.Flags(), &project, "import the skills-lock.json of the current repository into its [project] section")
	c.Flags().BoolVar(&sync, "sync", false, "run skenv sync --adopt after the import")
	return c
}

func syncCmd(a *app, name string) *cobra.Command {
	var o engine.Options
	var project bool
	c := &cobra.Command{
		Use:   name,
		Short: "Apply the manifest to this machine, or sync a project",
		Long: `Apply the manifest: pull the own repositories (editable git working copies
of your skills), copy each pinned third-party skill at its commit, link
everything into the store and the agent directories, and remove managed
paths that left the manifest.

In a project, a git repository whose skenv file has a [project] section
(checked at the root of the repository of the current directory), sync
works on the project instead: it copies every pinned skill of [project]
into its dir at its rev, removes copies whose entry is gone, and gives
every skill of dir to each mirror. It changes a skill authored in dir only
with --adopt, after a backup.
--manifest syncs the machine from there; --project requires a project.

Exit code 0 even with warnings: an own repository with uncommitted changes
or a diverged branch is left as it is, with a warning, and the rest is
synced. ` + "`skenv doctor`" + ` exits 0 only when the machine matches the manifest.
With --dry-run nothing is pulled, so the plan uses the own repositories (and
a manifest inside one) as they are now.

- Reads: the manifest, the own working copies, the store (~/.agents/skills),
  the agent directories and the state file ~/.local/state/skenv/state.json.
- Changes: the own working copies (clone, pull --ff-only), the store, the
  agent links and the state file; in a project, its dir and mirrors.
- Network: git clone and pull of own repositories, fetches of pinned skills
  into the clone cache ~/.cache/skenv/repos.
- Conflicts: an unmanaged path in the way is an error and stays; --adopt
  moves it to ~/.local/state/skenv/backup/<ts>/ and replaces it.
- Preview: --dry-run writes and pulls nothing, so upstream changes are not
  in the plan.
- Next: "skenv doctor".`,
		Example: `# Show what a sync would change
skenv sync --dry-run
# Pull, vendor and link
skenv sync
# In a project: copy its pinned skills and update the mirrors
skenv sync --project`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
			if name == "sync" {
				file, err := projectScope(ctx, env, name, project, true, o.Manifest)
				if err != nil {
					return engine.ExitFatal, err
				}
				if file != "" {
					e, err := engine.OpenProject(ctx, env, o, file)
					if err != nil {
						return engine.ExitFatal, err
					}
					defer e.Close()
					return e.Sync()
				}
			}
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
		c.Long = `Create store links for own skills and agent links for every skill of the
manifest. Projects have no links to create: ` + "`skenv sync`" + ` updates their mirrors.

- Reads: the manifest, the own working copies, the store, the agent
  directories and the state file.
- Changes: the store links of own skills, the agent links and the state
  file; it pulls, vendors and removes nothing.
- Network: none.
- Conflicts: an unmanaged path in the way is an error and stays; --adopt
  moves it to ~/.local/state/skenv/backup/<ts>/ and replaces it.
- Preview: --dry-run writes nothing.
- Next: "skenv doctor".`
		c.Example = "# Recreate a link removed by hand, without pulling\nskenv link"
	}
	manifestFlag(c.Flags(), &o)
	dryRunFlag(c.Flags(), &o.DryRun, dryRunSync)
	c.Flags().BoolVar(&o.Adopt, "adopt", false, "move conflicting unmanaged paths to ~/.local/state/skenv/backup/<ts>/ and replace them")
	if name == "sync" {
		c.Flags().BoolVar(&o.Quiet, "quiet", false, "print only warnings and errors")
		projectFlag(c.Flags(), &project, "sync the [project] section of the current repository (the default there)")
	}
	return c
}

func doctorCmd(a *app) *cobra.Command {
	o := engine.Options{ReadOnly: true}
	var asJSON, project bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Compare the machine with the manifest, or a project with its [project]",
		Long: `Compare the machine with the manifest. It changes no skill, link or file,
but runs ` + "`git fetch`" + ` in each own repository (network access; it updates their
remote-tracking branches) to report unpushed and behind.
Classes: missing, extra-managed, unmanaged, wrong-rev, broken-link, conflict,
dirty, unpushed, behind, agent-mismatch.

In a project (a git repository whose skenv file has [project]), doctor
compares the project with its [project] section instead, offline, so it can
run in CI. Classes: missing, wrong-rev, modified (a copy edited locally),
extra-managed, conflict, broken-mirror, mirror-drift, unmanaged (a skill
only in a mirror). --manifest checks the machine from there; --project
requires a project.

Exit code: 0 in sync, 1 discrepancies, 2 error. Exit code 0 is the check
that a sync converged: sync itself exits 0 with warnings.`,
		Example: `# The machine matches the manifest
skenv doctor
# A skill link was removed by hand
skenv doctor
# In a project: a copy was edited and a mirror link removed by hand
skenv doctor --project`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
			file, err := projectScope(ctx, env, "doctor", project, true, o.Manifest)
			if err != nil {
				return engine.ExitFatal, err
			}
			if file != "" {
				e, err := engine.OpenProject(ctx, env, o, file)
				if err != nil {
					return engine.ExitFatal, err
				}
				defer e.Close()
				return e.Doctor(asJSON)
			}
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
	projectFlag(c.Flags(), &project, "check the [project] section of the current repository (the default there)")
	return c
}

// vendorer is the manifest or a project, for the vendor commands.
type vendorer interface {
	VendorAdd(engine.VendorAddOptions) (int, error)
	VendorUpdate(names []string, rev string) (int, error)
	VendorRemove(name string) (int, error)
	Close()
}

func vendorCmd(a *app) *cobra.Command {
	type flags struct {
		o       engine.Options
		project bool
	}
	// shared wires the flags every vendor subcommand shares.
	shared := func(c *cobra.Command, f *flags, dryRun string) {
		manifestFlag(c.Flags(), &f.o)
		dryRunFlag(c.Flags(), &f.o.DryRun, dryRun)
		c.Flags().BoolVar(&f.o.Adopt, "adopt", false, "move conflicting unmanaged paths to the backup directory and replace them")
		projectFlag(c.Flags(), &f.project, "edit [project] of the current repository instead of the manifest, and sync the project")
	}
	withEngine := func(f *flags, fn func(v vendorer, args []string) (int, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			name := cmdName(cmd)
			return a.action(func(ctx context.Context, env engine.Env, args []string) (int, error) {
				file, err := projectScope(ctx, env, name, f.project, false, f.o.Manifest)
				if err != nil {
					return engine.ExitFatal, err
				}
				var v vendorer
				if file != "" {
					v, err = engine.OpenProject(ctx, env, f.o, file)
				} else {
					v, err = engine.Open(ctx, env, f.o)
				}
				if err != nil {
					return engine.ExitFatal, err
				}
				defer v.Close()
				return fn(v, args)
			})(cmd, args)
		}
	}
	revUsage := "commit to pin (default: HEAD of the default branch)"

	var addF flags
	var va engine.VendorAddOptions
	add := &cobra.Command{
		Use:   "add <repo>",
		Short: "Install a third-party skill, pinned to a commit",
		Long: `Pin a third-party skill in the manifest (HEAD of the default branch unless
--rev) and sync it. The manifest change is not committed.

--path is the directory of the skill inside the repository; it can be left
out when the repository has exactly one SKILL.md. The skill is installed
under --name, by default the last element of that directory (the
repository name when the skill is at its root), lowercased.

<repo> is written to the manifest as given: owner/repo on github.com,
gitlab:group/sub/repo, codeberg:owner/repo, <alias>:path of a host declared
under [environment.hosts.<alias>], or a full git URL. An unknown prefix is
an error. See https://qunaxis.github.io/skenv/git-hosts

With --project: add a [[project.vendor]] entry to the skenv file of the
current repository and sync the project, which copies the skill into its
dir and mirrors. Its hosts are the ones declared under
[project.hosts.<alias>]. Commit the file and the copies with the project.

- Reads: the manifest (or [project]) and the repository of the skill.
- Changes: the manifest (or [project]), the copy of the skill in the store
  (or the project), its agent links (or mirrors) and the state file.
- Network: fetches the repository into the clone cache ~/.cache/skenv/repos.
- Conflicts: an unmanaged path with the skill's name is an error and stays;
  --adopt moves it to ~/.local/state/skenv/backup/<ts>/ and replaces it.
- Preview: --dry-run writes nothing except the clone cache.
- Next: commit the skenv file; "skenv doctor".`,
		Example: `# Pin the skill in tools/release-notes/ at HEAD of the default branch
skenv vendor add example-vendor/tools --path tools/release-notes
# A skill from a GitLab subgroup
skenv vendor add gitlab:example-org/team/tools --path release-notes
# A skill from a self-hosted host declared as "work" in the manifest
skenv vendor add work:platform/skills --path deploy
# Pin it in the current project instead
skenv vendor add example-vendor/tools --path tools/release-notes --project`,
		Args: nArgs(1),
		RunE: withEngine(&addF, func(v vendorer, args []string) (int, error) {
			va.Repo = args[0]
			return v.VendorAdd(va)
		}),
	}
	shared(add, &addF, dryRunFetch)
	add.Flags().StringVar(&va.Path, "path", "", `directory with SKILL.md inside the repository ("." for the root; default: the only one)`)
	add.Flags().StringVar(&va.Name, "name", "", "skill name (default: last element of --path, lowercased)")
	add.Flags().StringVar(&va.Rev, "rev", "", revUsage)

	var updateF flags
	var rev string
	update := &cobra.Command{
		Use:     "update [name...]",
		Aliases: []string{"upgrade"},
		Short:   "Move third-party skills to a new commit",
		Long: `Move vendored skills to a new commit and sync them: the named ones, or every
vendored skill without names. Each goes to HEAD of its default branch;
--rev pins a single named skill. Shows the log of the skill's path.

With --project: move entries of [project] and sync the project. A skill of
a [[project.from]] entry moves the whole entry, whose skills share one rev.

- Reads: the manifest (or [project]) and the repositories of the skills.
- Changes: the rev of each moved skill in the manifest (or [project]), its
  copy, its links (or mirrors) and the state file.
- Network: fetches each repository into the clone cache ~/.cache/skenv/repos.
- Conflicts: as vendor add: --adopt replaces an unmanaged path, after a
  backup.
- Preview: --dry-run writes nothing except the clone cache.
- Next: commit the skenv file; "skenv doctor".`,
		Example: `# Move diagrams to HEAD of the default branch of its repository
skenv vendor update diagrams
# Update every vendored skill
skenv vendor update
# Update every pinned skill of the current project
skenv vendor update --project`,
		Args: func(cmd *cobra.Command, args []string) error {
			if rev != "" && len(args) != 1 {
				return usageError{fmt.Sprintf("%s: --rev needs exactly 1 name, got %d (see `%s --help`)", cmdName(cmd), len(args), cmd.CommandPath())}
			}
			return nil
		},
		RunE: withEngine(&updateF, func(v vendorer, args []string) (int, error) {
			return v.VendorUpdate(args, rev)
		}),
	}
	shared(update, &updateF, dryRunFetch)
	update.Flags().StringVar(&rev, "rev", "", revUsage)

	var rmF flags
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a third-party skill and its installed copy",
		Long: `Remove a vendored skill from the manifest and its managed paths.

With --project: remove its [[project.vendor]] entry and sync the project,
which removes the copy and its mirrors. A skill of a [[project.from]] entry
is removed by editing the skills of that entry.

- Reads: the manifest (or [project]) and the state file.
- Changes: the manifest (or [project]), and removes the paths the state file
  records for the skill: its copy and links (or mirrors).
- Network: none.
- Conflicts: paths skenv does not manage are left alone.
- Preview: --dry-run writes and removes nothing.
- Next: commit the skenv file.`,
		Example: "skenv vendor remove diagrams\n" +
			"# Remove a skill pinned in the current project\n" +
			"skenv vendor remove diagrams --project",
		Args: nArgs(1),
		RunE: withEngine(&rmF, func(v vendorer, args []string) (int, error) {
			return v.VendorRemove(args[0])
		}),
	}
	shared(remove, &rmF, dryRunPlain)

	c := group("vendor", "Install, update and remove third-party skills, pinned to a commit", add, update, remove)
	c.Long = `Install, update and remove third-party skills. Each is pinned: the manifest
records the repository, the directory of the skill and the commit
([[environment.vendor]]), and skenv installs a copy of the skill at that
commit. The copy changes only when "skenv vendor update" moves the pin, not
when the repository moves on. Your own skills are different: they are
linked from editable git working copies ([[environment.own]]), which sync
pulls.`
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
the skenv file (skenv.toml, .yaml, .yml or .json with [repo], [environment]
and [project]), "config" for the tool config ~/.config/skenv/config.*.

Files that skenv writes name their schema in a directive, so most editors
need no setup. Use this for offline work or a custom mapping, for example a
JSON Schema mapping in JetBrains IDEs or a rule in .taplo.toml. The same
schemas are published at ` + schemas.Base + `.`,
		Example:   "skenv schema > skenv.schema.json\nskenv schema config",
		Args:      rangeArgs(0, 1),
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
	sub := func(action, short, long string) *cobra.Command {
		return &cobra.Command{
			Use:     action,
			Short:   short,
			Long:    long,
			Example: "skenv autostart " + action,
			Args:    nArgs(0),
			RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
				return runAutostart(ctx, env, action)
			}),
		}
	}
	c := group("autostart", "Run `skenv sync --quiet` at login and every hour",
		sub("enable", "Install and load the autostart job", `Install and load the autostart job, which runs `+"`skenv sync --quiet`"+` at
login and every hour.

- Reads: the path of this skenv binary and of git.
- Changes: ~/Library/LaunchAgents/`+autostart.Label+`.plist (macOS) or
  skenv.service and skenv.timer in ~/.config/systemd/user (Linux), loaded
  with launchctl or systemctl --user; an existing job is replaced.
- Network: none itself; each hourly sync pulls and fetches.
- Preview: none; `+"`skenv sync --dry-run`"+` shows what the job would change.
- Next: "skenv autostart status"; the log is ~/.local/state/skenv/autostart.log.`),
		sub("disable", "Unload and remove the autostart job", ""),
		sub("status", "Show whether the autostart job is installed and loaded (exit 1 if not)", ""),
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

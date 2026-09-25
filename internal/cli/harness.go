package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/lint"
)

func lintCmd(a *app) *cobra.Command {
	var staged, publish, hook bool
	c := &cobra.Command{
		Use:   "lint [path...]",
		Short: "Check skills (L1-L6, P1)",
		Long: `Check skills (directories with SKILL.md) under each path (default "."):
L1 frontmatter, L2 name, L3 Agent Skills limits, L4 relative links,
L5 file size and secret-like files, L6 shebangs. --publish adds P1: a license,
metadata.source not book/internal/third-party-copy, no stop-list phrase
($SKENV_DENYLIST or ~/.config/skenv/denylist.txt) and gitleaks over the whole
history. --hook is the Claude Code PostToolUse hook: it reads the event on stdin.
Exit code 0: clean, 1: problems, 2: error (--hook: 2 with findings,
so Claude Code shows them to the agent).`,
		Example: `# Check every skill under the current directory
skenv lint
# A skill with problems
skenv lint skills/draft
# Before the repository goes public
skenv lint --publish`,
		RunE: a.action(func(ctx context.Context, env engine.Env, pos []string) (int, error) {
			return runLint(ctx, env, pos, staged, publish, hook)
		}),
	}
	c.Flags().BoolVar(&staged, "staged", false, "only skills with files changed in the git index")
	c.Flags().BoolVar(&publish, "publish", false, "also run the publication checks (P1)")
	c.Flags().BoolVar(&hook, "hook", false, "lint the skill of the file named in a Claude Code hook event on stdin")
	return c
}

func runLint(ctx context.Context, env engine.Env, pos []string, staged, publish, hook bool) (int, error) {
	var err error
	if hook {
		return lintHook(env)
	}
	var skills []string
	switch {
	case staged:
		if len(pos) > 0 {
			return engine.ExitFatal, usageError{"lint: --staged takes no paths"}
		}
		root, err := gitRoot(ctx, ".")
		if err != nil {
			return engine.ExitFatal, err
		}
		out, err := gitx.Git{}.Run(ctx, root, "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z")
		if err != nil {
			return engine.ExitFatal, err
		}
		var files []string
		for _, f := range strings.Split(out, "\x00") {
			if f != "" {
				files = append(files, f)
			}
		}
		skills = lint.ForFiles(root, files)
	default:
		if len(pos) == 0 {
			pos = []string{"."}
		}
		for _, p := range pos {
			found, err := lint.Find(p)
			if err != nil {
				return engine.ExitFatal, err
			}
			skills = append(skills, found...)
		}
	}
	var deny *lint.Denylist
	if publish {
		if deny, err = lint.LoadDenylist(env.Home, env.Getenv); err != nil {
			return engine.ExitFatal, err
		}
	}
	cwd, _ := os.Getwd()
	problems := 0
	report := func(fs []lint.Finding) {
		for _, f := range fs {
			if rel, err := filepath.Rel(cwd, f.Skill); err == nil && !strings.HasPrefix(rel, "..") {
				f.Skill = rel
			}
			fmt.Fprintln(env.Stdout, f)
			problems++
		}
	}
	for _, s := range skills {
		report(lint.Skill(s))
		if publish {
			report(lint.Publish(s))
		}
	}
	if publish {
		dir := "."
		if len(pos) > 0 {
			dir = pos[0]
		}
		root, err := gitRoot(ctx, dir)
		if err != nil {
			return engine.ExitFatal, fmt.Errorf("--publish scans the repository and its history: %w", err)
		}
		// The whole repository is published, not only the skills.
		files, err := lint.RepoFiles(root)
		if err != nil {
			return engine.ExitFatal, err
		}
		report(lint.ScanDenylist(root, files, deny))
		leaks, err := lint.Gitleaks(root)
		if err != nil {
			return engine.ExitFatal, err
		}
		if leaks != "" {
			fmt.Fprintf(env.Stdout, "%s: P1: gitleaks found secrets in the history (redacted report below)\n%s", root, leaks)
			problems++
		}
	}
	fmt.Fprintf(env.Stderr, "lint: %d skills, %d problems\n", len(skills), problems)
	if problems > 0 {
		return engine.ExitProblems, nil
	}
	return engine.ExitOK, nil
}

// hookStdin is the Claude Code hook event; replaced in tests.
var hookStdin io.Reader = os.Stdin

// lintHook handles a Claude Code PostToolUse event: lint the skill that
// contains the edited file and, when there are findings, print them to
// stderr with exit code 2, which Claude Code feeds back to the agent.
// Files outside skills and malformed events are ignored (exit 0) so the
// hook never gets in the way of unrelated edits.
func lintHook(env engine.Env) (int, error) {
	var event struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(hookStdin).Decode(&event); err != nil || event.ToolInput.FilePath == "" {
		return engine.ExitOK, nil //nolint:nilerr // not an edit event: nothing to lint
	}
	skill, ok := lint.SkillOf(event.ToolInput.FilePath)
	if !ok {
		return engine.ExitOK, nil
	}
	findings := lint.Skill(skill)
	if len(findings) == 0 {
		return engine.ExitOK, nil
	}
	fmt.Fprintf(env.Stderr, "skenv lint: %s has %d problems after %s; fix them:\n", skill, len(findings), event.ToolName)
	for _, f := range findings {
		fmt.Fprintln(env.Stderr, f)
	}
	return engine.ExitFatal, nil
}

func gitRoot(ctx context.Context, dir string) (string, error) {
	root, err := gitx.Git{}.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository", dir)
	}
	return root, nil
}

func repoCmd(a *app) *cobra.Command {
	var dir, visibility, format string
	var dryRun, force bool
	sub := func(name, use, short, long, example string) *cobra.Command {
		c := &cobra.Command{
			Use:     use,
			Short:   short,
			Long:    long,
			Example: example,
			Args:    nArgs(0),
			RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
				return runRepo(ctx, env, name, dir, visibility, format, dryRun, force)
			}),
		}
		if name != "check" {
			dryRunFlag(c.Flags(), &dryRun)
			c.Flags().BoolVar(&force, "force", false, "replace existing files that skenv does not manage yet")
		}
		return c
	}
	initC := sub("init", "init --visibility private|public", "Set up the harness of a skills repository",
		"Set up the harness of a skills repository: the [repo] section and the schema\ndirective of the skenv file, lefthook.yml, CI workflow, linter configs and the\nmanaged blocks of AGENTS.md and .gitignore; then `lefthook install`. Refuses\nif [repo] exists.\n\nWithout a skenv file it creates skenv.toml, or skenv.yaml or skenv.json with\n--format. An existing skenv file gets [repo] added in its own format;\n--format that disagrees with it is an error, and nothing is written.",
		"# Set up the harness of a public skills repository in the current directory\n"+
			"skenv repo init --visibility public")
	initC.Flags().StringVar(&visibility, "visibility", "", "private or public (required)")
	_ = initC.RegisterFlagCompletionFunc("visibility", cobra.FixedCompletions([]string{"private", "public"}, cobra.ShellCompDirectiveNoFileComp))
	formatFlag(initC, &format, "format of a new skenv file: toml, yaml or json (default toml; an existing file keeps its format)")
	apply := sub("apply", "apply", "Regenerate the managed files of the harness",
		"Regenerate the managed files and blocks from the templates of this skenv\n(harness "+harness.Latest+"; an older repo.harness is moved to it) and point\nthe schema directive of the skenv file at that version; then\n`lefthook install`.",
		"# Restore a managed file edited by hand\nskenv repo apply")
	check := sub("check", "check", "Compare the managed files with the harness templates",
		"Compare the managed files and blocks with the templates of this skenv\n(harness "+harness.Latest+"). Exit code 0: in sync, 1: drift (files listed), 2: error.\nA missing or outdated schema directive in the skenv file is a warning that\ndoes not change the exit code.",
		"# The managed files match the harness\nskenv repo check\n# A managed file was edited by hand\nskenv repo check")
	c := group("repo", "Set up and check the harness of a skills repository", initC, apply, check)
	c.Example = "skenv repo init --visibility private\nskenv repo check\nskenv repo apply"
	c.PersistentFlags().StringVar(&dir, "dir", ".", "repository (any directory inside it)")
	return c
}

func runRepo(ctx context.Context, env engine.Env, sub, dir, visibility, format string, dryRun, force bool) (int, error) {
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return engine.ExitFatal, err
	}
	switch sub {
	case "init":
		if visibility == "" {
			return engine.ExitFatal, usageError{"repo init: --visibility private|public is required"}
		}
		if err := fileformat.Valid(format); err != nil {
			return engine.ExitFatal, usageError{"repo init: " + err.Error()}
		}
		c, changes, err := harness.Init(root, visibility, format, dryRun, force)
		printChanges(env, changes, dryRun)
		if err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintf(env.Stdout, "harness %s (%s) set up in %s\n", c.Harness, c.Visibility, root)
		return lefthookInstall(ctx, env, root, dryRun), nil
	case "apply":
		c, err := harness.LoadConfig(root)
		if err != nil {
			return engine.ExitFatal, err
		}
		old := c.Harness
		c.Harness = harness.Latest
		// Refuse (foreign files without --force) before the version moves.
		if _, err := harness.Apply(root, c, true, force); err != nil {
			return engine.ExitFatal, err
		}
		// repo.harness and the schema directive of the skenv file.
		updated, err := harness.Update(root, harness.Latest, dryRun)
		if err != nil {
			return engine.ExitFatal, err
		}
		if old != harness.Latest {
			prefix := ""
			if dryRun {
				prefix = "would move "
			}
			fmt.Fprintf(env.Stdout, "%sharness %s → %s\n", prefix, old, harness.Latest)
		} else if updated {
			printChanges(env, []harness.Change{{Path: filepath.Base(c.File), Action: "update"}}, dryRun)
		}
		changes, err := harness.Apply(root, c, dryRun, force)
		printChanges(env, changes, dryRun)
		if err != nil {
			return engine.ExitFatal, err
		}
		if len(changes) == 0 && !updated {
			fmt.Fprintln(env.Stdout, "repo apply: up to date")
		}
		return lefthookInstall(ctx, env, root, dryRun), nil
	}
	drift, err := harness.Check(root)
	if err != nil {
		return engine.ExitFatal, err
	}
	// A missing or outdated schema directive is a warning: it only helps
	// editors, and the exit code stays.
	if c, err := harness.LoadConfig(root); err == nil {
		if w, err := harness.DirectiveWarning(c); err == nil && w != "" {
			fmt.Fprintf(env.Stderr, "warning: %s: %s\n", filepath.Base(c.File), w)
		}
	}
	for _, d := range drift {
		fmt.Fprintf(env.Stdout, "%s: %s\n", d.Path, d.Reason)
	}
	if len(drift) > 0 {
		fmt.Fprintf(env.Stdout, "repo check: %d files differ; run `skenv repo apply` (templates live in skenv, not in this repository)\n", len(drift))
		return engine.ExitProblems, nil
	}
	fmt.Fprintln(env.Stdout, "repo check: managed files match the harness")
	return engine.ExitOK, nil
}

func printChanges(env engine.Env, changes []harness.Change, dryRun bool) {
	prefix := ""
	if dryRun {
		prefix = "would "
	}
	for _, c := range changes {
		fmt.Fprintf(env.Stdout, "%s%s %s\n", prefix, c.Action, c.Path)
	}
}

// lefthookInstall activates the hooks; a missing lefthook is a warning,
// not a failure, so the files are still generated.
func lefthookInstall(ctx context.Context, env engine.Env, root string, dryRun bool) int {
	if dryRun {
		fmt.Fprintln(env.Stdout, "would run lefthook install")
		return engine.ExitOK
	}
	bin, err := lookLefthook("lefthook")
	if err != nil {
		fmt.Fprintln(env.Stderr, "warning: lefthook not found; install it (brew install lefthook) and run `lefthook install`")
		return engine.ExitOK
	}
	cmd := exec.CommandContext(ctx, bin, "install")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			fmt.Fprintf(env.Stderr, "error: lefthook install failed: %s\n", strings.TrimSpace(string(out)))
			return engine.ExitProblems
		}
		fmt.Fprintf(env.Stderr, "error: lefthook install: %v\n", err)
		return engine.ExitProblems
	}
	fmt.Fprintln(env.Stdout, "lefthook install: hooks active")
	return engine.ExitOK
}

// lookLefthook is replaced in tests.
var lookLefthook = exec.LookPath

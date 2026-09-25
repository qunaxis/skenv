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
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/lint"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

func lintCmd(a *app) *cobra.Command {
	var staged, publish, hook bool
	c := &cobra.Command{
		Use:   "lint [path...]",
		Short: "Check skills for format, links, size and secrets",
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
		// The hook reads its skill from the event: paths, --staged and
		// --publish would be ignored, so they are rejected instead.
		if len(pos) > 0 || staged || publish {
			return engine.ExitFatal, usageError{"lint: --hook takes the file from the hook event on stdin; it does not combine with paths, --staged or --publish"}
		}
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
	var dir, visibility, ci, format string
	var runner []string
	var dryRun, force bool
	sub := func(name, use, short, long, example string) *cobra.Command {
		c := &cobra.Command{
			Use:     use,
			Short:   short,
			Long:    long,
			Example: example,
			Args:    nArgs(0),
			RunE: a.action(func(ctx context.Context, env engine.Env, _ []string) (int, error) {
				return runRepo(ctx, env, name, dir, visibility, ci, format, runner, dryRun, force)
			}),
		}
		if name != "check" {
			dryRunFlag(c.Flags(), &dryRun, dryRunPlain)
			c.Flags().BoolVar(&force, "force", false, "replace existing files that skenv does not manage yet")
		}
		return c
	}
	initC := sub("init", "init --visibility private|public [--ci github|gitlab] [--runner label,...]", "Set up the harness of a skills repository",
		"Set up the harness of a skills repository: the [repo] section and the schema\ndirective of the skenv file, lefthook.yml, the CI pipeline, linter configs and\nthe managed blocks of AGENTS.md and .gitignore; then `lefthook install`.\nRefuses if [repo] exists.\n\n"+
			"--ci picks the CI system: github (.github/workflows/check.yml) or gitlab\n(.gitlab-ci.yml). Without it, the host of origin decides: gitlab when origin\nis on gitlab.com or on a host declared with type \"gitlab\" in the manifest\n(this repository's own [environment], else the manifest in the config file),\ngithub otherwise, also when there is no origin.\n\n"+
			"CI jobs of a public repository run on the hosted ubuntu-latest runners.\nThose of a private one run on --runner: the runs-on labels on GitHub, the\nrunner tags on GitLab; default self-hosted, linux, docker (a self-hosted\nDocker runner). --runner ubuntu-latest picks the GitHub-hosted runners.\nAfterwards repo.runner in the skenv file holds it; change it there and run\n`skenv repo apply`.\n\n"+
			"The generated git hooks need lefthook, uv and gitleaks on PATH; the output\nsays which of them are missing. Skill management and sync need none of\nthem: this harness is optional tooling for a repository you publish or\nshare.\n\n"+
			"Without a skenv file it creates skenv.toml, or skenv.yaml or skenv.json with\n--format. An existing skenv file gets [repo] added in its own format;\n--format that disagrees with it is an error, and nothing is written.\n\n"+
			"- Reads: the repository, its origin and skenv file, and the hosts declared\n  in the manifest (to detect the CI system).\n"+
			"- Changes: the skenv file ([repo], created if absent), the managed files\n  and blocks, and the git hooks (lefthook install).\n"+
			"- Network: none.\n"+
			"- Conflicts: a file that exists and that skenv does not manage yet is an\n  error; --force replaces it.\n"+
			"- Preview: --dry-run writes nothing and does not run lefthook install.\n"+
			"- Next: commit the generated files; \"skenv repo check\" compares them\n  with the templates later.",
		"# Set up the harness of a public skills repository in the current directory\n"+
			"skenv repo init --visibility public\n"+
			"# A private repository on GitLab, with jobs on runners tagged self-hosted, linux, docker\n"+
			"skenv repo init --visibility private --ci gitlab\n"+
			"# A private repository on GitHub, with jobs on the GitHub-hosted runners\n"+
			"skenv repo init --visibility private --runner ubuntu-latest")
	initC.Flags().StringVar(&visibility, "visibility", "", "private or public (required)")
	_ = initC.RegisterFlagCompletionFunc("visibility", cobra.FixedCompletions([]string{"private", "public"}, cobra.ShellCompDirectiveNoFileComp))
	initC.Flags().StringSliceVar(&runner, "runner", nil, "private repositories: runs-on labels (GitHub) or runner tags (GitLab) of the CI jobs, comma-separated or repeated (default self-hosted,linux,docker)")
	initC.Flags().StringVar(&ci, "ci", "", "CI system: github or gitlab (default: detected from the host of origin, else github)")
	_ = initC.RegisterFlagCompletionFunc("ci", cobra.FixedCompletions(harness.CIs, cobra.ShellCompDirectiveNoFileComp))
	formatFlag(initC, &format, "format of a new skenv file: toml, yaml or json (default toml; an existing file keeps its format)")
	apply := sub("apply", "apply", "Regenerate the managed files of the harness",
		"Regenerate the managed files and blocks from the templates of this skenv\n(harness "+harness.Latest+"; an older repo.harness is moved to it) and point\nthe schema directive of the skenv file at that version; then\n`lefthook install`.\n\nThe CI pipeline follows repo.ci of the skenv file. To switch CI systems, edit\nrepo.ci and run apply: it writes the pipeline of the new one and removes the\nmanaged file of the other (.github/workflows/check.yml or .gitlab-ci.yml).\n\n"+
			"- Reads: the skenv file ([repo]) and the managed files.\n"+
			"- Changes: the managed files and blocks, repo.harness and the schema\n  directive of the skenv file, and the git hooks (lefthook install).\n"+
			"- Network: none.\n"+
			"- Conflicts: a file that exists and that skenv does not manage yet is an\n  error; --force replaces it.\n"+
			"- Preview: --dry-run writes nothing and does not run lefthook install.\n"+
			"- Next: commit the changed files.",
		"# Restore a managed file edited by hand\nskenv repo apply")
	check := sub("check", "check", "Compare the managed files with the harness templates",
		"Compare the managed files and blocks with the templates of this skenv\n(harness "+harness.Latest+"). Exit code 0: in sync, 1: drift (files listed), 2: error.\nA missing or outdated schema directive in the skenv file is a warning that\ndoes not change the exit code.",
		"# The managed files match the harness\nskenv repo check\n# A managed file was edited by hand\nskenv repo check")
	c := group("repo", "Set up and check the harness of a skills repository", initC, apply, check)
	c.Example = "skenv repo init --visibility private\nskenv repo init --visibility public --ci gitlab\nskenv repo check\nskenv repo apply"
	c.PersistentFlags().StringVar(&dir, "dir", ".", "repository (any directory inside it)")
	return c
}

func runRepo(ctx context.Context, env engine.Env, sub, dir, visibility, ci, format string, runner []string, dryRun, force bool) (int, error) {
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
		detected := ""
		switch {
		case ci == "":
			ci, detected = detectCI(ctx, env, root)
		case !slices.Contains(harness.CIs, ci):
			return engine.ExitFatal, usageError{fmt.Sprintf("repo init: --ci must be github or gitlab, got %q", ci)}
		}
		c, changes, err := harness.Init(root, visibility, ci, format, runner, dryRun, force)
		printChanges(env, changes, dryRun)
		if err != nil {
			return engine.ExitFatal, err
		}
		if detected != "" {
			fmt.Fprintf(env.Stdout, "ci %s: %s; --ci overrides it\n", c.CI, detected)
		}
		fmt.Fprintf(env.Stdout, "harness %s (%s, ci %s) set up in %s\n", c.Harness, c.Visibility, c.CI, root)
		if c.Visibility == "private" {
			fmt.Fprintf(env.Stdout, "CI jobs run on runners %s (repo.runner); to change them, edit repo.runner and run `skenv repo apply`\n", strings.Join(c.Runner, ", "))
		}
		printHookTools(env)
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

// detectCI is repo.ci for `repo init` without --ci: gitlab when origin is
// on a GitLab host, github otherwise. The hosts declared in the manifest
// count: the [environment] of the repository itself, else the manifest of
// the config file. The origin URL is never printed (it may carry
// credentials); why says what decided.
func detectCI(ctx context.Context, env engine.Env, root string) (ci, why string) {
	origin, err := gitx.Git{}.Run(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(origin) == "" {
		return harness.CIGitHub, "the default, the repository has no origin"
	}
	r, err := declaredHosts(ctx, env, root).Resolve(origin)
	if err != nil {
		return harness.CIGitHub, "the default, origin is not a git URL skenv recognises"
	}
	ci = harness.DetectCI(r.Type)
	if ci == harness.CIGitLab {
		return ci, "detected from origin, on a GitLab host"
	}
	if r.Type == manifest.TypeGitHub {
		return ci, "detected from origin, on GitHub"
	}
	return ci, "the default, origin is not on a GitLab host"
}

// declaredHosts are the hosts of the manifest that applies to root, nil
// when there is none or it does not load.
func declaredHosts(ctx context.Context, env engine.Env, root string) manifest.Hosts {
	file, err := skenvfile.Find(root)
	if err == nil && file != "" {
		if doc, err := skenvfile.Read(file); err == nil && doc.Has(skenvfile.Environment) {
			m, err := manifest.Load(file)
			if err != nil {
				fmt.Fprintf(env.Stderr, "warning: hosts of %s not read, CI detection ignores them: %v\n", filepath.Base(file), err)
				return nil
			}
			return m.Hosts
		}
	}
	file, err = engine.ResolveManifest(ctx, env, "")
	if err != nil {
		return nil
	}
	m, err := manifest.Load(file)
	if err != nil {
		fmt.Fprintf(env.Stderr, "warning: hosts of the manifest not read, CI detection ignores them: %v\n", err)
		return nil
	}
	return m.Hosts
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

// hookTools are the programs the generated git hooks run besides skenv and
// git (templates/lefthook.yml); lookTool is replaced in tests.
var (
	hookTools = []string{"uv", "gitleaks"}
	lookTool  = exec.LookPath
)

// printHookTools warns about the programs the hooks need that are not on
// PATH: without them a commit in the repository fails.
func printHookTools(env engine.Env) {
	var missing []string
	if _, err := lookLefthook("lefthook"); err != nil {
		missing = append(missing, "lefthook")
	}
	for _, t := range hookTools {
		if _, err := lookTool(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(env.Stderr, "warning: the git hooks need %s, not found on PATH; install them before committing here\n", strings.Join(missing, ", "))
	}
}

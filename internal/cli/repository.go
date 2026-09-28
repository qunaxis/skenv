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

	"github.com/qunaxis/skenv/internal/features/lint"
	"github.com/qunaxis/skenv/internal/features/repository"
	"github.com/qunaxis/skenv/internal/features/skills"
	"github.com/qunaxis/skenv/internal/model/skenvfile"
	"github.com/qunaxis/skenv/internal/platform/docedit"
	"github.com/qunaxis/skenv/internal/platform/gitx"
)

func lintCmd(a *app) *cobra.Command {
	var staged, publish, hook bool
	c := &cobra.Command{
		Use:   "lint [path...]",
		Short: "Check skills for format, links, size and secrets",
		Long:  help("lint"),
		Example: `# Check every skill under the current directory
skenv lint
# A skill with problems
skenv lint skills/draft
# Before the repository goes public
skenv lint --publish`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.action(func(ctx context.Context, env skills.Env, pos []string) (int, error) {
				return runLint(ctx, env, cmd.InOrStdin(), pos, staged, publish, hook)
			})(cmd, args)
		},
	}
	c.Flags().BoolVar(&staged, "staged", false, "only skills with files changed in the git index")
	c.Flags().BoolVar(&publish, "publish", false, "also run the publication checks (P1)")
	c.Flags().BoolVar(&hook, "hook", false, "lint the skill of the file named in a Claude Code hook event on stdin")
	return c
}

func runLint(ctx context.Context, env skills.Env, stdin io.Reader, pos []string, staged, publish, hook bool) (int, error) {
	if hook {
		// The hook reads its skill from the event: paths, --staged and
		// --publish would be ignored, so they are rejected instead.
		if len(pos) > 0 || staged || publish {
			return 0, errors.New("lint: --hook takes the file from the hook event on stdin; it does not combine with paths, --staged or --publish")
		}
		return lintHook(env, stdin)
	}
	skillDirs, err := lintSkillDirs(ctx, pos, staged)
	if err != nil {
		return 0, err
	}
	var deny *lint.Denylist
	if publish {
		if deny, err = lint.LoadDenylist(env.Home, env.Getenv); err != nil {
			return 0, err
		}
	}
	problems, err := lintReport(ctx, env, skillDirs, pos, publish, deny)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stderr, "lint: %d skills, %d problems\n", len(skillDirs), problems)
	if problems > 0 {
		return skills.ExitProblems, nil
	}
	return skills.ExitOK, nil
}

// lintSkillDirs is the skills to check: those with files staged in the git
// index (--staged) or those found under pos (default ".").
func lintSkillDirs(ctx context.Context, pos []string, staged bool) ([]string, error) {
	if staged {
		if len(pos) > 0 {
			return nil, errors.New("lint: --staged takes no paths")
		}
		return lintStagedSkillDirs(ctx)
	}
	if len(pos) == 0 {
		pos = []string{"."}
	}
	var skillDirs []string
	for _, p := range pos {
		found, err := lint.Find(p)
		if err != nil {
			return nil, err
		}
		skillDirs = append(skillDirs, found...)
	}
	return skillDirs, nil
}

// lintStagedSkillDirs is lintSkillDirs's --staged case: the skills
// containing a file changed in the git index.
func lintStagedSkillDirs(ctx context.Context) ([]string, error) {
	root, err := gitRoot(ctx, ".")
	if err != nil {
		return nil, err
	}
	out, err := gitx.Git{}.Run(ctx, root, "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for f := range strings.SplitSeq(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return lint.ForFiles(root, files), nil
}

// lintReport prints the findings of skillDirs (L1-L6, plus P1 with
// --publish) and, with --publish, the repository-wide P1 findings (the
// stop-list and gitleaks); it returns the number of problems found. Skill
// paths are printed relative to the working directory when possible.
func lintReport(ctx context.Context, env skills.Env, skillDirs, pos []string, publish bool, deny *lint.Denylist) (int, error) {
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
	for _, s := range skillDirs {
		report(lint.Skill(s))
		if publish {
			report(lint.Publish(s))
		}
	}
	if !publish {
		return problems, nil
	}
	dir := "."
	if len(pos) > 0 {
		dir = pos[0]
	}
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return 0, fmt.Errorf("--publish scans the repository and its history: %w", err)
	}
	findings, leaks, err := lint.PublishScan(root, deny)
	if err != nil {
		return 0, err
	}
	report(findings)
	if leaks != "" {
		fmt.Fprintf(env.Stdout, "%s: P1: gitleaks found secrets in the history (redacted report below)\n%s", root, leaks)
		problems++
	}
	return problems, nil
}

// lintHook handles a Claude Code PostToolUse event on stdin: lint the
// skill that contains the edited file and, when there are findings, print
// them to stderr with exit code 2, which Claude Code feeds back to the agent.
// Files outside skills and malformed events are ignored (exit 0) so the
// hook never gets in the way of unrelated edits.
func lintHook(env skills.Env, stdin io.Reader) (int, error) {
	var event struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(stdin).Decode(&event); err != nil || event.ToolInput.FilePath == "" {
		return skills.ExitOK, nil //nolint:nilerr // not an edit event: nothing to lint
	}
	skill, ok := lint.SkillOf(event.ToolInput.FilePath)
	if !ok {
		return skills.ExitOK, nil
	}
	findings := lint.Skill(skill)
	if len(findings) == 0 {
		return skills.ExitOK, nil
	}
	fmt.Fprintf(env.Stderr, "skenv lint: %s has %d problems after %s; fix them:\n", skill, len(findings), event.ToolName)
	for _, f := range findings {
		fmt.Fprintln(env.Stderr, f)
	}
	return skills.ExitFatal, nil
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
		}
		if name != "check" {
			dryRunFlag(c.Flags(), &dryRun, dryRunPlain)
			c.Flags().BoolVar(&force, "force", false, "replace existing files that skenv does not manage yet")
		}
		return c
	}
	initC := sub("init", "init --visibility private|public [--ci github|gitlab] [--runner label,...]", "Set up the repository templates of a skills repository",
		help("repo_init"),
		"# Set up the repository templates of a public skills repository in the current directory\n"+
			"skenv repo init --visibility public\n"+
			"# A private repository on GitLab, with jobs on runners tagged self-hosted, linux, docker\n"+
			"skenv repo init --visibility private --ci gitlab\n"+
			"# A private repository on GitHub, with jobs on the GitHub-hosted runners\n"+
			"skenv repo init --visibility private --runner ubuntu-latest")
	initC.Flags().StringVar(&visibility, "visibility", "", "private or public, the declared publication policy (required)")
	_ = initC.MarkFlagRequired("visibility")
	_ = initC.RegisterFlagCompletionFunc("visibility", cobra.FixedCompletions([]string{"private", "public"}, cobra.ShellCompDirectiveNoFileComp))
	initC.Flags().StringSliceVar(&runner, "runner", nil, "private repositories: runs-on labels (GitHub) or runner tags (GitLab) of the CI jobs, comma-separated or repeated (default self-hosted,linux,docker)")
	initC.Flags().StringVar(&ci, "ci", "", "CI system: github or gitlab (default: detected from the host of origin, else github)")
	_ = initC.RegisterFlagCompletionFunc("ci", cobra.FixedCompletions(skenvfile.CIs, cobra.ShellCompDirectiveNoFileComp))
	formatFlag(initC, &format, "format of a new skenv file: toml, yaml or json (default toml; an existing file keeps its format)")
	initC.RunE = a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
		return runRepoInit(ctx, env, dir, visibility, ci, format, runner, initC.Flags().Changed("runner"), dryRun, force)
	})
	// repo_apply.md, repo_upgrade.md and repo_check.md carry %s where the
	// template version belongs: it moves at every release (LatestTemplates),
	// unlike the rest of the help text, so it stays a Go value, not a
	// baked-in string.
	apply := sub("apply", "apply", "Regenerate the managed files from the repository templates",
		fmt.Sprintf(help("repo_apply"), skenvfile.LatestTemplates, skenvfile.LatestTemplates),
		"# Restore a managed file edited by hand\nskenv repo apply")
	apply.RunE = a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
		return runRepoApply(ctx, env, dir, dryRun, force)
	})
	upgrade := sub("upgrade", "upgrade", "Move the repository to the templates of this skenv",
		fmt.Sprintf(help("repo_upgrade"), skenvfile.LatestTemplates),
		"# Move the repository to the templates of the installed skenv\nskenv repo upgrade --dry-run\nskenv repo upgrade")
	upgrade.RunE = a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
		return runRepoUpgrade(ctx, env, dir, dryRun, force)
	})
	check := sub("check", "check", "Compare the managed files with the repository templates",
		fmt.Sprintf(help("repo_check"), skenvfile.LatestTemplates),
		"# The managed files match the repository templates\nskenv repo check\n# A managed file was edited by hand\nskenv repo check")
	check.RunE = a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
		return runRepoCheck(ctx, env, dir)
	})
	c := group("repo", "Set up and check the repository templates of a skills repository", initC, apply, upgrade, check)
	c.Example = "skenv repo init --visibility private\nskenv repo init --visibility public --ci gitlab\nskenv repo check\nskenv repo apply\nskenv repo upgrade"
	c.PersistentFlags().StringVar(&dir, "dir", ".", "repository (any directory inside it)")
	return c
}

// runRepoInit is the RunE of `repo init`.
func runRepoInit(ctx context.Context, env skills.Env, dir, visibility, ci, format string, runner []string, runnerSet, dryRun, force bool) (int, error) {
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return 0, err
	}
	if err := docedit.ValidFormat(format); err != nil {
		return 0, fmt.Errorf("repo init: %w", err)
	}
	detected := ""
	switch {
	case ci == "":
		ci, detected = detectCI(ctx, env, root)
	case !slices.Contains(skenvfile.CIs, ci):
		return 0, fmt.Errorf("repo init: --ci must be github or gitlab, got %q", ci)
	}
	if len(runner) == 0 && runnerSet {
		return 0, errors.New("repo init: --runner needs at least one label")
	}
	c, changes, err := repository.Init(root, visibility, ci, format, runner, dryRun, force)
	printChanges(env, changes, dryRun)
	if err != nil {
		return 0, err
	}
	if detected != "" {
		fmt.Fprintf(env.Stdout, "ci %s: %s; --ci overrides it\n", c.Provider, detected)
	}
	setUp, run := "set up", "run"
	if dryRun {
		setUp, run = "would be set up", "would run"
	}
	fmt.Fprintf(env.Stdout, "repository templates %s (%s, ci %s) %s in %s\n", c.TemplateVersion, c.Visibility, c.Provider, setUp, root)
	if c.Visibility == "private" {
		key := "repository.ci.github.runs_on"
		if c.Provider == skenvfile.CIGitLab {
			key = "repository.ci.gitlab.tags"
		}
		fmt.Fprintf(env.Stdout, "CI jobs %s on runners %s (%s); to change them, edit it and run `skenv repo apply`\n", run, strings.Join(c.Runner, ", "), key)
	}
	printHookTools(env)
	return lefthookInstall(ctx, env, root, dryRun), nil
}

// runRepoApply is the RunE of `repo apply`.
func runRepoApply(ctx context.Context, env skills.Env, dir string, dryRun, force bool) (int, error) {
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return 0, err
	}
	c, err := skenvfile.LoadRepository(root)
	if err != nil {
		return 0, err
	}
	if c.TemplateVersion != skenvfile.LatestTemplates {
		return 0, fmt.Errorf("%s: template_version %s is not the template set of this skenv (%s); "+
			"run `skenv repo upgrade` to move the repository to %s, or use skenv %s", filepath.Base(c.File), c.TemplateVersion, skenvfile.LatestTemplates, skenvfile.LatestTemplates, c.TemplateVersion)
	}
	changes, err := repository.Apply(root, c, dryRun, force)
	printChanges(env, changes, dryRun)
	if err != nil {
		return 0, err
	}
	if len(changes) == 0 {
		fmt.Fprintln(env.Stdout, "repo apply: up to date")
	}
	return lefthookInstall(ctx, env, root, dryRun), nil
}

// runRepoUpgrade is the RunE of `repo upgrade`.
func runRepoUpgrade(ctx context.Context, env skills.Env, dir string, dryRun, force bool) (int, error) {
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return 0, err
	}
	c, err := skenvfile.LoadRepository(root)
	if err != nil {
		return 0, err
	}
	old := c.TemplateVersion
	c.TemplateVersion = skenvfile.LatestTemplates
	// Refuse (foreign files without --force) before the version moves.
	if _, err := repository.Apply(root, c, true, force); err != nil {
		return 0, err
	}
	// repository.template_version and the schema directive.
	updated, err := repository.Upgrade(root, skenvfile.LatestTemplates, dryRun)
	if err != nil {
		return 0, err
	}
	prefix := ""
	if dryRun {
		prefix = "would move "
	}
	switch {
	case old != skenvfile.LatestTemplates:
		fmt.Fprintf(env.Stdout, "%stemplate_version %s → %s\n", prefix, old, skenvfile.LatestTemplates)
	case updated:
		printChanges(env, []repository.Change{{Path: filepath.Base(c.File), Action: "update"}}, dryRun)
	default:
		fmt.Fprintf(env.Stdout, "template_version is %s already\n", skenvfile.LatestTemplates)
	}
	changes, err := repository.Apply(root, c, dryRun, force)
	printChanges(env, changes, dryRun)
	if err != nil {
		return 0, err
	}
	return lefthookInstall(ctx, env, root, dryRun), nil
}

// runRepoCheck is the RunE of `repo check`.
func runRepoCheck(ctx context.Context, env skills.Env, dir string) (int, error) {
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return 0, err
	}
	drift, err := repository.Check(root)
	if err != nil {
		return 0, err
	}
	// A missing or outdated schema directive is a warning: it only helps
	// editors, and the exit code stays.
	if c, err := skenvfile.LoadRepository(root); err == nil {
		if w, err := repository.DirectiveWarning(c); err == nil && w != "" {
			fmt.Fprintf(env.Stderr, "warning: %s: %s\n", filepath.Base(c.File), w)
		}
	}
	for _, d := range drift {
		fmt.Fprintf(env.Stdout, "%s: %s\n", d.Path, d.Reason)
	}
	if len(drift) > 0 {
		fmt.Fprintf(env.Stdout, "repo check: %d files differ; run `skenv repo apply` (templates live in skenv, not in this repository)\n", len(drift))
		return skills.ExitProblems, nil
	}
	fmt.Fprintln(env.Stdout, "repo check: managed files match the repository templates")
	return skills.ExitOK, nil
}

// detectCI is the CI system for `repo init` without --ci: gitlab when
// origin is on a GitLab host, github otherwise. The hosts declared in the
// manifest count: the [user] of the repository itself, else the manifest of
// the config file. The origin URL is never printed (it may carry
// credentials); why says what decided.
func detectCI(ctx context.Context, env skills.Env, root string) (ci, why string) {
	origin, err := gitx.Git{}.Run(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(origin) == "" {
		return skenvfile.CIGitHub, "the default, the repository has no origin"
	}
	r, err := declaredHosts(ctx, env, root).Resolve(origin)
	if err != nil {
		return skenvfile.CIGitHub, "the default, origin is not a git URL skenv recognises"
	}
	ci = repository.DetectCI(r.Type)
	if ci == skenvfile.CIGitLab {
		return ci, "detected from origin, on a GitLab host"
	}
	if r.Type == skenvfile.TypeGitHub {
		return ci, "detected from origin, on GitHub"
	}
	return ci, "the default, origin is not on a GitLab host"
}

// declaredHosts are the hosts of the manifest that applies to root, nil
// when there is none or it does not load.
func declaredHosts(ctx context.Context, env skills.Env, root string) skenvfile.Hosts {
	file, err := skenvfile.Find(root)
	if err == nil && file != "" {
		if doc, err := skenvfile.Read(file); err == nil && doc.Has(skenvfile.SectionUser) {
			m, err := skenvfile.LoadManifest(file)
			if err != nil {
				fmt.Fprintf(env.Stderr, "warning: hosts of %s not read, CI detection ignores them: %v\n", filepath.Base(file), err)
				return nil
			}
			return m.GitHosts
		}
	}
	file, err = skills.ResolveManifest(ctx, env, "")
	if err != nil {
		return nil
	}
	m, err := skenvfile.LoadManifest(file)
	if err != nil {
		fmt.Fprintf(env.Stderr, "warning: hosts of the manifest not read, CI detection ignores them: %v\n", err)
		return nil
	}
	return m.GitHosts
}

func printChanges(env skills.Env, changes []repository.Change, dryRun bool) {
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
func lefthookInstall(ctx context.Context, env skills.Env, root string, dryRun bool) int {
	if dryRun {
		fmt.Fprintln(env.Stdout, "would run lefthook install")
		return skills.ExitOK
	}
	bin, err := lookLefthook("lefthook")
	if err != nil {
		fmt.Fprintln(env.Stderr, "warning: lefthook not found; install it (brew install lefthook) and run `lefthook install`")
		return skills.ExitOK
	}
	cmd := exec.CommandContext(ctx, bin, "install")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			fmt.Fprintf(env.Stderr, "error: lefthook install failed: %s\n", strings.TrimSpace(string(out)))
			return skills.ExitProblems
		}
		fmt.Fprintf(env.Stderr, "error: lefthook install: %v\n", err)
		return skills.ExitProblems
	}
	fmt.Fprintln(env.Stdout, "lefthook install: hooks active")
	return skills.ExitOK
}

// lookLefthook is replaced in tests.
var lookLefthook = exec.LookPath

// hookTools are the programs the generated git hooks run besides skenv and
// git (templates/lefthook.yml); lookTool is replaced in tests.
var (
	hookTools = []string{"uv", "gitleaks"}
	lookTool  = exec.LookPath
)

// printHookTools lists the programs the hooks need, found or not on PATH,
// and warns about the missing ones: without them a commit in the
// repository fails.
func printHookTools(env skills.Env) {
	var found, missing []string
	if _, err := lookLefthook("lefthook"); err != nil {
		missing = append(missing, "lefthook")
	} else {
		found = append(found, "lefthook")
	}
	for _, t := range hookTools {
		if _, err := lookTool(t); err != nil {
			missing = append(missing, t)
		} else {
			found = append(found, t)
		}
	}
	list := func(names []string) string {
		if len(names) == 0 {
			return "none"
		}
		return strings.Join(names, ", ")
	}
	fmt.Fprintf(env.Stdout, "git hooks need lefthook, uv and gitleaks: found %s; missing %s\n", list(found), list(missing))
	if len(missing) > 0 {
		fmt.Fprintf(env.Stderr, "warning: the git hooks need %s, not found on PATH; install them before committing here\n", strings.Join(missing, ", "))
	}
}

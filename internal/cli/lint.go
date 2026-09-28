package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/features/lint"
	"github.com/qunaxis/skenv/internal/features/skills"
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
		return lintHook(ctx, env, stdin)
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
		report(lint.Skill(ctx, s))
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
	findings, leaks, err := lint.PublishScan(ctx, root, deny)
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
func lintHook(ctx context.Context, env skills.Env, stdin io.Reader) (int, error) {
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
	findings := lint.Skill(ctx, skill)
	if len(findings) == 0 {
		return skills.ExitOK, nil
	}
	fmt.Fprintf(env.Stderr, "skenv lint: %s has %d problems after %s; fix them:\n", skill, len(findings), event.ToolName)
	for _, f := range findings {
		fmt.Fprintln(env.Stderr, f)
	}
	return skills.ExitFatal, nil
}

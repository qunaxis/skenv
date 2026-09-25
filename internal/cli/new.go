package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/lint"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/paths"
)

// cmdNew scaffolds a skill (P3) in the own repository whose skenv.toml has
// the requested visibility (private by default), or in --dir.
func newCmd(a *app) *cobra.Command {
	var o engine.Options
	var repo, dir string
	c := &cobra.Command{
		Use:   "new <name>",
		Short: "Scaffold a skill",
		Long: `Create skills/<name>/ with SKILL.md (frontmatter) and references/ in the own
repository of the manifest whose skenv.toml has that visibility (default
private), or in the git repository at --dir.`,
		Args: nArgs(1),
	}
	c.RunE = a.action(func(ctx context.Context, env engine.Env, pos []string) (int, error) {
		return runNew(ctx, env, o, pos[0], repo, c.Flags().Changed("repo"), dir)
	})
	manifestFlag(c.Flags(), &o)
	c.Flags().StringVar(&repo, "repo", "private", "visibility of the target repository: private or public")
	_ = c.RegisterFlagCompletionFunc("repo", cobra.FixedCompletions([]string{"private", "public"}, cobra.ShellCompDirectiveNoFileComp))
	c.Flags().StringVar(&dir, "dir", "", "target repository instead of the manifest's own repositories")
	return c
}

func runNew(ctx context.Context, env engine.Env, o engine.Options, name, repo string, repoSet bool, dir string) (int, error) {
	var err error
	if err := manifest.ValidName(name); err != nil {
		return engine.ExitFatal, fmt.Errorf("skill %w", err)
	}
	if repo != "private" && repo != "public" {
		return engine.ExitFatal, usageError{fmt.Sprintf("new: --repo must be private or public, got %q", repo)}
	}

	var root, skillsDir string
	if dir != "" {
		if root, err = gitRoot(ctx, dir); err != nil {
			return engine.ExitFatal, err
		}
		skillsDir = "skills"
		// The repository's own [repo] section knows its visibility.
		c, ok, err := harness.ReadRaw(root)
		if err != nil {
			return engine.ExitFatal, err
		}
		if ok && c.Visibility != "" {
			if repoSet && c.Visibility != repo {
				return engine.ExitFatal, fmt.Errorf("--repo %s, but %s is %s according to its skenv file", repo, paths.Collapse(env.Home, root), c.Visibility)
			}
			repo = c.Visibility
		}
	} else {
		o.ReadOnly = true
		e, err := engine.Open(ctx, env, o)
		if err != nil {
			return engine.ExitFatal, err
		}
		defer e.Close()
		var matches []engine.OwnDir
		var missing []string
		for _, d := range e.OwnDirs() {
			if _, err := os.Stat(d.Path); err != nil {
				missing = append(missing, d.Repo)
				continue
			}
			c, ok, err := harness.ReadRaw(d.Path)
			if err != nil {
				return engine.ExitFatal, err
			}
			if ok && c.Visibility == repo {
				matches = append(matches, d)
			}
		}
		switch len(matches) {
		case 0:
			msg := fmt.Sprintf("no own repository in the manifest has visibility %q in its [repo] section; pass --dir", repo)
			if len(missing) > 0 {
				msg += fmt.Sprintf(" (not cloned yet: %s; run `skenv sync`)", strings.Join(missing, ", "))
			}
			return engine.ExitFatal, errors.New(msg)
		case 1:
			root, skillsDir = matches[0].Path, matches[0].SkillsDir
		default:
			var names []string
			for _, m := range matches {
				names = append(names, m.Repo)
			}
			return engine.ExitFatal, fmt.Errorf("several own repositories are %s (%s); pass --dir", repo, strings.Join(names, ", "))
		}
	}

	skill := filepath.Join(root, filepath.FromSlash(skillsDir), name)
	if _, err := os.Lstat(skill); err == nil {
		return engine.ExitFatal, fmt.Errorf("%s already exists", paths.Collapse(env.Home, skill))
	}
	title := strings.ReplaceAll(name, "-", " ")
	title = strings.ToUpper(title[:1]) + title[1:]
	files := map[string]string{
		"SKILL.md": "---\nname: " + name + "\n" +
			"description: \"TODO: what this skill does and when the agent should use it (at most 1024 characters).\"\n" +
			"metadata:\n  source: original\n---\n\n# " + title + "\n\nTODO: instructions for the agent. Put long material in references/ and link it,\nfor example [notes](references/notes.md).\n",
		"references/notes.md": "# Notes\n\nTODO: reference material, linked from SKILL.md.\n",
	}
	for rel, content := range files {
		p := filepath.Join(skill, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return engine.ExitFatal, err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return engine.ExitFatal, err
		}
	}
	fmt.Fprintf(env.Stdout, "created %s (SKILL.md, references/notes.md)\n", paths.Collapse(env.Home, skill))
	if findings := lint.Skill(skill); len(findings) > 0 {
		for _, f := range findings {
			fmt.Fprintln(env.Stderr, f)
		}
		return engine.ExitProblems, nil
	}
	hint := "fill in the description and instructions, then run `skenv lint`"
	if repo == "public" {
		hint += "; before publishing add a license (LICENSE or the license field) and run `skenv lint --publish`"
	}
	fmt.Fprintln(env.Stdout, hint)
	return engine.ExitOK, nil
}

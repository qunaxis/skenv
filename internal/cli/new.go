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

// newCmd scaffolds a skill (P3) in --dir or in a checkout of the manifest.
func newCmd(a *app) *cobra.Command {
	var o engine.Options
	var visibility, dir string
	c := &cobra.Command{
		Use:   "new <name>",
		Short: "Scaffold a skill",
		Long: `Create skills/<name>/ with SKILL.md (frontmatter) and references/ in the git
repository at --dir, or in a checkout of the manifest: the only one, else
the one whose [repository] section has --visibility (default private).
Neither needs repository templates ("skenv repo init").

A skill in a checkout of the manifest reaches your agents with "skenv
link": the skills of checkouts are linked from the working copy, nothing
to pull.

- Reads: the manifest and the skenv file of the target repository.
- Changes: creates skills/<name>/ in the target repository; nothing else.
- Network: none.
- Conflicts: an existing skills/<name> is an error.
- Preview: none.
- Next: fill in SKILL.md, "skenv lint", then "skenv link"; commit the skill.`,
		Example: `# Scaffold a skill in the git repository of the current directory
skenv new release-checklist --dir .
# Scaffold it in the checkout of the manifest
skenv new release-checklist`,
		Args: nArgs(1),
	}
	c.RunE = a.action(func(ctx context.Context, env engine.Env, pos []string) (int, error) {
		return runNew(ctx, env, o, pos[0], visibility, c.Flags().Changed("visibility"), dir)
	})
	manifestFlag(c.Flags(), &o)
	c.Flags().StringVar(&visibility, "visibility", "private", "with several checkouts: the visibility in [repository] of the target, private or public")
	_ = c.RegisterFlagCompletionFunc("visibility", cobra.FixedCompletions([]string{"private", "public"}, cobra.ShellCompDirectiveNoFileComp))
	c.Flags().StringVar(&dir, "dir", "", "target repository instead of the manifest's checkouts")
	return c
}

func runNew(ctx context.Context, env engine.Env, o engine.Options, name, visibility string, visibilitySet bool, dir string) (int, error) {
	if err := manifest.ValidName(name); err != nil {
		return engine.ExitFatal, fmt.Errorf("skill %w", err)
	}
	if visibility != "private" && visibility != "public" {
		return engine.ExitFatal, usageError{fmt.Sprintf("new: --visibility must be private or public, got %q", visibility)}
	}
	o.ReadOnly = true
	e, openErr := engine.Open(ctx, env, o)
	if openErr == nil {
		defer e.Close()
	}

	// target is the checkout of the manifest the skill goes to, nil
	// for a repository the manifest does not list.
	var target *engine.CheckoutDir
	var root, skillsDir string
	if dir != "" {
		var err error
		if root, err = gitRoot(ctx, dir); err != nil {
			return engine.ExitFatal, err
		}
		skillsDir = "skills"
		if openErr == nil {
			if target = ownAt(e.CheckoutDirs(), root); target != nil {
				skillsDir = target.SkillsDir
			}
		}
	} else {
		if openErr != nil {
			return engine.ExitFatal, openErr
		}
		d, err := ownTarget(e.CheckoutDirs(), visibility)
		if err != nil {
			return engine.ExitFatal, err
		}
		target = &d
		root, skillsDir = d.Path, d.SkillsDir
	}
	// The repository's own [repository] section knows its visibility.
	c, ok, err := harness.ReadRaw(root)
	if err != nil {
		return engine.ExitFatal, err
	}
	if ok && c.Visibility != "" {
		if visibilitySet && c.Visibility != visibility {
			return engine.ExitFatal, fmt.Errorf("--visibility %s, but %s is %s according to its skenv file", visibility, paths.Collapse(env.Home, root), c.Visibility)
		}
		visibility = c.Visibility
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
	fmt.Fprintln(env.Stdout, "next steps:")
	fmt.Fprintln(env.Stdout, "  - fill in the description and instructions, then run `skenv lint`")
	if visibility == "public" {
		fmt.Fprintln(env.Stdout, "  - before publishing, add a license (LICENSE or the license field) and run `skenv lint --publish`")
	}
	fmt.Fprintln(env.Stdout, "  - "+availability(env, name, root, target, openErr))
	return engine.ExitOK, nil
}

// availability is the step that makes the new skill name available to the
// agents: a link when the manifest installs it from target, otherwise what
// keeps it out.
func availability(env engine.Env, name, root string, target *engine.CheckoutDir, openErr error) string {
	switch {
	case errors.Is(openErr, engine.ErrNoManifest):
		return "no manifest is configured, so no agent sees the skill yet: `skenv init` starts one with this repository"
	case openErr != nil:
		return fmt.Sprintf("the manifest could not be read, so it is unknown whether agents will see the skill: %v", openErr)
	case target == nil:
		return fmt.Sprintf("%s is not a checkout of the manifest, so no agent sees the skill yet: "+
			"add it under [user.checkouts.<id>] and run `skenv sync`", paths.Collapse(env.Home, root))
	case !manifest.Selected(nil, target.Checkout.Exclude, name):
		return fmt.Sprintf("%s matches exclude of checkout %s in the manifest, so it is not installed; "+
			"change the pattern to install it", name, target.ID)
	case !target.Checkout.Selects(name):
		return fmt.Sprintf("checkout %s selects its skills with include, which does not match %s; "+
			"add it there in the manifest to install it", target.ID, name)
	}
	return "run `skenv link` to make it available to your agents"
}

// ownTarget picks the checkout that `new` writes to without --dir: the
// only one of the manifest, else the one whose [repository] section has
// visibility.
func ownTarget(dirs []engine.CheckoutDir, visibility string) (engine.CheckoutDir, error) {
	var cloned []engine.CheckoutDir
	var repos, missing []string
	for _, d := range dirs {
		repos = append(repos, d.Repo)
		if _, err := os.Stat(d.Path); err != nil {
			missing = append(missing, d.Repo)
			continue
		}
		cloned = append(cloned, d)
	}
	notCloned := ""
	if len(missing) > 0 {
		notCloned = fmt.Sprintf(" (not cloned yet: %s; run `skenv sync`)", strings.Join(missing, ", "))
	}
	switch len(dirs) {
	case 0:
		return engine.CheckoutDir{}, errors.New("the manifest has no checkout; pass --dir")
	case 1:
		if len(cloned) == 0 {
			return engine.CheckoutDir{}, errors.New("the checkout of the manifest is not cloned yet" + notCloned)
		}
		return cloned[0], nil
	}
	var matches []engine.CheckoutDir
	for _, d := range cloned {
		c, ok, err := harness.ReadRaw(d.Path)
		if err != nil {
			return engine.CheckoutDir{}, err
		}
		if ok && c.Visibility == visibility {
			matches = append(matches, d)
		}
	}
	switch len(matches) {
	case 0:
		return engine.CheckoutDir{}, fmt.Errorf("no checkout of the manifest (%s) has visibility %q in its [repository] section; pass --dir%s",
			strings.Join(repos, ", "), visibility, notCloned)
	case 1:
		return matches[0], nil
	}
	var names []string
	for _, m := range matches {
		names = append(names, m.Repo)
	}
	return engine.CheckoutDir{}, fmt.Errorf("several checkouts are %s (%s); pass --dir", visibility, strings.Join(names, ", "))
}

// ownAt returns the checkout whose working copy is root, which git reports
// with symlinks resolved.
func ownAt(dirs []engine.CheckoutDir, root string) *engine.CheckoutDir {
	for i, d := range dirs {
		if p, err := filepath.EvalSymlinks(d.Path); err == nil && p == root {
			return &dirs[i]
		}
	}
	return nil
}

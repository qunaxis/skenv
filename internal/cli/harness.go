package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/lint"
)

func cmdLint(ctx context.Context, env engine.Env, args []string) (int, error) {
	var staged bool
	fs := newFlags(env, "lint", "skenv lint [path...] [--staged]\n\nCheck skills (directories with SKILL.md) under each path (default \".\"):\nL1 frontmatter, L2 name, L3 Agent Skills limits, L4 relative links,\nL5 file size and secret-like files, L6 shebangs. Exit code 0: clean, 1: problems.")
	fs.BoolVar(&staged, "staged", false, "only skills with files changed in the git index")
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
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
	cwd, _ := os.Getwd()
	problems := 0
	for _, s := range skills {
		for _, f := range lint.Skill(s) {
			if rel, err := filepath.Rel(cwd, f.Skill); err == nil && !strings.HasPrefix(rel, "..") {
				f.Skill = rel
			}
			fmt.Fprintln(env.Stdout, f)
			problems++
		}
	}
	fmt.Fprintf(env.Stderr, "lint: %d skills, %d problems\n", len(skills), problems)
	if problems > 0 {
		return engine.ExitProblems, nil
	}
	return engine.ExitOK, nil
}

func gitRoot(ctx context.Context, dir string) (string, error) {
	root, err := gitx.Git{}.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository", dir)
	}
	return root, nil
}

func cmdRepo(ctx context.Context, env engine.Env, args []string) (int, error) {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, "Usage: skenv repo init|apply|check [--dir D]\n")
		return engine.ExitFatal, usageError{"repo: missing subcommand"}
	}
	sub, args := args[0], args[1:]
	var dir, visibility string
	var dryRun, upgrade bool
	var synopsis string
	switch sub {
	case "init":
		synopsis = "skenv repo init --visibility private|public [--dir D] [--dry-run]\n\nSet up the harness of a skills repository: skenv.toml, lefthook.yml, CI\nworkflow, linter configs and the managed blocks of AGENTS.md and .gitignore;\nthen `lefthook install`. Refuses if skenv.toml exists."
	case "apply":
		synopsis = "skenv repo apply [--upgrade] [--dir D] [--dry-run]\n\nRegenerate the managed files and blocks for the harness version in skenv.toml\n(--upgrade moves it to " + harness.Latest + " first); then `lefthook install`."
	case "check":
		synopsis = "skenv repo check [--dir D]\n\nCompare the managed files and blocks with the templates of the harness version.\nExit code 0: in sync, 1: drift (files listed), 2: error."
	default:
		return engine.ExitFatal, usageError{fmt.Sprintf("repo: unknown subcommand %q (init, apply, check)", sub)}
	}
	fs := newFlags(env, "repo "+sub, synopsis)
	fs.StringVar(&dir, "dir", ".", "repository (any directory inside it)")
	if sub == "init" {
		fs.StringVar(&visibility, "visibility", "", "private or public (required)")
	}
	if sub != "check" {
		fs.BoolVar(&dryRun, "dry-run", false, "print the plan, change nothing")
	}
	if sub == "apply" {
		fs.BoolVar(&upgrade, "upgrade", false, "move harness to "+harness.Latest+" (the templates of this skenv)")
	}
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
	}
	if err := wantArgs(fs, pos, 0); err != nil {
		return engine.ExitFatal, err
	}
	root, err := gitRoot(ctx, dir)
	if err != nil {
		return engine.ExitFatal, err
	}
	switch sub {
	case "init":
		if visibility == "" {
			fs.Usage()
			return engine.ExitFatal, usageError{"repo init: --visibility private|public is required"}
		}
		c, changes, err := harness.Init(root, visibility, dryRun)
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
		switch cmp := harness.Compare(c.Harness, harness.Latest); {
		case upgrade && cmp < 0:
			if err := harness.SetHarness(root, harness.Latest, dryRun); err != nil {
				return engine.ExitFatal, err
			}
			fmt.Fprintf(env.Stdout, "harness %s → %s\n", c.Harness, harness.Latest)
			c.Harness = harness.Latest
		case cmp < 0:
			fmt.Fprintf(env.Stderr, "note: this skenv has harness %s, the repository uses %s; `skenv repo apply --upgrade` moves to it\n", harness.Latest, c.Harness)
		}
		changes, err := harness.Apply(root, c, dryRun)
		printChanges(env, changes, dryRun)
		if err != nil {
			return engine.ExitFatal, err
		}
		if len(changes) == 0 {
			fmt.Fprintln(env.Stdout, "repo apply: up to date")
		}
		return lefthookInstall(ctx, env, root, dryRun), nil
	}
	drift, err := harness.Check(root)
	if err != nil {
		return engine.ExitFatal, err
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

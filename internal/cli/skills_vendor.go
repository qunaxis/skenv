package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/features/skills"
	"github.com/qunaxis/skenv/internal/model/skenvfile"
)

func vendorCmd(a *app) *cobra.Command {
	type flags struct {
		o       skills.Options
		project bool
	}
	// shared wires the flags every vendor subcommand shares.
	shared := func(c *cobra.Command, f *flags, dryRun string) {
		manifestFlag(c.Flags(), &f.o)
		dryRunFlag(c.Flags(), &f.o.DryRun, dryRun)
		c.Flags().BoolVar(&f.o.Adopt, "adopt", false, "move conflicting unmanaged paths to the backup directory and replace them")
		projectFlag(c.Flags(), &f.project, "edit [project] of the current repository instead of the manifest, and sync the project")
	}
	withEngine := func(f *flags, fn func(v scope, args []string) (int, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			name := cmdName(cmd)
			return a.action(func(ctx context.Context, env skills.Env, args []string) (int, error) {
				return withScope(ctx, env, f.o, name, f.project, false, func(s scope) (int, error) { return fn(s, args) })
			})(cmd, args)
		}
	}
	// pinned completes the names of the skills pinned in the manifest, or
	// in [project] with --project, leaving out the names already given.
	// It reads files only and stays silent on errors: completion must not
	// print or touch the network.
	pinned := func(f *flags, maxArgs int) cobra.CompletionFunc {
		return func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) >= maxArgs {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			env, err := newEnv(io.Discard, io.Discard)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			names, err := pinnedNames(cmd.Context(), env, f.project, f.o.Manifest)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return slices.DeleteFunc(names, func(n string) bool { return slices.Contains(args, n) }), cobra.ShellCompDirectiveNoFileComp
		}
	}
	revUsage := "commit to pin (default: HEAD of the default branch)"

	var addF flags
	var va skills.VendorAddOptions
	add := &cobra.Command{
		Use:   "add <repo>",
		Short: "Install a third-party skill, pinned to a commit",
		Long:  help("vendor_add"),
		Example: `# Pin the skill in tools/release-notes/ at HEAD of the default branch
skenv vendor add example-vendor/tools --path tools/release-notes
# A skill from a GitLab subgroup
skenv vendor add gitlab:example-org/team/tools --path release-notes
# A skill from a self-hosted host declared as "work" in the manifest
skenv vendor add work:platform/skills --path deploy
# Pin it in the current project instead
skenv vendor add example-vendor/tools --path tools/release-notes --project`,
		Args: nArgs(1),
		RunE: withEngine(&addF, func(v scope, args []string) (int, error) {
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
		Long:    help("vendor_update"),
		Example: `# Move diagrams to HEAD of the default branch of its repository
skenv vendor update diagrams
# Update every dependency
skenv vendor update
# Update every pinned skill of the current project
skenv vendor update --project`,
		Args: func(cmd *cobra.Command, args []string) error {
			if rev != "" && len(args) != 1 {
				return fmt.Errorf("%s: --rev needs exactly 1 name, got %d (see `%s --help`)", cmdName(cmd), len(args), cmd.CommandPath())
			}
			return nil
		},
		ValidArgsFunction: pinned(&updateF, math.MaxInt),
		RunE: withEngine(&updateF, func(v scope, args []string) (int, error) {
			return v.VendorUpdate(args, rev)
		}),
	}
	shared(update, &updateF, dryRunFetch)
	update.Flags().StringVar(&rev, "rev", "", revUsage)

	var rmF flags
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a third-party skill and its installed copy",
		Long:  help("vendor_remove"),
		Example: "skenv vendor remove diagrams\n" +
			"# Remove a skill pinned in the current project\n" +
			"skenv vendor remove diagrams --project",
		Args:              nArgs(1),
		ValidArgsFunction: pinned(&rmF, 1),
		RunE: withEngine(&rmF, func(v scope, args []string) (int, error) {
			return v.VendorRemove(args[0])
		}),
	}
	shared(remove, &rmF, dryRunPlain)

	c := group("vendor", "Install, update and remove third-party skills, pinned to a commit", add, update, remove)
	c.Long = help("vendor")
	c.Example = "skenv vendor add example-vendor/tools --path tools/release-notes\n" +
		"skenv vendor update\n" +
		"skenv vendor remove diagrams"
	return c
}

// pinnedNames are the names of the skills pinned in the manifest, or in the
// [project] section of the current repository.
func pinnedNames(ctx context.Context, env skills.Env, project bool, manifestFlag string) ([]string, error) {
	file, err := projectScope(ctx, env, "", project, false, manifestFlag)
	if err != nil {
		return nil, err
	}
	var deps []*skenvfile.Dependency
	if file != "" {
		p, err := skenvfile.LoadProject(file)
		if err != nil {
			return nil, err
		}
		deps = p.DependencyList()
	} else {
		file, err := skills.ResolveManifest(ctx, env, manifestFlag)
		if err != nil {
			return nil, err
		}
		m, err := skenvfile.LoadManifest(file)
		if err != nil {
			return nil, err
		}
		deps = m.DependencyList()
	}
	names := make([]string, 0, len(deps))
	for _, d := range deps {
		names = append(names, d.Name)
	}
	return names, nil
}

package cli

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/features/skills"
)

func importCmd(a *app) *cobra.Command {
	var o skills.Options
	var sync, project bool
	c := &cobra.Command{
		Use:   "import",
		Short: "Add the skills already installed on this machine, or in a project, to the skenv file",
		Long:  help("import"),
		Example: `# Show the manifest diff and the lock changes, write nothing
skenv import --dry-run
# Write the manifest and clean the lock of the skills CLI
skenv import
# The same, then take over the skills whose commit matched
skenv import --sync
# In a project: pin the skills of its skills-lock.json in [project]
skenv import --project`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
			if project {
				if o.Manifest != "" {
					return 0, errors.New("import: --project and --manifest exclude each other")
				}
				cwd, err := os.Getwd()
				if err != nil {
					return 0, err
				}
				return skills.ImportProject(ctx, env, cwd, o.DryRun, sync)
			}
			return skills.Import(ctx, env, o, sync)
		}),
	}
	manifestFlag(c.Flags(), &o)
	dryRunFlag(c.Flags(), &o.DryRun, dryRunFetch)
	projectFlag(c.Flags(), &project, "import the skills-lock.json of the current repository into its [project] section")
	c.Flags().BoolVar(&sync, "sync", false, "run skenv sync --adopt after the import; skills pinned without a matching commit stay as installed")
	return c
}

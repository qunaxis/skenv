package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/features/skills"
	"github.com/qunaxis/skenv/internal/platform/docedit"
	"github.com/qunaxis/skenv/schemas"
)

func initCmd(a *app) *cobra.Command {
	var dryRun, imp bool
	var dir, format, remote string
	c := &cobra.Command{
		Use:   "init",
		Short: "Start a manifest in a git repository",
		Long:  help("init"),
		Example: `# Start a manifest in the git repository of the current directory
skenv init
# Start one with the skills already installed here, and take them over
skenv init --import
# Start one in a repository without an origin yet, to be pushed to gitlab.com
skenv init --remote gitlab:example-group/my-skills`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errors.New("init: takes no <repo>; to connect this machine to an existing manifest: " +
					"`skenv clone <repo>` (or `skenv use <path>` for a checkout you already have)")
			}
			return nil
		},
		RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
			if err := docedit.ValidFormat(format); err != nil {
				return 0, fmt.Errorf("init: %w", err)
			}
			if imp {
				return skills.InitImport(ctx, env, dir, format, remote, dryRun)
			}
			return skills.NewManifest(ctx, env, dir, format, remote, dryRun)
		}),
	}
	c.Flags().StringVar(&dir, "dir", "", "the repository to start the manifest in (default: the current one)")
	c.Flags().StringVar(&remote, "remote", "", "for a repository without origin: its future remote, recorded as its checkout (owner/repo, gitlab:group/repo, codeberg:owner/repo or a URL)")
	formatFlag(c, &format, "format of a new file: toml, yaml or json (default toml; an existing file keeps its format)")
	dryRunFlag(c.Flags(), &dryRun, dryRunFetch)
	c.Flags().BoolVar(&imp, "import", false, "import the installed skills into the new manifest and run sync --adopt")
	return c
}

func cloneCmd(a *app) *cobra.Command {
	var dryRun bool
	var format string
	c := &cobra.Command{
		Use:   "clone <repo> [<dir>]",
		Short: "Clone a manifest repository and use its manifest on this machine",
		Long:  help("clone"),
		Example: `# Clone the manifest repository into ./skills and record it
skenv clone example-org/skills
# Into a directory of your choice
skenv clone example-org/skills ~/src/skills`,
		Args: rangeArgs(1, 2),
		RunE: a.action(func(ctx context.Context, env skills.Env, args []string) (int, error) {
			if err := docedit.ValidFormat(format); err != nil {
				return 0, fmt.Errorf("clone: %w", err)
			}
			dir := ""
			if len(args) == 2 {
				dir = args[1]
			}
			return skills.Clone(ctx, env, args[0], dir, format, dryRun)
		}),
	}
	formatFlag(c, &format, "format of a new config file: toml, yaml or json (default toml; an existing one keeps its format)")
	dryRunFlag(c.Flags(), &dryRun, dryRunPlain)
	return c
}

func useCmd(a *app) *cobra.Command {
	var dryRun bool
	var format string
	c := &cobra.Command{
		Use:   "use <path>",
		Short: "Use an existing manifest on this machine",
		Long:  help("use"),
		Example: `# Use the manifest of the repository in the current directory
skenv use .`,
		Args: nArgs(1),
		RunE: a.action(func(ctx context.Context, env skills.Env, args []string) (int, error) {
			if err := docedit.ValidFormat(format); err != nil {
				return 0, fmt.Errorf("use: %w", err)
			}
			return skills.Use(ctx, env, args[0], format, dryRun)
		}),
	}
	formatFlag(c, &format, "format of a new config file: toml, yaml or json (default toml; an existing one keeps its format)")
	dryRunFlag(c.Flags(), &dryRun, dryRunPlain)
	return c
}

func listCmd(a *app) *cobra.Command {
	o := skills.Options{ReadOnly: true}
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List the skills of the manifest and whether they are installed",
		Long:  help("list"),
		Example: `# The skills of the manifest on this machine
skenv list`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
			return withUser(ctx, env, o, func(e *skills.UserScope) (int, error) { return e.List(asJSON) })
		}),
	}
	manifestFlag(c.Flags(), &o)
	c.Flags().BoolVar(&asJSON, "json", false, "print the list as JSON")
	return c
}

func syncCmd(a *app, name string) *cobra.Command {
	var o skills.Options
	var project bool
	c := &cobra.Command{
		Use:   name,
		Short: "Apply the manifest to this machine, or sync a project",
		Long:  help("sync"),
		Example: `# Show what a sync would change
skenv sync --dry-run
# Pull, vendor and link
skenv sync
# In a project: copy its pinned skills and update the mirrors
skenv sync --project`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
			if name == "link" {
				return withUser(ctx, env, o, (*skills.UserScope).Link)
			}
			return withScope(ctx, env, o, name, project, true, scope.Sync)
		}),
	}
	if name == "link" {
		c.Short = "Create store and agent links without pulling"
		c.Long = help("link")
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
	o := skills.Options{ReadOnly: true}
	var asJSON, project bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Compare the machine with the manifest, or a project with its [project]",
		Long:  help("doctor"),
		Example: `# The machine matches the manifest
skenv doctor
# A skill link was removed by hand
skenv doctor
# In a project: a copy was edited and a mirror link removed by hand
skenv doctor --project`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
			return withScope(ctx, env, o, "doctor", project, true, func(s scope) (int, error) { return s.Doctor(asJSON) })
		}),
	}
	manifestFlag(c.Flags(), &o)
	c.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	projectFlag(c.Flags(), &project, "check the [project] section of the current repository (the default there)")
	return c
}

func configCmd(a *app) *cobra.Command {
	o := skills.Options{ReadOnly: true}
	var asJSON bool
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the effective configuration and why each skill is installed or not",
		Long:  help("config_show"),
		Example: `# Why is a skill installed on this machine, or not?
skenv config show`,
		Args: nArgs(0),
		RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
			_, source, err := skills.ManifestSource(ctx, env, o.Manifest)
			if err != nil {
				return 0, err
			}
			return withUser(ctx, env, o, func(e *skills.UserScope) (int, error) {
				return e.ConfigShow(source, asJSON)
			})
		}),
	}
	manifestFlag(show.Flags(), &o)
	show.Flags().BoolVar(&asJSON, "json", false, "print the configuration as JSON")
	c := group("config", "Show the effective configuration of this machine", show)
	c.Example = "skenv config show\nskenv config show --json"
	return c
}

func schemaCmd(a *app) *cobra.Command {
	kinds := map[string]string{"skenv": schemas.Skenv, "config": schemas.Config}
	return &cobra.Command{
		Use:       "schema [skenv|config]",
		Short:     "Print the JSON Schema of the skenv file or the tool config",
		Long:      help("schema"),
		Example:   "skenv schema > skenv.schema.json\nskenv schema config",
		Args:      rangeArgs(0, 1),
		ValidArgs: []string{"skenv", "config"},
		RunE: a.action(func(_ context.Context, env skills.Env, args []string) (int, error) {
			kind := "skenv"
			if len(args) == 1 {
				kind = args[0]
			}
			name, ok := kinds[kind]
			if !ok {
				return 0, fmt.Errorf("schema: unknown schema %q (skenv or config)", kind)
			}
			data, _ := schemas.Stamped(name, schemas.Running())
			_, err := env.Stdout.Write(data)
			return skills.ExitOK, err
		}),
	}
}

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/features/autostart"
	"github.com/qunaxis/skenv/internal/features/skills"
	"github.com/qunaxis/skenv/internal/platform/paths"
)

func autostartCmd(a *app) *cobra.Command {
	sub := func(action, short, long string) *cobra.Command {
		return &cobra.Command{
			Use:     action,
			Short:   short,
			Long:    long,
			Example: "skenv autostart " + action,
			Args:    nArgs(0),
			RunE: a.action(func(ctx context.Context, env skills.Env, _ []string) (int, error) {
				return runAutostart(ctx, env, action)
			}),
		}
	}
	c := group("autostart", "Run `skenv sync --quiet` at login and every hour",
		sub("enable", "Install and load the autostart job", help("autostart_enable")),
		sub("disable", "Unload and remove the autostart job", ""),
		sub("status", "Show whether the autostart job is installed and loaded (exit 1 if not)", ""),
	)
	c.Long = help("autostart")
	c.Example = "skenv autostart enable\nskenv autostart status\nskenv autostart disable"
	return c
}

func runAutostart(ctx context.Context, env skills.Env, action string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cfg := autostart.Config{
		GOOS: runtime.GOOS,
		Home: env.Home,
		Exe:  exe,
		Log:  paths.Layout{Home: env.Home}.AutostartLog(),
		Path: jobPath(exec.LookPath),
		UID:  os.Getuid(),
		Run:  autostart.ExecRunner,
	}
	switch action {
	case "enable":
		if err := cfg.Enable(ctx); err != nil {
			return 0, err
		}
		fmt.Fprintf(env.Stdout, "autostart enabled: %s sync --quiet at load and hourly; log %s\n", exe, paths.Collapse(env.Home, cfg.Log))
	case "disable":
		if err := cfg.Disable(ctx); err != nil {
			return 0, err
		}
		fmt.Fprintln(env.Stdout, "autostart disabled")
	default:
		st, err := cfg.Status(ctx)
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(env.Stdout, "autostart: %s\n", st.Detail)
		if !st.Installed || !st.Loaded {
			return skills.ExitProblems, nil
		}
	}
	return skills.ExitOK, nil
}

// jobPath is the PATH for the autostart job: the directory of the git
// lookPath finds plus the usual system locations.
func jobPath(lookPath func(string) (string, error)) string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if git, err := lookPath("git"); err == nil {
		add(filepath.Dir(git))
	}
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		add(d)
	}
	return strings.Join(dirs, ":")
}

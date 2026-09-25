// Package cli parses the skenv command line.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/qunaxis/skenv/internal/autostart"
	"github.com/qunaxis/skenv/internal/buildinfo"
	"github.com/qunaxis/skenv/internal/engine"
	"github.com/qunaxis/skenv/internal/gitx"
	"github.com/qunaxis/skenv/internal/paths"
)

const usage = `skenv keeps agent skills (Claude Code, Codex, pi) in sync with a manifest.

Usage:
  skenv init <owner/repo> [--path P]      clone the manifest repository and sync
  skenv sync [--adopt] [--dry-run] [--quiet]
  skenv link [--adopt] [--dry-run]
  skenv doctor [--json]                   exit 0: in sync, 1: discrepancies, 2: error
  skenv vendor add <owner/repo> [--path P] [--name N] [--rev SHA] [--dry-run]
  skenv vendor bump <name> [--rev SHA] [--dry-run]
  skenv vendor remove <name> [--dry-run]
  skenv autostart enable|disable|status
  skenv lint [path...] [--staged] [--publish]  check skills (L1-L6, P1)
  skenv new <name> [--repo private|public]      scaffold a skill
  skenv repo init --visibility private|public | apply [--upgrade] | check
  skenv version

Every command that reads the manifest accepts --manifest FILE (also
$SKENV_MANIFEST or "manifest" in ~/.config/skenv/config.toml).
Run "skenv <command> --help" for details.
`

// Main runs skenv with args (without the program name) and returns the
// exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	code, err := run(ctx, args, stdout, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return engine.ExitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "skenv: %s\n", gitx.Mask(err.Error()))
	}
	return code
}

func newEnv(stdout, stderr io.Writer) (engine.Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return engine.Env{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	host, _ := os.Hostname()
	return engine.Env{Home: home, Getenv: os.Getenv, Hostname: host, Stdout: stdout, Stderr: stderr}, nil
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func run(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return engine.ExitFatal, nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return engine.ExitOK, nil
	case "version", "--version":
		fmt.Fprintln(stdout, buildinfo.Get())
		return engine.ExitOK, nil
	}
	env, err := newEnv(stdout, stderr)
	if err != nil {
		return engine.ExitFatal, err
	}
	switch cmd {
	case "init":
		return cmdInit(ctx, env, rest)
	case "sync", "link":
		return cmdSync(ctx, env, cmd, rest)
	case "doctor":
		return cmdDoctor(ctx, env, rest)
	case "vendor":
		return cmdVendor(ctx, env, rest)
	case "autostart":
		return cmdAutostart(ctx, env, rest)
	case "lint":
		return cmdLint(ctx, env, rest)
	case "repo":
		return cmdRepo(ctx, env, rest)
	case "new":
		return cmdNew(ctx, env, rest)
	}
	fmt.Fprint(stderr, usage)
	return engine.ExitFatal, fmt.Errorf("unknown command %q", cmd)
}

// parse parses flags that may appear before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func newFlags(env engine.Env, name, synopsis string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "Usage: %s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

func manifestFlag(fs *flag.FlagSet, o *engine.Options) {
	fs.StringVar(&o.Manifest, "manifest", "", "path to env.toml")
}

func wantArgs(fs *flag.FlagSet, pos []string, n int) error {
	if len(pos) != n {
		fs.Usage()
		return usageError{fmt.Sprintf("%s: expected %d argument(s), got %d", fs.Name(), n, len(pos))}
	}
	return nil
}

func cmdInit(ctx context.Context, env engine.Env, args []string) (int, error) {
	var o engine.Options
	var dir string
	fs := newFlags(env, "init", "skenv init <owner/repo> [--path P] [--dry-run]\n\nClone the manifest repository (default ~/Personal/lab/<repo>), record its env.toml\nin ~/.config/skenv/config.toml and run sync. If the repository is already\ncloned, only the path is recorded.")
	fs.StringVar(&dir, "path", "", "where to clone the repository")
	fs.BoolVar(&o.DryRun, "dry-run", false, "print the plan, change nothing")
	fs.BoolVar(&o.Adopt, "adopt", false, "back up and replace unmanaged paths that conflict with the manifest")
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
	}
	if err := wantArgs(fs, pos, 1); err != nil {
		return engine.ExitFatal, err
	}
	return engine.Init(ctx, env, pos[0], dir, o)
}

func cmdSync(ctx context.Context, env engine.Env, name string, args []string) (int, error) {
	var o engine.Options
	synopsis := "skenv sync [--adopt] [--dry-run] [--quiet] [--manifest FILE]\n\nPull own repositories, vendor pinned skills, link everything into the store\nand agent directories, and remove managed paths that left the manifest."
	if name == "link" {
		synopsis = "skenv link [--adopt] [--dry-run] [--manifest FILE]\n\nCreate store links for own skills and agent links for every skill."
	}
	fs := newFlags(env, name, synopsis)
	manifestFlag(fs, &o)
	fs.BoolVar(&o.DryRun, "dry-run", false, "print the plan, change nothing")
	fs.BoolVar(&o.Adopt, "adopt", false, "move conflicting unmanaged paths to ~/.local/state/skenv/backup/<ts>/ and replace them")
	if name == "sync" {
		fs.BoolVar(&o.Quiet, "quiet", false, "print only warnings and errors")
	}
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
	}
	if err := wantArgs(fs, pos, 0); err != nil {
		return engine.ExitFatal, err
	}
	e, err := engine.Open(ctx, env, o)
	if err != nil {
		return engine.ExitFatal, err
	}
	defer e.Close()
	if name == "link" {
		return e.Link()
	}
	return e.Sync()
}

func cmdDoctor(ctx context.Context, env engine.Env, args []string) (int, error) {
	var o engine.Options
	var asJSON bool
	fs := newFlags(env, "doctor", "skenv doctor [--json] [--manifest FILE]\n\nCompare the machine with the manifest without changing it.\nClasses: missing, extra-managed, unmanaged, wrong-rev, broken-link, conflict,\ndirty, unpushed, behind, agent-mismatch.\nExit code: 0 in sync, 1 discrepancies, 2 error.")
	manifestFlag(fs, &o)
	fs.BoolVar(&asJSON, "json", false, "print the report as JSON")
	o.ReadOnly = true
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
	}
	if err := wantArgs(fs, pos, 0); err != nil {
		return engine.ExitFatal, err
	}
	e, err := engine.Open(ctx, env, o)
	if err != nil {
		return engine.ExitFatal, err
	}
	defer e.Close()
	return e.Doctor(asJSON)
}

func cmdVendor(ctx context.Context, env engine.Env, args []string) (int, error) {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, "Usage: skenv vendor add|bump|remove ...\n")
		return engine.ExitFatal, usageError{"vendor: missing subcommand"}
	}
	sub, args := args[0], args[1:]
	var o engine.Options
	var va engine.VendorAddOptions
	var synopsis string
	n := 1
	switch sub {
	case "add":
		synopsis = "skenv vendor add <owner/repo> [--path P] [--name N] [--rev SHA] [--dry-run]\n\nPin a third-party skill in the manifest (HEAD of the default branch unless\n--rev) and sync it. The manifest change is not committed."
	case "bump":
		synopsis = "skenv vendor bump <name> [--rev SHA] [--dry-run]\n\nMove a vendored skill to a new commit (default: HEAD), show the log, sync."
	case "remove":
		synopsis = "skenv vendor remove <name> [--dry-run]\n\nRemove a vendored skill from the manifest and its managed paths."
	default:
		return engine.ExitFatal, usageError{fmt.Sprintf("vendor: unknown subcommand %q (add, bump, remove)", sub)}
	}
	fs := newFlags(env, "vendor "+sub, synopsis)
	manifestFlag(fs, &o)
	fs.BoolVar(&o.DryRun, "dry-run", false, "print the plan, change nothing")
	fs.BoolVar(&o.Adopt, "adopt", false, "move conflicting unmanaged paths to the backup directory and replace them")
	if sub == "add" {
		fs.StringVar(&va.Path, "path", "", "directory with SKILL.md inside the repository (\".\" for the root)")
		fs.StringVar(&va.Name, "name", "", "skill name (default: last element of --path)")
	}
	if sub != "remove" {
		fs.StringVar(&va.Rev, "rev", "", "commit to pin (default: HEAD of the default branch)")
	}
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
	}
	if err := wantArgs(fs, pos, n); err != nil {
		return engine.ExitFatal, err
	}
	e, err := engine.Open(ctx, env, o)
	if err != nil {
		return engine.ExitFatal, err
	}
	defer e.Close()
	switch sub {
	case "add":
		va.Repo = pos[0]
		return e.VendorAdd(va)
	case "bump":
		return e.VendorBump(pos[0], va.Rev)
	}
	return e.VendorRemove(pos[0])
}

func cmdAutostart(ctx context.Context, env engine.Env, args []string) (int, error) {
	fs := newFlags(env, "autostart", "skenv autostart enable|disable|status\n\nRun `skenv sync --quiet` at login and every hour (macOS LaunchAgent\n"+autostart.Label+", Linux systemd user timer). Log: ~/.local/state/skenv/autostart.log.")
	pos, err := parse(fs, args)
	if err != nil {
		return engine.ExitFatal, err
	}
	if err := wantArgs(fs, pos, 1); err != nil {
		return engine.ExitFatal, err
	}
	exe, err := os.Executable()
	if err != nil {
		return engine.ExitFatal, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cfg := autostart.Config{
		GOOS: runtime.GOOS,
		Home: env.Home,
		Exe:  exe,
		Log:  paths.Layout{Home: env.Home}.AutostartLog(),
		Path: jobPath(),
		UID:  os.Getuid(),
		Run:  autostart.ExecRunner,
	}
	switch pos[0] {
	case "enable":
		if err := cfg.Enable(ctx); err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintf(env.Stdout, "autostart enabled: %s sync --quiet at load and hourly; log %s\n", exe, paths.Collapse(env.Home, cfg.Log))
	case "disable":
		if err := cfg.Disable(ctx); err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintln(env.Stdout, "autostart disabled")
	case "status":
		st, err := cfg.Status(ctx)
		if err != nil {
			return engine.ExitFatal, err
		}
		fmt.Fprintf(env.Stdout, "autostart: %s\n", st.Detail)
		if !st.Installed || !st.Loaded {
			return engine.ExitProblems, nil
		}
	default:
		return engine.ExitFatal, usageError{fmt.Sprintf("autostart: unknown action %q (enable, disable, status)", pos[0])}
	}
	return engine.ExitOK, nil
}

// jobPath is the PATH for the autostart job: the directory of the current
// git plus the usual system locations.
func jobPath() string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if git, err := execLookPath("git"); err == nil {
		add(filepath.Dir(git))
	}
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		add(d)
	}
	return strings.Join(dirs, ":")
}

var execLookPath = exec.LookPath

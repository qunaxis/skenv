package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/qunaxis/skenv/internal/autostart"
)

// compatCase is an invocation that shipped files, units or docs depend on,
// and what it must parse to: the command, the flags that were set and the
// positional arguments.
type compatCase struct {
	args  string
	cmd   string
	flags map[string]string
	pos   []string
}

// compatCases are the invocations released skenv versions documented or
// generated. They must keep parsing the same way; TestCompatCoversShipped
// fails when a template or unit uses one that is missing here.
var compatCases = []compatCase{
	// harness templates 0.2.0 and 0.3.0: lefthook, CI, AGENTS.md, Claude Code
	{args: "lint --staged", cmd: "skenv lint", flags: map[string]string{"staged": "true"}},
	{args: "lint --publish", cmd: "skenv lint", flags: map[string]string{"publish": "true"}},
	{args: "lint --hook", cmd: "skenv lint", flags: map[string]string{"hook": "true"}},
	{args: "lint", cmd: "skenv lint"},
	{args: "lint skills/demo", cmd: "skenv lint", pos: []string{"skills/demo"}},
	{args: "repo check", cmd: "skenv repo check"},
	{args: "repo apply", cmd: "skenv repo apply"},
	{args: "repo apply --upgrade", cmd: "skenv repo apply", flags: map[string]string{"upgrade": "true"}},
	{args: "new demo --dir .", cmd: "skenv new", flags: map[string]string{"dir": "."}, pos: []string{"demo"}},
	{args: "autostart enable", cmd: "skenv autostart enable"},
	{args: "autostart disable", cmd: "skenv autostart disable"},
	// autostart units (launchd ProgramArguments, systemd ExecStart)
	{args: "sync --quiet", cmd: "skenv sync", flags: map[string]string{"quiet": "true"}},
	// README and command reference
	{args: "init me/skills", cmd: "skenv init", pos: []string{"me/skills"}},
	{args: "init me/skills --path ~/src/my-skills --adopt --dry-run", cmd: "skenv init",
		flags: map[string]string{"path": "~/src/my-skills", "adopt": "true", "dry-run": "true"}, pos: []string{"me/skills"}},
	{args: "sync", cmd: "skenv sync"},
	{args: "sync --adopt --dry-run --quiet --manifest /tmp/env.toml", cmd: "skenv sync",
		flags: map[string]string{"adopt": "true", "dry-run": "true", "quiet": "true", "manifest": "/tmp/env.toml"}},
	{args: "link --adopt --dry-run", cmd: "skenv link", flags: map[string]string{"adopt": "true", "dry-run": "true"}},
	{args: "doctor", cmd: "skenv doctor"},
	{args: "doctor --json", cmd: "skenv doctor", flags: map[string]string{"json": "true"}},
	{args: "vendor add ext/tools --path tools/archify --name archify --rev 0123abc --dry-run", cmd: "skenv vendor add",
		flags: map[string]string{"path": "tools/archify", "name": "archify", "rev": "0123abc", "dry-run": "true"}, pos: []string{"ext/tools"}},
	{args: "vendor add --dry-run ext/tools", cmd: "skenv vendor add", flags: map[string]string{"dry-run": "true"}, pos: []string{"ext/tools"}},
	{args: "vendor add --path . ext/tools", cmd: "skenv vendor add", flags: map[string]string{"path": "."}, pos: []string{"ext/tools"}},
	{args: "vendor bump archify --rev 0123abc --dry-run", cmd: "skenv vendor bump",
		flags: map[string]string{"rev": "0123abc", "dry-run": "true"}, pos: []string{"archify"}},
	{args: "vendor remove archify --dry-run", cmd: "skenv vendor remove", flags: map[string]string{"dry-run": "true"}, pos: []string{"archify"}},
	{args: "autostart status", cmd: "skenv autostart status"},
	{args: "lint skills/a skills/b --staged --publish", cmd: "skenv lint",
		flags: map[string]string{"staged": "true", "publish": "true"}, pos: []string{"skills/a", "skills/b"}},
	{args: "new demo --repo public --manifest /tmp/env.toml", cmd: "skenv new",
		flags: map[string]string{"repo": "public", "manifest": "/tmp/env.toml"}, pos: []string{"demo"}},
	{args: "repo init --visibility private", cmd: "skenv repo init", flags: map[string]string{"visibility": "private"}},
	{args: "repo init --visibility public --dir /tmp/r --dry-run --force", cmd: "skenv repo init",
		flags: map[string]string{"visibility": "public", "dir": "/tmp/r", "dry-run": "true", "force": "true"}},
	{args: "repo apply --upgrade --dir /tmp/r --dry-run --force", cmd: "skenv repo apply",
		flags: map[string]string{"upgrade": "true", "dir": "/tmp/r", "dry-run": "true", "force": "true"}},
	{args: "repo check --dir /tmp/r", cmd: "skenv repo check", flags: map[string]string{"dir": "/tmp/r"}},
	// Go's flag package forms: --flag=value, -flag, -flag=value, --bool=false
	{args: "sync --quiet=true --dry-run=false", cmd: "skenv sync", flags: map[string]string{"quiet": "true", "dry-run": "false"}},
	{args: "vendor add --path=tools/archify ext/tools", cmd: "skenv vendor add", flags: map[string]string{"path": "tools/archify"}, pos: []string{"ext/tools"}},
	{args: "vendor add -- ext/tools", cmd: "skenv vendor add", pos: []string{"ext/tools"}},
}

// probe runs args through the real command tree with every action
// replaced, and reports what they parsed to.
func probe(t *testing.T, args []string) (code int, cmd *cobra.Command, pos []string, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &app{stdout: &out, stderr: &errOut, probe: func(c *cobra.Command, p []string) { cmd, pos = c, p }}
	code = execute(context.Background(), a, args)
	return code, cmd, pos, errOut.String()
}

func checkCompat(t *testing.T, c compatCase, args []string) {
	t.Helper()
	code, cmd, pos, errOut := probe(t, args)
	if code != 0 || cmd == nil {
		t.Fatalf("skenv %s: exit %d, no command ran\n%s", strings.Join(args, " "), code, errOut)
	}
	if cmd.CommandPath() != c.cmd {
		t.Errorf("skenv %s ran %q, want %q", strings.Join(args, " "), cmd.CommandPath(), c.cmd)
	}
	if !slices.Equal(pos, c.pos) && len(pos)+len(c.pos) > 0 {
		t.Errorf("skenv %s: arguments %q, want %q", strings.Join(args, " "), pos, c.pos)
	}
	var changed []string
	cmd.Flags().Visit(func(f *pflag.Flag) { changed = append(changed, f.Name) })
	for name, want := range c.flags {
		f := cmd.Flags().Lookup(name)
		if f == nil || !f.Changed {
			t.Errorf("skenv %s: --%s not set", strings.Join(args, " "), name)
			continue
		}
		if got := f.Value.String(); got != want {
			t.Errorf("skenv %s: --%s = %q, want %q", strings.Join(args, " "), name, got, want)
		}
	}
	if len(changed) != len(c.flags) {
		t.Errorf("skenv %s: flags set %q, want exactly %v", strings.Join(args, " "), changed, c.flags)
	}
}

// Every invocation keeps its meaning, both as written and with the
// single-dash long flags Go's flag package accepted (-quiet, -path=x).
func TestCompatInvocations(t *testing.T) {
	for _, c := range compatCases {
		t.Run(c.args, func(t *testing.T) {
			args := strings.Fields(c.args)
			checkCompat(t, c, args)
			single := make([]string, len(args))
			for i, a := range args {
				single[i] = a
				if strings.HasPrefix(a, "--") && len(a) > 2 {
					single[i] = a[1:]
				}
			}
			if !slices.Equal(single, args) {
				checkCompat(t, c, single)
			}
		})
	}
}

var shippedRe = regexp.MustCompile(`\bskenv ((?:lint|repo|sync|link|doctor|vendor|init|new|autostart|version)(?: (?:--?[a-z][a-z-]*|[a-z.][a-z0-9./-]*))*)`)

// shippedInvocations collects the skenv command lines of the embedded
// harness templates and of the autostart units.
func shippedInvocations(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{} // invocation → where
	root := filepath.Join("..", "harness", "templates")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// Placeholders of the docs become a sample value.
		text := strings.ReplaceAll(string(data), "<name>", "demo")
		for _, m := range shippedRe.FindAllStringSubmatch(text, -1) {
			found[m[1]] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, goos := range []string{"darwin", "linux"} {
		files, err := autostart.Config{GOOS: goos, Home: "/home/u", Exe: "/opt/skenv", Log: "/tmp/log", Path: "/usr/bin"}.Files()
		if err != nil {
			t.Fatal(err)
		}
		for name, text := range files {
			if strings.HasSuffix(name, ".plist") {
				// ProgramArguments: the binary, then one <string> per argument.
				args := regexp.MustCompile(`(?s)<key>ProgramArguments</key>\s*<array>(.*?)</array>`).FindStringSubmatch(text)
				if args == nil {
					t.Fatalf("%s: no ProgramArguments", name)
				}
				var list []string
				for _, s := range regexp.MustCompile(`<string>([^<]*)</string>`).FindAllStringSubmatch(args[1], -1)[1:] {
					list = append(list, s[1])
				}
				found[strings.Join(list, " ")] = name
			}
			if m := regexp.MustCompile(`(?m)^ExecStart="/opt/skenv" (.*)$`).FindStringSubmatch(text); m != nil {
				found[m[1]] = name
			}
		}
	}
	return found
}

// A template or unit that starts using a new invocation must add it to
// compatCases, so it is covered from then on.
func TestCompatCoversShipped(t *testing.T) {
	// lint --help: TestCompatLintHelpMentionsHook.
	known := map[string]bool{"lint --help": true}
	for _, c := range compatCases {
		known[c.args] = true
	}
	found := shippedInvocations(t)
	for _, want := range []string{"lint --staged", "lint --publish", "lint --hook", "repo check", "sync --quiet"} {
		if _, ok := found[want]; !ok {
			t.Errorf("extractor missed %q; it is broken", want)
		}
	}
	for inv, where := range found {
		t.Logf("%s: skenv %s", where, inv)
		if !known[inv] {
			t.Errorf("%s runs `skenv %s`, which is not in compatCases", where, inv)
		}
	}
}

// The Claude Code hook of harness 0.3.0 runs
// `skenv lint --help 2>&1 | grep -q -- -hook` to detect a skenv that has
// --hook; the help must keep mentioning it and exit 0.
func TestCompatLintHelpMentionsHook(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), []string{"lint", "--help"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String()+errOut.String(), "-hook") {
		t.Errorf("lint --help: exit %d, output lacks -hook:\n%s%s", code, out.String(), errOut.String())
	}
}

// Exit code 2 for usage errors, with a short message on stderr.
func TestCompatUsageErrors(t *testing.T) {
	for _, args := range []string{"", "bogus", "sync --bogus", "sync extra", "vendor", "vendor frob", "vendor add", "autostart", "autostart frob", "repo", "repo frob", "init"} {
		t.Run(args, func(t *testing.T) {
			code, cmd, _, errOut := probe(t, strings.Fields(args))
			if code != 2 || cmd != nil || errOut == "" {
				t.Errorf("skenv %s: exit %d, ran %v, stderr %q; want exit 2 with a message", args, code, cmd != nil, errOut)
			}
		})
	}
	for _, args := range []string{"--help", "-h", "help", "sync --help", "sync -h", "sync -help", "version", "--version", "completion bash", "completion zsh", "completion fish"} {
		t.Run(args, func(t *testing.T) {
			if code, _, _, errOut := probe(t, strings.Fields(args)); code != 0 {
				t.Errorf("skenv %s: exit %d\n%s", args, code, errOut)
			}
		})
	}
}

package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func runMain(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// Usage errors exit 2 with one short message on stderr; help, version
// and completion exit 0.
func TestExitCodes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range []string{"", "bogus", "sync --bogus", "sync -quiet", "sync extra", "vendor", "vendor frob", "vendor add", "autostart", "autostart frob", "repo", "repo frob", "init"} {
		code, _, errOut := runMain(strings.Fields(args)...)
		if code != 2 || errOut == "" {
			t.Errorf("skenv %s: exit %d, stderr %q; want exit 2 with a message", args, code, errOut)
		}
	}
	for _, args := range []string{"--help", "-h", "help", "sync --help", "lint --help", "version", "--version", "completion bash"} {
		if code, _, errOut := runMain(strings.Fields(args)...); code != 0 {
			t.Errorf("skenv %s: exit %d\n%s", args, code, errOut)
		}
	}
	if _, _, errOut := runMain("vendor", "add"); !strings.Contains(errOut, "vendor add: expected 1 argument(s), got 0") {
		t.Errorf("vendor add without arguments: %q", errOut)
	}
}

// Shell completion comes from the command tree: subcommands, flags and
// the fixed values of --visibility.
func TestCompletion(t *testing.T) {
	for args, want := range map[string][]string{
		"":                        {"sync", "vendor", "repo", "lint", "completion"},
		"vendor ":                 {"add", "bump", "remove"},
		"sync --":                 {"--quiet", "--dry-run", "--adopt", "--manifest"},
		"repo init --visibility ": {"private", "public"},
	} {
		fields := strings.Fields(args)
		if strings.HasSuffix(args, " ") || args == "" {
			fields = append(fields, "")
		}
		code, got, errOut := runMain(append([]string{"__complete"}, fields...)...)
		if code != 0 {
			t.Fatalf("__complete %q: exit %d\n%s", args, code, errOut)
		}
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("completion of %q lacks %s:\n%s", args, w, got)
			}
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if code, out, _ := runMain("completion", shell); code != 0 || !strings.Contains(out, "skenv") {
			t.Errorf("completion %s: exit %d", shell, code)
		}
	}
}

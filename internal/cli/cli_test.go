package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	for _, args := range []string{"", "bogus", "sync --bogus", "sync -quiet", "sync extra", "vendor", "vendor frob", "vendor add", "autostart", "autostart frob", "repo", "repo frob", "init", "completion", "completion powershell", "schema bogus", "schema skenv config"} {
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
// the fixed values of --visibility and --format.
func TestCompletion(t *testing.T) {
	for args, want := range map[string][]string{
		"":                        {"sync", "vendor", "repo", "lint", "completion"},
		"vendor ":                 {"add", "bump", "remove"},
		"sync --":                 {"--quiet", "--dry-run", "--adopt", "--manifest"},
		"repo init --visibility ": {"private", "public"},
		"repo init --format ":     {"toml", "yaml", "json"},
		"schema ":                 {"skenv", "config"},
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
	// skenv runs on darwin and linux only: no PowerShell script.
	if code, _, _ := runMain("completion", "powershell"); code != 2 {
		t.Errorf("completion powershell: exit %d, want 2", code)
	}
	code, got, _ := runMain("__complete", "completion", "")
	if code != 0 || strings.Contains(got, "powershell") {
		t.Errorf("completion of \"completion \" offers powershell:\n%s", got)
	}
	if _, help, _ := runMain("completion", "--help"); strings.Contains(help, "powershell") {
		t.Errorf("completion --help mentions powershell:\n%s", help)
	}
}

// skenv schema prints the embedded schemas; a development build names the
// unversioned URL in "$id".
func TestSchemaCommand(t *testing.T) {
	for args, want := range map[string]string{
		"schema":        `"$id": "https://qunaxis.github.io/skenv/schemas/skenv.schema.json"`,
		"schema skenv":  `"title": "skenv file"`,
		"schema config": `"$id": "https://qunaxis.github.io/skenv/schemas/config.schema.json"`,
	} {
		code, out, errOut := runMain(strings.Fields(args)...)
		if code != 0 || !strings.Contains(out, want) || !json.Valid([]byte(out)) {
			t.Errorf("skenv %s: exit %d, stderr %q, output lacks %s", args, code, errOut, want)
		}
	}
}

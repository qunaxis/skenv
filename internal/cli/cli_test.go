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

// Usage errors exit 2 with a message on stderr; help, version and
// completion exit 0.
func TestExitCodes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range []string{"bogus", "sync --bogus", "sync -quiet", "sync extra", "vendor frob", "vendor add", "vendor update a b --rev x", "autostart frob", "repo frob", "init a/b c/d", "init --format yml", "completion powershell", "schema bogus", "schema skenv config"} {
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
}

// An argument error names the argument and shows the usage line and an
// example.
func TestArgumentErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for args, want := range map[string]string{
		"vendor add": "skenv: vendor add: missing <repo>\n" +
			"Usage: skenv vendor add <repo> [flags]\n" +
			"Example: skenv vendor add example-vendor/tools --path tools/release-notes\n",
		"sync extra":          "skenv: sync: unexpected argument \"extra\"\nUsage: skenv sync [flags]\nExample: skenv sync --dry-run\n",
		"new":                 "skenv: new: missing <name>\n",
		"init a/b c/d":        "skenv: init: unexpected argument \"c/d\"\n",
		"schema skenv config": "skenv: schema: unexpected argument \"config\"\n",
		"vendor frob":         "skenv: vendor: unknown subcommand \"frob\" (add, update, remove)\n",
	} {
		code, _, errOut := runMain(strings.Fields(args)...)
		if code != 2 || !strings.HasPrefix(errOut, want) {
			t.Errorf("skenv %s: exit %d, stderr:\n%s\nwant it to start with:\n%s", args, code, errOut, want)
		}
	}
}

// The root and a group without a subcommand print their help, which lists
// the (sub)commands, and exit 0.
func TestGroupsPrintHelp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for args, want := range map[string][]string{
		"":           {"First steps", "Get started:", "Everyday:", "Write skills:", "Machine:"},
		"vendor":     {"pinned", "add ", "update ", "remove "},
		"repo":       {"init ", "apply ", "check "},
		"autostart":  {"enable ", "disable ", "status "},
		"completion": {"bash ", "zsh ", "fish "},
	} {
		code, out, errOut := runMain(strings.Fields(args)...)
		if code != 0 || errOut != "" {
			t.Errorf("skenv %s: exit %d, stderr %q", args, code, errOut)
		}
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("skenv %s: help lacks %q:\n%s", args, w, out)
			}
		}
	}
	// Configuration details live in the documentation, not the root help.
	if _, out, _ := runMain(); strings.Contains(out, "config.yml") {
		t.Errorf("root help explains the config file:\n%s", out)
	}
}

// Every command that changes something ends its help with the same
// contract.
func TestMutatingCommandsHaveAContract(t *testing.T) {
	for _, path := range []string{"init", "import", "sync", "link", "vendor add", "vendor update", "vendor remove", "new", "repo init", "repo apply", "autostart enable"} {
		_, out, _ := runMain(append(strings.Fields(path), "--help")...)
		last := ""
		for _, field := range []string{"- Reads: ", "- Changes: ", "- Network: ", "- Next: "} {
			i := strings.Index(out, "\n"+field)
			if i < 0 || i < strings.Index(out, last) {
				t.Errorf("skenv %s --help: %q missing or out of order:\n%s", path, field, out)
			}
			last = "\n" + field
		}
	}
}

// Shell completion comes from the command tree: subcommands, flags and
// the fixed values of --visibility and --format.
func TestCompletion(t *testing.T) {
	for args, want := range map[string][]string{
		"":                        {"sync", "vendor", "repo", "lint", "completion"},
		"vendor ":                 {"add", "update", "remove"},
		"sync --":                 {"--quiet", "--dry-run", "--adopt", "--manifest"},
		"repo init --visibility ": {"private", "public"},
		"repo init --format ":     {"toml", "yaml", "json"},
		"init --format ":          {"toml", "yaml", "json"},
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

// --help shows the examples, indented like the usage line, without the
// recorded output that the reference adds.
func TestHelpExamples(t *testing.T) {
	_, help, _ := runMain("doctor", "--help")
	want := "Examples:\n  # The machine matches the manifest\n  skenv doctor\n"
	if !strings.Contains(help, want) || strings.Contains(help, "ok: 3 skills") {
		t.Errorf("doctor --help:\n%s", help)
	}
}

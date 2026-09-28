package clidocs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/cli"
)

// Every skenv command and flag quoted in the repository's prose (README,
// AGENTS.md, docs/, skills/skenv/) must still exist, so a rename or
// removal fails here instead of leaving stale examples; see issue #79.
func TestProseCommandsExist(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := proseFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	cmdRoot := cli.Command()
	var findings []string
	candidates, ignored := 0, 0
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		cands, n := scanProse(f, src)
		candidates += len(cands)
		ignored += n
		for _, c := range cands {
			for _, problem := range proseResolve(cmdRoot, c.text) {
				findings = append(findings, proseFindingMessage(c, problem))
			}
		}
	}
	t.Logf("%d Markdown files scanned, %d skenv invocations, %d docs-check: ignore-next opt-outs", len(files), candidates, ignored)
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("prose commands or flags do not exist:\n%s", strings.Join(findings, "\n"))
	}
}

func TestProseResolve(t *testing.T) {
	root := cli.Command()
	for _, c := range []struct {
		name string
		text string
		want []string
	}{
		{"leaf command, no args", "skenv sync", nil},
		{"placeholder argument", "skenv vendor add <owner>/<repo> --path <skill-dir>", nil},
		{"flag with = value", "skenv init --format=yaml", nil},
		{"unknown flag with = value", "skenv init --format=yaml --bogus=1", []string{`unknown flag "--bogus=1"`}},
		{"persistent flag of a group", "skenv repo check --dir ../other", nil},
		{"persistent flag on another subcommand", "skenv repo apply --dir ../other", nil},
		{"unknown subcommand", "skenv vendor ad --rev x", []string{`unknown command "ad"`}},
		{"unknown top-level command", "skenv frobnicate", []string{`unknown command "frobnicate"`}},
		{"unknown flag on a leaf command", "skenv sync --nope", []string{`unknown flag "--nope"`}},
		{"alias resolves like the command", "skenv vendor upgrade diagrams", nil},
		{"pipe-separated subcommand alternatives", "skenv vendor add|update|remove", nil},
		{"pipe-separated alternatives, one unknown", "skenv vendor add|update|frobnicate", []string{`unknown command "frobnicate"`}},
		{"escaped pipe, as in a Markdown table cell", `skenv completion bash\|zsh\|fish`, nil},
		{"help always allowed", "skenv vendor add --help", nil},
		{"short help always allowed", "skenv sync -h", nil},
		{"trailing shell comment is ignored", "skenv sync --dry-run   # what sync would change", nil},
		{"bare skenv", "skenv", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := proseResolve(root, c.text)
			if !equalStrings(got, c.want) {
				t.Errorf("proseResolve(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScanProse(t *testing.T) {
	src := `# Title

A leaf command inline: ` + "`skenv sync --dry-run`" + ` and a prompt-form
one: ` + "`$ skenv doctor`" + `. An unrelated span: ` + "`gofmt -l .`" + ` is skipped.

` + "```sh" + `
skenv init
$ skenv list
` + "```" + `

` + "```toml" + `
skenv is not a command here, this block is ignored
` + "```" + `

<!-- docs-check: ignore-next -->
` + "```sh" + `
skenv vendor ad --nope
` + "```" + `

<!-- docs-check: ignore-next -->
` + "`skenv this is a deliberately wrong example`" + `
`

	cands, ignored := scanProse("fixture.md", []byte(src))
	var got []string
	for _, c := range cands {
		got = append(got, c.text)
	}
	want := []string{
		"skenv sync --dry-run",
		"skenv doctor",
		"skenv init",
		"skenv list",
	}
	if !equalStrings(got, want) {
		t.Errorf("scanProse candidates = %v, want %v", got, want)
	}
	if ignored != 2 {
		t.Errorf("scanProse ignored = %d, want 2", ignored)
	}
}

func TestProseFinding(t *testing.T) {
	f := proseCandidate{file: "docs/x.md", line: 12, text: "skenv vendor ad --rev x"}
	msg := proseFindingMessage(f, `unknown command "ad"`)
	want := `docs/x.md:12: skenv vendor ad --rev x: unknown command "ad"`
	if msg != want {
		t.Errorf("proseFindingMessage = %q, want %q", msg, want)
	}
	if !strings.Contains(msg, "unknown command") {
		t.Fatal("sanity: message should mention the problem")
	}
}

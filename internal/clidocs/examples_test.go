package clidocs

import (
	"testing"

	"github.com/qunaxis/skenv/internal/cliexample"
)

// The recorded output goes into the page as it is: the prose formatter
// only sees the comments. Examples without comment and output share a
// block; a non-zero exit code follows the block.
func TestExamplesSection(t *testing.T) {
	outputs := map[int]cliexample.Output{
		1: {Code: 1, Text: "--dry-run ~/.claude/skills SKILL.md \"skenv sync\"\n"},
		4: {Text: "no newline"},
	}
	got, err := examples("skenv x", "# Check with --dry-run\nskenv x --dry-run\nskenv x a\nskenv x b > out.txt\n# Last\nskenv x c", func(n int) (cliexample.Output, bool, error) {
		o, ok := outputs[n]
		return o, ok, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "### Examples\n\n" +
		"Check with `--dry-run`:\n\n" +
		"```console\n$ skenv x --dry-run\n--dry-run ~/.claude/skills SKILL.md \"skenv sync\"\n```\n\n" +
		"Exit code 1.\n\n" +
		"```console\n$ skenv x a\n$ skenv x b > out.txt\n```\n\n" +
		"Last:\n\n" +
		"```console\n$ skenv x c\nno newline\n```\n\n"
	if got != want {
		t.Errorf("examples:\n%s\nwant:\n%s", got, want)
	}
}

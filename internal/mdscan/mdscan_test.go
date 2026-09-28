package mdscan

import (
	"reflect"
	"testing"
)

func TestBlocks(t *testing.T) {
	data := "# Title\n" +
		"\n" +
		"```toml\n" +
		"a = 1\n" +
		"```\n" +
		"\n" +
		"```sh\n" +
		"skenv doctor\n" +
		"```\n" +
		"\n" +
		"" + SkipDirective + "\n" +
		"```json\n" +
		"{\"a\": 1}\n" +
		"```\n"
	got := Blocks("x.md", data, "toml", "json")
	want := []Block{
		{File: "x.md", Line: 3, Lang: "toml", Text: "a = 1\n"},
		{File: "x.md", Line: 12, Lang: "json", Text: "{\"a\": 1}\n", Skip: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Blocks() = %#v, want %#v", got, want)
	}
}

// A blank line between the marker and the fence does not carry the skip:
// the marker must be the line right before the fence.
func TestBlocksSkipDirectiveMustBeAdjacent(t *testing.T) {
	data := SkipDirective + "\n\n```toml\na = 1\n```\n"
	got := Blocks("x.md", data, "toml")
	if len(got) != 1 || got[0].Skip {
		t.Errorf("Blocks() = %#v, want a single non-skipped block", got)
	}
}

func TestBlocksLangFilter(t *testing.T) {
	data := "```yaml\na: 1\n```\n```toml\nb = 1\n```\n"
	got := Blocks("x.md", data, "toml")
	if len(got) != 1 || got[0].Lang != "toml" {
		t.Errorf("Blocks() = %#v, want only the toml block", got)
	}
}

func TestBlocksEmptyAndUnclosed(t *testing.T) {
	if got := Blocks("x.md", "```toml\n```\n", "toml"); len(got) != 1 || got[0].Text != "" {
		t.Errorf("empty block: %#v", got)
	}
	// An unclosed fence yields no block rather than panicking.
	if got := Blocks("x.md", "```toml\na = 1\n", "toml"); len(got) != 0 {
		t.Errorf("unclosed fence: %#v", got)
	}
}

package docedit

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestIsBlankOrComment(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"# a comment\n", true},
		{"  # indented comment", true},
		{"key = 1\n", false},
	} {
		if got := IsBlankOrComment(tc.line); got != tc.want {
			t.Errorf("IsBlankOrComment(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	for _, tc := range []struct {
		data string
		want []string
	}{
		{"", nil},
		{"a\n", []string{"a\n"}},
		{"a\nb\n", []string{"a\n", "b\n"}},
		{"a\nb", []string{"a\n", "b"}},
	} {
		got := SplitLines([]byte(tc.data))
		if len(got) != len(tc.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tc.data, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("SplitLines(%q)[%d] = %q, want %q", tc.data, i, got[i], tc.want[i])
			}
		}
	}
}

func TestQuote(t *testing.T) {
	if got := Quote("a\"b\\c"); got != `"a\"b\\c"` {
		t.Errorf("Quote = %s", got)
	}
}

// Quote must escape U+007F (DEL) to produce valid TOML.
func TestQuoteEscapesDEL(t *testing.T) {
	input := "hello\x7fworld"
	encoded := Quote(input)

	// Must produce valid TOML that parses back to the original.
	tomlContent := "key = " + encoded + "\n"
	var m map[string]string
	if _, err := toml.Decode(tomlContent, &m); err != nil {
		t.Fatalf("invalid TOML: %v\nTOML content: %q", err, tomlContent)
	}

	if m["key"] != input {
		t.Errorf("got %q, want %q", m["key"], input)
	}
}

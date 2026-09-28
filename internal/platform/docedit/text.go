package docedit

import (
	"fmt"
	"strings"
)

// IsBlankOrComment reports whether line, once trimmed, is empty or a "#"
// comment.
func IsBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// SplitLines splits data into lines, each keeping its line ending. A
// trailing empty element from a final newline is dropped, so
// len(SplitLines(data)) is the number of lines data ends with.
func SplitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	ls := strings.SplitAfter(string(data), "\n")
	if ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

// Quote renders s as a TOML basic string.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

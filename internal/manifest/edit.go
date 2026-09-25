package manifest

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/qunaxis/skenv/internal/atomicfile"
)

// The editing helpers below work on the manifest text rather than on the
// decoded structure so that comments, ordering and formatting survive
// `skenv vendor add|bump|remove`.

var (
	headerRe  = regexp.MustCompile(`^\s*\[`)
	vendorRe  = regexp.MustCompile(`^\s*\[\[\s*vendor\s*\]\]\s*(#.*)?$`)
	nameKeyRe = regexp.MustCompile(`^\s*name\s*=\s*"([^"]*)"`)
	revKeyRe  = regexp.MustCompile(`^(\s*rev\s*=\s*)"[^"]*"(.*)$`)
)

type block struct{ start, end int } // line range [start, end)

func splitLines(data []byte) []string {
	s := string(data)
	if s == "" {
		return nil
	}
	return strings.SplitAfter(s, "\n")
}

// vendorBlock finds the [[vendor]] table whose name is name.
func vendorBlock(lines []string, name string) (block, error) {
	for i := 0; i < len(lines); i++ {
		if !vendorRe.MatchString(lines[i]) {
			continue
		}
		end := i + 1
		for end < len(lines) && !headerRe.MatchString(lines[end]) {
			end++
		}
		for j := i + 1; j < end; j++ {
			if m := nameKeyRe.FindStringSubmatch(lines[j]); m != nil && m[1] == name {
				return block{i, end}, nil
			}
		}
		i = end - 1
	}
	return block{}, fmt.Errorf("vendor %q not found in manifest", name)
}

// AppendVendor returns data with a new [[vendor]] table appended.
func AppendVendor(data []byte, v Vendor) ([]byte, error) {
	var b bytes.Buffer
	b.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		b.WriteByte('\n')
	}
	if len(bytes.TrimSpace(data)) > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "[[vendor]]\nname = %s\nrepo = %s\npath = %s\nrev  = %s\n",
		quote(v.Name), quote(v.Repo), quote(v.Path), quote(v.Rev))
	return checked(b.Bytes())
}

// SetVendorRev returns data with the rev of vendor name replaced.
func SetVendorRev(data []byte, name, rev string) ([]byte, error) {
	lines := splitLines(data)
	blk, err := vendorBlock(lines, name)
	if err != nil {
		return nil, err
	}
	for j := blk.start + 1; j < blk.end; j++ {
		if m := revKeyRe.FindStringSubmatch(strings.TrimRight(lines[j], "\r\n")); m != nil {
			nl := lines[j][len(strings.TrimRight(lines[j], "\r\n")):]
			lines[j] = m[1] + quote(rev) + m[2] + nl
			return checked([]byte(strings.Join(lines, "")))
		}
	}
	return nil, fmt.Errorf("vendor %q has no rev line", name)
}

// RemoveVendor returns data without the [[vendor]] table for name. Comment
// and blank lines that trail the table (they usually belong to the next
// table) are kept.
func RemoveVendor(data []byte, name string) ([]byte, error) {
	lines := splitLines(data)
	blk, err := vendorBlock(lines, name)
	if err != nil {
		return nil, err
	}
	last := blk.start
	for j := blk.start + 1; j < blk.end; j++ {
		t := strings.TrimSpace(lines[j])
		if t != "" && !strings.HasPrefix(t, "#") {
			last = j
		}
	}
	start := blk.start
	// Drop one blank separator line before the table as well.
	if start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	out := append(append([]string{}, lines[:start]...), lines[last+1:]...)
	return checked([]byte(strings.Join(out, "")))
}

func checked(data []byte) ([]byte, error) {
	if _, err := Parse(data); err != nil {
		return nil, fmt.Errorf("edited manifest is invalid: %w", err)
	}
	return data, nil
}

// quote renders s as a TOML basic string.
func quote(s string) string {
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

// WriteFile atomically replaces file with data, keeping its permissions.
func WriteFile(file string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	return atomicfile.Write(file, data, mode)
}

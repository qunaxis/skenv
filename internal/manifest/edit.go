package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/qunaxis/skenv/internal/atomicfile"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// The editing helpers below work on the TOML text rather than on the
// decoded structure so that comments, ordering and formatting survive
// `skenv vendor add|bump|remove`. YAML and JSON skenv files are decoded and
// encoded again instead: their comments and key order are not kept.

var (
	headerRe  = regexp.MustCompile(`^\s*\[`)
	vendorRe  = regexp.MustCompile(`^\s*\[\[\s*environment\s*\.\s*vendor\s*\]\]\s*(#.*)?$`)
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

// vendorBlock finds the [[environment.vendor]] table whose name is name.
func vendorBlock(lines []string, name string) (block, error) {
	for i := 0; i < len(lines); i++ {
		if !vendorRe.MatchString(strings.TrimRight(lines[i], "\r\n")) {
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

// AppendVendor returns data with a new vendor entry appended.
func AppendVendor(data []byte, ext string, v Vendor) ([]byte, error) {
	if ext != ".toml" {
		return rewriteVendors(data, ext, func(list []any) ([]any, error) {
			return append(list, map[string]any{"name": v.Name, "repo": v.Repo, "path": v.Path, "rev": v.Rev}), nil
		})
	}
	var b bytes.Buffer
	b.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		b.WriteByte('\n')
	}
	if len(bytes.TrimSpace(data)) > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "[[environment.vendor]]\nname = %s\nrepo = %s\npath = %s\nrev  = %s\n",
		quote(v.Name), quote(v.Repo), quote(v.Path), quote(v.Rev))
	return checked(b.Bytes(), ext)
}

// SetVendorRev returns data with the rev of vendor name replaced.
func SetVendorRev(data []byte, ext, name, rev string) ([]byte, error) {
	if ext != ".toml" {
		return rewriteVendors(data, ext, func(list []any) ([]any, error) {
			i, err := vendorIndex(list, name)
			if err != nil {
				return nil, err
			}
			list[i].(map[string]any)["rev"] = rev
			return list, nil
		})
	}
	lines := splitLines(data)
	blk, err := vendorBlock(lines, name)
	if err != nil {
		return nil, err
	}
	for j := blk.start + 1; j < blk.end; j++ {
		if m := revKeyRe.FindStringSubmatch(strings.TrimRight(lines[j], "\r\n")); m != nil {
			nl := lines[j][len(strings.TrimRight(lines[j], "\r\n")):]
			lines[j] = m[1] + quote(rev) + m[2] + nl
			return checked([]byte(strings.Join(lines, "")), ext)
		}
	}
	return nil, fmt.Errorf("vendor %q has no rev line", name)
}

// RemoveVendor returns data without the vendor entry for name. In TOML,
// comment and blank lines that trail the table (they usually belong to the
// next table) are kept.
func RemoveVendor(data []byte, ext, name string) ([]byte, error) {
	if ext != ".toml" {
		return rewriteVendors(data, ext, func(list []any) ([]any, error) {
			i, err := vendorIndex(list, name)
			if err != nil {
				return nil, err
			}
			return append(list[:i], list[i+1:]...), nil
		})
	}
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
	return checked([]byte(strings.Join(out, "")), ext)
}

// rewriteVendors edits environment.vendor of a YAML or JSON skenv file.
func rewriteVendors(data []byte, ext string, edit func([]any) ([]any, error)) ([]byte, error) {
	out, err := skenvfile.Rewrite(data, ext, func(doc map[string]any) error {
		env, err := skenvfile.Table(doc, skenvfile.Environment)
		if err != nil {
			return err
		}
		list, _ := env["vendor"].([]any)
		if env["vendor"] != nil && list == nil {
			return errors.New("environment.vendor must be a list")
		}
		if list, err = edit(list); err != nil {
			return err
		}
		env["vendor"] = list
		return nil
	})
	if err != nil {
		return nil, err
	}
	return checked(out, ext)
}

func vendorIndex(list []any, name string) (int, error) {
	for i, v := range list {
		if m, ok := v.(map[string]any); ok && m["name"] == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("vendor %q not found in manifest", name)
}

func checked(data []byte, ext string) ([]byte, error) {
	if _, err := Parse(data, ext); err != nil {
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

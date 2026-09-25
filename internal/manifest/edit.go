package manifest

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/qunaxis/skenv/internal/atomicfile"
	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// The editing helpers below work on the TOML text rather than on the
// decoded structure so that comments, ordering and formatting survive
// `skenv vendor add|update|remove` and `skenv import`. YAML and JSON skenv files are edited with
// internal/docedit, which keeps comments and key order as well.

var (
	headerRe  = regexp.MustCompile(`^\s*\[`)
	nameKeyRe = regexp.MustCompile(`^\s*name\s*=\s*"([^"]*)"`)
	revKeyRe  = regexp.MustCompile(`^(\s*rev\s*=\s*)"[^"]*"(.*)$`)
)

// arrayRe matches the header of the array of tables <section>.<key>, as in
// [[environment.vendor]], with optional spaces and a trailing comment.
func arrayRe(section, key string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*\[\[\s*` + section + `\s*\.\s*` + key + `\s*\]\]\s*(#.*)?$`)
}

// sectionRe matches the header of any table of section: [project],
// [[project.from]], [environment.host."x"].
func sectionRe(section string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*\[\[?\s*` + section + `\s*[.\]]`)
}

type block struct{ start, end int } // line range [start, end)

func splitLines(data []byte) []string {
	s := string(data)
	if s == "" {
		return nil
	}
	return strings.SplitAfter(s, "\n")
}

// tableEnd is the end of the table whose header is at line i: the next
// header, or the end of the file.
func tableEnd(lines []string, i int) int {
	end := i + 1
	for end < len(lines) && !headerRe.MatchString(lines[end]) {
		end++
	}
	return end
}

// arrayBlocks lists the tables of the array <section>.<key> in order.
func arrayBlocks(lines []string, section, key string) []block {
	re := arrayRe(section, key)
	var out []block
	for i := 0; i < len(lines); i++ {
		if re.MatchString(strings.TrimRight(lines[i], "\r\n")) {
			end := tableEnd(lines, i)
			out = append(out, block{i, end})
			i = end - 1
		}
	}
	return out
}

// vendorBlock finds the [[<section>.vendor]] table whose name is name.
func vendorBlock(lines []string, section, name string) (block, error) {
	for _, b := range arrayBlocks(lines, section, "vendor") {
		for j := b.start + 1; j < b.end; j++ {
			if m := nameKeyRe.FindStringSubmatch(lines[j]); m != nil && m[1] == name {
				return b, nil
			}
		}
	}
	return block{}, fmt.Errorf("%s.vendor %q not found", section, name)
}

// AppendVendor returns data with a new vendor entry in section
// ("environment" or "project"). In TOML the table goes after the last table
// of the section, before the comments and blank lines that lead to the next
// one, or to the end of the file.
func AppendVendor(data []byte, ext, section string, v Vendor) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, section, func(d docedit.Doc) error {
			return d.Append([]string{section, "vendor"},
				docedit.Map{{Key: "name", Value: v.Name}, {Key: "repo", Value: v.Repo}, {Key: "path", Value: v.Path}, {Key: "rev", Value: v.Rev}})
		})
	}
	table := fmt.Sprintf("[[%s.vendor]]\nname = %s\nrepo = %s\npath = %s\nrev  = %s\n",
		section, quote(v.Name), quote(v.Repo), quote(v.Path), quote(v.Rev))
	return checked(insertTable(data, section, table), ext, section)
}

// AppendOwn returns data with a new own entry appended; skills_dir and
// skills are written only when they are set and not the default.
func AppendOwn(data []byte, ext string, o Own) ([]byte, error) {
	if ext != ".toml" {
		item := docedit.Map{{Key: "repo", Value: o.Repo}, {Key: "path", Value: o.Path}}
		if o.SkillsDir != "" && o.SkillsDir != DefaultSkillsDir {
			item = append(item, docedit.Field{Key: "skills_dir", Value: o.SkillsDir})
		}
		if o.Skills != nil {
			item = append(item, docedit.Field{Key: "skills", Value: o.Skills})
		}
		return editDoc(data, ext, skenvfile.Environment, func(d docedit.Doc) error {
			return d.Append([]string{skenvfile.Environment, "own"}, item)
		})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[[environment.own]]\nrepo = %s\npath = %s\n", quote(o.Repo), quote(o.Path))
	if o.SkillsDir != "" && o.SkillsDir != DefaultSkillsDir {
		fmt.Fprintf(&b, "skills_dir = %s\n", quote(o.SkillsDir))
	}
	if o.Skills != nil {
		q := make([]string, len(o.Skills))
		for i, n := range o.Skills {
			q[i] = quote(n)
		}
		fmt.Fprintf(&b, "skills = [%s]\n", strings.Join(q, ", "))
	}
	return checked(insertTable(data, skenvfile.Environment, b.String()), ext, skenvfile.Environment)
}

// insertTable returns TOML data with table (a complete table, header
// first) after the last table of section, before the blank lines and
// comments that lead to the next table; at the end of the file when
// section has no table. Comments right under the last table, with no
// blank line between, belong to it (such as commented keys to uncomment)
// and stay with it. A blank line separates the new table from its
// neighbours.
func insertTable(data []byte, section, table string) []byte {
	lines := splitLines(data)
	at := len(lines)
	re := sectionRe(section)
	for i := len(lines) - 1; i >= 0; i-- {
		if re.MatchString(lines[i]) {
			at = tableEnd(lines, i)
			for at > i+1 && isBlankOrComment(lines[at-1]) {
				at--
			}
			for at < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[at]), "#") && !headerRe.MatchString(lines[at]) {
				at++
			}
			break
		}
	}
	var b bytes.Buffer
	b.WriteString(strings.Join(lines[:at], ""))
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		b.WriteByte('\n')
	}
	if len(bytes.TrimSpace(b.Bytes())) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(table)
	if at < len(lines) {
		if strings.TrimSpace(lines[at]) != "" {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(lines[at:], ""))
	}
	return b.Bytes()
}

func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// SetVendorRev returns data with the rev of vendor name in section
// replaced.
func SetVendorRev(data []byte, ext, section, name, rev string) ([]byte, error) {
	if ext != ".toml" {
		i, err := vendorIndex(data, ext, section, name)
		if err != nil {
			return nil, err
		}
		return editDoc(data, ext, section, func(d docedit.Doc) error {
			return d.SetString([]any{section, "vendor", i, "rev"}, rev)
		})
	}
	lines := splitLines(data)
	blk, err := vendorBlock(lines, section, name)
	if err != nil {
		return nil, err
	}
	return setRev(lines, blk, ext, section, fmt.Sprintf("vendor %q", name), rev)
}

// SetFromRev returns data with the rev of the i-th [[project.from]] entry
// replaced.
func SetFromRev(data []byte, ext string, i int, rev string) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, skenvfile.Project, func(d docedit.Doc) error {
			return d.SetString([]any{skenvfile.Project, "from", i, "rev"}, rev)
		})
	}
	lines := splitLines(data)
	blocks := arrayBlocks(lines, skenvfile.Project, "from")
	if i < 0 || i >= len(blocks) {
		return nil, fmt.Errorf("project.from[%d] not found (write [[project.from]] tables in the multi-line form)", i)
	}
	return setRev(lines, blocks[i], ext, skenvfile.Project, fmt.Sprintf("project.from[%d]", i), rev)
}

// setRev replaces the value of the rev line of blk.
func setRev(lines []string, blk block, ext, section, what, rev string) ([]byte, error) {
	for j := blk.start + 1; j < blk.end; j++ {
		if m := revKeyRe.FindStringSubmatch(strings.TrimRight(lines[j], "\r\n")); m != nil {
			nl := lines[j][len(strings.TrimRight(lines[j], "\r\n")):]
			lines[j] = m[1] + quote(rev) + m[2] + nl
			return checked([]byte(strings.Join(lines, "")), ext, section)
		}
	}
	return nil, fmt.Errorf("%s has no rev line", what)
}

// RemoveVendor returns data without the vendor entry for name in section.
// In TOML, comment and blank lines that trail the table (they usually
// belong to the next table) are kept.
func RemoveVendor(data []byte, ext, section, name string) ([]byte, error) {
	if ext != ".toml" {
		i, err := vendorIndex(data, ext, section, name)
		if err != nil {
			return nil, err
		}
		return editDoc(data, ext, section, func(d docedit.Doc) error {
			return d.Remove([]any{section, "vendor", i})
		})
	}
	lines := splitLines(data)
	blk, err := vendorBlock(lines, section, name)
	if err != nil {
		return nil, err
	}
	last := blk.start
	for j := blk.start + 1; j < blk.end; j++ {
		if !isBlankOrComment(lines[j]) {
			last = j
		}
	}
	start := blk.start
	// Drop one blank separator line before the table as well.
	if start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	out := append(append([]string{}, lines[:start]...), lines[last+1:]...)
	return checked([]byte(strings.Join(out, "")), ext, section)
}

// editDoc edits a YAML or JSON skenv file in place.
func editDoc(data []byte, ext, section string, edit func(docedit.Doc) error) ([]byte, error) {
	d, err := docedit.Open(data, ext)
	if err != nil {
		return nil, err
	}
	if err := edit(d); err != nil {
		return nil, err
	}
	return checked(d.Bytes(), ext, section)
}

// vendorIndex is the position of vendor name in <section>.vendor.
func vendorIndex(data []byte, ext, section, name string) (int, error) {
	vendors, err := sectionVendors(data, ext, section)
	if err != nil {
		return 0, err
	}
	for i, v := range vendors {
		if v.Name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%s.vendor %q not found", section, name)
}

// sectionVendors parses section and returns its vendor entries.
func sectionVendors(data []byte, ext, section string) ([]Vendor, error) {
	if section == skenvfile.Project {
		p, err := ParseProject(data, ext)
		if err != nil {
			return nil, err
		}
		return p.Vendor, nil
	}
	m, err := Parse(data, ext)
	if err != nil {
		return nil, err
	}
	return m.Vendor, nil
}

// checked parses section of the edited file, so an edit that breaks it
// never lands.
func checked(data []byte, ext, section string) ([]byte, error) {
	if _, err := sectionVendors(data, ext, section); err != nil {
		return nil, fmt.Errorf("edited skenv file is invalid: %w", err)
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

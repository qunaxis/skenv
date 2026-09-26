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
	headerRe     = regexp.MustCompile(`^\s*\[`)
	commitKeyRe  = regexp.MustCompile(`^(\s*commit\s*=\s*)"[^"]*"(.*)$`)
	tomlKeyStart = `\s*\[\s*`
)

// tableRe matches the header of the table <section>.<key>.<name>, as in
// [user.dependencies.archify], with the name bare or double-quoted,
// optional spaces and a trailing comment.
func tableRe(section, key, name string) *regexp.Regexp {
	n := regexp.QuoteMeta(name)
	return regexp.MustCompile(`^` + tomlKeyStart + section + `\s*\.\s*` + key + `\s*\.\s*(?:` + n + `|"` + n + `")\s*\]\s*(#.*)?$`)
}

// sectionRe matches the header of any table of section: [project],
// [project.from.x], [user.machines."x"].
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

// namedBlock finds the table [<section>.<key>.<name>].
func namedBlock(lines []string, section, key, name string) (block, error) {
	re := tableRe(section, key, name)
	for i := range lines {
		if re.MatchString(strings.TrimRight(lines[i], "\r\n")) {
			return block{i, tableEnd(lines, i)}, nil
		}
	}
	return block{}, fmt.Errorf("%s.%s.%s not found (skenv edits [%s.%s.<name>] tables written in the multi-line form)", section, key, name, section, key)
}

// AppendDependency returns data with a new dependency d in section
// ("user" or "project"). In TOML the table goes after the last table of
// the section, before the comments and blank lines that lead to the next
// one, or to the end of the file.
func AppendDependency(data []byte, ext, section string, d Dependency) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, section, func(doc docedit.Doc) error {
			return doc.Put([]string{section, "dependencies"}, d.Name,
				docedit.Map{{Key: "repo", Value: d.Repo}, {Key: "skill_dir", Value: d.SkillDir}, {Key: "commit", Value: d.Commit}}, false)
		})
	}
	table := fmt.Sprintf("[%s.dependencies.%s]\nrepo      = %s\nskill_dir = %s\ncommit    = %s\n",
		section, d.Name, quote(d.Repo), quote(d.SkillDir), quote(d.Commit))
	return checked(insertTable(data, section, table), ext, section)
}

// AppendCheckout returns data with a new checkout c in [user];
// skills_dir and include are written only when they are set and not the
// default.
func AppendCheckout(data []byte, ext string, c Checkout) ([]byte, error) {
	if ext != ".toml" {
		item := docedit.Map{{Key: "repo", Value: c.Repo}, {Key: "checkout_dir", Value: c.CheckoutDir}}
		if c.SkillsDir != "" && c.SkillsDir != DefaultSkillsDir {
			item = append(item, docedit.Field{Key: "skills_dir", Value: c.SkillsDir})
		}
		if c.Branch != "" {
			item = append(item, docedit.Field{Key: "branch", Value: c.Branch})
		}
		if c.Include != nil {
			item = append(item, docedit.Field{Key: "include", Value: c.Include})
		}
		return editDoc(data, ext, skenvfile.User, func(d docedit.Doc) error {
			return d.Put([]string{skenvfile.User, "checkouts"}, c.ID, item, false)
		})
	}
	return checked(insertTable(data, skenvfile.User, checkoutTable(c)), ext, skenvfile.User)
}

// checkoutTable is the TOML table of a new checkout.
func checkoutTable(c Checkout) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[user.checkouts.%s]\nrepo         = %s\ncheckout_dir = %s\n", c.ID, quote(c.Repo), quote(c.CheckoutDir))
	if c.SkillsDir != "" && c.SkillsDir != DefaultSkillsDir {
		fmt.Fprintf(&b, "skills_dir   = %s\n", quote(c.SkillsDir))
	}
	if c.Branch != "" {
		fmt.Fprintf(&b, "branch       = %s\n", quote(c.Branch))
	}
	if c.Include != nil {
		q := make([]string, len(c.Include))
		for i, n := range c.Include {
			q[i] = quote(n)
		}
		fmt.Fprintf(&b, "include      = [%s]\n", strings.Join(q, ", "))
	}
	return b.String()
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

// SetDependencyCommit returns data with the commit of dependency name in
// section replaced.
func SetDependencyCommit(data []byte, ext, section, name, commit string) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, section, func(d docedit.Doc) error {
			return d.SetString([]any{section, "dependencies", name, "commit"}, commit)
		})
	}
	lines := splitLines(data)
	blk, err := namedBlock(lines, section, "dependencies", name)
	if err != nil {
		return nil, err
	}
	return setCommit(lines, blk, ext, section, section+".dependencies."+name, commit)
}

// SetFromCommit returns data with the commit of [project.from.<id>]
// replaced.
func SetFromCommit(data []byte, ext, id, commit string) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, skenvfile.Project, func(d docedit.Doc) error {
			return d.SetString([]any{skenvfile.Project, "from", id, "commit"}, commit)
		})
	}
	lines := splitLines(data)
	blk, err := namedBlock(lines, skenvfile.Project, "from", id)
	if err != nil {
		return nil, err
	}
	return setCommit(lines, blk, ext, skenvfile.Project, "project.from."+id, commit)
}

// setCommit replaces the value of the commit line of blk.
func setCommit(lines []string, blk block, ext, section, what, commit string) ([]byte, error) {
	for j := blk.start + 1; j < blk.end; j++ {
		if m := commitKeyRe.FindStringSubmatch(strings.TrimRight(lines[j], "\r\n")); m != nil {
			nl := lines[j][len(strings.TrimRight(lines[j], "\r\n")):]
			lines[j] = m[1] + quote(commit) + m[2] + nl
			return checked([]byte(strings.Join(lines, "")), ext, section)
		}
	}
	return nil, fmt.Errorf("%s has no commit = \"...\" line", what)
}

// RemoveDependency returns data without the dependency name in section.
// In TOML, comment and blank lines that trail the table (they usually
// belong to the next table) are kept.
func RemoveDependency(data []byte, ext, section, name string) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, section, func(d docedit.Doc) error {
			return d.Remove([]any{section, "dependencies", name})
		})
	}
	lines := splitLines(data)
	blk, err := namedBlock(lines, section, "dependencies", name)
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

// parseSection parses section ("user" or "project") of an edited file.
func parseSection(data []byte, ext, section string) error {
	if section == skenvfile.Project {
		_, err := ParseProject(data, ext)
		return err
	}
	_, err := Parse(data, ext)
	return err
}

// checked parses section of the edited file, so an edit that breaks it
// never lands.
func checked(data []byte, ext, section string) ([]byte, error) {
	if err := parseSection(data, ext, section); err != nil {
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

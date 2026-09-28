package skenvfile

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/qunaxis/skenv/internal/platform/atomicfile"
	"github.com/qunaxis/skenv/internal/platform/docedit"
)

// The editing helpers below work on the TOML text rather than on the
// decoded structure so that comments, ordering and formatting survive
// `skenv vendor add|update|remove` and `skenv import`. YAML and JSON skenv files are edited with
// internal/platform/docedit, which keeps comments and key order as well.

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

// splitLines is docedit.SplitLines, except it keeps a trailing empty
// element when data ends with a newline. insertTable sizes its blank
// separator off len(lines), and dropping that element (as
// docedit.SplitLines does) changes the number of blank lines it inserts
// before a brand new section's first table (a pre-existing quirk, #110).
// Kept byte for byte so this refactor changes no output (#69); unifying
// the two is a separate, behaviour-changing follow-up.
func splitLines(data []byte) []string {
	ls := docedit.SplitLines(data)
	if len(data) > 0 && data[len(data)-1] == '\n' {
		ls = append(ls, "")
	}
	return ls
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
		section, d.Name, docedit.Quote(d.Repo), docedit.Quote(d.SkillDir), docedit.Quote(d.Commit))
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
		return editDoc(data, ext, SectionUser, func(d docedit.Doc) error {
			return d.Put([]string{SectionUser, "checkouts"}, c.ID, item, false)
		})
	}
	return checked(insertTable(data, SectionUser, checkoutTable(c)), ext, SectionUser)
}

// checkoutTable is the TOML table of a new checkout.
func checkoutTable(c Checkout) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[user.checkouts.%s]\nrepo         = %s\ncheckout_dir = %s\n", c.ID, docedit.Quote(c.Repo), docedit.Quote(c.CheckoutDir))
	if c.SkillsDir != "" && c.SkillsDir != DefaultSkillsDir {
		fmt.Fprintf(&b, "skills_dir   = %s\n", docedit.Quote(c.SkillsDir))
	}
	if c.Branch != "" {
		fmt.Fprintf(&b, "branch       = %s\n", docedit.Quote(c.Branch))
	}
	if c.Include != nil {
		q := make([]string, len(c.Include))
		for i, n := range c.Include {
			q[i] = docedit.Quote(n)
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
	for i, line := range slices.Backward(lines) {
		if re.MatchString(line) {
			at = tableEnd(lines, i)
			for at > i+1 && docedit.IsBlankOrComment(lines[at-1]) {
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

// SetDependencyCommit returns data with the commit of dependency name in
// section replaced.
func SetDependencyCommit(data []byte, ext, section, name, commit string) ([]byte, error) {
	if ext != ".toml" {
		return editDoc(data, ext, section, func(d docedit.Doc) error {
			return d.SetString([]string{section, "dependencies", name, "commit"}, commit)
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
		return editDoc(data, ext, SectionProject, func(d docedit.Doc) error {
			return d.SetString([]string{SectionProject, "from", id, "commit"}, commit)
		})
	}
	lines := splitLines(data)
	blk, err := namedBlock(lines, SectionProject, "from", id)
	if err != nil {
		return nil, err
	}
	return setCommit(lines, blk, ext, SectionProject, "project.from."+id, commit)
}

// setCommit replaces the value of the commit line of blk.
func setCommit(lines []string, blk block, ext, section, what, commit string) ([]byte, error) {
	for j := blk.start + 1; j < blk.end; j++ {
		if m := commitKeyRe.FindStringSubmatch(strings.TrimRight(lines[j], "\r\n")); m != nil {
			nl := lines[j][len(strings.TrimRight(lines[j], "\r\n")):]
			lines[j] = m[1] + docedit.Quote(commit) + m[2] + nl
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
			return d.Remove([]string{section, "dependencies", name})
		})
	}
	lines := splitLines(data)
	blk, err := namedBlock(lines, section, "dependencies", name)
	if err != nil {
		return nil, err
	}
	last := blk.start
	for j := blk.start + 1; j < blk.end; j++ {
		if !docedit.IsBlankOrComment(lines[j]) {
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
	if section == SectionProject {
		_, err := ParseProject(data, ext)
		return err
	}
	_, err := ParseManifest(data, ext)
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

// WriteFile atomically replaces file with data, keeping its permissions.
func WriteFile(file string, data []byte) error {
	return atomicfile.Replace(file, data)
}

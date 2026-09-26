package manifest

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// Comments of the [user] section that `skenv init` adds. The TOML
// skeleton carries them inside the section; YAML gets the lead above
// the section only (JSON has no comments).
const (
	userLead = "# The manifest: `skenv sync` links the skills listed here into the agent\n" +
		"# directories of this user. Reference: https://qunaxis.github.io/skenv/manifest\n"
	userYAMLHint = "# Git repositories kept as working copies go under checkouts, third-party skills\n" +
		"# pinned to a commit under dependencies: `skenv vendor add <owner/repo> --path <dir>`\n" +
		"# adds one.\n"
	checkoutComment = "# Git repositories kept as editable working copies: every skill in\n" +
		"# <checkout_dir>/skills/ is linked. checkout_dir \".\" is this repository.\n"
	checkoutSelection = "# include = [\"<skill>\"]         # only these skills (default: all)\n" +
		"# exclude = [\"experimental-*\"]  # not these\n"
	checkoutExample = "# [user.checkouts.<id>]\n" +
		"# repo         = \"<owner>/<skills-repo>\"\n" +
		"# checkout_dir = \"~/src/<skills-repo>\"\n" + checkoutSelection
	dependencyExample = "# Third-party skills pinned to a commit; `skenv vendor add <owner/repo> --path <dir>`\n" +
		"# adds one:\n" +
		"# [user.dependencies.<skill>]\n" +
		"# repo      = \"<owner>/<repo>\"\n" +
		"# skill_dir = \"<directory of the skill in the repository>\"\n" +
		"# commit    = \"<full 40-character commit SHA>\"\n"
)

// AddUser returns data, a skenv file in the format of ext (empty for a new
// file), with a [user] section added: a commented skeleton, and c as the
// first checkout when it is not nil. The file must not have [user] yet.
// TOML is appended to as text; YAML and JSON are edited in place, so
// comments and key order of the rest stay. The caller adds the schema
// directive.
func AddUser(data []byte, ext string, c *Checkout) ([]byte, error) {
	section := userLead + "[user]\n\n" + checkoutComment
	user := docedit.Map{}
	if c != nil {
		section += checkoutTable(*c) + checkoutSelection
		user = docedit.Map{{Key: "checkouts", Value: docedit.Map{{Key: c.ID, Value: docedit.Map{{Key: "repo", Value: c.Repo}, {Key: "checkout_dir", Value: c.CheckoutDir}}}}}}
	} else {
		section += checkoutExample
	}
	out, err := addSection(data, ext, skenvfile.User, section+"\n"+dependencyExample, user, userLead+userYAMLHint)
	if err != nil {
		return nil, err
	}
	// The result must read back as a manifest.
	if _, err := Parse(out, ext); err != nil {
		return nil, fmt.Errorf("adding [user] would make the file invalid: %w", err)
	}
	return out, nil
}

// projectLead is the comment above a [project] section that `skenv import
// --project` adds.
const projectLead = "# The skills this project carries, committed with it: `skenv sync` copies the\n" +
	"# pinned ones into dir and mirrors dir. Reference: https://qunaxis.github.io/skenv/project-skills\n"

// AddProject returns data, a skenv file in the format of ext (empty for a
// new file), with a [project] section added that has the default dir and
// the given mirrors. The file must not have [project] yet; it is edited as
// AddEnvironment edits it.
func AddProject(data []byte, ext string, mirrors []string) ([]byte, error) {
	section := projectLead + "[project]\n"
	value := docedit.Map{}
	if len(mirrors) > 0 {
		q := make([]string, len(mirrors))
		for i, m := range mirrors {
			q[i] = quote(m)
		}
		section += fmt.Sprintf("mirrors = [%s]\n", strings.Join(q, ", "))
		value = docedit.Map{{Key: "mirrors", Value: mirrors}}
	}
	out, err := addSection(data, ext, skenvfile.Project, section, value, projectLead)
	if err != nil {
		return nil, err
	}
	if _, err := ParseProject(out, ext); err != nil {
		return nil, fmt.Errorf("adding [project] would make the file invalid: %w", err)
	}
	return out, nil
}

// addSection returns data with the top-level section added, which it must
// not have yet: in TOML the text toml after a blank line, in YAML and JSON
// the key with value, and in YAML lead as a comment above it. Added lines
// take the line endings of the file.
func addSection(data []byte, ext, section, toml string, value docedit.Map, lead string) ([]byte, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if doc.Has(section) {
		return nil, fmt.Errorf("the skenv file has [%s] already", section)
	}
	nl := func(s string) string {
		if bytes.Contains(data, []byte("\r\n")) {
			return strings.ReplaceAll(s, "\n", "\r\n")
		}
		return s
	}
	switch ext {
	case ".toml":
		var b strings.Builder
		b.Write(data)
		if len(bytes.TrimSpace(data)) > 0 {
			if !bytes.HasSuffix(data, []byte("\n")) {
				b.WriteString(nl("\n"))
			}
			b.WriteString(nl("\n"))
		}
		b.WriteString(nl(toml))
		return []byte(b.String()), nil
	case ".yaml", ".yml", ".json":
		d, err := docedit.Open(data, ext)
		if err != nil {
			return nil, err
		}
		if err := d.Put(nil, section, value, false); err != nil {
			return nil, err
		}
		if ext == ".json" {
			return d.Bytes(), nil
		}
		return commentYAMLKey(d.Bytes(), section, nl(lead))
	}
	return nil, fmt.Errorf("unsupported format %q", ext)
}

// commentYAMLKey inserts comment lines above the top-level key of a block
// mapping; a flow-style document is returned unchanged.
func commentYAMLKey(data []byte, key, comment string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return data, nil
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode || m.Style&yaml.FlowStyle != 0 {
		return data, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Value != key || k.Column != 1 {
			continue
		}
		lines := strings.SplitAfter(string(data), "\n")
		at := k.Line - 1
		out := strings.Join(lines[:at], "") + comment + strings.Join(lines[at:], "")
		return []byte(out), nil
	}
	return data, nil
}

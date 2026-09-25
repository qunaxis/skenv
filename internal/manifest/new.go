package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/internal/skenvfile"
)

// Comments of the [environment] section that `skenv init` adds. The TOML
// skeleton carries them inside the section; YAML gets the lead above
// the section only (JSON has no comments).
const (
	envLead = "# The manifest of your machines: `skenv sync` links the skills listed here\n" +
		"# into the agent directories. Reference: https://qunaxis.github.io/skenv/manifest\n"
	envYAMLHint = "# Own repositories go under own, third-party skills pinned to a commit under\n" +
		"# vendor: `skenv vendor add <owner/repo> --path <dir>` adds one.\n"
	ownComment = "# Your own skills repositories, kept as working copies: every skill in\n" +
		"# <path>/skills/ is linked.\n"
	ownSelection = "# skills  = [\"<skill>\"]         # only these skills (default: all)\n" +
		"# exclude = [\"experimental-*\"]  # not these\n"
	ownExample = "# [[environment.own]]\n" +
		"# repo = \"<owner>/<skills-repo>\"\n" +
		"# path = \"~/src/<skills-repo>\"\n" + ownSelection
	vendorExample = "# Third-party skills pinned to a commit; `skenv vendor add <owner/repo> --path <dir>`\n" +
		"# adds one:\n" +
		"# [[environment.vendor]]\n" +
		"# name = \"<skill>\"\n" +
		"# repo = \"<owner>/<repo>\"\n" +
		"# path = \"<directory of the skill in the repository>\"\n" +
		"# rev  = \"<full 40-character commit SHA>\"\n"
)

// AddEnvironment returns data, a skenv file in the format of ext (empty
// for a new file), with an [environment] section added: a commented
// skeleton, and own as the first [[environment.own]] entry when it is not
// nil. The file must not have [environment] yet. TOML is appended to as
// text; YAML and JSON are edited in place, so comments and key order of
// the rest stay. The caller adds the schema directive.
func AddEnvironment(data []byte, ext string, own *Own) ([]byte, error) {
	doc, err := skenvfile.Parse(data, ext)
	if err != nil {
		return nil, err
	}
	if doc.Has(skenvfile.Environment) {
		return nil, errors.New("the skenv file has [environment] already")
	}
	// Added lines take the line endings of the file.
	nl := func(s string) string {
		if bytes.Contains(data, []byte("\r\n")) {
			return strings.ReplaceAll(s, "\n", "\r\n")
		}
		return s
	}
	var out []byte
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
		section := envLead + "[environment]\n\n" + ownComment
		if own != nil {
			section += fmt.Sprintf("[[environment.own]]\nrepo = %s\npath = %s\n", quote(own.Repo), quote(own.Path)) + ownSelection
		} else {
			section += ownExample
		}
		b.WriteString(nl(section + "\n" + vendorExample))
		out = []byte(b.String())
	case ".yaml", ".yml", ".json":
		d, err := docedit.Open(data, ext)
		if err != nil {
			return nil, err
		}
		env := docedit.Map{}
		if own != nil {
			env = docedit.Map{{Key: "own", Value: []docedit.Map{{{Key: "repo", Value: own.Repo}, {Key: "path", Value: own.Path}}}}}
		}
		if err := d.Put(nil, skenvfile.Environment, env, false); err != nil {
			return nil, err
		}
		out = d.Bytes()
		if ext != ".json" {
			if out, err = commentYAMLKey(out, skenvfile.Environment, nl(envLead+envYAMLHint)); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unsupported format %q", ext)
	}
	// The result must read back as a manifest.
	if _, err := Parse(out, ext); err != nil {
		return nil, fmt.Errorf("adding [environment] would make the file invalid: %w", err)
	}
	return out, nil
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

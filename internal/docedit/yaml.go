package docedit

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// yamlDoc edits YAML as lines of text. Each operation finds its node with
// the parser, splices the text at the reported line and column, and parses
// the result again, so later operations see fresh positions.
type yamlDoc struct {
	lines []string // with their line endings
	root  *yaml.Node
	nl    string // line ending for new lines
	unit  int    // indentation step of nested blocks
}

func openYAML(data []byte) (*yamlDoc, error) {
	d := &yamlDoc{nl: "\n"}
	if strings.Contains(string(data), "\r\n") {
		d.nl = "\r\n"
	}
	return d, d.load(string(data))
}

func (d *yamlDoc) load(text string) error {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(text), &n); err != nil {
		return err
	}
	d.lines = nil
	if text != "" {
		d.lines = strings.SplitAfter(text, "\n")
		if d.lines[len(d.lines)-1] == "" {
			d.lines = d.lines[:len(d.lines)-1]
		}
	}
	d.root = nil
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		r := n.Content[0]
		if r.Kind == yaml.ScalarNode && r.Tag == "!!null" {
			return nil
		}
		if r.Kind != yaml.MappingNode {
			return errors.New("the top level must be a mapping")
		}
		d.root = r
	}
	d.unit = 2
	if d.root != nil {
		for i := 1; i < len(d.root.Content); i += 2 {
			v := d.root.Content[i]
			if (v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode) && v.Style&yaml.FlowStyle == 0 && v.Column > 1 {
				d.unit = v.Column - 1
				break
			}
		}
	}
	return nil
}

func (d *yamlDoc) Bytes() []byte { return []byte(strings.Join(d.lines, "")) }

// splice replaces lines [from, to) with repl and parses the result.
func (d *yamlDoc) splice(from, to int, repl []string) error {
	if from > 0 && !strings.HasSuffix(d.lines[from-1], "\n") {
		d.lines[from-1] += d.nl
	}
	for i := range repl {
		if !strings.HasSuffix(repl[i], "\n") {
			repl[i] += d.nl
		}
	}
	out := append(append(append([]string{}, d.lines[:from]...), repl...), d.lines[to:]...)
	if err := d.load(strings.Join(out, "")); err != nil {
		return fmt.Errorf("docedit: the edit produced invalid YAML: %w", err)
	}
	return nil
}

// pair returns the key and value nodes of key in mapping m.
func pair(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// find returns the node at path and, for a mapping value, its key node.
func (d *yamlDoc) find(path []any) (key, val *yaml.Node, err error) {
	cur := d.root
	if cur == nil && len(path) > 0 {
		return nil, nil, fmt.Errorf("%s does not exist", pathString(path[:1]))
	}
	for i, p := range path {
		key = nil
		switch p := p.(type) {
		case string:
			if cur.Kind != yaml.MappingNode {
				return nil, nil, fmt.Errorf("%s is not a mapping", pathString(path[:i]))
			}
			key, cur = pair(cur, p)
			if cur == nil {
				return nil, nil, fmt.Errorf("%s does not exist", pathString(path[:i+1]))
			}
		case int:
			if cur.Kind != yaml.SequenceNode {
				return nil, nil, fmt.Errorf("%s is not a list", pathString(path[:i]))
			}
			if p < 0 || p >= len(cur.Content) {
				return nil, nil, fmt.Errorf("%s does not exist", pathString(path[:i+1]))
			}
			cur = cur.Content[p]
		}
	}
	return key, cur, nil
}

func isFlow(n *yaml.Node) bool { return n.Style&yaml.FlowStyle != 0 }

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

// byteOffset converts the 1-based character column of the parser into a
// byte offset in line.
func byteOffset(line string, col int) int {
	off := 0
	for i := 1; i < col && off < len(line); i++ {
		_, size := utf8.DecodeRuneInString(line[off:])
		off += size
	}
	return off
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

func isDash(line string) bool {
	t := strings.TrimSpace(line)
	return t == "-" || strings.HasPrefix(t, "- ")
}

// blockEnd returns the last line (0-based) with content of the node that
// starts at line start and is owned by column col: later lines belong to it
// while they are indented deeper, or, with dashAtCol, are items of a
// sequence written at col itself. Trailing comments and blank lines are not
// part of it.
func (d *yamlDoc) blockEnd(start, col int, dashAtCol bool) int {
	last := start
	for i := start + 1; i < len(d.lines); i++ {
		l := d.lines[i]
		if isBlankOrComment(l) {
			continue
		}
		if ind := indentOf(l); ind > col || (dashAtCol && ind == col && isDash(l)) {
			last = i
			continue
		}
		break
	}
	return last
}

// entryEnd is the last line of the mapping entry key: val.
func (d *yamlDoc) entryEnd(key, val *yaml.Node) int {
	col := key.Column - 1
	indentless := val.Kind == yaml.SequenceNode && !isFlow(val) && len(val.Content) > 0 && val.Column-1 == col
	return d.blockEnd(key.Line-1, col, indentless)
}

// yamlScalar renders s as a YAML scalar: plain when it reads back as the
// same string, double-quoted otherwise.
func yamlScalar(s string) string {
	out, err := yaml.Marshal(s)
	t := strings.TrimSuffix(string(out), "\n")
	if err != nil || strings.Contains(t, "\n") || t == "" {
		return jsonString(s)
	}
	return t
}

func flowScalar(s string) string {
	t := yamlScalar(s)
	if strings.ContainsAny(t, ",[]{}#") && !strings.HasPrefix(t, `"`) && !strings.HasPrefix(t, `'`) {
		return jsonString(s)
	}
	return t
}

func pad(n int) string { return strings.Repeat(" ", n) }

// entryLines renders key: value at column col.
func (d *yamlDoc) entryLines(key string, v any, col int) ([]string, error) {
	head := pad(col) + yamlScalar(key) + ":"
	switch v := v.(type) {
	case string:
		return []string{head + " " + yamlScalar(v)}, nil
	case []string:
		items := make([]string, len(v))
		for i, s := range v {
			items[i] = flowScalar(s)
		}
		return []string{head + " [" + strings.Join(items, ", ") + "]"}, nil
	case Map:
		if len(v) == 0 {
			return []string{head + " {}"}, nil
		}
		out := []string{head}
		for _, f := range v {
			l, err := d.entryLines(f.Key, f.Value, col+d.unit)
			if err != nil {
				return nil, err
			}
			out = append(out, l...)
		}
		return out, nil
	case []Map:
		if len(v) == 0 {
			return []string{head + " []"}, nil
		}
		out := []string{head}
		for _, m := range v {
			l, err := d.itemLines(m, col+d.unit, col+d.unit+2)
			if err != nil {
				return nil, err
			}
			out = append(out, l...)
		}
		return out, nil
	}
	return nil, fmt.Errorf("docedit: unsupported value %T", v)
}

// itemLines renders a sequence item: the dash at dashCol, the keys at col.
func (d *yamlDoc) itemLines(m Map, dashCol, col int) ([]string, error) {
	if len(m) == 0 {
		return []string{pad(dashCol) + "- {}"}, nil
	}
	var out []string
	for _, f := range m {
		l, err := d.entryLines(f.Key, f.Value, col)
		if err != nil {
			return nil, err
		}
		out = append(out, l...)
	}
	out[0] = pad(dashCol) + "-" + pad(col-dashCol-1) + out[0][col:]
	return out, nil
}

func (d *yamlDoc) SetString(path []any, value string) error {
	_, v, err := d.find(path)
	if err != nil {
		return err
	}
	if v.Kind != yaml.ScalarNode || isNull(v) || v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return fmt.Errorf("%s must be a string on one line", pathString(path))
	}
	i := v.Line - 1
	line := d.lines[i]
	start := byteOffset(line, v.Column)
	end := -1
	var repl string
	switch {
	case v.Style&yaml.DoubleQuotedStyle != 0:
		for j := start + 1; j < len(line); j++ {
			if line[j] == '\\' {
				j++
				continue
			}
			if line[j] == '"' {
				end = j + 1
				break
			}
		}
		repl = jsonString(value)
	case v.Style&yaml.SingleQuotedStyle != 0:
		for j := start + 1; j < len(line); j++ {
			if line[j] == '\'' {
				if j+1 < len(line) && line[j+1] == '\'' {
					j++
					continue
				}
				end = j + 1
				break
			}
		}
		repl = "'" + strings.ReplaceAll(value, "'", "''") + "'"
	default:
		rest := strings.TrimRight(line[start:], "\r\n")
		if k := strings.Index(rest, " #"); k >= 0 {
			rest = rest[:k]
		}
		rest = strings.TrimRight(rest, " \t")
		if rest != v.Value {
			break
		}
		end = start + len(rest)
		repl = yamlScalar(value)
	}
	if end < 0 {
		return fmt.Errorf("%s: skenv can only edit a string written on one line", pathString(path))
	}
	return d.splice(i, i+1, []string{line[:start] + repl + line[end:]})
}

func (d *yamlDoc) Append(path []string, item Map) error {
	parent, key := path[:len(path)-1], path[len(path)-1]
	_, pm, err := d.find(keysPath(parent))
	if err != nil || d.root == nil {
		return d.Put(parent, key, []Map{item}, false)
	}
	if pm.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", pathString(keysPath(parent)))
	}
	k, v := pair(pm, key)
	switch {
	case v == nil:
		return d.Put(parent, key, []Map{item}, false)
	case v.Kind == yaml.SequenceNode && !isFlow(v) && len(v.Content) > 0:
		last := v.Content[len(v.Content)-1]
		dashCol := v.Column - 1
		lines, err := d.itemLines(item, dashCol, last.Column-1)
		if err != nil {
			return err
		}
		end := d.blockEnd(last.Line-1, dashCol, false)
		return d.splice(end+1, end+1, lines)
	case (v.Kind == yaml.SequenceNode && isFlow(v) && len(v.Content) == 0) || (isNull(v) && v.Value == ""):
		dashCol := k.Column - 1 + d.unit
		lines, err := d.itemLines(item, dashCol, dashCol+2)
		if err != nil {
			return err
		}
		return d.replaceEmpty(k, v, "[", "]", lines)
	case v.Kind == yaml.SequenceNode && isFlow(v):
		return fmt.Errorf("%s is written in flow style ([...]); write it as a block list (one \"- \" item per entry) so that skenv can edit it", pathString(keysPath(path)))
	}
	return fmt.Errorf("%s must be a list", pathString(keysPath(path)))
}

// replaceEmpty turns an empty flow collection (or an implicit null) on the
// line of key into a block: the brackets go, lines follow the key line.
func (d *yamlDoc) replaceEmpty(k, v *yaml.Node, open, closing string, lines []string) error {
	i := k.Line - 1
	line := d.lines[i]
	if !isNull(v) {
		if v.Line != k.Line {
			return fmt.Errorf("%s: skenv can only edit an empty %s%s on the line of its key", k.Value, open, closing)
		}
		start := byteOffset(line, v.Column)
		end := strings.Index(line[start:], closing)
		if !strings.HasPrefix(line[start:], open) || end < 0 {
			return fmt.Errorf("%s: unexpected text at the value", k.Value)
		}
		before := strings.TrimRight(line[:start], " \t")
		line = before + line[start+end+1:]
	}
	return d.splice(i, i+1, append([]string{line}, lines...))
}

func (d *yamlDoc) Remove(path []any) error {
	idx, ok := path[len(path)-1].(int)
	if !ok {
		return errors.New("docedit: Remove needs an index")
	}
	k, seq, err := d.find(path[:len(path)-1])
	if err != nil {
		return err
	}
	if seq.Kind != yaml.SequenceNode || idx < 0 || idx >= len(seq.Content) {
		return fmt.Errorf("%s does not exist", pathString(path))
	}
	if isFlow(seq) {
		return fmt.Errorf("%s is written in flow style ([...]); write it as a block list so that skenv can edit it", pathString(path[:len(path)-1]))
	}
	item := seq.Content[idx]
	dashCol := seq.Column - 1
	start := item.Line - 1
	for start > 0 && (indentOf(d.lines[start]) != dashCol || !isDash(d.lines[start])) {
		start--
	}
	end := d.blockEnd(item.Line-1, dashCol, false)
	if len(seq.Content) > 1 || k == nil {
		return d.splice(start, end+1, nil)
	}
	// The last item: leave an empty list behind rather than a null.
	kl := k.Line - 1
	line := d.lines[kl]
	at := byteOffset(line, k.Column)
	switch {
	case k.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
		q := line[at]
		at = strings.IndexByte(line[at+1:], q) + at + 2
	default:
		at += len(k.Value)
	}
	colon := strings.IndexByte(line[at:], ':')
	if colon < 0 || strings.TrimSpace(line[at:at+colon]) != "" {
		return fmt.Errorf("%s: cannot find the colon after the key", k.Value)
	}
	at += colon + 1
	keyLine := line[:at] + " []" + line[at:]
	repl := append(append([]string{}, d.lines[kl+1:start]...), d.lines[end+1:]...)
	return d.splice(kl, len(d.lines), append([]string{keyLine}, repl...))
}

func (d *yamlDoc) Put(path []string, key string, value any, first bool) error {
	// nest wraps key: value into the mappings of path[from:].
	nest := func(from int) Map {
		nested := Map{{key, value}}
		for j := len(path) - 1; j >= from; j-- {
			nested = Map{{path[j], nested}}
		}
		return nested
	}
	if d.root == nil {
		if len(path) == 0 {
			return d.putIn(nil, nil, key, value, first)
		}
		return d.putIn(nil, nil, path[0], nest(1), first)
	}
	m := d.root
	for i, p := range path {
		if m.Kind != yaml.MappingNode {
			return fmt.Errorf("%s must be a mapping", pathString(keysPath(path[:i])))
		}
		k, v := pair(m, p)
		switch {
		case v == nil:
			// Create the missing mappings as one nested value.
			return d.putIn(m, path[:i], p, nest(i+1), false)
		case (isNull(v) && v.Value == "") || (v.Kind == yaml.MappingNode && isFlow(v) && len(v.Content) == 0):
			lines, err := d.blockLines(nest(i+1), k.Column-1+d.unit)
			if err != nil {
				return err
			}
			return d.replaceEmpty(k, v, "{", "}", lines)
		}
		m = v
	}
	return d.putIn(m, path, key, value, first)
}

// blockLines renders the entries of m at col.
func (d *yamlDoc) blockLines(m Map, col int) ([]string, error) {
	var out []string
	for _, f := range m {
		l, err := d.entryLines(f.Key, f.Value, col)
		if err != nil {
			return nil, err
		}
		out = append(out, l...)
	}
	return out, nil
}

// putIn adds key: value to the existing mapping m (nil: the empty top
// level) at path.
func (d *yamlDoc) putIn(m *yaml.Node, path []string, key string, value any, first bool) error {
	if m == nil {
		lines, err := d.entryLines(key, value, 0)
		if err != nil {
			return err
		}
		return d.splice(len(d.lines), len(d.lines), lines)
	}
	if m.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", pathString(keysPath(path)))
	}
	if isFlow(m) {
		return fmt.Errorf("%s is written in flow style ({...}); write it as a block mapping so that skenv can edit it", pathString(keysPath(path)))
	}
	if k, _ := pair(m, key); k != nil {
		return fmt.Errorf("%s exists already", pathString(keysPath(append(append([]string{}, path...), key))))
	}
	col := m.Column - 1
	lines, err := d.entryLines(key, value, col)
	if err != nil {
		return err
	}
	var at int
	switch {
	case len(m.Content) == 0:
		at = len(d.lines)
	case first && m.Content[0].Value == SchemaKey && key != SchemaKey:
		at = d.entryEnd(m.Content[0], m.Content[1]) + 1
	case first:
		at = m.Content[0].Line - 1
		// Comments right above the first key belong to it.
		for at > 0 && indentOf(d.lines[at-1]) == col && strings.HasPrefix(strings.TrimSpace(d.lines[at-1]), "#") &&
			!yamlDirectiveRe.MatchString(d.lines[at-1]) {
			at--
		}
	default:
		at = d.entryEnd(m.Content[len(m.Content)-2], m.Content[len(m.Content)-1]) + 1
	}
	return d.splice(at, at, lines)
}

package docedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// jnode is a JSON value that remembers the order of object keys.
type jnode struct {
	kind  byte // 'o' object, 'a' array, 's' scalar
	keys  []string
	vals  []*jnode // object values, or array items
	raw   string   // scalar: its JSON text
	isStr bool     // scalar is a string
	str   string   // its value
}

type jsonDoc struct{ root *jnode }

func openJSON(data []byte) (*jsonDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &jsonDoc{root: &jnode{kind: 'o'}}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	root, err := decodeJSON(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the top-level value")
	}
	if root.kind != 'o' {
		return nil, errors.New("the top level must be an object")
	}
	return &jsonDoc{root: root}, nil
}

func decodeJSON(dec *json.Decoder) (*jnode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		n := &jnode{kind: 'a'}
		if t == '{' {
			n.kind = 'o'
		}
		for dec.More() {
			if n.kind == 'o' {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, k.(string))
			}
			v, err := decodeJSON(dec)
			if err != nil {
				return nil, err
			}
			n.vals = append(n.vals, v)
		}
		if _, err := dec.Token(); err != nil { // the closing delimiter
			return nil, err
		}
		return n, nil
	case string:
		return jstring(t), nil
	case json.Number:
		return &jnode{kind: 's', raw: t.String()}, nil
	case bool:
		return &jnode{kind: 's', raw: fmt.Sprint(t)}, nil
	default: // nil
		return &jnode{kind: 's', raw: "null"}, nil
	}
}

// jsonString encodes s without HTML escaping.
func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func jstring(s string) *jnode { return &jnode{kind: 's', raw: jsonString(s), isStr: true, str: s} }

func toJSON(v any) (*jnode, error) {
	switch v := v.(type) {
	case string:
		return jstring(v), nil
	case []string:
		n := &jnode{kind: 'a'}
		for _, s := range v {
			n.vals = append(n.vals, jstring(s))
		}
		return n, nil
	case Map:
		n := &jnode{kind: 'o'}
		for _, f := range v {
			c, err := toJSON(f.Value)
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, f.Key)
			n.vals = append(n.vals, c)
		}
		return n, nil
	case []Map:
		n := &jnode{kind: 'a'}
		for _, m := range v {
			c, err := toJSON(m)
			if err != nil {
				return nil, err
			}
			n.vals = append(n.vals, c)
		}
		return n, nil
	}
	return nil, fmt.Errorf("docedit: unsupported value %T", v)
}

func (n *jnode) get(key string) (int, *jnode) {
	for i, k := range n.keys {
		if k == key {
			return i, n.vals[i]
		}
	}
	return -1, nil
}

// find returns the node at path.
func (d *jsonDoc) find(path []any) (*jnode, error) {
	cur := d.root
	for i, p := range path {
		switch p := p.(type) {
		case string:
			if cur.kind != 'o' {
				return nil, fmt.Errorf("%s is not an object", pathString(path[:i]))
			}
			_, v := cur.get(p)
			if v == nil {
				return nil, fmt.Errorf("%s does not exist", pathString(path[:i+1]))
			}
			cur = v
		case int:
			if cur.kind != 'a' {
				return nil, fmt.Errorf("%s is not a list", pathString(path[:i]))
			}
			if p < 0 || p >= len(cur.vals) {
				return nil, fmt.Errorf("%s does not exist", pathString(path[:i+1]))
			}
			cur = cur.vals[p]
		}
	}
	return cur, nil
}

// object returns the object at path, creating missing objects.
func (d *jsonDoc) object(path []string) (*jnode, error) {
	cur := d.root
	for i, k := range path {
		_, v := cur.get(k)
		if v == nil {
			v = &jnode{kind: 'o'}
			cur.keys = append(cur.keys, k)
			cur.vals = append(cur.vals, v)
		}
		if v.kind != 'o' {
			return nil, fmt.Errorf("%s must be an object", pathString(keysPath(path[:i+1])))
		}
		cur = v
	}
	return cur, nil
}

func (d *jsonDoc) SetString(path []any, value string) error {
	n, err := d.find(path)
	if err != nil {
		return err
	}
	if n.kind != 's' || !n.isStr {
		return fmt.Errorf("%s must be a string", pathString(path))
	}
	*n = *jstring(value)
	return nil
}

func (d *jsonDoc) Append(path []string, item Map) error {
	parent, err := d.object(path[:len(path)-1])
	if err != nil {
		return err
	}
	c, err := toJSON(item)
	if err != nil {
		return err
	}
	key := path[len(path)-1]
	i, seq := parent.get(key)
	switch {
	case seq == nil:
		parent.keys = append(parent.keys, key)
		parent.vals = append(parent.vals, &jnode{kind: 'a', vals: []*jnode{c}})
	case seq.kind == 's' && seq.raw == "null":
		parent.vals[i] = &jnode{kind: 'a', vals: []*jnode{c}}
	case seq.kind != 'a':
		return fmt.Errorf("%s must be a list", pathString(keysPath(path)))
	default:
		seq.vals = append(seq.vals, c)
	}
	return nil
}

func (d *jsonDoc) Remove(path []any) error {
	idx, ok := path[len(path)-1].(int)
	if !ok {
		return errors.New("docedit: Remove needs an index")
	}
	seq, err := d.find(path[:len(path)-1])
	if err != nil {
		return err
	}
	if seq.kind != 'a' || idx < 0 || idx >= len(seq.vals) {
		return fmt.Errorf("%s does not exist", pathString(path))
	}
	seq.vals = append(seq.vals[:idx], seq.vals[idx+1:]...)
	return nil
}

func (d *jsonDoc) Put(path []string, key string, value any, first bool) error {
	obj, err := d.object(path)
	if err != nil {
		return err
	}
	if i, _ := obj.get(key); i >= 0 {
		return fmt.Errorf("%s exists already", pathString(keysPath(append(append([]string{}, path...), key))))
	}
	v, err := toJSON(value)
	if err != nil {
		return err
	}
	at := len(obj.keys)
	if first {
		at = 0
		if len(obj.keys) > 0 && obj.keys[0] == SchemaKey && key != SchemaKey {
			at = 1
		}
	}
	obj.keys = append(obj.keys[:at], append([]string{key}, obj.keys[at:]...)...)
	obj.vals = append(obj.vals[:at], append([]*jnode{v}, obj.vals[at:]...)...)
	return nil
}

func (d *jsonDoc) Bytes() []byte {
	var b strings.Builder
	writeJSON(&b, d.root, 0)
	b.WriteByte('\n')
	return []byte(b.String())
}

func writeJSON(b *strings.Builder, n *jnode, depth int) {
	switch n.kind {
	case 's':
		b.WriteString(n.raw)
		return
	case 'o', 'a':
	}
	open, closing := "[", "]"
	if n.kind == 'o' {
		open, closing = "{", "}"
	}
	if len(n.vals) == 0 {
		b.WriteString(open + closing)
		return
	}
	b.WriteString(open + "\n")
	for i, v := range n.vals {
		b.WriteString(strings.Repeat("  ", depth+1))
		if n.kind == 'o' {
			b.WriteString(jsonString(n.keys[i]) + ": ")
		}
		writeJSON(b, v, depth+1)
		if i < len(n.vals)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(strings.Repeat("  ", depth) + closing)
}

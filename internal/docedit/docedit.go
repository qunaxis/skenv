// Package docedit edits YAML and JSON documents in place, and reads and
// writes the schema directive of TOML, YAML and JSON files.
//
// YAML is edited as text at the positions the parser reports, so comments,
// blank lines, quoting and key order of everything else stay byte for byte.
// JSON is decoded into an order-preserving tree and encoded again with
// two-space indentation and without HTML escaping.
//
// Paths address nodes from the top of the document: a string selects a
// mapping key, an int a sequence item.
package docedit

import (
	"fmt"
	"strings"
)

// Field is one key of a Map.
type Field struct {
	Key   string
	Value any // string, []string, Map or []Map
}

// Map is an ordered mapping to write: the keys come out in this order.
type Map []Field

// Doc is a YAML or JSON document being edited.
type Doc interface {
	// SetString replaces the string at path, which must exist.
	SetString(path []any, value string) error
	// Append adds item at the end of the sequence at path, creating the
	// sequence and the mappings above it when they are missing.
	Append(path []string, item Map) error
	// Remove deletes the sequence item at path (its last element is the
	// index).
	Remove(path []any) error
	// Put adds key to the mapping at path, creating the mappings above it
	// when missing. With first, the key goes before the other keys (after
	// a "$schema" key); otherwise after them. The key must not exist yet.
	Put(path []string, key string, value any, first bool) error
	// Bytes returns the edited document.
	Bytes() []byte
}

// Open parses data in the format of ext (".yaml", ".yml" or ".json").
func Open(data []byte, ext string) (Doc, error) {
	switch ext {
	case ".yaml", ".yml":
		return openYAML(data)
	case ".json":
		return openJSON(data)
	}
	return nil, fmt.Errorf("docedit: unsupported format %q", ext)
}

func pathString(path []any) string {
	var b strings.Builder
	for _, p := range path {
		switch v := p.(type) {
		case int:
			fmt.Fprintf(&b, "[%d]", v)
		default:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			fmt.Fprint(&b, v)
		}
	}
	if b.Len() == 0 {
		return "the top level"
	}
	return b.String()
}

func keysPath(keys []string) []any {
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

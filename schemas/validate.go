package schemas

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"github.com/qunaxis/skenv/internal/platform/docedit"
)

// The parsers validate a decoded document against the embedded schema, so
// the structural rules (types, required and unknown keys, patterns, enums)
// live in one place, the schema that editors show too. Go code keeps only
// what a schema cannot express.

type compiled struct {
	schema *jsonschema.Schema
	raw    any // the schema document, for the errorMessage hints
}

var compiledSchemas = sync.OnceValue(func() map[string]compiled {
	out := map[string]compiled{}
	for _, name := range Names {
		data, _ := Get(name)
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			panic("schemas: " + name + ": " + err.Error())
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(URL(name, ""), doc); err != nil {
			panic("schemas: " + name + ": " + err.Error())
		}
		s, err := c.Compile(URL(name, ""))
		if err != nil {
			panic("schemas: " + name + ": " + err.Error())
		}
		out[name] = compiled{s, doc}
	}
	return out
})

// Validate checks doc, a document decoded from TOML, YAML or JSON, against
// schema name. The error has one line per violation, sorted, each naming
// the key as a TOML path ("[user.dependencies.x] commit") and the fix.
func Validate(name string, doc map[string]any) error {
	c := compiledSchemas()[name]
	inst := jsonValue(doc)
	err := c.schema.Validate(inst)
	var verr *jsonschema.ValidationError
	if err == nil || !errors.As(err, &verr) {
		return err
	}
	f := formatter{schema: c.raw, doc: inst}
	f.walk(verr, nil)
	slices.Sort(f.lines)
	lines := slices.Compact(f.lines)
	if len(lines) == 1 {
		return errors.New(lines[0])
	}
	return fmt.Errorf("%d errors:\n  %s", len(lines), strings.Join(lines, "\n  "))
}

// jsonValue converts a decoded document to the JSON data model of the
// validator: TOML arrays of tables and YAML maps with non-string keys
// become []any and map[string]any. Other values (a TOML date) stay, and the
// validator reports them as invalid.
func jsonValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			m[k] = jsonValue(x)
		}
		return m
	case map[any]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			m[fmt.Sprint(k)] = jsonValue(x)
		}
		return m
	case []map[string]any:
		a := make([]any, len(v))
		for i, x := range v {
			a[i] = jsonValue(x)
		}
		return a
	case []any:
		a := make([]any, len(v))
		for i, x := range v {
			a[i] = jsonValue(x)
		}
		return a
	}
	return v
}

type formatter struct {
	schema any
	doc    any
	lines  []string
}

// walk collects a line for every leaf of the error tree. at overrides the
// instance location: the causes of propertyNames describe a key, not the
// object that has it.
func (f *formatter) walk(e *jsonschema.ValidationError, at []string) {
	loc := e.InstanceLocation
	if at != nil {
		loc = at
	}
	switch k := e.ErrorKind.(type) {
	case *kind.PropertyNames:
		at = append(slices.Clone(loc), k.Property)
	case *kind.Not:
		f.add(loc, f.notMessage(e))
		return
	case *kind.Required:
		for _, m := range k.Missing {
			f.add(append(slices.Clone(loc), m), "is required")
		}
		return
	case *kind.AdditionalProperties:
		known := f.known(e.SchemaURL)
		for _, p := range k.Properties {
			f.add(append(slices.Clone(loc), p), "unknown key (known: "+strings.Join(known, ", ")+")")
		}
		return
	}
	if len(e.Causes) == 0 {
		f.add(loc, f.message(e, loc))
		return
	}
	for _, c := range e.Causes {
		f.walk(c, at)
	}
}

func (f *formatter) add(loc []string, msg string) {
	f.lines = append(f.lines, f.path(loc)+": "+msg)
}

// message describes a leaf error in words; values are not echoed, since a
// URL may hold credentials.
func (f *formatter) message(e *jsonschema.ValidationError, loc []string) string {
	sub := f.subschema(e.SchemaURL)
	hint := str(sub, "errorMessage")
	var msg string
	switch k := e.ErrorKind.(type) {
	case *kind.Type:
		msg = typeMessage(k)
	case *kind.InvalidJsonValue:
		msg = "must be a string, a table or a list, got a date, time or special number; quote it"
	case *kind.Enum:
		want := make([]string, len(k.Want))
		for i, w := range k.Want {
			want[i] = strconv.Quote(fmt.Sprint(w))
		}
		msg = "must be one of " + strings.Join(want, ", ")
	case *kind.Pattern:
		msg = "invalid value"
		if h := str(sub, "patternErrorMessage"); h != "" {
			hint = h
		}
	case *kind.MinLength:
		msg = "must not be empty"
	case *kind.MaxLength:
		msg = fmt.Sprintf("is longer than %d characters", k.Want)
	case *kind.MinItems:
		msg = fmt.Sprintf("must list at least %d", k.Want)
	case *kind.UniqueItems:
		msg = fmt.Sprintf("items %d and %d are the same; keep one", k.Duplicates[0], k.Duplicates[1])
	default:
		msg = e.ErrorKind.LocalizedString(nil)
	}
	if hint != "" {
		msg += ". " + hint
	}
	return msg
}

// typeMessage describes a value of the wrong type.
func typeMessage(k *kind.Type) string {
	switch {
	case k.Got == "null":
		return "is empty (null); give it a value or remove it"
	case slices.Contains(k.Want, "string") && (k.Got == "number" || k.Got == "integer" || k.Got == "boolean"):
		return "must be a string, got a " + k.Got + "; quote it"
	}
	return "must be " + typeNames(k.Want) + ", got " + typeName(k.Got)
}

// notMessage is the errorMessage of a failed "not": inside the negated
// schema or beside it.
func (f *formatter) notMessage(e *jsonschema.ValidationError) string {
	sub := f.subschema(e.SchemaURL)
	if m, ok := sub.(map[string]any); ok {
		if h := str(m["not"], "errorMessage"); h != "" {
			return h
		}
	}
	if h := str(sub, "errorMessage"); h != "" {
		return h
	}
	return "is not allowed"
}

// known lists the keys the object schema at url allows.
func (f *formatter) known(url string) []string {
	obj, _ := f.subschema(url).(map[string]any)
	props, _ := obj["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// subschema resolves the fragment of a schema location in the schema
// document, nil when it cannot.
func (f *formatter) subschema(url string) any {
	_, frag, _ := strings.Cut(url, "#")
	var segs []string
	for tok := range strings.SplitSeq(strings.TrimPrefix(frag, "/"), "/") {
		if tok != "" {
			segs = append(segs, strings.NewReplacer("~1", "/", "~0", "~").Replace(tok))
		}
	}
	return walkPointer(f.schema, segs)
}

// walkPointer follows segs, keys of tables and indexes of lists, from
// cur; nil when one does not lead anywhere.
func walkPointer(cur any, segs []string) any {
	for _, seg := range segs {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i >= len(c) {
				return nil
			}
			cur = c[i]
		default:
			return nil
		}
	}
	return cur
}

// path writes an instance location the way the docs name keys: the
// innermost table in brackets, then the key in it, "[user.dependencies.x]
// commit"; list items as "mirrors[1]"; a top-level key alone.
func (f *formatter) path(loc []string) string {
	table := 0 // loc[:table] is the innermost table that holds the key
	cur := f.doc
	for i, seg := range loc[:max(len(loc)-1, 0)] {
		m, ok := cur.(map[string]any)
		if !ok {
			break
		}
		cur = m[seg]
		if _, isMap := cur.(map[string]any); !isMap {
			break
		}
		table = i + 1
	}
	var key strings.Builder
	for i, seg := range loc[table:] {
		if f.isIndex(loc[:table+i]) {
			key.WriteString("[" + seg + "]")
			continue
		}
		if i > 0 {
			key.WriteByte('.')
		}
		key.WriteString(docedit.QuoteKey(seg))
	}
	if table == 0 {
		return key.String()
	}
	quoted := make([]string, table)
	for i, seg := range loc[:table] {
		quoted[i] = docedit.QuoteKey(seg)
	}
	return "[" + strings.Join(quoted, ".") + "] " + key.String()
}

// isIndex reports whether the value at loc is a list.
func (f *formatter) isIndex(loc []string) bool {
	_, ok := f.at(loc).([]any)
	return ok
}

func (f *formatter) at(loc []string) any { return walkPointer(f.doc, loc) }

func str(schema any, key string) string {
	m, _ := schema.(map[string]any)
	s, _ := m[key].(string)
	return s
}

func typeName(t string) string {
	switch t {
	case "object":
		return "a table"
	case "array":
		return "a list"
	case "integer":
		return "a number"
	}
	return "a " + t
}

func typeNames(ts []string) string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = typeName(t)
	}
	return strings.Join(out, " or ")
}

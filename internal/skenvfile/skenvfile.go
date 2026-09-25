// Package skenvfile reads the skenv file of a repository:
// skenv.toml (or skenv.yaml, skenv.yml, skenv.json) in its root.
//
// The file has two optional top-level sections:
//
//   - [repo]: the harness of a skills repository (harness, visibility,
//     runner), written by `skenv repo init|apply`;
//   - [environment]: the manifest of a user's machines (layout, own,
//     vendor, host), edited by `skenv vendor add|update|remove`.
//
// Nothing else may appear at the top level, except "$schema" (a string,
// ignored) for editors. Each section is decoded strictly: unknown keys are
// errors.
//
// YAML and JSON files are edited with internal/docedit, which keeps
// comments and key order; TOML files are edited as text by their callers.
package skenvfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/schemas"
)

// Names are the accepted file names, in lookup order. skenv creates the
// first one when a repository has none.
var Names = []string{"skenv.toml", "skenv.yaml", "skenv.yml", "skenv.json"}

// Sections of the file.
const (
	Repo        = "repo"
	Environment = "environment"
)

// oldManifest is the manifest file of skenv before 0.4.
const oldManifest = "env.toml"

// Find returns the skenv file in dir, "" when there is none. Several
// skenv files, or an env.toml of an older skenv, are errors.
func Find(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, oldManifest)); err == nil {
		return "", OldManifestError(filepath.Join(dir, oldManifest))
	}
	var found []string
	for _, n := range Names {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	}
	names := make([]string, len(found))
	for i, p := range found {
		names[i] = filepath.Base(p)
	}
	return "", fmt.Errorf("several skenv files in %s (%s); keep one", dir, strings.Join(names, ", "))
}

// OldManifestError explains that env.toml is no longer read.
func OldManifestError(path string) error {
	return fmt.Errorf("%s is no longer read: move its content under [environment] in skenv.toml "+
		"(tables become [environment.layout], [[environment.own]], [[environment.vendor]], "+
		"[environment.host.<name>]) and delete it", path)
}

// Doc is a parsed skenv file.
type Doc struct {
	Path string
	ext  string
	raw  map[string]any // the whole document, for IsDefined
	data []byte
	yam  map[string]yaml.Node // YAML and JSON sections
}

// Read parses the skenv file at path.
func Read(path string) (*Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := Parse(data, filepath.Ext(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d.Path = path
	return d, nil
}

// Parse parses data in the format of ext (".toml", ".yaml", ".yml",
// ".json") and checks the top level.
func Parse(data []byte, ext string) (*Doc, error) {
	d := &Doc{ext: ext}
	var err error
	switch ext {
	case ".toml":
		d.data = data
		_, err = toml.Decode(string(data), &d.raw)
	case ".yaml", ".yml":
		if err = yaml.Unmarshal(data, &d.yam); err == nil {
			err = yaml.Unmarshal(data, &d.raw)
		}
	case ".json":
		// Sections are decoded as YAML (JSON is a subset of it), whose
		// decoder matches keys exactly; encoding/json ignores case.
		if len(bytes.TrimSpace(data)) > 0 {
			if err = json.Unmarshal(data, &d.raw); err == nil {
				err = yaml.Unmarshal(data, &d.yam)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported format %q (toml, yaml, yml or json)", ext)
	}
	if err != nil {
		return nil, err
	}
	if d.raw == nil {
		d.raw = map[string]any{}
	}
	var unknown []string
	for k := range d.raw {
		if k != Repo && k != Environment && k != docedit.SchemaKey {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		for _, k := range unknown {
			if k == "harness" || k == "visibility" || k == "runner" {
				return nil, fmt.Errorf("unknown top-level keys: %s; this is the skenv.toml of skenv before 0.4: move harness, visibility and runner under [repo] (see docs/skenv-file.md)", strings.Join(unknown, ", "))
			}
		}
		return nil, fmt.Errorf("unknown top-level keys: %s (settings live under [repo] and [environment])", strings.Join(unknown, ", "))
	}
	for _, s := range []string{Repo, Environment} {
		if v, ok := d.raw[s]; ok {
			if _, isMap := v.(map[string]any); !isMap {
				return nil, fmt.Errorf("%s must be a table", s)
			}
			if err := stringLeaves(s, v); err != nil {
				return nil, err
			}
		}
	}
	if v, ok := d.raw[docedit.SchemaKey]; ok {
		if _, isString := v.(string); !isString {
			return nil, fmt.Errorf("%s must be a string (the URL of the schema, for editors)", docedit.SchemaKey)
		}
	}
	return d, nil
}

// stringLeaves checks that every value in a section is a table, a list or a
// string: the file has no other kind of value, and YAML would otherwise
// turn null, 1 or an all-digit commit SHA into something the schema
// rejects.
func stringLeaves(path string, v any) error {
	switch v := v.(type) {
	case string:
		return nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := stringLeaves(path+"."+k, v[k]); err != nil {
				return err
			}
		}
	case []map[string]any: // TOML arrays of tables
		for i, x := range v {
			if err := stringLeaves(fmt.Sprintf("%s[%d]", path, i), x); err != nil {
				return err
			}
		}
	case []any:
		for i, x := range v {
			if err := stringLeaves(fmt.Sprintf("%s[%d]", path, i), x); err != nil {
				return err
			}
		}
	case nil:
		return fmt.Errorf("%s is empty (null); give it a value or remove it", path)
	default:
		return fmt.Errorf("%s must be a string, got %v; quote it", path, v)
	}
	return nil
}

// Has reports whether the document has section.
func (d *Doc) Has(section string) bool {
	_, ok := d.raw[section]
	return ok
}

// Decode decodes section into out, rejecting unknown keys. out keeps its
// zero value when the section is absent.
func (d *Doc) Decode(section string, out any) error {
	if !d.Has(section) {
		return nil
	}
	switch d.ext {
	case ".toml":
		// A fresh decode per call: the metadata tracks which keys were used.
		var top map[string]toml.Primitive
		md, err := toml.Decode(string(d.data), &top)
		if err != nil {
			return err
		}
		if err := md.PrimitiveDecode(top[section], out); err != nil {
			return fmt.Errorf("[%s]: %w", section, err)
		}
		var unknown []string
		for _, k := range md.Undecoded() {
			if len(k) > 1 && k[0] == section {
				unknown = append(unknown, k.String())
			}
		}
		if len(unknown) > 0 {
			return fmt.Errorf("unknown keys: %s", strings.Join(unknown, ", "))
		}
		return nil
	default: // YAML and JSON
		node := d.yam[section]
		b, err := yaml.Marshal(&node)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("%s: %w", section, err)
		}
		return nil
	}
}

// IsDefined reports whether the key path exists, for example
// IsDefined("environment", "layout", "targets").
func (d *Doc) IsDefined(keys ...string) bool {
	var cur any = d.raw
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[k]; !ok {
			return false
		}
	}
	return true
}

// Harness returns repo.harness as written, "" when it is missing or not a
// string.
func (d *Doc) Harness() string {
	r, _ := d.raw[Repo].(map[string]any)
	v, _ := r["harness"].(string)
	return v
}

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// SchemaVersion is the skenv release whose schema the directive of this
// file names: repo.harness in a repository with a harness, because that is
// the skenv its CI installs (`skenv repo check` compares with it); the
// running skenv otherwise ("" for a development build: the latest schema).
func (d *Doc) SchemaVersion() string {
	if h := d.Harness(); versionRe.MatchString(h) {
		return h
	}
	return schemas.Running()
}

// Stamp returns data, a skenv file after an edit, with its schema
// directive at SchemaVersion: a skenv directive moves there, a missing one
// is added only with add, any other URL stays.
func Stamp(data []byte, ext string, add bool) ([]byte, error) {
	d, err := Parse(data, ext)
	if err != nil {
		return nil, err
	}
	return schemas.Stamp(data, ext, schemas.Skenv, d.SchemaVersion(), add)
}

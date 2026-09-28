// Package skenvfile reads, validates and edits the skenv file of a
// repository: skenv.toml (or skenv.yaml, skenv.yml, skenv.json) in its
// root.
//
// The file has three optional top-level sections, independent of each
// other (docs/adr/0002-config-format.md):
//
//   - [repository]: the development tooling of a skills repository
//     (template_version, visibility, ci), written by `skenv repo
//     init|upgrade`;
//   - [user]: the manifest, the skills of the current OS user's agents
//     (checkouts, dependencies, machines, agents, storage, git_hosts,
//     unmanaged), edited by `skenv vendor add|update|remove`
//     (Manifest, manifest.go; git_hosts: hosts.go);
//   - [project]: the skills a project repository carries (dir, mirrors,
//     mirrors_mode, git_hosts, dependencies, from), edited by `skenv
//     vendor ... --project` (Project, project.go).
//
// Keys of the format before skenv 0.6 are errors that name their
// replacement (legacy.go); there is no legacy reading.
//
// Nothing else may appear at the top level, except "$schema" (a string,
// ignored) for editors. The structure (types, required and unknown keys,
// patterns, enums) is the JSON Schema's, checked by Parse.
//
// YAML and JSON files are edited with internal/platform/docedit, which keeps
// comments and key order; TOML files are edited as text by their callers.
package skenvfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/schemas"
)

// Names are the accepted file names, in lookup order. skenv creates the
// first one when a repository has none.
var Names = []string{"skenv.toml", "skenv.yaml", "skenv.yml", "skenv.json"}

// Sections of the file.
const (
	SectionRepository = "repository"
	SectionUser       = "user"
	SectionProject    = "project"
)

// Sections lists the sections in the order the docs present them.
var Sections = []string{SectionRepository, SectionUser, SectionProject}

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
	return fmt.Errorf("%s is no longer read: move its content under [user] in skenv.toml and delete it; "+
		"the keys are renamed, see %s", path, MigrationURL)
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
// ".json") and validates it against the JSON Schema of the skenv file
// (schemas.Skenv). Go code checks only what a schema cannot express.
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
	if err := legacyError(d.raw); err != nil {
		return nil, err
	}
	if err := schemas.Validate(schemas.Skenv, d.raw); err != nil {
		return nil, err
	}
	return d, nil
}

// Has reports whether the document has section.
func (d *Doc) Has(section string) bool {
	_, ok := d.raw[section]
	return ok
}

// Decode decodes section into out; Parse has validated it. out keeps its
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
		return nil
	default: // YAML and JSON
		node := d.yam[section]
		b, err := yaml.Marshal(&node)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(b, out); err != nil {
			return fmt.Errorf("%s: %w", section, err)
		}
		return nil
	}
}

// IsDefined reports whether the key path exists, for example
// IsDefined("user", "agents", "enabled").
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

// TemplateVersion returns repository.template_version as written, "" when
// it is missing or not a string.
func (d *Doc) TemplateVersion() string {
	r, _ := d.raw[SectionRepository].(map[string]any)
	v, _ := r["template_version"].(string)
	return v
}

// SchemaVersion is the skenv release whose schema the directive of this
// file names: repository.template_version in a repository with templates,
// because that is the skenv its CI installs (`skenv repo check` compares
// with it); the running skenv otherwise ("" for a development build: the
// latest schema).
func (d *Doc) SchemaVersion() string {
	if h := d.TemplateVersion(); VersionRe.MatchString(h) {
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

// Package config reads and writes skenv's own configuration,
// ~/.config/skenv/config.{toml,yaml,yml,json}.
//
// A setting comes from, highest first: a command-line flag, the SKENV_<KEY>
// environment variable, the config file, the built-in default. Keys are
// lowercase and case-sensitive in every format. At most one config file may
// exist; skenv refuses to guess between two.
//
// The skenv file of a repository (skenv.toml with [repo] and [environment])
// is not configuration of the tool and is read by internal/skenvfile.
//
// Unknown keys are errors; "$schema" is allowed for editors. `skenv init`
// edits the file in place: comments, other keys and their order stay.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/atomicfile"
	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/schemas"
)

// Config lists the keys of the tool config. Its doc comments are the
// descriptions of the JSON Schema (`make schemas`), which adds the flag, the
// environment variable and the precedence of each key.
type Config struct {
	// Manifest is the skenv file with the [environment] section, or the
	// directory that holds it ("~" allowed). `skenv init` records it. There
	// is no default: without it, commands that need the manifest stop with
	// an error.
	Manifest string `toml:"manifest" yaml:"manifest" json:"manifest"`
}

// Keys are the keys of Config, in order.
func Keys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, t.NumField())
	for i := range keys {
		keys[i] = t.Field(i).Tag.Get("json")
	}
	return keys
}

// Names are the accepted config file names, in lookup order. `skenv init`
// creates the first one when none exists.
var Names = []string{"config.toml", "config.yaml", "config.yml", "config.json"}

// Dir is ~/.config/skenv.
func Dir(home string) string { return filepath.Join(home, ".config", "skenv") }

// File is a loaded config file. Path is empty when there is none.
type File struct {
	Path   string
	values map[string]any
}

// Find returns the config file in dir, "" when there is none, and an
// error when there are several.
func Find(dir string) (string, error) {
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
	return "", fmt.Errorf("several config files in %s (%s); keep one", dir, strings.Join(names, ", "))
}

// Load reads the config file of home, if any.
func Load(home string) (*File, error) {
	p, err := Find(Dir(home))
	if err != nil || p == "" {
		return &File{values: map[string]any{}}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	values, err := decode(p, data)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", p, err)
	}
	return &File{Path: p, values: values}, nil
}

func decode(path string, data []byte) (map[string]any, error) {
	values := map[string]any{}
	var err error
	switch filepath.Ext(path) {
	case ".toml":
		_, err = toml.Decode(string(data), &values)
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &values)
	case ".json":
		if len(bytes.TrimSpace(data)) > 0 {
			err = json.Unmarshal(data, &values)
		}
	default:
		err = fmt.Errorf("unsupported format %q", filepath.Ext(path))
	}
	if err != nil {
		return nil, err
	}
	if values == nil { // an empty YAML document
		values = map[string]any{}
	}
	var unknown []string
	for k := range values {
		if k != docedit.SchemaKey && !slices.Contains(Keys(), k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown key %q (known keys: %s)", strings.Join(unknown, `", "`), strings.Join(Keys(), ", "))
	}
	for _, k := range append([]string{docedit.SchemaKey}, Keys()...) {
		if v, ok := values[k]; ok {
			if v == nil {
				return nil, fmt.Errorf("%s must be a string, got an empty value (null); give it a value or remove it", k)
			}
			if _, isString := v.(string); !isString {
				return nil, fmt.Errorf("%s must be a string, got %v", k, v)
			}
		}
	}
	return values, nil
}

// String returns key from the file; ok is false when it is not set.
func (f *File) String(key string) (string, bool, error) {
	v, ok := f.values[key]
	if !ok {
		return "", false, nil
	}
	s, isString := v.(string)
	if !isString {
		return "", false, fmt.Errorf("config %s: %s must be a string, got %T", f.Path, key, v)
	}
	return s, s != "", nil
}

// EnvVar is the environment variable of key: SKENV_<KEY>.
func EnvVar(key string) string {
	return "SKENV_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

// Source says where a resolved value came from.
type Source string

// Sources of a value, highest precedence first.
const (
	FromFlag    Source = "flag"
	FromEnv     Source = "env"
	FromFile    Source = "file"
	FromDefault Source = "default"
)

// Resolve returns key by precedence: flag (when not empty), SKENV_<KEY>,
// the config file of home, then def.
func Resolve(home string, getenv func(string) string, key, flag, def string) (string, Source, error) {
	if flag != "" {
		return flag, FromFlag, nil
	}
	if v := getenv(EnvVar(key)); v != "" {
		return v, FromEnv, nil
	}
	f, err := Load(home)
	if err != nil {
		return "", "", err
	}
	v, ok, err := f.String(key)
	if err != nil {
		return "", "", err
	}
	if ok {
		return v, FromFile, nil
	}
	return def, FromDefault, nil
}

// Set writes key = value into the config file of home. An existing file
// keeps its format, comments, other keys and their order, and a skenv
// schema directive in it moves to the version of the running skenv. Without
// a file, config.toml is created with a header and the schema directive.
func Set(home, key, value string) (string, error) {
	return SetFormat(home, "", key, value)
}

// Target returns the config file that SetFormat writes for format ("" for
// the existing file or TOML): the existing file, or a new
// config.<format>. An existing file in another format is an error.
func Target(home, format string) (string, error) {
	f, err := Load(home)
	if err != nil {
		return "", err
	}
	return fileformat.Choose(Dir(home), "config", f.Path, format)
}

// SetFormat is Set with the format of a new file ("toml", "yaml", "json";
// "" for TOML). An existing file keeps its format; format that disagrees
// with it is an error, and nothing is written.
func SetFormat(home, format, key, value string) (string, error) {
	f, err := Load(home)
	if err != nil {
		return "", err
	}
	path, err := fileformat.Choose(Dir(home), "config", f.Path, format)
	if err != nil {
		return "", err
	}
	var out []byte
	if f.Path == "" {
		if out, err = newFile(filepath.Ext(path), key, value); err != nil {
			return "", err
		}
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if out, err = setKey(data, filepath.Ext(path), key, value); err != nil {
			return "", fmt.Errorf("config %s: %w", path, err)
		}
		if out, err = schemas.Stamp(out, filepath.Ext(path), schemas.Config, schemas.Running(), false); err != nil {
			return "", err
		}
	}
	// The edit must read back as intended.
	values, err := decode(path, out)
	if err != nil {
		return "", fmt.Errorf("config %s: the edit would make it invalid: %w", path, err)
	}
	if values[key] != value {
		return "", fmt.Errorf("config %s: could not set %s; edit the file by hand", path, key)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return path, atomicfile.Write(path, out, mode)
}

// header is the first comment of a new config file (TOML and YAML; JSON
// has no comments).
const header = "# skenv configuration, written by `skenv init`\n"

// newFile returns a new config file in the format of ext with key = value,
// the header and the schema directive.
func newFile(ext, key, value string) ([]byte, error) {
	url := schemas.URL(schemas.Config, schemas.Running())
	if ext == ".toml" {
		return fmt.Appendf(nil, "#:schema %s\n%s%s = %s\n", url, header, key, tomlString(value)), nil
	}
	d, err := docedit.Open(nil, ext)
	if err != nil {
		return nil, err
	}
	if err := d.Put(nil, key, value, false); err != nil {
		return nil, err
	}
	out := d.Bytes()
	if ext != ".json" {
		out = append([]byte(header), out...)
	}
	return docedit.SetDirective(out, ext, url)
}

// A top-level TOML key line: key = "value" or 'value', with an optional
// comment.
var tomlKeyRe = regexp.MustCompile(`^(\s*)([A-Za-z0-9_-]+|"[^"]*")(\s*=\s*)("(?:[^"\\]|\\.)*"|'[^']*')(.*)$`)

func setKey(data []byte, ext, key, value string) ([]byte, error) {
	if ext != ".toml" {
		d, err := docedit.Open(data, ext)
		if err != nil {
			return nil, err
		}
		cur, err := decode("x"+ext, data)
		if err != nil {
			return nil, err
		}
		if _, exists := cur[key]; exists {
			err = d.SetString([]any{key}, value)
		} else {
			err = d.Put(nil, key, value, false)
		}
		if err != nil {
			return nil, err
		}
		return d.Bytes(), nil
	}
	// TOML: replace the line of the key among the top-level keys, or add
	// one after them.
	var lines []string
	if len(data) > 0 {
		lines = strings.SplitAfter(string(data), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}
	end := len(lines)
	for i, l := range lines {
		t := strings.TrimRight(l, "\r\n")
		if strings.HasPrefix(strings.TrimSpace(t), "[") {
			end = i
			break
		}
		m := tomlKeyRe.FindStringSubmatch(t)
		if m != nil && strings.Trim(m[2], `"`) == key {
			lines[i] = m[1] + m[2] + m[3] + tomlString(value) + m[5] + l[len(t):]
			return []byte(strings.Join(lines, "")), nil
		}
	}
	// After the last top-level key, before the blank lines and comments
	// that lead into the first table.
	at := end
	for at > 0 && isBlankOrComment(lines[at-1]) && end < len(lines) {
		at--
	}
	nl := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		nl = "\r\n"
	}
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += nl
	}
	line := key + " = " + tomlString(value) + nl
	lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	return []byte(strings.Join(lines, "")), nil
}

func isBlankOrComment(l string) bool {
	t := strings.TrimSpace(l)
	return t == "" || strings.HasPrefix(t, "#")
}

// tomlString renders s as a TOML basic string.
func tomlString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

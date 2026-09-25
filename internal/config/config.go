// Package config reads and writes skenv's own configuration,
// ~/.config/skenv/config.{toml,yaml,yml,json}.
//
// A setting comes from, highest first: a command-line flag, the SKENV_<KEY>
// environment variable, the config file, the built-in default. Keys are
// lowercase and case-sensitive in every format. At most one config file may
// exist; skenv refuses to guess between two.
//
// The manifest (env.toml) and the repository harness (skenv.toml) are data
// files, not configuration, and stay TOML only: see
// docs/adr/0001-cli-and-config-framework.md.
package config

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

	"github.com/qunaxis/skenv/internal/atomicfile"
)

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
	if values == nil { // an empty YAML document
		values = map[string]any{}
	}
	return values, err
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

// Set writes key = value into the config file of home, keeping the other
// keys. An existing file keeps its format; without one, config.toml is
// created. Comments in the file are not kept.
func Set(home, key, value string) (string, error) {
	f, err := Load(home)
	if err != nil {
		return "", err
	}
	path := f.Path
	if path == "" {
		path = filepath.Join(Dir(home), Names[0])
	}
	f.values[key] = value
	var b bytes.Buffer
	switch filepath.Ext(path) {
	case ".toml":
		b.WriteString("# skenv configuration, written by `skenv init`\n")
		err = toml.NewEncoder(&b).Encode(f.values)
	case ".yaml", ".yml":
		b.WriteString("# skenv configuration, written by `skenv init`\n")
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		err = enc.Encode(f.values)
	case ".json":
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		err = enc.Encode(f.values)
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return path, atomicfile.Write(path, b.Bytes(), mode)
}

// Package config loads skenv's own tool configuration from
// ~/.config/skenv/config.{toml,yaml,yml,json}.
//
// Precedence, highest first: flags, $SKENV_<KEY> environment variables, the
// config file, defaults. Keys are flat, lower case and case-sensitive; the
// environment variable of a key is SKENV_ plus the key in upper case. At most
// one config file may exist: several are an error rather than a silent pick.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	kenv "github.com/knadh/koanf/providers/env/v2"
	kfile "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/qunaxis/skenv/internal/atomicfile"
	"github.com/qunaxis/skenv/internal/paths"
)

// Known keys. A key not listed here is kept in the file but ignored.
const (
	Manifest = "manifest"
	Store    = "store" // hypothetical future key, shows how keys are added
)

// Keys lists every known key.
var Keys = []string{Manifest, Store}

// Defaults are the values used when nothing else sets a key.
var Defaults = map[string]string{Store: "~/.agents/skills"}

// EnvPrefix prefixes the environment variable of every key.
const EnvPrefix = "SKENV_"

// Exts are the config file extensions, in the order they are reported.
var Exts = []string{"toml", "yaml", "yml", "json"}

// Header starts every config file written by skenv in TOML.
const Header = "# skenv configuration, written by `skenv init`\n"

// Config is the effective configuration.
type Config struct {
	File   string            // the config file read, "" when there is none
	Values map[string]string // every known key with a non-empty value
}

// Get returns the value of key, "" when unset.
func (c *Config) Get(key string) string { return c.Values[key] }

// Dir is ~/.config/skenv.
func Dir(home string) string { return filepath.Join(home, ".config", "skenv") }

// DefaultFile is the file created when there is no config file yet.
func DefaultFile(home string) string { return filepath.Join(Dir(home), "config.toml") }

// Find returns the config file, "" when there is none, or an error when
// there are several.
func Find(home string) (string, error) {
	var found []string
	for _, ext := range Exts {
		f := filepath.Join(Dir(home), "config."+ext)
		if _, err := os.Stat(f); err == nil {
			found = append(found, f)
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
	show := make([]string, len(found))
	for i, f := range found {
		show[i] = paths.Collapse(home, f)
	}
	return "", fmt.Errorf("several config files: %s; keep one", strings.Join(show, ", "))
}

// parser picks the koanf parser for a config file by its extension.
func parser(file string) koanf.Parser {
	switch filepath.Ext(file) {
	case ".yaml", ".yml":
		return yaml.Parser()
	case ".json":
		return json.Parser()
	}
	return toml.Parser()
}

// loadFile returns a koanf instance holding file, empty when it is missing.
func loadFile(file string) (*koanf.Koanf, error) {
	k := koanf.New(".")
	if file == "" {
		return k, nil
	}
	if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
		return k, nil
	}
	if err := k.Load(kfile.Provider(file), parser(file)); err != nil {
		return nil, fmt.Errorf("config %s: %w", file, err)
	}
	return k, nil
}

// Load resolves every known key. flags holds the values given on the
// command line; an empty value counts as not given.
func Load(home string, getenv func(string) string, flags map[string]string) (*Config, error) {
	file, err := Find(home)
	if err != nil {
		return nil, err
	}
	k, err := loadFile(file)
	if err != nil {
		return nil, err
	}
	for _, key := range Keys {
		if _, ok := k.Get(key).(string); k.Exists(key) && !ok {
			return nil, fmt.Errorf("config %s: %s must be a string", paths.Collapse(home, file), key)
		}
	}
	defaults := map[string]any{}
	for key, d := range Defaults {
		if !k.Exists(key) || k.String(key) == "" {
			defaults[key] = d
		}
	}
	// The env provider takes an environ function, so the injected getenv
	// works: it is asked for the known keys only.
	environ := func() []string {
		var out []string
		for _, key := range Keys {
			if v := getenv(EnvPrefix + strings.ToUpper(key)); v != "" {
				out = append(out, EnvPrefix+strings.ToUpper(key)+"="+v)
			}
		}
		return out
	}
	env := kenv.Provider(".", kenv.Opt{
		Prefix:        EnvPrefix,
		EnvironFunc:   environ,
		TransformFunc: func(k, v string) (string, any) { return strings.ToLower(strings.TrimPrefix(k, EnvPrefix)), v },
	})
	given := map[string]any{}
	for key, v := range flags {
		if v != "" {
			given[key] = v
		}
	}
	for _, p := range []koanf.Provider{confmap.Provider(defaults, "."), env, confmap.Provider(given, ".")} {
		if err := k.Load(p, nil); err != nil {
			return nil, err
		}
	}
	c := &Config{File: file, Values: map[string]string{}}
	for _, key := range Keys {
		if v := k.String(key); v != "" {
			c.Values[key] = v
		}
	}
	return c, nil
}

// Set writes key = value into the config file, keeping its other keys and
// its format. Without a config file it creates config.toml. It returns the
// file written. Comments are not kept.
func Set(home, key, value string) (string, error) {
	file, err := Find(home)
	if err != nil {
		return "", err
	}
	if file == "" {
		file = DefaultFile(home)
	}
	k, err := loadFile(file)
	if err != nil {
		return "", err
	}
	if err := k.Set(key, value); err != nil {
		return "", err
	}
	out, err := k.Marshal(parser(file))
	if err != nil {
		return "", err
	}
	if filepath.Ext(file) == ".toml" {
		out = append([]byte(Header), out...)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	return file, atomicfile.Write(file, out, mode)
}

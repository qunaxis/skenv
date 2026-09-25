// Package config loads skenv's own tool configuration from
// ~/.config/skenv/config.{toml,yaml,yml,json}.
//
// Precedence, highest first: flags, $SKENV_<KEY> environment variables, the
// config file, defaults. Keys are flat, lower case and case-sensitive; the
// environment variable of a key is SKENV_ plus the key in upper case. At most
// one config file may exist: several are an error rather than a silent pick.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

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

// Load resolves every known key. flags holds the values given on the
// command line; an empty value counts as not given.
func Load(home string, getenv func(string) string, flags map[string]string) (*Config, error) {
	file, err := Find(home) // viper would silently take the first of several files
	if err != nil {
		return nil, err
	}
	v := viper.New()
	for k, d := range Defaults {
		v.SetDefault(k, d)
	}
	if file != "" {
		v.SetConfigFile(file)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config %s: %w", file, err)
		}
	}
	for _, k := range Keys {
		// viper's GetString casts any scalar to a string; keep the file strict.
		if v.InConfig(k) {
			if _, ok := v.Get(k).(string); !ok {
				return nil, fmt.Errorf("config %s: %s must be a string", paths.Collapse(home, file), k)
			}
		}
		// AutomaticEnv/BindEnv read os.LookupEnv only, so the injected getenv
		// is applied by hand, as an override below the flag.
		for _, val := range []string{getenv(EnvPrefix + strings.ToUpper(k)), flags[k]} {
			if val != "" {
				v.Set(k, val)
			}
		}
	}
	c := &Config{File: file, Values: map[string]string{}}
	for _, k := range Keys {
		if val := v.GetString(k); val != "" {
			c.Values[k] = val
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
	v := viper.New()
	if file == "" {
		file = DefaultFile(home)
	} else {
		v.SetConfigFile(file)
		if err := v.ReadInConfig(); err != nil {
			return "", fmt.Errorf("config %s: %w", file, err)
		}
	}
	v.SetConfigType(strings.TrimPrefix(filepath.Ext(file), "."))
	v.Set(key, value)
	var b bytes.Buffer
	if filepath.Ext(file) == ".toml" {
		b.WriteString(Header)
	}
	if err := v.WriteConfigTo(&b); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	return file, atomicfile.Write(file, b.Bytes(), mode)
}

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// These tests pin viper behaviour the spike relies on or works around. They
// are evidence for the ADR, not tests of skenv code.

func readViper(t *testing.T, file string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigFile(file)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	return v
}

// Keys are case-insensitive: viper lowercases every key it reads, nested
// ones too, and writes them back lower case.
func TestViperQuirkKeyCase(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	f := writeConfig(t, home, "toml", "Manifest = \"~/a\"\nOtherKey = 1\n[Sync]\nInterval = \"1h\"\n")
	v := readViper(t, f)
	if v.GetString("manifest") != "~/a" || v.GetString("MANIFEST") != "~/a" {
		t.Errorf("Manifest not found as manifest: %q", v.GetString("manifest"))
	}
	if v.GetString("sync.interval") != "1h" {
		t.Errorf("nested key: %q", v.GetString("sync.interval"))
	}
	var b bytes.Buffer
	if err := v.WriteConfigTo(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "otherkey = 1") || !strings.Contains(b.String(), "[sync]") {
		t.Errorf("written keys:\n%s", b.String())
	}
	// Two keys differing only in case: YAML accepts the file and one of them
	// silently wins.
	f = writeConfig(t, home, "yaml", "manifest: ~/lower\nManifest: ~/upper\n")
	v = readViper(t, f)
	t.Logf("manifest + Manifest in YAML -> %q", v.GetString("manifest"))
}

// AutomaticEnv reads the process environment (os.LookupEnv); there is no
// hook for an injected getenv, so such tests need t.Setenv and cannot run
// in parallel.
func TestViperQuirkAutomaticEnv(t *testing.T) {
	t.Setenv("SKENV_MANIFEST", "~/env")
	t.Setenv("SKENV_SYNC_INTERVAL", "2h")
	home := t.TempDir()
	f := writeConfig(t, home, "toml", "manifest = \"~/file\"\n[sync]\ninterval = \"1h\"\n")
	v := readViper(t, f)
	v.SetEnvPrefix("SKENV")
	v.AutomaticEnv()
	if v.GetString("manifest") != "~/env" {
		t.Errorf("manifest = %q", v.GetString("manifest"))
	}
	if v.GetString("sync.interval") != "1h" {
		t.Errorf("without a key replacer SKENV_SYNC_INTERVAL should be ignored, got %q", v.GetString("sync.interval"))
	}
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	if v.GetString("sync.interval") != "2h" {
		t.Errorf("with the replacer sync.interval = %q", v.GetString("sync.interval"))
	}
	// A key that is only in the environment is invisible to AllSettings
	// (and so to Unmarshal) unless it is bound or defaulted.
	t.Setenv("SKENV_STORE", "~/store")
	if v.GetString("store") != "~/store" {
		t.Errorf("Get store = %q", v.GetString("store"))
	}
	if _, ok := v.AllSettings()["store"]; ok {
		t.Error("env-only key unexpectedly in AllSettings")
	}
	// WriteConfig writes AllSettings: the env override of a key that is in
	// the file ends up in the file.
	var b bytes.Buffer
	v.SetConfigType("toml")
	if err := v.WriteConfigTo(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "~/env") {
		t.Errorf("expected env value to leak into the written file:\n%s", b.String())
	}
}

// Defaults and Set values are written back too.
func TestViperQuirkWriteIncludesDefaults(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	f := writeConfig(t, home, "toml", "# header comment\nmanifest = \"~/file\"\n")
	v := readViper(t, f)
	v.SetDefault("store", "~/.agents/skills")
	var b bytes.Buffer
	if err := v.WriteConfigTo(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "store = '~/.agents/skills'") || strings.Contains(out, "# header comment") {
		t.Errorf("written:\n%s", out)
	}
}

// With SetConfigName + AddConfigPath, viper walks SupportedExts in order
// (json, toml, yaml, yml, properties, …, dotenv, env, ini) and silently
// takes the first file found.
func TestViperQuirkSeveralFiles(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeConfig(t, home, "toml", "manifest = \"~/toml\"\n")
	writeConfig(t, home, "json", "{\"manifest\": \"~/json\"}")
	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(Dir(home))
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(v.ConfigFileUsed()) != "config.json" || v.GetString("manifest") != "~/json" {
		t.Errorf("used %s manifest %q", v.ConfigFileUsed(), v.GetString("manifest"))
	}
	// Unrelated files named config.* are candidates too.
	home = t.TempDir()
	writeConfig(t, home, "env", "MANIFEST=~/dotenv\n")
	v = viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(Dir(home))
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	t.Logf("config.env alone -> used %s, manifest %q", filepath.Base(v.ConfigFileUsed()), v.GetString("manifest"))
	home = t.TempDir()
	writeConfig(t, home, "ini", "manifest = ~/ini\n")
	v = viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(Dir(home))
	t.Logf("config.ini alone -> err %v", v.ReadInConfig())
}

// The config.toml that `skenv init` wrote with BurntSushi/toml (header
// comment, basic strings, other keys) parses the same with viper's
// pelletier/go-toml/v2.
func TestViperQuirkReadsInitConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	body := Header + "manifest = \"~/src/skills/env.toml\"\nother = 1\n\n[extra]\n  list = [\"a\", \"b\"]\n"
	f := writeConfig(t, home, "toml", body)
	v := readViper(t, f)
	if v.GetString("manifest") != "~/src/skills/env.toml" || v.GetInt("other") != 1 || len(v.GetStringSlice("extra.list")) != 2 {
		t.Errorf("settings = %v", v.AllSettings())
	}
	// viper's own writer uses TOML literal strings, not basic strings.
	var b bytes.Buffer
	if err := v.WriteConfigTo(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "manifest = '~/src/skills/env.toml'") {
		t.Errorf("written:\n%s", b.String())
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal(err)
	}
}

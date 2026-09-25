package config

import (
	"testing"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/parsers/yaml"
	kfile "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// These tests pin koanf behaviour the spike relies on. They are evidence for
// the ADR, not tests of skenv code.

// Keys are case-sensitive: Manifest is not manifest.
func TestKoanfQuirkKeyCase(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	f := writeConfig(t, home, "toml", "Manifest = \"~/a\"\n[Sync]\nInterval = \"1h\"\n")
	k := koanf.New(".")
	if err := k.Load(kfile.Provider(f), toml.Parser()); err != nil {
		t.Fatal(err)
	}
	if k.String("manifest") != "" || k.String("Manifest") != "~/a" || k.String("Sync.Interval") != "1h" {
		t.Errorf("keys = %v", k.Keys())
	}
	cfg, err := Load(home, fakeEnv(nil), nil)
	if err != nil || cfg.Get(Manifest) != "" {
		t.Errorf("Load picked up Manifest: %q %v", cfg.Get(Manifest), err)
	}
}

// Loading several files merges them, the last one winning key by key; koanf
// has no discovery of its own and never complains.
func TestKoanfQuirkSeveralFilesMerge(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	ft := writeConfig(t, home, "toml", "manifest = \"~/toml\"\nstore = \"~/store\"\n")
	fy := writeConfig(t, home, "yaml", "manifest: ~/yaml\n")
	k := koanf.New(".")
	if err := k.Load(kfile.Provider(ft), toml.Parser()); err != nil {
		t.Fatal(err)
	}
	if err := k.Load(kfile.Provider(fy), yaml.Parser()); err != nil {
		t.Fatal(err)
	}
	if k.String("manifest") != "~/yaml" || k.String("store") != "~/store" {
		t.Errorf("merged = %v", k.All())
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each case gets its own temp HOME and a fake getenv, so the cases run in
// parallel without touching the process environment.

var fileBodies = map[string]string{
	"toml": "# skenv configuration, written by `skenv init`\nmanifest = \"~/file/env.toml\"\nother = 1\n",
	"yaml": "# comment\nmanifest: ~/file/env.toml\nother: 1\n",
	"yml":  "manifest: \"~/file/env.toml\"\nother: 1\n",
	"json": "{\"manifest\": \"~/file/env.toml\", \"other\": 1}\n",
}

func writeConfig(t *testing.T, home, ext, body string) string {
	t.Helper()
	f := filepath.Join(Dir(home), "config."+ext)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoadPrecedence(t *testing.T) {
	t.Parallel()
	type tc struct {
		name  string
		files []string // extensions of the config files to create
		env   map[string]string
		flag  string
		want  string
		err   string
	}
	var cases []tc
	for _, ext := range Exts {
		cases = append(cases,
			tc{name: ext + "/flag wins", files: []string{ext}, env: map[string]string{"SKENV_MANIFEST": "~/env/env.toml"}, flag: "~/flag/env.toml", want: "~/flag/env.toml"},
			tc{name: ext + "/env beats file", files: []string{ext}, env: map[string]string{"SKENV_MANIFEST": "~/env/env.toml"}, want: "~/env/env.toml"},
			tc{name: ext + "/file", files: []string{ext}, want: "~/file/env.toml"},
			tc{name: ext + "/empty env and flag fall through", files: []string{ext}, env: map[string]string{"SKENV_MANIFEST": ""}, want: "~/file/env.toml"},
		)
	}
	cases = append(cases,
		tc{name: "none", want: ""},
		tc{name: "none/env", env: map[string]string{"SKENV_MANIFEST": "~/env/env.toml"}, want: "~/env/env.toml"},
		tc{name: "several", files: []string{"toml", "yaml"}, err: "several config files: ~/.config/skenv/config.toml, ~/.config/skenv/config.yaml; keep one"},
		tc{name: "several/yaml+yml", files: []string{"yaml", "yml"}, err: "several config files"},
		tc{name: "several/even with a flag", files: []string{"toml", "json"}, flag: "~/flag/env.toml", err: "several config files"},
	)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			for _, ext := range c.files {
				writeConfig(t, home, ext, fileBodies[ext])
			}
			cfg, err := Load(home, fakeEnv(c.env), map[string]string{Manifest: c.flag})
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Get(Manifest); got != c.want {
				t.Fatalf("manifest = %q, want %q", got, c.want)
			}
			if got := cfg.Get(Store); got != "~/.agents/skills" {
				t.Fatalf("store default = %q", got)
			}
		})
	}
}

func TestLoadStoreKey(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeConfig(t, home, "yaml", "store: ~/file/store\n")
	cfg, err := Load(home, fakeEnv(nil), nil)
	if err != nil || cfg.Get(Store) != "~/file/store" {
		t.Fatalf("store = %q, %v", cfg.Get(Store), err)
	}
	cfg, err = Load(home, fakeEnv(map[string]string{"SKENV_STORE": "~/env/store"}), nil)
	if err != nil || cfg.Get(Store) != "~/env/store" {
		t.Fatalf("store = %q, %v", cfg.Get(Store), err)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	for ext, body := range map[string]string{
		"toml": "manifest = \n",
		"yaml": "manifest: [\n",
		"json": "{\"manifest\": }",
	} {
		home := t.TempDir()
		writeConfig(t, home, ext, body)
		if _, err := Load(home, fakeEnv(nil), nil); err == nil || !strings.Contains(err.Error(), "config.") {
			t.Errorf("%s: err = %v", ext, err)
		}
	}
	home := t.TempDir()
	writeConfig(t, home, "json", "{\"manifest\": 1}")
	if _, err := Load(home, fakeEnv(nil), nil); err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("non-string: err = %v", err)
	}
}

func TestSet(t *testing.T) {
	t.Parallel()
	t.Run("creates config.toml", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		file, err := Set(home, Manifest, "~/kit/env.toml")
		if err != nil {
			t.Fatal(err)
		}
		if file != DefaultFile(home) {
			t.Fatalf("file = %s", file)
		}
		got, _ := os.ReadFile(file)
		if string(got) != Header+"manifest = '~/kit/env.toml'\n" {
			t.Fatalf("config.toml:\n%s", got)
		}
	})
	for _, ext := range Exts {
		t.Run("updates config."+ext, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			f := writeConfig(t, home, ext, fileBodies[ext])
			file, err := Set(home, Manifest, "~/kit/env.toml")
			if err != nil {
				t.Fatal(err)
			}
			if file != f {
				t.Fatalf("file = %s, want %s", file, f)
			}
			cfg, err := Load(home, fakeEnv(nil), nil)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Get(Manifest) != "~/kit/env.toml" {
				t.Fatalf("manifest = %q", cfg.Get(Manifest))
			}
			got, _ := os.ReadFile(file)
			if !strings.Contains(string(got), "other") {
				t.Fatalf("other key lost:\n%s", got)
			}
			if _, err := os.Stat(DefaultFile(home)); ext != "toml" && err == nil {
				t.Fatal("config.toml created next to " + file)
			}
		})
	}
	t.Run("refuses several", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		writeConfig(t, home, "toml", fileBodies["toml"])
		writeConfig(t, home, "json", fileBodies["json"])
		if _, err := Set(home, Manifest, "x"); err == nil {
			t.Fatal("want error")
		}
	})
}

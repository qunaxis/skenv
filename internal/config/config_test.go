package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The same setting in each format.
var samples = map[string]string{
	"config.toml": "# comment\nmanifest = \"~/file/env.toml\"\nother = 1\n",
	"config.yaml": "# comment\nmanifest: ~/file/env.toml\nother: 1\n",
	"config.yml":  "manifest: \"~/file/env.toml\"\nother: 1\n",
	"config.json": "{\"manifest\": \"~/file/env.toml\", \"other\": 1}\n",
}

func write(t *testing.T, home, name, content string) {
	t.Helper()
	p := filepath.Join(Dir(home), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// Precedence per format: flag > SKENV_MANIFEST > config file > default.
func TestResolvePrecedence(t *testing.T) {
	for name, content := range samples {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			write(t, home, name, content)
			cases := []struct {
				flag, env string
				want      string
				src       Source
			}{
				{flag: "/flag/env.toml", env: "/env/env.toml", want: "/flag/env.toml", src: FromFlag},
				{env: "/env/env.toml", want: "/env/env.toml", src: FromEnv},
				{want: "~/file/env.toml", src: FromFile},
			}
			for _, c := range cases {
				got, src, err := Resolve(home, env(map[string]string{"SKENV_MANIFEST": c.env}), "manifest", c.flag, "def")
				if err != nil || got != c.want || src != c.src {
					t.Errorf("flag %q env %q: got %q from %s (%v), want %q from %s", c.flag, c.env, got, src, err, c.want, c.src)
				}
			}
			// A key the file does not set falls back to the default.
			got, src, err := Resolve(home, env(nil), "store", "", "def")
			if err != nil || got != "def" || src != FromDefault {
				t.Errorf("unset key: got %q from %s (%v)", got, src, err)
			}
		})
	}
	t.Run("no file", func(t *testing.T) {
		got, src, err := Resolve(t.TempDir(), env(nil), "manifest", "", "")
		if err != nil || got != "" || src != FromDefault {
			t.Errorf("got %q from %s (%v)", got, src, err)
		}
	})
}

func TestSeveralFiles(t *testing.T) {
	home := t.TempDir()
	write(t, home, "config.toml", samples["config.toml"])
	write(t, home, "config.json", samples["config.json"])
	_, _, err := Resolve(home, env(nil), "manifest", "", "")
	if err == nil || !strings.Contains(err.Error(), "config.toml, config.json") || !strings.Contains(err.Error(), "keep one") {
		t.Fatalf("err = %v", err)
	}
	// A flag or the environment still win without reading the files.
	if got, _, err := Resolve(home, env(map[string]string{"SKENV_MANIFEST": "/e"}), "manifest", "", ""); err != nil || got != "/e" {
		t.Errorf("env with several files: %q, %v", got, err)
	}
}

func TestBadFiles(t *testing.T) {
	for name, content := range map[string]string{
		"config.toml": "manifest = [1]\n",
		"config.yaml": "manifest: [1]\n",
		"config.json": "{\"manifest\": 1}",
		"config.yml":  "manifest: \"unterminated\n",
	} {
		home := t.TempDir()
		write(t, home, name, content)
		if _, _, err := Resolve(home, env(nil), "manifest", "", ""); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v, want an error naming the file", name, err)
		}
	}
	// Keys are case-sensitive in every format.
	home := t.TempDir()
	write(t, home, "config.json", `{"Manifest": "/x"}`)
	if got, src, err := Resolve(home, env(nil), "manifest", "", ""); err != nil || got != "" || src != FromDefault {
		t.Errorf("Manifest must not match manifest: %q from %s (%v)", got, src, err)
	}
	// Empty files are valid.
	for _, name := range Names {
		home := t.TempDir()
		write(t, home, name, "")
		if _, _, err := Resolve(home, env(nil), "manifest", "", ""); err != nil {
			t.Errorf("empty %s: %v", name, err)
		}
	}
}

// Set keeps the format of an existing file and its other keys, and
// creates config.toml when there is none.
func TestSet(t *testing.T) {
	home := t.TempDir()
	p, err := Set(home, "manifest", "~/a/env.toml")
	if err != nil || filepath.Base(p) != "config.toml" {
		t.Fatalf("Set without a file: %s, %v", p, err)
	}
	for name, content := range samples {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			write(t, home, name, content)
			p, err := Set(home, "manifest", "~/new/env.toml")
			if err != nil || filepath.Base(p) != name {
				t.Fatalf("Set: %s, %v", p, err)
			}
			f, err := Load(home)
			if err != nil {
				t.Fatal(err)
			}
			if got, _, _ := f.String("manifest"); got != "~/new/env.toml" {
				t.Errorf("manifest = %q", got)
			}
			if _, ok := f.values["other"]; !ok {
				t.Errorf("other key lost:\n%s", readFile(t, p))
			}
		})
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEnvVar(t *testing.T) {
	for key, want := range map[string]string{"manifest": "SKENV_MANIFEST", "dry-run": "SKENV_DRY_RUN"} {
		if got := EnvVar(key); got != want {
			t.Errorf("EnvVar(%q) = %q, want %q", key, got, want)
		}
	}
}

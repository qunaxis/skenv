package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/schemas"
)

// The same setting in each format.
var samples = map[string]string{
	"config.toml": "# comment\nmanifest = \"~/file/env.toml\"\n",
	"config.yaml": "# comment\nmanifest: ~/file/env.toml\n",
	"config.yml":  "$schema: https://example.org/x.json\nmanifest: \"~/file/env.toml\"\n",
	"config.json": "{\"$schema\": \"https://example.org/x.json\", \"manifest\": \"~/file/env.toml\"}\n",
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
	// Keys are case-sensitive in every format, and unknown keys are errors
	// that name the key and the file.
	for name, content := range map[string]string{
		"config.json": `{"Manifest": "/x"}`,
		"config.toml": "manifests = \"/nonexistent\"\n",
		"config.yaml": "manifest: /x\nstore: /y\n",
	} {
		home := t.TempDir()
		write(t, home, name, content)
		_, _, err := Resolve(home, env(nil), "manifest", "", "")
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "unknown key") {
			t.Errorf("%s: err = %v, want an unknown-key error naming the file", name, err)
		}
	}
	home := t.TempDir()
	write(t, home, "config.toml", "\"$schema\" = 1\n")
	if _, _, err := Resolve(home, env(nil), "manifest", "", ""); err == nil || !strings.Contains(err.Error(), "$schema must be a string") {
		t.Errorf("$schema = 1: %v", err)
	}
	// null is not a string (the schema says the same).
	home = t.TempDir()
	write(t, home, "config.yaml", "manifest: null\n")
	if _, _, err := Resolve(home, env(nil), "manifest", "", "d"); err == nil || !strings.Contains(err.Error(), "manifest must be a string") {
		t.Errorf("manifest: null: %v", err)
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
			if strings.Contains(content, "$schema") {
				if _, ok := f.values["$schema"]; !ok {
					t.Errorf("$schema lost:\n%s", readFile(t, p))
				}
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

// Set changes only the key: comments, the schema directive, other keys and
// their order stay. A skenv directive moves to the running version (the
// latest URL for a test binary).
func TestSetKeepsTheRest(t *testing.T) {
	latest := schemas.URL(schemas.Config, "")
	old := schemas.Base + "v0.3.9/" + schemas.Config
	cases := map[string]struct{ in, want string }{
		"config.toml": {
			in:   "#:schema " + old + "\n# my note: keep this\nmanifest = '~/old'   # where\n",
			want: "#:schema " + latest + "\n# my note: keep this\nmanifest = \"~/new\"   # where\n",
		},
		"config.yaml": {
			in:   "# yaml-language-server: $schema=" + old + "\n# my note\nmanifest: ~/old # where\n",
			want: "# yaml-language-server: $schema=" + latest + "\n# my note\nmanifest: ~/new # where\n",
		},
		"config.yml": {
			in:   "# yaml-language-server: $schema=./mine.json\n# no manifest yet\n",
			want: "# yaml-language-server: $schema=./mine.json\n# no manifest yet\nmanifest: ~/new\n",
		},
		"config.json": {
			in:   `{"$schema": "` + old + `?a=1&b=2", "manifest": "~/old"}`,
			want: "{\n  \"$schema\": \"" + old + "?a=1&b=2\",\n  \"manifest\": \"~/new\"\n}\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			write(t, home, name, c.in)
			p, err := Set(home, "manifest", "~/new")
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, p); got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
	// A new file carries the header and the directive.
	home := t.TempDir()
	p, err := Set(home, "manifest", "~/a <&> b")
	if err != nil {
		t.Fatal(err)
	}
	want := "#:schema " + latest + "\n# skenv configuration, written by `skenv init`\nmanifest = \"~/a <&> b\"\n"
	if got := readFile(t, p); got != want {
		t.Errorf("new file:\n%s\nwant:\n%s", got, want)
	}
	// A new line follows the line endings of the file.
	home = t.TempDir()
	write(t, home, "config.toml", "# c\r\n")
	p, _ = Set(home, "manifest", "~/m")
	if got := readFile(t, p); got != "# c\r\nmanifest = \"~/m\"\r\n" {
		t.Errorf("CRLF:\n%q", got)
	}
	// A key missing from a TOML file goes after the top-level keys.
	home = t.TempDir()
	write(t, home, "config.toml", "# c\n\"$schema\" = \"x\"\n")
	p, _ = Set(home, "manifest", "~/m")
	if got := readFile(t, p); got != "# c\n\"$schema\" = \"x\"\nmanifest = \"~/m\"\n" {
		t.Errorf("appended key:\n%s", got)
	}
}

// A new config file takes the format of --format, with the header where the
// format has comments and the schema directive; later writes keep it.
func TestSetFormat(t *testing.T) {
	for format, want := range map[string]string{
		"toml": "#:schema " + schemas.URL(schemas.Config, schemas.Running()) + "\n# skenv configuration, written by `skenv init`\nmanifest = \"~/a b/skenv.toml\"\n",
		"yaml": "# yaml-language-server: $schema=" + schemas.URL(schemas.Config, schemas.Running()) + "\n# skenv configuration, written by `skenv init`\nmanifest: ~/a b/skenv.toml\n",
		"json": "{\n  \"$schema\": \"" + schemas.URL(schemas.Config, schemas.Running()) + "\",\n  \"manifest\": \"~/a b/skenv.toml\"\n}\n",
	} {
		t.Run(format, func(t *testing.T) {
			home := t.TempDir()
			p, err := SetFormat(home, format, "manifest", "~/a b/skenv.toml")
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(p) != "config."+format {
				t.Fatalf("path = %s", p)
			}
			if got, _ := os.ReadFile(p); string(got) != want {
				t.Errorf("new config.%s:\n%s\nwant:\n%s", format, got, want)
			}
			// Without --format the file keeps its format.
			if p2, err := Set(home, "manifest", "~/other"); err != nil || p2 != p {
				t.Fatalf("Set: %s, %v", p2, err)
			}
			if f, err := Load(home); err != nil || f.Path != p {
				t.Fatalf("Load: %+v, %v", f, err)
			} else if v, _, _ := f.String("manifest"); v != "~/other" {
				t.Errorf("manifest = %q", v)
			}
			// --format that disagrees with the file is an error; nothing changes.
			before, _ := os.ReadFile(p)
			other := "json"
			if format == "json" {
				other = "toml"
			}
			if _, err := SetFormat(home, other, "manifest", "~/x"); err == nil || !strings.Contains(err.Error(), "does not convert it") {
				t.Errorf("mismatch: %v", err)
			}
			if _, err := Target(home, other); err == nil {
				t.Error("Target accepted a mismatch")
			}
			after, _ := os.ReadFile(p)
			entries, _ := os.ReadDir(Dir(home))
			if string(after) != string(before) || len(entries) != 1 {
				t.Errorf("mismatch wrote something: %d files\n%s", len(entries), after)
			}
		})
	}
}

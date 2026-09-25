package skenvfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The same document in every format.
var docs = map[string]string{
	".toml": "[repo]\nharness = \"0.4.0\"\n\n[environment.layout]\ntargets = []\n",
	".yaml": "repo:\n  harness: 0.4.0\nenvironment:\n  layout:\n    targets: []\n",
	".yml":  "repo: {harness: \"0.4.0\"}\nenvironment: {layout: {targets: []}}\n",
	".json": `{"repo": {"harness": "0.4.0"}, "environment": {"layout": {"targets": []}}}`,
}

func TestParseFormats(t *testing.T) {
	for ext, text := range docs {
		d, err := Parse([]byte(text), ext)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		var repo struct {
			Harness string `toml:"harness" yaml:"harness" json:"harness"`
		}
		if err := d.Decode(Repo, &repo); err != nil || repo.Harness != "0.4.0" {
			t.Errorf("%s: repo = %+v, %v", ext, repo, err)
		}
		if !d.Has(Environment) || !d.IsDefined(Environment, "layout", "targets") || d.IsDefined(Environment, "layout", "store") {
			t.Errorf("%s: Has/IsDefined wrong", ext)
		}
		// Unknown keys inside a section are errors in every format.
		var strict struct {
			Other string `toml:"other" yaml:"other" json:"other"`
		}
		if err := d.Decode(Repo, &strict); err == nil || !strings.Contains(err.Error(), "harness") {
			t.Errorf("%s: unknown key accepted: %v", ext, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, c := range []struct{ text, ext, want string }{
		{"harness = \"0.3.0\"\n", ".toml", "unknown top-level keys: harness"},
		{"[[vendor]]\nname = \"x\"\n", ".toml", "unknown top-level keys: vendor"},
		{"layout:\n  store: x\n", ".yaml", "unknown top-level keys: layout"},
		{`{"repo": 1}`, ".json", "repo must be a table"},
		{"x", ".ini", "unsupported format"},
	} {
		if _, err := Parse([]byte(c.text), c.ext); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: err = %v, want %q", c.ext, c.text, err, c.want)
		}
	}
	for _, ext := range []string{".toml", ".yaml", ".json"} {
		if d, err := Parse(nil, ext); err != nil || d.Has(Repo) {
			t.Errorf("empty %s: %v", ext, err)
		}
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	if p, err := Find(dir); p != "" || err != nil {
		t.Fatalf("empty dir: %q %v", p, err)
	}
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("skenv.yaml")
	if p, err := Find(dir); filepath.Base(p) != "skenv.yaml" || err != nil {
		t.Fatalf("one file: %q %v", p, err)
	}
	touch("skenv.toml")
	if _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "several skenv files") {
		t.Fatalf("two files: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "skenv.yaml")); err != nil {
		t.Fatal(err)
	}
	touch("env.toml")
	if _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "no longer read") || !strings.Contains(err.Error(), "[[environment.vendor]]") {
		t.Fatalf("env.toml: %v", err)
	}
}

func TestRewrite(t *testing.T) {
	for _, ext := range []string{".yaml", ".json"} {
		out, err := Rewrite([]byte(docs[ext]), ext, func(doc map[string]any) error {
			repo, err := Table(doc, Repo)
			repo["harness"] = "0.5.0"
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		d, err := Parse(out, ext)
		if err != nil {
			t.Fatalf("%s: %v\n%s", ext, err, out)
		}
		var repo struct {
			Harness string `yaml:"harness" json:"harness"`
		}
		if err := d.Decode(Repo, &repo); err != nil || repo.Harness != "0.5.0" || !d.IsDefined(Environment, "layout", "targets") {
			t.Errorf("%s: %+v %v\n%s", ext, repo, err, out)
		}
	}
	if _, err := Rewrite(nil, ".toml", nil); err == nil {
		t.Error("TOML must be edited as text")
	}
}

// Keys are case-sensitive in JSON too (encoding/json alone would ignore
// case).
func TestJSONKeysAreCaseSensitive(t *testing.T) {
	d, err := Parse([]byte(`{"repo": {"Harness": "0.4.0"}}`), ".json")
	if err != nil {
		t.Fatal(err)
	}
	var repo struct {
		Harness string `yaml:"harness" json:"harness"`
	}
	if err := d.Decode(Repo, &repo); err == nil {
		t.Errorf("Harness accepted as harness: %+v", repo)
	}
}

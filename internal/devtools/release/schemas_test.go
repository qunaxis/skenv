package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scripts/site-schemas.sh publishes the schemas of every release tag, each
// with its versioned "$id", and those of the newest release (not of a
// pre-release, not of main) at the unversioned URLs.
func TestSiteSchemas(t *testing.T) {
	r := newRepo(t)
	const base = "https://qunaxis.github.io/skenv/schemas/"
	schema := func(tag string) string {
		return "{\n  \"$id\": \"" + base + "skenv.schema.json\",\n  \"title\": \"" + tag + "\"\n}\n"
	}
	r.commit("feat: before schemas")
	r.run("git", "tag", "-a", "v0.3.0", "-m", "v0.3.0")
	for _, tag := range []string{"v0.4.0", "v0.10.0", "v0.11.0-rc.1", ""} {
		if err := os.MkdirAll(filepath.Join(r.dir, "schemas"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.dir, "schemas", "skenv.schema.json"), []byte(schema(tag)), 0o644); err != nil {
			t.Fatal(err)
		}
		r.run("git", "add", "-A")
		r.commit("feat: schema " + tag)
		if tag != "" {
			r.run("git", "tag", "-a", tag, "-m", tag)
		}
	}
	out := filepath.Join(r.dir, "site")
	// A leftover of an earlier build is removed; the README stays.
	for _, f := range []string{"v0.2.0/skenv.schema.json", "README.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(out, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, f), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := r.run(filepath.Join(root(t), "scripts", "site-schemas.sh"), out)
	if !strings.Contains(log, "schemas of 3 releases, latest v0.10.0") {
		t.Errorf("log: %s", log)
	}
	want := map[string]string{
		"skenv.schema.json":              "v0.10.0",
		"v0.4.0/skenv.schema.json":       "v0.4.0",
		"v0.10.0/skenv.schema.json":      "v0.10.0",
		"v0.11.0-rc.1/skenv.schema.json": "v0.11.0-rc.1",
	}
	for f, tag := range want {
		data, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if !strings.Contains(string(data), `"$id": "`+base+tag+`/skenv.schema.json"`) || !strings.Contains(string(data), `"title": "`+tag+`"`) {
			t.Errorf("%s:\n%s", f, data)
		}
	}
	for _, gone := range []string{"v0.3.0", "v0.2.0"} {
		if _, err := os.Stat(filepath.Join(out, gone)); err == nil {
			t.Errorf("%s must not be published", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Error("README.md removed")
	}
}

package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/schemas"
)

const root = "../.."

// The committed schemas must match the Go types; CI runs this, so a change
// of a type, its doc comments or a parser constant without `make schemas`
// fails the build.
func TestSchemasUpToDate(t *testing.T) {
	files, err := Generate(root, "")
	if err != nil {
		t.Fatal(err)
	}
	committed, _ := filepath.Glob(filepath.Join(root, "schemas", "*.schema.json"))
	if len(committed) != len(files) {
		t.Errorf("schemas/ has %d schemas, the generator writes %d: run `make schemas`", len(committed), len(files))
	}
	for name, want := range files {
		got, ok := schemas.Get(name)
		if !ok || !bytes.Equal(got, want) {
			t.Errorf("schemas/%s is stale: run `make schemas`", name)
		}
	}
	// A release differs only in "$id".
	v, _ := Generate(root, "0.4.0")
	if got := schemas.SetID(files[schemas.Skenv], schemas.Skenv, "0.4.0"); !bytes.Equal(got, v[schemas.Skenv]) {
		t.Error("SetID does not produce the schema of a release")
	}
}

func compile(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	data, _ := schemas.Get(name)
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(schemas.URL(name, ""), doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(schemas.URL(name, ""))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// instance decodes a document in the format of ext into the JSON data
// model the validator works on.
func instance(t *testing.T, text, ext string) any {
	t.Helper()
	var v any
	var err error
	switch ext {
	case ".toml":
		var m map[string]any
		_, err = toml.Decode(text, &m)
		v = m
	case ".yaml", ".yml":
		err = yaml.Unmarshal([]byte(text), &v)
	case ".json":
		v = json.RawMessage(text)
	}
	if err != nil {
		t.Fatalf("decode %s: %v\n%s", ext, err, text)
	}
	if v == nil {
		v = map[string]any{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// parseSkenv is everything skenv checks in a skenv file without the file
// system: the top level, [environment] and [repo].
func parseSkenv(text, ext string) error {
	if _, _, err := harness.Parse([]byte(text), ext); err != nil {
		return err
	}
	if strings.Contains(text, "environment") {
		if _, err := manifest.Parse([]byte(text), ext); err != nil && !strings.Contains(err.Error(), "no [environment] section") {
			return err
		}
	}
	return nil
}

func parseConfig(t *testing.T, text, ext string) error {
	home := t.TempDir()
	p := filepath.Join(config.Dir(home), "config"+ext)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := config.Load(home)
	if err != nil {
		return err
	}
	for _, k := range config.Keys() {
		if _, _, err := f.String(k); err != nil {
			return err
		}
	}
	return nil
}

const sha = "0123456789abcdef0123456789abcdef01234567"

var placeholders = strings.NewReplacer(
	"<full 40-character commit SHA>", sha,
	"<rev>", sha, "<new-rev>", sha,
)

type example struct {
	where, ext, text string
}

var fenceRe = regexp.MustCompile("(?ms)^```(toml|yaml|json)\n(.*?)^```")

// examples returns the skenv files and tool configs among the code blocks
// of the docs and the golden files of the tests.
func examples(t *testing.T) (skenv, cfg []example) {
	t.Helper()
	docs, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	docs = append(docs, filepath.Join(root, "README.md"))
	var all []example
	for _, p := range docs {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range fenceRe.FindAllStringSubmatch(string(data), -1) {
			all = append(all, example{filepath.Base(p), "." + m[1], m[2]})
		}
	}
	golden, _ := filepath.Glob(filepath.Join(root, "internal", "*", "testdata", "*"))
	for _, p := range golden {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		ext := filepath.Ext(strings.TrimSuffix(filepath.Base(p), ".golden"))
		for _, e := range []string{".toml", ".yaml", ".json"} {
			if strings.Contains(filepath.Base(p), e) {
				ext = e
			}
		}
		all = append(all, example{filepath.Base(p), ext, string(data)})
	}
	for _, e := range all {
		e.text = placeholders.Replace(e.text)
		var top map[string]any
		var err error
		switch e.ext {
		case ".toml":
			_, err = toml.Decode(e.text, &top)
		case ".yaml":
			err = yaml.Unmarshal([]byte(e.text), &top)
		case ".json":
			err = json.Unmarshal([]byte(e.text), &top)
		}
		if err != nil {
			t.Errorf("%s: a %s block does not parse: %v\n%s", e.where, e.ext, err, e.text)
			continue
		}
		_, repo := top["repo"]
		_, env := top["environment"]
		_, man := top["manifest"]
		switch {
		case repo || env:
			skenv = append(skenv, e)
		case man:
			cfg = append(cfg, e)
		}
	}
	return skenv, cfg
}

// Every skenv file and tool config in the docs and the golden files is
// valid for the schema and for skenv.
func TestExamplesValidate(t *testing.T) {
	skenvSchema, cfgSchema := compile(t, schemas.Skenv), compile(t, schemas.Config)
	skenv, cfg := examples(t)
	if len(skenv) < 8 || len(cfg) < 1 {
		t.Errorf("only %d skenv file and %d config examples found", len(skenv), len(cfg))
	}
	for _, e := range skenv {
		if err := skenvSchema.Validate(instance(t, e.text, e.ext)); err != nil {
			t.Errorf("%s: schema rejects:\n%s\n%v", e.where, e.text, err)
		}
		if err := parseSkenv(e.text, e.ext); err != nil {
			t.Errorf("%s: skenv rejects:\n%s\n%v", e.where, e.text, err)
		}
	}
	for _, e := range cfg {
		if err := cfgSchema.Validate(instance(t, e.text, e.ext)); err != nil {
			t.Errorf("%s: schema rejects:\n%s\n%v", e.where, e.text, err)
		}
		if err := parseConfig(t, e.text, e.ext); err != nil {
			t.Errorf("%s: skenv rejects:\n%s\n%v", e.where, e.text, err)
		}
	}
}

// The schema and the parser agree: each document is valid for both or
// rejected by both, and the schema says why.
func TestSchemaAndParserAgree(t *testing.T) {
	s := compile(t, schemas.Skenv)
	vendor := func(fields string) string {
		return "[[environment.vendor]]\n" + fields
	}
	full := `name = "archify"` + "\n" + `repo = "a/b"` + "\n" + `rev = "` + sha + `"` + "\n"
	cases := []struct {
		name, ext, text string
		valid           bool
		msg             string // part of the schema's error
	}{
		{"minimal repo", ".toml", "[repo]\nharness = \"0.4.0\"\nvisibility = \"private\"\n", true, ""},
		{"minimal environment", ".toml", "[environment]\n", true, ""},
		{"$schema in JSON", ".json", `{"$schema": "https://example.org/s.json", "environment": {}}`, true, ""},
		{"$schema in YAML", ".yaml", "$schema: x\nenvironment: {}\n", true, ""},
		{"vendor paths", ".toml", vendor(full+"path = \".\"\n") + vendor(strings.Replace(full, "archify", "b", 1)+"path = \"a/.b/..c\"\n"), true, ""},
		{"empty path is the default", ".toml", vendor(full + "path = \"\"\n"), true, ""},
		{"unknown top-level key", ".toml", "other = 1\n", false, "additional properties 'other'"},
		{"$schema not a string", ".json", `{"$schema": 1}`, false, "want string"},
		{"unknown key in repo", ".toml", "[repo]\nharness = \"0.4.0\"\nvisibility = \"private\"\nbranch = \"main\"\n", false, "additional properties 'branch'"},
		{"unknown key in vendor", ".toml", vendor(full + "tag = \"v1\"\n"), false, "additional properties 'tag'"},
		{"unknown key in layout", ".yaml", "environment:\n  layout:\n    stores: x\n", false, "additional properties 'stores'"},
		{"short rev", ".toml", vendor(strings.Replace(full, sha, sha[:7], 1)), false, "does not match pattern"},
		{"uppercase rev", ".toml", vendor(strings.Replace(full, sha, strings.ToUpper(sha), 1)), false, "does not match pattern"},
		{"missing rev", ".toml", vendor("name = \"x\"\nrepo = \"a/b\"\n"), false, "missing property 'rev'"},
		{"missing repo of own", ".toml", "[[environment.own]]\npath = \"~/x\"\n", false, "missing property 'repo'"},
		{"missing harness", ".toml", "[repo]\nvisibility = \"private\"\n", false, "missing property 'harness'"},
		{"harness not a version", ".toml", "[repo]\nharness = \"0.4\"\nvisibility = \"private\"\n", false, "does not match pattern"},
		{"bad visibility", ".toml", "[repo]\nharness = \"0.4.0\"\nvisibility = \"internal\"\n", false, "value must be one of"},
		{"public with environment", ".toml", "[repo]\nharness = \"0.4.0\"\nvisibility = \"public\"\n[environment]\n", false, "at '/environment': 'not' failed"},
		{"dotted name", ".toml", vendor(strings.Replace(full, "archify", "foo.bar_v2", 1)), false, "does not match pattern"},
		{"double hyphen", ".toml", vendor(strings.Replace(full, "archify", "foo--bar", 1)), false, "does not match pattern"},
		{"long name", ".toml", vendor(strings.Replace(full, "archify", strings.Repeat("a", 65), 1)), false, "maxLength"},
		{"reserved name", ".toml", vendor(strings.Replace(full, "archify", "synced", 1)), false, "at '/environment/vendor/0/name': 'not' failed"},
		{"absolute path", ".toml", vendor(full + "path = \"/etc\"\n"), false, "does not match pattern"},
		{"path escapes", ".toml", vendor(full + "path = \"a/../../b\"\n"), false, "does not match pattern"},
		{"unclean path", ".toml", vendor(full + "path = \"a//b\"\n"), false, "does not match pattern"},
		{"skills_dir escapes", ".toml", "[[environment.own]]\nrepo = \"a/b\"\npath = \"~/x\"\nskills_dir = \"..\"\n", false, "does not match pattern"},
		{"ignore with a slash", ".toml", "[environment.layout]\nignore = [\"a/b\"]\n", false, "does not match pattern"},
		{"repo not a table", ".json", `{"repo": 1}`, false, "want object"},
		{"null list", ".json", `{"environment": {"vendor": null}}`, false, "want array"},
		{"null targets", ".yaml", "environment:\n  layout:\n    targets: ~\n", false, "want array"},
		{"null host", ".json", `{"environment": {"host": {"mac": null}}}`, false, "want object"},
		{"number in runner", ".yaml", "repo: {harness: 0.4.0, visibility: private, runner: [1]}\n", false, "want string"},
		{"unquoted numeric rev", ".yaml", "environment:\n  vendor:\n    - {name: a, repo: a/b, rev: " + strings.Repeat("1", 40) + "}\n", false, "want string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.Validate(instance(t, c.text, c.ext))
			perr := parseSkenv(c.text, c.ext)
			switch {
			case c.valid && (err != nil || perr != nil):
				t.Errorf("want valid; schema: %v; parser: %v", err, perr)
			case !c.valid && (err == nil || perr == nil):
				t.Errorf("want invalid; schema: %v; parser: %v", err, perr)
			case !c.valid && !strings.Contains(fmt.Sprint(err), c.msg):
				t.Errorf("schema error does not say %q:\n%v", c.msg, err)
			}
		})
	}

	cs := compile(t, schemas.Config)
	for _, c := range []struct {
		ext, text string
		valid     bool
	}{
		{".toml", "manifest = \"~/src/skills\"\n", true},
		{".toml", "\"$schema\" = \"x\"\nmanifest = \"~/s\"\n", true},
		{".toml", "manifests = \"/nonexistent\"\n", false},
		{".toml", "manifest = 1\n", false},
		{".yaml", "manifest: ~\n", false},
		{".json", `{"$schema": null}`, false},
	} {
		err := cs.Validate(instance(t, c.text, c.ext))
		perr := parseConfig(t, c.text, c.ext)
		if (err == nil) != c.valid || (perr == nil) != c.valid {
			t.Errorf("config %s %q: schema %v, parser %v, want valid=%v", c.ext, c.text, err, perr, c.valid)
		}
	}
}

// RelPathPattern is the parser's rule for vendor paths and skills_dir.
func TestRelPathPattern(t *testing.T) {
	re := regexp.MustCompile(RelPathPattern)
	var paths []string
	for _, seg := range []string{"a", ".", "..", "...", ".a", "..a", "a.", ""} {
		for _, seg2 := range []string{"", "/b", "/.", "/..", "/", "//c", "/.b"} {
			paths = append(paths, seg+seg2, "/"+seg+seg2)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		text := "[[environment.vendor]]\nname = \"x\"\nrepo = \"a/b\"\nrev = \"" + sha + "\"\npath = " + fmt.Sprintf("%q", p) + "\n"
		_, err := manifest.Parse([]byte(text), ".toml")
		if (err == nil) != re.MatchString(p) {
			t.Errorf("path %q: parser error %v, pattern match %v", p, err, re.MatchString(p))
		}
	}
}

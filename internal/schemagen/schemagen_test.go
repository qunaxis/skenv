package schemagen

import (
	"bytes"
	"encoding/json"
	"errors"
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
// system: the top level, [repository], [user] and [project].
func parseSkenv(text, ext string) error {
	if _, _, err := harness.Parse([]byte(text), ext); err != nil {
		return err
	}
	if strings.Contains(text, "user") {
		if _, err := manifest.Parse([]byte(text), ext); err != nil && !strings.Contains(err.Error(), "no [user] section") {
			return err
		}
	}
	if _, err := manifest.ParseProject([]byte(text), ext); err != nil && !errors.Is(err, manifest.ErrNoProject) {
		return err
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
		_, repo := top["repository"]
		_, env := top["user"]
		_, man := top["manifest"]
		_, project := top["project"]
		switch {
		case repo || env || project:
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
	dep := func(name, fields string) string {
		return "[user.dependencies." + name + "]\n" + fields
	}
	checkoutEntry := "[user.checkouts.a]\nrepo = \"a/b\"\ncheckout_dir = \"~/x\"\n"
	hosts := "[user.git_hosts.work]\nbase_url = \"https://git.example.com\"\nprovider = \"gitlab\"\n"
	full := `repo = "a/b"` + "\n" + `commit = "` + sha + `"` + "\n"
	repo := "[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"private\"\n"
	cases := []struct {
		name, ext, text string
		valid           bool
		msg             string // part of the schema's error
	}{
		{"minimal repository", ".toml", repo, true, ""},
		{"minimal user", ".toml", "[user]\n", true, ""},
		{"$schema in JSON", ".json", `{"$schema": "https://example.org/s.json", "user": {}}`, true, ""},
		{"$schema in YAML", ".yaml", "$schema: x\nuser: {}\n", true, ""},
		{"skill dirs", ".toml", dep("archify", full+"skill_dir = \".\"\n") + dep("b", full+"skill_dir = \"a/.b/..c\"\n"), true, ""},
		{"empty skill_dir is the default", ".toml", dep("x", full+"skill_dir = \"\"\n"), true, ""},
		{"unknown top-level key", ".toml", "other = 1\n", false, "additional properties 'other'"},
		{"$schema not a string", ".json", `{"$schema": 1}`, false, "want string"},
		{"unknown key in repository", ".toml", repo + "branch = \"main\"\n", false, "additional properties 'branch'"},
		{"unknown key in a dependency", ".toml", dep("x", full+"tag = \"v1\"\n"), false, "additional properties 'tag'"},
		{"unknown key in storage", ".yaml", "user:\n  storage:\n    dirs: x\n", false, "additional properties 'dirs'"},
		{"short commit", ".toml", dep("x", strings.Replace(full, sha, sha[:7], 1)), false, "does not match pattern"},
		{"uppercase commit", ".toml", dep("x", strings.Replace(full, sha, strings.ToUpper(sha), 1)), false, "does not match pattern"},
		{"missing commit", ".toml", dep("x", "repo = \"a/b\"\n"), false, "missing property 'commit'"},
		{"missing repo of a checkout", ".toml", "[user.checkouts.a]\ncheckout_dir = \"~/x\"\n", false, "missing property 'repo'"},
		{"missing checkout_dir", ".toml", "[user.checkouts.a]\nrepo = \"a/b\"\n", false, "missing property 'checkout_dir'"},
		{"bad checkout ID", ".toml", "[user.checkouts.A]\nrepo = \"a/b\"\ncheckout_dir = \"~/x\"\n", false, "does not match pattern"},
		{"missing template_version", ".toml", "[repository]\nvisibility = \"private\"\n", false, "missing property 'template_version'"},
		{"template_version not a version", ".toml", "[repository]\ntemplate_version = \"0.4\"\nvisibility = \"private\"\n", false, "does not match pattern"},
		{"bad visibility", ".toml", "[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"internal\"\n", false, "value must be one of"},
		{"public with user", ".toml", "[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"public\"\n[user]\n", false, "at '/user': 'not' failed"},
		{"github runs_on", ".toml", repo + "[repository.ci.github]\nruns_on = [\"ubuntu-latest\"]\n", true, ""},
		{"gitlab tags", ".yaml", "repository: {template_version: 0.4.0, visibility: private, ci: {gitlab: {tags: [saas-linux-small-amd64]}}}\n", true, ""},
		{"two CI tables", ".toml", repo + "[repository.ci.github]\n[repository.ci.gitlab]\n", false, "'not' failed"},
		{"unknown CI", ".toml", repo + "[repository.ci.jenkins]\n", false, "additional properties 'jenkins'"},
		{"dotted name", ".toml", dep(`"foo.bar_v2"`, full), false, "does not match pattern"},
		{"double hyphen", ".toml", dep("foo--bar", full), false, "does not match pattern"},
		{"long name", ".toml", dep(strings.Repeat("a", 65), full), false, "maxLength"},
		{"reserved name", ".toml", dep("synced", full), false, "'not' failed"},
		{"absolute skill_dir", ".toml", dep("x", full+"skill_dir = \"/etc\"\n"), false, "does not match pattern"},
		{"skill_dir escapes", ".toml", dep("x", full+"skill_dir = \"a/../../b\"\n"), false, "does not match pattern"},
		{"unclean skill_dir", ".toml", dep("x", full+"skill_dir = \"a//b\"\n"), false, "does not match pattern"},
		{"skills_dir escapes", ".toml", checkoutEntry + "skills_dir = \"..\"\n", false, "does not match pattern"},
		{"unmanaged with a slash", ".toml", "[user]\nunmanaged = [\"a/b\"]\n", false, "does not match pattern"},
		{"repository not a table", ".json", `{"repository": 1}`, false, "want object"},
		{"null dependencies", ".json", `{"user": {"dependencies": null}}`, false, "want object"},
		{"null enabled", ".yaml", "user:\n  agents:\n    enabled: ~\n", false, "want array"},
		{"null machine", ".json", `{"user": {"machines": {"mac": null}}}`, false, "want object"},
		{"number in runs_on", ".yaml", "repository: {template_version: 0.4.0, visibility: private, ci: {github: {runs_on: [1]}}}\n", false, "want string"},
		{"unquoted numeric commit", ".yaml", "user:\n  dependencies:\n    a: {repo: a/b, commit: " + strings.Repeat("1", 40) + "}\n", false, "want string"},
		{"checkout selection", ".toml", checkoutEntry + "include = [\"alpha\", \"beta-*\"]\nexclude = [\"exp-*\"]\n", true, ""},
		{"empty include", ".toml", checkoutEntry + "include = []\n", true, ""},
		{"duplicate include", ".toml", checkoutEntry + "include = [\"a\", \"a\"]\n", false, "items at 0 and 1 are equal"},
		{"exclude with a slash", ".toml", checkoutEntry + "exclude = [\"a/*\"]\n", false, "does not match pattern"},
		{"null include", ".yaml", "user:\n  checkouts:\n    a: {repo: a/b, checkout_dir: ~/x, include: }\n", false, "want array"},
		{"machine rules", ".toml", checkoutEntry + "[user.machines.laptop]\ninclude = [\"*\"]\nexclude = [\"x\"]\n[user.machines.laptop.checkout_dirs]\na = \"~/y\"\n", true, ""},
		{"agents", ".toml", "[user.agents]\nenabled = [\"claude\"]\nextra_dirs = [\"~/.agents/skills\"]\n[user.agents.paths]\npi = \"~/p\"\n[user.storage]\ndir = \"~/.local/share/skenv/skills\"\n", true, ""},
		{"unknown agent", ".toml", "[user.agents]\nenabled = [\"codex\"]\n", false, "value must be one of"},
		{"declared host", ".toml", hosts + "[user.checkouts.a]\nrepo = \"work:g/sub/r\"\ncheckout_dir = \"~/x\"\n", true, ""},
		{"ssh host in YAML", ".yaml", "user:\n  git_hosts:\n    work: {base_url: \"ssh://git@git.example.com:2222/scm\", provider: gitea}\n", true, ""},
		{"host in JSON", ".json", `{"user": {"git_hosts": {"work": {"base_url": "https://git.example.com/scm"}}}}`, true, ""},
		{"host without base_url", ".toml", "[user.git_hosts.work]\nprovider = \"gitlab\"\n", false, "missing property 'base_url'"},
		{"host url with credentials", ".toml", "[user.git_hosts.work]\nbase_url = \"https://u:t@git.example.com\"\n", false, "does not match pattern"},
		{"host url without scheme", ".toml", "[user.git_hosts.work]\nbase_url = \"git.example.com\"\n", false, "does not match pattern"},
		{"unknown provider", ".toml", "[user.git_hosts.work]\nbase_url = \"https://a.example\"\nprovider = \"bitbucket\"\n", false, "value must be one of"},
		{"unknown key in host", ".toml", hosts + "token = \"x\"\n", false, "additional properties 'token'"},
		{"uppercase alias", ".toml", "[user.git_hosts.Work]\nbase_url = \"https://a.example\"\n", false, "does not match pattern"},
		{"built-in alias", ".toml", "[user.git_hosts.gitlab]\nbase_url = \"https://a.example\"\n", false, "'not' failed"},
		{"minimal project", ".toml", "[project]\n", true, ""},
		{"full project", ".toml", "[project]\ndir = \"skills\"\nmirrors = [\".claude/skills\", \".pi/skills\"]\nmirrors_mode = \"copy\"\n" +
			"[project.dependencies.archify]\n" + full + "[project.from.c]\nrepo = \"a/c\"\nskills_dir = \"s\"\nskills = [\"x\", \"y\"]\ncommit = \"" + sha + "\"\n", true, ""},
		{"project with repository", ".yaml", "repository: {template_version: 0.4.0, visibility: public}\nproject:\n  mirrors: [.claude/skills]\n", true, ""},
		{"project dir is the root", ".toml", "[project]\ndir = \".\"\n", false, "'not' failed"},
		{"project dir escapes", ".toml", "[project]\ndir = \"../skills\"\n", false, "does not match pattern"},
		{"empty mirror", ".toml", "[project]\nmirrors = [\"\"]\n", false, "minLength"},
		{"duplicate mirror", ".toml", "[project]\nmirrors = [\"a\", \"a\"]\n", false, "items at 0 and 1 are equal"},
		{"bad mirrors_mode", ".toml", "[project]\nmirrors_mode = \"hardlink\"\n", false, "value must be one of"},
		{"unknown key in project", ".toml", "[project]\nmirror = [\"a\"]\n", false, "additional properties 'mirror'"},
		{"project dependency short commit", ".toml", "[project.dependencies.x]\n" + strings.Replace(full, sha, sha[:7], 1), false, "does not match pattern"},
		{"from without skills", ".toml", "[project.from.a]\nrepo = \"a/b\"\ncommit = \"" + sha + "\"\n", false, "missing property 'skills'"},
		{"from with empty skills", ".toml", "[project.from.a]\nrepo = \"a/b\"\nskills = []\ncommit = \"" + sha + "\"\n", false, "minItems"},
		{"from with a branch", ".toml", "[project.from.a]\nrepo = \"a/b\"\nskills = [\"x\"]\ncommit = \"main\"\n", false, "does not match pattern"},
		{"project host", ".toml", "[project.git_hosts.work]\nbase_url = \"https://git.example.com\"\n[project.from.a]\nrepo = \"work:g/r\"\nskills = [\"x\"]\ncommit = \"" + sha + "\"\n", true, ""},
		{"project host without base_url", ".toml", "[project.git_hosts.work]\nprovider = \"gitlab\"\n", false, "missing property 'base_url'"},
		{"project built-in alias", ".toml", "[project.git_hosts.codeberg]\nbase_url = \"https://a.example\"\n", false, "'not' failed"},
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
		{".toml", "manifest = \"~/src/skills\"\nmachine = \"laptop\"\n", true},
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

// RelPathPattern is the parser's rule for skill_dir and skills_dir.
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
		text := "[user.dependencies.x]\nrepo = \"a/b\"\ncommit = \"" + sha + "\"\nskill_dir = " + fmt.Sprintf("%q", p) + "\n"
		_, err := manifest.Parse([]byte(text), ".toml")
		if (err == nil) != re.MatchString(p) {
			t.Errorf("path %q: parser error %v, pattern match %v", p, err, re.MatchString(p))
		}
	}
}

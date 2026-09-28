package skenvfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The same document in every format.
var docs = map[string]string{
	".toml": "[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"private\"\n\n[user.agents]\nenabled = []\n",
	".yaml": "repository:\n  template_version: 0.4.0\n  visibility: private\nuser:\n  agents:\n    enabled: []\n",
	".yml":  "repository: {template_version: \"0.4.0\", visibility: private}\nuser: {agents: {enabled: []}}\n",
	".json": `{"repository": {"template_version": "0.4.0", "visibility": "private"}, "user": {"agents": {"enabled": []}}}`,
}

func TestParseFormats(t *testing.T) {
	for ext, text := range docs {
		d, err := Parse([]byte(text), ext)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		var repo struct {
			TemplateVersion string `toml:"template_version" yaml:"template_version" json:"template_version"`
		}
		if err := d.Decode(SectionRepository, &repo); err != nil || repo.TemplateVersion != "0.4.0" {
			t.Errorf("%s: repository = %+v, %v", ext, repo, err)
		}
		if !d.Has(SectionUser) || !d.IsDefined(SectionUser, "agents", "enabled") || d.IsDefined(SectionUser, "agents", "paths") {
			t.Errorf("%s: Has/IsDefined wrong", ext)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, c := range []struct{ text, ext, want string }{
		{"harness = \"0.3.0\"\n", ".toml", "harness (top level) → under [repository]: template_version"},
		{"[[vendor]]\nname = \"x\"\n", ".toml", "vendor: unknown key (known: $schema, project, repository, user)"},
		{"layout:\n  store: x\n", ".yaml", "layout: unknown key"},
		{`{"repository": 1}`, ".json", "repository: must be a table, got a number"},
		{`{"user": {"dependencies": null}}`, ".json", "[user] dependencies: is empty (null)"},
		{"repository:\n  ci:\n    github:\n      runs_on: [ubuntu, 1]\n", ".yaml", "[repository.ci.github] runs_on[1]: must be a string, got a number; quote it"},
		{"user:\n  dependencies:\n    x:\n      commit: 1234\n", ".yaml", "[user.dependencies.x] commit: must be a string"},
		{"x", ".ini", "unsupported format"},
	} {
		if _, err := Parse([]byte(c.text), c.ext); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: err = %v, want %q", c.ext, c.text, err, c.want)
		}
	}
	for _, ext := range []string{".toml", ".yaml", ".json"} {
		if d, err := Parse(nil, ext); err != nil || d.Has(SectionRepository) {
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
	if _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "no longer read") || !strings.Contains(err.Error(), "[user]") {
		t.Fatalf("env.toml: %v", err)
	}
}

// "$schema" is accepted and ignored at the top level in every format; it
// must be a string, and other unknown keys are still errors.
func TestSchemaKey(t *testing.T) {
	for ext, text := range map[string]string{
		".json": `{"$schema": "https://qunaxis.github.io/skenv/schemas/skenv.schema.json", "user": {"storage": {"dir": "~/.skills"}}}`,
		".yaml": "$schema: x\nuser: {}\n",
		".toml": "\"$schema\" = \"x\"\n[user]\n",
	} {
		d, err := Parse([]byte(text), ext)
		if err != nil || !d.Has(SectionUser) {
			t.Errorf("%s: %v", ext, err)
		}
	}
	for ext, c := range map[string]struct{ text, want string }{
		".json": {`{"$schema": 1}`, `"$schema": must be a string`},
		".yaml": {"$schema: [x]\n", `"$schema": must be a string, got a list`},
		".toml": {"\"$schema\" = \"x\"\nother = 1\n", "other: unknown key"},
	} {
		if _, err := Parse([]byte(c.text), ext); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: err = %v, want %q", ext, c.text, err, c.want)
		}
	}
}

func TestSchemaVersion(t *testing.T) {
	for text, want := range map[string]string{
		"[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"private\"\n": "0.4.0",
		"[user]\n": "", // a test binary is a development build
	} {
		d, err := Parse([]byte(text), ".toml")
		if err != nil {
			t.Fatal(err)
		}
		if got := d.SchemaVersion(); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

// Keys are case-sensitive in JSON too (encoding/json alone would ignore
// case).
func TestJSONKeysAreCaseSensitive(t *testing.T) {
	_, err := Parse([]byte(`{"repository": {"Template_version": "0.4.0", "template_version": "0.4.0", "visibility": "private"}}`), ".json")
	if err == nil || !strings.Contains(err.Error(), "[repository] Template_version: unknown key") {
		t.Errorf("Template_version accepted: %v", err)
	}
}

// Every key of the format before 0.6 is an error that names its
// replacement, in every format, all at once.
func TestLegacyKeys(t *testing.T) {
	old := `[repo]
harness = "0.5.0"
visibility = "private"
ci = "gitlab"
runner = ["x"]

[environment.layout]
store = "~/s"
targets = []
ignore = ["p-*"]

[environment.hosts.work]
url = "https://git.example.com"
type = "gitlab"
ssh = "git@git.example.com"

[[environment.own]]
repo = "me/skills"
path = "~/src/skills"
skills = ["a"]

[[environment.vendor]]
name = "x"
repo = "a/b"
path = "x"
rev = "0000000000000000000000000000000000000000"

[environment.host.laptop]
skip = ["a"]

[project]
[[project.vendor]]
name = "y"
repo = "a/b"
path = "y"
rev = "0000000000000000000000000000000000000000"

[project.hosts.work]
url = "https://git.example.com"

[[project.from]]
repo = "me/skills"
skills = ["a"]
rev = "0000000000000000000000000000000000000000"
`
	_, err := Parse([]byte(old), ".toml")
	if err == nil {
		t.Fatal("old format accepted")
	}
	for _, want := range []string{
		MigrationURL,
		"[repo] → [repository]",
		"repo.harness → repository.template_version",
		"repo.runner → repository.ci.github.runs_on (GitHub Actions) or ci.gitlab.tags (GitLab CI)",
		`repo.ci = "gitlab" → a [repository.ci.gitlab] table`,
		"[environment] → [user]",
		"environment.layout.store → user.storage.dir",
		"environment.layout.targets → user.agents (enabled, paths, extra_dirs)",
		"environment.layout.ignore → user.unmanaged",
		"environment.hosts → user.git_hosts",
		"environment.hosts.<alias>.url → base_url",
		"environment.hosts.<alias>.type → provider",
		"environment.hosts.<alias>.ssh → base_url",
		"environment.own → user.checkouts.<id>",
		"environment.own.path → checkout_dir",
		"environment.own.skills → include",
		"environment.vendor → user.dependencies.<name>",
		"environment.vendor.name → the table key",
		"environment.vendor.path → skill_dir",
		"environment.vendor.rev → commit",
		"environment.host → user.machines",
		"environment.host.<name>.skip → exclude",
		"project.vendor → project.dependencies.<name>",
		"project.hosts → project.git_hosts",
		"[[project.from]] → project.from.<id>",
		"project.from.rev → commit",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
	// Old keys under the new section names, in YAML and JSON.
	for ext, text := range map[string]string{
		".yaml": "user:\n  checkouts:\n    me:\n      repo: me/skills\n      path: ~/x\n  dependencies:\n    x:\n      rev: \"1\"\n",
		".json": `{"user": {"checkouts": {"me": {"repo": "me/skills", "path": "~/x"}}, "dependencies": {"x": {"rev": "1"}}}}`,
	} {
		_, err := Parse([]byte(text), ext)
		if err == nil || !strings.Contains(err.Error(), "user.checkouts.<id>.path → checkout_dir") || !strings.Contains(err.Error(), "user.dependencies.<name>.rev → commit") {
			t.Errorf("%s: %v", ext, err)
		}
	}
}

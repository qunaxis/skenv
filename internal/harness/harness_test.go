package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/schemas"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitCheckApply(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "AGENTS.md"), "# Local\n\nKeep this text.\n")
	write(t, filepath.Join(root, ".gitignore"), "/local-only\n")
	if _, _, err := Init(root, "private", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if d, err := Check(root); err != nil || len(d) != 0 {
		t.Fatalf("check after init: %v %v", d, err)
	}
	for _, it := range mustConfig(t, root).managed() {
		lines := strings.SplitN(read(t, filepath.Join(root, it.Path)), "\n", 3)
		switch {
		case it.Comment == jsonHeader:
			if lines[1] != `  "$comment": "managed by skenv `+Latest+` — do not edit; personal settings go to .claude/settings.local.json",` {
				t.Errorf("%s header = %q", it.Path, lines[1])
			}
		case it.Kind == whole && lines[0] != it.Comment+" managed by skenv "+Latest+" — do not edit":
			t.Errorf("%s first line = %q", it.Path, lines[0])
		}
	}
	agents := read(t, filepath.Join(root, "AGENTS.md"))
	if !strings.HasPrefix(agents, "# Local\n\nKeep this text.\n\n<!-- skenv:begin managed by skenv "+Latest+" — do not edit -->\n") {
		t.Errorf("AGENTS.md:\n%s", agents)
	}
	if !strings.HasPrefix(read(t, filepath.Join(root, ".gitignore")), "/local-only\n\n# skenv:begin") {
		t.Error(".gitignore local lines must stay first")
	}

	// Drift in a managed file and inside a block; text outside the block is free.
	write(t, filepath.Join(root, "lefthook.yml"), read(t, filepath.Join(root, "lefthook.yml"))+"# hand edit\n")
	agents = strings.Replace(read(t, filepath.Join(root, "AGENTS.md")), "Keep this text.", "Keep this text, edited.", 1)
	agents = strings.Replace(agents, "### Checking", "### Checking (edited)", 1)
	write(t, filepath.Join(root, "AGENTS.md"), agents+"\nTrailing local notes.\n")
	d, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 2 || d[0].Path != "lefthook.yml" || d[1].Path != "AGENTS.md" {
		t.Fatalf("drift = %v", d)
	}
	changes, err := Apply(root, mustConfig(t, root), false, false)
	if err != nil || len(changes) != 2 {
		t.Fatalf("apply: %v %v", changes, err)
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Fatalf("check after apply: %v", d)
	}
	agents = read(t, filepath.Join(root, "AGENTS.md"))
	if !strings.Contains(agents, "Keep this text, edited.") || !strings.HasSuffix(agents, "\nTrailing local notes.\n") || strings.Contains(agents, "(edited)") {
		t.Errorf("apply must keep local text and restore the block:\n%s", agents)
	}
	if changes, _ := Apply(root, mustConfig(t, root), false, false); len(changes) != 0 {
		t.Errorf("second apply changed %v", changes)
	}
	if _, _, err := Init(root, "private", "", "", false, false); err == nil || !strings.Contains(err.Error(), "already has [repo]") {
		t.Errorf("second init: %v", err)
	}
}

func mustConfig(t *testing.T, root string) *Config {
	t.Helper()
	c, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCheckReportsClaudeMD(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Init(root, "public", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".claude", "CLAUDE.md"), "x")
	write(t, filepath.Join(root, "CLAUDE.md"), "x")
	d, _ := Check(root)
	if len(d) != 2 || d[0].Path != "CLAUDE.md" || d[1].Path != ".claude/CLAUDE.md" || !strings.Contains(d[0].Reason, "AGENTS.md") {
		t.Fatalf("drift = %v", d)
	}
}

func TestMissingAndBrokenBlocks(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Init(root, "public", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "ruff.toml")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".gitignore"), "only local\n")
	write(t, filepath.Join(root, "AGENTS.md"), "<!-- skenv:begin x -->\nno end\n")
	d, _ := Check(root)
	got := map[string]string{}
	for _, x := range d {
		got[x.Path] = x.Reason
	}
	if got["ruff.toml"] != "missing" || !strings.Contains(got[".gitignore"], "missing") || !strings.Contains(got["AGENTS.md"], "without skenv:end") {
		t.Fatalf("drift = %v", d)
	}
	if _, err := Apply(root, mustConfig(t, root), false, false); err == nil {
		t.Error("apply must refuse broken markers")
	}
}

func TestWorkflowVisibility(t *testing.T) {
	private, err := render(&Config{Harness: Latest, Visibility: "private", Runner: DefaultRunner}, items[1])
	if err != nil {
		t.Fatal(err)
	}
	public, err := render(&Config{Harness: Latest, Visibility: "public"}, items[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"runs-on: [self-hosted, linux, docker]", "UV_CACHE_DIR=$RUNNER_TOOL_CACHE/uv-cache", `version: "0.12.10"`, "node-version: 22", "enable-cache: false", `SKENV_VERSION: "` + Latest + `"`, "python: [\"3.9\", \"3.12\"]", "working-directory: skills/${{ matrix.skill }}"} {
		if !strings.Contains(private, want) {
			t.Errorf("private workflow lacks %q", want)
		}
	}
	for _, bad := range []string{"actions/cache", "cache: npm", "cache: pip", "self-hosted"} {
		if strings.Contains(public, bad) {
			t.Errorf("public workflow contains %q", bad)
		}
	}
	if !strings.Contains(public, "runs-on: ubuntu-latest") || strings.Contains(public, "UV_CACHE_DIR") {
		t.Error("public workflow must use ubuntu-latest without the tool cache env")
	}
	if strings.Contains(private, "actions/cache") {
		t.Error("private workflow must not use actions/cache")
	}
	// Every tool version is pinned.
	for _, pin := range []string{`GITLEAKS_VERSION: "8.30.1"`, `RUFF_VERSION: "0.16.9"`, `PYRIGHT_VERSION: "1.1.414"`, `SHELLCHECK_PY_VERSION: "0.11.0.1"`, `MARKDOWNLINT_CLI2_VERSION: "0.23.3"`} {
		if !strings.Contains(public, pin) {
			t.Errorf("missing pin %s", pin)
		}
	}
	custom, _ := render(&Config{Harness: Latest, Visibility: "private", Runner: []string{"self-hosted", "macOS", "native"}}, items[1])
	if !strings.Contains(custom, "runs-on: [self-hosted, macOS, native]") {
		t.Error("runner from skenv.toml not used")
	}
}

func TestConfig(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, ConfigFile)
	write(t, cfg, "[repo]\nharness = \"9.9.9\"\nvisibility = \"private\"\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "newer than") {
		t.Errorf("newer harness: %v", err)
	}
	write(t, cfg, "[repo]\nharness = \"0.4.0\"\nvisibility = \"internal\"\n")
	if _, err := LoadConfig(root); err == nil {
		t.Error("bad visibility accepted")
	}
	write(t, cfg, "[repo]\nharness = \"0.4.0\"\nvisibility = \"public\"\nbranch = \"main\"\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "repo.branch") {
		t.Errorf("unknown key: %v", err)
	}
	// Top-level keys of the skenv.toml of older skenv versions are rejected.
	write(t, cfg, "harness = \"0.3.0\"\nvisibility = \"public\"\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "unknown top-level keys: harness, visibility") {
		t.Errorf("old layout: %v", err)
	}
	write(t, cfg, "[environment]\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "no [repo] section") {
		t.Errorf("no [repo]: %v", err)
	}
	write(t, cfg, "[repo]\nharness = \"0.4\"\nvisibility = \"public\"\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "must be a version") {
		t.Errorf("bad version: %v", err)
	}
	// Update only touches the harness line under [repo], and adds the
	// schema directive of that version.
	write(t, cfg, "# keep\n[environment.layout]\nharness = \"x\"\n\n[repo]\nharness = \"0.1.0\" # old\nvisibility = \"public\"\n")
	if changed, err := Update(root, "0.4.0", false); err != nil || !changed {
		t.Fatal(changed, err)
	}
	want := "#:schema " + schemas.URL(schemas.Skenv, "0.4.0") + "\n# keep\n[environment.layout]\nharness = \"x\"\n\n[repo]\nharness = \"0.4.0\" # old\nvisibility = \"public\"\n"
	if got := read(t, cfg); got != want {
		t.Errorf("Update:\n%s", got)
	}
	if changed, err := Update(root, "0.4.0", false); err != nil || changed {
		t.Errorf("second Update: %v %v", changed, err)
	}
}

// repo init adds [repo] to the skenv file of a manifest repository and
// keeps the rest; a public repository must not carry [environment].
func TestInitNextToEnvironment(t *testing.T) {
	for _, c := range []struct{ name, content string }{
		{"skenv.toml", "# my machines\n[environment.layout]\nstore = \"~/s\"  # kept\n"},
		{"skenv.yaml", "environment:\n  layout:\n    store: ~/s\n"},
		{"skenv.json", `{"environment": {"layout": {"store": "~/s"}}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, c.name), c.content)
			if _, _, err := Init(root, "public", "", "", false, false); err == nil || !strings.Contains(err.Error(), "must not carry [environment]") {
				t.Fatalf("public init with [environment]: %v", err)
			}
			if _, _, err := Init(root, "private", "", "", false, false); err != nil {
				t.Fatal(err)
			}
			got := read(t, filepath.Join(root, c.name))
			if c.name == "skenv.toml" && !strings.HasPrefix(got, "#:schema "+schemas.URL(schemas.Skenv, Latest)+"\n"+c.content) {
				t.Errorf("existing text not kept:\n%s", got)
			}
			if u, ok := docedit.Directive([]byte(got), filepath.Ext(c.name)); !ok || u != schemas.URL(schemas.Skenv, Latest) {
				t.Errorf("directive %q, %v:\n%s", u, ok, got)
			}
			if d, err := Check(root); err != nil || len(d) != 0 {
				t.Errorf("check: %v %v\n%s", d, err, got)
			}
			if _, _, err := Init(root, "private", "", "", false, false); err == nil || !strings.Contains(err.Error(), "already has [repo]") {
				t.Errorf("second init: %v", err)
			}
			// Turning it public is reported by check.
			if err := os.WriteFile(filepath.Join(root, c.name), []byte(strings.Replace(got, "private", "public", 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			d, _ := Check(root)
			found := false
			for _, x := range d {
				found = found || strings.Contains(x.Reason, "must not carry [environment]")
			}
			if !found {
				t.Errorf("public + [environment] not reported: %v", d)
			}
		})
	}
}

// A repository on an older harness is reported by check and moved to
// Latest by Update (what `repo apply` does).
func TestOlderHarness(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Init(root, "private", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(root, "0.3.0", false); err != nil {
		t.Fatal(err)
	}
	d, _ := Check(root)
	if len(d) != 1 || !strings.Contains(d[0].Reason, "harness 0.3.0; this skenv generates "+Latest) {
		t.Fatalf("drift = %v", d)
	}
	if _, err := Update(root, Latest, false); err != nil {
		t.Fatal(err)
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Fatalf("drift after Update = %v", d)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"0.2.0", "0.2.0", 0}, {"0.1.9", "0.2.0", -1}, {"v0.10.0", "0.9.9", 1}, {"0.2.0-dev+abc", "0.2.0", 0}}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d", c.a, c.b, got)
		}
	}
}

func TestApplyKeepsCRLFOutsideBlock(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "a\r\nb\r\n")
	if _, _, err := Init(root, "public", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ".gitignore")); !strings.HasPrefix(got, "a\r\nb\r\n\n# skenv:begin") {
		t.Errorf(".gitignore = %q", got[:30])
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Errorf("drift = %v", d)
	}
}

// Every rendered workflow and hook config must parse.
func TestRenderedFilesParse(t *testing.T) {
	for _, ci := range CIs {
		for _, vis := range []string{"private", "public"} {
			c := &Config{Harness: Latest, Visibility: vis, CI: ci, Runner: DefaultRunner}
			for _, it := range c.managed() {
				text, err := render(c, it)
				if err != nil {
					t.Fatal(err)
				}
				var doc any
				switch {
				case strings.HasSuffix(it.Path, ".yml"), strings.HasSuffix(it.Path, ".yaml"):
					err = yaml.Unmarshal([]byte(text), &doc)
				case it.Comment == jsonHeader:
					err = json.Unmarshal([]byte(text), &doc)
				}
				if err != nil {
					t.Errorf("%s %s %s: %v", ci, vis, it.Path, err)
				}
			}
		}
	}
}

func TestTemplates(t *testing.T) {
	public, _ := render(&Config{Harness: Latest, Visibility: "public"}, items[1])
	private, _ := render(&Config{Harness: Latest, Visibility: "private", Runner: DefaultRunner}, items[1])
	for _, want := range []string{"skenv lint --publish", "DENYLIST: ${{ secrets.SKENV_DENYLIST }}", "SKENV_DENYLIST=\"$list\" skenv lint --publish"} {
		if !strings.Contains(public, want) {
			t.Errorf("public workflow lacks %q", want)
		}
	}
	if strings.Contains(private, "--publish") || strings.Contains(private, "SKENV_DENYLIST") {
		t.Error("private workflow must not run the publication check")
	}
	lhPublic, _ := render(&Config{Harness: Latest, Visibility: "public"}, items[0])
	lhPrivate, _ := render(&Config{Harness: Latest, Visibility: "private", Runner: DefaultRunner}, items[0])
	if !strings.Contains(lhPublic, "publish-check:\n      # Public") || !strings.Contains(lhPublic, "run: skenv lint --publish") {
		t.Errorf("public lefthook lacks the pre-push publication check:\n%s", lhPublic)
	}
	if strings.Contains(lhPrivate, "--publish") {
		t.Error("private lefthook must not run --publish")
	}
	// The stop-list is never echoed.
	if strings.Contains(public, "echo \"$DENYLIST") || strings.Contains(public, "cat \"$list") {
		t.Error("stop-list printed")
	}
	settings, err := render(&Config{Harness: Latest, Visibility: "private", Runner: DefaultRunner}, items[len(items)-1])
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct{ Type, Command string }
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		t.Fatalf("settings.json is not JSON: %v", err)
	}
	post := parsed.Hooks["PostToolUse"]
	if len(post) != 1 || post[0].Matcher != "Edit|Write|MultiEdit" || post[0].Hooks[0].Command != "command -v skenv >/dev/null 2>&1 || exit 0; skenv lint --hook" {
		t.Errorf("hook = %+v", post)
	}
}

// Files skenv does not manage yet are only replaced with --force.
func TestForeignFilesNeedForce(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".github/workflows/check.yml"), "name: hand-written\n")
	if _, _, err := Init(root, "private", "", "", false, false); err == nil || !strings.Contains(err.Error(), ".github/workflows/check.yml exist and are not managed") {
		t.Fatalf("init over a hand-written workflow: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ConfigFile)); err == nil {
		t.Fatal("refused init must not leave skenv.toml behind")
	}
	if _, _, err := Init(root, "private", "", "", false, true); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".claude/settings.json"), `{"permissions": {"allow": ["Bash(ls)"]}}`)
	_, err := Apply(root, mustConfig(t, root), false, false)
	if err == nil || !strings.Contains(err.Error(), ".claude/settings.json") || !strings.Contains(err.Error(), "settings.local.json") {
		t.Fatalf("apply over foreign settings.json: %v", err)
	}
	if got := read(t, filepath.Join(root, ".claude/settings.json")); !strings.Contains(got, "Bash(ls)") {
		t.Fatal("foreign settings.json was overwritten")
	}
	if _, err := Apply(root, mustConfig(t, root), false, true); err != nil {
		t.Fatal(err)
	}
	// Managed files with local edits are drift, not foreign: no --force needed.
	write(t, filepath.Join(root, "ruff.toml"), read(t, filepath.Join(root, "ruff.toml"))+"# edit\n")
	if _, err := Apply(root, mustConfig(t, root), false, false); err != nil {
		t.Fatalf("apply over an edited managed file: %v", err)
	}
}

// repo init and apply edit YAML and JSON in place: comments, the
// directive, key order and formatting stay; [repo] goes to the top.
func TestInitAndUpdateKeepYAMLAndJSON(t *testing.T) {
	url := schemas.URL(schemas.Skenv, Latest)
	old := schemas.Base + "v0.3.9/" + schemas.Skenv
	cases := []struct{ name, in, init, update string }{
		{
			name: "skenv.yaml",
			in:   "# yaml-language-server: $schema=" + old + "\n# my manifest\nenvironment:\n  # where skills live\n  layout:\n    store: ~/.skills\n\n    targets: []\n",
			init: "# yaml-language-server: $schema=" + url + "\nrepo:\n  harness: " + Latest + "\n  visibility: private\n  ci: github\n  runner: [self-hosted, linux, docker]\n" +
				"# my manifest\nenvironment:\n  # where skills live\n  layout:\n    store: ~/.skills\n\n    targets: []\n",
			// 0.1.0 predates the schemas: the directive names the latest.
			update: "# yaml-language-server: $schema=" + schemas.URL(schemas.Skenv, "") + "\nrepo:\n  harness: 0.1.0\n  visibility: private\n  ci: github\n  runner: [self-hosted, linux, docker]\n" +
				"# my manifest\nenvironment:\n  # where skills live\n  layout:\n    store: ~/.skills\n\n    targets: []\n",
		},
		{
			name: "skenv.json",
			in:   `{"environment": {"vendor": [], "layout": {"ignore": ["a<b>&*"]}}}`,
			init: "{\n  \"$schema\": \"" + url + "\",\n  \"repo\": {\n    \"harness\": \"" + Latest + "\",\n    \"visibility\": \"private\",\n    \"ci\": \"github\",\n    \"runner\": [\n      \"self-hosted\",\n      \"linux\",\n      \"docker\"\n    ]\n  },\n" +
				"  \"environment\": {\n    \"vendor\": [],\n    \"layout\": {\n      \"ignore\": [\n        \"a<b>&*\"\n      ]\n    }\n  }\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, c.name)
			write(t, file, c.in)
			if _, _, err := Init(root, "private", "", "", false, false); err != nil {
				t.Fatal(err)
			}
			if got := read(t, file); got != c.init {
				t.Errorf("init:\n%s\nwant:\n%s", got, c.init)
			}
			if c.update == "" {
				return
			}
			if _, err := Update(root, "0.1.0", false); err != nil {
				t.Fatal(err)
			}
			if got := read(t, file); got != c.update {
				t.Errorf("update:\n%s\nwant:\n%s", got, c.update)
			}
		})
	}
}

// Without a skenv file, Init creates skenv.<format>: the header comment
// where the format has comments, [repo] and the schema directive. A format
// that disagrees with an existing file is an error, and nothing is written.
func TestInitFormat(t *testing.T) {
	url := schemas.URL(schemas.Skenv, Latest)
	for format, want := range map[string]string{
		"":     "#:schema " + url + "\n" + repoHeader + "[repo]\nharness    = \"" + Latest + "\"\nvisibility = \"public\"\nci         = \"github\"\n",
		"toml": "#:schema " + url + "\n" + repoHeader + "[repo]\nharness    = \"" + Latest + "\"\nvisibility = \"public\"\nci         = \"github\"\n",
		"yaml": "# yaml-language-server: $schema=" + url + "\n" + repoHeader + "repo:\n  harness: " + Latest + "\n  visibility: public\n  ci: github\n",
		"json": "{\n  \"$schema\": \"" + url + "\",\n  \"repo\": {\n    \"harness\": \"" + Latest + "\",\n    \"visibility\": \"public\",\n    \"ci\": \"github\"\n  }\n}\n",
	} {
		t.Run("format="+format, func(t *testing.T) {
			root := t.TempDir()
			c, _, err := Init(root, "public", "", format, false, false)
			if err != nil {
				t.Fatal(err)
			}
			name := "skenv." + format
			if format == "" {
				name = "skenv.toml"
			}
			if filepath.Base(c.File) != name {
				t.Fatalf("file = %s, want %s", c.File, name)
			}
			if got := read(t, c.File); got != want {
				t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
			}
			if drift, err := Check(root); err != nil || len(drift) > 0 {
				t.Errorf("check: %v %v", drift, err)
			}
		})
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "skenv.yml"), "environment: {}\n")
	if _, _, err := Init(root, "private", "", "json", false, false); err == nil || !strings.Contains(err.Error(), "skenv.yml exists and is YAML; --format json does not convert it") {
		t.Fatalf("mismatch: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 || read(t, filepath.Join(root, "skenv.yml")) != "environment: {}\n" {
		t.Errorf("a refused init wrote files: %v", entries)
	}
	// yml is read, and kept, but --format yaml matches it.
	if c, _, err := Init(root, "private", "", "yaml", false, false); err != nil || filepath.Base(c.File) != "skenv.yml" {
		t.Fatalf("yml: %+v %v", c, err)
	}
}

// The GitLab pipeline has the jobs of the GitHub workflow: tags only for a
// private repository, pinned tools, the publication check only for a
// public one, with the stop-list never printed.
func TestGitLabPipeline(t *testing.T) {
	gitlab := func(visibility string, runner []string) (string, map[string]any) {
		t.Helper()
		c := &Config{Harness: Latest, Visibility: visibility, CI: CIGitLab, Runner: runner}
		it := c.managed()[1]
		if it.Path != ".gitlab-ci.yml" {
			t.Fatalf("managed()[1] = %s", it.Path)
		}
		text, err := render(c, it)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatalf("%s: %v\n%s", visibility, err, text)
		}
		return text, doc
	}
	private, privDoc := gitlab("private", []string{"self-hosted", "linux", "docker"})
	public, pubDoc := gitlab("public", nil)

	// Structure: known top-level keywords, hidden keys, and jobs with a
	// script; every job of check.yml has a counterpart.
	keywords := map[string]bool{"workflow": true, "variables": true, "default": true}
	for _, doc := range []map[string]any{privDoc, pubDoc} {
		var jobs []string
		for k, v := range doc {
			if keywords[k] || strings.HasPrefix(k, ".") {
				continue
			}
			job, ok := v.(map[string]any)
			if !ok || job["script"] == nil || job["timeout"] == nil {
				t.Errorf("job %s has no script or timeout: %v", k, v)
			}
			jobs = append(jobs, k)
		}
		slices.Sort(jobs)
		if strings.Join(jobs, " ") != "gitleaks lint skenv skills" {
			t.Errorf("jobs = %v", jobs)
		}
		vars := doc["variables"].(map[string]any)
		if vars["SKENV_VERSION"] != Latest || vars["GIT_DEPTH"] != "0" {
			t.Errorf("variables = %v", vars)
		}
		matrix := doc["skills"].(map[string]any)["parallel"].(map[string]any)["matrix"].([]any)
		if py := matrix[0].(map[string]any)["PYTHON"].([]any); len(py) != 2 || py[0] != "3.9" || py[1] != "3.12" {
			t.Errorf("matrix = %v", matrix)
		}
	}
	tags := privDoc["default"].(map[string]any)["tags"].([]any)
	if len(tags) != 3 || tags[0] != "self-hosted" || tags[2] != "docker" {
		t.Errorf("private tags = %v", tags)
	}
	if _, ok := pubDoc["default"].(map[string]any)["tags"]; ok || strings.Contains(public, "tags:") {
		t.Error("a public repository must run on shared runners, without tags")
	}
	for _, pin := range []string{`GITLEAKS_VERSION: "8.30.1"`, `RUFF_VERSION: "0.16.9"`, `PYRIGHT_VERSION: "1.1.414"`, `SHELLCHECK_PY_VERSION: "0.11.0.1"`, `MARKDOWNLINT_CLI2_VERSION: "0.23.3"`, `UV_VERSION: "0.12.10"`, "image: node:22-bookworm"} {
		if !strings.Contains(private, pin) || !strings.Contains(public, pin) {
			t.Errorf("missing pin %s", pin)
		}
	}
	for _, want := range []string{"skenv repo check", "skenv lint", "ruff@$RUFF_VERSION\" format --check", "pyright@$PYRIGHT_VERSION", "shellcheck -x", "markdownlint-cli2@", "gitleaks git --redact", "CI_MERGE_REQUEST_DIFF_BASE_SHA", "CI_COMMIT_BEFORE_SHA", "CHECK_PYTHON=\"$PYTHON\""} {
		if !strings.Contains(private, want) {
			t.Errorf("pipeline lacks %q", want)
		}
	}
	if !strings.Contains(public, `SKENV_DENYLIST="$list" skenv lint --publish`) || strings.Contains(private, "--publish") || strings.Contains(private, "SKENV_DENYLIST") {
		t.Error("only a public repository runs the publication check")
	}
	for _, bad := range []string{`echo "$SKENV_DENYLIST`, `cat "$list`, "set -x"} {
		if strings.Contains(public, bad) {
			t.Errorf("stop-list may be printed: %q", bad)
		}
	}
	custom, _ := gitlab("private", []string{"saas-linux-small-amd64"})
	if !strings.Contains(custom, `tags: ["saas-linux-small-amd64"]`) {
		t.Error("runner of the skenv file not used as tags")
	}
}

// The changed-skills script of the GitLab pipeline, run with bash as GitLab
// runs it: a merge request against its diff base, a push against the
// previous head, a first push (all-zero or empty before-SHA, or one not in
// the history) runs every skill with ./check.
func TestGitLabChangedSkills(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	text, err := render(&Config{Harness: Latest, Visibility: "public", CI: CIGitLab}, items[2])
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Scripts map[string][]string `yaml:".scripts"`
	}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	script := strings.Join(doc.Scripts["changed-skills"], "\n")
	if script == "" {
		t.Fatalf("no changed-skills script in:\n%s", text)
	}

	repo := t.TempDir()
	home := t.TempDir()
	var gitEnv []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CI_") {
			gitEnv = append(gitEnv, kv)
		}
	}
	gitEnv = append(gitEnv, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	skill := func(name string, check bool, body string) {
		write(t, filepath.Join(repo, "skills", name, "SKILL.md"), body)
		if check {
			write(t, filepath.Join(repo, "skills", name, "check"), "#!/bin/sh\n")
			if err := os.Chmod(filepath.Join(repo, "skills", name, "check"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	git("init", "--quiet", "-b", "main")
	skill("alpha", true, "a\n")
	skill("beta", true, "b\n")
	skill("gamma", false, "c\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "first")
	first := git("rev-parse", "HEAD")
	skill("alpha", true, "a2\n")
	skill("gamma", false, "c2\n") // changed, but without ./check
	git("add", "-A")
	git("commit", "--quiet", "-m", "second")
	second := git("rev-parse", "HEAD")
	skill("beta", true, "b2\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "third")

	run := func(vars ...string) string {
		t.Helper()
		cmd := exec.Command(bash, "-eo", "pipefail", "-c", script+"\nprintf 'RESULT=%s\\n' \"$skills\"")
		cmd.Dir = repo
		cmd.Env = append(append([]string{}, gitEnv...), vars...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", vars, err, out)
		}
		_, result, _ := strings.Cut(string(out), "RESULT=")
		return strings.TrimSpace(result)
	}
	zero := strings.Repeat("0", 40)
	for _, c := range []struct {
		name string
		vars []string
		want string
	}{
		{"merge request", []string{"CI_PIPELINE_SOURCE=merge_request_event", "CI_MERGE_REQUEST_DIFF_BASE_SHA=" + second, "CI_COMMIT_BEFORE_SHA=" + zero}, "beta"},
		{"merge request over two commits", []string{"CI_PIPELINE_SOURCE=merge_request_event", "CI_MERGE_REQUEST_DIFF_BASE_SHA=" + first, "CI_COMMIT_BEFORE_SHA=" + zero}, "alpha beta"},
		{"merged results", []string{"CI_PIPELINE_SOURCE=merge_request_event", "CI_MERGE_REQUEST_TARGET_BRANCH_SHA=" + second, "CI_MERGE_REQUEST_DIFF_BASE_SHA=" + first}, "beta"},
		{"push", []string{"CI_PIPELINE_SOURCE=push", "CI_COMMIT_BEFORE_SHA=" + first}, "alpha beta"},
		{"push of one commit", []string{"CI_PIPELINE_SOURCE=push", "CI_COMMIT_BEFORE_SHA=" + second}, "beta"},
		{"first push", []string{"CI_PIPELINE_SOURCE=push", "CI_COMMIT_BEFORE_SHA=" + zero}, "alpha beta"},
		{"no before-SHA", []string{"CI_PIPELINE_SOURCE=web"}, "alpha beta"},
		{"before-SHA not in the history", []string{"CI_PIPELINE_SOURCE=push", "CI_COMMIT_BEFORE_SHA=" + strings.Repeat("1", 40)}, "alpha beta"},
	} {
		if got := run(c.vars...); got != c.want {
			t.Errorf("%s: skills = %q, want %q", c.name, got, c.want)
		}
	}
}

// Switching repo.ci: check reports the managed file of the other CI, and
// apply writes the new pipeline and removes the old one; a file of the
// other CI that skenv does not manage stays.
func TestSwitchCI(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Init(root, "private", CIGitHub, "", false, false); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".github/dependabot.yml"), "version: 2\n")
	cfg := filepath.Join(root, ConfigFile)
	text := read(t, cfg)
	if !strings.Contains(text, "ci         = \"github\"\nrunner     = [\"self-hosted\", \"linux\", \"docker\"]  # runs-on of the CI jobs\n") {
		t.Fatalf("skenv.toml:\n%s", text)
	}
	write(t, cfg, strings.Replace(text, `ci         = "github"`, `ci         = "gitlab"`, 1))
	d, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range d {
		got[x.Path] = x.Reason
	}
	if len(d) != 3 || got[".gitlab-ci.yml"] != "missing" || !strings.Contains(got[".github/workflows/check.yml"], `managed file of ci = "github"`) ||
		!strings.Contains(got["AGENTS.md"], "managed block differs") {
		t.Fatalf("drift = %v", d)
	}
	changes, err := Apply(root, mustConfig(t, root), true, false)
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for _, c := range changes {
		plan = append(plan, c.Action+" "+c.Path)
	}
	if strings.Join(plan, ", ") != "create .gitlab-ci.yml, update AGENTS.md, remove .github/workflows/check.yml" {
		t.Fatalf("plan = %v", plan)
	}
	if _, err := os.Stat(filepath.Join(root, ".github/workflows/check.yml")); err != nil {
		t.Fatal("dry run removed the workflow")
	}
	if _, err := Apply(root, mustConfig(t, root), false, false); err != nil {
		t.Fatal(err)
	}
	if d, err := Check(root); err != nil || len(d) != 0 {
		t.Fatalf("check after the switch: %v %v", d, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".github/workflows")); err == nil {
		t.Error("the empty .github/workflows must go")
	}
	if read(t, filepath.Join(root, ".github/dependabot.yml")) != "version: 2\n" {
		t.Error("other files under .github must stay")
	}
	if agents := read(t, filepath.Join(root, "AGENTS.md")); !strings.Contains(agents, "`.gitlab-ci.yml`, GitLab CI") {
		t.Errorf("AGENTS.md block does not name GitLab CI:\n%s", agents)
	}

	// Back to GitHub: a hand-written .gitlab-ci.yml is not skenv's to remove.
	write(t, filepath.Join(root, ".gitlab-ci.yml"), "hand: written\n")
	write(t, cfg, text)
	if _, err := Apply(root, mustConfig(t, root), false, false); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(root, ".gitlab-ci.yml")) != "hand: written\n" {
		t.Error("an unmanaged .gitlab-ci.yml was touched")
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Errorf("an unmanaged file of the other CI is not drift: %v", d)
	}
}

func TestCIValue(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, ConfigFile)
	write(t, cfg, "[repo]\nharness = \""+Latest+"\"\nvisibility = \"public\"\n")
	if c, err := LoadConfig(root); err != nil || c.CI != CIGitHub {
		t.Errorf("without ci: %+v %v", c, err)
	}
	write(t, cfg, "[repo]\nharness = \""+Latest+"\"\nvisibility = \"public\"\nci = \"jenkins\"\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), `repo.ci must be "github" or "gitlab", got "jenkins"`) {
		t.Errorf("bad ci: %v", err)
	}
	for host, want := range map[string]string{"gitlab": CIGitLab, "github": CIGitHub, "gitea": CIGitHub, "generic": CIGitHub, "": CIGitHub} {
		if got := DetectCI(host); got != want {
			t.Errorf("DetectCI(%q) = %s", host, got)
		}
	}
	// A private GitLab repository names its runner tags in the comment.
	c := &Config{Harness: Latest, Visibility: "private", CI: CIGitLab, Runner: DefaultRunner}
	if !strings.Contains(string(c.encode()), `ci         = "gitlab"`+"\n"+`runner     = ["self-hosted", "linux", "docker"]  # tags of the CI jobs`) {
		t.Errorf("encode:\n%s", c.encode())
	}
}

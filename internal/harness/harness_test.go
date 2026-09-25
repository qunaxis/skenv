package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if _, _, err := Init(root, "private", false, false); err != nil {
		t.Fatal(err)
	}
	if d, err := Check(root); err != nil || len(d) != 0 {
		t.Fatalf("check after init: %v %v", d, err)
	}
	for _, it := range items {
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
	if _, _, err := Init(root, "private", false, false); err == nil || !strings.Contains(err.Error(), "already has [repo]") {
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
	if _, _, err := Init(root, "public", false, false); err != nil {
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
	if _, _, err := Init(root, "public", false, false); err != nil {
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
			if _, _, err := Init(root, "public", false, false); err == nil || !strings.Contains(err.Error(), "must not carry [environment]") {
				t.Fatalf("public init with [environment]: %v", err)
			}
			if _, _, err := Init(root, "private", false, false); err != nil {
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
			if _, _, err := Init(root, "private", false, false); err == nil || !strings.Contains(err.Error(), "already has [repo]") {
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
	if _, _, err := Init(root, "private", false, false); err != nil {
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
	if _, _, err := Init(root, "public", false, false); err != nil {
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
	for _, v := range []string{Latest} {
		for _, vis := range []string{"private", "public"} {
			c := &Config{Harness: v, Visibility: vis, Runner: DefaultRunner}
			for _, it := range items {
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
					t.Errorf("%s %s %s: %v", v, vis, it.Path, err)
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
	if _, _, err := Init(root, "private", false, false); err == nil || !strings.Contains(err.Error(), ".github/workflows/check.yml exist and are not managed") {
		t.Fatalf("init over a hand-written workflow: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ConfigFile)); err == nil {
		t.Fatal("refused init must not leave skenv.toml behind")
	}
	if _, _, err := Init(root, "private", false, true); err != nil {
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
			init: "# yaml-language-server: $schema=" + url + "\nrepo:\n  harness: " + Latest + "\n  visibility: private\n  runner: [self-hosted, linux, docker]\n" +
				"# my manifest\nenvironment:\n  # where skills live\n  layout:\n    store: ~/.skills\n\n    targets: []\n",
			// 0.1.0 predates the schemas: the directive names the latest.
			update: "# yaml-language-server: $schema=" + schemas.URL(schemas.Skenv, "") + "\nrepo:\n  harness: 0.1.0\n  visibility: private\n  runner: [self-hosted, linux, docker]\n" +
				"# my manifest\nenvironment:\n  # where skills live\n  layout:\n    store: ~/.skills\n\n    targets: []\n",
		},
		{
			name: "skenv.json",
			in:   `{"environment": {"vendor": [], "layout": {"ignore": ["a<b>&*"]}}}`,
			init: "{\n  \"$schema\": \"" + url + "\",\n  \"repo\": {\n    \"harness\": \"" + Latest + "\",\n    \"visibility\": \"private\",\n    \"runner\": [\n      \"self-hosted\",\n      \"linux\",\n      \"docker\"\n    ]\n  },\n" +
				"  \"environment\": {\n    \"vendor\": [],\n    \"layout\": {\n      \"ignore\": [\n        \"a<b>&*\"\n      ]\n    }\n  }\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, c.name)
			write(t, file, c.in)
			if _, _, err := Init(root, "private", false, false); err != nil {
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

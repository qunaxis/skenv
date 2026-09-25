package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
	if _, _, err := Init(root, "private", false, false); err == nil || !strings.Contains(err.Error(), "already exists") {
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
	write(t, filepath.Join(root, ConfigFile), "harness = \"9.9.9\"\nvisibility = \"private\"\n")
	if _, err := LoadConfig(root); err == nil || !strings.Contains(err.Error(), "unknown to this skenv") {
		t.Errorf("unknown harness: %v", err)
	}
	write(t, filepath.Join(root, ConfigFile), "harness = \"0.2.0\"\nvisibility = \"internal\"\n")
	if _, err := LoadConfig(root); err == nil {
		t.Error("bad visibility accepted")
	}
	write(t, filepath.Join(root, ConfigFile), "# keep\nharness = \"0.1.0\" # old\nvisibility = \"public\"\n")
	if err := SetHarness(root, "0.2.0", false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ConfigFile)); got != "# keep\nharness    = \"0.2.0\"\nvisibility = \"public\"\n" {
		t.Errorf("SetHarness:\n%s", got)
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
	for _, v := range []string{"0.2.0", "0.3.0"} {
		for _, vis := range []string{"private", "public"} {
			c := &Config{Harness: v, Visibility: vis, Runner: DefaultRunner}
			for _, it := range itemsFor(c) {
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

func TestHarness030(t *testing.T) {
	public, _ := render(&Config{Harness: "0.3.0", Visibility: "public"}, items[1])
	private, _ := render(&Config{Harness: "0.3.0", Visibility: "private", Runner: DefaultRunner}, items[1])
	for _, want := range []string{"skenv lint --publish", "DENYLIST: ${{ secrets.SKENV_DENYLIST }}", "SKENV_DENYLIST=\"$list\" skenv lint --publish"} {
		if !strings.Contains(public, want) {
			t.Errorf("public workflow lacks %q", want)
		}
	}
	if strings.Contains(private, "--publish") || strings.Contains(private, "SKENV_DENYLIST") {
		t.Error("private workflow must not run the publication check")
	}
	lhPublic, _ := render(&Config{Harness: "0.3.0", Visibility: "public"}, items[0])
	lhPrivate, _ := render(&Config{Harness: "0.3.0", Visibility: "private", Runner: DefaultRunner}, items[0])
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
	settings, err := render(&Config{Harness: "0.3.0", Visibility: "private", Runner: DefaultRunner}, items[len(items)-1])
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
	if len(post) != 1 || post[0].Matcher != "Edit|Write|MultiEdit" || !strings.Contains(post[0].Hooks[0].Command, "skenv lint --hook") {
		t.Errorf("hook = %+v", post)
	}
}

// A repository on harness 0.2.0 keeps its file set until --upgrade.
func TestUpgradeFrom020(t *testing.T) {
	root := t.TempDir()
	c := &Config{Harness: "0.2.0", Visibility: "private"}
	write(t, filepath.Join(root, ConfigFile), string(c.encode()))
	if _, err := Apply(root, mustConfig(t, root), false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); err == nil {
		t.Fatal("harness 0.2.0 must not create .claude/settings.json")
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Fatalf("0.2.0 check: %v", d)
	}
	if err := SetHarness(root, Latest, false); err != nil {
		t.Fatal(err)
	}
	d, _ := Check(root)
	if len(d) == 0 {
		t.Fatal("after the version bump the files must differ")
	}
	changes, err := Apply(root, mustConfig(t, root), false, false)
	if err != nil {
		t.Fatal(err)
	}
	created := false
	for _, ch := range changes {
		if ch.Path == ".claude/settings.json" && ch.Action == "create" {
			created = true
		}
	}
	if !created {
		t.Errorf("changes = %v", changes)
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Fatalf("check after upgrade: %v", d)
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

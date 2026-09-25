package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if _, _, err := Init(root, "private", false); err != nil {
		t.Fatal(err)
	}
	if d, err := Check(root); err != nil || len(d) != 0 {
		t.Fatalf("check after init: %v %v", d, err)
	}
	for _, it := range items {
		first := strings.SplitN(read(t, filepath.Join(root, it.Path)), "\n", 2)[0]
		if it.Kind == whole && first != it.Comment+" managed by skenv 0.2.0 — do not edit" {
			t.Errorf("%s first line = %q", it.Path, first)
		}
	}
	agents := read(t, filepath.Join(root, "AGENTS.md"))
	if !strings.HasPrefix(agents, "# Local\n\nKeep this text.\n\n<!-- skenv:begin managed by skenv 0.2.0 — do not edit -->\n") {
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
	changes, err := Apply(root, mustConfig(t, root), false)
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
	if changes, _ := Apply(root, mustConfig(t, root), false); len(changes) != 0 {
		t.Errorf("second apply changed %v", changes)
	}
	if _, _, err := Init(root, "private", false); err == nil || !strings.Contains(err.Error(), "already exists") {
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
	if _, _, err := Init(root, "public", false); err != nil {
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
	if _, _, err := Init(root, "public", false); err != nil {
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
	if _, err := Apply(root, mustConfig(t, root), false); err == nil {
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
	for _, want := range []string{"runs-on: [self-hosted, linux, docker]", "UV_CACHE_DIR=$RUNNER_TOOL_CACHE/uv-cache", `version: "0.12.10"`, "node-version: 22", "enable-cache: false", `SKENV_VERSION: "0.2.0"`, "python: [\"3.9\", \"3.12\"]", "working-directory: skills/${{ matrix.skill }}"} {
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
	if _, _, err := Init(root, "public", false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ".gitignore")); !strings.HasPrefix(got, "a\r\nb\r\n\n# skenv:begin") {
		t.Errorf(".gitignore = %q", got[:30])
	}
	if d, _ := Check(root); len(d) != 0 {
		t.Errorf("drift = %v", d)
	}
}

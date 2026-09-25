package manifest

import (
	"strings"
	"testing"
)

const sha = "9e35d2b0b39b0000000000000000000000000000"

func TestParseFull(t *testing.T) {
	m, err := Parse([]byte(`
[layout]
store   = "~/.agents/skills"
targets = ["~/.claude/skills", "~/.pi/agent/skills"]

[[own]]
repo = "qunaxis/skills-private"
path = "~/Personal/lab/skills-private"

[[vendor]]
name = "archify"
repo = "tt-a1i/archify"
path = "archify"
rev  = "` + sha + `"

[[vendor]]
name = "root"
repo = "https://example.com/x/root.git"
rev  = "` + sha + `"

[host."mbp"]
skip = ["bpmn-process-modeler"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Own[0].SkillsDir != "skills" {
		t.Errorf("default skills_dir = %q", m.Own[0].SkillsDir)
	}
	if m.Vendor[1].Path != "." {
		t.Errorf("default vendor path = %q", m.Vendor[1].Path)
	}
	if len(m.Layout.Targets) != 2 || m.Layout.Store != "~/.agents/skills" {
		t.Errorf("layout = %+v", m.Layout)
	}
	if !m.Skipped("mbp")["bpmn-process-modeler"] || len(m.Skipped("other")) != 0 {
		t.Error("host skip")
	}
}

func TestTargetsUnsetVsEmpty(t *testing.T) {
	m, err := Parse([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if m.Layout.Targets != nil {
		t.Error("unset targets must be nil (use the agent table)")
	}
	m, err = Parse([]byte("[layout]\ntargets = []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Layout.Targets == nil || len(m.Layout.Targets) != 0 {
		t.Error("explicit empty targets must override the table")
	}
}

func TestParseErrors(t *testing.T) {
	vendor := func(name, rev string) string {
		return "[[vendor]]\nname = \"" + name + "\"\nrepo = \"a/b\"\nrev = \"" + rev + "\"\n"
	}
	cases := map[string]struct{ src, want string }{
		"M1 duplicate vendor": {vendor("x", sha) + vendor("x", sha), "duplicate skill name"},
		"M2 short rev":        {vendor("x", "9e35d2b"), "full 40-character"},
		"M2 branch rev":       {vendor("x", "main"), "full 40-character"},
		"M2 uppercase rev":    {vendor("x", strings.ToUpper(sha)), "full 40-character"},
		"bad name":            {vendor("X/y", sha), "must match"},
		"reserved name":       {vendor("synced", sha), "reserved"},
		"unknown key":         {"[[own]]\nrepo = \"a/b\"\npath = \"~/x\"\nbranch = \"main\"\n", "unknown keys: own.branch"},
		"own without path":    {"[[own]]\nrepo = \"a/b\"\n", "path is required"},
		"vendor escapes repo": {"[[vendor]]\nname = \"x\"\nrepo = \"a/b\"\npath = \"../x\"\nrev = \"" + sha + "\"\n", "relative path"},
		"syntax":              {"[[vendor]\n", "expected"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(c.src))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// M1 across own and vendor skills.
func TestCheckNames(t *testing.T) {
	m, err := Parse([]byte(`
[[own]]
repo = "me/a"
path = "~/a"
[[own]]
repo = "me/b"
path = "~/b"
[[vendor]]
name = "v"
repo = "x/y"
rev = "` + sha + `"
`))
	if err != nil {
		t.Fatal(err)
	}
	refs, err := m.CheckNames(map[int][]string{0: {"s1"}, 1: {"s2"}})
	if err != nil || len(refs) != 3 || refs[0].Name != "s1" || refs[2].Vendor == nil {
		t.Fatalf("refs = %+v, err = %v", refs, err)
	}
	if _, err := m.CheckNames(map[int][]string{0: {"v"}}); err == nil || !strings.Contains(err.Error(), `"v" is defined twice`) {
		t.Errorf("own/vendor clash: %v", err)
	}
	if _, err := m.CheckNames(map[int][]string{0: {"s"}, 1: {"s"}}); err == nil {
		t.Error("own/own clash not detected")
	}
}

func TestRepo(t *testing.T) {
	cases := []struct{ repo, url, key, name string }{
		{"tt-a1i/archify", "https://github.com/tt-a1i/archify.git", "tt-a1i__archify", "archify"},
		{"https://gitlab.com/g/sub/tool.git", "https://gitlab.com/g/sub/tool.git", "sub__tool", "tool"},
		{"git@github.com:o/r.git", "git@github.com:o/r.git", "o__r", "r"},
		{"https://user:tok@host/o/r", "https://user:tok@host/o/r", "o__r", "r"},
	}
	for _, c := range cases {
		if got := RepoURL(c.repo); got != c.url {
			t.Errorf("RepoURL(%q) = %q", c.repo, got)
		}
		if got := CacheKey(c.repo); got != c.key {
			t.Errorf("CacheKey(%q) = %q", c.repo, got)
		}
		if got := RepoName(c.repo); got != c.name {
			t.Errorf("RepoName(%q) = %q", c.repo, got)
		}
	}
}

func TestLayoutIgnore(t *testing.T) {
	m, err := Parse([]byte("[layout]\nignore = [\"peon-ping-*\", \"tmp?\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"peon-ping-toggle": true, "peon-ping": false, "tmp1": true, "archify": false} {
		if got := m.Layout.Ignored(name); got != want {
			t.Errorf("Ignored(%q) = %v", name, got)
		}
	}
	for _, bad := range []string{`ignore = ["[x"]`, `ignore = ["a/b"]`, `ignore = [""]`} {
		if _, err := Parse([]byte("[layout]\n" + bad + "\n")); err == nil || !strings.Contains(err.Error(), "layout.ignore") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
	m, _ = Parse([]byte("[layout]\nignore = [\"peon-*\"]\n[[vendor]]\nname = \"peon-x\"\nrepo = \"a/b\"\nrev = \"" + sha + "\"\n"))
	if _, err := m.CheckNames(nil); err == nil || !strings.Contains(err.Error(), "matches layout.ignore") {
		t.Errorf("skill matching ignore: %v", err)
	}
}

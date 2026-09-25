package manifest

import (
	"regexp"
	"strings"
	"testing"
)

const sha = "9e35d2b0b39b0000000000000000000000000000"

func TestParseFull(t *testing.T) {
	m, err := Parse([]byte(`
[environment.layout]
store   = "~/.agents/skills"
targets = ["~/.claude/skills", "~/.pi/agent/skills"]

[[environment.own]]
repo = "me/my-skills"
path = "~/src/my-skills"

[[environment.vendor]]
name = "archify"
repo = "tt-a1i/archify"
path = "archify"
rev  = "`+sha+`"

[[environment.vendor]]
name = "root"
repo = "https://example.com/x/root.git"
rev  = "`+sha+`"

[environment.host."mbp"]
skip = ["bpmn-process-modeler"]
`), ".toml")
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
	m, err := Parse([]byte("[environment]\n"), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Layout.Targets != nil {
		t.Error("unset targets must be nil (use the agent table)")
	}
	m, err = Parse([]byte("[environment.layout]\ntargets = []\n"), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Layout.Targets == nil || len(m.Layout.Targets) != 0 {
		t.Error("explicit empty targets must override the table")
	}
}

func own(lines string) string {
	return "[[environment.own]]\nrepo = \"a/b\"\npath = \"~/x\"\n" + lines
}

func TestParseErrors(t *testing.T) {
	vendor := func(name, rev string) string {
		return "[[environment.vendor]]\nname = \"" + name + "\"\nrepo = \"a/b\"\nrev = \"" + rev + "\"\n"
	}
	cases := map[string]struct{ src, want string }{
		"M1 duplicate vendor": {vendor("x", sha) + vendor("x", sha), "duplicate skill name"},
		"M2 short rev":        {vendor("x", "9e35d2b"), "full 40-character"},
		"M2 branch rev":       {vendor("x", "main"), "full 40-character"},
		"M2 uppercase rev":    {vendor("x", strings.ToUpper(sha)), "full 40-character"},
		"bad name":            {vendor("X/y", sha), "single hyphens"},
		"dotted name":         {vendor("foo.bar_v2", sha), "single hyphens"},
		"reserved name":       {vendor("synced", sha), "reserved"},
		"unknown key":         {"[[environment.own]]\nrepo = \"a/b\"\npath = \"~/x\"\nbranch = \"main\"\n", "unknown keys: environment.own.branch"},
		"own without path":    {"[[environment.own]]\nrepo = \"a/b\"\n", "path is required"},
		"vendor escapes repo": {"[[environment.vendor]]\nname = \"x\"\nrepo = \"a/b\"\npath = \"../x\"\nrev = \"" + sha + "\"\n", "relative path"},
		"syntax":              {"[[vendor]\n", "expected"},
		"empty skills":        {own("skills = []\n"), "skills is empty"},
		"bad skills name":     {own("skills = [\"Foo\"]\n"), "single hyphens"},
		"reserved skills":     {own("skills = [\"synced\"]\n"), "reserved"},
		"duplicate skills":    {own("skills = [\"a\", \"a\"]\n"), "lists \"a\" twice"},
		"exclude with slash":  {own("exclude = [\"a/b\"]\n"), "glob over skill names"},
		"exclude bad glob":    {own("exclude = [\"[\"]\n"), "glob over skill names"},
		"exclude empty":       {own("exclude = [\"\"]\n"), "glob over skill names"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(c.src), ".toml")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// M1 across own and vendor skills.
func TestCheckNames(t *testing.T) {
	m, err := Parse([]byte(`
[[environment.own]]
repo = "me/a"
path = "~/a"
[[environment.own]]
repo = "me/b"
path = "~/b"
[[environment.vendor]]
name = "v"
repo = "x/y"
rev = "`+sha+`"
`), ".toml")
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

func TestRepoName(t *testing.T) {
	for repo, name := range map[string]string{
		"https://github.com/tt-a1i/archify.git": "archify",
		"https://gitlab.com/g/sub/tool.git":     "tool",
		"git@github.com:o/r.git":                "r",
		"https://user:tok@host/o/r":             "r",
		"gitlab:g/sub/tool":                     "tool",
	} {
		if got := RepoName(repo); got != name {
			t.Errorf("RepoName(%q) = %q, want %q", repo, got, name)
		}
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/tt-a1i/archify.git":   "github.com/tt-a1i/archify",
		"https://GitHub.com/tt-a1i/archify/":      "github.com/tt-a1i/archify",
		"http://github.com/tt-a1i/archify":        "github.com/tt-a1i/archify",
		"git@github.com:tt-a1i/archify.git":       "github.com/tt-a1i/archify",
		"ssh://git@github.com/tt-a1i/archify.git": "github.com/tt-a1i/archify",
		"ssh://git@github.com:22/tt-a1i/archify":  "github.com/tt-a1i/archify",
		"ssh://git@host:2222/o/r.git":             "host:2222/o/r",
		"https://user:secret@Host.example/o/r":    "host.example/o/r",
		"https://gitlab.com/Group/Sub/Tool.git":   "gitlab.com/Group/Sub/Tool",
		"file:///srv/git/o/r.git":                 "/srv/git/o/r",
		"/srv/git/o/r.git":                        "/srv/git/o/r",
		"/srv/git/o/r/":                           "/srv/git/o/r",
	}
	for in, want := range cases {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCacheKey(t *testing.T) {
	key := CacheKey("https://github.com/tt-a1i/archify.git")
	if !regexp.MustCompile(`^github\.com-tt-a1i-archify-[0-9a-f]{12}$`).MatchString(key) {
		t.Errorf("CacheKey = %q", key)
	}
	// Spellings of one repository share a cache.
	for _, same := range []string{"git@GitHub.com:tt-a1i/archify", "https://user:tok@github.com/tt-a1i/archify/"} {
		if got := CacheKey(same); got != key {
			t.Errorf("CacheKey(%q) = %q, want %q", same, got, key)
		}
	}
	// Different repositories never do, even when the slugs are alike.
	distinct := []string{
		"https://github.com/x/skills",
		"https://gitlab.com/x/skills",
		"https://gitlab.com/a/x/skills",
		"https://gitlab.com/a/x/skills/more",
		"https://gitlab.com/a-x/skills",
		"https://gitlab.com/A/x/skills",
		"https://gitlab.com:8443/a/x/skills",
		"/srv/a/x/skills",
	}
	seen := map[string]string{}
	for _, u := range distinct {
		k := CacheKey(u)
		if prev, ok := seen[k]; ok {
			t.Errorf("CacheKey(%q) = CacheKey(%q) = %q", u, prev, k)
		}
		seen[k] = u
		if strings.Contains(k, "/") || strings.HasPrefix(k, ".") || strings.HasPrefix(k, "-") {
			t.Errorf("CacheKey(%q) = %q is not a plain directory name", u, k)
		}
	}
	for _, u := range []string{"https://user:secret@host/o/r", "https://user:secret%zz@host/o/r", "ssh://user:secret@host:bad/o/r"} {
		if k := CacheKey(u); strings.Contains(k, "secret") || strings.Contains(k, "user") {
			t.Errorf("credentials in cache key %q", k)
		}
	}
	if k := CacheKey("https://host/" + strings.Repeat("a/", 100) + "r"); len(k) > maxSlug+13 {
		t.Errorf("long key %q", k)
	}
}

func TestLayoutIgnore(t *testing.T) {
	m, err := Parse([]byte("[environment.layout]\nignore = [\"peon-ping-*\", \"tmp?\"]\n"), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"peon-ping-toggle": true, "peon-ping": false, "tmp1": true, "archify": false} {
		if got := m.Layout.Ignored(name); got != want {
			t.Errorf("Ignored(%q) = %v", name, got)
		}
	}
	for _, bad := range []string{`ignore = ["[x"]`, `ignore = ["a/b"]`, `ignore = [""]`} {
		if _, err := Parse([]byte("[environment.layout]\n"+bad+"\n"), ".toml"); err == nil || !strings.Contains(err.Error(), "layout.ignore") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
	m, _ = Parse([]byte("[environment.layout]\nignore = [\"peon-*\"]\n[[environment.vendor]]\nname = \"peon-x\"\nrepo = \"a/b\"\nrev = \""+sha+"\"\n"), ".toml")
	if _, err := m.CheckNames(nil); err == nil || !strings.Contains(err.Error(), "matches layout.ignore") {
		t.Errorf("skill matching ignore: %v", err)
	}
}

// Issue #9: skills (allowlist) then exclude (globs) select from the skills
// found in an own repository.
func TestOwnSelect(t *testing.T) {
	found := []string{"alpha", "beta", "exp-one", "exp-two"}
	cases := []struct {
		name, lines string
		want        string // selected names, or the error
	}{
		{"all", "", "alpha beta exp-one exp-two"},
		{"allowlist", `skills = ["beta", "alpha"]`, "alpha beta"},
		{"exclude glob", `exclude = ["exp-*"]`, "alpha beta"},
		{"allowlist then exclude", `skills = ["alpha", "exp-one"]` + "\n" + `exclude = ["exp-*", "none-*"]`, "alpha"},
		{"unknown name", `skills = ["alpha", "typo", "gone"]`, `error: own a/b: skills lists "typo", "gone", not found in ~/x/skills`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Parse([]byte(own(c.lines+"\n")), ".toml")
			if err != nil {
				t.Fatal(err)
			}
			got, err := m.Own[0].Select(found)
			res := strings.Join(got, " ")
			if err != nil {
				res = "error: " + err.Error()
			}
			if !strings.HasPrefix(res, c.want) {
				t.Errorf("got %q, want %q", res, c.want)
			}
		})
	}
	// YAML and JSON read the same fields.
	for ext, text := range map[string]string{
		".yaml": "environment:\n  own:\n    - repo: a/b\n      path: ~/x\n      skills: [alpha]\n      exclude: [\"exp-*\"]\n",
		".json": `{"environment": {"own": [{"repo": "a/b", "path": "~/x", "skills": ["alpha"], "exclude": ["exp-*"]}]}}`,
	} {
		m, err := Parse([]byte(text), ext)
		if err != nil || len(m.Own[0].Skills) != 1 || len(m.Own[0].Exclude) != 1 {
			t.Errorf("%s: %+v %v", ext, m, err)
		}
	}
}

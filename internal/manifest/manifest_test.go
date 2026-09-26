package manifest

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

const sha = "9e35d2b0b39b0000000000000000000000000000"

func TestParseFull(t *testing.T) {
	m, err := ParseIn([]byte(`
[user]
unmanaged = ["peon-*"]

[user.storage]
dir = "store"

[user.agents]
enabled    = ["claude", "pi"]
extra_dirs = ["~/.agents/extra"]

[user.agents.paths]
pi = "~/pi-skills"

[user.checkouts.mine]
repo         = "me/my-skills"
checkout_dir = "."

[user.dependencies.archify]
repo      = "tt-a1i/archify"
skill_dir = "archify"
commit    = "`+sha+`"

[user.dependencies.root]
repo   = "https://example.com/x/root.git"
commit = "`+sha+`"

[user.machines.mbp]
exclude = ["bpmn-process-modeler"]

[user.machines.mbp.checkout_dirs]
mine = "~/elsewhere"
`), ".toml", "/m")
	if err != nil {
		t.Fatal(err)
	}
	c := m.Checkouts["mine"]
	if c.SkillsDir != "skills" || c.ID != "mine" || c.Include != nil {
		t.Errorf("checkout = %+v", c)
	}
	if d := m.Dependencies["root"]; d.SkillDir != "." || d.Name != "root" {
		t.Errorf("default skill_dir = %+v", d)
	}
	if got := m.Path("/home", m.Storage.Dir); got != "/m/store" {
		t.Errorf("storage.dir relative to the file = %q", got)
	}
	if got := m.Path("/home", c.CheckoutDir); got != "/m" {
		t.Errorf("checkout_dir . = %q", got)
	}
	if got := m.Path("/home", m.Agents.Paths["pi"]); got != "/home/pi-skills" {
		t.Errorf("~ = %q", got)
	}
	if mc := m.Machines["mbp"]; mc.Selects("bpmn-process-modeler") || !mc.Selects("x") || mc.CheckoutDirs["mine"] != "~/elsewhere" {
		t.Errorf("machine = %+v", mc)
	}
}

func TestEnabledUnsetVsEmpty(t *testing.T) {
	m, err := Parse([]byte("[user]\n"), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Agents.Enabled != nil {
		t.Error("unset enabled must be nil (detect the agents)")
	}
	for ext, text := range map[string]string{
		".toml": "[user.agents]\nenabled = []\n[user.checkouts.a]\nrepo = \"a/b\"\ncheckout_dir = \"x\"\ninclude = []\n[user.machines.m]\ninclude = []\n",
		".yaml": "user:\n  agents: {enabled: []}\n  checkouts: {a: {repo: a/b, checkout_dir: x, include: []}}\n  machines: {m: {include: []}}\n",
		".json": `{"user": {"agents": {"enabled": []}, "checkouts": {"a": {"repo": "a/b", "checkout_dir": "x", "include": []}}, "machines": {"m": {"include": []}}}}`,
	} {
		m, err := Parse([]byte(text), ext)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if m.Agents.Enabled == nil || m.Checkouts["a"].Include == nil || m.Machines["m"].Include == nil {
			t.Errorf("%s: an explicit [] must stay empty, not unset: %+v", ext, m)
		}
		// include = [] selects nothing: no error, no fallback to all.
		c := m.Checkouts["a"]
		if got, err := c.Select([]string{"x", "y"}); err != nil || len(got) != 0 {
			t.Errorf("%s: include = [] selected %v, %v", ext, got, err)
		}
		if m.Machines["m"].Selects("x") {
			t.Errorf("%s: machine include = [] selected a skill", ext)
		}
	}
}

func checkout(lines string) string {
	return "[user.checkouts.a]\nrepo = \"a/b\"\ncheckout_dir = \"~/x\"\n" + lines
}

func dependency(name, commit string) string {
	return "[user.dependencies." + name + "]\nrepo = \"a/b\"\ncommit = \"" + commit + "\"\n"
}

func TestParseErrors(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"M2 short commit":          {dependency("x", "9e35d2b"), "full 40-character"},
		"M2 branch commit":         {dependency("x", "main"), "full 40-character"},
		"M2 uppercase commit":      {dependency("x", strings.ToUpper(sha)), "full 40-character"},
		"bad name":                 {dependency(`"X"`, sha), "single hyphens"},
		"dotted name":              {dependency(`"foo.bar_v2"`, sha), "single hyphens"},
		"reserved name":            {dependency("synced", sha), "reserved"},
		"unknown key":              {checkout("path2 = \"main\"\n"), "unknown keys: user.checkouts.a.path2"},
		"checkout without dir":     {"[user.checkouts.a]\nrepo = \"a/b\"\n", "checkout_dir is required"},
		"bad checkout id":          {"[user.checkouts.A]\nrepo = \"a/b\"\ncheckout_dir = \"x\"\n", "the ID must be lowercase"},
		"dependency escapes repo":  {"[user.dependencies.x]\nrepo = \"a/b\"\nskill_dir = \"../x\"\ncommit = \"" + sha + "\"\n", "relative path"},
		"syntax":                   {"[[vendor]\n", "expected"},
		"bad include name":         {checkout("include = [\"Foo\"]\n"), "single hyphens"},
		"reserved include":         {checkout("include = [\"synced\"]\n"), "reserved"},
		"duplicate include":        {checkout("include = [\"a\", \"a\"]\n"), "lists \"a\" twice"},
		"exclude with slash":       {checkout("exclude = [\"a/b\"]\n"), "glob over names"},
		"exclude bad glob":         {checkout("exclude = [\"[\"]\n"), "glob over names"},
		"exclude empty":            {checkout("exclude = [\"\"]\n"), "glob over names"},
		"machine unknown checkout": {checkout("[user.machines.m.checkout_dirs]\nb = \"~/y\"\n"), `checkout_dirs: "b" is not a checkout ID (known: a)`},
		"machine bad exclude":      {"[user.machines.m]\nexclude = [\"a/b\"]\n", "user.machines.m.exclude"},
		"codex is not an agent":    {"[user.agents]\nenabled = [\"codex\"]\n", "codex is not a link destination"},
		"unknown agent":            {"[user.agents]\nenabled = [\"cursor\"]\n", `unknown agent "cursor" (built in: claude, pi`},
		"unknown agent path":       {"[user.agents.paths]\ncursor = \"~/c\"\n", "user.agents.paths: unknown agent"},
		"agent twice":              {"[user.agents]\nenabled = [\"pi\", \"pi\"]\n", `lists "pi" twice`},
		"unmanaged slash":          {"[user]\nunmanaged = [\"a/b\"]\n", "user.unmanaged"},
		"old key":                  {"[user]\nlayout = {}\n", "user.layout → user.storage"},
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

// M1 across the skills of checkouts and dependencies.
func TestCheckNames(t *testing.T) {
	m, err := Parse([]byte(`
[user.checkouts.a]
repo = "me/a"
checkout_dir = "~/a"
[user.checkouts.b]
repo = "me/b"
checkout_dir = "~/b"
[user.dependencies.v]
repo = "x/y"
commit = "`+sha+`"
`), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := m.CheckNames(map[string][]string{"a": {"s1"}, "b": {"s2"}})
	if err != nil || len(refs) != 3 || refs[0].Name != "s1" || refs[2].Dependency == nil {
		t.Fatalf("refs = %+v, err = %v", refs, err)
	}
	if _, err := m.CheckNames(map[string][]string{"a": {"v"}}); err == nil || !strings.Contains(err.Error(), `"v" is defined twice`) {
		t.Errorf("checkout/dependency clash: %v", err)
	}
	if _, err := m.CheckNames(map[string][]string{"a": {"s"}, "b": {"s"}}); err == nil || !strings.Contains(err.Error(), "checkout a and checkout b") {
		t.Errorf("checkout/checkout clash: %v", err)
	}
}

// Reordering independent declarations does not change what the manifest
// resolves to: the same checkouts, dependencies, machines and skills in
// the same order, in every format.
func TestReorderingInvariance(t *testing.T) {
	blocks := []string{
		"[user.checkouts.a]\nrepo = \"me/a\"\ncheckout_dir = \"~/a\"\ninclude = [\"x-*\", \"y\"]\n",
		"[user.checkouts.b]\nrepo = \"me/b\"\ncheckout_dir = \"~/b\"\nexclude = [\"z\", \"w\"]\n",
		"[user.dependencies.d1]\nrepo = \"x/y\"\ncommit = \"" + sha + "\"\n",
		"[user.dependencies.d2]\nrepo = \"x/z\"\nskill_dir = \"d2\"\ncommit = \"" + sha + "\"\n",
		"[user.machines.m]\nexclude = [\"d2\", \"y\"]\n",
		"[user.git_hosts.work]\nbase_url = \"https://git.example.com\"\n",
	}
	resolve := func(order []int) string {
		var b strings.Builder
		for _, i := range order {
			b.WriteString(blocks[i])
		}
		m, err := Parse([]byte(b.String()), ".toml")
		if err != nil {
			t.Fatal(err)
		}
		refs, err := m.CheckNames(map[string][]string{"a": {"x-1", "y"}, "b": {"q"}})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range refs {
			from := "dep"
			if r.Checkout != nil {
				from = r.Checkout.ID
			}
			out = append(out, fmt.Sprintf("%s<%s>%v", r.Name, from, m.Machines["m"].Selects(r.Name)))
		}
		for _, c := range m.CheckoutList() {
			out = append(out, c.ID)
			for _, n := range []string{"x-9", "y", "z", "w"} {
				out = append(out, fmt.Sprint(c.Selects(n)))
			}
		}
		for _, d := range m.DependencyList() {
			out = append(out, d.Name+"@"+d.SkillDir)
		}
		return strings.Join(out, " ")
	}
	want := resolve([]int{0, 1, 2, 3, 4, 5})
	for _, order := range [][]int{{5, 4, 3, 2, 1, 0}, {3, 0, 5, 1, 4, 2}, {1, 3, 5, 0, 2, 4}} {
		if got := resolve(order); got != want {
			t.Errorf("order %v resolves to\n%s\nwant\n%s", order, got, want)
		}
	}
	// Reordering list values (include, exclude) changes nothing either.
	m1, _ := Parse([]byte(checkout("include = [\"b*\", \"a\"]\nexclude = [\"bz\", \"ba\"]\n")), ".toml")
	m2, _ := Parse([]byte(checkout("include = [\"a\", \"b*\"]\nexclude = [\"ba\", \"bz\"]\n")), ".toml")
	c1, c2 := m1.Checkouts["a"], m2.Checkouts["a"]
	found := []string{"a", "ba", "bb", "bz", "c"}
	s1, _ := c1.Select(found)
	s2, _ := c2.Select(found)
	if strings.Join(s1, ",") != strings.Join(s2, ",") || strings.Join(s1, ",") != "a,bb" {
		t.Errorf("list order changes the selection: %v vs %v", s1, s2)
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

func TestUnmanaged(t *testing.T) {
	m, err := Parse([]byte("[user]\nunmanaged = [\"peon-ping-*\", \"tmp?\"]\n"), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"peon-ping-toggle": true, "peon-ping": false, "tmp1": true, "archify": false} {
		if got := m.IsUnmanaged(name); got != want {
			t.Errorf("IsUnmanaged(%q) = %v", name, got)
		}
	}
	for _, bad := range []string{`unmanaged = ["[x"]`, `unmanaged = ["a/b"]`, `unmanaged = [""]`} {
		if _, err := Parse([]byte("[user]\n"+bad+"\n"), ".toml"); err == nil || !strings.Contains(err.Error(), "user.unmanaged") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
	m, _ = Parse([]byte("[user]\nunmanaged = [\"peon-*\"]\n"+dependency("peon-x", sha)), ".toml")
	if _, err := m.CheckNames(nil); err == nil || !strings.Contains(err.Error(), "matches user.unmanaged") {
		t.Errorf("skill matching unmanaged: %v", err)
	}
}

// include then exclude (names and globs) select from the skills found in a
// checkout; exclude wins.
func TestCheckoutSelect(t *testing.T) {
	found := []string{"alpha", "beta", "exp-one", "exp-two"}
	cases := []struct {
		name, lines string
		want        string // selected names, or the error
	}{
		{"all", "", "alpha beta exp-one exp-two"},
		{"names", `include = ["beta", "alpha"]`, "alpha beta"},
		{"glob include", `include = ["exp-*"]`, "exp-one exp-two"},
		{"exclude glob", `exclude = ["exp-*"]`, "alpha beta"},
		{"include then exclude", `include = ["alpha", "exp-*"]` + "\n" + `exclude = ["exp-t*", "none-*"]`, "alpha exp-one"},
		{"exclude wins", `include = ["alpha"]` + "\n" + `exclude = ["alpha"]`, ""},
		{"glob matching nothing", `include = ["zzz-*"]`, ""},
		{"unknown name", `include = ["alpha", "typo", "gone"]`, `error: user.checkouts.a: include lists "typo", "gone", not found in ~/x/skills`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Parse([]byte(checkout(c.lines+"\n")), ".toml")
			if err != nil {
				t.Fatal(err)
			}
			co := m.Checkouts["a"]
			got, err := co.Select(found)
			res := strings.Join(got, " ")
			if err != nil {
				res = "error: " + err.Error()
			}
			if !strings.HasPrefix(res, c.want) || (c.want == "" && res != "") {
				t.Errorf("got %q, want %q", res, c.want)
			}
		})
	}
	// YAML and JSON read the same fields.
	for ext, text := range map[string]string{
		".yaml": "user:\n  checkouts:\n    a:\n      repo: a/b\n      checkout_dir: ~/x\n      include: [alpha]\n      exclude: [\"exp-*\"]\n",
		".json": `{"user": {"checkouts": {"a": {"repo": "a/b", "checkout_dir": "~/x", "include": ["alpha"], "exclude": ["exp-*"]}}}}`,
	} {
		m, err := Parse([]byte(text), ext)
		if err != nil || len(m.Checkouts["a"].Include) != 1 || len(m.Checkouts["a"].Exclude) != 1 {
			t.Errorf("%s: %+v %v", ext, m, err)
		}
	}
}

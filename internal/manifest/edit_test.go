package manifest

import (
	"strings"
	"testing"
)

const base = `# my skills
[user.checkouts.skills]
repo = "me/skills"   # mine
checkout_dir = "~/skills"

[user.dependencies.a]
repo      = "x/a"
skill_dir = "."
commit    = "` + sha + `" # pinned on purpose

# the machine table
[user.machines."mbp"]
exclude = []
`

func TestAppendDependency(t *testing.T) {
	out, err := AppendDependency([]byte(base), ".toml", "user", Dependency{Name: "b", Repo: "x/b", SkillDir: "skills/b", Commit: sha})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, base) {
		t.Fatal("existing text must be kept verbatim")
	}
	if !strings.HasSuffix(s, "\n[user.dependencies.b]\nrepo      = \"x/b\"\nskill_dir = \"skills/b\"\ncommit    = \""+sha+"\"\n") {
		t.Errorf("appended:\n%s", s)
	}
	m, _ := Parse(out, ".toml")
	if len(m.Dependencies) != 2 || len(m.Machines) != 1 {
		t.Error("appended table breaks the structure")
	}
	if _, err := AppendDependency([]byte(base), ".toml", "user", Dependency{Name: "a", Repo: "x/a", SkillDir: ".", Commit: sha}); err == nil {
		t.Error("a duplicate table must fail validation")
	}
}

func TestSetDependencyCommit(t *testing.T) {
	next := strings.Repeat("b", 40)
	out, err := SetDependencyCommit([]byte(base), ".toml", "user", "a", next)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(base, sha, next, 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, err := SetDependencyCommit([]byte(base), ".toml", "user", "missing", next); err == nil {
		t.Error("an unknown dependency must fail")
	}
}

func TestRemoveDependency(t *testing.T) {
	with, err := AppendDependency([]byte(base), ".toml", "user", Dependency{Name: "b", Repo: "x/b", SkillDir: ".", Commit: sha})
	if err != nil {
		t.Fatal(err)
	}
	out, err := RemoveDependency(with, ".toml", "user", "a")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, keep := range []string{"# my skills", `repo = "me/skills"   # mine`, "# the machine table", `[user.dependencies.b]`} {
		if !strings.Contains(s, keep) {
			t.Errorf("lost %q:\n%s", keep, s)
		}
	}
	if strings.Contains(s, `[user.dependencies.a]`) || strings.Contains(s, "pinned on purpose") {
		t.Errorf("dependency a not removed:\n%s", s)
	}
	out, err = RemoveDependency(out, ".toml", "user", "b")
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := Parse(out, ".toml"); len(m.Dependencies) != 0 {
		t.Error("dependency b not removed")
	}
}

func TestQuote(t *testing.T) {
	if got := quote("a\"b\\c"); got != `"a\"b\\c"` {
		t.Errorf("quote = %s", got)
	}
}

// YAML and JSON manifests are edited in place too.
func TestEditOtherFormats(t *testing.T) {
	for ext, text := range map[string]string{
		".yaml": "user:\n  checkouts:\n    skills:\n      repo: me/skills # mine\n      checkout_dir: ~/skills\n  dependencies:\n    a:\n      repo: x/a\n      commit: \"" + sha + "\"\n",
		".json": `{"user": {"checkouts": {"skills": {"repo": "me/skills", "checkout_dir": "~/skills"}}, "dependencies": {"a": {"repo": "x/a", "commit": "` + sha + `"}}}}`,
	} {
		t.Run(ext, func(t *testing.T) {
			next := strings.Repeat("b", 40)
			out, err := AppendDependency([]byte(text), ext, "user", Dependency{Name: "b", Repo: "x/b", SkillDir: ".", Commit: sha})
			if err != nil {
				t.Fatal(err)
			}
			if out, err = SetDependencyCommit(out, ext, "user", "a", next); err != nil {
				t.Fatal(err)
			}
			if out, err = RemoveDependency(out, ext, "user", "b"); err != nil {
				t.Fatal(err)
			}
			m, err := Parse(out, ext)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Dependencies) != 1 || m.Dependencies["a"].Commit != next || len(m.Checkouts) != 1 {
				t.Errorf("result:\n%s", out)
			}
			if ext == ".yaml" && !strings.Contains(string(out), "# mine") {
				t.Errorf("comment lost:\n%s", out)
			}
			if _, err := SetDependencyCommit(out, ext, "user", "missing", next); err == nil {
				t.Error("an unknown dependency must fail")
			}
			// Removing the last one leaves an empty table.
			if out, err = RemoveDependency(out, ext, "user", "a"); err != nil {
				t.Fatal(err)
			}
			if m, err := Parse(out, ext); err != nil || len(m.Dependencies) != 0 {
				t.Errorf("last removed: %v\n%s", err, out)
			}
		})
	}
}

func TestParseNeedsUser(t *testing.T) {
	if _, err := Parse([]byte("[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"public\"\n"), ".toml"); err == nil || !strings.Contains(err.Error(), "no [user] section") {
		t.Errorf("err = %v", err)
	}
}

// A dependency table header may carry a comment, spaces and a quoted key.
func TestDependencyHeaderForms(t *testing.T) {
	for _, header := range []string{`[ user . dependencies . b ]  # pinned`, `[user.dependencies."b"]`} {
		text := "[user.storage]\ndir = \"~/s\"\n\n" + header + "\nrepo = \"x/b\"\ncommit = \"" + sha + "\"\n"
		out, err := RemoveDependency([]byte(text), ".toml", "user", "b")
		if err != nil {
			t.Fatalf("%s: %v", header, err)
		}
		if strings.Contains(string(out), `repo = "x/b"`) {
			t.Errorf("%s: not removed:\n%s", header, out)
		}
	}
	// An inline form is not edited: the error says which form works.
	inline := "[user.dependencies]\nb = { repo = \"x/b\", commit = \"" + sha + "\" }\n"
	if _, err := SetDependencyCommit([]byte(inline), ".toml", "user", "b", sha); err == nil || !strings.Contains(err.Error(), "multi-line form") {
		t.Errorf("inline: %v", err)
	}
}

func TestAppendCheckout(t *testing.T) {
	out, err := AppendCheckout([]byte(base), ".toml", Checkout{ID: "more", Repo: "me/more", CheckoutDir: "~/src/more", SkillsDir: "agent/skills", Include: []string{"a2", "b2"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, base) || !strings.HasSuffix(s, "\n[user.checkouts.more]\nrepo         = \"me/more\"\ncheckout_dir = \"~/src/more\"\nskills_dir   = \"agent/skills\"\ninclude      = [\"a2\", \"b2\"]\n") {
		t.Errorf("appended:\n%s", s)
	}
	m, _ := Parse(out, ".toml")
	if len(m.Checkouts) != 2 || len(m.Dependencies) != 1 || len(m.Machines) != 1 || m.Checkouts["more"].SkillsDir != "agent/skills" {
		t.Errorf("appended table breaks the structure: %+v", m)
	}
	// The default skills_dir and no selection write only repo and
	// checkout_dir.
	for _, ext := range []string{".toml", ".yaml", ".json"} {
		data := map[string]string{".toml": base, ".yaml": "# c\nuser:\n  checkouts: {}\n", ".json": `{"user": {}}`}[ext]
		out, err := AppendCheckout([]byte(data), ext, Checkout{ID: "x", Repo: "me/x", CheckoutDir: "~/x", SkillsDir: DefaultSkillsDir})
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		m, err := Parse(out, ext)
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if c := m.Checkouts["x"]; c.Repo != "me/x" || c.Include != nil || strings.Contains(string(out), "skills_dir") {
			t.Errorf("%s:\n%s", ext, out)
		}
	}
}

// Commented keys right under a table belong to it: a new dependency table
// goes after them, so uncommenting them later keeps them in their own
// table.
func TestAppendDependencyKeepsTrailingComments(t *testing.T) {
	out, err := AddUser(nil, ".toml", &Checkout{ID: "skills", Repo: "me/skills", CheckoutDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	if out, err = AppendDependency(out, ".toml", "user", Dependency{Name: "b", Repo: "x/b", SkillDir: "skills/b", Commit: sha}); err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if i, j := strings.Index(s, "# exclude = "), strings.Index(s, "[user.dependencies.b]"); i < 0 || j < i {
		t.Fatalf("the dependency table splits the checkout from its commented keys:\n%s", s)
	}
	uncommented := strings.Replace(s, `# exclude = ["experimental-*"]`, `exclude = ["experimental-*"]`, 1)
	m, err := Parse([]byte(uncommented), ".toml")
	if err != nil {
		t.Fatalf("uncommented exclude: %v\n%s", err, uncommented)
	}
	if len(m.Checkouts) != 1 || len(m.Checkouts["skills"].Exclude) != 1 || len(m.Dependencies) != 1 {
		t.Errorf("parsed: %+v", m)
	}
}

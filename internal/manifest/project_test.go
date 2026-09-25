package manifest

import (
	"errors"
	"strings"
	"testing"
)

func TestParseProjectDefaults(t *testing.T) {
	p, err := ParseProject([]byte("[project]\n"), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != DefaultProjectDir || p.MirrorsMode != MirrorSymlink || len(p.Mirrors) != 0 {
		t.Errorf("defaults: %+v", p)
	}
	if _, err := ParseProject([]byte("[environment]\n"), ".toml"); !errors.Is(err, ErrNoProject) {
		t.Errorf("without [project]: %v", err)
	}
}

func TestProjectSkills(t *testing.T) {
	text := `[project]
mirrors = [".claude/skills"]

[[project.vendor]]
name = "zeta"
repo = "x/z"
rev  = "` + sha + `"

[[project.from]]
repo   = "me/skills"
skills = ["beta", "alpha"]
rev    = "` + sha + `"
`
	p, err := ParseProject([]byte(text), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range p.Skills() {
		got = append(got, s.Name+"="+s.Repo+":"+s.Path)
	}
	want := "alpha=me/skills:skills/alpha beta=me/skills:skills/beta zeta=x/z:."
	if strings.Join(got, " ") != want {
		t.Errorf("skills = %v, want %s", got, want)
	}
	if s, ok := p.Skill("beta"); !ok || s.From == nil || s.FromAt != 0 {
		t.Errorf("Skill(beta) = %+v, %v", s, ok)
	}
}

func TestParseProjectErrors(t *testing.T) {
	v := func(name string) string {
		return "[[project.vendor]]\nname = \"" + name + "\"\nrepo = \"x/a\"\nrev = \"" + sha + "\"\n"
	}
	for _, c := range []struct{ text, want string }{
		{"[project]\ndir = \".\"\n", "must be a relative directory"},
		{"[project]\ndir = \"/abs\"\n", "must be a relative directory"},
		{"[project]\nmirrors = [\".agents/skills\"]\n", "overlaps"},
		{"[project]\nmirrors = [\".agents\"]\n", "overlaps"},
		{"[project]\nmirrors = [\"a\", \"a/b\"]\n", "overlaps"},
		{"[project]\nmirrors = [\"../x\"]\n", "must be a relative directory"},
		{"[project]\nmirrors_mode = \"hardlink\"\n", `must be "symlink" or "copy"`},
		{"[project]\ndirs = \"x\"\n", "unknown keys"},
		{v("a") + v("a"), `project skill "a" is defined twice`},
		{v("a") + "[[project.from]]\nrepo = \"me/s\"\nskills = [\"a\"]\nrev = \"" + sha + "\"\n", `project skill "a" is defined twice: vendor x/a and from me/s`},
		{"[[project.from]]\nrepo = \"me/s\"\nrev = \"" + sha + "\"\n", "skills is required"},
		{"[[project.from]]\nrepo = \"me/s\"\nskills = [\"a\", \"a\"]\nrev = \"" + sha + "\"\n", `lists "a" twice`},
		{"[[project.from]]\nrepo = \"me/s\"\nskills = [\"a\"]\nrev = \"main\"\n", "full 40-character"},
		{"[[project.from]]\nskills = [\"a\"]\nrev = \"" + sha + "\"\n", "repo is required"},
		{"[[project.vendor]]\nname = \"Bad\"\nrepo = \"x/a\"\nrev = \"" + sha + "\"\n", "project.vendor"},
	} {
		if _, err := ParseProject([]byte(c.text), ".toml"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want %q", c.text, err, c.want)
		}
	}
}

const projectBase = `# my project
[repo]
harness    = "0.4.0"
visibility = "public"

[project]
dir     = ".agents/skills"
mirrors = [".claude/skills"] # Claude Code

[[project.from]]
repo   = "me/skills"
skills = ["alpha"]
rev    = "` + sha + `" # pinned

# the machine
[environment.layout]
store = "~/.agents/skills"
`

// A new [[project.vendor]] goes after the last table of [project], not
// after an unrelated section.
func TestAppendProjectVendor(t *testing.T) {
	out, err := AppendVendor([]byte(projectBase), ".toml", "project", Vendor{Name: "b", Repo: "x/b", Path: "skills/b", Rev: sha})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(projectBase, "\n# the machine", "\n[[project.vendor]]\nname = \"b\"\nrepo = \"x/b\"\npath = \"skills/b\"\nrev  = \""+sha+"\"\n\n# the machine", 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	p, err := ParseProject(out, ".toml")
	if err != nil || len(p.Vendor) != 1 {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Parse(out, ".toml"); err != nil {
		t.Errorf("[environment] broken: %v", err)
	}
	next := strings.Repeat("b", 40)
	if out, err = SetVendorRev(out, ".toml", "project", "b", next); err != nil {
		t.Fatal(err)
	}
	if out, err = SetFromRev(out, ".toml", 0, next); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `rev    = "`+next+`" # pinned`) {
		t.Errorf("from rev not set in place:\n%s", out)
	}
	if out, err = RemoveVendor(out, ".toml", "project", "b"); err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(projectBase, sha, next, 1); string(out) != want {
		t.Errorf("after remove:\n%s\nwant:\n%s", out, want)
	}
	if _, err := SetFromRev(out, ".toml", 1, next); err == nil {
		t.Error("a missing from entry must fail")
	}
	if _, err := RemoveVendor(out, ".toml", "environment", "b"); err == nil {
		t.Error("the section must match")
	}
}

func TestEditProjectOtherFormats(t *testing.T) {
	for ext, text := range map[string]string{
		".yaml": "project:\n  mirrors: [.claude/skills]\n  from:\n    - repo: me/skills\n      skills: [alpha]\n      rev: \"" + sha + "\"\n",
		".json": `{"project": {"mirrors": [".claude/skills"], "from": [{"repo": "me/skills", "skills": ["alpha"], "rev": "` + sha + `"}]}}`,
	} {
		t.Run(ext, func(t *testing.T) {
			next := strings.Repeat("b", 40)
			out, err := AppendVendor([]byte(text), ext, "project", Vendor{Name: "b", Repo: "x/b", Path: ".", Rev: sha})
			if err != nil {
				t.Fatal(err)
			}
			if out, err = SetVendorRev(out, ext, "project", "b", next); err != nil {
				t.Fatal(err)
			}
			if out, err = SetFromRev(out, ext, 0, next); err != nil {
				t.Fatal(err)
			}
			p, err := ParseProject(out, ext)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Vendor) != 1 || p.Vendor[0].Rev != next || p.From[0].Rev != next {
				t.Errorf("result:\n%s", out)
			}
			if out, err = RemoveVendor(out, ext, "project", "b"); err != nil {
				t.Fatal(err)
			}
			if p, _ := ParseProject(out, ext); len(p.Vendor) != 0 {
				t.Errorf("not removed:\n%s", out)
			}
		})
	}
}

// A project resolves repo with its own hosts and the built-in prefixes,
// never with [environment.hosts].
func TestProjectHosts(t *testing.T) {
	entry := "[[project.vendor]]\nname = \"a\"\nrepo = \"work:platform/skills\"\nrev = \"" + sha + "\"\n"
	p, err := ParseProject([]byte("[project.hosts.work]\nurl = \"https://git.example.com\"\ntype = \"gitlab\"\n"+entry), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Hosts.Resolve(p.Vendor[0].Repo)
	if err != nil || r.URL != "https://git.example.com/platform/skills.git" {
		t.Errorf("resolve: %v, %v", r.URL, err)
	}
	if p.Hosts["work"].Type != TypeGitLab {
		t.Errorf("host = %+v", p.Hosts["work"])
	}
	_, err = ParseProject([]byte("[environment.hosts.work]\nurl = \"https://git.example.com\"\n"+entry), ".toml")
	if err == nil || !strings.Contains(err.Error(), "[project.hosts.work]") {
		t.Errorf("manifest hosts used by a project: %v", err)
	}
	if _, err := ParseProject([]byte("[[project.from]]\nrepo = \"gitlab:g/sub/r\"\nskills = [\"x\"]\nrev = \""+sha+"\"\n"), ".toml"); err != nil {
		t.Errorf("built-in prefix: %v", err)
	}
	if _, err := ParseProject([]byte("[project.hosts.gitlab]\nurl = \"https://a.example\"\n"), ".toml"); err == nil || !strings.Contains(err.Error(), "project.hosts.gitlab") {
		t.Errorf("built-in alias: %v", err)
	}
}

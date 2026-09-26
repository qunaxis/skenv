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
	if _, err := ParseProject([]byte("[user]\n"), ".toml"); !errors.Is(err, ErrNoProject) {
		t.Errorf("without [project]: %v", err)
	}
}

func TestProjectSkills(t *testing.T) {
	text := `[project]
mirrors = [".claude/skills"]

[project.dependencies.zeta]
repo   = "x/z"
commit = "` + sha + `"

[project.from.mine]
repo   = "me/skills"
skills = ["beta", "alpha"]
commit = "` + sha + `"
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
	if s, ok := p.Skill("beta"); !ok || s.From == nil || s.From.ID != "mine" {
		t.Errorf("Skill(beta) = %+v, %v", s, ok)
	}
}

func TestParseProjectErrors(t *testing.T) {
	d := func(name string) string {
		return "[project.dependencies." + name + "]\nrepo = \"x/a\"\ncommit = \"" + sha + "\"\n"
	}
	from := func(id, lines string) string {
		return "[project.from." + id + "]\n" + lines
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
		{d("a") + from("s", "repo = \"me/s\"\nskills = [\"a\"]\ncommit = \""+sha+"\"\n"), `project skill "a" is defined twice: dependency a (x/a) and from.s (me/s)`},
		{from("s", "repo = \"me/s\"\ncommit = \""+sha+"\"\n"), "skills is required"},
		{from("s", "repo = \"me/s\"\nskills = [\"a\", \"a\"]\ncommit = \""+sha+"\"\n"), `lists "a" twice`},
		{from("s", "repo = \"me/s\"\nskills = [\"a\"]\ncommit = \"main\"\n"), "full 40-character"},
		{from("s", "skills = [\"a\"]\ncommit = \""+sha+"\"\n"), "repo is required"},
		{from("S", "repo = \"me/s\"\nskills = [\"a\"]\ncommit = \""+sha+"\"\n"), "the ID must be lowercase"},
		{d(`"Bad"`), "project.dependencies"},
	} {
		if _, err := ParseProject([]byte(c.text), ".toml"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want %q", c.text, err, c.want)
		}
	}
}

const projectBase = `# my project
[repository]
template_version = "0.4.0"
visibility       = "public"

[project]
dir     = ".agents/skills"
mirrors = [".claude/skills"] # Claude Code

[project.from.mine]
repo   = "me/skills"
skills = ["alpha"]
commit = "` + sha + `" # pinned

# the machine
[user.storage]
dir = "~/.agents/skills"
`

// A new [project.dependencies.<name>] goes after the last table of
// [project], not after an unrelated section.
func TestAppendProjectDependency(t *testing.T) {
	out, err := AppendDependency([]byte(projectBase), ".toml", "project", Dependency{Name: "b", Repo: "x/b", SkillDir: "skills/b", Commit: sha})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(projectBase, "\n# the machine", "\n[project.dependencies.b]\nrepo      = \"x/b\"\nskill_dir = \"skills/b\"\ncommit    = \""+sha+"\"\n\n# the machine", 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	p, err := ParseProject(out, ".toml")
	if err != nil || len(p.Dependencies) != 1 {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Parse(out, ".toml"); err != nil {
		t.Errorf("[user] broken: %v", err)
	}
	next := strings.Repeat("b", 40)
	if out, err = SetDependencyCommit(out, ".toml", "project", "b", next); err != nil {
		t.Fatal(err)
	}
	if out, err = SetFromCommit(out, ".toml", "mine", next); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `commit = "`+next+`" # pinned`) {
		t.Errorf("from commit not set in place:\n%s", out)
	}
	if out, err = RemoveDependency(out, ".toml", "project", "b"); err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(projectBase, sha, next, 1); string(out) != want {
		t.Errorf("after remove:\n%s\nwant:\n%s", out, want)
	}
	if _, err := SetFromCommit(out, ".toml", "other", next); err == nil {
		t.Error("a missing from entry must fail")
	}
	if _, err := RemoveDependency(out, ".toml", "user", "b"); err == nil {
		t.Error("the section must match")
	}
}

func TestEditProjectOtherFormats(t *testing.T) {
	for ext, text := range map[string]string{
		".yaml": "project:\n  mirrors: [.claude/skills]\n  from:\n    mine:\n      repo: me/skills\n      skills: [alpha]\n      commit: \"" + sha + "\"\n",
		".json": `{"project": {"mirrors": [".claude/skills"], "from": {"mine": {"repo": "me/skills", "skills": ["alpha"], "commit": "` + sha + `"}}}}`,
	} {
		t.Run(ext, func(t *testing.T) {
			next := strings.Repeat("b", 40)
			out, err := AppendDependency([]byte(text), ext, "project", Dependency{Name: "b", Repo: "x/b", SkillDir: ".", Commit: sha})
			if err != nil {
				t.Fatal(err)
			}
			if out, err = SetDependencyCommit(out, ext, "project", "b", next); err != nil {
				t.Fatal(err)
			}
			if out, err = SetFromCommit(out, ext, "mine", next); err != nil {
				t.Fatal(err)
			}
			p, err := ParseProject(out, ext)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Dependencies) != 1 || p.Dependencies["b"].Commit != next || p.From["mine"].Commit != next {
				t.Errorf("result:\n%s", out)
			}
			if out, err = RemoveDependency(out, ext, "project", "b"); err != nil {
				t.Fatal(err)
			}
			if p, _ := ParseProject(out, ext); len(p.Dependencies) != 0 {
				t.Errorf("not removed:\n%s", out)
			}
		})
	}
}

// A project resolves repo with its own hosts and the built-in prefixes,
// never with user.git_hosts.
func TestProjectHosts(t *testing.T) {
	entry := "[project.dependencies.a]\nrepo = \"work:platform/skills\"\ncommit = \"" + sha + "\"\n"
	p, err := ParseProject([]byte("[project.git_hosts.work]\nbase_url = \"https://git.example.com\"\nprovider = \"gitlab\"\n"+entry), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.GitHosts.Resolve(p.Dependencies["a"].Repo)
	if err != nil || r.URL != "https://git.example.com/platform/skills.git" {
		t.Errorf("resolve: %v, %v", r.URL, err)
	}
	if p.GitHosts["work"].Provider != TypeGitLab {
		t.Errorf("host = %+v", p.GitHosts["work"])
	}
	_, err = ParseProject([]byte("[user.git_hosts.work]\nbase_url = \"https://git.example.com\"\n"+entry), ".toml")
	if err == nil || !strings.Contains(err.Error(), "[project.git_hosts.work]") {
		t.Errorf("manifest hosts used by a project: %v", err)
	}
	if _, err := ParseProject([]byte("[project.from.x]\nrepo = \"gitlab:g/sub/r\"\nskills = [\"x\"]\ncommit = \""+sha+"\"\n"), ".toml"); err != nil {
		t.Errorf("built-in prefix: %v", err)
	}
	if _, err := ParseProject([]byte("[project.git_hosts.gitlab]\nbase_url = \"https://a.example\"\n"), ".toml"); err == nil || !strings.Contains(err.Error(), "project.git_hosts.gitlab") {
		t.Errorf("built-in alias: %v", err)
	}
}

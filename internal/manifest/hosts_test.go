package manifest

import (
	"path/filepath"
	"strings"
	"testing"
)

var testHosts = Hosts{
	"work":  {URL: "https://git.example.com", Type: TypeGitLab},
	"forge": {URL: "https://forge.example.com/", Type: TypeGitea, SSH: "ssh://git@forge.example.com:2222"},
	"plain": {URL: "https://git.example.org/scm", Type: TypeGeneric},
	"hub":   {URL: "https://github.example.com", Type: TypeGitHub},
}

func TestResolve(t *testing.T) {
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ repo, url, typ string }{
		// github.com shorthand, with and without .git
		{"tt-a1i/archify", "https://github.com/tt-a1i/archify.git", TypeGitHub},
		{"tt-a1i/archify.git", "https://github.com/tt-a1i/archify.git", TypeGitHub},
		{" o/r ", "https://github.com/o/r.git", TypeGitHub},
		// built-in prefixes, subgroups of any depth on gitlab
		{"gitlab:group/repo", "https://gitlab.com/group/repo.git", TypeGitLab},
		{"gitlab:group/sub/deeper/repo.git", "https://gitlab.com/group/sub/deeper/repo.git", TypeGitLab},
		{"codeberg:owner/repo", "https://codeberg.org/owner/repo.git", TypeGitea},
		// declared aliases
		{"work:group/sub/repo", "https://git.example.com/group/sub/repo.git", TypeGitLab},
		{"work:group/repo.git/", "https://git.example.com/group/repo.git", TypeGitLab},
		{"forge:owner/repo", "https://forge.example.com/owner/repo.git", TypeGitea},
		{"hub:owner/repo", "https://github.example.com/owner/repo.git", TypeGitHub},
		{"plain:repo", "https://git.example.org/scm/repo", TypeGeneric},
		{"plain:a/b/repo.git", "https://git.example.org/scm/a/b/repo.git", TypeGeneric},
		// full URLs are kept; the type comes from the host
		{"https://gitlab.com/g/sub/tool.git", "https://gitlab.com/g/sub/tool.git", TypeGitLab},
		{"https://git.example.com/g/r", "https://git.example.com/g/r", TypeGitLab},
		{"ssh://git@forge.example.com:2222/o/r.git", "ssh://git@forge.example.com:2222/o/r.git", TypeGitea},
		{"https://elsewhere.example/o/r.git", "https://elsewhere.example/o/r.git", TypeGeneric},
		{"file:///srv/git/o/r.git", "file:///srv/git/o/r.git", TypeGeneric},
		// scp-like: a dot or "@" before the colon, or an absolute path
		{"git@github.com:o/r.git", "git@github.com:o/r.git", TypeGitHub},
		{"git@git.example.com:g/sub/r.git", "git@git.example.com:g/sub/r.git", TypeGitLab},
		{"host.example:o/r", "host.example:o/r", TypeGeneric},
		{"myserver:/srv/git/r.git", "myserver:/srv/git/r.git", TypeGeneric},
		// local paths
		{"/srv/git/o/r.git", "/srv/git/o/r.git", TypeGeneric},
		{"./skills", filepath.Join(cwd, "skills"), TypeGeneric},
		{"a/b/c", filepath.Join(cwd, "a/b/c"), TypeGeneric},
	}
	for _, c := range cases {
		got, err := testHosts.Resolve(c.repo)
		if err != nil {
			t.Errorf("Resolve(%q): %v", c.repo, err)
			continue
		}
		if got.URL != c.url || got.Type != c.typ {
			t.Errorf("Resolve(%q) = %q, %q; want %q, %q", c.repo, got.URL, got.Type, c.url, c.typ)
		}
	}
}

func TestResolveErrors(t *testing.T) {
	for repo, want := range map[string]string{
		"":                  "repo is empty",
		"gitlab:repo":       "group/repo or group/subgroup",
		"codeberg:a/b/c":    "owner/repo",
		"hub:a/b/c":         "owner/repo",
		"work:g/../r":       "not a repository path",
		"gitlab:g//r":       "not a repository path",
		"myserver:group/r":  `unknown host prefix "myserver:"`,
		"GitLab:group/r":    `unknown host prefix "GitLab:"`,
		"github:owner/repo": `unknown host prefix "github:"`,
	} {
		_, err := testHosts.Resolve(repo)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve(%q) = %v, want an error with %q", repo, err, want)
		}
	}
	// The error lists the known prefixes and how to declare the missing one.
	_, err := testHosts.Resolve("acme:g/r")
	for _, want := range []string{"gitlab:, codeberg:, forge:, hub:, plain:, work:", "[environment.hosts.acme]"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("unknown prefix error %v lacks %q", err, want)
		}
	}
	// Without declared hosts an alias is unknown.
	if _, err := Hosts(nil).Resolve("work:g/r"); err == nil {
		t.Error("undeclared alias resolved")
	}
}

// Every form of one repository has the same identity.
func TestResolveIdentity(t *testing.T) {
	var keys []string
	for _, repo := range []string{
		"work:group/sub/repo",
		"work:group/sub/repo.git",
		"https://git.example.com/group/sub/repo.git",
		"https://GIT.example.com/group/sub/repo/",
		"git@git.example.com:group/sub/repo.git",
		"ssh://git@git.example.com/group/sub/repo",
	} {
		r, err := testHosts.Resolve(repo)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, CacheKey(r.URL))
	}
	for _, k := range keys[1:] {
		if k != keys[0] {
			t.Errorf("cache keys differ: %q", keys)
		}
	}
}

func TestShortForm(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/me/skills.git":             "me/skills",
		"https://user@github.com/me/skills/":           "me/skills",
		"git@GitHub.com:me/sk.ills":                    "me/sk.ills",
		"ssh://git@github.com/me/skills.git":           "me/skills",
		"https://github.com/me/skills/sub.git":         "",
		"https://gitlab.com/group/sub/skills.git":      "gitlab:group/sub/skills",
		"git@gitlab.com:group/skills.git":              "gitlab:group/skills",
		"https://codeberg.org/me/skills":               "codeberg:me/skills",
		"git@codeberg.org:me/skills.git":               "codeberg:me/skills",
		"https://git.example.com/group/sub/skills.git": "work:group/sub/skills",
		"git@git.example.com:group/skills.git":         "work:group/skills",
		"ssh://git@forge.example.com:2222/me/skills":   "forge:me/skills",
		"https://forge.example.com/me/skills.git":      "forge:me/skills",
		"https://git.example.org/scm/a/skills.git":     "plain:a/skills.git",
		"https://git.example.org/other/skills.git":     "",
		"https://elsewhere.example/me/skills.git":      "",
		"file:///tmp/remotes/me/skills.git":            "",
		"/tmp/remotes/me/skills.git":                   "",
		"":                                             "",
	} {
		got, ok := testHosts.ShortForm(remote)
		if got != want || ok != (want != "") {
			t.Errorf("ShortForm(%q) = %q, %v; want %q", remote, got, ok, want)
			continue
		}
		if !ok {
			continue
		}
		// The short form resolves to a URL with the same short form.
		r, err := testHosts.Resolve(got)
		if again, _ := testHosts.ShortForm(r.URL); err != nil || again != got {
			t.Errorf("Resolve(ShortForm(%q)) = %q, %v", remote, r.URL, err)
		}
	}
}

func TestAccessHint(t *testing.T) {
	for repo, want := range map[string]string{
		"work:g/r":                    `url."git@git.example.com:".insteadOf "https://git.example.com/"`,
		"forge:o/r":                   `url."ssh://git@forge.example.com:2222/".insteadOf "https://forge.example.com/"`,
		"plain:r":                     `url."git@git.example.org:".insteadOf "https://git.example.org/scm/"`,
		"o/r":                         `url."git@github.com:".insteadOf "https://github.com/"`,
		"https://elsewhere.example/r": "credential helper",
	} {
		r, err := testHosts.Resolve(repo)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.AccessHint(); !strings.Contains(got, want) {
			t.Errorf("AccessHint of %q = %q, want %q", repo, got, want)
		}
	}
	// No ssh hint for a repository that is already on ssh.
	r, _ := testHosts.Resolve("git@git.example.com:g/r.git")
	if strings.Contains(r.AccessHint(), "insteadOf") {
		t.Errorf("ssh URL got an insteadOf hint: %s", r.AccessHint())
	}
}

func TestParseHosts(t *testing.T) {
	m, err := Parse([]byte(`[environment.hosts.work]
url = "https://git.example.com"
type = "gitlab"

[environment.hosts.plain]
url = "https://git.example.org"

[[environment.own]]
repo = "work:group/sub/skills"
path = "~/src/skills"

[[environment.vendor]]
name = "tool"
repo = "plain:tools.git"
rev = "`+strings.Repeat("a", 40)+`"
`), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Hosts["plain"].Type != TypeGeneric || m.Hosts["work"].Type != TypeGitLab {
		t.Errorf("hosts = %+v", m.Hosts)
	}
	if m.Own[0].Repo != "work:group/sub/skills" {
		t.Errorf("the manifest keeps what was written, got %q", m.Own[0].Repo)
	}

	for body, want := range map[string]string{
		`[environment.hosts.Work]` + "\nurl = \"https://a.example\"\n":                                           "hosts.Work: the alias must be",
		`[environment.hosts.gitlab]` + "\nurl = \"https://a.example\"\n":                                         `"gitlab" is built in`,
		`[environment.hosts.work]` + "\ntype = \"gitlab\"\n":                                                     "hosts.work.url: is required",
		`[environment.hosts.work]` + "\nurl = \"git.example.com\"\n":                                             "must be a base URL",
		`[environment.hosts.work]` + "\nurl = \"https://user:tok@git.example.com\"\n":                            "must not carry credentials",
		`[environment.hosts.work]` + "\nurl = \"oauth2:tok@git.example.com\"\n":                                  "hosts.work.url: must be a base URL",
		`[environment.hosts.work]` + "\nurl = \"https://a.example\"\ntype = \"bitbucket\"":                       `hosts.work.type: "bitbucket" must be one of github, gitlab, gitea, generic`,
		`[environment.hosts.work]` + "\nurl = \"https://a.example\"\nssh = \"https://a\"":                        "must be user@host",
		"[[environment.own]]\nrepo = \"acme:g/r\"\npath = \"~/x\"\n":                                             `own[0]: repo "acme:g/r": unknown host prefix`,
		"[[environment.vendor]]\nname = \"t\"\nrepo = \"gitlab:r\"\nrev = \"" + strings.Repeat("a", 40) + "\"\n": `vendor "t": repo "gitlab:r"`,
	} {
		_, err := Parse([]byte(body), ".toml")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want an error with %q", body, err, want)
		}
	}
}

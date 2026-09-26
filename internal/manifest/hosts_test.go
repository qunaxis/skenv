package manifest

import (
	"path/filepath"
	"strings"
	"testing"
)

var testHosts = Hosts{
	"work": {BaseURL: "https://git.example.com", Provider: TypeGitLab},
	// base_url selects the transport: this host is cloned over ssh.
	"forge": {BaseURL: "ssh://git@forge.example.com:2222", Provider: TypeGitea},
	"plain": {BaseURL: "https://git.example.org/scm", Provider: TypeGeneric},
	"hub":   {BaseURL: "https://github.example.com", Provider: TypeGitHub},
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
		// github: names the same host explicitly
		{"github:tt-a1i/archify", "https://github.com/tt-a1i/archify.git", TypeGitHub},
		// built-in prefixes, subgroups of any depth on gitlab
		{"gitlab:group/repo", "https://gitlab.com/group/repo.git", TypeGitLab},
		{"gitlab:group/sub/deeper/repo.git", "https://gitlab.com/group/sub/deeper/repo.git", TypeGitLab},
		{"codeberg:owner/repo", "https://codeberg.org/owner/repo.git", TypeGitea},
		// declared aliases
		{"work:group/sub/repo", "https://git.example.com/group/sub/repo.git", TypeGitLab},
		{"work:group/repo.git/", "https://git.example.com/group/repo.git", TypeGitLab},
		{"forge:owner/repo", "ssh://git@forge.example.com:2222/owner/repo.git", TypeGitea},
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
		"":                 "repo is empty",
		"gitlab:repo":      "group/repo or group/subgroup",
		"codeberg:a/b/c":   "owner/repo",
		"hub:a/b/c":        "owner/repo",
		"work:g/../r":      "not a repository path",
		"gitlab:g//r":      "not a repository path",
		"myserver:group/r": `unknown host prefix "myserver:"`,
		"GitLab:group/r":   `unknown host prefix "GitLab:"`,
		"github:a/b/c":     "owner/repo",
	} {
		_, err := testHosts.Resolve(repo)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve(%q) = %v, want an error with %q", repo, err, want)
		}
	}
	// The error lists the known prefixes and how to declare the missing one.
	_, err := testHosts.Resolve("acme:g/r")
	for _, want := range []string{"github:, gitlab:, codeberg:, forge:, hub:, plain:, work:", "[user.git_hosts.acme]"} {
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
	// No ssh hint for a repository that is already on ssh, or on a host
	// whose base_url is ssh.
	for _, repo := range []string{"git@git.example.com:g/r.git", "forge:o/r"} {
		r, _ := testHosts.Resolve(repo)
		if strings.Contains(r.AccessHint(), "insteadOf") {
			t.Errorf("%s got an insteadOf hint: %s", repo, r.AccessHint())
		}
	}
}

func TestParseHosts(t *testing.T) {
	m, err := Parse([]byte(`[user.git_hosts.work]
base_url = "https://git.example.com"
provider = "gitlab"

[user.git_hosts.plain]
base_url = "https://git.example.org"

[user.git_hosts.ssh]
base_url = "ssh://git@git.example.net:2222/scm"
provider = "gitlab"

[user.checkouts.skills]
repo = "work:group/sub/skills"
checkout_dir = "~/src/skills"

[user.dependencies.tool]
repo = "plain:tools.git"
commit = "`+strings.Repeat("a", 40)+`"
`), ".toml")
	if err != nil {
		t.Fatal(err)
	}
	if m.GitHosts["plain"].Provider != TypeGeneric || m.GitHosts["work"].Provider != TypeGitLab {
		t.Errorf("hosts = %+v", m.GitHosts)
	}
	if m.Checkouts["skills"].Repo != "work:group/sub/skills" {
		t.Errorf("the manifest keeps what was written, got %q", m.Checkouts["skills"].Repo)
	}
	if r, err := m.Remote("ssh:g/r"); err != nil || r.URL != "ssh://git@git.example.net:2222/scm/g/r.git" {
		t.Errorf("ssh base_url: %+v, %v", r, err)
	}
	if got, ok := m.GitHosts.ShortForm("https://git.example.net/scm/g/r.git"); !ok || got != "ssh:g/r" {
		t.Errorf("https form of an ssh host: %q, %v", got, ok)
	}

	for body, want := range map[string]string{
		`[user.git_hosts.Work]` + "\nbase_url = \"https://a.example\"\n":                             "git_hosts.Work: the alias must be",
		`[user.git_hosts.gitlab]` + "\nbase_url = \"https://a.example\"\n":                           `"gitlab" is built in`,
		`[user.git_hosts.github]` + "\nbase_url = \"https://a.example\"\n":                           `"github" is built in`,
		`[user.git_hosts.work]` + "\nprovider = \"gitlab\"\n":                                        "git_hosts.work.base_url: is required",
		`[user.git_hosts.work]` + "\nbase_url = \"git.example.com\"\n":                               "must be a base URL",
		`[user.git_hosts.work]` + "\nbase_url = \"https://user:tok@git.example.com\"\n":              "must not carry credentials",
		`[user.git_hosts.work]` + "\nbase_url = \"ssh://git:pw@git.example.com\"\n":                  "must not carry a password",
		`[user.git_hosts.work]` + "\nbase_url = \"ftp://git.example.com\"\n":                         `scheme "ftp": use https:// or ssh://`,
		`[user.git_hosts.work]` + "\nbase_url = \"oauth2:tok@git.example.com\"\n":                    "git_hosts.work.base_url: must be a base URL",
		`[user.git_hosts.work]` + "\nbase_url = \"https://a.example\"\nprovider = \"bitbucket\"":     `git_hosts.work.provider: "bitbucket" must be one of github, gitlab, gitea, generic`,
		"[user.checkouts.x]\nrepo = \"acme:g/r\"\ncheckout_dir = \"~/x\"\n":                          `user.checkouts.x: repo "acme:g/r": unknown host prefix`,
		"[user.dependencies.t]\nrepo = \"gitlab:r\"\ncommit = \"" + strings.Repeat("a", 40) + "\"\n": `user.dependencies.t: repo "gitlab:r"`,
	} {
		_, err := Parse([]byte(body), ".toml")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want an error with %q", body, err, want)
		}
	}
}

// A relative local repo resolves against the directory of the skenv file,
// not the working directory.
func TestResolveIn(t *testing.T) {
	r, err := Hosts(nil).ResolveIn("/m/dir", "../remotes/r.git")
	if err != nil || r.URL != "/m/remotes/r.git" {
		t.Errorf("ResolveIn = %+v, %v", r, err)
	}
	m, err := ParseIn([]byte("[user.dependencies.x]\nrepo = \"./r\"\ncommit = \""+strings.Repeat("a", 40)+"\"\n"), ".toml", "/m/dir")
	if err != nil {
		t.Fatal(err)
	}
	if r, err := m.Remote(m.Dependencies["x"].Repo); err != nil || r.URL != "/m/dir/r" {
		t.Errorf("Remote = %+v, %v", r, err)
	}
}

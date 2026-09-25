package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// selfHosted is the manifest of TestDeclaredHost: a GitLab instance
// declared as "work", an own repository and a vendored skill in a subgroup
// on it.
func selfHosted(rev string) string {
	return `[environment.hosts.work]
url  = "https://git.example.com"
type = "gitlab"

[[environment.own]]
repo = "work:team/skills"
path = "~/` + ownPath + `"

[[environment.vendor]]
name = "tool"
repo = "work:team/sub/tools"
path = "tool"
rev  = "` + rev + `"
`
}

// machine is what TestDeclaredHost compares between two machines.
type machine struct {
	marker, vendored string
	cache            []string
	links            map[string]string
}

func (w *world) machine() machine {
	w.t.Helper()
	m := machine{
		marker:   readFile(w.t, w.path(".agents/skills/tool/.skenv")),
		vendored: w.vendored("tool"),
		cache:    w.cacheDirs(),
		links:    map[string]string{},
	}
	for _, s := range []string{"alpha", "tool"} {
		dest, _ := os.Readlink(w.path(".claude/skills/" + s))
		m.links[s] = strings.TrimPrefix(dest, w.home)
	}
	return m
}

// secondMachine is a fresh temporary $HOME next to w's, with the same git
// config (the same url.insteadOf) and the same remotes.
func (w *world) secondMachine() *world {
	w.t.Helper()
	gitconfig := readFile(w.t, filepath.Join(w.home, ".gitconfig"))
	w2 := *w
	w2.home = filepath.Join(filepath.Dir(w.home), "home2")
	for _, d := range []string{w2.home, filepath.Join(w2.home, ".pi", "agent"), filepath.Join(w2.home, ".claude", "skills", "synced")} {
		mustMkdir(w.t, d)
	}
	w.t.Setenv("HOME", w2.home)
	w.t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(w2.home, ".gitconfig"))
	writeFile(w.t, filepath.Join(w2.home, ".gitconfig"), gitconfig)
	return &w2
}

// #29: a self-hosted host declared in the manifest serves own and vendor
// entries (a local bare repository behind url.insteadOf), the canonical
// URL keys the cache and fills the .skenv marker, and a second machine with
// the same manifest resolves everything identically.
func TestDeclaredHost(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://git.example.com/", "selfhosted")
	rev := w.pushTool("selfhosted/team/sub/tools", "from-subgroup")
	w.push("selfhosted/team/skills", map[string]string{
		"skenv.toml":            selfHosted(rev),
		"skills/alpha/SKILL.md": skillMD("alpha", ""),
	}, "feat: initial")

	// The manifest is not cloned yet, so clone takes the full URL.
	if _, errOut := w.mustRun(2, "clone", "work:team/skills"); !strings.Contains(errOut, `unknown host prefix "work:"`) ||
		!strings.Contains(errOut, "pass the full URL") {
		t.Errorf("clone with an alias:\n%s", errOut)
	}
	w.cloneSync("https://git.example.com/team/skills.git", "~/"+ownPath)
	first := w.machine()
	if !strings.Contains(first.vendored, "from-subgroup") {
		t.Errorf("vendored from the wrong repository:\n%s", first.vendored)
	}
	if !strings.Contains(first.marker, `repo = "https://git.example.com/team/sub/tools.git"`) {
		t.Errorf("marker does not record the canonical URL:\n%s", first.marker)
	}
	if first.links["alpha"] == "" || first.links["tool"] == "" {
		t.Errorf("links = %v", first.links)
	}
	// The cache is keyed by the URL git fetches from, after url.insteadOf
	// (here the local bare repository).
	if !slices.ContainsFunc(first.cache, func(d string) bool { return strings.Contains(d, "selfhosted-team-sub-tools-") }) {
		t.Errorf("cache = %v", first.cache)
	}
	w.mustRun(0, "doctor")

	// The CLI takes the alias too; the manifest keeps what was written.
	w.pushTool("selfhosted/team/other", "other")
	w.mustRun(0, "vendor", "add", "work:team/other", "--path", "tool", "--name", "other")
	manifest := readFile(t, w.path(ownPath+"/skenv.toml"))
	if !strings.Contains(manifest, `repo = "work:team/other"`) {
		t.Errorf("manifest after vendor add:\n%s", manifest)
	}
	if !strings.Contains(w.vendored("other"), "other") {
		t.Error("other not vendored")
	}
	w.git(w.path(ownPath), "checkout", "--quiet", "--", "skenv.toml")
	w.mustRun(0, "sync", "--quiet")

	// A second machine: a fresh $HOME, the same manifest.
	w2 := w.secondMachine()
	w2.cloneSync("https://git.example.com/team/skills.git", "~/"+ownPath)
	second := w2.machine()
	if first.marker != second.marker || first.vendored != second.vendored || !slices.Equal(first.cache, second.cache) {
		t.Errorf("machines differ:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	for s, dest := range first.links {
		if second.links[s] != dest {
			t.Errorf("link %s: %q on the first machine, %q on the second", s, dest, second.links[s])
		}
	}
	w2.mustRun(0, "doctor")
}

// An unknown prefix is an error, never a fallback to GitHub.
func TestUnknownHostPrefix(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	before := readFile(t, w.path(ownPath+"/skenv.toml"))
	_, errOut := w.mustRun(2, "vendor", "add", "acme:team/tools")
	if !strings.Contains(errOut, `unknown host prefix "acme:"`) || !strings.Contains(errOut, "[environment.hosts.acme]") {
		t.Errorf("vendor add:\n%s", errOut)
	}
	if readFile(t, w.path(ownPath+"/skenv.toml")) != before {
		t.Error("the manifest changed")
	}

	writeFile(t, w.path(ownPath+"/skenv.toml"), before+"\n[[environment.own]]\nrepo = \"acme:team/more\"\npath = \"~/src/more\"\n")
	if _, errOut := w.mustRun(2, "sync"); !strings.Contains(errOut, `own[1]: repo "acme:team/more": unknown host prefix`) {
		t.Errorf("sync:\n%s", errOut)
	}
}

// `skenv init` without a repository writes the short form of an origin on
// GitLab (subgroups included) or Codeberg, and the URL of an origin on
// another host, credentials removed.
func TestInitStartsManifestOnOtherHosts(t *testing.T) {
	for origin, want := range map[string]string{
		"https://gitlab.com/example-org/team/skills.git":      `repo = "gitlab:example-org/team/skills"`,
		"git@gitlab.com:example-org/skills.git":               `repo = "gitlab:example-org/skills"`,
		"https://codeberg.org/example-org/skills.git":         `repo = "codeberg:example-org/skills"`,
		"git@git.example.com:team/sub/skills.git":             `repo = "git@git.example.com:team/sub/skills.git"`,
		"https://user:secret@git.example.com/team/skills.git": `repo = "https://git.example.com/team/skills.git"`,
	} {
		t.Run(origin, func(t *testing.T) {
			w := newWorld(t)
			repo := w.path("src/skills")
			mustMkdir(t, repo)
			w.git(repo, "init", "--quiet")
			w.git(repo, "remote", "add", "origin", origin)
			out, _ := w.mustRun(0, "init", "--dir", repo)
			if !strings.Contains(out, "as its first own repository") {
				t.Errorf("init:\n%s", out)
			}
			text := readFile(t, filepath.Join(repo, "skenv.toml"))
			if !strings.Contains(text, want+"\npath = \"~/src/skills\"\n") || strings.Contains(text+out, "secret") {
				t.Errorf("skenv.toml:\n%s", text)
			}
			// The hint for the next machine names the same repository.
			repoValue := strings.Trim(strings.TrimPrefix(want, "repo = "), `"`)
			if !strings.Contains(out, "on another machine: skenv clone "+repoValue+"\n") {
				t.Errorf("init hint:\n%s", out)
			}
		})
	}

	// An origin whose credentials cannot be removed is left out.
	w := newWorld(t)
	repo := w.path("src/skills")
	mustMkdir(t, repo)
	w.git(repo, "init", "--quiet")
	w.git(repo, "remote", "add", "origin", "https://user:p%zz@git.example.com/team/skills.git")
	out, _ := w.mustRun(0, "init", "--dir", repo)
	if text := readFile(t, filepath.Join(repo, "skenv.toml")); strings.Contains(text+out, "p%zz") || strings.Contains(out, "first own repository") {
		t.Errorf("unparsable origin written:\n%s\n%s", out, text)
	}
}

// The .skenv marker records the canonical URL: another spelling of the
// same repository keeps the vendored copy, a mirror (another url of the
// host) copies it again.
func TestMarkerIdentity(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://git.example.com/", "selfhosted")
	rev := w.pushTool("selfhosted/team/sub/tools", "from-subgroup")
	w.push("selfhosted/team/skills", map[string]string{
		"skenv.toml":            selfHosted(rev),
		"skills/alpha/SKILL.md": skillMD("alpha", ""),
	}, "feat: initial")
	w.cloneSync("https://git.example.com/team/skills.git", "~/"+ownPath)
	manifest := w.path(ownPath + "/skenv.toml")
	text := readFile(t, manifest)

	// One directory behind several prefixes: --add keeps the earlier ones.
	alsoMap := func(prefix string) {
		w.git(w.home, "config", "--global", "--add", "url.file://"+filepath.Join(w.remotes, "selfhosted")+"/.insteadOf", prefix)
	}
	alsoMap("git@git.example.com:")
	writeFile(t, manifest, strings.Replace(text, `repo = "work:team/sub/tools"`, `repo = "git@git.example.com:team/sub/tools.git"`, 1))
	if out, _ := w.mustRun(0, "sync"); strings.Contains(out, "vendor tool from") {
		t.Errorf("another spelling copied the skill again:\n%s", out)
	}

	alsoMap("https://mirror.example.com/")
	writeFile(t, manifest, strings.Replace(text, `url  = "https://git.example.com"`, `url  = "https://mirror.example.com"`, 1))
	out, _ := w.mustRun(0, "sync")
	if !strings.Contains(out, "vendor tool from work:team/sub/tools") {
		t.Errorf("a mirror did not copy the skill again:\n%s", out)
	}
	if mk := readFile(t, w.path(".agents/skills/tool/.skenv")); !strings.Contains(mk, `repo = "https://mirror.example.com/team/sub/tools.git"`) {
		t.Errorf("marker:\n%s", mk)
	}
}

// `skenv init --remote` names the future remote of a repository without an
// origin: written like an origin would be. With an origin, with <repo>, or
// for a local path it is an error.
func TestInitRemote(t *testing.T) {
	for remote, want := range map[string]string{
		"example-org/skills":                                  `repo = "example-org/skills"`,
		"gitlab:example-group/sub/skills":                     `repo = "gitlab:example-group/sub/skills"`,
		"https://gitlab.com/example-group/skills.git":         `repo = "gitlab:example-group/skills"`,
		"codeberg:example-org/skills":                         `repo = "codeberg:example-org/skills"`,
		"https://user:secret@git.example.com/team/skills.git": `repo = "https://git.example.com/team/skills.git"`,
	} {
		t.Run(remote, func(t *testing.T) {
			w := newWorld(t)
			repo := w.path("src/skills")
			mustMkdir(t, repo)
			w.git(repo, "init", "--quiet")
			out, _ := w.mustRun(0, "init", "--dir", repo, "--remote", remote)
			text := readFile(t, filepath.Join(repo, "skenv.toml"))
			if !strings.Contains(text, want+"\npath = \"~/src/skills\"\n") || strings.Contains(text+out, "secret") {
				t.Errorf("skenv.toml:\n%s\n%s", text, out)
			}
		})
	}
	w := newWorld(t)
	repo := w.path("src/skills")
	mustMkdir(t, repo)
	w.git(repo, "init", "--quiet")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--remote", "./elsewhere"}, "not a local path"},
		{[]string{"--remote", "work:team/skills"}, `unknown host prefix "work:"`},
		{[]string{"example-org/skills", "--remote", "example-org/skills"}, "init: takes no <repo>"},
	} {
		if _, errOut := w.mustRun(2, append([]string{"init", "--dir", repo}, c.args...)...); !strings.Contains(errOut, c.want) {
			t.Errorf("%v: %s", c.args, errOut)
		}
	}
	w.git(repo, "remote", "add", "origin", "https://github.com/example-org/skills.git")
	if _, errOut := w.mustRun(2, "init", "--dir", repo, "--remote", "gitlab:example-group/skills"); !strings.Contains(errOut, "--remote is for a repository without an origin remote") {
		t.Errorf("with an origin: %s", errOut)
	}
	if fileExists(filepath.Join(repo, "skenv.toml")) {
		t.Error("a refused init wrote skenv.toml")
	}
}

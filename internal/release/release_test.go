package release

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func need(t *testing.T, tool string) {
	t.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is required in CI", tool)
		}
		t.Skipf("%s not installed", tool)
	}
}

type repo struct {
	t   *testing.T
	dir string
	env []string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	need(t, "git")
	dir := t.TempDir()
	r := &repo{t: t, dir: dir, env: append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".gitconfig-none"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")}
	r.run("git", "init", "--quiet", "-b", "main")
	return r
}

func (r *repo) run(name string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		r.t.Fatalf("%s %s: %v\n%s%s", name, strings.Join(args, " "), err, out, stderr)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) commit(msgs ...string) {
	r.t.Helper()
	for _, m := range msgs {
		r.run("git", "commit", "--quiet", "--allow-empty", "-m", m)
	}
}

func (r *repo) cliff(args ...string) string {
	r.t.Helper()
	return r.run("git", append([]string{"cliff", "--config", filepath.Join(root(r.t), "cliff.toml")}, args...)...)
}

// V3: next version computed from the commits since v0.1.0.
func TestBumpedVersion(t *testing.T) {
	need(t, "git-cliff")
	cases := []struct {
		name    string
		commits []string
		want    string // "" = no release
	}{
		{"fix", []string{"fix(sync): handle dirty tree"}, "v0.1.1"},
		{"perf", []string{"perf(doctor): cache fetch"}, "v0.1.1"},
		{"feat", []string{"fix: a", "feat(vendor): add bump"}, "v0.2.0"},
		{"feat!", []string{"feat(manifest)!: rename store"}, "v0.2.0"},
		{"breaking footer", []string{"fix: change\n\nBREAKING CHANGE: state format"}, "v0.2.0"},
		{"docs only", []string{"docs: readme", "docs(link): typo"}, ""},
		{"other types only", []string{"refactor: x", "test: y", "ci: z", "chore: w", "build: v", "revert: u"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			r.commit("feat: initial")
			r.run("git", "tag", "-a", "v0.1.0", "-m", "v0.1.0")
			r.commit(c.commits...)
			got := r.cliff("--bumped-version")
			want := c.want
			if want == "" {
				want = "v0.1.0" // unchanged: scripts/release.sh stops, nothing to release
			}
			if got != want {
				t.Errorf("bumped version = %s, want %s", got, want)
			}
		})
	}
}

func TestFirstReleaseIsV010(t *testing.T) {
	need(t, "git-cliff")
	r := newRepo(t)
	r.commit("feat(sync): first", "fix: second")
	if got := r.cliff("--bumped-version"); got != "v0.1.0" {
		t.Errorf("first version = %s", got)
	}
}

// V5: groups, scope and short SHA; hidden types are not listed.
func TestChangelogGroups(t *testing.T) {
	need(t, "git-cliff")
	r := newRepo(t)
	r.commit("feat(sync): add sync", "fix(doctor): exit code", "perf: faster", "feat(link)!: relative links",
		"docs: readme", "ci: workflow", "chore(release): v0.1.0")
	short := r.run("git", "rev-parse", "--short=7", "HEAD~6")
	notes := r.cliff("--tag", "v0.1.0", "--strip", "header")
	for _, want := range []string{"## v0.1.0", "### Breaking changes", "**link:** relative links", "### Features",
		"**sync:** add sync (" + short + ")", "### Bug fixes", "**doctor:** exit code", "### Performance"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	for _, hidden := range []string{"readme", "workflow", "chore(release)", "Other"} {
		if strings.Contains(notes, hidden) {
			t.Errorf("notes must not contain %q:\n%s", hidden, notes)
		}
	}
	if strings.Index(notes, "Breaking changes") > strings.Index(notes, "Features") {
		t.Error("breaking changes must come first")
	}
}

// V1-V2: the commit-msg hook.
func TestCommitMessageCheck(t *testing.T) {
	need(t, "bash")
	script := filepath.Join(root(t), "scripts", "check-commit-msg.sh")
	good := []string{
		"feat(sync): add --adopt",
		"fix: handle empty manifest",
		"feat(vendor)!: require full SHA",
		"docs(readme): install command\n\nExplain why.\n\nCo-Authored-By: Someone <a@b.c>",
		"chore(release): v0.1.0",
		"# comment from the editor\nci: check commits",
	}
	bad := []string{
		"",
		"Add feature",
		"feature: x",
		"feat:missing space",
		"feat(): empty scope",
		"Feat: capital type",
		"fix: subject\nno blank line",
		"feat(sync) : space",
		"Merge branch 'main'",
		"feat: " + strings.Repeat("x", 100),
	}
	check := func(msg string) error {
		cmd := exec.Command("bash", script, "-")
		cmd.Stdin = strings.NewReader(msg)
		return cmd.Run()
	}
	for _, m := range good {
		if err := check(m); err != nil {
			t.Errorf("rejected valid message %q: %v", m, err)
		}
	}
	for _, m := range bad {
		if err := check(m); err == nil {
			t.Errorf("accepted invalid message %q", m)
		}
	}
}

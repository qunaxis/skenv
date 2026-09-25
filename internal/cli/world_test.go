package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// world is a temporary $HOME plus local bare repositories that stand in for
// github.com: git's url.insteadOf maps https://github.com/ to them.
type world struct {
	t       *testing.T
	home    string
	remotes string
	work    string // scratch clones used to push to the remotes
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	w := &world{t: t, home: filepath.Join(root, "home"), remotes: filepath.Join(root, "remotes"), work: filepath.Join(root, "work")}
	for _, d := range []string{w.home, w.remotes, w.work,
		filepath.Join(w.home, ".claude", "skills", "synced"), // must never be touched
		filepath.Join(w.home, ".pi", "agent"),
	} {
		mustMkdir(t, d)
	}
	t.Setenv("HOME", w.home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("SKENV_MANIFEST", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(w.home, ".gitconfig"))
	writeFile(t, filepath.Join(w.home, ".gitconfig"), `[user]
	name = skenv test
	email = test@example.invalid
[init]
	defaultBranch = main
[url "file://`+w.remotes+`/"]
	insteadOf = https://github.com/
[protocol "file"]
	allow = always
`)
	return w
}

func mustMkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (w *world) git(dir string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// push commits files (path → content, "" deletes) to remote owner/repo,
// creating the bare repository on first use, and returns the new HEAD.
func (w *world) push(repo string, files map[string]string, msg string) string {
	w.t.Helper()
	bare := filepath.Join(w.remotes, repo+".git")
	clone := filepath.Join(w.work, strings.ReplaceAll(repo, "/", "__"))
	if _, err := os.Stat(bare); err != nil {
		mustMkdir(w.t, bare)
		w.git(bare, "init", "--quiet", "--bare", "-b", "main")
		w.git(w.work, "clone", "--quiet", bare, clone)
	} else {
		w.git(clone, "pull", "--quiet", "--ff-only", "origin", "main")
	}
	for p, content := range files {
		full := filepath.Join(clone, p)
		if content == "" {
			if err := os.RemoveAll(full); err != nil {
				w.t.Fatal(err)
			}
			continue
		}
		writeFile(w.t, full, content)
		if strings.HasPrefix(content, "#!") {
			if err := os.Chmod(full, 0o755); err != nil {
				w.t.Fatal(err)
			}
		}
	}
	w.git(clone, "add", "-A")
	w.git(clone, "commit", "--quiet", "--allow-empty", "-m", msg)
	w.git(clone, "push", "--quiet", "origin", "HEAD:main")
	return w.git(clone, "rev-parse", "HEAD")
}

func skillMD(name, extra string) string {
	return "---\nname: " + name + "\ndescription: test skill " + name + "\n---\n" + extra + "\n"
}

// run executes skenv in-process.
func (w *world) run(args ...string) (int, string, string) {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (w *world) mustRun(want int, args ...string) (string, string) {
	w.t.Helper()
	code, out, errOut := w.run(args...)
	if code != want {
		w.t.Fatalf("skenv %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, want, out, errOut)
	}
	return out, errOut
}

func (w *world) path(p string) string { return filepath.Join(w.home, p) }

func (w *world) readlink(p string) string {
	w.t.Helper()
	dest, err := os.Readlink(w.path(p))
	if err != nil {
		w.t.Fatalf("readlink %s: %v", p, err)
	}
	return dest
}

func (w *world) exists(p string) bool {
	_, err := os.Lstat(w.path(p))
	return err == nil
}

const ownPath = "src/skills"

// standard sets up the usual remotes: an own repository me/skills
// with skills alpha and beta and the manifest, and a vendor repository
// ext/tools with skills archify and other. It returns the vendor HEAD.
func (w *world) standard(extraManifest string) string {
	w.t.Helper()
	rev := w.push("ext/tools", map[string]string{
		"README.md":                 "tools\n",
		"tools/archify/SKILL.md":    skillMD("archify", "v1"),
		"tools/archify/scripts/run": "#!/bin/sh\necho run\n",
		"tools/other/SKILL.md":      skillMD("other", "v1"),
	}, "feat: initial")
	w.push("me/skills", map[string]string{
		"skenv.toml":            manifestText(rev, extraManifest),
		"skills/alpha/SKILL.md": skillMD("alpha", ""),
		"skills/beta/SKILL.md":  skillMD("beta", ""),
		"README.md":             "not a skill\n",
	}, "feat: initial")
	return rev
}

func manifestText(rev, extra string) string {
	return `# test manifest
[[environment.own]]
repo = "me/skills"
path = "~/` + ownPath + `"

# pinned third-party skill
[[environment.vendor]]
name = "archify"
repo = "ext/tools"
path = "tools/archify"
rev  = "` + rev + `" # keep this comment
` + extra
}

// initStandard runs `skenv init` on the standard remotes.
func (w *world) initStandard(extraManifest string) string {
	w.t.Helper()
	rev := w.standard(extraManifest)
	w.mustRun(0, "init", "me/skills", "--path", "~/"+ownPath)
	return rev
}

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// golden compares got with testdata/<name>.golden after replacing each key
// of subst (commit SHAs, temporary paths) with its value.
func golden(t *testing.T, name, got string, subst map[string]string) {
	t.Helper()
	for from, to := range subst {
		got = strings.ReplaceAll(got, from, to)
	}
	p := filepath.Join("testdata", name+".golden")
	if *update {
		writeFile(t, p, got)
		return
	}
	if want := readFile(t, p); got != want {
		t.Errorf("%s differs from %s (rerun with -update if intended):\n--- got\n%s\n--- want\n%s", name, p, got, want)
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mapHost makes git fetch https://<host>/... from the local remotes under
// <remotes>/<dir>/, so several "hosts" can serve repositories with the same
// owner/name.
func (w *world) mapHost(prefix, dir string) {
	w.t.Helper()
	w.git(w.home, "config", "--global", "url.file://"+filepath.Join(w.remotes, dir)+"/.insteadOf", prefix)
}

func vendorEntry(name, repo, rev string) string {
	return `
[user.dependencies.` + name + `]
repo      = "` + repo + `"
skill_dir = "tool"
commit    = "` + rev + `"
`
}

// pushTool publishes a repository with one skill in tool/ whose body is
// marker.
func (w *world) pushTool(repo, marker string) string {
	w.t.Helper()
	return w.push(repo, map[string]string{"tool/SKILL.md": skillMD("tool", marker)}, "feat: "+marker)
}

func (w *world) vendored(name string) string {
	w.t.Helper()
	return readFile(w.t, w.path(".agents/skills/"+name+"/SKILL.md"))
}

func (w *world) cacheDirs() []string {
	w.t.Helper()
	entries, err := os.ReadDir(w.path(".cache/skenv/repos"))
	if err != nil {
		w.t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// #28: repositories with the same owner/name on different hosts, and a
// GitLab subgroup path that ends like a shorter path, each get their own
// vendor cache.
func TestVendorCacheKeyedByHostAndFullPath(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://host-a.test/", "host-a")
	w.mapHost("https://host-b.test/", "host-b")
	w.mapHost("https://gitlab.test/", "gitlab")
	revA := w.pushTool("host-a/x/skills", "from-host-a")
	revB := w.pushTool("host-b/x/skills", "from-host-b")
	revG := w.pushTool("gitlab/grp/x/skills", "from-gitlab-subgroup")
	revS := w.pushTool("gitlab/x/skills", "from-gitlab-short")
	revH := w.pushTool("x/skills", "from-github") // https://github.com/x/skills
	w.initStandard(vendorEntry("tool-a", "https://host-a.test/x/skills.git", revA) +
		vendorEntry("tool-b", "https://host-b.test/x/skills.git", revB) +
		vendorEntry("tool-g", "https://gitlab.test/grp/x/skills.git", revG) +
		vendorEntry("tool-s", "https://gitlab.test/x/skills.git", revS) +
		vendorEntry("tool-h", "x/skills", revH))
	for name, want := range map[string]string{
		"tool-a": "from-host-a", "tool-b": "from-host-b",
		"tool-g": "from-gitlab-subgroup", "tool-s": "from-gitlab-short",
		"tool-h": "from-github",
	} {
		if got := w.vendored(name); !strings.Contains(got, want) {
			t.Errorf("%s vendored from the wrong repository:\n%s", name, got)
		}
	}
	// ext/tools of the standard manifest plus the five above.
	if dirs := w.cacheDirs(); len(dirs) != 6 {
		t.Errorf("cache dirs = %v, want 6", dirs)
	}
	w.mustRun(0, "doctor")
}

// #28: pointing an entry at another repository with the same owner/name
// vendors from the new one, and a cache whose origin was changed behind
// skenv's back is cloned again instead of being fetched from.
func TestVendorCacheFollowsRepoChange(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://host-a.test/", "host-a")
	w.mapHost("https://host-b.test/", "host-b")
	revA := w.pushTool("host-a/x/skills", "from-host-a")
	revB := w.pushTool("host-b/x/skills", "from-host-b")
	rev := w.initStandard(vendorEntry("tool", "https://host-a.test/x/skills", revA))
	if got := w.vendored("tool"); !strings.Contains(got, "from-host-a") {
		t.Fatalf("tool:\n%s", got)
	}

	w.push("me/skills", map[string]string{"skenv.toml": manifestText(rev, vendorEntry("tool", "https://host-b.test/x/skills", revB))}, "chore: mirror")
	w.mustRun(0, "sync")
	if got := w.vendored("tool"); !strings.Contains(got, "from-host-b") {
		t.Fatalf("tool after the repo change:\n%s", got)
	}

	// Point every cache at host-a behind skenv's back: the host-b cache
	// must be cloned again, not fetched from host-a.
	for _, d := range w.cacheDirs() {
		w.git(w.path(".cache/skenv/repos/"+d), "remote", "set-url", "origin", "https://host-a.test/x/skills")
	}
	revB2 := w.pushTool("host-b/x/skills", "from-host-b-v2")
	w.push("me/skills", map[string]string{"skenv.toml": manifestText(rev, vendorEntry("tool", "https://host-b.test/x/skills", revB2))}, "chore: bump")
	out, _ := w.mustRun(0, "sync")
	if !strings.Contains(out, "cloning again") {
		t.Errorf("no re-clone reported:\n%s", out)
	}
	if got := w.vendored("tool"); !strings.Contains(got, "from-host-b-v2") {
		t.Errorf("tool after the origin change:\n%s", got)
	}
	for _, d := range w.cacheDirs() {
		if strings.HasPrefix(d, ".") {
			t.Errorf("temporary directory left in the cache: %s", d)
		}
	}
}

// #28, N5: credentials in a vendor URL never reach the output or the cache
// directory name, not even in the re-clone notice or a missing-commit error.
func TestVendorCacheHidesCredentials(t *testing.T) {
	const secret = "s3cret-token"
	w := newWorld(t)
	w.mapHost("https://ci:"+secret+"@host-a.test/", "host-a")
	w.mapHost("https://host-b.test/", "host-b")
	repo := "https://ci:" + secret + "@host-a.test/x/skills"
	revA := w.pushTool("host-a/x/skills", "from-host-a")
	w.pushTool("host-b/x/skills", "from-host-b")
	rev := w.standard(vendorEntry("tool", repo, revA))
	out, errOut := w.cloneSync("me/skills", "~/"+ownPath)
	all := out + errOut
	setRev := func(r string) {
		w.push("me/skills", map[string]string{"skenv.toml": manifestText(rev, vendorEntry("tool", repo, r))}, "chore: pin")
	}

	for _, d := range w.cacheDirs() {
		if strings.Contains(d, secret) {
			t.Errorf("credentials in cache dir name %q", d)
		}
		w.git(w.path(".cache/skenv/repos/"+d), "remote", "set-url", "origin", "https://ci:"+secret+"@host-b.test/x/skills")
	}
	setRev(w.pushTool("host-a/x/skills", "from-host-a-v2"))
	out, errOut = w.mustRun(0, "sync")
	all += out + errOut
	if !strings.Contains(out, "cloning again") {
		t.Errorf("no re-clone reported:\n%s", out)
	}

	const missing = "1111111111111111111111111111111111111111"
	setRev(missing)
	code, out, errOut := w.run("sync")
	all += out + errOut
	if code == 0 || !strings.Contains(errOut, "commit "+missing+" not found in https://***@host-a.test/x/skills") {
		t.Errorf("missing commit: exit %d\n%s", code, errOut)
	}
	if strings.Contains(all, secret) {
		t.Errorf("credentials leaked:\n%s", all)
	}
}

// #28: the cache does not depend on the directory skenv runs in: the local
// config of a repository there (here an insteadOf to a mirror) changes
// neither the key nor the clone, so the next sync does not clone again.
func TestVendorCacheIgnoresCurrentRepository(t *testing.T) {
	w := newWorld(t)
	w.mapHost("https://host-a.test/", "host-a")
	revA := w.pushTool("host-a/x/skills", "from-host-a")
	w.pushTool("mirror/x/skills", "from-mirror")
	rev := w.initStandard(vendorEntry("tool", "https://host-a.test/x/skills", revA))
	dirs := w.cacheDirs()

	elsewhere := filepath.Join(w.work, "elsewhere")
	mustMkdir(t, elsewhere)
	w.git(elsewhere, "init", "--quiet")
	w.git(elsewhere, "config", "url.file://"+filepath.Join(w.remotes, "mirror", "x")+"/.insteadOf", "https://host-a.test/x/")
	t.Chdir(elsewhere)

	revA2 := w.pushTool("host-a/x/skills", "from-host-a-v2")
	w.push("me/skills", map[string]string{"skenv.toml": manifestText(rev, vendorEntry("tool", "https://host-a.test/x/skills", revA2))}, "chore: bump")
	out, _ := w.mustRun(0, "sync")
	if strings.Contains(out, "cloning again") {
		t.Errorf("sync cloned again:\n%s", out)
	}
	if got := w.vendored("tool"); !strings.Contains(got, "from-host-a-v2") {
		t.Errorf("tool:\n%s", got)
	}
	if got := w.cacheDirs(); strings.Join(got, " ") != strings.Join(dirs, " ") {
		t.Errorf("cache dirs %v, before %v", got, dirs)
	}
}

package cli

import (
	"os"
	"strings"
	"testing"
)

// clone reuses a working copy of the same repository, whatever the form of
// its origin URL, and refuses any other existing directory before it
// records anything.
func TestCloneExistingDirectory(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	w.git(w.home, "clone", "--quiet", "https://github.com/me/skills", w.path(ownPath))
	out, _ := w.mustRun(0, "clone", "me/skills", "~/"+ownPath)
	if !strings.Contains(out, "is a working copy of me/skills already; using it") ||
		!strings.Contains(readFile(t, w.path(".config/skenv/config.toml")), `manifest = "~/`+ownPath+`/skenv.toml"`) {
		t.Errorf("clone into its working copy:\n%s", out)
	}
	if w.exists(".agents/skills/alpha") {
		t.Error("clone synced")
	}

	if err := os.Remove(w.path(".config/skenv/config.toml")); err != nil {
		t.Fatal(err)
	}
	w.git(w.home, "clone", "--quiet", "https://github.com/ext/tools", w.path("src/tools"))
	mustMkdir(t, w.path("src/plain"))
	for dir, want := range map[string]string{
		"~/src/tools": "is a working copy of another repository",
		"~/src/plain": "is not a git working copy",
	} {
		if _, errOut := w.mustRun(2, "clone", "me/skills", dir); !strings.Contains(errOut, want) {
			t.Errorf("clone into %s: %s", dir, errOut)
		}
	}
	if w.exists(".config/skenv/config.toml") {
		t.Error("a refused clone recorded a manifest")
	}
}

// use records an existing checkout without its remote address, names the
// manifest it replaces and rejects a skenv file that is not a manifest.
func TestUse(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	w.git(w.home, "clone", "--quiet", "https://github.com/me/skills", w.path(ownPath))
	t.Chdir(w.path(ownPath))

	// The checkout has a manifest that is not recorded yet.
	if _, errOut := w.mustRun(2, "sync"); !strings.Contains(errOut, "this repository has a manifest (skenv.toml): run `skenv use .`") {
		t.Errorf("sync without a manifest in a checkout:\n%s", errOut)
	}
	out, _ := w.mustRun(0, "use", ".", "--dry-run")
	if !strings.Contains(out, "would record ~/"+ownPath+"/skenv.toml") || w.exists(".config/skenv") {
		t.Errorf("use --dry-run:\n%s", out)
	}
	w.mustRun(0, "use", ".")
	w.mustRun(0, "sync")
	w.mustRun(0, "doctor")

	other := w.path("src/other")
	mustMkdir(t, other)
	w.git(other, "init", "--quiet", "-b", "main")
	w.mustRun(0, "init", "--dir", other)
	out, _ = w.mustRun(0, "use", "~/"+ownPath+"/skenv.toml")
	if !strings.Contains(out, "it replaces manifest ~/src/other/skenv.toml") {
		t.Errorf("use after another manifest:\n%s", out)
	}
	writeFile(t, w.path("src/harness-only/skenv.toml"), "[repo]\nharness = \"0.4.0\"\nvisibility = \"private\"\n")
	if _, errOut := w.mustRun(2, "use", "~/src/harness-only"); !strings.Contains(errOut, "not a manifest") {
		t.Errorf("use of a file without [environment]: %s", errOut)
	}
}

// clone and use warn when the manifest names its own repository at another
// path: sync would clone a second working copy there.
func TestOwnPathMismatchWarning(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	_, errOut := w.mustRun(0, "clone", "me/skills", "~/elsewhere/skills")
	want := `warning: own me/skills has path "~/` + ownPath + `", but its checkout is ~/elsewhere/skills: ` +
		`sync would clone a second working copy at ~/` + ownPath + `; set path = "~/elsewhere/skills"`
	if !strings.Contains(errOut, want) {
		t.Errorf("clone into another path:\n%s", errOut)
	}
	if _, errOut = w.mustRun(0, "use", "~/elsewhere/skills"); !strings.Contains(errOut, want) {
		t.Errorf("use of a checkout at another path:\n%s", errOut)
	}
	w.git(w.home, "clone", "--quiet", "https://github.com/me/skills", w.path(ownPath))
	if _, errOut = w.mustRun(0, "use", "~/"+ownPath); strings.Contains(errOut, "warning") {
		t.Errorf("use of the checkout at its path warns:\n%s", errOut)
	}
}

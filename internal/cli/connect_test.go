package cli

import (
	"encoding/json"
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

// list shows every skill of the manifest with its kind, source, version
// and installation state, offline and with exit 0.
func TestList(t *testing.T) {
	w := newWorld(t)
	rev := w.push("ext/tools", map[string]string{"tools/archify/SKILL.md": skillMD("archify", "")}, "feat: archify")
	host, _ := os.Hostname()
	w.push("me/skills", map[string]string{
		"skenv.toml": selectManifest(rev, "exclude = [\"exp-*\"]\n",
			"\n[[environment.own]]\nrepo = \"me/team\"\npath = \"~/src/team\"\n\n[environment.host.\""+host+"\"]\nskip = [\"beta\"]\n"),
		"skills/alpha/SKILL.md":   skillMD("alpha", ""),
		"skills/beta/SKILL.md":    skillMD("beta", ""),
		"skills/exp-one/SKILL.md": skillMD("exp-one", ""),
	}, "feat: initial")
	w.mustRun(0, "clone", "me/skills", "~/"+ownPath)
	// me/team does not exist yet: sync reports it and links the rest.
	w.mustRun(1, "sync")
	writeFile(t, w.path(ownPath+"/skills/gamma/SKILL.md"), skillMD("gamma", ""))
	if err := os.RemoveAll(w.path(".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.path(".claude/skills/alpha/SKILL.md"), "mine\n")

	out, _ := w.mustRun(0, "list")
	for _, want := range []string{
		"manifest  ~/" + ownPath + "/skenv.toml\nstore     ~/.agents/skills\nagents    ~/.claude/skills, ~/.pi/agent/skills\n",
		"alpha    editable  me/skills  ~/" + ownPath + "  conflict\n",
		"archify  pinned    ext/tools  " + rev[:12] + "  installed\n",
		"exp-one  editable  me/skills  ~/" + ownPath + "  not selected\n",
		"gamma    editable  me/skills  ~/" + ownPath + "  not synced\n",
		"1 not synced: run `skenv sync`\n",
		"1 in conflict with paths skenv does not manage",
		"not cloned: me/team (~/src/team); run `skenv sync`\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if host != "" && !strings.Contains(out, "skipped on this host") {
		t.Errorf("list lacks the skipped skill:\n%s", out)
	}

	out, _ = w.mustRun(0, "list", "--json")
	var r struct {
		Skills []struct {
			Name, Kind, Source, Version, State string
		}
		NotCloned []string `json:"not_cloned"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("list --json: %v\n%s", err, out)
	}
	if len(r.NotCloned) != 1 || r.Skills[1].Name != "archify" || r.Skills[1].Version != rev {
		t.Errorf("list --json:\n%s", out)
	}
}

// vendor update and remove complete the names of the pinned skills,
// without files, errors or network access. cobra itself notes the
// directive on stderr, which the shell scripts discard.
func TestCompletionOfPinnedSkills(t *testing.T) {
	w := newWorld(t)
	w.initStandard("")
	for args, want := range map[string]string{
		"vendor update ":         "archify\n",
		"vendor update archify ": ":4\n",
		"vendor remove ":         "archify\n",
		"vendor remove archify ": ":4\n",
	} {
		fields := append([]string{"__complete"}, strings.Fields(args)...)
		code, out, errOut := w.run(append(fields, "")...)
		if code != 0 || !strings.HasPrefix(out, want) || !onlyDirective(errOut) {
			t.Errorf("completion of %q: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	// No manifest: nothing to offer, and nothing printed.
	t.Setenv("SKENV_MANIFEST", "/nonexistent/skenv.toml")
	if code, out, errOut := w.run("__complete", "vendor", "remove", ""); code != 0 || !strings.HasPrefix(out, ":4\n") || !onlyDirective(errOut) {
		t.Errorf("completion without a manifest: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func onlyDirective(stderr string) bool {
	return stderr == "Completion ended with directive: ShellCompDirectiveNoFileComp\n"
}

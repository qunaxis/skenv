package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/qunaxis/skenv/internal/config"
	"github.com/qunaxis/skenv/internal/docedit"
	"github.com/qunaxis/skenv/internal/fileformat"
	"github.com/qunaxis/skenv/internal/harness"
	"github.com/qunaxis/skenv/internal/manifest"
	"github.com/qunaxis/skenv/internal/skenvfile"
	"github.com/qunaxis/skenv/schemas"
)

var formats = []string{"toml", "yaml", "json"}

// noLefthook makes `repo init|apply` skip `lefthook install` for the test.
func noLefthook(t *testing.T) {
	lookLefthook = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookLefthook = exec.LookPath })
}

// directive is the schema directive line or key of a file in format.
func directive(format, url string) string {
	switch fileformat.Of("x." + format) {
	case "toml":
		return "#:schema " + url + "\n"
	case "yaml":
		return "# yaml-language-server: $schema=" + url + "\n"
	}
	return `"$schema": "` + url + `", `
}

// manifestIn is the standard manifest in format, with comments where the
// format has them and a schema directive.
func manifestIn(format, rev string) string {
	url := schemas.URL(schemas.Skenv, "")
	switch fileformat.Of("x." + format) {
	case "yaml":
		return directive(format, url) + "# test manifest\nenvironment:\n  own:\n    - repo: me/skills\n      path: ~/" + ownPath + "\n" +
			"  # pinned third-party skill\n  vendor:\n    - name: archify\n      repo: ext/tools\n      path: tools/archify\n      rev: \"" + rev + "\" # keep this comment\n"
	case "json":
		return `{` + directive(format, url) + `"environment": {"own": [{"repo": "me/skills", "path": "~/` + ownPath + `"}], ` +
			`"vendor": [{"name": "archify", "repo": "ext/tools", "path": "tools/archify", "rev": "` + rev + `"}]}}` + "\n"
	}
	return directive(format, url) + manifestText(rev, "")
}

// comments are the comments of manifestIn that every write keeps.
func comments(format string) []string {
	if format == "json" {
		return nil
	}
	return []string{"# test manifest", "# pinned third-party skill", "# keep this comment"}
}

// assertKept checks the invariant of every write: dir holds exactly one
// <base>.* file, <base>.<format>; it keeps the comments and a skenv
// schema directive; and parse accepts it.
func assertKept(t *testing.T, dir, base, format string, keep []string, parse func([]byte, string) error) []byte {
	t.Helper()
	var found []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), base+".") {
			found = append(found, e.Name())
		}
	}
	if want := []string{base + "." + format}; !slices.Equal(found, want) {
		t.Fatalf("%s holds %v, want %v", dir, found, want)
	}
	file := filepath.Join(dir, base+"."+format)
	data := []byte(readFile(t, file))
	for _, c := range keep {
		if !strings.Contains(string(data), c) {
			t.Errorf("%s lost %q:\n%s", filepath.Base(file), c, data)
		}
	}
	if url, ok := docedit.Directive(data, "."+format); !ok {
		t.Errorf("%s lost its schema directive:\n%s", filepath.Base(file), data)
	} else if _, _, ours := schemas.ParseURL(url); !ours {
		t.Errorf("%s: directive %s is not a skenv schema", filepath.Base(file), url)
	}
	if err := parse(data, "."+format); err != nil {
		t.Errorf("%s does not read back: %v\n%s", filepath.Base(file), err, data)
	}
	return data
}

// vendorsAre returns a parse function that checks the vendor names of the
// manifest.
func vendorsAre(names ...string) func([]byte, string) error {
	return func(data []byte, ext string) error {
		m, err := manifest.Parse(data, ext)
		if err != nil {
			return err
		}
		var got []string
		for _, v := range m.Vendor {
			got = append(got, v.Name)
		}
		if !slices.Equal(got, names) {
			return &mismatch{"vendors", strings.Join(got, ","), strings.Join(names, ",")}
		}
		return nil
	}
}

type mismatch struct{ what, got, want string }

func (m *mismatch) Error() string { return m.what + " = " + m.got + ", want " + m.want }

// Invariant (#20): no write changes the format of a file. Every write path
// (init recording the manifest in the tool config, init starting a
// manifest, repo init adding [repo], repo apply, vendor add|update|remove)
// runs on a file in each format (and skenv.yml, config.yml, which are
// read but never created); the file keeps its name and extension,
// reads back to the expected data, and keeps its comments and its schema
// directive.
func TestWritesKeepFormat(t *testing.T) {
	for _, format := range append(formats, "yml") {
		t.Run(format, func(t *testing.T) {
			noLefthook(t)
			w := newWorld(t)
			rev := w.standard("")
			w.push("me/skills", map[string]string{"skenv.toml": "", "skenv." + format: manifestIn(format, rev)}, "chore: "+format)
			cfgDir := config.Dir(w.home)
			own := w.path(ownPath)
			manifestFile := "~/" + ownPath + "/skenv." + format

			cfgKeep := []string{"# my config"}
			cfgText := directive(format, schemas.URL(schemas.Config, "")) + "# my config\nmanifest = \"/elsewhere\"\n"
			switch fileformat.Of("x." + format) {
			case "yaml":
				cfgText = directive(format, schemas.URL(schemas.Config, "")) + "# my config\nmanifest: /elsewhere\n"
			case "json":
				cfgText = "{" + directive(format, schemas.URL(schemas.Config, "")) + `"manifest": "/elsewhere"}` + "\n"
				cfgKeep = nil
			}
			writeFile(t, filepath.Join(cfgDir, "config."+format), cfgText)
			// manifestIs checks the manifest key of a config file.
			manifestIs := func(want string) func([]byte, string) error {
				return func(data []byte, ext string) error {
					home := t.TempDir()
					writeFile(t, filepath.Join(config.Dir(home), "config"+ext), string(data))
					cf, err := config.Load(home)
					if err != nil {
						return err
					}
					if v, _, _ := cf.String("manifest"); v != want {
						return &mismatch{"manifest", v, want}
					}
					return nil
				}
			}

			steps := []struct {
				name  string
				run   func()
				dir   string
				base  string
				keep  []string
				parse func([]byte, string) error
			}{
				{"clone records the manifest", func() {
					w.cloneSync("me/skills", "~/"+ownPath)
				}, cfgDir, "config", cfgKeep, manifestIs(manifestFile)},
				{"use with --format of the existing config", func() {
					w.mustRun(0, "use", "~/"+ownPath, "--format", fileformat.Of("x."+format))
				}, cfgDir, "config", cfgKeep, manifestIs(manifestFile)},
				{"vendor add", func() {
					w.mustRun(0, "vendor", "add", "ext/tools", "--path", "tools/other")
				}, own, "skenv", comments(format), vendorsAre("archify", "other")},
				{"vendor update", func() {
					next := w.push("ext/tools", map[string]string{"tools/other/SKILL.md": skillMD("other", "v2")}, "fix: other v2")
					w.mustRun(0, "vendor", "update", "other", "--rev", next)
					if !strings.Contains(readFile(t, filepath.Join(own, "skenv."+format)), next) {
						t.Error("rev not updated")
					}
				}, own, "skenv", comments(format), vendorsAre("archify", "other")},
				{"vendor remove", func() {
					w.mustRun(0, "vendor", "remove", "other")
				}, own, "skenv", comments(format), vendorsAre("archify")},
				{"repo init", func() {
					w.mustRun(0, "repo", "init", "--visibility", "private", "--dir", own)
				}, own, "skenv", comments(format), func(data []byte, ext string) error {
					if _, ok, err := harness.Parse(data, ext); err != nil || !ok {
						return &mismatch{"[repo]", "missing", "present"}
					}
					return vendorsAre("archify")(data, ext)
				}},
				{"repo apply", func() {
					file := filepath.Join(own, "skenv."+format)
					text := readFile(t, file)
					older := strings.NewReplacer(`"`+harness.Latest+`"`, `"0.3.0"`, "harness: "+harness.Latest, "harness: 0.3.0").Replace(text)
					if older == text {
						t.Fatalf("no harness to move back in:\n%s", text)
					}
					writeFile(t, file, older)
					w.mustRun(0, "repo", "apply", "--dir", own)
				}, own, "skenv", comments(format), func(data []byte, ext string) error {
					doc, err := skenvfile.Parse(data, ext)
					if err != nil {
						return err
					}
					if doc.Harness() != harness.Latest {
						return &mismatch{"repo.harness", doc.Harness(), harness.Latest}
					}
					return vendorsAre("archify")(data, ext)
				}},
				{"import", func() {
					tools := filepath.Join(w.work, "ext__tools")
					w.installed("other", map[string]string{"SKILL.md": skillMD("other", "v2")})
					w.writeLock(map[string]lockEntry{"other": githubEntry("ext/tools", "tools/other/SKILL.md", w.git(tools, "rev-parse", "HEAD:tools/other"))})
					w.mustRun(0, "import", "--sync")
				}, own, "skenv", comments(format), vendorsAre("archify", "other")},
			}
			for _, s := range steps {
				t.Run(s.name, func(t *testing.T) {
					s.run()
					assertKept(t, s.dir, s.base, format, s.keep, s.parse)
				})
			}
			w.mustRun(0, "repo", "check", "--dir", own)
			w.git(own, "add", "-A")
			w.git(own, "commit", "--quiet", "-m", "chore: harness")
			w.git(own, "push", "--quiet")
			w.mustRun(0, "doctor")

			// init without a repository adds [environment] to the skenv
			// file of another repository, and records it in the config.
			fresh := filepath.Join(w.home, "fresh")
			mustMkdir(t, fresh)
			w.git(fresh, "init", "--quiet", "-b", "main")
			repoText := map[string]string{
				"toml": "# my repo\n[repo]\nharness    = \"" + harness.Latest + "\"\nvisibility = \"private\"\n",
				"yaml": "# my repo\nrepo:\n  harness: " + harness.Latest + "\n  visibility: private\n",
				"yml":  "# my repo\nrepo:\n  harness: " + harness.Latest + "\n  visibility: private\n",
				"json": `{"repo": {"harness": "` + harness.Latest + `", "visibility": "private"}}`,
			}[format]
			writeFile(t, filepath.Join(fresh, "skenv."+format), repoText)
			var keep []string
			if format != "json" {
				keep = []string{"# my repo"}
			}
			t.Run("init starts a manifest", func(t *testing.T) {
				w.mustRun(0, "init", "--dir", fresh)
				assertKept(t, fresh, "skenv", format, keep, vendorsAre())
				assertKept(t, cfgDir, "config", format, cfgKeep, manifestIs("~/fresh/skenv."+format))
			})
		})
	}
}

// snapshot lists the files under dir (without .git) with their content,
// to prove that a refused command wrote nothing.
func snapshot(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.Type()&os.ModeSymlink != 0 {
				dest, err := os.Readlink(p)
				out[p] = "-> " + dest
				return err
			}
			if !d.IsDir() {
				b, err := os.ReadFile(p)
				out[p] = string(b)
				return err
			}
			out[p] = "<dir>"
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return out
}

func assertUnchanged(t *testing.T, before map[string]string, dirs ...string) {
	t.Helper()
	after := snapshot(t, dirs...)
	for p, v := range after {
		if before[p] != v {
			t.Errorf("%s changed", p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("%s removed", p)
		}
	}
}

// Acceptance (#20): repo init --format creates skenv.<format> with [repo]
// and the schema directive; repo check passes and repo apply keeps the
// format. --format that disagrees with an existing file exits 2 and writes
// nothing.
func TestRepoInitFormat(t *testing.T) {
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			noLefthook(t)
			w, repo := harnessRepo(t)
			w.mustRun(0, "repo", "init", "--visibility", "private", "--format", format, "--dir", repo)
			parse := func(data []byte, ext string) error {
				_, ok, err := harness.Parse(data, ext)
				if err == nil && !ok {
					return &mismatch{"[repo]", "missing", "present"}
				}
				return err
			}
			var keep []string
			if format != "json" {
				keep = []string{"# Repository harness"}
			}
			assertKept(t, repo, "skenv", format, keep, parse)
			w.mustRun(0, "repo", "check", "--dir", repo)
			out, _ := w.mustRun(0, "repo", "apply", "--dir", repo)
			if !strings.Contains(out, "up to date") {
				t.Errorf("apply after init: %s", out)
			}
			assertKept(t, repo, "skenv", format, keep, parse)
		})
	}
	w, repo := harnessRepo(t)
	writeFile(t, filepath.Join(repo, "skenv.toml"), "# mine\n[environment]\n")
	before := snapshot(t, repo)
	_, errOut := w.mustRun(2, "repo", "init", "--visibility", "private", "--format", "yaml", "--dir", repo)
	if !strings.Contains(errOut, "skenv.toml exists and is TOML; --format yaml does not convert it") {
		t.Errorf("mismatch: %s", errOut)
	}
	assertUnchanged(t, before, repo)
	if _, errOut := w.mustRun(2, "repo", "init", "--visibility", "private", "--format", "yml", "--dir", repo); !strings.Contains(errOut, "--format must be toml, yaml, json") {
		t.Errorf("--format yml: %s", errOut)
	}
}

// Acceptance (#20): clone <repo> --format json without a config creates
// config.json, and later runs keep it; --format that disagrees with the
// config exits 2 before anything is cloned.
func TestCloneConfigFormat(t *testing.T) {
	w := newWorld(t)
	w.standard("")
	w.mustRun(0, "clone", "me/skills", "~/"+ownPath, "--format", "json")
	cfgDir := config.Dir(w.home)
	manifestIs := func(data []byte, ext string) error {
		if !strings.Contains(string(data), `"manifest": "~/`+ownPath+`/skenv.toml"`) {
			return &mismatch{"manifest", string(data), ownPath}
		}
		return nil
	}
	assertKept(t, cfgDir, "config", "json", nil, manifestIs)
	w.cloneSync("me/skills", "~/"+ownPath)
	assertKept(t, cfgDir, "config", "json", nil, manifestIs)

	before := snapshot(t, w.home)
	for _, args := range [][]string{
		{"clone", "me/skills", "~/other", "--format", "toml"},
		{"clone", "me/skills", "~/other", "--format", "yaml", "--dry-run"},
		{"use", "~/" + ownPath, "--format", "toml"},
	} {
		_, errOut := w.mustRun(2, args...)
		if !strings.Contains(errOut, "config.json exists and is JSON; --format") {
			t.Errorf("skenv %s: %s", strings.Join(args, " "), errOut)
		}
	}
	assertUnchanged(t, before, w.home)
}

// Acceptance (#21): init starts a manifest in the
// current repository and records it; doctor then passes with nothing to
// sync.
func TestInitStartsManifest(t *testing.T) {
	for _, format := range append([]string{""}, formats...) {
		t.Run("format="+format, func(t *testing.T) {
			w, repo := harnessRepo(t)
			args := []string{"init", "--dir", repo}
			if format != "" {
				args = append(args, "--format", format)
			}
			before := snapshot(t, w.home)
			out, _ := w.mustRun(0, append(args, "--dry-run")...)
			if !strings.Contains(out, "would create ~/skills-repo/skenv.") || !strings.Contains(out, "would record") {
				t.Errorf("dry run:\n%s", out)
			}
			assertUnchanged(t, before, w.home)

			out, _ = w.mustRun(0, args...)
			if !strings.Contains(out, "next steps") || !strings.Contains(out, "skenv vendor add") {
				t.Errorf("init:\n%s", out)
			}
			if format == "" {
				format = "toml"
			}
			assertKept(t, repo, "skenv", format, nil, vendorsAre())
			assertKept(t, config.Dir(w.home), "config", format, nil, func(data []byte, ext string) error {
				if !strings.Contains(string(data), "~/skills-repo/skenv."+format) {
					return &mismatch{"manifest", string(data), "~/skills-repo/skenv." + format}
				}
				return nil
			})
			w.mustRun(0, "doctor")
			w.mustRun(0, "sync", "--quiet")

			// A second run refuses: the file has [environment].
			before = snapshot(t, w.home)
			if _, errOut := w.mustRun(2, "init", "--dir", repo); !strings.Contains(errOut, "has [environment] already") || !strings.Contains(errOut, "skenv use ") {
				t.Errorf("second init: %s", errOut)
			}
			assertUnchanged(t, before, w.home)
		})
	}
}

// With a GitHub origin the repository is the first own entry; its skills
// are synced like any own repository's.
func TestInitStartsManifestWithOwnRepository(t *testing.T) {
	w := newWorld(t)
	w.push("me/skills", map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", "")}, "feat: alpha")
	mustMkdir(t, w.path("src"))
	w.git(w.path("src"), "clone", "--quiet", "https://github.com/me/skills.git")
	out, _ := w.mustRun(0, "init", "--dir", w.path(ownPath+"/skills"), "--format", "yaml")
	if !strings.Contains(out, "me/skills as its first own repository") {
		t.Errorf("init:\n%s", out)
	}
	text := readFile(t, w.path(ownPath+"/skenv.yaml"))
	if !strings.Contains(text, "own:\n    - repo: me/skills\n      path: ~/"+ownPath+"\n") {
		t.Errorf("skenv.yaml:\n%s", text)
	}
	w.git(w.path(ownPath), "add", "-A")
	w.git(w.path(ownPath), "commit", "--quiet", "-m", "feat: manifest")
	w.git(w.path(ownPath), "push", "--quiet")
	w.mustRun(0, "sync", "--quiet")
	if w.readlink(".agents/skills/alpha") == "" {
		t.Error("alpha not linked")
	}
	w.mustRun(0, "doctor")
}

// An existing skenv file gets [environment] in its own format with its
// comments and [repo] intact; a public [repo], a --format mismatch and a
// directory outside git are errors that write nothing.
func TestInitStartsManifestInExistingFile(t *testing.T) {
	noLefthook(t)
	w, repo := harnessRepo(t)
	w.mustRun(0, "repo", "init", "--visibility", "private", "--dir", repo)
	file := filepath.Join(repo, "skenv.toml")
	withRepo := strings.Replace(readFile(t, file), "[repo]\n", "# my harness\n[repo]\n", 1)
	writeFile(t, file, withRepo)
	w.mustRun(0, "init", "--dir", repo)
	text := readFile(t, file)
	if !strings.HasPrefix(text, withRepo) || !strings.Contains(text, "\n[environment]\n") {
		t.Errorf("skenv.toml:\n%s", text)
	}
	w.mustRun(0, "repo", "check", "--dir", repo)
	w.mustRun(0, "doctor")

	pub := filepath.Join(w.home, "public")
	mustMkdir(t, pub)
	w.git(pub, "init", "--quiet", "-b", "main")
	w.mustRun(0, "repo", "init", "--visibility", "public", "--dir", pub)
	writeFile(t, filepath.Join(w.home, "yaml/skenv.yaml"), "repo:\n  harness: "+harness.Latest+"\n  visibility: private\n")
	w.git(w.path("yaml"), "init", "--quiet", "-b", "main")
	before := snapshot(t, w.home)
	for args, want := range map[string]string{
		"init --dir " + pub: `visibility = "public"`,
		"init --dir " + w.path("yaml") + " --format json": "skenv.yaml exists and is YAML; --format json does not convert it",
		"init --dir " + w.path(".config"):                 "is not inside a git repository",
		"init --path x":                                   "unknown flag: --path",
		"init me/skills --dir " + repo:                    "skenv clone <repo>",
	} {
		if _, errOut := w.mustRun(2, strings.Fields(args)...); !strings.Contains(errOut, want) {
			t.Errorf("skenv %s: %s", args, errOut)
		}
	}
	assertUnchanged(t, before, w.home)
}

// init without a repository runs in the repository of the current
// directory; a config that cannot be updated stops it before the skenv
// file is written.
func TestInitStartsManifestInCurrentDirectory(t *testing.T) {
	w, repo := harnessRepo(t)
	sub := filepath.Join(repo, "sub")
	mustMkdir(t, sub)
	t.Chdir(sub)

	writeFile(t, filepath.Join(config.Dir(w.home), "config.toml"), "manifest = \n")
	before := snapshot(t, w.home)
	if _, errOut := w.mustRun(2, "init"); !strings.Contains(errOut, "config.toml") {
		t.Errorf("broken config: %s", errOut)
	}
	assertUnchanged(t, before, w.home)

	writeFile(t, filepath.Join(config.Dir(w.home), "config.toml"), "manifest = \"~/old/skenv.toml\"\n")
	out, _ := w.mustRun(0, "init", "--dry-run")
	if !strings.Contains(out, "would replace manifest ~/old/skenv.toml") {
		t.Errorf("dry run does not show the replaced manifest:\n%s", out)
	}
	_, errOut := w.mustRun(0, "init")
	if !strings.Contains(errOut, "the config pointed at ~/old/skenv.toml") {
		t.Errorf("init: %s", errOut)
	}
	assertKept(t, repo, "skenv", "toml", nil, vendorsAre())
	// A new config would be TOML; the existing TOML config is kept.
	if got := readFile(t, filepath.Join(config.Dir(w.home), "config.toml")); !strings.Contains(got, `manifest = "~/skills-repo/skenv.toml"`) {
		t.Errorf("config.toml:\n%s", got)
	}
}

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/qunaxis/skenv/internal/buildinfo"
	"github.com/qunaxis/skenv/internal/cliexample"
	"github.com/qunaxis/skenv/internal/harness"
)

// The examples of every command (cobra's Example field) run here against a
// fixed world, and their output is recorded in docs/commands/examples,
// which the reference generator embeds under each example. After a change
// in the output: `make examples docs`.
//
// A runnable example needs a scenario (key: command path and the 1-based
// number of the example) or a reason to skip it; a line with shell syntax
// is not run.

// exampleSkips are runnable examples whose output is not recorded, by
// command path or by "<command path>/<n>".
var exampleSkips = map[string]string{
	"skenv":                   "the subcommand pages show the output",
	"skenv vendor":            "the subcommand pages show the output",
	"skenv repo":              "the subcommand pages show the output",
	"skenv autostart":         "the subcommand pages show the output",
	"skenv autostart enable":  "installs a LaunchAgent or a systemd user timer on the machine that runs it",
	"skenv autostart disable": "removes the LaunchAgent or the systemd user timer of the machine that runs it",
	"skenv autostart status":  "reports launchctl or systemd, which differ between machines",
	"skenv schema/2":          "prints the whole JSON Schema, published with the documentation",
}

// exampleScenarios prepare the world for one example: remotes, clones and
// the current directory.
var exampleScenarios = map[string]func(f *exampleWorld){
	"skenv init/1": func(f *exampleWorld) {
		repo := f.path("src/my-skills")
		mustMkdir(f.t, repo)
		f.git(repo, "init", "--quiet", "-b", "main")
		f.git(repo, "remote", "add", "origin", "https://github.com/example-org/my-skills.git")
		f.t.Chdir(repo)
	},
	"skenv init/2": func(f *exampleWorld) {
		f.pushRemotes()
		f.push("example-org/my-skills", map[string]string{
			"skills/code-review/SKILL.md": exampleSkill("code-review", "Review a diff for bugs before it is merged.", "Read the whole diff first."),
		}, "feat: code-review")
		mustMkdir(f.t, f.path("src"))
		f.git(f.path("src"), "clone", "--quiet", "https://github.com/example-org/my-skills.git")
		f.npxInstalled()
		f.t.Chdir(f.path("src/my-skills"))
	},
	"skenv init/3": func(f *exampleWorld) {
		repo := f.path("src/my-skills")
		mustMkdir(f.t, repo)
		f.git(repo, "init", "--quiet", "-b", "main")
		f.t.Chdir(repo)
	},
	"skenv clone/1": func(f *exampleWorld) {
		f.pushRemotes()
		mustMkdir(f.t, f.path("src"))
		f.t.Chdir(f.path("src"))
	},
	"skenv clone/2": func(f *exampleWorld) { f.pushRemotes() },
	"skenv use/1": func(f *exampleWorld) {
		f.pushRemotes()
		mustMkdir(f.t, f.path("src"))
		f.git(f.path("src"), "clone", "--quiet", "https://github.com/example-org/skills.git")
		f.t.Chdir(f.path("src/skills"))
	},
	"skenv list/1": func(f *exampleWorld) {
		// A skill written in the working copy, not linked yet.
		f.initialized()
		writeFile(f.t, f.path("src/skills/skills/write-tests/SKILL.md"),
			exampleSkill("write-tests", "Write table-driven tests for a Go function.", "Cover the edge cases first."))
	},
	"skenv import/1": func(f *exampleWorld) { f.initialized(); f.npxInstalled() },
	"skenv import/2": func(f *exampleWorld) { f.initialized(); f.npxInstalled() },
	"skenv import/3": func(f *exampleWorld) {
		// release-notes edited since it was installed, and a hash in the lock
		// that no commit has (a rewritten history): unmatched.
		f.initialized()
		f.npxInstalled()
		writeFile(f.t, f.path(".agents/skills/release-notes/SKILL.md"),
			exampleSkill("release-notes", "Draft release notes from merged changes.", "Edited here."))
		f.writeLock(map[string]lockEntry{"release-notes": githubEntry("example-vendor/tools", "tools/release-notes/SKILL.md", strings.Repeat("0", 40))})
	},
	"skenv import/4": func(f *exampleWorld) {
		// A project with a skill of `npx skills add` and one of its own.
		f.pushRemotes()
		dir := f.path("src/web-app")
		mustMkdir(f.t, dir)
		f.git(dir, "init", "--quiet", "-b", "main")
		tools := filepath.Join(f.work, "example-vendor__tools")
		writeFile(f.t, filepath.Join(dir, ".agents/skills/release-notes/SKILL.md"), readFile(f.t, filepath.Join(tools, "tools/release-notes/SKILL.md")))
		f.symlink("../../.agents/skills/release-notes", "src/web-app/.claude/skills/release-notes")
		// computeSkillFolderHash of skills 1.7.0 over tools/release-notes.
		writeFile(f.t, filepath.Join(dir, "skills-lock.json"), `{
  "version": 1,
  "skills": {
    "release-notes": {
      "source": "example-vendor/tools",
      "sourceType": "github",
      "skillPath": "tools/release-notes/SKILL.md",
      "computedHash": "652cdbb69a71f15b5ccbe20399899c611b35330bd2f7fc1dc6ff6ab9d9d55343"
    }
  }
}
`)
		writeFile(f.t, filepath.Join(dir, ".agents/skills/deploy/SKILL.md"),
			exampleSkill("deploy", "Deploy the web app to staging, then production.", "Run the smoke tests between the two."))
		f.t.Chdir(dir)
	},
	"skenv sync/1": func(f *exampleWorld) {
		// Committed, not pushed yet: a new skill and a second vendor skill.
		f.initialized()
		writeFile(f.t, f.path("src/skills/skills/write-tests/SKILL.md"),
			exampleSkill("write-tests", "Write table-driven tests for a Go function.", "Cover the edge cases first."))
		manifest := f.path("src/skills/skenv.toml")
		writeFile(f.t, manifest, readFile(f.t, manifest)+`
[[environment.vendor]]
name = "release-notes"
repo = "example-vendor/tools"
path = "tools/release-notes"
rev = "`+f.git(filepath.Join(f.work, "example-vendor__tools"), "rev-parse", "HEAD")+`"
`)
		f.git(f.path("src/skills"), "add", "-A")
		f.git(f.path("src/skills"), "commit", "--quiet", "-m", "feat: write-tests and release-notes")
	},
	"skenv sync/2": func(f *exampleWorld) { f.initialized(); f.upstreamChanges() },
	"skenv link/1": func(f *exampleWorld) {
		f.initialized()
		f.remove(".claude/skills/code-review")
	},
	"skenv doctor/1": func(f *exampleWorld) { f.initialized() },
	"skenv doctor/2": func(f *exampleWorld) {
		f.initialized()
		f.remove(".claude/skills/code-review")
	},
	"skenv vendor add/1": func(f *exampleWorld) { f.initialized() },
	"skenv vendor add/2": func(f *exampleWorld) {
		f.initialized()
		f.mapHost("https://gitlab.com/", "gitlab.com")
		f.push("gitlab.com/example-org/team/tools", map[string]string{
			"release-notes/SKILL.md": exampleSkill("release-notes", "Write release notes from the commits since the last tag.", "Group the commits by type."),
		}, "feat: release-notes")
	},
	"skenv vendor add/3": func(f *exampleWorld) {
		f.initialized()
		manifest := f.path("src/skills/skenv.toml")
		writeFile(f.t, manifest, readFile(f.t, manifest)+`
[environment.hosts.work]
url = "https://git.example.com"
type = "gitlab"
`)
		f.git(f.path("src/skills"), "commit", "--quiet", "-am", "feat: declare the work host")
		f.mapHost("https://git.example.com/", "git.example.com")
		f.push("git.example.com/platform/skills", map[string]string{
			"deploy/SKILL.md": exampleSkill("deploy", "Deploy a service to the staging cluster.", "Check the rollout before you leave."),
		}, "feat: deploy")
	},
	"skenv vendor update/1": func(f *exampleWorld) {
		f.initialized()
		f.newVendorCommits()
	},
	"skenv vendor update/2": func(f *exampleWorld) {
		f.initialized()
		f.mustRun(0, "vendor", "add", "example-vendor/tools", "--path", "tools/release-notes")
		f.newVendorCommits()
		f.push("example-vendor/tools", map[string]string{
			"tools/release-notes/SKILL.md": exampleSkill("release-notes", "Write release notes from the commits since the last tag.", "Group the commits by type; breaking changes first."),
		}, "feat(release-notes): breaking changes first")
	},
	"skenv vendor remove/1": func(f *exampleWorld) { f.initialized() },
	"skenv sync/3":          func(f *exampleWorld) { f.project(false) },
	"skenv doctor/3": func(f *exampleWorld) {
		dir := f.project(true)
		writeFile(f.t, filepath.Join(dir, ".agents/skills/diagrams/SKILL.md"),
			exampleSkill("diagrams", "Draw architecture and sequence diagrams.", "Draw the diagram in Mermaid."))
		f.remove("src/web-app/.claude/skills/deploy")
	},
	"skenv vendor add/4": func(f *exampleWorld) { f.project(true) },
	"skenv vendor update/3": func(f *exampleWorld) {
		f.project(true)
		f.newVendorCommits()
	},
	"skenv vendor remove/2": func(f *exampleWorld) { f.project(true) },
	"skenv lint/1": func(f *exampleWorld) {
		f.initialized()
		f.t.Chdir(f.path("src/skills"))
	},
	"skenv lint/2": func(f *exampleWorld) {
		f.initialized()
		writeFile(f.t, f.path("src/skills/skills/draft/SKILL.md"), "---\nname: Draft\n---\nSee [notes](references/notes.md).\n")
		f.t.Chdir(f.path("src/skills"))
	},
	"skenv lint/3": func(f *exampleWorld) {
		needTool(f.t, "gitleaks")
		repo := f.publicRepo()
		writeFile(f.t, f.path(".config/skenv/denylist.txt"), "internal-codename\n")
		f.t.Chdir(repo)
	},
	"skenv new/1": func(f *exampleWorld) {
		f.initialized()
		f.t.Chdir(f.path("src/skills"))
	},
	"skenv new/2": func(f *exampleWorld) { f.initialized() },
	"skenv repo init/1": func(f *exampleWorld) {
		repo := f.path("src/public-skills")
		mustMkdir(f.t, repo)
		f.git(repo, "init", "--quiet", "-b", "main")
		f.t.Chdir(repo)
	},
	"skenv repo init/2": func(f *exampleWorld) {
		repo := f.path("src/team-skills")
		mustMkdir(f.t, repo)
		f.git(repo, "init", "--quiet", "-b", "main")
		f.t.Chdir(repo)
	},
	"skenv repo init/3": func(f *exampleWorld) {
		repo := f.path("src/my-skills")
		mustMkdir(f.t, repo)
		f.git(repo, "init", "--quiet", "-b", "main")
		f.t.Chdir(repo)
	},
	"skenv repo apply/1": func(f *exampleWorld) {
		repo := f.publicRepo()
		f.editLefthook(repo)
		f.t.Chdir(repo)
	},
	"skenv repo check/1": func(f *exampleWorld) { f.t.Chdir(f.publicRepo()) },
	"skenv repo check/2": func(f *exampleWorld) {
		repo := f.publicRepo()
		f.editLefthook(repo)
		f.t.Chdir(repo)
	},
	"skenv version/1": func(f *exampleWorld) {
		// A release build: goreleaser sets these through -ldflags.
		old := [3]string{buildinfo.Version, buildinfo.Commit, buildinfo.Date}
		buildinfo.Version, buildinfo.Commit, buildinfo.Date = harness.Latest, "0123456789abcdef0123456789abcdef01234567", "2026-09-01T12:00:00Z"
		f.t.Cleanup(func() { buildinfo.Version, buildinfo.Commit, buildinfo.Date = old[0], old[1], old[2] })
	},
}

func TestExamples(t *testing.T) {
	// Absolute: the scenarios change the current directory.
	docs, err := filepath.Abs(filepath.Join("..", "..", "docs", "commands"))
	if err != nil {
		t.Fatal(err)
	}
	recorded := map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if !c.IsAvailableCommand() && c.HasParent() {
			return
		}
		path := c.CommandPath()
		examples := cliexample.Parse(c.Example)
		if len(examples) == 0 {
			t.Errorf("%s has no Example", path)
		}
		for i, ex := range examples {
			key := fmt.Sprintf("%s/%d", path, i+1)
			args, ok := ex.Args()
			if !ok || exampleSkips[path] != "" || exampleSkips[key] != "" {
				continue
			}
			setup := exampleScenarios[key]
			if setup == nil {
				t.Errorf("%s: no scenario for %q; add one to exampleScenarios or a reason to exampleSkips", key, ex.Line)
				continue
			}
			file := filepath.Join(docs, cliexample.File(path, i+1))
			recorded[file] = true
			t.Run(key, func(t *testing.T) {
				f := newExampleWorld(t)
				setup(f)
				got := f.record(args)
				if *update {
					writeFile(t, file, got.Format())
					return
				}
				want, ok, err := cliexample.Read(file)
				if err != nil {
					t.Fatal(err)
				}
				if !ok || got != want {
					t.Errorf("%s: output of %q differs from %s (run `make examples docs` if intended):\n--- got (exit %d)\n%s--- want (exit %d)\n%s",
						key, ex.Line, file, got.Code, got.Text, want.Code, want.Text)
				}
			})
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(Command())

	// Output of an example that no longer exists.
	files, err := filepath.Glob(filepath.Join(docs, "examples", "*", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if recorded[file] {
			continue
		}
		if *update {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			continue
		}
		t.Errorf("%s belongs to no example (run `make examples docs`)", file)
	}
}

// exampleWorld is a world with neutral names, fixed commit dates (stable
// SHAs) and a stand-in lefthook.
type exampleWorld struct {
	*world
	root string // parent of $HOME, remotes and scratch clones
}

func newExampleWorld(t *testing.T) *exampleWorld {
	t.Helper()
	w := newWorld(t)
	for _, k := range []string{"GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE"} {
		t.Setenv(k, "2026-09-01T12:00:00Z")
	}
	f := &exampleWorld{world: w, root: filepath.Dir(w.home)}
	lefthook := filepath.Join(f.root, "bin", "lefthook")
	writeFile(t, lefthook, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(lefthook, 0o755); err != nil {
		t.Fatal(err)
	}
	old := lookLefthook
	lookLefthook = func(string) (string, error) { return lefthook, nil }
	t.Cleanup(func() { lookLefthook = old })
	oldTool := lookTool
	lookTool = func(name string) (string, error) { return filepath.Join(f.root, "bin", name), nil }
	t.Cleanup(func() { lookTool = oldTool })
	return f
}

func exampleSkill(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\n" + body + "\n"
}

// pushRemotes pushes the manifest repository example-org/skills (a private
// skills repository whose skenv.toml holds the manifest) and the vendor
// repository example-vendor/tools; the manifest pins diagrams.
func (f *exampleWorld) pushRemotes() {
	rev := f.push("example-vendor/tools", map[string]string{
		"README.md":                    "# Tools\n",
		"tools/diagrams/SKILL.md":      exampleSkill("diagrams", "Draw architecture and sequence diagrams.", "Draw the diagram, then check it."),
		"tools/release-notes/SKILL.md": exampleSkill("release-notes", "Write release notes from the commits since the last tag.", "Group the commits by type."),
	}, "feat: diagrams and release-notes")
	f.push("example-org/skills", map[string]string{
		"skenv.toml": `[repo]
harness = "` + harness.Latest + `"
visibility = "private"

[[environment.own]]
repo = "example-org/skills"
path = "~/src/skills"

[[environment.vendor]]
name = "diagrams"
repo = "example-vendor/tools"
path = "tools/diagrams"
rev = "` + rev + `"
`,
		"skills/code-review/SKILL.md":    exampleSkill("code-review", "Review a diff for bugs before it is merged.", "Read the whole diff first."),
		"skills/commit-message/SKILL.md": exampleSkill("commit-message", "Write a Conventional Commits message for the staged changes.", "Explain why in the body."),
	}, "feat: first skills")
}

// npxInstalled installs release-notes of example-vendor/tools the way
// `npx skills add -g` does (a copy in the store, a link for Claude Code and
// an entry in its lock), and a skill by hand.
func (f *exampleWorld) npxInstalled() {
	tools := filepath.Join(f.work, "example-vendor__tools")
	f.installed("release-notes", map[string]string{
		"SKILL.md": readFile(f.t, filepath.Join(tools, "tools/release-notes/SKILL.md")),
	})
	entry := githubEntry("example-vendor/tools", "tools/release-notes/SKILL.md", f.git(tools, "rev-parse", "HEAD:tools/release-notes"))
	entry["installedAt"], entry["updatedAt"] = "2026-09-01T12:00:00.000Z", "2026-09-01T12:00:00.000Z"
	f.writeLock(map[string]lockEntry{"release-notes": entry})
	writeFile(f.t, f.path(".claude/skills/notes/SKILL.md"), exampleSkill("notes", "Take meeting notes.", "One line per decision."))
}

// project is a project repository ~/src/web-app, the current directory,
// whose skenv.toml pins diagrams from example-vendor/tools and code-review
// from example-org/skills, mirrored into .claude/skills, with a
// project-own skill deploy; synced and committed when synced is set. It
// returns the repository.
func (f *exampleWorld) project(synced bool) string {
	f.pushRemotes()
	vendorRev := f.git(filepath.Join(f.work, "example-vendor__tools"), "rev-parse", "HEAD")
	ownRev := f.git(filepath.Join(f.work, "example-org__skills"), "rev-parse", "HEAD")
	dir := f.path("src/web-app")
	mustMkdir(f.t, dir)
	f.git(dir, "init", "--quiet", "-b", "main")
	writeFile(f.t, filepath.Join(dir, "skenv.toml"), `[project]
dir     = ".agents/skills"
mirrors = [".claude/skills"]

[[project.vendor]]
name = "diagrams"
repo = "example-vendor/tools"
path = "tools/diagrams"
rev  = "`+vendorRev+`"

[[project.from]]
repo   = "example-org/skills"
skills = ["code-review"]
rev    = "`+ownRev+`"
`)
	writeFile(f.t, filepath.Join(dir, ".agents/skills/deploy/SKILL.md"),
		exampleSkill("deploy", "Deploy the web app to staging, then production.", "Run the smoke tests between the two."))
	f.t.Chdir(dir)
	if synced {
		f.mustRun(0, "sync")
		f.git(dir, "add", "-A")
		f.git(dir, "commit", "--quiet", "-m", "chore(skills): sync project skills")
	}
	return dir
}

// initialized is the machine after `skenv clone example-org/skills
// ~/src/skills` and `skenv sync`.
func (f *exampleWorld) initialized() {
	f.pushRemotes()
	f.cloneSync("example-org/skills", "~/src/skills")
}

// newVendorCommits moves example-vendor/tools ahead of the pin of diagrams.
func (f *exampleWorld) newVendorCommits() {
	f.push("example-vendor/tools", map[string]string{
		"tools/diagrams/SKILL.md": exampleSkill("diagrams", "Draw architecture and sequence diagrams as SVG.", "Prefer SVG over PNG."),
	}, "feat(diagrams): prefer SVG")
	f.push("example-vendor/tools", map[string]string{
		"tools/diagrams/references/shapes.md": "# Shapes\n",
	}, "docs(diagrams): list the shapes")
}

// upstreamChanges adds a skill to the manifest repository and moves the
// pin of diagrams, as another machine would.
func (f *exampleWorld) upstreamChanges() {
	rev := f.push("example-vendor/tools", map[string]string{
		"tools/diagrams/SKILL.md": exampleSkill("diagrams", "Draw architecture and sequence diagrams as SVG.", "Prefer SVG over PNG."),
	}, "feat(diagrams): prefer SVG")
	manifest := readFile(f.t, filepath.Join(f.work, "example-org__skills", "skenv.toml"))
	_, old, _ := strings.Cut(manifest, `rev = "`)
	old, _, _ = strings.Cut(old, `"`)
	f.push("example-org/skills", map[string]string{
		"skenv.toml":                  strings.Replace(manifest, old, rev, 1),
		"skills/write-tests/SKILL.md": exampleSkill("write-tests", "Write table-driven tests for a Go function.", "Cover the edge cases first."),
	}, "feat: write-tests, diagrams with SVG")
}

// publicRepo is a public skills repository with its harness set up and
// one committed skill.
func (f *exampleWorld) publicRepo() string {
	repo := f.path("src/public-skills")
	mustMkdir(f.t, repo)
	f.git(repo, "init", "--quiet", "-b", "main")
	f.mustRun(0, "repo", "init", "--visibility", "public", "--dir", repo)
	writeFile(f.t, filepath.Join(repo, "LICENSE"), "MIT License\n")
	writeFile(f.t, filepath.Join(repo, "skills/changelog/SKILL.md"),
		"---\nname: changelog\ndescription: Keep CHANGELOG.md in the Keep a Changelog format.\nlicense: MIT\nmetadata:\n  source: original\n---\n\n# changelog\n\nAdd an entry under Unreleased.\n")
	f.git(repo, "add", "-A")
	f.git(repo, "commit", "--quiet", "-m", "feat: changelog")
	return repo
}

func (f *exampleWorld) editLefthook(repo string) {
	p := filepath.Join(repo, "lefthook.yml")
	writeFile(f.t, p, readFile(f.t, p)+"# local change\n")
}

func (f *exampleWorld) remove(p string) {
	if err := os.RemoveAll(f.path(p)); err != nil {
		f.t.Fatal(err)
	}
}

var backupTS = regexp.MustCompile(`backup/[0-9]{8}T[0-9]{6}Z/`)

// record runs skenv with stdout and stderr in one stream, as a terminal
// shows them, and replaces the temporary directories: $HOME by ~, the
// remotes by https://github.com.
func (f *exampleWorld) record(args []string) cliexample.Output {
	f.t.Helper()
	var out bytes.Buffer
	code := Main(context.Background(), args, &out, &out)
	text := out.String()
	for _, r := range [][2]string{
		{"file://" + f.remotes, "https://github.com"},
		{f.remotes, "https://github.com"},
		{f.home, "~"},
		{f.root, "/tmp"},
	} {
		text = strings.ReplaceAll(text, r[0], r[1])
	}
	text = backupTS.ReplaceAllString(text, "backup/<timestamp>/")
	return cliexample.Output{Code: code, Text: text}
}

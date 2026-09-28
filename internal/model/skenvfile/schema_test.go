package skenvfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each structural rule the Go code used to check by hand is now the
// schema's (#73): the parser rejects the document, and the error names the
// key as a TOML path and says how to fix it.
func TestSchemaRules(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	dep := func(name, fields string) string { return "[user.dependencies." + name + "]\n" + fields }
	full := "repo = \"a/b\"\ncommit = \"" + sha + "\"\n"
	checkout := "[user.checkouts.a]\nrepo = \"a/b\"\ncheckout_dir = \"~/x\"\n"
	from := func(fields string) string { return "[project.from.c]\nrepo = \"a/c\"\n" + fields }
	fromFull := "skills = [\"x\"]\ncommit = \"" + sha + "\"\n"
	cases := []struct {
		rule, ext, text, want string
	}{
		// The top level and the kinds of values (skenvfile.go).
		{"unknown top-level key", ".toml", "other = \"x\"\n", "other: unknown key (known: $schema, project, repository, user)"},
		{"section not a table", ".json", `{"repository": 1}`, "repository: must be a table, got a number"},
		{"$schema not a string", ".json", `{"$schema": 1}`, `"$schema": must be a string, got a number; quote it`},
		{"null value", ".json", `{"user": {"dependencies": null}}`, "[user] dependencies: is empty (null); give it a value or remove it"},
		{"number in a list", ".yaml", "project:\n  mirrors: [a, 1]\n", "[project] mirrors[1]: must be a string, got a number; quote it"},
		{"unquoted numeric commit", ".yaml", "user:\n  dependencies:\n    x: {repo: a/b, commit: 1234}\n", "[user.dependencies.x] commit: must be a string, got a number; quote it"},
		{"TOML date", ".toml", "[user.storage]\ndir = 2024-01-01\n", "[user.storage] dir: must be a string, a table or a list, got a date"},
		{"unknown key in TOML", ".toml", dep("x", full+"tag = \"v1\"\n"), "[user.dependencies.x] tag: unknown key (known: commit, repo, skill_dir)"},
		{"unknown key in YAML", ".yaml", "user:\n  storage:\n    dirs: x\n", "[user.storage] dirs: unknown key (known: dir)"},
		{"key case in JSON", ".json", `{"user": {"Storage": {}}}`, "[user] Storage: unknown key"},

		// [repository] (repository.go).
		{"public repository with [user]", ".toml", "[repository]\ntemplate_version = \"0.4.0\"\nvisibility = \"public\"\n[user]\n", "user: A public repository must not carry [user]"},

		// [user] (manifest.go).
		{"checkout ID", ".toml", "[user.checkouts.A]\nrepo = \"a/b\"\ncheckout_dir = \"~/x\"\n", `[user.checkouts] A: invalid value. An ID is lowercase letters`},
		{"checkout repo required", ".toml", "[user.checkouts.a]\ncheckout_dir = \"~/x\"\n", "[user.checkouts.a] repo: is required"},
		{"checkout repo empty", ".toml", "[user.checkouts.a]\nrepo = \"\"\ncheckout_dir = \"~/x\"\n", "[user.checkouts.a] repo: must not be empty"},
		{"checkout_dir required", ".toml", "[user.checkouts.a]\nrepo = \"a/b\"\n", "[user.checkouts.a] checkout_dir: is required"},
		{"skills_dir escapes", ".toml", checkout + "skills_dir = \"../s\"\n", "[user.checkouts.a] skills_dir: invalid value. A relative path inside the repository"},
		{"branch", ".toml", checkout + "branch = \"-x\"\n", "[user.checkouts.a] branch: invalid value. A git branch name."},
		{"include twice", ".toml", checkout + "include = [\"x\", \"x\"]\n", `[user.checkouts.a] include: lists "x" twice`},
		{"include with a slash", ".toml", checkout + "include = [\"a/b\"]\n", "[user.checkouts.a] include[0]: invalid value. A skill name or a glob over names"},
		{"empty exclude", ".toml", checkout + "exclude = [\"\"]\n", "[user.checkouts.a] exclude[0]: invalid value. A skill name or a glob over names"},
		{"unmanaged with a slash", ".toml", "[user]\nunmanaged = [\"a/b\"]\n", "[user] unmanaged[0]: invalid value. A glob over entry names"},
		{"dependency name", ".toml", dep(`"foo.bar"`, full), `[user.dependencies] "foo.bar": invalid value. A skill name is lowercase letters`},
		{"reserved dependency name", ".toml", dep("synced", full), `[user.dependencies] synced: "synced" is reserved`},
		{"long dependency name", ".toml", dep(strings.Repeat("a", 65), full), "is longer than 64 characters"},
		{"dependency repo required", ".toml", dep("x", "commit = \""+sha+"\"\n"), "[user.dependencies.x] repo: is required"},
		{"dependency commit required", ".toml", dep("x", "repo = \"a/b\"\n"), "[user.dependencies.x] commit: is required"},
		{"short commit", ".toml", dep("x", "repo = \"a/b\"\ncommit = \"abc1234\"\n"), "[user.dependencies.x] commit: invalid value. A full 40-character lowercase commit SHA"},
		{"absolute skill_dir", ".toml", dep("x", full+"skill_dir = \"/etc\"\n"), "[user.dependencies.x] skill_dir: invalid value. A relative path"},
		{"machine include twice", ".toml", "[user.machines.m]\ninclude = [\"a\", \"a\"]\n", `[user.machines.m] include: lists "a" twice`},
		{"empty checkout_dirs entry", ".toml", checkout + "[user.machines.m.checkout_dirs]\na = \"\"\n", "[user.machines.m.checkout_dirs] a: must not be empty"},
		{"unknown agent", ".toml", "[user.agents]\nenabled = [\"codex\"]\n", `[user.agents] enabled[0]: must be one of "claude", "pi". Add other directories to user.agents.extra_dirs; Codex needs no entry`},
		{"agent twice", ".toml", "[user.agents]\nenabled = [\"pi\", \"pi\"]\n", `[user.agents] enabled: lists "pi" twice`},
		{"agent path of an unknown agent", ".toml", "[user.agents.paths]\ncodex = \"~/x\"\n", `[user.agents.paths] codex: must be one of "claude", "pi". Add other directories`},
		{"empty agent path", ".toml", "[user.agents.paths]\npi = \"\"\n", "[user.agents.paths] pi: must not be empty"},
		{"empty extra dir", ".toml", "[user.agents]\nextra_dirs = [\"\"]\n", "[user.agents] extra_dirs[0]: must not be empty"},

		// [project] (project.go).
		{"project dir is the root", ".toml", "[project]\ndir = \".\"\n", "[project] dir: dir must be a directory inside the repository, not its root."},
		{"project dir escapes", ".toml", "[project]\ndir = \"../s\"\n", "[project] dir: invalid value. A relative path"},
		{"mirror is the root", ".toml", "[project]\nmirrors = [\".\"]\n", "[project] mirrors[0]: A mirror is a directory inside the repository, not its root."},
		{"empty mirror", ".toml", "[project]\nmirrors = [\"\"]\n", "[project] mirrors[0]: must not be empty"},
		{"mirrors_mode", ".toml", "[project]\nmirrors_mode = \"hardlink\"\n", `[project] mirrors_mode: must be one of "symlink", "copy"`},
		{"from ID", ".toml", "[project.from.C]\nrepo = \"a/c\"\n" + fromFull, "[project.from] C: invalid value. An ID is"},
		{"from repo required", ".toml", "[project.from.c]\n" + fromFull, "[project.from.c] repo: is required"},
		{"from skills_dir", ".toml", from(fromFull + "skills_dir = \"/s\"\n"), "[project.from.c] skills_dir: invalid value. A relative path"},
		{"from skills required", ".toml", from("commit = \"" + sha + "\"\n"), "[project.from.c] skills: is required"},
		{"from skills empty", ".toml", from("skills = []\ncommit = \"" + sha + "\"\n"), "[project.from.c] skills: must list at least 1"},
		{"from skill name", ".toml", from("skills = [\"A\"]\ncommit = \"" + sha + "\"\n"), "[project.from.c] skills[0]: invalid value. A skill name is"},
		{"from skill twice", ".toml", from("skills = [\"x\", \"x\"]\ncommit = \"" + sha + "\"\n"), `[project.from.c] skills: lists "x" twice`},
		{"from commit", ".toml", from("skills = [\"x\"]\ncommit = \"main\"\n"), "[project.from.c] commit: invalid value. A full 40-character"},
		{"project dependency commit", ".toml", "[project.dependencies.x]\nrepo = \"a/b\"\ncommit = \"HEAD\"\n", "[project.dependencies.x] commit: invalid value. A full 40-character"},

		// git_hosts (hosts.go).
		{"alias", ".toml", "[user.git_hosts.Work]\nbase_url = \"https://a.example\"\n", "[user.git_hosts] Work: invalid value. An alias is lowercase letters"},
		{"built-in alias", ".toml", "[project.git_hosts.gitlab]\nbase_url = \"https://a.example\"\n", "[project.git_hosts] gitlab: This prefix is built in and cannot be declared."},
		{"base_url required", ".toml", "[user.git_hosts.work]\nprovider = \"gitlab\"\n", "[user.git_hosts.work] base_url: is required"},
		{"provider", ".toml", "[user.git_hosts.work]\nbase_url = \"https://a.example\"\nprovider = \"bitbucket\"\n", `[user.git_hosts.work] provider: must be one of "github", "gitlab", "gitea", "generic"`},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			_, err := Parse([]byte(c.text), c.ext)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v\nwant %q", err, c.want)
			}
		})
	}
}

// Several violations come one per line, sorted, after the file name, and
// no value is echoed: a base URL may carry credentials.
func TestSchemaErrorFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skenv.toml")
	text := "[user.git_hosts.work]\nbase_url = \"https://user:secret@git.example.com\"\n[user.checkouts.a]\nrepo = \"a/b\"\n"
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadManifest(p)
	want := "manifest " + p + ": 2 errors:\n" +
		"  [user.checkouts.a] checkout_dir: is required\n" +
		"  [user.git_hosts.work] base_url: invalid value. A base URL such as \"https://git.example.com\" (no credentials) or \"ssh://git@git.example.com\"."
	if err == nil || err.Error() != want {
		t.Errorf("err =\n%v\nwant\n%s", err, want)
	}
}

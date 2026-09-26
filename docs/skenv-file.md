# The skenv file

Every repository skenv works with has one skenv file at its root:
`skenv.toml`, or `skenv.yaml`, `skenv.yml` or `skenv.json` if you prefer
those formats. More than one of them in the same directory is an error.

- [Sections](#sections)
- [Formats and editing](#formats-and-editing)
- [Creating the file](#creating-the-file)
- [Editor support](#editor-support)
- [Where skenv looks](#where-skenv-looks)
- [Moving to the 0.6 format](#moving-to-the-0-6-format)

## Sections

The file has three optional top-level sections, independent of each
other. Nothing else is allowed at
the top level (except `$schema`, the schema URL for editors, which skenv
ignores), and unknown keys inside a section are errors.

| Section        | What it is                                                                                                        | Who writes it                              | Reference                     |
| -------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------ | ----------------------------- |
| `[repository]` | The development tooling of a skills repository: `template_version`, `visibility`, `ci`.                           | [`skenv repo init`](harness.md#skenv-repo-init), [`skenv repo upgrade`](harness.md#skenv-repo-upgrade) | [harness](harness.md)         |
| `[user]`       | The manifest: skills for the agents of your OS user, across projects: `checkouts`, `dependencies`, `machines`, `agents`, `storage`, `unmanaged`, `git_hosts`. | you, [`skenv init`](#creating-the-file) (starts it), [`skenv import`](adopting.md), [`skenv vendor add`](manage-skills.md#add-a-third-party-skill), [`update`](manage-skills.md#update-pinned-skills), [`remove`](manage-skills.md#remove-a-skill) | [manifest](manifest.md)       |
| `[project]`    | The skills a project repository carries: `dir`, `mirrors`, `mirrors_mode`, `git_hosts`, `dependencies`, `from`.   | you, [`skenv vendor add`, `update`, `remove`](project-skills.md#commands) with `--project` | [project skills](project-skills.md) |

- A skills repository has `[repository]`.
- The repository that holds your manifest has `[user]`, and usually
  `[repository]` too, because it is also where your own skills live.
- `repo` in `[user]` takes `owner/repo` or `github:owner/repo` on GitHub,
  `gitlab:` and `codeberg:` short forms, aliases of self-hosted servers
  declared under `[user.git_hosts.<alias>]`, and any git URL; see
  [Git hosts](git-hosts.md).
- A project repository has `[project]`; a skills repository whose own
  development uses project skills has `[repository]` and `[project]`. The
  user-level `skenv sync` never reads `[project]`.
- A **public** repository must not have `[user]`. The manifest is
  personal: it names paths in your home directory, your machine names and
  the skills you use. `skenv repo init --visibility public` refuses such a
  file and `skenv repo check` fails on it. `visibility` is a policy you
  declare; skenv never reads or changes the access setting on the hosting
  service.

A private skills repository that also holds the manifest:

```toml
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.github]
runs_on = ["ubuntu-latest"]

[user]
unmanaged = ["peon-ping-*"]

[user.checkouts.skills-repo]
repo         = "<owner>/<skills-repo>"
checkout_dir = "."

[user.dependencies.archify]
repo      = "tt-a1i/archify"
skill_dir = "archify"
commit    = "<full 40-character commit SHA>"

[user.machines."my-laptop"]
exclude = ["bpmn-process-modeler"]
```

The same in YAML (`skenv.yaml`):

```yaml
repository:
  template_version: 0.6.0
  visibility: private
  ci:
    github:
      runs_on: [ubuntu-latest]
user:
  unmanaged: ["peon-ping-*"]
  checkouts:
    skills-repo:
      repo: <owner>/<skills-repo>
      checkout_dir: .
  dependencies:
    archify:
      repo: tt-a1i/archify
      skill_dir: archify
      commit: "<full 40-character commit SHA>"
  machines:
    my-laptop:
      exclude: [bpmn-process-modeler]
```

## Formats and editing

TOML, YAML and JSON are read the same way; keys are lowercase and
case-sensitive in every format. The skenv file is the desired state:
`sync`, `link`, `doctor`, `list`, `config show` and `repo apply` read it
and never change it. skenv edits the file only in these places:

- `skenv vendor add|update|remove` change `user.dependencies`, and with
  `--project` `project.dependencies` (and the `commit` of `project.from`);
- `skenv repo init` adds `[repository]` (it creates the file when the
  repository has none, see [Creating the file](#creating-the-file));
- `skenv repo upgrade` sets `repository.template_version` to the templates
  of the installed skenv and updates the schema directive;
- `skenv init` adds `[user]` (it creates the file too);
- `skenv import` adds `user.checkouts` and `user.dependencies` entries for
  the skills already installed on the machine (and `[user]` itself when the
  file has none); `skenv import --project` adds `project.dependencies`
  entries (and `[project]`, or `skenv.toml`, when missing).

No edit changes the format of the file: `skenv.yaml` stays YAML under the
same name, `skenv.yml` stays `skenv.yml`, and so on.

Each edit changes only what it has to, in every format: comments, blank
lines, key order and the formatting of everything else stay. New entries
come in the order the docs use (`repo`, `skill_dir`, `commit` for a
dependency; `template_version`, `visibility`, `ci` for `[repository]`). In
YAML, `[repository]` goes to the top of the file and a new dependency
follows the existing ones. JSON is written with two-space indentation,
keeps its key order and `"$schema"`, and leaves `<`, `>` and `&` as they
are.

skenv can only edit a YAML list or mapping written in block style (one
`- ` item or `key:` per line); an empty `[]` or `{}` is fine. If
`user.dependencies` is written in flow style (`{archify: {…}}`), the edit
stops with an error that says so, and the file is left as it is.

## Creating the file

Two commands create a skenv file when the repository has none:
`skenv repo init` with `[repository]`, and `skenv init`
with `[user]`. Both write `skenv.toml` unless you pass `--format`:

```sh
skenv repo init --visibility private --format yaml   # skenv.yaml with [repository]
skenv init --format json                             # skenv.json with [user]
```

`--format` takes `toml` (the default), `yaml` or `json`. skenv reads
`skenv.yml` but never creates it. A new TOML or YAML file starts with a
comment that says what the section is for, and every new file names its
schema (see [Editor support](#editor-support)). A new `[user]` is a
commented skeleton: your repository becomes its first checkout,
`[user.checkouts.<repository name>]` with `checkout_dir = "."`, when its
`origin` is on a network host (GitHub, GitLab and Codeberg in
their short forms, see [Git hosts](git-hosts.md#starting-a-manifest-with-skenv-init)),
and a `[user.dependencies.<skill>]` table is shown as a commented example
in TOML.

When the repository already has a skenv file, both commands add their
section to it in its own format. They never convert it: `--format` that
disagrees with the file is an error (exit code 2), and nothing is written.
Leave `--format` out to use the file as it is, or convert the file by hand.

`skenv init` starts a manifest in the git repository of the current
directory (or `--dir`):

```sh
cd ~/src/<skills-repo>
skenv init --dry-run   # show what it would write
skenv init
```

It refuses when the file has `[user]` already (`skenv use .` records
that one), or when its `[repository]` says `visibility = "public"`. Then it
records the file as `manifest` in the [tool config](configuration.md), the
same way `skenv use` does, and prints the next steps. It doesn't sync. The new manifest is not
necessarily empty: when the repository's `origin` is on a network host, the
repository itself is already its first checkout, so the
first `skenv sync` links the skills it holds.

## Editor support

skenv publishes a JSON Schema of this file, and every skenv file it writes
names it in a directive: a first line `#:schema <url>` in TOML,
`# yaml-language-server: $schema=<url>` in YAML, a `"$schema"` key in JSON.
Editors then complete keys, describe them on hover and mark mistakes. See
[Editor support](editor-support.md) for the URLs and editor setup.

## Where skenv looks

- The manifest: `--manifest`, then `$SKENV_MANIFEST`, then `manifest` in the
  tool config `~/.config/skenv/config.toml` (written by `skenv init`,
  `skenv clone` or `skenv use`). Each
  names the skenv file or the directory that holds it.
- The harness: the skenv file at the root of the repository (`--dir`,
  default: the current repository).
- Project skills: the skenv file at the root of the git repository of the
  current directory, when it has `[project]`.

The tool config is separate on purpose: it belongs to the machine, not to a
repository, and lives in `~/.config/skenv/config.{toml,yaml,yml,json}`.

<a id="moving-to-the-0-6-format"></a>

## Moving to the 0.6 format

skenv 0.6 renamed the sections and most keys of the skenv file. It does
not read the old format, and there is no migrate command: a file with an
old key stops every command with an error that lists each old key found
and its replacement. Rewrite the file by hand with the table below, then
run `skenv doctor` (and `skenv repo check` in a skills repository).

| Before 0.6 | 0.6 | Notes |
| --- | --- | --- |
| `[environment]` | `[user]` | |
| `[[environment.own]]` | `[user.checkouts.<id>]` | A table per checkout, keyed by an ID of your choice (`^[a-z0-9][a-z0-9_-]{0,63}$`), for example the repository name. |
| `own.path` | `checkout_dir` | Relative values now resolve against the directory of the skenv file; they used to depend on the working directory of the command. `"."` is the repository that holds the file. |
| `own.skills` | `include` | Omitted: every skill; `include = []` selects none (it used to be an error). Patterns are allowed. |
| `own.exclude` | `exclude` | Unchanged. |
| `own.repo`, `own.skills_dir` | `repo`, `skills_dir` | Unchanged. |
| — | `branch` | New, optional: the branch `sync` keeps the checkout on; default the default branch of `origin`. `sync` now pulls a checkout only when it is clean and on that branch, see [Checkouts and branches](manifest.md#checkouts-and-branches). |
| `[[environment.vendor]]` | `[user.dependencies.<name>]` | The table key is the skill name; `name` is gone. |
| `vendor.path` | `skill_dir` | Default `"."`. |
| `vendor.rev` | `commit` | Still a full 40-character SHA. |
| `[environment.host."<name>"]` | `[user.machines."<name>"]` | See [machine rules](manifest.md#machine-rules) for how the name is chosen. |
| `host.<name>.skip` | `machines.<name>.exclude` | `include` and `checkout_dirs` are new. |
| `[environment.hosts.<alias>]` | `[user.git_hosts.<alias>]` | |
| `hosts.<alias>.url` | `git_hosts.<alias>.base_url` | The scheme is the transport: `ssh://git@host` clones over ssh. |
| `hosts.<alias>.type` | `git_hosts.<alias>.provider` | Same values. |
| `hosts.<alias>.ssh` | removed | Set `base_url` to the ssh address to clone over ssh. |
| `environment.layout.store` | `user.storage.dir` | Same default, `~/.agents/skills`. |
| `environment.layout.targets` | `[user.agents]` | `enabled` (`"claude"`, `"pi"`), `paths.<agent>`, `extra_dirs`. `targets` replaced the whole agent table; list the agents in `enabled` and other directories in `extra_dirs`. |
| `environment.layout.ignore` | `user.unmanaged` | |
| `[repo]` | `[repository]` | |
| `repo.harness` | `repository.template_version` | The version the repository asks for; `skenv repo apply` no longer changes it, [`skenv repo upgrade`](harness.md#skenv-repo-upgrade) does. |
| `repo.visibility` | `repository.visibility` | Unchanged values; a declared policy. |
| `repo.ci = "github"` | `[repository.ci.github]` | The table present selects the CI system; neither means GitHub Actions. |
| `repo.ci = "gitlab"` | `[repository.ci.gitlab]` | |
| `repo.runner` | `repository.ci.github.runs_on` or `repository.ci.gitlab.tags` | In the table of the CI system. |
| `[[project.vendor]]` | `[project.dependencies.<name>]` | With `skill_dir` and `commit`, as in `[user]`. |
| `[project.hosts.<alias>]` | `[project.git_hosts.<alias>]` | With `base_url` and `provider`. |
| `[[project.from]]` | `[project.from.<id>]` | A table per repository, keyed by an ID; `rev` is `commit`, `skills` stays a list of names. |
| `project.dir`, `mirrors`, `mirrors_mode` | same | Unchanged. |

Before:

```toml
[repo]
harness    = "0.5.0"
visibility = "private"
ci         = "gitlab"
runner     = ["saas-linux-small-amd64"]

[environment.layout]
ignore = ["peon-ping-*"]

[environment.hosts.work]
url  = "https://git.example.com"
type = "gitlab"

[[environment.own]]
repo   = "<owner>/<skills-repo>"
path   = "~/src/<skills-repo>"
skills = ["alpha", "beta"]

[[environment.vendor]]
name = "archify"
repo = "tt-a1i/archify"
path = "archify"
rev  = "<full 40-character commit SHA>"

[environment.host."my-laptop"]
skip = ["beta"]
```

After:

```toml
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.gitlab]
tags = ["saas-linux-small-amd64"]

[user]
unmanaged = ["peon-ping-*"]

[user.git_hosts.work]
base_url = "https://git.example.com"
provider = "gitlab"

[user.checkouts.skills-repo]
repo         = "<owner>/<skills-repo>"
checkout_dir = "."
include      = ["alpha", "beta"]

[user.dependencies.archify]
repo      = "tt-a1i/archify"
skill_dir = "archify"
commit    = "<full 40-character commit SHA>"

[user.machines."my-laptop"]
exclude = ["beta"]
```

`checkout_dir = "."` fits when the skenv file lives in that repository;
otherwise keep the old path (`"~/src/<skills-repo>"`).

In YAML and JSON the same renames apply. The arrays become mappings keyed
by the ID or the skill name: `own:` with `- repo: …` items becomes
`checkouts:` with `<id>:` keys, `vendor:` becomes `dependencies:` with
`<name>:` keys (drop `name:`), and `ci: github` becomes `ci: {github: {}}`
(`"ci": {"github": {}}` in JSON), with `runs_on` or `tags` inside when you
had `runner`.

After rewriting the file:

1. In a skills repository, run `skenv repo upgrade` to move
   `template_version` to the templates of the installed skenv and
   regenerate the managed files; `skenv repo check` then passes.
2. Run `skenv sync --dry-run`, then `skenv sync`, and `skenv doctor`.

skenv does not read `env.toml` either, the manifest file before skenv
0.4: move its tables into `[user]` of `skenv.toml` with the same table,
and point the tool config at the new file with `skenv use <checkout>`.

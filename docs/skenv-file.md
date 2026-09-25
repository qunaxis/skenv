# The skenv file

Every repository skenv works with has one skenv file at its root:
`skenv.toml`, or `skenv.yaml`, `skenv.yml` or `skenv.json` if you prefer
those formats. More than one of them in the same directory is an error.

- [Sections](#sections)
- [Formats and editing](#formats-and-editing)
- [Creating the file](#creating-the-file)
- [Editor support](#editor-support)
- [Where skenv looks](#where-skenv-looks)
- [Moving from `env.toml` and the old `skenv.toml`](#moving-from-envtoml-and-the-old-skenvtoml)

## Sections

The file has three optional top-level sections, independent of each
other. Nothing else is allowed at
the top level (except `$schema`, the schema URL for editors, which skenv
ignores), and unknown keys inside a section are errors.

| Section         | What it is                                                                                            | Who writes it                              | Reference                     |
| --------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------ | ----------------------------- |
| `[repo]`        | The harness of a skills repository: `harness`, `visibility`, `ci`, `runner`.                          | [`skenv repo init`](harness.md#skenv-repo-init), [`skenv repo apply`](harness.md#skenv-repo-apply) | [harness](harness.md)         |
| `[environment]` | The manifest of your machines: `layout`, `hosts`, `own`, `vendor`, `host`.                            | you, [`skenv init`](#creating-the-file) (starts it), [`skenv import`](adopting.md), [`skenv vendor add`](commands.md#skenv-vendor-add), [`update`](commands.md#skenv-vendor-update), [`remove`](commands.md#skenv-vendor-remove) | [manifest](manifest.md)       |
| `[project]`     | The skills a project repository carries: `dir`, `mirrors`, `mirrors_mode`, `hosts`, `vendor`, `from`.| you, [`skenv vendor add`, `update`, `remove`](project-skills.md#commands) with `--project` | [project skills](project-skills.md) |

- A skills repository has `[repo]`.
- The repository that holds your manifest has `[environment]`, and usually
  `[repo]` too, because it is also where your own skills live.
- `repo` in `[environment]` takes `owner/repo` on GitHub, `gitlab:` and
  `codeberg:` short forms, aliases of self-hosted servers declared under
  `[environment.hosts.<alias>]`, and any git URL; see [Git hosts](git-hosts.md).
- A project repository has `[project]`; a skills repository whose own
  development uses project skills has `[repo]` and `[project]`. The
  user-level `skenv sync` never reads `[project]`.
- A **public** repository must not have `[environment]`. The manifest is
  personal: it names paths in your home directory, your host names and the
  skills you use. `skenv repo init --visibility public` refuses such a file
  and `skenv repo check` fails on it.

A private skills repository that also holds the manifest:

```toml
[repo]
harness    = "0.5.0"
visibility = "private"
ci         = "github"
runner     = ["ubuntu-latest"]

[environment.layout]
ignore = ["peon-ping-*"]

[[environment.own]]
repo = "<owner>/<skills-repo>"
path = "~/src/<skills-repo>"

[[environment.vendor]]
name = "archify"
repo = "tt-a1i/archify"
path = "archify"
rev  = "<full 40-character commit SHA>"

[environment.host."my-laptop"]
skip = ["bpmn-process-modeler"]
```

The same in YAML (`skenv.yaml`):

```yaml
repo:
  harness: 0.5.0
  visibility: private
  ci: github
  runner: [ubuntu-latest]
environment:
  layout:
    ignore: ["peon-ping-*"]
  own:
    - repo: <owner>/<skills-repo>
      path: ~/src/<skills-repo>
  vendor:
    - name: archify
      repo: tt-a1i/archify
      path: archify
      rev: "<full 40-character commit SHA>"
  host:
    my-laptop:
      skip: [bpmn-process-modeler]
```

## Formats and editing

TOML, YAML and JSON are read the same way; keys are lowercase and
case-sensitive in every format. skenv edits the file in these places:

- `skenv vendor add|update|remove` change `environment.vendor`, and with
  `--project` `project.vendor` (and the `rev` of `project.from`);
- `skenv repo init` adds `[repo]` (it creates the file when the repository
  has none, see [Creating the file](#creating-the-file));
- `skenv init` adds `[environment]` (it creates the
  file too);
- `skenv import` adds `environment.vendor` and `environment.own` entries for
  the skills already installed on the machine (and `[environment]` itself
  when the file has none); `skenv import --project` adds `project.vendor`
  entries (and `[project]`, or `skenv.toml`, when missing);
- `skenv repo apply` sets `repo.harness` when it moves the repository to the
  templates of the installed skenv.

No edit changes the format of the file: `skenv.yaml` stays YAML under the
same name, `skenv.yml` stays `skenv.yml`, and so on.

Each edit changes only what it has to, in every format: comments, blank
lines, key order and the formatting of everything else stay. New entries
come in the order the docs use (`name`, `repo`, `path`, `rev` for a vendor;
`harness`, `visibility`, `ci`, `runner` for `[repo]`). In YAML, `[repo]` goes to
the top of the file and a vendor entry follows the existing ones. JSON is
written with two-space indentation, keeps its key order and `"$schema"`,
and leaves `<`, `>` and `&` as they are.

skenv can only edit a YAML list or mapping written in block style (one
`- ` item or `key:` per line); an empty `[]` or `{}` is fine. If
`environment.vendor` is written in flow style (`[{name: …}]`), the edit
stops with an error that says so, and the file is left as it is.

## Creating the file

Two commands create a skenv file when the repository has none:
`skenv repo init` with `[repo]`, and `skenv init`
with `[environment]`. Both write `skenv.toml` unless you pass `--format`:

```sh
skenv repo init --visibility private --format yaml   # skenv.yaml with [repo]
skenv init --format json                             # skenv.json with [environment]
```

`--format` takes `toml` (the default), `yaml` or `json`. skenv reads
`skenv.yml` but never creates it. A new TOML or YAML file starts with a
comment that says what the section is for, and every new file names its
schema (see [Editor support](#editor-support)). A new `[environment]` is a
commented skeleton: your repository becomes its first `[[environment.own]]`
entry when its `origin` is on a network host (GitHub, GitLab and Codeberg in
their short forms, see [Git hosts](git-hosts.md#starting-a-manifest-with-skenv-init)),
and `[[environment.vendor]]` is shown as a commented example in TOML.

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

It refuses when the file has `[environment]` already (`skenv use .` records
that one), or when its `[repo]` says `visibility = "public"`. Then it
records the file as `manifest` in the [tool config](configuration.md), the
same way `skenv use` does, and prints the next steps. It doesn't sync. The new manifest is not
necessarily empty: when the repository's `origin` is on a network host, the
repository itself is already its first `[[environment.own]]` entry, so the
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

## Moving from `env.toml` and the old `skenv.toml`

skenv 0.4 no longer reads `env.toml`, or a `skenv.toml` with `harness`,
`visibility` and `runner` at the top level. It stops with an error naming
the file. To move a repository by hand:

1. Create `skenv.toml` with a `[repo]` section holding the old top-level
   `harness`, `visibility` and `runner` lines (skip this for a repository
   without a harness).
2. Append the content of `env.toml`, renaming its tables:

   | `env.toml`         | `skenv.toml`                   |
   | ------------------ | ------------------------------ |
   | `[layout]`         | `[environment.layout]`         |
   | `[[own]]`          | `[[environment.own]]`          |
   | `[[vendor]]`       | `[[environment.vendor]]`       |
   | `[host."<name>"]`  | `[environment.host."<name>"]`  |

   Comments and key order are kept as they are. Keys that `env.toml` had
   before its first table (for example `layout.store = "…"`) must go under
   `[environment]` too: put them before `[repo]` as `environment.layout.store`,
   or into the renamed table.
3. Delete `env.toml` and commit.
4. Run `skenv repo apply` to move the harness to the templates of the installed skenv, and check the
   result with `skenv repo check` and `skenv doctor`.
5. If `~/.config/skenv/config.toml` records `manifest = ".../env.toml"`,
   point it at the new file or its directory with `skenv use <checkout>`.

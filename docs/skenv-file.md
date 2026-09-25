# The skenv file

Every repository skenv works with has one skenv file at its root:
`skenv.toml`, or `skenv.yaml`, `skenv.yml` or `skenv.json` if you prefer
those formats. More than one of them in the same directory is an error.

- [Sections](#sections)
- [Formats and editing](#formats-and-editing)
- [Where skenv looks](#where-skenv-looks)
- [Moving from `env.toml` and the old `skenv.toml`](#moving-from-envtoml-and-the-old-skenvtoml)

## Sections

The file has two optional top-level sections. Nothing else is allowed at
the top level, and unknown keys inside a section are errors.

| Section         | What it is                                                                                            | Who writes it                              | Reference                     |
| --------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------ | ----------------------------- |
| `[repo]`        | The harness of a skills repository: `harness`, `visibility`, `runner`.                                | `skenv repo init`, `skenv repo apply`      | [harness](harness.md)         |
| `[environment]` | The manifest of your machines: `layout`, `own`, `vendor`, `host`.                                     | you, `skenv vendor add\|bump\|remove`      | [manifest](manifest.md)       |

- A skills repository has `[repo]`.
- The repository that holds your manifest has `[environment]`, and usually
  `[repo]` too, because it is also where your own skills live.
- A **public** repository must not have `[environment]`. The manifest is
  personal: it names paths in your home directory, your host names and the
  skills you use. `skenv repo init --visibility public` refuses such a file
  and `skenv repo check` fails on it.

A private skills repository that also holds the manifest:

```toml
[repo]
harness    = "0.4.0"
visibility = "private"
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
  harness: 0.4.0
  visibility: private
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
case-sensitive in every format. skenv edits the file in three places:

- `skenv vendor add|bump|remove` change `environment.vendor`;
- `skenv repo init` adds `[repo]` (it creates `skenv.toml` when the
  repository has no skenv file);
- `skenv repo apply` sets `repo.harness` when it moves the repository to the
  templates of the installed skenv.

In `skenv.toml` these edits change only the affected lines, so comments,
order and formatting stay. A YAML or JSON file is decoded, changed and
encoded again: YAML comments are lost and keys come out sorted. Prefer TOML
for a file you annotate.

## Where skenv looks

- The manifest: `--manifest`, then `$SKENV_MANIFEST`, then `manifest` in the
  tool config `~/.config/skenv/config.toml` (written by `skenv init`). Each
  names the skenv file or the directory that holds it.
- The harness: the skenv file at the root of the repository (`--dir`,
  default: the current repository).

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
4. Run `skenv repo apply` to move the harness to 0.4.0, and check the
   result with `skenv repo check` and `skenv doctor`.
5. If `~/.config/skenv/config.toml` records `manifest = ".../env.toml"`,
   point it at the new file or its directory, or rerun
   `skenv init <owner>/<skills-repo> --path <checkout>`.

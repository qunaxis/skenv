# Manifest: `[environment]`

The manifest describes which skills a machine should have. It lives in your
skills repository, next to your own skills, so every machine that runs
`skenv sync` converges on the same set.

- [Where the manifest is found](#where-the-manifest-is-found)
- [Format](#format)
- [Rules](#rules)
- [Selecting skills of an own repository](#selecting-skills-of-an-own-repository)
- [Layout on disk](#layout-on-disk)
- [Mapping to `skills-lock.json`](#mapping-to-skills-lockjson)

## Where the manifest is found

Found via `--manifest`, then `$SKENV_MANIFEST`, then `manifest` in the skenv
config file (written by `skenv init`). Each names the skenv file or the
directory that holds it. The config file is
`~/.config/skenv/config.toml`, or `config.yaml`, `config.yml` or `config.json`
if you prefer; only one of them may exist. Every setting follows the same
order: flag, `SKENV_<KEY>` environment variable, config file, default. There is no default
location: when none of them is set, commands that need the manifest stop
with an error that suggests `skenv init <owner/repo>` or `--manifest`.

## Format

The manifest is the `[environment]` section of the skenv file at the root
of your skills repository (`skenv.toml`, or `skenv.yaml`, `skenv.yml`,
`skenv.json`; see [the skenv file](skenv-file.md)). In TOML:

```toml
[environment.layout]
store   = "~/.agents/skills"                          # optional, this is the default
targets = ["~/.claude/skills", "~/.pi/agent/skills"]  # optional, replaces the agent table
ignore  = ["peon-ping-*"]                            # optional, entries owned by other tools

[[environment.own]]            # your skills repository, kept as a working copy
repo = "<owner>/<skills-repo>" # any name
path = "~/src/my-skills"       # any location
skills_dir = "skills"          # optional, default "skills"

[[environment.own]]            # a shared repository: only some of its skills
repo = "<team>/<shared-skills>"
path = "~/src/shared-skills"
skills  = ["alpha", "beta"]    # optional allowlist; default: every skill
exclude = ["experimental-*"]   # optional globs, applied after skills

[[environment.vendor]]         # someone else's skill, pinned to a commit
name = "archify"
repo = "tt-a1i/archify"         # owner/repo on github.com or a full git URL
path = "archify"               # directory with SKILL.md; "." for the root
rev  = "<full 40-character commit SHA>"

[environment.host."my-laptop"] # optional, per hostname (full or short)
skip = ["bpmn-process-modeler"]
```

> [!IMPORTANT]
> `own.path` must be where the repository is actually cloned (for example
> where `skenv init` put it). Otherwise `sync` clones a second working
> copy at `path` and links the skills from that copy.

## Rules

> [!IMPORTANT]
> `rev` must be a full 40-character commit SHA; branches, tags and short
> SHAs are rejected. A vendored skill never follows a branch: it changes
> only when you run `skenv vendor bump` (or edit `rev`) and commit the
> manifest.

- Skill names (a vendor `name`, a skill directory in an own repository)
  follow the [Agent Skills](https://agentskills.io/specification) rule:
  1 to 64 lowercase letters, digits and single hyphens, with no hyphen at
  the start or end (`^[a-z0-9]+(-[a-z0-9]+)*$`). `synced` is reserved.
  `skenv lint` and `skenv new` apply the same rule.
- Skill names are unique across own and vendor skills; a clash is an error.
- `owner/repo` is cloned from `https://github.com/owner/repo.git`. To use ssh,
  map it in git: `git config --global url."git@github.com:".insteadOf https://github.com/`.
- Skills listed in `host.<name>.skip` are neither stored nor linked on that host.
- `layout.ignore` holds glob patterns over entry names in the store and the
  agent directories that belong to other tools (for example the skills of
  the `peon-ping` Homebrew package). `doctor` does not report them as
  `unmanaged`, and `sync`/`link` never touch them, not even with
  `--adopt`. A manifest skill whose name matches a pattern is an error.
- `vendor add|bump|remove` edit the skenv file in place, keeping comments
  and order in every format. In TOML, keep `[[environment.vendor]]` tables
  in the multi-line form above with double-quoted `name` and `rev`; in
  YAML, write `vendor` as a block list (see
  [formats and editing](skenv-file.md#formats-and-editing)).
- A vendored skill is copied as is, symlinks included; vendor only
  repositories you trust.

## Selecting skills of an own repository

By default every skill of an own repository is installed: every directory
under `<path>/<skills_dir>/` that holds a `SKILL.md`, including ones added
later. Two optional keys of `[[environment.own]]` narrow that down on every
machine:

- `skills` lists the skills to install. A skill added to the repository
  later is not installed until you list it. A name that is not a skill of
  the repository is an error in `sync` and `doctor` that names the entry,
  so a typo never goes unnoticed. `skills = []` is an error too: to
  install nothing, remove the entry or comment it out.
- `exclude` lists glob patterns over skill names, applied after `skills`.
  A pattern that matches nothing is fine.

`[environment.host."<name>"] skip` applies after both, per machine. So a
skill is installed on a host when it is in `skills` (or `skills` is not
set), matches no `exclude` pattern, and is not in that host's `skip`.

```toml
[[environment.own]]
repo    = "<team>/<shared-skills>"
path    = "~/src/shared-skills"
skills  = ["alpha", "beta", "gamma"]
exclude = ["*-draft"]

[environment.host."my-laptop"]
skip = ["gamma"]
```

- **Leaving the selection.** A skill that you remove from `skills` or match
  with `exclude` is unlinked by the next `sync`, like any skill that left
  the manifest: skenv removes only the store and agent links it created.
  The skill stays in the repository, and paths skenv did not create are
  never touched. `skenv sync --dry-run` shows the removals first, and
  `skenv doctor` reports the leftover links as `extra-managed` with the
  detail "not selected by own `<repo>`".
- **Name clashes.** Skill names must be unique among the skills the
  manifest selects: two own repositories may each hold a skill with the
  same name as long as only one of them selects it. The check does not
  depend on the host, so `host.<name>.skip` cannot resolve a clash and a
  manifest is valid or not on every machine alike.
- **`skenv new`** in a repository that selects its skills warns that the
  new skill is not installed until you add it to `skills`.
- **Per-agent selection** (a skill only for Claude Code, say) is not
  supported: every selected skill is linked into every agent directory.
  `layout.targets` chooses the agents for all skills at once.

## Layout on disk

- **Store** `~/.agents/skills`: own skills are symlinks to
  `<own.path>/<skills_dir>/<name>`; vendored skills are directories with a
  `.skenv` marker (`repo`, `path`, `rev`). Codex reads this directory
  directly.
- **Targets**: `<target>/<name>` is a relative symlink to the store entry.
  Built-in agents: Claude Code (`$CLAUDE_CONFIG_DIR/skills`, else
  `~/.claude/skills`) and pi (`~/.pi/agent/skills`), each only if its base
  directory (`~/.claude`, `~/.pi/agent`) exists. `layout.targets` replaces
  this table. `~/.claude/skills/synced` is never touched.
> [!NOTE]
> Codex has no target of its own: it reads the store `~/.agents/skills`
> directly. Claude Code and pi are linked only when their base directory
> exists, so install the agent (or create the directory) before `sync`.
> `layout.targets` replaces the built-in table entirely.

- **State** `~/.local/state/skenv/state.json` lists the paths skenv created.
  Vendor clones are cached in `~/.cache/skenv/repos/<owner>__<repo>`.
- **Backups** made by `--adopt` go to `~/.local/state/skenv/backup/<timestamp>/`.

## Mapping to `skills-lock.json`

The vendor fields follow the project lock file of the
[`skills` CLI](https://github.com/vercel-labs/skills) (checked against 1.7.0),
so a manifest can be translated if skenv is ever replaced by it:

| `[[environment.vendor]]` | `skills-lock.json` entry             | Notes                                                                         |
| ------------------------ | ------------------------------------ | ----------------------------------------------------------------------------- |
| `name`                   | entry key                            | skill name                                                                    |
| `repo`                   | `source` (+ `sourceType = "github"`) | `owner/repo`; a full URL maps to `source`/`sourceUrl`                         |
| `path`                   | `skillPath`                          | skills-lock stores the file: `archify` ↔ `archify/SKILL.md`, `.` ↔ `SKILL.md` |
| `rev`                    | `ref`                                | skenv requires a full SHA; `ref` also accepts branches and tags               |
| —                        | `computedHash`                       | not recorded by skenv; the SHA pins the content                               |

`[[environment.own]]` has no equivalent: the `skills` CLI does not manage
working copies. A translation would list each skill an own entry selects
(after `skills` and `exclude`) as an entry of its own, pinned to the commit
of the working copy.

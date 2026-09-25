# Manifest: `env.toml`

`env.toml` describes which skills a machine should have. It lives in your
skills repository, next to your own skills, so every machine that runs
`skenv sync` converges on the same set.

- [Where the manifest is found](#where-the-manifest-is-found)
- [Format](#format)
- [Rules](#rules)
- [Layout on disk](#layout-on-disk)
- [Mapping to `skills-lock.json`](#mapping-to-skills-lockjson)

## Where the manifest is found

Found via `--manifest`, then `$SKENV_MANIFEST`, then `manifest` in
`~/.config/skenv/config.toml` (written by `skenv init`). There is no default
location: when none of them is set, commands that need the manifest stop
with an error that suggests `skenv init <owner/repo>` or `--manifest`.

## Format

```toml
[layout]
store   = "~/.agents/skills"                          # optional, this is the default
targets = ["~/.claude/skills", "~/.pi/agent/skills"]  # optional, replaces the agent table
ignore  = ["peon-ping-*"]                            # optional, entries owned by other tools

[[own]]                        # your skills repository, kept as a working copy
repo = "<owner>/<skills-repo>" # any name
path = "~/src/my-skills"       # any location
skills_dir = "skills"          # optional, default "skills"

[[vendor]]                     # someone else's skill, pinned to a commit
name = "archify"
repo = "tt-a1i/archify"         # owner/repo on github.com or a full git URL
path = "archify"               # directory with SKILL.md; "." for the root
rev  = "<full 40-character commit SHA>"

[host."my-laptop"]             # optional, per hostname (full or short)
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

- Skill names are unique across own and vendor skills; a clash is an error.
- `owner/repo` is cloned from `https://github.com/owner/repo.git`. To use ssh,
  map it in git: `git config --global url."git@github.com:".insteadOf https://github.com/`.
- Skills listed in `host.<name>.skip` are neither stored nor linked on that host.
- `layout.ignore` holds glob patterns over entry names in the store and the
  agent directories that belong to other tools (for example the skills of
  the `peon-ping` Homebrew package). `doctor` does not report them as
  `unmanaged`, and `sync`/`link` never touch them, not even with
  `--adopt`. A manifest skill whose name matches a pattern is an error.
- `vendor add|bump|remove` edit the file as text: keep `[[vendor]]` tables in
  the multi-line form above with double-quoted `name` and `rev`.
- A vendored skill is copied as is, symlinks included; vendor only
  repositories you trust.

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

| `env.toml` `[[vendor]]` | `skills-lock.json` entry             | Notes                                                                         |
| ----------------------- | ------------------------------------ | ----------------------------------------------------------------------------- |
| `name`                  | entry key                            | skill name                                                                    |
| `repo`                  | `source` (+ `sourceType = "github"`) | `owner/repo`; a full URL maps to `source`/`sourceUrl`                         |
| `path`                  | `skillPath`                          | skills-lock stores the file: `archify` ↔ `archify/SKILL.md`, `.` ↔ `SKILL.md` |
| `rev`                   | `ref`                                | skenv requires a full SHA; `ref` also accepts branches and tags               |
| —                       | `computedHash`                       | not recorded by skenv; the SHA pins the content                               |

`[[own]]` has no equivalent: the `skills` CLI does not manage working copies.

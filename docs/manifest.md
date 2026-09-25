# Manifest: `[environment]`

The manifest describes which skills a machine should have. It lives in your
skills repository, next to your own skills, so every machine that runs
`skenv sync` converges on the same set.

- [Where the manifest is found](#where-the-manifest-is-found)
- [Format](#format)
- [Rules](#rules)
- [Git hosts](git-hosts.md): GitLab, Codeberg and self-hosted servers
- [Selecting skills of an own repository](#selecting-skills-of-an-own-repository)
- [Layout on disk](#layout-on-disk)
- [How vendoring works](#how-vendoring-works)
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
`skenv.json`; see [the skenv file](skenv-file.md)). `skenv init` without
`<owner/repo>` starts one in the current repository (see
[Creating the file](skenv-file.md#creating-the-file)). In TOML:

```toml
[environment.layout]
store   = "~/.agents/skills"                          # optional, this is the default
targets = ["~/.claude/skills", "~/.pi/agent/skills"]  # optional, replaces the agent table
ignore  = ["peon-ping-*"]                            # optional, entries owned by other tools

[environment.hosts.work]       # optional, a self-hosted server (see Git hosts)
url  = "https://git.example.com"
type = "gitlab"                # github | gitlab | gitea | generic

[[environment.own]]            # your skills repository, kept as a working copy
repo = "<owner>/<skills-repo>" # any name
path = "~/src/my-skills"       # any location
skills_dir = "skills"          # optional, default "skills"

[[environment.own]]            # a shared repository: only some of its skills
repo = "work:platform/shared-skills" # on the host declared as "work"
path = "~/src/shared-skills"
skills  = ["alpha", "beta"]    # optional allowlist; default: every skill
exclude = ["experimental-*"]   # optional globs, applied after skills

[[environment.vendor]]         # someone else's skill, pinned to a commit
name = "archify"
repo = "tt-a1i/archify"        # owner/repo on github.com, gitlab:, codeberg:, an alias or a URL
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
> only when you run `skenv vendor update` (or edit `rev`) and commit the
> manifest.

- Skill names (a vendor `name`, a skill directory in an own repository)
  follow the [Agent Skills](https://agentskills.io/specification) rule:
  1 to 64 lowercase letters, digits and single hyphens, with no hyphen at
  the start or end (`^[a-z0-9]+(-[a-z0-9]+)*$`). `synced` is reserved.
  `skenv lint` and `skenv new` apply the same rule.
- Skill names are unique across own and vendor skills; a clash is an error.
- `repo` is `owner/repo` on github.com (cloned from
  `https://github.com/owner/repo.git`), `gitlab:group/sub/repo`,
  `codeberg:owner/repo`, `<alias>:path` of a host declared under
  `[environment.hosts.<alias>]`, or a full git URL. An unknown prefix is an
  error. Short forms are cloned over https; to use ssh, map the host with
  git's `url.<base>.insteadOf`. Every form, host declarations and
  authentication: [Git hosts](git-hosts.md).
- Skills listed in `host.<name>.skip` are neither stored nor linked on that host.
- `layout.ignore` holds glob patterns over entry names in the store and the
  agent directories that belong to other tools (for example the skills of
  the `peon-ping` Homebrew package). `doctor` does not report them as
  `unmanaged`, and `sync`/`link` never touch them, not even with
  `--adopt`. A manifest skill whose name matches a pattern is an error.
- `vendor add|update|remove` edit the skenv file in place, keeping comments
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
- **`skenv new`** warns when the new skill is not selected: when the
  repository lists its skills in `skills` without it (add it there), or
  when an `exclude` pattern matches it.
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
  Vendor clones are cached in `~/.cache/skenv/repos` (see
  [How vendoring works](#how-vendoring-works)).
- **Backups** made by `--adopt` go to `~/.local/state/skenv/backup/<timestamp>/`.

## How vendoring works

`sync` copies each vendored skill from a local clone of its repository, the
vendor cache, into the store. The copy carries a `.skenv` marker with
`repo`, `path` and `rev`; while the marker matches the manifest, `sync`
leaves the copy alone and does not touch the network.

### Where the cache lives

The vendor cache is `~/.cache/skenv/repos`, one partial clone
(`--filter=blob:none`) per repository. The directory name is derived from
the URL git really fetches from, after
[`url.<base>.insteadOf`](https://git-scm.com/docs/git-config#Documentation/git-config.txt-urlltbasegtinsteadOf)
rewrites in your global git config (`git ls-remote --get-url <repo>`, run in
the cache so the repository you start skenv in does not matter), normalised to the host and the
full path:

- the scheme, credentials, trailing slashes and `.git` are dropped, and the
  host is lowercased; the path keeps its case;
- `git@host:owner/repo` and `ssh://git@host/owner/repo` are the same
  repository; a non-default port stays part of the host;
- local paths and `file://` URLs become absolute paths.

The name is a readable slug of that string plus the first 12 hex digits of
its SHA-256:

| `repo` in the manifest                   | Cache directory                           |
| ---------------------------------------- | ----------------------------------------- |
| `tt-a1i/archify`                         | `github.com-tt-a1i-archify-<hash>`        |
| `git@github.com:tt-a1i/archify.git`      | `github.com-tt-a1i-archify-<hash>` (same) |
| `https://gitlab.com/group/sub/tools.git` | `gitlab.com-group-sub-tools-<hash>`       |

So `github.com/x/skills` and `gitlab.com/x/skills` get separate caches, and
so do the subgroup `gitlab.com/a/x/skills` and `gitlab.com/x/skills`. The hash keeps two
repositories apart even when their slugs look alike, and the flat layout
means one repository never sits inside another's directory. Credentials in
a URL never become part of the name.

### Verification on reuse

Before reusing a cache, skenv checks that `git remote get-url origin` in it
is the repository the manifest asks for, compared after the same
normalisation. On a mismatch (someone ran `git remote set-url`, or the
directory was copied or edited by hand) it prints a notice with credentials
masked and clones again. Every clone goes into a temporary directory next
to the cache first and replaces it only when complete, so an interrupted
clone never leaves a broken cache.

### Deleting the cache

The cache holds nothing that is not upstream, so it is always safe to
delete; the next `sync` or `vendor` command clones what it needs again.
This includes `<owner>__<repo>` directories from skenv 0.4 and earlier,
which are no longer used, and `.skenv-tmp-*` or `.skenv-old-*` directories
left by an interrupted run:

```sh
rm -rf ~/.cache/skenv/repos
```

Store copies are unaffected: they are rebuilt only when their marker no
longer matches the manifest.

### Troubleshooting: `commit … not found in …`

`sync` fetched the repository and the pinned `rev` is not in it. Common
causes:

- `repo` and `rev` do not belong together, for example after `repo` was
  changed to a fork or mirror that lacks the commit, or `rev` was copied
  from another repository. Check whether the commit exists upstream; the
  second command prints `commit` if it does:

  ```sh
  git clone --quiet --filter=blob:none --no-checkout <repo-url> /tmp/skenv-check
  git -C /tmp/skenv-check cat-file -t <rev>
  ```

- The commit was force-pushed away upstream. Set `rev` to a commit that
  still exists and commit the manifest.
- git cannot reach the repository with your credentials or `insteadOf`
  rules; `git ls-remote <repo-url>` shows the same error without skenv.

A stale or damaged cache is not the cause on its own: skenv re-clones a
cache whose origin does not match. To rule it out anyway, delete the cache
(see above) and run `skenv sync` again.

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

The other direction, from the global lock `~/.agents/.skill-lock.json` to
the manifest, is `skenv import`; see
[Adopting an existing setup](adopting.md#what-import-reads).

`[[environment.own]]` has no equivalent: the `skills` CLI does not manage
working copies. A translation would list each skill an own entry selects
(after `skills` and `exclude`) as an entry of its own, pinned to the commit
of the working copy.

# Manifest: `[user]`

The manifest describes which skills the agents of your OS user should
have, across projects. It lives in your skills repository, next to your own
skills, so every machine that runs `skenv sync` converges on the same set.
Skills that belong to one project and should reach everyone who clones it
are [project skills](project-skills.md) instead.

- [Where the manifest is found](#where-the-manifest-is-found)
- [Format](#format)
- [Rules](#rules)
- [Git hosts](git-hosts.md): GitLab, Codeberg and self-hosted servers
- [Checkouts and branches](#checkouts-and-branches)
- [Selecting skills](#selecting-skills)
- [Machine rules](#machine-rules)
- [Agents and storage](#agents-and-storage)
- [How vendoring works](#how-vendoring-works)
- [Mapping to `skills-lock.json`](#mapping-to-skills-lockjson)

To see how the manifest applies on this machine (which machine rules,
agents and checkout directories are in effect, and why each skill is
installed or not), run [`skenv config show`](configuration.md#skenv-config-show).

## Where the manifest is found

Found via `--manifest`, then `$SKENV_MANIFEST`, then `manifest` in the skenv
config file (written by `skenv init`, `skenv clone` or `skenv use`). Each
names the skenv file or the directory that holds it. The config file is
`~/.config/skenv/config.toml`, or `config.yaml`, `config.yml` or `config.json`
if you prefer; only one of them may exist. Every setting follows the same
order: flag, `SKENV_<KEY>` environment variable, config file, default. There is no default
location: when none of them is set, commands that need the manifest stop
with an error that suggests `skenv init`, `skenv clone <repo>`,
`skenv use <path>` or `--manifest` (`skenv use .` inside a repository that
holds a manifest).

## Format

The manifest is the `[user]` section of the skenv file at the root of your
skills repository (`skenv.toml`, or `skenv.yaml`, `skenv.yml`,
`skenv.json`; see [the skenv file](skenv-file.md)). `skenv init` starts
one in the current repository (see
[Creating the file](skenv-file.md#creating-the-file)). In TOML:

```toml
[user]
unmanaged = ["peon-ping-*"]      # optional, entries owned by other tools

[user.checkouts.my-skills]       # your skills repository, kept as a working copy
repo         = "<owner>/<skills-repo>"
checkout_dir = "."               # the repository that holds this file
skills_dir   = "skills"          # optional, default "skills"

[user.checkouts.team]            # a shared repository: only some of its skills
repo         = "work:platform/shared-skills" # on the git host declared as "work"
checkout_dir = "~/src/shared-skills"
branch       = "main"            # optional, default: the default branch of origin
include      = ["alpha", "beta"] # optional, default: every skill
exclude      = ["experimental-*"] # optional, applied after include

[user.dependencies.archify]      # someone else's skill, pinned to a commit
repo      = "tt-a1i/archify"      # owner/repo on github.com, github:, gitlab:, codeberg:, an alias or a URL
skill_dir = "archify"            # directory with SKILL.md; default "." (the root)
commit    = "<full 40-character commit SHA>"

[user.git_hosts.work]            # optional, a self-hosted server (see Git hosts)
base_url = "https://git.example.com"
provider = "gitlab"              # github | gitlab | gitea | generic

[user.agents]                    # optional, see Agents and storage
enabled = ["claude"]             # default: every built-in agent that is installed

[user.storage]
dir = "~/.agents/skills"         # optional, this is the default

[user.machines."my-laptop"]      # optional, rules for one machine
exclude = ["bpmn-process-modeler"]
```

The keys of a checkout:

| Key            | Default                        | Meaning |
| -------------- | ------------------------------ | ------- |
| `repo`         | required                       | The repository: `owner/repo`, a prefixed short form, an alias of `git_hosts` or a git URL. |
| `checkout_dir` | required                       | Where the working copy lives. |
| `skills_dir`   | `"skills"`                     | The directory in the repository whose subdirectories with a `SKILL.md` are the skills. |
| `branch`       | the default branch of `origin` | The branch `sync` clones and keeps the working copy on; see [Checkouts and branches](#checkouts-and-branches). |
| `include`      | every skill                    | Skill names and patterns to install; see [Selecting skills](#selecting-skills). |
| `exclude`      | none                           | Skill names and patterns not to install. |

Checkout IDs (`my-skills`, `team`) are names of your choice: 1 to 64
lowercase letters, digits, `-` and `_`, starting with a letter or a digit.
Machine rules refer to checkouts by them. The key of a dependency is the
installed skill name.

A relative `checkout_dir` resolves against the directory of the skenv file,
`~/` against your home directory. `checkout_dir = "."` is the repository
that holds the manifest, wherever it is cloned; `skenv init` writes that.
The same holds for the other local paths of `[user]`: `storage.dir`,
`agents.paths`, `agents.extra_dirs`, the `checkout_dirs` of machine rules
and a `repo` that is a local path.

> [!IMPORTANT]
> `checkout_dir` must be where the repository is actually cloned (for
> example where `skenv init` found it). Otherwise `sync` clones a second
> working copy at `checkout_dir` and links the skills from that copy.

## Rules

> [!IMPORTANT]
> `commit` must be a full 40-character commit SHA; branches, tags and short
> SHAs are rejected. A dependency never follows a branch: it changes
> only when you run `skenv vendor update` (or edit `commit`) and commit the
> manifest.

- Skill names (the key of a dependency, a skill directory in a checkout)
  follow the [Agent Skills](https://agentskills.io/specification) rule:
  1 to 64 lowercase letters, digits and single hyphens, with no hyphen at
  the start or end (`^[a-z0-9]+(-[a-z0-9]+)*$`). `synced` is reserved.
  `skenv lint` and `skenv new` apply the same rule.
- Skill names are unique across the skills that checkouts select and the
  dependencies; a clash is an error.
- `repo` is `owner/repo` or `github:owner/repo` on github.com (cloned from
  `https://github.com/owner/repo.git`), `gitlab:group/sub/repo`,
  `codeberg:owner/repo`, `<alias>:path` of a host declared under
  `[user.git_hosts.<alias>]`, or a full git URL. An unknown prefix is an
  error. Built-in prefixes are cloned over https; a declared host is cloned
  over the scheme of its `base_url`. Every form, host declarations and
  authentication: [Git hosts](git-hosts.md).
- `unmanaged` holds glob patterns over entry names in the store and the
  agent directories that belong to other tools (for example the skills of
  the `peon-ping` Homebrew package). `doctor` does not report them as
  `unmanaged`, and `sync`/`link` never touch them, not even with
  `--adopt`. A selected skill whose name matches a pattern is an error.
- `vendor add|update|remove` and `import` edit the skenv file in place,
  keeping comments and order in every format. `sync` never edits it. In
  TOML, keep each `[user.dependencies.<name>]` table in the multi-line form
  above (see [formats and editing](skenv-file.md#formats-and-editing)).
- A dependency is copied as is, symlinks included; vendor only
  repositories you trust.

## Checkouts and branches

A checkout is an editable working copy: `sync` keeps it on a branch, but
never takes your local work away. For each checkout it:

- clones the repository into `checkout_dir` when nothing is there, with
  `--branch` when `branch` is set;
- fast-forwards it from `origin/<branch>` (`git pull --ff-only`) only when
  the working copy is clean and on that branch. The branch is `branch`, or
  without it the default branch of `origin` (`origin/HEAD`, else asked
  from the remote);
- never resets, switches branches, stashes or clones again.

Whatever it cannot bring to the declared state is printed as an
`unresolved:` line, and the summary counts it
(`sync: 2 changes, 1 unresolved, 0 warnings, 0 errors`):

| The working copy | Its skills | Exit code |
| ---------------- | ---------- | --------- |
| on another branch, or a detached HEAD | linked as checked out, not updated | 0 |
| has uncommitted changes | linked as checked out, not updated | 0 |
| has diverged from `origin/<branch>` (or origin is unreachable) | linked as checked out, not updated | 0 |
| the default branch of `origin` is unknown (offline, no `branch`) | linked as checked out, not updated | 0 |
| is not a working copy of `repo`: another origin, no origin, or no git at all | **not linked**, and no link is pruned in that run | 1 |

The first four are local development state: editing skills in a checkout
is its purpose, so `sync` leaves them to you, for example:

```text
unresolved: ~/src/my-skills has uncommitted changes: local development state, not updated (commit or stash, then rerun sync)
```

The last one is an error: the directory is not what the manifest
declares. The origin matches when it is the same repository as `repo`,
written the same or after git's `url.<base>.insteadOf` rewrites. `sync`
changes nothing in it and keeps the links it made earlier, so they are not
mistaken for stale ones. Fix `checkout_dir` or `repo` (or the machine's
`checkout_dirs`), then run `sync` again.

A branch set in the manifest never makes `sync` switch: switching is your
action (`git switch <branch>`). `skenv doctor` reports a checkout on
another branch as `wrong-branch` and a directory of another repository as
`wrong-origin`; `skenv list` names the latter as `not used`. Only
dependencies are reproducible from the file alone: checkouts follow their
branch and your local edits.

## Selecting skills

By default every skill of a checkout is installed: every directory under
`<checkout_dir>/<skills_dir>/` that holds a `SKILL.md`, including ones
added later. Two optional keys of `[user.checkouts.<id>]` narrow that down
on every machine. Both take skill names and glob patterns over names (no
`/`):

- `include` selects the skills to install. Omitted, it selects every
  skill. With a list, a skill added to the repository later is installed
  only if a name or a pattern of the list matches it. `include = []`
  selects nothing; the checkout stays in the manifest and installs no
  skill. A literal name that is not a skill of the checkout is an error in
  `sync` and `doctor` that names the checkout, so a typo never goes
  unnoticed; a pattern that matches nothing is fine.
- `exclude` removes skills from what `include` selected; exclude wins.
  Omitted, it removes nothing. A pattern that matches nothing is fine.

[Machine rules](#machine-rules) apply after both. So a skill is installed
on a machine when its checkout selects it (`include`, then `exclude`) and
the rules of that machine keep it.

```toml
[user.checkouts.team]
repo         = "<team>/<shared-skills>"
checkout_dir = "~/src/shared-skills"
include      = ["alpha", "beta", "review-*"]
exclude      = ["*-draft"]

[user.machines."my-laptop"]
exclude = ["beta"]
```

- **Leaving the selection.** A skill that `include` no longer selects or
  that `exclude` matches is unlinked by the next `sync`, like any skill
  that left the manifest: skenv removes only the store and agent links it
  created. The skill stays in the checkout, and paths skenv did not create
  are never touched. `skenv sync --dry-run` shows the removals first, and
  `skenv doctor` reports the leftover links as `extra-managed` with the
  detail "not selected by checkout `<id>`".
- **Name clashes.** Skill names must be unique among the skills the
  manifest selects: two checkouts may each hold a skill with the same name
  as long as only one of them selects it. The check does not depend on the
  machine, so machine rules cannot resolve a clash and a manifest is valid
  or not on every machine alike.
- **`skenv new`** says when the new skill is not selected: when the
  checkout's `include` does not match it (add it there), or when an
  `exclude` pattern matches it.
- **Per-agent selection** (a skill only for Claude Code, say) is not
  supported: every selected skill is linked into every agent directory.
  `[user.agents]` chooses the agents for all skills at once.

## Machine rules

`[user.machines.<name>]` holds the rules of one machine:

```toml
[user.checkouts.team]
repo         = "work:platform/shared-skills"
checkout_dir = "~/src/shared-skills"

[user.git_hosts.work]
base_url = "ssh://git@git.example.com"
provider = "gitlab"

[user.machines."work-laptop"]
include = ["deploy-*", "review"]  # optional: only these skills here
exclude = ["review-legacy"]       # optional: not these

[user.machines."work-laptop".checkout_dirs]
team = "~/work/shared-skills"     # checkout ID = where it is on this machine
```

- `include` and `exclude` work as in a checkout, over the skills of the
  whole manifest (checkouts and dependencies), and they only narrow the
  selection: a machine rule cannot install a skill that its checkout
  excludes. Omitted `include` keeps every selected skill, `include = []`
  keeps none.
- `checkout_dirs` maps a checkout ID to the location of that checkout on
  this machine, in place of its `checkout_dir`. A key that is not a
  checkout ID is an error.
- A skill that a machine rule leaves out is neither stored nor linked on
  that machine; `skenv list` shows it as `excluded on this machine`.

The machine name is, first match wins:

1. `$SKENV_MACHINE`, else `machine` in the [tool config](configuration.md).
   The manifest must then have a `[user.machines.<name>]` entry (it may be
   empty); otherwise commands stop with an error that lists the names the
   manifest has.
2. The full hostname, if the manifest has rules for it.
3. The short hostname (the part before the first dot).

The rules of the full and the short hostname are never merged. A hostname
without rules is fine: nothing is narrowed.

## Agents and storage

- **Store** `[user.storage] dir`, default `~/.agents/skills`: skills of a
  checkout are symlinks to `<checkout_dir>/<skills_dir>/<name>`;
  dependencies are directories with a `.skenv` marker (`repo`, `path`,
  `rev`). Codex and other agents that follow the `~/.agents/skills`
  convention read the default store directly, so there is no `codex`
  agent to enable.
- **Agents**: `<agent dir>/<name>` is a relative symlink to the store
  entry. Built-in agents: `claude` (`$CLAUDE_CONFIG_DIR/skills`, else
  `~/.claude/skills`) and `pi` (`~/.pi/agent/skills`).
  `~/.claude/skills/synced` is never touched.

`[user.agents]` chooses the agent directories:

```toml
[user.agents]
enabled    = ["claude", "pi"]       # omitted: detect; []: none
extra_dirs = ["~/.config/other-agent/skills"]  # added, never replacing

[user.agents.paths]
claude = "~/work/.claude/skills"    # this agent only
```

- `enabled` omitted: each built-in agent whose base directory
  (`~/.claude`, `~/.pi/agent`) exists. An explicit list links exactly
  those agents, installed or not; `[]` links none. An unknown name is an
  error; `codex` is rejected with the reason above.
- `paths.<agent>` replaces the directory of that agent only; the others
  keep theirs.
- `extra_dirs` adds more directories that get a link per skill.

> [!NOTE]
> With `enabled` omitted, Claude Code and pi are linked only when their
> base directory exists, so install the agent (or create the directory)
> before `sync`, or list it in `enabled`.

To keep the store out of `~/.agents/skills`, set a neutral store and, to
keep Codex, add the old location as an extra directory:

```toml
[user.storage]
dir = "~/.local/share/skenv/skills"

[user.agents]
extra_dirs = ["~/.agents/skills"]
```

Changing `storage.dir` makes the next `sync` create the new store and
remove only the managed entries of the old one; entries skenv did not
create stay where they are. Disabling an agent or dropping an `extra_dirs`
entry removes only the links skenv recorded there.

- **State** `~/.local/state/skenv/state.json` lists the paths skenv created.
  Dependency clones are cached in `~/.cache/skenv/repos` (see
  [How vendoring works](#how-vendoring-works)).
- **Backups** made by `--adopt` go to `~/.local/state/skenv/backup/<timestamp>/`.

## How vendoring works

`sync` copies each dependency from a local clone of its repository, the
dependency cache, into the store. The copy carries a `.skenv` marker with
`repo`, `path` (the `skill_dir`) and `rev` (the `commit`); while the marker
matches the manifest, `sync` leaves the copy alone and does not touch the
network.

### Where the cache lives

The dependency cache is `~/.cache/skenv/repos`, one partial clone
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

`sync` fetched the repository and the pinned `commit` is not in it. Common
causes:

- `repo` and `commit` do not belong together, for example after `repo` was
  changed to a fork or mirror that lacks the commit, or `commit` was copied
  from another repository. Check whether the commit exists upstream; the
  second command prints `commit` if it does:

  ```sh
  git clone --quiet --filter=blob:none --no-checkout <repo-url> /tmp/skenv-check
  git -C /tmp/skenv-check cat-file -t <commit>
  ```

- The commit was force-pushed away upstream. Set `commit` to one that
  still exists and commit the manifest.
- git cannot reach the repository with your credentials or `insteadOf`
  rules; `git ls-remote <repo-url>` shows the same error without skenv.

A stale or damaged cache is not the cause on its own: skenv re-clones a
cache whose origin does not match. To rule it out anyway, delete the cache
(see above) and run `skenv sync` again.

## Mapping to `skills-lock.json`

The dependency fields follow the project lock file of the
[`skills` CLI](https://github.com/vercel-labs/skills) (checked against 1.7.0),
so a manifest can be translated if skenv is ever replaced by it:

| `[user.dependencies.<name>]` | `skills-lock.json` entry             | Notes                                                                         |
| ---------------------------- | ------------------------------------ | ----------------------------------------------------------------------------- |
| table key `<name>`           | entry key                            | skill name                                                                    |
| `repo`                       | `source` (+ `sourceType = "github"`) | `owner/repo`; a full URL maps to `source`/`sourceUrl`                         |
| `skill_dir`                  | `skillPath`                          | skills-lock stores the file: `archify` ↔ `archify/SKILL.md`, `.` ↔ `SKILL.md` |
| `commit`                     | `ref`                                | skenv requires a full SHA; `ref` also accepts branches and tags               |
| —                            | `computedHash`                       | not recorded by skenv; the SHA pins the content                               |

The other direction, from the global lock `~/.agents/.skill-lock.json` to
the manifest, is `skenv import`; see
[Adopt existing skills](adopting.md#what-import-reads).

`[user.checkouts.<id>]` has no equivalent: the `skills` CLI does not manage
working copies. A translation would list each skill a checkout selects
(after `include` and `exclude`) as an entry of its own, pinned to the commit
of the working copy.

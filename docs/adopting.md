# Adopt existing skills

A machine that already has skills, installed with `npx skills add -g`,
linked by hand from a clone, or copied in, can move to skenv with one
command: `skenv import` writes what is installed into the manifest, and
`skenv sync --adopt` then replaces the installed copies with managed ones.
A skill whose installed commit `import` cannot find is recorded too, but
`--sync` leaves it as installed until you decide what to do with it.

- [In one command: `init --import`](#in-one-command-init---import)
- [Step by step](#step-by-step)
- [What `import` changes](#what-import-changes)
- [Reading the report](#reading-the-report)
- [Unmatched skills and `--sync`](#unmatched-skills-and---sync)
- [What `import` reads](#what-import-reads)
- [How the commit of a skill is found](#how-the-commit-of-a-skill-is-found)
- [Project skills: `import --project`](#project-skills-import---project)

## In one command: `init --import`

In the git repository that will hold the manifest (usually your private
skills repository):

```sh
cd ~/src/<skills-repo>
skenv init --import --dry-run   # the plan: new manifest, entries, lock changes
skenv init --import
skenv list
```

`skenv init --import` does what `skenv init` does
(adds `[user]` to the skenv file of the repository or creates
`skenv.toml`, and records it as your manifest), imports the installed skills
into it, and runs `skenv sync --adopt` as `skenv import --sync` does: the
skills pinned without a matching commit stay as installed (see
[Unmatched skills](#unmatched-skills-and---sync)). The repository itself
becomes a checkout, `[user.checkouts.<repo-name>]` with `checkout_dir =
"."`, written from its `origin` as with `skenv init` (see
[Git hosts](git-hosts.md#starting-a-manifest-with-skenv-init)).

The output is the import report (see [Reading the report](#reading-the-report)),
the manifest diff, then the sync. It ends with what was recorded and what
was taken over:

```text
sync: 8 changes, 1 unresolved, 0 warnings, 0 errors
recorded in ~/src/<skills-repo>/skenv.toml: release-notes
taken over: release-notes
```

The line `unresolved: ~/src/<skills-repo> has uncommitted changes: local
development state, not updated (commit or stash, then rerun sync)` is
expected here: the new manifest is not committed yet. Then verify:

```sh
skenv list     # the imported skills: installed (unmatched ones: conflict)
skenv doctor   # after you commit and push skenv.toml: exit 0, or the skills left for you to decide
```

Commit the manifest and push it, so that other machines can
[use it](another-machine.md):

```sh
git -C ~/src/<skills-repo> add skenv.toml
git -C ~/src/<skills-repo> commit -m "chore(manifest): import installed skills"
git -C ~/src/<skills-repo> push
```

## Step by step

With a manifest already configured (`skenv clone <repo>`,
`skenv use <path>`, `--manifest` or `$SKENV_MANIFEST`), the same happens in
steps you can inspect:

```sh
# 1. Preview: what becomes managed, the manifest diff, the lock changes.
skenv import --dry-run

# 2. Write the manifest and clean the lock of the skills CLI.
skenv import

# 3. Replace the installed copies with managed ones (old ones are backed up).
skenv sync --dry-run --adopt
skenv sync --adopt

# 4. Commit and push skenv.toml; then the machine matches the manifest (exit 0).
skenv doctor
```

`skenv import --sync` runs step 3 right after step 2, for every skill it
imported except the unmatched ones. Run step 3 yourself and it replaces
those too: that is the decision. A skenv file that has no `[user]` yet
gets one first, as `skenv init` adds it (and a public `[repository]` is
refused, as there). Without any manifest, `skenv import` stops and points
at `skenv init --import`.

Running `skenv import` again imports nothing: everything it added is in the
manifest now, and it skips skills that are. So `import --sync` after an
earlier `import` has nothing unmatched to hold back, and its
`sync --adopt` takes over every installed copy.

## What `import` changes

- **The manifest**: new `[user.checkouts.<id>]` and
  `[user.dependencies.<name>]` entries at the end of `[user]`, in the
  format of the file, with its comments and order kept. The ID of a
  checkout is derived from the repository name (`<skills-repo>`, then
  `<skills-repo>-2` if it is taken). The diff is printed; the change is not
  committed, and `import` prints the commit command.
- **The lock of the `skills` CLI: ownership moves to skenv.** Every skill
  that is in the manifest now is removed from
  `~/.agents/.skill-lock.json`, so `npx skills update -g` no longer
  overwrites a directory skenv manages; updates go through
  `skenv vendor update` from now on. Other entries and keys stay. The lock
  as it was goes to `~/.local/state/skenv/backup/<timestamp>/` first, under
  its path relative to your home (`.agents/.skill-lock.json`); copy it back
  to undo.
- **Nothing installed**: the installed copies and links stay until
  `skenv sync --adopt`, which moves each one it replaces to
  `~/.local/state/skenv/backup/<timestamp>/` (see
  [`--adopt`](conflicts.md#take-over-with---adopt)). `--sync` and
  `init --import` run it for you, except for the unmatched skills.

`--dry-run` prints the same report, diff and lock changes and writes
nothing (it may fetch into the clone cache `~/.cache/skenv/repos` to search
the history).

## Reading the report

The report leads with what becomes managed, then what is not imported, then
the lock change and the diff:

```text
becomes managed: 4 entries in ~/src/<skills-repo>/skenv.toml
  exact: the commit has the hash recorded in the lock
    archify from <owner>/<repo> (skills/archify) at 6616c70c36e4
  same files: the commit has the files of the installed copy (no commit has the hash in the lock)
    lost from <owner>/<repo> (skills/lost) at 6616c70c36e4: skillFolderHash 000000000000 is not in the history of the default branch
  unmatched: no commit matched, pinned to the tip of the branch; the installed copy may differ
    drifted from <owner>/<repo> (skills/drifted) at 5d0e1f2a3b4c: skillFolderHash 111111111111 is not in the history of the default branch, and no commit has the files of the installed copy; HEAD of the default branch
  checkout: repositories kept as git working copies
    <team-skills>: <owner>/<team-skills> at ~/src/<team-skills> (include alpha, beta)
not imported: 2
  ~/.claude/skills/handmade: not in ~/.agents/.skill-lock.json and not a link into a git working copy; move it into a checkout, or add "handmade" to user.unmanaged
  removed, in ~/.agents/.skill-lock.json: not installed in ~/.agents/skills or an agent directory; it stays in the lock
remove archify, drifted, lost from ~/.agents/.skill-lock.json, so that the skills CLI no longer updates them; skenv manages each once sync takes its installed copy over (a copy of the lock goes to ~/.local/state/skenv/backup/<timestamp>/.agents/.skill-lock.json)
--- ~/src/<skills-repo>/skenv.toml
+++ ~/src/<skills-repo>/skenv.toml
...
import: 4 manifest entries (1 exact, 1 same files, 1 unmatched, 1 checkout), 3 removed from the skills lock, 1 unmanaged, 0 warnings, 0 errors
```

- **`exact`**: the dependency will be what the `skills` CLI installed
  (local edits of the copy aside: they are not in the hash).
- **`same files`**: no commit has the hash, but one has the same files as
  the copy on disk. Usually right; pin another commit with
  `skenv vendor update <name> --rev <sha>` if not.
- **`unmatched`**: nothing matched, so the entry is pinned to HEAD, which
  may differ from what you have. See
  [Unmatched skills](#unmatched-skills-and---sync).
- **`checkout`**: a repository kept as an editable working copy, from
  links into it (or the repository of the manifest, with `init --import`,
  as `checkout_dir = "."`). `include` lists the linked skills when only
  some of its skills are linked.
- **`not imported`**: with the reason and a hint. Typical ones: a
  directory installed by hand (move it into a checkout, or add its name
  to `user.unmanaged` to leave it alone), a skill of the lock that was not
  installed from a git repository, a link under another name than its
  directory, a skill of an existing checkout that its `include` or
  `exclude` does not select, or a lock entry that is not installed (it
  stays in the lock).
  A skill that cannot be fetched is an `error:` there and exits 1; the
  rest is imported.

## Unmatched skills and `--sync`

An unmatched skill is recorded in the manifest (and leaves the lock) like
any other, but `import --sync` and `init --import` do not take it over:
its installed copy and links stay as they are, and `doctor` reports them
as a conflict until you decide. The exact and same-files skills and the
checkouts are taken over as usual. After the sync the report says
which skills it recorded and which it took over:

```text
leave ~/.agents/skills/drifted as it is: drifted was pinned without a matching commit, so it is not taken over
...
sync: 12 changes, 1 unresolved, 0 warnings, 0 errors
recorded in ~/src/<skills-repo>/skenv.toml: alpha, archify, beta, drifted, lost
taken over: alpha, archify, beta, lost
left as installed (unmatched): drifted; decide for each:
  drifted: `skenv sync --adopt` replaces it with the pinned commit (the copy goes to ~/.local/state/skenv/backup), or `skenv vendor remove drifted` drops the entry and leaves the copy to neither skenv nor the skills CLI (its lock entry is in the backup of the lock)
```

Compare the installed copy with the skill at the pinned commit first (the
`commit` of its entry, in a clone or the repository's web view), then:

- **Keep the pinned commit**: `skenv sync --adopt` backs up the installed
  copy and replaces it. To pin another commit first:
  `skenv vendor update <name> --rev <sha>`.
- **Keep your copy, unmanaged**: `skenv vendor remove <name>` drops the
  entry; the copy stays where it is and `doctor` reports it as unmanaged.
  Import already removed it from the lock of the skills CLI, so neither
  tool updates it now: move it into a checkout to manage it with skenv,
  add its name to `user.unmanaged`, or give it back to the skills CLI
  by restoring its entry from the backup of the lock in
  `~/.local/state/skenv/backup/<timestamp>/`.

`--dry-run --sync` says which skills the sync would leave as installed.

## What `import` reads

`import` looks at every entry of the store (`~/.agents/skills`, or
`user.storage.dir`) and of the agent directories (`~/.claude/skills`,
`~/.pi/agent/skills`, or what `[user.agents]` selects), once per name:

| Found                                            | Becomes                                                   |
| ------------------------------------------------ | --------------------------------------------------------- |
| a skill of the lock of the `skills` CLI          | `[user.dependencies.<name>]`                              |
| a link into a git working copy with an `origin`  | `[user.checkouts.<id>]`, with `include = [...]` if partial |
| anything else                                    | reported as `unmanaged`, not imported                     |

Skipped without a word: `~/.claude/skills/synced` (Claude's), links into
`~/.claude/plugins` (skills of Claude Code plugins), names matching
`user.unmanaged`, and skills that are in the manifest already.

**The lock of the `skills` CLI** is `~/.agents/.skill-lock.json` (or
`$XDG_STATE_HOME/skills/.skill-lock.json`), version 3, as written by
`npx skills add -g` (checked against skills 1.7.0). It pins no commit; each
entry, keyed by the skill name, maps to a dependency like this:

| Lock field        | Meaning                                                        | skenv                                             |
| ----------------- | -------------------------------------------------------------- | ------------------------------------------------- |
| entry key         | skill name                                                     | the table key, `[user.dependencies.<name>]`       |
| `source`          | `owner/repo` for GitHub, the URL for other hosts               | `repo` (GitHub)                                   |
| `sourceType`      | `github`, `git`, `gitlab`, `local`, …                          | only git sources are imported                     |
| `sourceUrl`       | clone URL                                                      | `repo` (other git hosts): the short form on GitLab, Codeberg or a host declared in the manifest (`gitlab:group/repo`, `<alias>:path`), the URL otherwise |
| `ref`             | branch, tag or commit, only when installed as `owner/repo#ref` | where the commit is searched                      |
| `skillPath`       | `…/SKILL.md` inside the repository                             | `skill_dir`: its directory, `.` for the root      |
| `skillFolderHash` | GitHub: git tree id of the skill directory at install or update time; other hosts: a sha256 of its files | finds `commit`, see below                         |
| `installedAt`, `updatedAt` | when the skill was installed, last updated            | where the search starts                           |

The inverse mapping, from a manifest to the project lock
`skills-lock.json`, is in [Manifest](manifest.md#mapping-to-skills-lockjson).

**Links into a working copy**: a symlink whose target is a skill directory
(with `SKILL.md`) inside a git working copy becomes a checkout, keyed by
an ID derived from the repository name: `repo` from its `origin` (the
short form on GitHub, GitLab, Codeberg or a host declared in the manifest,
see [Git hosts](git-hosts.md); the URL otherwise), `checkout_dir` the
working copy (`~/...` in your home, `"."` for the repository of the
manifest), `skills_dir` the directory that holds the skill. When only some
skills of that directory are linked, the entry lists them in `include`
(see [Selecting skills](manifest.md#selecting-skills)), so `sync` does not
start linking the others.

## How the commit of a skill is found

The `skills` CLI records a fingerprint of the skill directory, not a
commit. For a GitHub install, `skillFolderHash` is the git tree id of the
directory (40 hex digits); for other git hosts it is a sha256 of its files
(64 hex digits, see [the file hash](#the-file-hash-computedhash)). Either
names the content exactly, so `import` pins the commit it came from:

1. Take `ref` of the entry, or the default branch.
2. Walk `git log -- <skill directory>` on it, newest first, starting with
   the commits made by `updatedAt` (else `installedAt`).
3. Pin the first commit where `git rev-parse <commit>:<skill directory>`
   equals the tree id, or whose files hash to the sha256.

Local edits of the installed copy do not matter: they are not in the hash.
When no commit matches (the history was rewritten, or the ref is gone),
`import` pins the commit whose files match the installed copy (the files
the `skills` CLI copies: without `metadata.json`, `.git` and
`__pycache__`), and if none does, HEAD of the ref or the default branch.
These are the three groups of the report: `exact`, `same files` and
`unmatched`; only the last is a warning, and `--sync` leaves it as
installed (see [Unmatched skills](#unmatched-skills-and---sync)).

## Project skills: `import --project`

A project whose skills were added with `npx skills add` (without `-g`) has
a `skills-lock.json` in its root and the copies in `.agents/skills`, linked
into `.claude/skills` and other agent directories. `skenv import --project`
turns that into the `[project]` section of the project's skenv file (see
[Project skills](project-skills.md)), in the same steps:

```sh
cd ~/src/<project>
skenv import --project --dry-run   # the diff, the duplicates, the lock changes
skenv import --project             # write [project], clean skills-lock.json
skenv sync --adopt                 # replace the installed copies with pinned ones
skenv doctor                       # exit 0
git add -- skenv.toml .agents/skills .claude/skills skills-lock.json
git commit -m "chore(skills): import project skills"
```

`skenv import --project --sync` runs `skenv sync --adopt` right after the
import, except for the unmatched skills: their installed copies stay, and
the report offers `skenv sync --adopt` or
`skenv vendor remove --project <name>`, as at user level (see
[Unmatched skills](#unmatched-skills-and---sync)). It works from any directory of the repository and never touches
your machine's manifest (`--manifest` is refused with `--project`).

### What it reads and writes

- **The skenv file** at the repository root: a file without `[project]`
  gets one, and a repository without a skenv file gets `skenv.toml`. The
  new section keeps the default `dir` (`.agents/skills`, where the
  `skills` CLI installs) and lists the agent directories that exist,
  `.claude/skills` and `.pi/skills`, as `mirrors`. Each lock entry becomes
  a `[project.dependencies.<name>]` at the end of `[project]`, in the
  file's format, with its comments kept; the diff is printed.
- **`skills-lock.json`**, version 1 (skills 1.7.0). Per skill:

  | Lock field     | Meaning                                                   | skenv                                          |
  | -------------- | --------------------------------------------------------- | ---------------------------------------------- |
  | entry key      | skill name                                                | the table key, `[project.dependencies.<name>]` |
  | `source`       | `owner/repo` for GitHub, the URL for other hosts          | `repo` (GitHub)                                |
  | `sourceType`   | `github`, `git`, `gitlab`, `local`, `node_modules`, …     | only git sources are imported; the others stay in the lock |
  | `sourceUrl`    | clone URL (git and GitLab sources)                        | `repo`: the short form on GitLab, Codeberg or a host of `project.git_hosts`, the URL otherwise |
  | `ref`          | branch or tag, when installed as `owner/repo#ref`         | where the commit is searched                   |
  | `skillPath`    | `…/SKILL.md` inside the repository                        | `skill_dir`: its directory, `.` for the root   |
  | `computedHash` | sha256 of the skill's files at install time              | finds `commit`, see below                      |

  There are no dates: every commit that touched the skill directory is a
  candidate, newest first.
- **The lock afterwards**: every skill now in `[project]` leaves it, so
  `npx skills` does not restore or update a directory skenv manages; a
  lock with no skills left is removed. The file as it was goes to
  `~/.local/state/skenv/backup/<timestamp>/<path of the project>/skills-lock.json`
  first. Commit the change with the project.
- **Nothing else**: the installed copies stay until `skenv sync --adopt`
  moves each to the backup directory and copies the pinned commit in its
  place (`--sync` does that for every skill but the unmatched ones).

`--dry-run` prints the same report, diff and lock changes and writes
nothing. A second `import --project` imports nothing; an entry the
`skills` CLI adds again for a skill already in `[project]` only leaves the
lock.

### The file hash (`computedHash`)

`computedHash` is not a git hash. It is usually `computeSkillFolderHash`
of the `skills` CLI, computed on a clone of the repository: sha256 over the
skill directory's files, each fed as its path relative to the directory,
then its content. skenv recomputes it from each candidate commit's tree and
pins the newest commit that gives the same value:

- **Files**: every regular file, executable or not. Symbolic links are
  left out (the CLI skips anything that is not a file), and so are the
  directories `.git` and `node_modules` at any depth. `metadata.json` and
  `__pycache__` count here, although installed copies leave them out.
- **Order**: the paths are sorted with JavaScript's `localeCompare`,
  which is not byte order: it is the Unicode Collation Algorithm with the
  root collation of CLDR (skenv uses the same collation, checked against
  Node for every pair of printable ASCII characters). Case is ignored
  until the last level, where lowercase comes first, and punctuation sorts
  before digits and letters in its own order (`_`, `-`, `.`, `/`), so
  `SKILL.md` sorts between `scripts/…` and `templates/…`, and
  `references/a_b.md` before `references/a-b.md`.

For a few owners (`vercel`, `vercel-labs`, `heygen-com`,
`remotion-dev`) the CLI downloads a snapshot from its own servers instead
of cloning, and records the snapshot's hash: the same sha256, over the
files it installs (without `metadata.json`, `__pycache__` and
`__pypackages__`, with `node_modules`). skenv tries that variant too; when
the servers computed the hash some other way, the installed-copy fallback
pins the commit. The same fallback covers repositories whose checkout
changes the files (Git LFS, line-ending conversion in `.gitattributes`),
where no commit's stored files give the recorded hash.

The global lock records the same hash as `skillFolderHash` for installs
from outside GitHub, and `import` without `--project` matches it the same
way.

### Project-own skills and duplicates

Skills in the project that are neither in the lock nor in `[project]`
(and carry no `.skenv` marker) are the project's own. `import --project`
looks for them in `dir`, the mirrors, `.agents/skills`, `.claude/skills`
and `.pi/skills`, and never changes or removes one:

```text
project-own skill .agents/skills/deploy: kept as it is; sync mirrors it
warning: project-own skill solo is only in .claude/skills/solo: move it to .agents/skills/solo, which skenv mirrors (sync leaves it where it is, doctor reports it unmanaged)
warning: project-own skill review is in .agents/skills/review, .claude/skills/review, and the copies differ (.agents/skills/review and .claude/skills/review differ: 1 file (SKILL.md) changed, 1 file (checklist.md) only in .agents/skills/review); pick the version to keep, put it in .agents/skills/review and remove the others: skenv never removes a project-own skill, and sync --adopt would replace a differing mirror with a link to .agents/skills
```

- **In `dir`**: nothing to do; `sync` links it into every mirror.
- **Only in another directory**: move it into `dir`. `sync` does not
  mirror a skill from a mirror, and `doctor` reports it as `unmanaged`.
- **In several directories with the same files** (copies rather than
  links): an info line; the one in `dir` is the source, and `sync`
  replaces the identical mirror copies with links (or its own copies, with
  `mirrors_mode = "copy"`).
- **In several directories with different files**: a warning with the
  files that differ. Decide which version is right, keep it in `dir`, and
  delete or merge the others by hand. Until then `--sync` does not run
  `skenv sync --adopt` (it exits 1), because `--adopt` would back up the
  differing mirror copy and link the mirror to `dir`.

### Reading the output

The report is that of the user-level import (see
[Reading the report](#reading-the-report)), with `computedHash` for the
fingerprint and the project-own skills after what is not imported:

```text
becomes managed: 2 entries in ~/src/<project>/skenv.toml
  exact: the commit has the hash recorded in the lock
    release-notes from example-vendor/tools (tools/release-notes) at 27f221f8f2a4
  same files: the commit has the files of the installed copy (no commit has the hash in the lock)
    lost from <owner>/<repo> (skills/lost) at 6616c70c36e4: computedHash 000000000000 is not in the history of the default branch
not imported: 1
  local-one, in skills-lock.json: installed from a "local" source, not a git repository; `skenv vendor add` needs one; it stays in the lock
project-own skill .agents/skills/deploy: kept as it is; sync mirrors it
remove lost, release-notes from skills-lock.json, so that the skills CLI no longer updates them; skenv manages each once sync takes its installed copy over (a copy of the lock goes to ~/.local/state/skenv/backup/<timestamp>/src/<project>/skills-lock.json)
...
import: 2 [project] entries (1 exact, 1 same files), 2 removed from skills-lock.json, 1 project-own skills, 0 differing duplicates, 0 warnings, 0 errors
```

The [command reference](commands/skenv_import.md) has complete runs.

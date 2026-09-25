# Adopting an existing setup

A machine that already has skills, installed with `npx skills add -g`,
linked by hand from a clone, or copied in, can move to skenv with one
command: `skenv import` writes what is installed into the manifest, and
`skenv sync --adopt` then replaces the installed copies with managed ones.

- [In one command: `init --import`](#in-one-command-init---import)
- [Step by step](#step-by-step)
- [What `import` reads](#what-import-reads)
- [How the commit of a skill is found](#how-the-commit-of-a-skill-is-found)
- [What `import` writes](#what-import-writes)
- [Reading the output](#reading-the-output)
- [Project skills: `import --project`](#project-skills-import---project)

## In one command: `init --import`

In the git repository that will hold the manifest (usually your private
skills repository):

```sh
cd ~/src/<skills-repo>
skenv init --import --dry-run   # the plan: new manifest, entries, lock changes
skenv init --import
skenv doctor
```

`skenv init --import` does what `skenv init` without `<owner/repo>` does
(adds `[environment]` to the skenv file of the repository or creates
`skenv.toml`, and records it as your manifest), imports the installed skills
into it, and runs `skenv sync --adopt`. The repository itself becomes an own
repository, written from its `origin` as with `skenv init` (see
[Git hosts](git-hosts.md#skenv-init-without-a-repository)). Commit the
manifest afterwards:

```sh
git -C ~/src/<skills-repo> add skenv.toml
git -C ~/src/<skills-repo> commit -m "chore(manifest): import installed skills"
```

## Step by step

With a manifest already configured (`skenv init <owner/repo>`, `--manifest`
or `$SKENV_MANIFEST`), the same happens in steps you can inspect:

```sh
# 1. Preview: the manifest diff and the lock entries that would be removed.
skenv import --dry-run

# 2. Write the manifest and clean the lock of the skills CLI.
skenv import

# 3. Replace the installed copies with managed ones (old ones are backed up).
skenv sync --dry-run --adopt
skenv sync --adopt

# 4. The machine matches the manifest (exit 0).
skenv doctor
```

`skenv import --sync` runs step 3 right after step 2. A skenv file that has
no `[environment]` yet gets one first, as `skenv init` adds it (and a
public `[repo]` is refused, as there). Without any manifest, `skenv import`
stops and points at `skenv init --import`.

Running `skenv import` again imports nothing: everything it added is in the
manifest now, and it skips skills that are.

## What `import` reads

`import` looks at every entry of the store (`~/.agents/skills`, or
`layout.store`) and of the agent directories (`~/.claude/skills`,
`~/.pi/agent/skills`, or `layout.targets`), once per name:

| Found                                            | Becomes                                                   |
| ------------------------------------------------ | --------------------------------------------------------- |
| a skill of the lock of the `skills` CLI          | `[[environment.vendor]]`                                  |
| a link into a git working copy with an `origin`  | `[[environment.own]]`, with `skills = [...]` if partial   |
| anything else                                    | reported as `unmanaged`, not imported                     |

Skipped without a word: `~/.claude/skills/synced` (Claude's), links into
`~/.claude/plugins` (skills of Claude Code plugins), names matching
`layout.ignore`, and skills that are in the manifest already.

**The lock of the `skills` CLI** is `~/.agents/.skill-lock.json` (or
`$XDG_STATE_HOME/skills/.skill-lock.json`), version 3, as written by
`npx skills add -g` (checked against skills 1.7.0). It pins no commit; each
entry, keyed by the skill name, maps to a vendor entry like this:

| Lock field        | Meaning                                                        | skenv                                             |
| ----------------- | -------------------------------------------------------------- | ------------------------------------------------- |
| entry key         | skill name                                                     | `name`                                            |
| `source`          | `owner/repo` for GitHub, the URL for other hosts               | `repo` (GitHub)                                   |
| `sourceType`      | `github`, `git`, `gitlab`, `local`, …                          | only git sources are imported                     |
| `sourceUrl`       | clone URL                                                      | `repo` (other git hosts): the short form on GitLab, Codeberg or a host declared in the manifest (`gitlab:group/repo`, `<alias>:path`), the URL otherwise |
| `ref`             | branch, tag or commit, only when installed as `owner/repo#ref` | where the commit is searched                      |
| `skillPath`       | `…/SKILL.md` inside the repository                             | `path`: its directory, `.` for the root           |
| `skillFolderHash` | GitHub: git tree id of the skill directory at install or update time; other hosts: a sha256 of its files |  finds `rev`, see below                            |
| `installedAt`, `updatedAt` | when the skill was installed, last updated            | where the search starts                           |

The inverse mapping, from a manifest to the project lock
`skills-lock.json`, is in [Manifest](manifest.md#mapping-to-skills-lockjson).

**Links into a working copy**: a symlink whose target is a skill directory
(with `SKILL.md`) inside a git working copy becomes an own repository:
`repo` from its `origin` (the short form on GitHub, GitLab, Codeberg or a
host declared in the manifest, see [Git hosts](git-hosts.md); the URL
otherwise),
`path` the working copy, `skills_dir` the directory that holds the skill.
When only some skills of that directory are linked, the entry lists them
in `skills` (see
[Selecting skills](manifest.md#selecting-skills-of-an-own-repository)), so
`sync` does not start linking the others.

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
Both fallbacks are warnings.

## What `import` writes

- **The manifest**: new `[[environment.own]]` and `[[environment.vendor]]`
  entries at the end of `[environment]`, in the format of the file, with
  its comments and order kept. The diff is printed; the change is not
  committed, and `import` prints the commit command.
- **The lock of the `skills` CLI**: every skill that is in the manifest now
  is removed from it, so `npx skills update -g` does not overwrite a
  directory skenv manages. Other entries and keys stay. The lock as it was
  goes to `~/.local/state/skenv/backup/<timestamp>/` first, under its path
  relative to your home (`.agents/.skill-lock.json`); copy it back to undo.
- Nothing else: the installed copies and links stay until
  `skenv sync --adopt`, which moves each one it replaces to
  `~/.local/state/skenv/backup/<timestamp>/` (see
  [`--adopt`](commands.md#--dry-run-and---adopt)).

`--dry-run` prints the same report, diff and lock changes and writes
nothing (it may fetch into the clone cache `~/.cache/skenv/repos` to search
the history).

## Reading the output

```text
import own <owner>/<skills-repo> at ~/src/<skills-repo> (skills alpha, beta)
import vendor archify from <owner>/<repo> (skills/archify) at 6616c70c36e4
  its skillFolderHash ed9275516424 is the tree of skills/archify at that commit
import vendor lost from <owner>/<repo> (skills/lost) at 6616c70c36e4
unmanaged ~/.claude/skills/handmade: not in ~/.agents/.skill-lock.json and not a link into a git working copy; move it into an own repository, or add "handmade" to layout.ignore
--- ~/src/<skills-repo>/skenv.toml
+++ ~/src/<skills-repo>/skenv.toml
...
remove archify, lost from ~/.agents/.skill-lock.json (a copy goes to ~/.local/state/skenv/backup/<timestamp>/.agents/.skill-lock.json)
import: 3 manifest entries, 2 removed from the skills lock, 1 unmanaged, 1 warnings, 0 errors
warning: vendor lost: skillFolderHash 000000000000 is not in the history of the default branch; pinned 6616c70c36e4, whose files match the installed copy
```

- **`its skillFolderHash … is the tree`**: an exact match; the vendored
  skill will be what the `skills` CLI installed.
- **`warning: … whose files match the installed copy`**: no commit has the
  hash, but one has the same files as the copy on disk. Usually right;
  check with `skenv vendor update <name> --rev <sha>` if not.
- **`warning: … pinned HEAD`**: nothing matched; `sync --adopt` will
  replace the copy with the current HEAD, which may differ from what you
  had. Compare them first, or pin another commit with
  `skenv vendor update <name> --rev <sha>`.
- **`unmanaged`**: not imported, with the reason and a hint. Typical ones:
  a directory installed by hand (move it into an own repository, or add
  its name to `layout.ignore` to leave it alone), a skill of the lock that
  was not installed from a git repository, a link under another name than
  its directory, or an own skill that `skills` or `exclude` of its entry
  does not select.
- **`skip <name> of ~/.agents/.skill-lock.json: not installed`**: the lock
  lists a skill that is not on disk; it is left in the lock.
- A skill that cannot be fetched is an `error:` and exits 1; the rest is
  imported.

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
import. It works from any directory of the repository and never touches
your machine's manifest (`--manifest` is refused with `--project`).

### What it reads and writes

- **The skenv file** at the repository root: a file without `[project]`
  gets one, and a repository without a skenv file gets `skenv.toml`. The
  new section keeps the default `dir` (`.agents/skills`, where the
  `skills` CLI installs) and lists the agent directories that exist,
  `.claude/skills` and `.pi/skills`, as `mirrors`. Each lock entry becomes
  a `[[project.vendor]]` at the end of `[project]`, in the file's format,
  with its comments kept; the diff is printed.
- **`skills-lock.json`**, version 1 (skills 1.7.0). Per skill:

  | Lock field     | Meaning                                                   | skenv                                          |
  | -------------- | --------------------------------------------------------- | ---------------------------------------------- |
  | entry key      | skill name                                                | `name`                                         |
  | `source`       | `owner/repo` for GitHub, the URL for other hosts          | `repo` (GitHub)                                |
  | `sourceType`   | `github`, `git`, `gitlab`, `local`, `node_modules`, …     | only git sources are imported; the others stay in the lock |
  | `sourceUrl`    | clone URL (git and GitLab sources)                        | `repo`: the short form on GitLab, Codeberg or a host of `project.hosts`, the URL otherwise |
  | `ref`          | branch or tag, when installed as `owner/repo#ref`         | where the commit is searched                   |
  | `skillPath`    | `…/SKILL.md` inside the repository                        | `path`: its directory, `.` for the root        |
  | `computedHash` | sha256 of the skill's files at install time              | finds `rev`, see below                         |

  There are no dates: every commit that touched the skill directory is a
  candidate, newest first.
- **The lock afterwards**: every skill now in `[project]` leaves it, so
  `npx skills` does not restore or update a directory skenv manages; a
  lock with no skills left is removed. The file as it was goes to
  `~/.local/state/skenv/backup/<timestamp>/<path of the project>/skills-lock.json`
  first. Commit the change with the project.
- **Nothing else**: the installed copies stay until `skenv sync --adopt`
  moves each to the backup directory and copies the pinned commit in its
  place.

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

The lines are those of the user-level import (see
[Reading the output](#reading-the-output)), with `computedHash` for the
fingerprint:

```text
import vendor release-notes from example-vendor/tools (tools/release-notes) at 27f221f8f2a4
  its computedHash 652cdbb69a71 is the sha256 of the files of tools/release-notes at that commit
warning: vendor lost: computedHash 000000000000 is not in the history of the default branch; pinned 6616c70c36e4, whose files match the installed copy
skip local-one of skills-lock.json: installed from a "local" source, not a git repository; `skenv vendor add` needs one; it stays in the lock
import: 2 [project] entries, 2 removed from skills-lock.json, 1 project-own skills, 0 differing duplicates, 1 warnings, 0 errors
```

The [command reference](commands/skenv_import.md) has a complete run.

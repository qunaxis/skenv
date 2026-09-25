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
- [Project skills](#project-skills)

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
repository when its `origin` is on GitHub, as with `skenv init`. Commit the
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
| `sourceUrl`       | clone URL                                                      | `repo` (other git hosts)                          |
| `ref`             | branch, tag or commit, only when installed as `owner/repo#ref` | where the commit is searched                      |
| `skillPath`       | `…/SKILL.md` inside the repository                             | `path`: its directory, `.` for the root           |
| `skillFolderHash` | GitHub: git tree id of the skill directory at install or update time; other hosts: a sha256 of its files |  finds `rev`, see below                            |
| `installedAt`, `updatedAt` | when the skill was installed, last updated            | where the search starts                           |

The inverse mapping, from a manifest to the project lock
`skills-lock.json`, is in [Manifest](manifest.md#mapping-to-skills-lockjson).

**Links into a working copy**: a symlink whose target is a skill directory
(with `SKILL.md`) inside a git working copy becomes an own repository:
`repo` from its `origin` (`owner/repo` on GitHub, the URL otherwise),
`path` the working copy, `skills_dir` the directory that holds the skill.
When only some skills of that directory are linked, the entry lists them
in `skills` (see
[Selecting skills](manifest.md#selecting-skills-of-an-own-repository)), so
`sync` does not start linking the others.

## How the commit of a skill is found

For a GitHub install, `skillFolderHash` is the git tree id of the skill
directory, which names its content exactly, so `import` pins the commit it
came from:

1. Take `ref` of the entry, or the default branch.
2. Walk `git log -- <skill directory>` on it, newest first, starting with
   the commits made by `updatedAt` (else `installedAt`).
3. Pin the first commit where `git rev-parse <commit>:<skill directory>`
   equals `skillFolderHash`.

Local edits of the installed copy do not matter: they are not in the hash.
When no commit matches (the history was rewritten, or the ref is gone),
`import` pins the commit whose files match the installed copy (the files
the `skills` CLI copies: without `metadata.json`, `.git` and
`__pycache__`), and if none does, HEAD of the ref or the default branch.
Both fallbacks are warnings. For other git hosts the `skills` CLI records a
sha256 of the files instead of a tree id, so the installed copy is what
`import` matches there.

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
- **`warning: … pinned its HEAD`**: nothing matched; `sync --adopt` will
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

## Project skills

Importing a project's `skills-lock.json` (`skenv import --project`) comes
with project manifests; see
[#26](https://github.com/qunaxis/skenv/issues/26).

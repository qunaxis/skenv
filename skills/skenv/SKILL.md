---
name: skenv
description: Manage agent skills (Claude Code, Codex, pi) with skenv instead of `npx skills add` or manual copies. Use when asked to install, update, pin or remove a third-party skill, create a new skill, fix skill links or a failing `skenv doctor`, decide where a skill belongs (private or public repository), or publish a skill.
license: MIT
metadata:
  source: original
---

# skenv

skenv makes the skills on a machine match a declarative manifest, the
`[user]` section of a `skenv.toml`, for every agent at once: the store `~/.agents/skills` (read
directly by Codex) and symlinks in `~/.claude/skills` and
`~/.pi/agent/skills`.

## Repositories

- **Manifest repository**: the git repository whose `skenv.toml` holds
  `[user]`. Never
  assume its path or name; skenv resolves the manifest from `--manifest`,
  then `$SKENV_MANIFEST`, then `manifest` in `~/.config/skenv/config.toml`
  (written by `skenv init`). `skenv doctor --json` prints the resolved path
  in its `manifest` field. If none of these is set, ask the user where the
  manifest is instead of guessing a default location.
- **Private repository**: the user's own repository listed in the manifest
  whose `skenv.toml` has `[repository]` `visibility = "private"`. Usually it is also the
  manifest repository.
- **Public repository**: the user's own repository whose `skenv.toml` has
  `[repository]` `visibility = "public"`. It must not hold `[user]`.

## Rules

- Change skills only through skenv and the manifest. Never run
  `npx skills add`, never copy or symlink skills into agent directories by
  hand: skenv will report such paths as `unmanaged` or `conflict`.
- skenv only removes or replaces paths it created. It never touches paths
  outside the manifest unless the user explicitly runs `--adopt`, and never
  `~/.claude/skills/synced` or entries matched by `user.unmanaged`.
- New skills go to the private repository by default. A skill moves to the
  public repository only through the publication checklist
  ([publishing.md](references/publishing.md)).
- After changing the manifest, commit and push it in the manifest repository;
  other machines pick it up with `skenv sync` (hourly with
  `skenv autostart`).
- Run `--dry-run` first when unsure; it prints the plan and changes nothing.

## Third-party skills

Third-party skills are pinned to a full commit SHA in `[user.dependencies.<name>]` tables
of the manifest; they are never copied into our repositories.

| Task | Command |
| --- | --- |
| Add | `skenv vendor add <owner/repo> [--path <dir with SKILL.md>] [--name N] [--rev SHA]` |
| Update | `skenv vendor update [name...]` (every dependency without names; `--rev SHA` with exactly one name; shows `git log` of each path) |
| Remove | `skenv vendor remove <name>` |

`add` needs `--path` when the repository contains several skills (the error
lists them); without `--rev` it pins the HEAD of the default branch. Each
command edits the manifest's `skenv.toml` in place (a `[user.dependencies.<name>]`
table with `repo`, `skill_dir` and `commit`) and syncs the skills it touched, but does not
commit. Review what the new commit brings, then commit with the command skenv
prints; it already names the manifest directory and file. To do it by hand, derive
the directory from the resolved manifest path:

```sh
manifest=$(skenv doctor --json | jq -r .manifest)   # or the path you know
manifest=${manifest/#\~/$HOME}                     # doctor shows ~ for $HOME
dir=$(dirname "$manifest")
git -C "$dir" commit -m "chore(manifest): add dependency <name>" -- "$(basename "$manifest")"
git -C "$dir" push
```

## A new skill of our own

```sh
skenv new <name>                 # the private repository (default)
skenv new <name> --visibility public   # the public repository
skenv new <name> --dir <repo>    # any other git repository
```

It creates `skills/<name>/SKILL.md` with frontmatter and `references/`.
Fill in `description` (what the skill does and when to use it, at most 1024
characters) and the instructions, then `skenv lint skills/<name>` until it
is clean, commit (hooks run the same checks where a repository has them) and `skenv link`
(a skill in a checkout of the manifest reaches your agents this way; nothing to pull).
The name is lowercase letters, digits and hyphens and equals the directory
name.

## Checking a machine

`skenv doctor` compares the machine with the manifest without changing
anything: exit 0 means in sync, 1 lists discrepancies, 2 means it could not
run. What to do for each class: [doctor.md](references/doctor.md). Most
classes are fixed by `skenv sync`; `conflict` and `unmanaged` need a look
first.

## Where a skill belongs

| Skill | Repository |
| --- | --- |
| Own, not meant for others, or with internal context | private repository (default) |
| Based on a book, internal material or a copy of third-party work | private repository, never published |
| Own, general, passed the publication checklist | public repository |
| Someone else's | `[user.dependencies.<name>]` in the manifest |

## Checks

- `skenv lint [path]` — frontmatter, name, limits, relative links, file
  size and secret-like files, shebangs.
- `skenv lint --publish` — also license, `metadata.source`, the private
  stop-list and gitleaks over the whole history; required before
  publishing.
- `skenv repo check` — the managed files of a skills repository match its
  templates.

# Resolve conflicts and restore backups

skenv only replaces or removes paths it created itself (it records them in
`~/.local/state/skenv/state.json`). Anything else in the way of the
manifest is a **conflict**: skenv reports it and leaves it alone until you
decide.

- [Finding conflicts](#finding-conflicts)
- [Take over with `--adopt`](#take-over-with---adopt)
- [Keep what is installed](#keep-what-is-installed)
- [Restore a backup](#restore-a-backup)
- [Checkouts that are not pulled](#checkouts-that-are-not-pulled)
- [What `--dry-run` shows](#what---dry-run-shows)

## Finding conflicts

- `skenv sync` prints an error per conflict, goes on with the rest and
  exits 1:

  ```text
  error: conflict: ~/.claude/skills/<name> exists and is not managed by skenv (skill "<name>"); inspect it, then rerun with --adopt to move it to ~/.local/state/skenv/backup and replace it
  ```

- `skenv list` shows the skill with the state `conflict`.
- `skenv doctor` lists it with the class `conflict`, and anything in the
  store or an agent directory that the manifest does not know as
  `unmanaged` (see [`doctor` classes](list-skills.md#doctor-classes)).

Typical causes: a skill installed with `npx skills add` or copied by hand
under the same name, or a machine that had skills before its first
`skenv sync`.

## Take over with `--adopt`

```sh
skenv sync --dry-run --adopt   # which paths it would replace
skenv sync --adopt
skenv doctor
```

`--adopt` moves each conflicting path to
`~/.local/state/skenv/backup/<timestamp>/` and puts the managed skill in
its place. `link`, `vendor add|update|remove` and `sync` in a project take
`--adopt` too.

> [!CAUTION]
> `--adopt` takes over every conflicting path the manifest needs, including
> skills you installed by hand. They are moved, not deleted; restore from
> the backup if needed. Run the same command with `--dry-run` first. Without
> `--adopt` skenv never deletes or replaces a path it did not create, and
> entries matching `unmanaged` are left alone even with it.

Skills that the manifest does not list are never touched, with or without
`--adopt`. To bring them into the manifest first, use `skenv import` (see
[Adopt existing skills](adopting.md)).

## Keep what is installed

- **The other tool should keep the skill**: add its name (or a glob) to
  `unmanaged` in `[user]`; skenv then neither changes nor reports
  it (see [Manifest format](manifest.md#format)).
- **You do not want the manifest's skill here**: exclude it on this
  machine with `[user.machines."<name>"] exclude = ["<skill>"]` (see
  [machine rules](manifest.md#machine-rules)), or remove it from the
  manifest with `skenv vendor remove <skill>`.
- **An unmatched skill after `skenv import`**: compare the copy with the
  pinned commit, then `skenv sync --adopt` or `skenv vendor remove <name>`
  (see [Unmatched skills](adopting.md#unmatched-skills-and---sync)).

## Restore a backup

A backup keeps the path relative to your home directory:

```text
~/.local/state/skenv/backup/<timestamp>/.claude/skills/<name>
~/.local/state/skenv/backup/<timestamp>/.agents/skills/<name>
~/.local/state/skenv/backup/<timestamp>/.agents/.skill-lock.json   # skenv import
```

To go back to the old copy of a skill, first make sure skenv no longer
wants that path (`skenv vendor remove <name>`, an `exclude` in
the machine rules, or `unmanaged`), otherwise the next `sync` reports a conflict again. Then
move the directory back:

```sh
ls ~/.local/state/skenv/backup/
rm ~/.claude/skills/<name>                  # only if something is still there
mv ~/.local/state/skenv/backup/<timestamp>/.claude/skills/<name> ~/.claude/skills/
skenv doctor                                 # it is unmanaged now, as before
```

skenv never deletes backups; remove old ones yourself.

## Checkouts that are not pulled

`sync` runs `git pull --ff-only` in each checkout. A working copy
with uncommitted changes, or a branch that has diverged from its upstream,
is left as it is with a warning, and `sync` still exits 0. `skenv doctor`
reports it as `dirty`, `unpushed` or `behind`. Commit, push, rebase or
stash in that repository with git, then run `skenv sync` again.

## What `--dry-run` shows

`--dry-run` prints the plan and writes neither the skenv file, the store,
the agent directories nor the state. It is a preview with limits:

- `vendor add|update`, `import` and `init --import` still fetch into the
  clone cache `~/.cache/skenv/repos` to resolve commits (network access).
- `sync --dry-run` pulls nothing, so the plan uses the checkouts as they
  are now; when the manifest lives in one of them, changes pushed from
  another machine are not in the plan. Each such repository is marked in
  the output.

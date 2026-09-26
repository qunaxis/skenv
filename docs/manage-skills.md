# Add, update and remove skills

Every change goes through the manifest: skenv edits `skenv.toml` in place
(comments and order kept, in TOML, YAML or JSON), applies it on this
machine and prints the `git commit` command. Other machines get the change
after you commit **and push** it and they run `skenv sync`.

- [Add a third-party skill](#add-a-third-party-skill)
- [Update pinned skills](#update-pinned-skills)
- [Remove a skill](#remove-a-skill)
- [Add a checkout of your own skills](#add-a-checkout-of-your-own-skills)
- [Skills of a project](#skills-of-a-project)

Each command takes `--dry-run`; it writes nothing, but resolving a commit
still fetches into the clone cache `~/.cache/skenv/repos`. The flags are in
the [command reference](commands/README.md).

## Add a third-party skill

```sh
skenv vendor add <owner>/<repo> --path <skill-dir>
skenv vendor add <owner>/<repo> --path <skill-dir> --name <name> --rev <sha>
```

A third-party skill is a **dependency**, pinned: skenv records it as
`[user.dependencies.<name>]` with a full commit SHA and installs a copy of
the skill at that commit. Nothing changes until you
update it.

- `<owner>/<repo>` is on GitHub; `gitlab:group/sub/repo`,
  `codeberg:owner/repo`, `github:owner/repo`, an alias from `[user.git_hosts]` or a full URL
  work too (see [Git hosts](git-hosts.md)).
- `--path` is the directory of the skill inside that repository, written
  as `skill_dir`. Leave it out when the repository has exactly one `SKILL.md`.
- `--name`, the table key, defaults to the last element of `--path`,
  lowercased (the repository name for `.`).
- `--rev` sets `commit`; it defaults to HEAD of the default branch.

The skill is installed right away; check it with `skenv list`, then commit
and push the manifest:

```sh
git -C ~/src/<skills-repo> commit -m "chore(manifest): add dependency <name>" -- skenv.toml
git -C ~/src/<skills-repo> push
```

Reference: [skenv vendor add](commands/skenv_vendor_add.md).

## Update pinned skills

```sh
skenv vendor update                     # every dependency
skenv vendor update <name>...           # only these
skenv vendor update <name> --rev <sha>  # one skill, to a given commit
```

Each skill moves to HEAD of its default branch (or `--rev`) and is synced;
for each one skenv shows `git log --oneline old..new -- <skill_dir>`, so you see
what changed. `sync` alone never updates a pinned skill: applying the
manifest and upgrading are separate steps. Alias: `upgrade`. Reference:
[skenv vendor update](commands/skenv_vendor_update.md).

Checkouts need no update command: `skenv sync` fast-forwards them
(`git pull --ff-only`) on every run while they are clean and on their
branch (see [Checkouts and branches](manifest.md#checkouts-and-branches)).

## Remove a skill

```sh
skenv vendor remove <name>
```

Removes the entry from the manifest, and the store copy and links skenv
created for it. Shell completion offers the dependency names. Reference:
[skenv vendor remove](commands/skenv_vendor_remove.md).

A skill of a checkout is removed by deleting it from the repository (and
pushing), or by leaving it out of the checkout's selection (`include` or
`exclude`, see [Selecting skills](manifest.md#selecting-skills)); the next
`skenv sync` removes its links. The skill stays in the working copy.

## Add a checkout of your own skills

A checkout is a git repository with skills in `skills/<name>/` (or
`skills_dir`) that you edit, kept as a working copy. There is no command
for it: add a table to the manifest by hand, keyed by an ID of your
choice, then sync:

```toml
[user.checkouts.team-skills]
repo         = "<owner>/<team-skills>"
checkout_dir = "~/src/<team-skills>"
# include = ["review", "deploy"]   # only these (default: all)
```

```sh
skenv sync        # clones it to checkout_dir and links its skills
skenv list        # its skills are "editable"
```

The format, selection and machine rules are in
[Manifest format](manifest.md).

## Skills of a project

With `--project`, `vendor add`, `vendor update` and `vendor remove` edit
the `[project]` section of the current repository instead of your
manifest: skills committed with a project, for everyone who clones it.
See [Project skills](project-skills.md).

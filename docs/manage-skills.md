# Add, update and remove skills

Every change goes through the manifest: skenv edits `skenv.toml` in place
(comments and order kept, in TOML, YAML or JSON), applies it on this
machine and prints the `git commit` command. Other machines get the change
after you commit **and push** it and they run `skenv sync`.

- [Add a third-party skill](#add-a-third-party-skill)
- [Update pinned skills](#update-pinned-skills)
- [Remove a skill](#remove-a-skill)
- [Add a repository of your own skills](#add-a-repository-of-your-own-skills)
- [Skills of a project](#skills-of-a-project)

Each command takes `--dry-run`; it writes nothing, but resolving a commit
still fetches into the clone cache `~/.cache/skenv/repos`. The flags are in
the [command reference](commands/README.md).

## Add a third-party skill

```sh
skenv vendor add <owner>/<repo> --path <skill-dir>
skenv vendor add <owner>/<repo> --path <skill-dir> --name <name> --rev <sha>
```

A third-party skill is **pinned**: skenv records a full commit SHA and
installs a copy of the skill at that commit. Nothing changes until you
update it.

- `<owner>/<repo>` is on GitHub; `gitlab:group/sub/repo`,
  `codeberg:owner/repo`, an alias from `[environment.hosts]` or a full URL
  work too (see [Git hosts](git-hosts.md)).
- `--path` is the directory of the skill inside that repository. Leave it
  out when the repository has exactly one `SKILL.md`.
- `--name` defaults to the last element of `--path`, lowercased (the
  repository name for `.`).
- `--rev` defaults to HEAD of the default branch.

The skill is installed right away; check it with `skenv list`, then commit
and push the manifest:

```sh
git -C ~/src/<skills-repo> commit -m "chore(manifest): add vendor skill <name>" -- skenv.toml
git -C ~/src/<skills-repo> push
```

Reference: [skenv vendor add](commands/skenv_vendor_add.md).

## Update pinned skills

```sh
skenv vendor update                     # every pinned skill
skenv vendor update <name>...           # only these
skenv vendor update <name> --rev <sha>  # one skill, to a given commit
```

Each skill moves to HEAD of its default branch (or `--rev`) and is synced;
for each one skenv shows `git log --oneline old..new -- <path>`, so you see
what changed. `sync` alone never updates a pinned skill: applying the
manifest and upgrading are separate steps. Alias: `upgrade`. Reference:
[skenv vendor update](commands/skenv_vendor_update.md).

Own skills need no update command: `skenv sync` pulls their repositories
(`git pull --ff-only`) on every run.

## Remove a skill

```sh
skenv vendor remove <name>
```

Removes the entry from the manifest, and the store copy and links skenv
created for it. Shell completion offers the pinned names. Reference:
[skenv vendor remove](commands/skenv_vendor_remove.md).

A skill of an own repository is removed by deleting it from the repository
(and pushing), or by leaving it out of that entry's selection (`skills` or
`exclude`, see
[Selecting skills](manifest.md#selecting-skills-of-an-own-repository));
the next `skenv sync` removes its links.

## Add a repository of your own skills

An own repository is a git repository with skills in `skills/<name>/`
(or `skills_dir`) that you edit. There is no command for it: add an entry
to the manifest by hand, then sync:

```toml
[[environment.own]]
repo = "<owner>/<team-skills>"
path = "~/src/<team-skills>"
# skills = ["review", "deploy"]   # only these (default: all)
```

```sh
skenv sync        # clones it to path and links its skills
skenv list        # its skills are "editable"
```

The format, selection and per-host skips are in
[Manifest format](manifest.md).

## Skills of a project

With `--project`, `vendor add`, `vendor update` and `vendor remove` edit
the `[project]` section of the current repository instead of your
manifest: skills committed with a project, for everyone who clones it.
See [Project skills](project-skills.md).

# Create your first environment

Start here when no machine uses skenv yet and you have no skills installed
that you want to keep (for those, see [Adopt existing skills](adopting.md)).
You create a manifest in a git repository, install a skill from it, check
the machine, and push the manifest so other machines can use it.

- [1. Pick the repository](#1-pick-the-repository)
- [2. Start the manifest](#2-start-the-manifest)
- [3. Add a skill](#3-add-a-skill)
- [4. Apply](#4-apply)
- [5. Commit and push](#5-commit-and-push)
- [6. Verify](#6-verify)
- [Next steps](#next-steps)

## 1. Pick the repository

The manifest is the `[user]` section of `skenv.toml` at the root of a git
repository you own: usually a **private** skills repository, where
your own skills will live in `skills/<name>/`. The manifest is personal (it
names paths in your home directory, machine names and the skills you use),
so skenv refuses it in a repository whose `[repository]` says it is
public.

Any git repository works, new or existing:

```sh
mkdir -p ~/src/<skills-repo> && cd ~/src/<skills-repo>
git init
git remote add origin git@github.com:<owner>/<skills-repo>.git   # when it exists on the server
```

## 2. Start the manifest

```sh
cd ~/src/<skills-repo>
skenv init --dry-run   # what it would write
skenv init             # or: skenv init --format yaml (json)
```

`skenv init` adds `[user]` to the skenv file of the repository (or
creates `skenv.toml`), records the file as the manifest of this machine in
`~/.config/skenv/config.toml` and prints the next steps. It installs
nothing. Expected output:

```text
create ~/src/<skills-repo>/skenv.toml with [user], <owner>/<skills-repo> as its first checkout (<skills-repo>)
manifest ~/src/<skills-repo>/skenv.toml recorded in ~/.config/skenv/config.toml
next steps:
  - skenv vendor add <repo> --path <dir>         pin a third-party skill
  - skenv sync                                  link the skills of the manifest
  - commit and push skenv.toml; on another machine: skenv clone <owner>/<skills-repo>
```

The new manifest is not empty when the repository has an `origin` on a
network host: the repository itself is its first checkout, so every skill
in its `skills/` directory is linked on `sync`:

```toml
[user.checkouts.skills-repo]
repo         = "<owner>/<skills-repo>"
checkout_dir = "."
```

The table key (`skills-repo` here) is the checkout ID, derived from the
repository name. `checkout_dir = "."` is the repository that holds the
manifest, wherever it is cloned on each machine; a relative path resolves
against the directory of the skenv file.
A repository without an `origin` yet names its future remote with
`skenv init --remote <owner>/<skills-repo>`; GitLab, Codeberg and
self-hosted servers are covered in
[Git hosts](git-hosts.md#starting-a-manifest-with-skenv-init).

Two kinds of skills can be listed:

- **Checkouts** (`[user.checkouts.<id>]`): git repositories you work in.
  skenv clones them, fast-forwards clean working copies and links their
  skills, so they are editable in place. `include` and `exclude` choose
  which of their skills are installed; by default, all.
- **Dependencies** (`[user.dependencies.<name>]`): a skill from someone
  else's repository, copied at a full commit SHA. It changes only when you
  run `skenv vendor update`.

The whole format is in [Manifest format](manifest.md).

## 3. Add a skill

Pin a third-party skill; skenv finds the current commit, writes the entry
into `skenv.toml` and installs it:

```sh
skenv vendor add <owner>/<repo> --path <skill-dir>
```

```text
add dependency <skill-dir> (<owner>/<repo>@27f221f8f2a4, <skill-dir>) to ~/src/<skills-repo>/skenv.toml
vendor <skill-dir> from <owner>/<repo>@27f221f8f2a4 (<skill-dir>)
link ~/.claude/skills/<skill-dir> → ../../.agents/skills/<skill-dir>
...
manifest changed but not committed; to commit:
  git -C ~/src/<skills-repo> commit -m "chore(manifest): add dependency <skill-dir>" -- skenv.toml
vendor: 4 changes, 0 warnings, 0 errors
```

`--path` is the directory of the skill inside that repository (it becomes
`skill_dir`); leave it out when the repository has exactly one `SKILL.md`.
The name, the key of the `[user.dependencies.<name>]` table, defaults to
the last element of `--path`. More in
[Add, update and remove skills](manage-skills.md). To write a skill of your
own instead, see [Create a skill](create-skill.md).

## 4. Apply

```sh
skenv sync
skenv list
```

`skenv sync` links the skills of your checkout and anything else the
manifest lists. `skenv list` shows each skill with its state, `installed`
when it is in the store and linked into every agent directory found on
this machine. Start a new agent session to pick up new skills.

## 5. Commit and push

skenv never commits or pushes. Other machines, and `skenv clone` on them,
see only what is pushed:

```sh
git -C ~/src/<skills-repo> add skenv.toml
git -C ~/src/<skills-repo> commit -m "chore(manifest): start the manifest"
git -C ~/src/<skills-repo> push -u origin HEAD
```

## 6. Verify

```sh
skenv doctor
```

`skenv doctor` exits 0 with
`ok: N skills match ~/src/<skills-repo>/skenv.toml ...` when the machine
matches the manifest. It also checks the checkouts against their
upstream, so run it after the push: before it, the uncommitted
`skenv.toml` is reported as `dirty` and a branch without upstream as a
warning.

## Next steps

- [Connect another machine](another-machine.md): `skenv clone <owner>/<skills-repo>`
  there.
- [Create a skill](create-skill.md) in this repository.
- [Enable automatic sync](autostart.md) once manual syncs work.

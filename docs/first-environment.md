# Create your first environment

Start here when no machine uses skenv yet and you have no skills installed
that you want to keep (for those, see [Adopt existing skills](adopting.md)).
You create a manifest in a git repository, install a skill from it, check
the machine, and push the manifest so other machines can use it.

- [1. Pick the repository](#1-pick-the-repository)
- [2. Start the manifest](#2-start-the-manifest)
- [3. Add a skill](#3-add-a-skill)
- [4. Apply and verify](#4-apply-and-verify)
- [5. Commit and push](#5-commit-and-push)
- [Next steps](#next-steps)

## 1. Pick the repository

The manifest is the `[environment]` section of `skenv.toml` at the root of
a git repository you own: usually a **private** skills repository, where
your own skills will live in `skills/<name>/`. The manifest is personal (it
names paths in your home directory, host names and the skills you use), so
skenv refuses it in a repository whose `[repo]` says it is public.

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

`skenv init` adds `[environment]` to the skenv file of the repository (or
creates `skenv.toml`), records the file as the manifest of this machine in
`~/.config/skenv/config.toml` and prints the next steps. It installs
nothing. Expected output:

```text
create ~/src/<skills-repo>/skenv.toml with [environment], <owner>/<skills-repo> as its first own repository
manifest ~/src/<skills-repo>/skenv.toml recorded in ~/.config/skenv/config.toml
next steps:
  - skenv vendor add <repo> --path <dir>         pin a third-party skill
  - skenv sync                                  link the skills of the manifest
  - commit and push skenv.toml; on another machine: skenv clone <owner>/<skills-repo>
```

The new manifest is not empty when the repository has an `origin` on a
network host: the repository itself is its first own repository, so every
skill in its `skills/` directory is linked on `sync`:

```toml
[[environment.own]]
repo = "<owner>/<skills-repo>"
path = "~/src/<skills-repo>"
```

`path` is where the working copy lives on each machine; keep your checkout
there on every machine, or `sync` clones a second working copy at `path`.
A repository without an `origin` yet names its future remote with
`skenv init --remote <owner>/<skills-repo>`; GitLab, Codeberg and
self-hosted servers are covered in
[Git hosts](git-hosts.md#starting-a-manifest-with-skenv-init).

Two kinds of skills can be listed:

- **Own skills** (`[[environment.own]]`): git repositories you work in.
  skenv clones them, fast-forwards clean working copies and links their
  skills, so they are editable in place.
- **Pinned skills** (`[[environment.vendor]]`): a skill from someone else's
  repository, copied at a full commit SHA. It changes only when you run
  `skenv vendor update`.

The whole format is in [Manifest format](manifest.md).

## 3. Add a skill

Pin a third-party skill; skenv finds the current commit, writes the entry
into `skenv.toml` and installs it:

```sh
skenv vendor add <owner>/<repo> --path <skill-dir>
```

```text
add vendor <skill-dir> (<owner>/<repo>@27f221f8f2a4, <skill-dir>) to ~/src/<skills-repo>/skenv.toml
vendor <skill-dir> from <owner>/<repo>@27f221f8f2a4 (<skill-dir>)
link ~/.claude/skills/<skill-dir> → ../../.agents/skills/<skill-dir>
...
manifest changed but not committed; to commit:
  git -C ~/src/<skills-repo> commit -m "chore(manifest): add vendor skill <skill-dir>" -- skenv.toml
vendor: 4 changes, 0 warnings, 0 errors
```

`--path` is the directory of the skill inside that repository; leave it out
when the repository has exactly one `SKILL.md`. The name defaults to the
last element of `--path`. More in
[Add, update and remove skills](manage-skills.md). To write a skill of your
own instead, see [Create a skill](create-skill.md).

## 4. Apply and verify

```sh
skenv sync
skenv list
skenv doctor
```

`skenv sync` links the skills of your own repository and anything else the
manifest lists. `skenv list` shows each skill with its state, `installed`
when it is in the store and linked into every agent directory found on
this machine. `skenv doctor` exits 0 with
`ok: N skills match ~/src/<skills-repo>/skenv.toml ...` when the machine
matches the manifest. Start a new agent session to pick up new skills.

## 5. Commit and push

skenv never commits or pushes. Other machines, and `skenv clone` on them,
see only what is pushed:

```sh
git -C ~/src/<skills-repo> add skenv.toml
git -C ~/src/<skills-repo> commit -m "chore(manifest): start the manifest"
git -C ~/src/<skills-repo> push -u origin HEAD
```

## Next steps

- [Connect another machine](another-machine.md): `skenv clone <owner>/<skills-repo>`
  there.
- [Create a skill](create-skill.md) in this repository.
- [Enable automatic sync](autostart.md) once manual syncs work.

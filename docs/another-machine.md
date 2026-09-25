# Connect another machine

Your manifest already exists in a git repository (you created it with
`skenv init` or `skenv init --import` on another machine, then committed
and pushed it). This page sets up one more machine from it: clone the
repository, look at the plan, apply it and check the result.

- [Before you start](#before-you-start)
- [1. Clone the manifest repository](#1-clone-the-manifest-repository)
- [2. Preview and apply](#2-preview-and-apply)
- [3. Verify](#3-verify)
- [Already have a checkout](#already-have-a-checkout)
- [Keeping machines in step](#keeping-machines-in-step)

## Before you start

- skenv is [installed](install.md) and `git` can clone your repository:
  for a private one, the ssh key or credential helper you normally use
  (see [Authentication](git-hosts.md#authentication)).
- The manifest on the server has everything you want here. skenv never
  commits or pushes: a change that is only committed on another machine,
  or only in its working copy, does not reach this one. On the machine
  where you changed it:

  ```sh
  git -C ~/src/<skills-repo> status   # nothing to commit, and not ahead of origin
  git -C ~/src/<skills-repo> push
  ```

## 1. Clone the manifest repository

```sh
cd ~/src
skenv clone <owner>/<skills-repo>
```

`skenv clone` clones the repository into `./<skills-repo>` (or into the
directory you name: `skenv clone <owner>/<skills-repo> ~/src/my-skills`)
and records its skenv file as the manifest of this machine in
`~/.config/skenv/config.toml`. It installs nothing yet. Expected output:

```text
cloned <owner>/<skills-repo> into ~/src/<skills-repo>
manifest ~/src/<skills-repo>/skenv.toml recorded in ~/.config/skenv/config.toml
next steps:
  - skenv sync --dry-run    show what sync would change on this machine
  - skenv sync              apply it; --adopt backs up and replaces skills installed another way
```

`<owner>/<skills-repo>` is on GitHub. Use `gitlab:group/sub/repo`,
`codeberg:owner/repo` or a full git URL for other hosts (see
[Git hosts](git-hosts.md)).

The manifest usually lists its own repository as an own entry, and its
`path` is the working copy `sync` keeps up to date. Without a directory
argument, `skenv clone` puts a new clone at that `path` when nothing is
there yet (the output says `moved it to ...`). If you clone elsewhere,
`clone` warns: `sync` would keep a second working copy at `path` and never
pull the checkout that holds your manifest, so manifest changes pushed from
other machines would not arrive. Follow the advice in the warning: clone to
that path (`skenv clone <owner>/<skills-repo> <path>`), or `skenv use <path>`
when a working copy is already there. Until then `sync` warns and
`skenv doctor` reports `manifest-checkout` (exit 1). Changing `path` in the
manifest moves it on every machine, so do that only if every machine
should use the new location.

## 2. Preview and apply

```sh
skenv sync --dry-run
```

It prints one `would ...` line per change and a summary such as
`sync: 6 planned changes, 0 warnings, 0 errors`. Look for errors like
`error: conflict: ~/.claude/skills/<name> exists and is not managed by skenv`:
the machine already has a skill
of that name, installed by hand or with `npx skills`.

- **Nothing in the way**: apply it.

  ```sh
  skenv sync
  ```

- **Skills already installed here**: `--adopt` moves each conflicting path
  to `~/.local/state/skenv/backup/<timestamp>/` and replaces it with the
  managed skill. Preview it first:

  ```sh
  skenv sync --dry-run --adopt
  skenv sync --adopt
  ```

  Skills installed here that the manifest does not list stay as they are.
  To bring them into the manifest, see
  [Adopt existing skills](adopting.md#step-by-step).

`sync` ends with `sync: N changes, N warnings, N errors`. It exits 0 even
when it leaves something undone with a warning (an own repository with
uncommitted changes is not pulled, for example), so check the result.

## 3. Verify

```sh
skenv list
skenv doctor
```

`skenv list` shows the manifest, the store and the agent directories it
found, and every skill with its state; after a successful sync each one is
`installed` (or `not selected` / `skipped on this host`, if the manifest
says so):

```text
manifest  ~/src/<skills-repo>/skenv.toml
store     ~/.agents/skills
agents    ~/.claude/skills, ~/.pi/agent/skills

SKILL           KIND      SOURCE                  VERSION              STATE
code-review     editable  <owner>/<skills-repo>   ~/src/<skills-repo>  installed
diagrams        pinned    example-vendor/tools    27f221f8f2a4         installed
```

`skenv doctor` exits 0 and prints
`ok: N skills match ~/src/<skills-repo>/skenv.toml ...` when the machine
matches the manifest; otherwise it lists each discrepancy with the command
that fixes it (see [List installed skills](list-skills.md)). Start a new
agent session: agents read their skills directory when a session starts.

Once the manual sync works, you can let skenv sync at login and hourly:
see [Enable automatic sync](autostart.md).

## Already have a checkout

If the repository is cloned on this machine already, record it without the
repository address:

```sh
cd ~/src/<skills-repo>
skenv use .
```

`skenv use` accepts the skenv file or the directory that holds it, and
names the manifest it replaces, if any. Then continue with
[step 2](#2-preview-and-apply). skenv never selects a manifest from the
current directory by itself; `skenv use` is the only switch.

## Keeping machines in step

- A change made on one machine (`skenv vendor add`, a new skill, an edit of
  the manifest) reaches the others after you **commit and push** it there,
  and they run `skenv sync` (or autostart runs it within the hour).
- `skenv sync` pulls own repositories with `--ff-only`. A working copy with
  uncommitted changes or a diverged branch is left alone with a warning;
  `skenv doctor` reports it as `dirty`, `unpushed` or `behind`, and a
  manifest outside the working copy that `sync` pulls as
  `manifest-checkout`.
- `skenv sync --dry-run` does not pull, so it cannot show changes pushed
  from another machine that are not in the local working copy yet; the
  output marks such repositories.

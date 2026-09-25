<div align="center">

# skenv

**One manifest, the same agent skills on every machine.**

**[Documentation site: qunaxis.github.io/skenv](https://qunaxis.github.io/skenv/)**

[Install](#install) • [Getting started](#getting-started) • [Documentation](#documentation) • [Contribute](#contribute)

[![Latest release](https://img.shields.io/github/v/release/qunaxis/skenv?style=flat-square&label=release)](https://github.com/qunaxis/skenv/releases/latest)
[![CI](https://img.shields.io/github/actions/workflow/status/qunaxis/skenv/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/qunaxis/skenv/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/qunaxis/skenv?style=flat-square)](go.mod)

![Terminal demo: skenv clone and skenv sync clone a skills repository and link the same skills into Claude Code, pi and the Codex store; skenv list shows them installed, skenv doctor reports a deleted link, skenv sync restores it, and skenv vendor add pins a third-party skill to a commit](docs/demo/demo.gif)

</div>

---

## Table of contents

- [Introduction](#introduction)
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Install](#install)
- [Getting started](#getting-started)
- [Documentation](#documentation)
- [Roadmap](#roadmap)
- [Contribute](#contribute)

## Introduction

<!-- #region introduction -->
Agent skills pile up fast: some you write yourself, some you borrow from
other people's repositories, and each agent keeps them in its own
directory. Copying them by hand between Claude Code, Codex and pi, on a
laptop, a desktop and a server, drifts within a week.

`skenv` keeps the agent skills on a machine in sync with a declarative
manifest (the `[environment]` section of `skenv.toml`) that lives in a git
repository you own. Run
`skenv sync` on any machine and it ends up with the same skills, linked
into every agent that is installed there.

- **Own skills** live in git repositories you work in; skenv clones them and
  fast-forwards clean working copies.
- **Vendored skills** from other people's repositories are pinned to a full
  commit SHA and copied into the store.
- Everything is linked into each agent's skills directory.

Its design is guided by these mantras:

- **Declarative and pinned.** The manifest is the truth. Third-party skills
  are pinned to a full commit SHA, `sync` is idempotent, and upgrading a
  skill is an explicit `skenv vendor update` that shows you the log.
- **Never touch what you don't manage.** skenv records every path it
  creates and only ever replaces or removes those. Anything else is reported
  by `skenv doctor` and left alone unless you pass `--adopt`, which backs it
  up first.
- **Look before you leap.** `skenv doctor` and `--dry-run` leave your skills,
  links and skenv file alone and tell you what `sync` would do. They may
  still use the network: `doctor` runs `git fetch` in own repositories, and
  a `--dry-run` that resolves commits fetches into the clone cache.
- **Only `git` at runtime.** Managing and syncing skills needs nothing but
  `git`: no daemon, no service, no registry. skenv uses your normal git
  authentication (ssh key or credential helper) for private repositories.
  The optional tooling for skill authors (repository hooks and CI, the
  publication check) uses a few more tools, listed where you set it up.
- **Your layout, your names.** skenv hardcodes neither the name of your
  skills repository nor where you keep it. There is no default manifest
  location: you point skenv at yours once with `skenv init`, `skenv clone`
  or `skenv use`.
<!-- #endregion introduction -->

## Features

- Sync own skills (git working copies) and vendored skills (pinned copies)
  from one manifest into Claude Code, Codex and pi.
- `skenv init` starts a manifest, `skenv init --import` takes over the
  skills already installed, and `skenv clone <repo>` (or `skenv use .` in a
  checkout) connects another machine; none of them syncs until you run
  `skenv sync`.
- `skenv list` shows every skill of the manifest, editable or pinned, its
  source and version, and whether it is installed on this machine.
- `skenv doctor` compares the machine with the manifest and classifies every
  discrepancy (missing, conflict, wrong-rev, dirty, unpushed, …), as text or
  JSON.
- `skenv vendor add|update|remove` edit the manifest in place, keeping its
  comments and order, in TOML, YAML or JSON.
- Project skills: a `[project]` section pins skills into a project
  repository as committed copies, mirrors them into the directory of each
  agent, and `skenv doctor` checks them in CI
  ([project skills](docs/project-skills.md)).
- JSON Schemas for the skenv file and the tool config: files skenv writes
  name their schema, so editors complete and check them
  ([editor support](docs/editor-support.md)).
- A selection of skills per own repository (`skills`, `exclude`), per-host
  skips, custom agent directories, and ignore patterns for skills owned by
  other tools.
- `skenv autostart` runs `sync` at login and hourly via launchd or systemd.
- `skenv new` scaffolds a skill and lints it; `skenv lint` checks skills
  against the Agent Skills rules and, with `--publish`, runs a publication
  check before a skill goes public.
- Optional for skill authors: `skenv repo` generates and verifies a
  versioned harness for skills repositories (lefthook hooks, a CI
  workflow, linter configs and a Claude Code hook that lints skills as the
  agent edits them). Nothing else needs it.

## Prerequisites

- **Required:**
  - `git`
  - macOS or Linux, on amd64 or arm64
- **Agents** (skenv links into those that are installed):
  - [Claude Code](https://docs.anthropic.com/en/docs/claude-code):
    `$CLAUDE_CONFIG_DIR/skills`, else `~/.claude/skills`
  - [Codex](https://github.com/openai/codex): reads the store
    `~/.agents/skills` directly
  - pi: `~/.pi/agent/skills`
- **Optional:**
  - [`gh`](https://cli.github.com), logged in, for the install one-liner
  - Go 1.27.1 or newer, for `go install`
  - [gitleaks](https://github.com/gitleaks/gitleaks), for
    `skenv lint --publish`
  - for the repository checks of `skenv repo init|apply` only:
    [lefthook](https://github.com/evilmartians/lefthook),
    [gitleaks](https://github.com/gitleaks/gitleaks) and
    [uv](https://docs.astral.sh/uv/), which the generated hooks run
    (`skenv repo init` says which of them are missing)

## Install

<!-- #region install -->
Pre-built binaries for darwin/linux × amd64/arm64 are attached to every
[GitHub release](https://github.com/qunaxis/skenv/releases). Archives are
named `skenv_<version>_<os>_<arch>.tar.gz`, for example
`skenv_0.3.0_darwin_arm64.tar.gz`, and contain the `skenv` binary and
its man pages in `man/`. To install the latest binary into `~/.local/bin`:

```sh
mkdir -p ~/.local/bin
gh release download -R qunaxis/skenv \
  -p "skenv_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/).tar.gz" -O - \
  | tar xz -C ~/.local/bin skenv
```

`~/.local/bin` must be on your `PATH`. With Go installed you can use this
instead:

```sh
go install github.com/qunaxis/skenv/cmd/skenv@latest
```

The man pages (`man skenv`, `man skenv-vendor-add`, …) go into
`~/.local/share/man/man1`, which `man` searches when `~/.local/bin` is on
your `PATH`:

```sh
tmp="$(mktemp -d)"
gh release download -R qunaxis/skenv \
  -p "skenv_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/).tar.gz" -O - \
  | tar xz -C "$tmp"
mkdir -p ~/.local/share/man/man1
cp "$tmp"/man/*.1 ~/.local/share/man/man1/
rm -rf "$tmp"
```

From a checkout, `make man` writes the same pages into `man/`.

### Shell completion

`skenv completion bash|zsh|fish` prints a completion script for commands,
flags and the names of pinned skills. For zsh, for example:

```sh
skenv completion zsh > "${fpath[1]}/_skenv"
```

`skenv completion <shell> --help` says where each shell looks for it.

### Check the installation

```sh
skenv version
skenv --help
```

`skenv --help` starts with the first command for each situation. Nothing
is set up yet: continue with [Getting started](docs/getting-started.md).

> [!WARNING]
> **Upgrading from v0.3.0 or earlier:** `env.toml` and the old
> `skenv.toml` are no longer read. First move them into one `skenv.toml`
> with `[repo]` and `[environment]` sections, as described in
> [moving from `env.toml`](docs/skenv-file.md#moving-from-envtoml-and-the-old-skenvtoml).
> skenv also has no built-in default manifest location: record your
> checkout once with `skenv use <checkout>`; the clone is not touched,
> only its skenv file is recorded.
<!-- #endregion install -->

## Getting started

<!-- #region getting-started -->
skenv needs a **manifest**: the `[environment]` section of `skenv.toml` in
a git repository you own, usually a private skills repository. It lists
your own skills (git repositories you edit, kept as working copies) and
third-party skills (copies pinned to a commit). Start from the situation
that fits; each guide shows the expected output.

**Starting from scratch**: no manifest yet, no installed skills to keep.
See [Create your first environment](docs/first-environment.md).

```sh
cd ~/src/<skills-repo>                               # a git repository you own
skenv init                                           # adds [environment] to skenv.toml
skenv vendor add <owner>/<repo> --path <skill-dir>   # pin and install a skill
skenv sync                                           # link the skills of the manifest
skenv list                                           # every skill: installed
git add skenv.toml && git commit -m "chore(manifest): start the manifest" && git push
skenv doctor                                         # exit 0: the machine matches
```

**Adopting skills already installed** with `npx skills add -g`, by hand or
as links into clones. See [Adopt existing skills](docs/adopting.md).

```sh
cd ~/src/<skills-repo>
skenv init --import --dry-run   # what becomes managed, what is not imported
skenv init --import             # record them, back up and take them over
skenv list
git add skenv.toml && git commit -m "chore(manifest): import installed skills" && git push
skenv doctor
```

A skill whose installed commit skenv cannot find is recorded but left as
installed until you decide; the report says how.

**Connecting another machine** to a manifest you have pushed. See
[Connect another machine](docs/another-machine.md).

```sh
cd ~/src
skenv clone <owner>/<skills-repo>   # clone it and record its manifest; no sync
skenv sync --dry-run                # what sync would change here
skenv sync                          # or: skenv sync --adopt, if skills are installed here already
skenv list
skenv doctor
```

Already have a checkout? `skenv use .` inside it records it without the
repository address.

skenv never commits or pushes. A change reaches your other machines after
you commit **and push** it and they run `skenv sync`. Once manual syncs
work, [`skenv autostart enable`](docs/autostart.md) runs `sync` at login
and hourly. To write a skill of your own, `skenv new my-skill --dir .` in
the skills repository, then `skenv link` (see
[Create a skill](docs/create-skill.md)); it needs no hooks or CI.
<!-- #endregion getting-started -->

## Documentation

The same pages, with search, are published at
<https://qunaxis.github.io/skenv/>.

**Getting started**

- [Getting started](docs/getting-started.md): the three situations above.
- [Install](docs/install.md): binaries, `go install`, man pages and shell
  completion.
- [Create your first environment](docs/first-environment.md): `skenv init`,
  a first skill, `sync`, verify, commit and push.
- [Adopt existing skills](docs/adopting.md): `skenv init --import` and
  `skenv import` for skills installed with `npx skills` or by hand, the
  import report, and `skenv import --project`.
- [Connect another machine](docs/another-machine.md): `skenv clone`,
  `skenv use`, the first sync and how to verify it.

**Everyday tasks**

- [Commands by task](docs/commands.md): which command does what, and exit
  codes.
- [List installed skills](docs/list-skills.md): `skenv list`,
  `skenv doctor` and its classes.
- [Add, update and remove skills](docs/manage-skills.md): `skenv vendor`
  and own repositories.
- [Create a skill](docs/create-skill.md): `skenv new`, `skenv link`.
- [Resolve conflicts and restore backups](docs/conflicts.md): `--adopt`,
  backups, `--dry-run` limits.
- [Enable automatic sync](docs/autostart.md): `skenv autostart`.

**Reference**

- [Command reference](docs/commands/README.md): one page per command with
  every flag, generated from the command definitions by `make docs`.
- [Manifest format](docs/manifest.md): `[environment]`, where it is found,
  the layout on disk, and the mapping to `skills-lock.json`.
- [The skenv file](docs/skenv-file.md): `skenv.toml` with its `[repo]`,
  `[environment]` and `[project]` sections, formats, and moving from
  `env.toml`.
- [Machine configuration](docs/configuration.md): the tool config and
  precedence of flags, environment and file.
- [Git hosts and authentication](docs/git-hosts.md): `repo` forms for
  GitHub, GitLab, Codeberg and self-hosted servers, host aliases, ssh and
  authentication.
- [Project skills](docs/project-skills.md): `[project]`, skills committed
  with a project, mirrors, `doctor` in CI.
- [Editor support](docs/editor-support.md): JSON Schemas of the skenv file
  and the tool config, and editor setup.

**For skill authors**

- [Validation and publication](docs/lint.md): `skenv lint`, its rules and
  the publication check.
- [Repository checks and CI](docs/harness.md): the optional harness of
  `skenv repo init|apply|check`: hooks, the GitHub Actions and GitLab CI
  pipelines, runners and prerequisites.
- [Claude Code hook](docs/claude-code-hook.md): how skills are linted while
  an agent edits them.

**Contributing**

- [Development and releases](docs/releasing.md): `make` targets, commit
  rules, versioning and cutting a release.
- [Changelog](CHANGELOG.md).

`skenv --help` and `skenv <command> --help` print the command reference in
the terminal.

## Roadmap

Done:

- **v0.1** — `env.toml`, `sync`, `link`, `init`, `doctor`, `vendor
  add|bump|remove`, `autostart`.
- **v0.2** — `skenv lint` (L1–L6), the skills-repository harness
  (`skenv repo`), `layout.ignore`.
- **v0.3** — the publication check (`lint --publish`), the Claude Code hook,
  `skenv new`, harness 0.3.0.

Being considered next, in no particular order and with no dates:

- a Homebrew tap;
- stabilising the manifest format and the CLI for 1.0.

## Contribute

<!-- #region contribute -->
Issues and pull requests are welcome.

- Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
  in English, with a body that explains why. `make hooks` installs the
  lefthook `commit-msg` check; CI checks every commit of a pull request.
- Run `make check` before pushing: go vet, staticcheck, golangci-lint,
  `go test -race` and the commit check.
- Tests use a temporary `$HOME` and local bare repositories; never run
  `sync`, `link`, `init`, `clone`, `use` or `autostart` against a real home
  directory from tests.
- Coding agents: read [AGENTS.md](AGENTS.md).
- Releases are cut with `make release` only; `CHANGELOG.md` is generated.
  See [docs/releasing.md](docs/releasing.md).
<!-- #endregion contribute -->

# skenv

`skenv` keeps the agent skills on a machine in sync with a declarative
manifest, the same way on every machine, for Claude Code, Codex and pi.

- **Own skills** live in git repositories you work in; skenv clones them and
  fast-forwards clean working copies.
- **Vendored skills** from other people's repositories are pinned to a full
  commit SHA and copied into the store.
- Everything is linked into each agent's skills directory. skenv only ever
  replaces or removes paths it created itself; anything else is reported by
  `skenv doctor` and left alone unless you pass `--adopt`.

The only runtime dependency is `git`. It uses your normal git authentication
(ssh key or credential helper) for private repositories.

## Install

Pre-built binaries for darwin/linux × amd64/arm64 are attached to every
[GitHub release](https://github.com/qunaxis/skenv/releases). Archives are
named `skenv_<version>_<os>_<arch>.tar.gz`, for example
`skenv_0.1.0_darwin_arm64.tar.gz`, and contain the `skenv` binary.

A new machine from scratch (needs `gh`, logged in):

```sh
mkdir -p ~/.local/bin
gh release download -R qunaxis/skenv \
  -p "skenv_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/).tar.gz" -O - \
  | tar xz -C ~/.local/bin skenv
skenv init qunaxis/skills-private
skenv autostart enable
```

`~/.local/bin` must be on your `PATH`. With Go installed you can use
`go install github.com/qunaxis/skenv/cmd/skenv@latest` instead.

## Usage

| Command | What it does |
| --- | --- |
| `skenv init <owner/repo> [--path P]` | Clone the repository that holds `env.toml` (default `~/Personal/lab/<repo>`), record the manifest path in `~/.config/skenv/config.toml`, run `sync`. If the repository is already cloned, only the path is recorded. |
| `skenv sync [--adopt] [--dry-run] [--quiet]` | Clone or `pull --ff-only` own repositories (dirty or diverged copies are left alone with a warning), vendor pinned skills, link everything, remove managed paths that left the manifest. Idempotent. |
| `skenv link [--adopt] [--dry-run]` | Only the linking step of `sync`. |
| `skenv doctor [--json]` | Compare the machine with the manifest without changing anything. Exit code 0: in sync, 1: discrepancies, 2: could not run. |
| `skenv vendor add <owner/repo> [--path P] [--name N] [--rev SHA]` | Pin a third-party skill. Without `--path` the repository must contain exactly one `SKILL.md`; without `--rev` the HEAD of the default branch is used. The manifest is edited in place (comments and order kept) but not committed; skenv prints the commit command. |
| `skenv vendor bump <name> [--rev SHA]` | Move a vendored skill to a new commit, show `git log --oneline old..new -- path`, sync it. |
| `skenv vendor remove <name>` | Remove a vendored skill from the manifest and its managed paths. |
| `skenv autostart enable\|disable\|status` | Run `skenv sync --quiet` at login and hourly: a LaunchAgent `com.qunaxis.skenv` on macOS, a systemd user timer on Linux. Log: `~/.local/state/skenv/autostart.log`. |
| `skenv version` | Version, commit and build date. |

`--dry-run` prints the plan and changes nothing (`vendor add|bump --dry-run`
still fetch into the clone cache `~/.cache/skenv/repos` to resolve the commit). `--adopt` moves a
conflicting unmanaged path to `~/.local/state/skenv/backup/<timestamp>/` and
replaces it; the first run on a machine that already has skills installed is
usually `skenv sync --adopt`.

`doctor` reports these classes:

| Class | Meaning |
| --- | --- |
| `missing` | a skill is not in the store, or not linked for any agent; an own repository is not cloned |
| `agent-mismatch` | a skill is linked for some agents but not all |
| `extra-managed` | a path skenv created is no longer in the manifest (`sync` removes it) |
| `unmanaged` | something in the store or an agent directory that is not from the manifest |
| `conflict` | a path the manifest needs is taken by something skenv did not create |
| `wrong-rev` | a vendored copy does not match the pinned repo/path/rev |
| `broken-link` | a managed symlink dangles or points elsewhere |
| `dirty`, `unpushed`, `behind` | an own working copy has uncommitted changes, is ahead of or behind its upstream (after `git fetch`) |

## Manifest: `env.toml`

Found via `--manifest`, then `$SKENV_MANIFEST`, then `manifest` in
`~/.config/skenv/config.toml`, then `~/Personal/lab/skills-private/env.toml`.

```toml
[layout]
store   = "~/.agents/skills"                          # optional, this is the default
targets = ["~/.claude/skills", "~/.pi/agent/skills"]  # optional, replaces the agent table

[[own]]                        # your skills repository, kept as a working copy
repo = "qunaxis/skills-private"
path = "~/Personal/lab/skills-private"
skills_dir = "skills"          # optional, default "skills"

[[vendor]]                     # someone else's skill, pinned to a commit
name = "archify"
repo = "tt-a1i/archify"         # owner/repo on github.com or a full git URL
path = "archify"               # directory with SKILL.md; "." for the root
rev  = "<full 40-character commit SHA>"

[host."my-laptop"]             # optional, per hostname (full or short)
skip = ["bpmn-process-modeler"]
```

- Skill names are unique across own and vendor skills; a clash is an error.
- `rev` must be a full 40-character SHA.
- `owner/repo` is cloned from `https://github.com/owner/repo.git`. To use ssh,
  map it in git: `git config --global url."git@github.com:".insteadOf https://github.com/`.
- Skills listed in `host.<name>.skip` are neither stored nor linked on that host.
- `vendor add|bump|remove` edit the file as text: keep `[[vendor]]` tables in
  the multi-line form above with double-quoted `name` and `rev`.
- A vendored skill is copied as is, symlinks included; vendor only
  repositories you trust.

### Layout

- **Store** `~/.agents/skills`: own skills are symlinks to
  `<own.path>/<skills_dir>/<name>`; vendored skills are directories with a
  `.skenv` marker (`repo`, `path`, `rev`). Codex reads this directory
  directly.
- **Targets**: `<target>/<name>` is a relative symlink to the store entry.
  Built-in agents: Claude Code (`$CLAUDE_CONFIG_DIR/skills`, else
  `~/.claude/skills`) and pi (`~/.pi/agent/skills`), each only if its base
  directory (`~/.claude`, `~/.pi/agent`) exists. `layout.targets` replaces
  this table. `~/.claude/skills/synced` is never touched.
- **State** `~/.local/state/skenv/state.json` lists the paths skenv created.
  Vendor clones are cached in `~/.cache/skenv/repos/<owner>__<repo>`.

### Mapping to `skills-lock.json`

The vendor fields follow the project lock file of the
[`skills` CLI](https://github.com/vercel-labs/skills) (checked against 1.7.0),
so a manifest can be translated if skenv is ever replaced by it:

| `env.toml` `[[vendor]]` | `skills-lock.json` entry | Notes |
| --- | --- | --- |
| `name` | entry key | skill name |
| `repo` | `source` (+ `sourceType = "github"`) | `owner/repo`; a full URL maps to `source`/`sourceUrl` |
| `path` | `skillPath` | skills-lock stores the file: `archify` ↔ `archify/SKILL.md`, `.` ↔ `SKILL.md` |
| `rev` | `ref` | skenv requires a full SHA; `ref` also accepts branches and tags |
| — | `computedHash` | not recorded by skenv; the SHA pins the content |

`[[own]]` has no equivalent: the `skills` CLI does not manage working copies.

## Development

```sh
make hooks     # lefthook: commit-msg check, gofmt
make check     # go vet, staticcheck, golangci-lint, go test -race, commit check
make snapshot  # local goreleaser build into dist/
```

Integration tests run skenv against a temporary `$HOME` with local bare
repositories standing in for GitHub; they never touch your real home.

### Commits and releases

Commits follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):
`<type>(<scope>)?!?: <subject>` with type `feat`, `fix`, `perf`, `refactor`,
`docs`, `test`, `build`, `ci`, `chore` or `revert`, and scope the command or
subsystem (`sync`, `doctor`, `vendor`, `link`, `autostart`, `manifest`, …).
The lefthook `commit-msg` hook and the `conventional commits` CI job enforce
it for every commit, so pull requests are merged by squash or rebase only
(merge commits are disabled in the repository settings).

Versions are computed by [git-cliff](https://git-cliff.org) (`cliff.toml`)
from the commits since the last tag. Until 1.0.0:

| Commits since the last tag | Next version |
| --- | --- |
| breaking (`!` or `BREAKING CHANGE:`) | `0.(x+1).0` |
| `feat` | `0.(x+1).0` |
| `fix` or `perf` | `0.x.(y+1)` |
| anything else | no release |

`make release` computes the version, regenerates `CHANGELOG.md`, commits
`chore(release): vX.Y.Z`, creates an annotated tag, pushes, and runs
goreleaser with git-cliff release notes. It is safe to rerun until the tag
exists. Run it from the `release` workflow (Actions → release → Run
workflow) or locally:

```sh
GITHUB_TOKEN="$(gh auth token)" make release
```

`CHANGELOG.md` is generated; do not edit it by hand.

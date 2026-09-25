# Project skills: `[project]`

Project skills live in a project repository and are committed with it, so
everyone who clones the project gets them: teammates, CI and cloud agents,
none of whom have your `~/.agents/skills`. The `[project]` section of the
project's skenv file says where they live, which third-party skills and
which skills of your skills repositories are copied in, pinned to a commit,
and which other agent directories mirror them.

- [When to use project skills](#when-to-use-project-skills)
- [Quick start](#quick-start)
- [The `[project]` section](#the-project-section)
- [Layout in the repository](#layout-in-the-repository)
- [Project-own skills](#project-own-skills)
- [Mirrors: symlink or copy](#mirrors-symlink-or-copy)
- [Commands](#commands)
- [What to commit](#what-to-commit)
- [`doctor` classes in a project](#doctor-classes-in-a-project)
- [CI](#ci)
- [Examples](#examples)
- [With `[repo]` and `[environment]`](#with-repo-and-environment)

## When to use project skills

| Use                                     | when the skill                                                                    |
| --------------------------------------- | --------------------------------------------------------------------------------- |
| user-level skills ([manifest](manifest.md)) | is yours: you want it in every project, on every machine you work on              |
| project skills (this page)              | belongs to one project: its build, deploy or domain rules, or a third-party skill the whole team should use there |

User-level skenv links skills from your machine: symlinks into the store
and into your own working copies. That cannot work inside a project, because
nobody else has your store or your working copies. Project skills are therefore
**real files in the repository**. skenv copies pinned skills into the
project, checks that nobody edited the copies, and keeps the directories of
other agents in line.

## Quick start

In the root of the project repository, add a `[project]` section to its
skenv file (`skenv.toml`, or `skenv.yaml`, `skenv.yml`, `skenv.json`):

```toml
[project]
dir     = ".agents/skills"   # where the skills live (Codex reads it)
mirrors = [".claude/skills"] # Claude Code gets a symlink per skill
```

Then pin a third-party skill, check, and commit:

```sh
skenv vendor add <owner>/<repo> --path <skill-dir> --project
skenv doctor
git add skenv.toml .agents/skills .claude/skills
git commit -m "chore(skills): add project skills"
```

A project with skills from `npx skills add` (a `skills-lock.json` in its
root) starts with `skenv import --project` instead, which writes the
section and the entries for you; see
[Adopt existing skills](adopting.md#project-skills-import---project).

After a clone nothing needs to run: the skills are in the repository.
`skenv sync` in the project is only needed after you edit `[project]` by
hand or add a skill to `dir`.

## The `[project]` section

All paths are relative to the repository root and must stay inside it.

| Key            | Type            | Default            | Meaning                                                                                                                  |
| -------------- | --------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `dir`          | string          | `".agents/skills"` | The directory that holds the project's skills: the copies skenv makes and the skills authored there. Not the root.      |
| `mirrors`      | list of strings | `[]`               | Other agent directories that get every skill of `dir`, for example `.claude/skills`. They must not overlap `dir` or each other. |
| `hosts`        | table of tables | `{}`               | Git servers by alias for `repo` values of this section, the same keys as [`[environment.hosts.<alias>]`](git-hosts.md#declaring-a-self-hosted-host): `[project.hosts.<alias>]`. |
| `mirrors_mode` | string          | `"symlink"`        | `"symlink"`: `<mirror>/<name>` is a relative symlink to `<dir>/<name>`. `"copy"`: a full copy. See [Mirrors](#mirrors-symlink-or-copy). |
| `vendor`       | list of tables  | `[]`               | Third-party skills, one `[[project.vendor]]` table each.                                                                 |
| `from`         | list of tables  | `[]`               | Skills of a skills repository (your own, for example), one `[[project.from]]` table per repository and commit.           |

`[[project.vendor]]`: a third-party skill pinned to a commit. The same keys
as [`[[environment.vendor]]`](manifest.md#format); `skenv vendor add
--project` writes them.

| Key    | Type   | Default  | Meaning                                                                                                  |
| ------ | ------ | -------- | -------------------------------------------------------------------------------------------------------- |
| `name` | string | required | The skill name, and the directory `<dir>/<name>`: lowercase letters, digits and single hyphens.           |
| `repo` | string | required | `owner/repo` on github.com, `gitlab:group/sub/repo`, `codeberg:owner/repo`, `<alias>:path` of a host in `project.hosts`, or a full git URL; see [Git hosts](git-hosts.md). |
| `path` | string | `"."`    | The directory with `SKILL.md` inside the repository; `"."` for its root.                                 |
| `rev`  | string | required | A full 40-character lowercase commit SHA. Branches, tags and short SHAs are rejected.                    |

`[[project.from]]`: some skills of a skills repository, pinned to one
commit.

| Key          | Type            | Default    | Meaning                                                                                              |
| ------------ | --------------- | ---------- | ---------------------------------------------------------------------------------------------------- |
| `repo`       | string          | required   | The same forms as `repo` of `[[project.vendor]]`.                                                    |
| `skills_dir` | string          | `"skills"` | The directory inside the repository whose subdirectories are the skills.                             |
| `skills`     | list of strings | required   | The skills to copy, by directory name. Required and not empty: every copy is committed, so each is named. |
| `rev`        | string          | required   | A full 40-character lowercase commit SHA; every skill of the entry is copied at it.                  |

A skill name appears once across `vendor` and `from`. Skills from your own
repositories are copied at a pinned commit too, never linked to a working
copy on your machine: the project must build the same for everyone.

The whole section in TOML:

```toml
[project]
dir          = ".agents/skills"
mirrors      = [".claude/skills"]
mirrors_mode = "symlink"

[[project.vendor]]
name = "karpathy-coder"
repo = "alirezarezvani/claude-skills"
path = "engineering/karpathy-coder/skills/karpathy-coder"
rev  = "<full 40-character commit SHA>"

[[project.from]]
repo       = "<owner>/<skills-repo>"
skills_dir = "skills"
skills     = ["anti-slop-code", "commit-message"]
rev        = "<full 40-character commit SHA>"
```

The same in YAML (`skenv.yaml`):

```yaml
project:
  dir: .agents/skills
  mirrors: [.claude/skills]
  mirrors_mode: symlink
  vendor:
    - name: karpathy-coder
      repo: alirezarezvani/claude-skills
      path: engineering/karpathy-coder/skills/karpathy-coder
      rev: "<full 40-character commit SHA>"
  from:
    - repo: <owner>/<skills-repo>
      skills_dir: skills
      skills: [anti-slop-code, commit-message]
      rev: "<full 40-character commit SHA>"
```

And in JSON (`skenv.json`):

```json
{
  "project": {
    "dir": ".agents/skills",
    "mirrors": [".claude/skills"],
    "mirrors_mode": "symlink",
    "vendor": [
      {
        "name": "karpathy-coder",
        "repo": "alirezarezvani/claude-skills",
        "path": "engineering/karpathy-coder/skills/karpathy-coder",
        "rev": "<full 40-character commit SHA>"
      }
    ],
    "from": [
      {
        "repo": "<owner>/<skills-repo>",
        "skills_dir": "skills",
        "skills": ["anti-slop-code", "commit-message"],
        "rev": "<full 40-character commit SHA>"
      }
    ]
  }
}
```

A project resolves `repo` with its own `[project.hosts]` and the built-in
prefixes only, never with the hosts of anyone's manifest: every clone of
the project must resolve its entries the same way. A self-hosted server
used by the project is declared in the project:

```toml
[project.hosts.work]
url  = "https://git.example.com"
type = "gitlab"

[[project.vendor]]
name = "deploy-checklist"
repo = "work:platform/skills"
path = "skills/deploy-checklist"
rev  = "<full 40-character commit SHA>"
```

Quote `rev` in YAML: an all-digit SHA would otherwise be read as a number.
The [JSON Schema](editor-support.md) covers `[project]`, so editors
complete and check these keys.

## Layout in the repository

With the section above and a skill `deploy` written for the project,
`skenv sync` produces:

```text
my-project/
├── skenv.toml
├── .agents/skills/                    dir: real files, committed
│   ├── deploy/                        project-own: written here, no marker
│   │   └── SKILL.md
│   ├── karpathy-coder/                copied from [[project.vendor]]
│   │   ├── .skenv                     marker: repo, path, rev, hash
│   │   └── SKILL.md
│   ├── anti-slop-code/                copied from [[project.from]]
│   │   ├── .skenv
│   │   └── SKILL.md
│   └── commit-message/
│       ├── .skenv
│       └── SKILL.md
└── .claude/skills/                    a mirror
    ├── deploy -> ../../.agents/skills/deploy
    ├── karpathy-coder -> ../../.agents/skills/karpathy-coder
    ├── anti-slop-code -> ../../.agents/skills/anti-slop-code
    └── commit-message -> ../../.agents/skills/commit-message
```

The `.skenv` marker of a copy records where it came from (the clone URL
that `repo` resolves to) and the SHA-256 hash of its content (every file
with its executable bit, every symlink, without the marker itself):

```text
# managed by skenv, do not edit
repo = "https://github.com/alirezarezvani/claude-skills.git"
path = "engineering/karpathy-coder/skills/karpathy-coder"
rev = "<full 40-character commit SHA>"
hash = "sha256:<hex>"
```

The marker is how skenv tells its copies from your skills: there is no
state outside the repository. A directory in `dir` with a marker is skenv's
copy; one without is the project's own.

## Project-own skills

A skill written for the project goes straight into `dir`, with no entry in
`[project]`:

```sh
mkdir -p .agents/skills/deploy
$EDITOR .agents/skills/deploy/SKILL.md
skenv sync   # mirrors it
```

skenv does not change or remove a project-own skill; it only mirrors it.
An entry whose name is taken by a project-own skill is a `conflict` in
`sync` and `doctor`; rename one of them. `skenv vendor add --project`
refuses such a name before it edits anything (unless `--adopt`). Only
`--adopt` replaces a
project-own skill with the copy of an entry of the same name, after moving
it to the backup directory.

Edit a project-own skill in `dir`, not in a mirror: with symlink mirrors
both are the same files anyway, and with copy mirrors `sync` refuses to
overwrite a mirror copy that differs from `dir` and was not made by skenv.

## Mirrors: symlink or copy

Every agent reads its own directory: Codex reads `.agents/skills`, Claude
Code `.claude/skills`. One of them is `dir`, the others are mirrors, and
each mirror gets every skill of `dir`, project-own and copied alike.

| `mirrors_mode` | `<mirror>/<name>` is                          | Good                                                                 | Watch out                                                                                                     |
| -------------- | --------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `"symlink"`    | a relative symlink, `../../.agents/skills/<name>` | one copy of every file; mirrors cannot drift; small diffs         | git on Windows checks symlinks out as plain text files unless `core.symlinks` is on; tools that do not follow symlinks see nothing |
| `"copy"`       | a full copy with a `.skenv` marker (`mirror`, `hash`) | works for every tool and on every file system                   | every file twice in the repository and in diffs; an edit in the mirror is reported as `mirror-drift`         |

The symlinks are relative and point inside the repository, so git stores
them and they work after a clone on macOS and Linux. Switching the mode is a
`skenv sync`: it replaces symlinks with copies and back. skenv, like the
rest of its features, supports macOS and Linux; Windows checkouts are out of
scope, and `"copy"` is the mode to pick if Windows users read the
repository.

A mirror entry whose skill left `dir` is removed by `sync` when skenv made
it (a symlink into `dir`, or an unedited copy with a mirror marker).
Anything else in a mirror is left alone and reported as `unmanaged`: a
skill that lives only in `.claude/skills` is the drift this section exists
to prevent, so move it to `dir`.

## Commands

The commands find the project from the current directory: the root of its
git repository must hold a skenv file with `[project]`. Any directory inside
the repository works.

| Command                                  | In a project                                                                                                    |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `skenv sync`                             | copies every entry into `dir` at its `rev`, removes copies whose entry is gone, updates every mirror             |
| `skenv doctor`                           | compares the project with `[project]`, offline, and exits 1 on any discrepancy                                   |
| `skenv vendor add <repo> --project`      | adds a `[[project.vendor]]` entry, then syncs the project                                                       |
| `skenv vendor update [name...] --project` | moves entries to HEAD of their default branch (or `--rev`), then syncs; a skill of a `[[project.from]]` entry moves the whole entry |
| `skenv vendor remove <name> --project`   | removes a `[[project.vendor]]` entry, its copy and its mirrors                                                   |
| `skenv import --project`                 | pins the skills of `skills-lock.json` (`npx skills add`) in `[project]` and cleans the lock; see [Adopting](adopting.md#project-skills-import---project) |

- `sync` and `doctor` work on the project whenever they run inside one;
  `--project` makes that explicit and fails outside a project, and
  `--manifest` makes them work on your machine instead, from anywhere.
- `vendor` commands edit the manifest unless you pass `--project`.
- `--dry-run` prints the plan of every command and writes nothing in the
  project; `vendor add|update --dry-run` still fetch into the clone cache
  `~/.cache/skenv/repos` to resolve commits.
- `sync` never overwrites a copy that was edited locally, a project-own
  skill, or a directory in a mirror that differs from `dir` and was not
  made by skenv. It reports an error and goes on. `--adopt` moves such a
  path to `~/.local/state/skenv/backup/<timestamp>/` and replaces it: use
  it to restore an edited copy, or to take over copies installed by
  another tool. A symlink in a mirror that stays inside the repository
  holds no content and is pointed back at `dir`; one that leads out of it
  is a conflict too.
- A `dir` or mirror that leads out of the repository, or onto `dir` or
  another mirror, through a symlink is an error before anything changes.
- `sync` and `doctor` warn about files of `dir` and the mirrors that
  `.gitignore` keeps out of the commit (see [What to commit](#what-to-commit)),
  and `sync` about symlinks in a copy that point out of it.
- Neither command commits. After a change skenv prints the `git add` and
  `git commit` commands for the skenv file, `dir` and the mirrors.
- `vendor update` shows `git log --oneline old..new` of the skill's path
  (for a `[[project.from]]` entry, of its `skills_dir`), as it does for the
  manifest.
- A skill of a `[[project.from]]` entry is removed by editing the `skills`
  of that entry (or removing the entry) and running `skenv sync`.

## What to commit

Commit the skenv file (`skenv.toml` in the examples, or your
`skenv.yaml` or `skenv.json`), `dir` with every copy and its `.skenv`
marker, and every mirror: they are what teammates, CI and cloud agents read. Nothing
under `~` is involved.

Make sure `.gitignore` does not hide them. A pattern such as `.*`,
`.claude/` or `*.md` would leave copies half committed, and `doctor` in a
fresh clone would report them as `modified`, `conflict` or `missing`.
Check before the first commit:

```sh
git ls-files --others --ignored --exclude-standard -- .agents/skills .claude/skills
```

It lists the files of `dir` and the mirrors that `.gitignore` keeps out of
the commit, and prints nothing when none are. If a vendored skill ships files
your `.gitignore` excludes (build output, `*.log`), add a negation such as
`!.agents/skills/**` rather than editing the copy. Do not add the `.skenv`
markers to `.gitignore`: `doctor` needs them. `git add` stores the mirror
symlinks as symlinks; keep `core.symlinks` enabled (the default on macOS
and Linux).

A `.gitattributes` rule that converts line endings (`text=auto`,
`eol=crlf`) or sends files to Git LFS changes what a clone gets, and
`doctor` there reports `modified`. Exclude `dir` and the mirrors from such
rules, for example with `.agents/skills/** -text -filter`.

## `doctor` classes in a project

| Class           | Meaning                                                                                      | Fix                                                                                           |
| --------------- | -------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `missing`       | an entry of `[project]` has no copy in `dir`                                                  | `skenv sync`                                                                                  |
| `wrong-rev`     | the copy's marker names another repo, path or rev than the entry                             | `skenv sync`                                                                                  |
| `modified`      | the copy was edited: its content hash is not the one in its marker                            | move the change upstream or into a project-own skill, then `skenv sync --adopt` restores the copy |
| `extra-managed` | a copy (a directory with a marker) whose entry left `[project]`                              | `skenv sync` removes it; to keep it as a project-own skill, delete its `.skenv` instead        |
| `conflict`      | an entry's name is taken by a project-own skill                                              | rename the skill or the entry; `skenv sync --adopt` backs the skill up and copies the entry    |
| `broken-mirror` | a mirror entry is missing, points elsewhere, or belongs to a skill no longer in `dir`         | `skenv sync`                                                                                  |
| `mirror-drift`  | a mirror entry is a directory where a symlink is expected, a symlink where a copy is expected, or a copy that differs from `dir` | `skenv sync`; if the mirror was edited, move the change to `dir` first, then `skenv sync --adopt` |
| `unmanaged`     | a skill that is only in a mirror, not in `dir`                                               | move it to `dir`                                                                              |

`doctor` reads only the repository: no network, no cache, no state. Exit
code 0 when the project matches `[project]`, 1 on discrepancies, 2 when it
could not run. `--json` prints the report as JSON with `ok`, `file`,
`dir`, `mirrors`, `mirrors_mode`, `skills`, `issues` (`class`, `skill`,
`path`, `detail`) and `warnings`. A directory in `dir` without `SKILL.md` is
not a skill: `doctor` warns about it, and it is not mirrored.

## CI

`skenv doctor --project` fails the build when a copy was edited, a mirror
is broken or a pinned skill is missing. (A skills repository gets its pipeline
from the harness instead, for GitHub Actions or GitLab CI: see
[Harness: CI](harness.md#ci).) The job needs git and the skenv
binary from the [releases](https://github.com/qunaxis/skenv/releases);
set `SKENV_VERSION` to a release, without the `v` (the first release with
`[project]` or later), for a reproducible build, or leave it empty for the
latest release.

GitHub Actions (`.github/workflows/skills.yml`):

```yaml
name: skills
on: [push, pull_request]
jobs:
  doctor:
    runs-on: ubuntu-latest
    env:
      SKENV_VERSION: ""
    steps:
      - uses: actions/checkout@v4
      - name: install skenv
        run: |
          version=${SKENV_VERSION:-$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/qunaxis/skenv/releases/latest | sed 's|.*/v||')}
          arch=$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)
          mkdir -p "$RUNNER_TEMP/bin"
          curl -fsSL "https://github.com/qunaxis/skenv/releases/download/v$version/skenv_${version}_linux_${arch}.tar.gz" | tar xz -C "$RUNNER_TEMP/bin" skenv
          echo "$RUNNER_TEMP/bin" >> "$GITHUB_PATH"
      - run: skenv doctor --project
```

GitLab CI (`.gitlab-ci.yml`):

```yaml
skills:
  image: alpine:3.20
  variables:
    SKENV_VERSION: ""
  before_script:
    - apk add --no-cache git curl
    - git config --global --add safe.directory "$CI_PROJECT_DIR"
    - version=${SKENV_VERSION:-$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/qunaxis/skenv/releases/latest | sed 's|.*/v||')}
    - arch=$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)
    - curl -fsSL "https://github.com/qunaxis/skenv/releases/download/v$version/skenv_${version}_linux_${arch}.tar.gz" | tar xz -C /usr/local/bin skenv
  script:
    - skenv doctor --project
```

## Examples

Add a third-party skill to a project, at HEAD of its default branch:

```sh
cd ~/src/my-project
skenv vendor add alirezarezvani/claude-skills --path engineering/karpathy-coder/skills/karpathy-coder --project --dry-run
skenv vendor add alirezarezvani/claude-skills --path engineering/karpathy-coder/skills/karpathy-coder --project
git add skenv.toml .agents/skills .claude/skills
git commit -m "chore(skills): add karpathy-coder"
```

Add skills from your own skills repository: write the entry with the
commit to copy, then sync. The commit must be pushed, since skenv copies
from the remote; this prints the head of its default branch:

```sh
git ls-remote https://github.com/<owner>/<skills-repo> HEAD
```

```toml
[[project.from]]
repo   = "<owner>/<skills-repo>"
skills = ["anti-slop-code"]
rev    = "<full 40-character commit SHA>"
```

```sh
skenv sync
skenv doctor
git add skenv.toml .agents/skills .claude/skills
git commit -m "chore(skills): add anti-slop-code"
```

A single skill works with `vendor add` as well:
`skenv vendor add <owner>/<skills-repo> --path skills/anti-slop-code --project`.

Update every pinned skill, or one of them, and review the log it prints:

```sh
# every pinned skill
skenv vendor update --project
# or one skill
skenv vendor update karpathy-coder --project
# or one skill at a given commit
skenv vendor update karpathy-coder --rev <full 40-character commit SHA> --project
git add skenv.toml .agents/skills .claude/skills
git commit -m "chore(skills): update karpathy-coder"
```

Remove a skill with its copy and mirrors:

```sh
skenv vendor remove karpathy-coder --project
git add skenv.toml .agents/skills .claude/skills
git commit -m "chore(skills): remove karpathy-coder"
```

Take over the skills `npx skills add` installed, which keeps no pinned
commit: `skenv import --project` pins each entry of its `skills-lock.json`
to the commit it was installed from, adds `[project]` when the skenv file
has none, reports project-own skills that differ between agent
directories, and removes the imported entries from the lock; see
[Adopt existing skills](adopting.md#project-skills-import---project):

```sh
skenv import --project --dry-run
skenv import --project --sync
skenv doctor
git add -- skenv.toml .agents/skills .claude/skills skills-lock.json
git commit -m "chore(skills): import project skills"
```

`--sync` leaves a skill whose commit it could not match as installed, and
`doctor` reports it until you decide: `skenv sync --adopt` replaces it with
the pinned commit, `skenv vendor remove --project <name>` drops the entry.

A single copy another tool installed, without a lock: add an entry with
`--adopt`, which backs up the old copy and replaces it with the pinned one.
Entries written by hand work the same way: `skenv sync --adopt` replaces
every directory an entry names.

```sh
skenv vendor add <owner>/<repo> --path <skill-dir> --project --adopt
```

## With `[repo]` and `[environment]`

The three sections of a skenv file are independent. A project has
`[project]`; a skills repository that is also a project (its own skills in
`skills/`, the skills its development uses in `.agents/skills`) has
`[repo]` and `[project]`; the repository of your manifest may have all
three. `[project]` is allowed in public repositories, unlike
`[environment]`.

The user-level `skenv sync` reads only `[environment]`: it never copies the
project skills of a repository, not even of one listed in
`[[environment.own]]`. Project skills are synced only when skenv runs inside
that project. From there, `skenv sync --manifest <file>` syncs your machine.

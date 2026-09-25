# Commands

Every command prints its own usage with `skenv <command> --help`
(`skenv vendor add --help`, `skenv repo apply --help`, …). This page
collects them in one place.

- [Machine: init, sync, link, doctor, vendor, autostart](#machine)
- [`--dry-run` and `--adopt`](#--dry-run-and---adopt)
- [`doctor` classes](#doctor-classes)
- [Skills repositories: lint, new, repo](#skills-repositories)
- [Exit codes](#exit-codes)

Every command that reads the manifest accepts `--manifest FILE`; see
[where the manifest is found](manifest.md#where-the-manifest-is-found).

## Machine

| Command                                                                     | What it does                                                                                                                                                                                                                                                        |
| --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `skenv init <owner/repo> [--path P] [--adopt] [--dry-run]`                  | Clone the repository that holds `env.toml` into `P` (default `./<repo>` in the current directory, like `git clone`), record the manifest path in `~/.config/skenv/config.toml`, run `sync`. If the repository is already cloned, only the path is recorded.         |
| `skenv sync [--adopt] [--dry-run] [--quiet]`                                | Clone or `pull --ff-only` own repositories (dirty or diverged copies are left alone with a warning), vendor pinned skills, link everything, remove managed paths that left the manifest. Idempotent. `--quiet` prints only warnings and errors.                      |
| `skenv link [--adopt] [--dry-run]`                                          | Only the linking step of `sync`: store links for own skills and agent links for every skill.                                                                                                                                                                        |
| `skenv doctor [--json]`                                                     | Compare the machine with the manifest without changing anything. Exit code 0: in sync, 1: discrepancies, 2: could not run. `--json` prints the report as JSON.                                                                                                      |
| `skenv vendor add <owner/repo> [--path P] [--name N] [--rev SHA] [--dry-run]` | Pin a third-party skill. Without `--path` the repository must contain exactly one `SKILL.md`; without `--rev` the HEAD of the default branch is used; `--name` defaults to the last element of `--path`. The manifest is edited in place (comments and order kept) but not committed; skenv prints the commit command. |
| `skenv vendor bump <name> [--rev SHA] [--dry-run]`                          | Move a vendored skill to a new commit (default: HEAD of the default branch), show `git log --oneline old..new -- path`, sync it.                                                                                                                                   |
| `skenv vendor remove <name> [--dry-run]`                                    | Remove a vendored skill from the manifest and its managed paths.                                                                                                                                                                                                    |
| `skenv autostart enable\|disable\|status`                                   | Run `skenv sync --quiet` at login and hourly: a LaunchAgent `com.qunaxis.skenv` on macOS, a systemd user timer on Linux. Log: `~/.local/state/skenv/autostart.log`.                                                                                                 |
| `skenv version`                                                             | Version, commit and build date.                                                                                                                                                                                                                                     |

`vendor add|bump|remove` also accept `--adopt`.

## `--dry-run` and `--adopt`

`--dry-run` prints the plan and changes nothing (`vendor add|bump --dry-run`
still fetch into the clone cache `~/.cache/skenv/repos` to resolve the
commit).

`--adopt` moves a conflicting unmanaged path to
`~/.local/state/skenv/backup/<timestamp>/` and replaces it. The first run on
a machine that already has skills installed is usually `skenv sync --adopt`.
Without `--adopt` skenv never deletes or replaces a path it did not create.

## `doctor` classes

| Class                         | Meaning                                                                                             |
| ----------------------------- | --------------------------------------------------------------------------------------------------- |
| `missing`                     | a skill is not in the store, or not linked for any agent; an own repository is not cloned           |
| `agent-mismatch`              | a skill is linked for some agents but not all                                                       |
| `extra-managed`               | a path skenv created is no longer in the manifest (`sync` removes it)                               |
| `unmanaged`                   | something in the store or an agent directory that is not from the manifest                          |
| `conflict`                    | a path the manifest needs is taken by something skenv did not create                                |
| `wrong-rev`                   | a vendored copy does not match the pinned repo/path/rev                                             |
| `broken-link`                 | a managed symlink dangles or points elsewhere                                                       |
| `dirty`, `unpushed`, `behind` | an own working copy has uncommitted changes, is ahead of or behind its upstream (after `git fetch`) |

`doctor` also warns about own repositories whose `harness` is older than the
newest templates of the installed skenv (see [harness](harness.md)).

## Skills repositories

`skenv lint`, `skenv new` and `skenv repo` keep the repositories that hold
skills in shape. They work in any git repository with skills under
`skills/<name>/`.

### `skenv lint [path...] [--staged] [--publish]`

Checks every skill (a directory with `SKILL.md`) under the given paths
(default `.`); `--staged` checks only skills with files in the git index.
Inside git only the files git would commit are checked. Exit code 0: clean,
1: problems, 2: could not run.

| Rule | Check                                                                                                                         |
| ---- | ----------------------------------------------------------------------------------------------------------------------------- |
| L1   | YAML frontmatter is valid and has `name` and `description`                                                                    |
| L2   | `name` equals the directory name and uses only `[a-z0-9-]`                                                                    |
| L3   | Agent Skills limits: `name` ≤ 64 characters; `description` not empty, ≤ 1024; `license` a string, `metadata` a map of strings |
| L4   | relative links in `SKILL.md` and `references/*.md` point to existing files inside the skill                                   |
| L5   | no file larger than 10 MB; no `.env`, `*.pem`, `*.key`, `.credentials*`                                                       |
| L6   | executable files start with a shebang                                                                                         |

`--publish` adds the publication check P1, required in CI of public
repositories:

- every skill has a license: a non-empty `LICENSE*` file in the skill or a
  `license` field in the frontmatter;
- `metadata.source` is not `book`, `internal` or `third-party-copy`;
- no file path or text in the whole repository (every file git would
  publish, not only the skills) contains a phrase of the stop-list: one
  phrase per line, `#` comments, matched case-insensitively with any run of
  whitespace (line breaks and no-break spaces included) treated as one
  space, so wrapped phrases are found. The stop-list is read from
  `$SKENV_DENYLIST` or `~/.config/skenv/denylist.txt`, lives outside public
  repositories and is never printed: findings cite the stop-list line number
  and redact matching path components. Without a stop-list `--publish`
  refuses to run (exit 2);
- [gitleaks](https://github.com/gitleaks/gitleaks) finds no secret in the
  whole git history (redacted report with file, commit and rule). gitleaks
  must be installed for `--publish`.

`skenv lint --hook` is the Claude Code PostToolUse mode: it reads the hook
event on stdin and lints the skill of the edited file. See
[Claude Code hook](claude-code-hook.md).

### `skenv new <name> [--repo private|public] [--dir D]`

Creates `skills/<name>/` with `SKILL.md` (frontmatter with `name`, a TODO
`description`, `metadata.source: original`) and `references/notes.md`, then
lints it. The target is the own repository of the manifest whose
`skenv.toml` has the requested visibility (default `private`), or the git
repository at `--dir` (its `skenv.toml` decides the visibility there).

### `skenv repo init|apply|check`

Set up, regenerate and verify the harness of a skills repository (lefthook,
CI workflow, linter configs, managed blocks). See [harness](harness.md).

## Exit codes

`doctor`, `lint` and `repo check` share one convention: 0 when everything
is in order, 1 when they found problems, 2 when they could not run.

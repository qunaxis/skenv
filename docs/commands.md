# Commands

Every command prints its own usage with `skenv <command> --help`
(`skenv vendor add --help`, `skenv repo apply --help`, …). This page
collects them in one place. The per-command pages in [commands/](commands/README.md) are
generated from the same definitions by `make docs`, and so are the man pages
shipped in the release archives (`make man` writes them into `man/`; see
[Install](../README.md#install)); shell completion comes from
`skenv completion bash|zsh|fish`.

- [Machine: init, clone, use, import, sync, list, link, doctor, vendor, autostart](#machine)
- [Projects: sync, doctor, vendor --project](#projects)
- [`--dry-run` and `--adopt`](#--dry-run-and---adopt)
- [`doctor` classes](#doctor-classes)
- [Skills repositories: lint, new, repo](#skills-repositories)
- [JSON Schemas: schema](#json-schemas)
- [Exit codes](#exit-codes)

Every command that reads the manifest accepts `--manifest FILE`; see
[where the manifest is found](manifest.md#where-the-manifest-is-found).

## Machine

### `skenv init`

```sh
skenv init                  # in the git repository of the current directory
skenv init --format json    # skenv.json when the repository has no skenv file
skenv init --dir ~/src/my-skills --dry-run
```

Starts a manifest: it adds `[environment]` to the skenv file of the
repository (or creates one), records it in the tool config and prints the
next steps; see [Creating the file](skenv-file.md#creating-the-file). An
existing file keeps its format: `--format` that disagrees with it exits 2.
It takes no repository: `skenv clone` and `skenv use` connect a machine to
an existing manifest.

`skenv init --import` also imports the skills already installed on the
machine into the new manifest and runs `sync --adopt`, which leaves a skill
pinned without a matching commit as installed; see
[Adopting an existing setup](adopting.md).
Reference: [skenv init](commands/skenv_init.md).

### `skenv clone`

```sh
skenv clone <owner>/<repo>                    # into ./<repo>, like git clone
skenv clone <owner>/<repo> ~/src/my-skills
skenv clone <owner>/<repo> --format yaml      # a new config.yaml instead of config.toml
```

Clones the repository that holds the manifest (`skenv.toml` with
`[environment]`) and records the manifest path in
`~/.config/skenv/config.toml`. A directory that is a working copy of the
same repository already is used as it is; any other existing directory is
an error. It never syncs: run `skenv sync --dry-run`, then `skenv sync`.
`<owner>/<repo>` may also be `gitlab:group/sub/repo`, `codeberg:owner/repo`
or a full git URL (see [Git hosts](git-hosts.md)). Reference:
[skenv clone](commands/skenv_clone.md).

### `skenv use`

```sh
skenv use .                     # in the root of a checkout
skenv use ~/src/my-skills/skenv.toml
```

Records an existing manifest (a skenv file with `[environment]`, or its
directory) in the tool config, without its repository address, and names
the manifest it replaces. skenv never picks a manifest from the current
directory by itself. Reference: [skenv use](commands/skenv_use.md).

Without `<dir>`, `clone` puts a new clone at the `path` that the manifest
gives its own repository, when nothing is there yet. `clone` and `use` warn
when the manifest checkout is elsewhere: `sync` would keep a second working
copy at `path` and never pull the manifest checkout, so changes pushed from
other machines would not arrive. `sync` warns and `doctor` reports
`manifest-checkout` until you `skenv use` the working copy at `path`.

### `skenv import`

```sh
skenv import --dry-run   # the manifest diff and the lock changes, nothing written
skenv import
skenv import --sync      # then skenv sync --adopt, except for unmatched skills
skenv import --project   # in a project: its skills-lock.json into [project]
```

Adds the skills installed on the machine that the manifest does not have
yet: entries of the lock of the `skills` CLI (`~/.agents/.skill-lock.json`)
become vendor entries pinned to a commit, links into git working copies
become own repositories, and anything else is reported as not imported. The
skills now in the manifest leave that lock, after a backup, so the `skills`
CLI no longer updates them. The report groups the vendor entries by how
their commit was found: `exact` (the lock's hash), `same files` (the files
of the installed copy) or `unmatched` (neither: the tip of the branch, a
warning). `--sync` then takes over everything except the unmatched skills,
which stay as installed until you run `skenv sync --adopt` or
`skenv vendor remove <name>`. Idempotent. With `--project`, the same for the
`skills-lock.json` of the current repository: its entries become
`[[project.vendor]]`, pinned to the commit whose files have their
`computedHash`, and project-own skills that differ between agent
directories are reported. See
[Adopting an existing setup](adopting.md). Reference:
[skenv import](commands/skenv_import.md).

### `skenv sync`

```sh
skenv sync
skenv sync --quiet   # only warnings and errors
```

Clones or runs `pull --ff-only` on own repositories (dirty or diverged
copies are left alone with a warning), vendors pinned skills, links
everything and removes managed paths that left the manifest. Idempotent.
Also takes `--adopt` and `--dry-run`. Inside a project it syncs the
project instead (see [Projects](#projects)). Reference:
[skenv sync](commands/skenv_sync.md).

### `skenv list`

```sh
skenv list
skenv list --json
```

Lists the manifest, the store and the agent directories, then every skill
of the manifest: `editable` (an own skill, linked from its working copy) or
`pinned` (a vendored copy at a commit), its source, the commit or working
copy, and its state on this machine: `installed`, `not synced`, `conflict`,
`not selected` or `skipped on this host`. Offline and read-only; exit 0.
Reference: [skenv list](commands/skenv_list.md).

### `skenv link`

```sh
skenv link
```

Only the linking step of `sync`: store links for own skills and agent links
for every skill. Also takes `--adopt` and `--dry-run`. Reference:
[skenv link](commands/skenv_link.md).

### `skenv doctor`

```sh
skenv doctor
skenv doctor --json   # the report as JSON
```

Compares the machine with the manifest. It changes no skill, link or file,
but runs `git fetch` in each own repository (network access, and it updates
their remote-tracking branches) to report `unpushed` and `behind`. Exit
code 0: in sync, 1: discrepancies, 2: could not run. `sync` exits 0 even
when it leaves something undone with a warning (an own repository with
uncommitted changes is not pulled, for example), so `doctor` is the check
that the machine matches. See
[`doctor` classes](#doctor-classes). Inside a project it checks the
project instead (see [Projects](#projects)). Reference:
[skenv doctor](commands/skenv_doctor.md).

### `skenv vendor add`

```sh
skenv vendor add <owner>/<repo> --path <skill-dir>
skenv vendor add <owner>/<repo> --path <skill-dir> --name <name> --rev <sha>
```

Pins a third-party skill from any [git host](git-hosts.md): `owner/repo`
on GitHub, `gitlab:group/sub/repo`, `codeberg:owner/repo`, an alias
declared under `[environment.hosts]` or a full URL. Without `--path` the repository must contain
exactly one `SKILL.md`; without `--rev` the HEAD of the default branch is
used; `--name` defaults to the last element of `--path`. The manifest is
edited in place (comments and order kept) but not committed; skenv prints
the commit command. Also takes `--dry-run`. Reference:
[skenv vendor add](commands/skenv_vendor_add.md).

### `skenv vendor update`

```sh
skenv vendor update
skenv vendor update <name>...
skenv vendor update <name> --rev <sha>
```

Moves vendored skills to a new commit and syncs them: every vendored skill
without names, only the named ones otherwise. Each goes to HEAD of its
default branch; `--rev` pins a single named skill. For each skill it shows
`git log --oneline old..new -- path`. Alias: `upgrade`. Also takes
`--dry-run`. Reference:
[skenv vendor update](commands/skenv_vendor_update.md).

### `skenv vendor remove`

```sh
skenv vendor remove <name>
```

Removes a vendored skill from the manifest and its managed paths. Also takes
`--dry-run`. Reference:
[skenv vendor remove](commands/skenv_vendor_remove.md).

`vendor add`, `vendor update` and `vendor remove` also accept `--adopt`,
and `--project` to edit `[project]` of the current repository instead of
the manifest (see [Projects](#projects)).

### `skenv autostart`

```sh
skenv autostart enable
skenv autostart disable
skenv autostart status
```

`enable` runs `skenv sync --quiet` at login and hourly: a LaunchAgent
`com.qunaxis.skenv` on macOS, a systemd user timer on Linux. Log:
`~/.local/state/skenv/autostart.log`. `status` exits 1 when the job is not
installed and loaded. Reference:
[skenv autostart](commands/skenv_autostart.md).

### `skenv version`

```sh
skenv version
```

Prints the version, commit and build date. Reference:
[skenv version](commands/skenv_version.md).

## Projects

```sh
skenv sync                                              # inside a project: its [project]
skenv doctor --project                                  # fail outside a project, e.g. in CI
skenv vendor add <owner>/<repo> --path <skill-dir> --project
skenv vendor update [name...] --project
skenv vendor remove <name> --project
skenv import --project                                  # adopt the skills-lock.json of npx skills
skenv sync --manifest ~/src/my-skills                   # the machine, from inside a project
```

A project is a git repository whose skenv file, at its root, has a
`[project]` section. Inside one, `sync` copies its pinned skills into
`project.dir`, removes copies whose entry is gone and updates the mirrors;
`doctor` checks all that offline and exits 1 on the classes `missing`,
`wrong-rev`, `modified`, `extra-managed`, `conflict`, `broken-mirror`,
`mirror-drift` and `unmanaged`. `--project` requires a project,
`--manifest` works on the machine instead. The `vendor` commands edit
`[project]` only with `--project`. See [Project skills](project-skills.md).

## `--dry-run` and `--adopt`

`--dry-run` prints the plan and writes neither the skenv file, the store,
the agent directories nor the state. It is a preview with limits:

- `vendor add|update`, `import` and `init --import` still fetch into the
  clone cache `~/.cache/skenv/repos` to resolve commits (network access).
- `sync --dry-run` pulls nothing, so the plan uses the own repositories as
  they are now; when the manifest lives in one of them, changes pushed from
  another machine are not in the plan. Each such repository is marked in
  the output.

`--adopt` moves a conflicting unmanaged path to
`~/.local/state/skenv/backup/<timestamp>/` and replaces it. The first run on
a machine that already has skills installed is usually `skenv sync --adopt`.

> [!CAUTION]
> `--adopt` takes over every conflicting path the manifest needs, including
> skills you installed by hand. They are moved, not deleted, to
> `~/.local/state/skenv/backup/<timestamp>/`; restore from there if needed.
> Run the same command with `--dry-run` first to see what it would replace.
> Without `--adopt` skenv never deletes or replaces a path it did not
> create, and entries matching `layout.ignore` are left alone even with it.

## `doctor` classes

| Class                         | Meaning                                                                                             |
| ----------------------------- | --------------------------------------------------------------------------------------------------- |
| `missing`                     | a skill is not in the store, or not linked for any agent; an own repository is not cloned           |
| `agent-mismatch`              | a skill is linked for some agents but not all                                                       |
| `manifest-checkout`           | the manifest is not in the working copy its own entry names, so `sync` never pulls it: `skenv use` that working copy, or clone the repository there |
| `extra-managed`               | a path skenv created is no longer in the manifest, not selected, or skipped on this host (`sync` removes it) |
| `unmanaged`                   | something in the store or an agent directory that is not from the manifest                          |
| `conflict`                    | a path the manifest needs is taken by something skenv did not create                                |
| `wrong-rev`                   | a vendored copy does not match the pinned repo/path/rev                                             |
| `broken-link`                 | a managed symlink dangles or points elsewhere                                                       |
| `dirty`, `unpushed`, `behind` | an own working copy has uncommitted changes, is ahead of or behind its upstream (after `git fetch`) |

`doctor` also warns about own repositories whose `harness` is older than the
newest templates of the installed skenv (see [harness](harness.md)).
Inside a project `doctor` has its own classes; see
[`doctor` classes in a project](project-skills.md#doctor-classes-in-a-project).

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
| L2   | `name` equals the directory name and is lowercase letters, digits and single hyphens, with no hyphen at the start or end       |
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

> [!CAUTION]
> `--publish` fails closed: without a stop-list, with an empty one, or
> without gitleaks it exits 2 instead of passing. Run it before the first
> push to a public repository, not after: once pushed, content stays in
> the git history, forks and clones even if you delete it later.

`skenv lint --hook` is the Claude Code PostToolUse mode: it reads the hook
event on stdin and lints the skill of the edited file. See
[Claude Code hook](claude-code-hook.md).

### `skenv new <name> [--dir D] [--visibility private|public]`

Creates `skills/<name>/` with `SKILL.md` (frontmatter with `name`, a TODO
`description`, `metadata.source: original`) and `references/notes.md`, then
lints it. The target is the git repository at `--dir`, or else the only own
repository of the manifest; with several, the one whose `[repo]` has
`--visibility` (default `private`). No harness is needed. The `[repo]` of
the target decides its visibility, which only adds the publication step for
a public repository. It then prints the step that makes the skill available:
`skenv link` for a selected skill of an own repository, otherwise what keeps
it out of the manifest.

### `skenv repo init|apply|check`

Set up, regenerate and verify the harness of a skills repository (lefthook,
the GitHub Actions or GitLab CI pipeline, linter configs, managed blocks).
`repo init --ci github|gitlab` picks the CI system; without it, the host of
`origin` decides. See [harness](harness.md) and
[its CI section](harness.md#ci).

## JSON Schemas

### `skenv schema`

```sh
skenv schema          # the skenv file
skenv schema config   # the tool config
```

Prints the JSON Schema of the installed skenv version for the skenv file
or the tool config, for offline use and custom editor mappings. The files
skenv writes name their schema already. See
[Editor support](editor-support.md). Reference:
[skenv schema](commands/skenv_schema.md).

## Exit codes

`doctor`, `lint` and `repo check` share one convention: 0 when everything
is in order, 1 when they found problems, 2 when they could not run.

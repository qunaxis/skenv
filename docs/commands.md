# Commands by task

This page maps what you want to do to the command that does it. The flags
and arguments of every command are in the
[command reference](commands/README.md), generated from the command
definitions; `skenv <command> --help` prints the same in the terminal, and
the release archives carry it as man pages (`man skenv-vendor-add`).

`skenv` without a command (or `skenv --help`) starts with the first action
for each situation and lists the commands in groups: Get started,
Everyday, Write skills and Machine. A group such as `skenv vendor` prints
its subcommands. Every command that changes something ends its help with
what it reads, what it changes, whether it uses the network, how it treats
conflicts, what `--dry-run` previews and what to run next.

- [Get started](#get-started)
- [Everyday](#everyday)
- [Write skills](#write-skills)
- [Projects](#projects)
- [Machine](#machine)
- [Exit codes](#exit-codes)

## Get started

| Task | Command | Guide |
| ---- | ------- | ----- |
| Start a manifest in a git repository | [`skenv init`](commands/skenv_init.md) | [Create your first environment](first-environment.md) |
| Start one and take over the skills already installed | [`skenv init --import`](commands/skenv_init.md) | [Adopt existing skills](adopting.md) |
| Add installed skills to an existing manifest | [`skenv import`](commands/skenv_import.md) | [Adopt existing skills](adopting.md#step-by-step) |
| Set up a machine from your manifest repository | [`skenv clone <repo>`](commands/skenv_clone.md) | [Connect another machine](another-machine.md) |
| Use a checkout you already have | [`skenv use <path>`](commands/skenv_use.md) | [Already have a checkout](another-machine.md#already-have-a-checkout) |

`init`, `clone` and `use` record the manifest in the tool config and never
sync; `skenv sync` applies it. `init --import` and `import --sync` run
`skenv sync --adopt` for you, except for skills pinned without a matching
commit.

## Everyday

| Task | Command | Guide |
| ---- | ------- | ----- |
| Apply the manifest to this machine | [`skenv sync`](commands/skenv_sync.md) | [Connect another machine](another-machine.md#2-preview-and-apply) |
| See the skills and whether they are installed | [`skenv list`](commands/skenv_list.md) | [List installed skills](list-skills.md) |
| Check that the machine matches the manifest | [`skenv doctor`](commands/skenv_doctor.md) | [List installed skills](list-skills.md#skenv-doctor-does-the-machine-match) |
| Install a third-party skill, pinned to a commit | [`skenv vendor add`](commands/skenv_vendor_add.md) | [Add, update and remove skills](manage-skills.md) |
| Move pinned skills to newer commits | [`skenv vendor update`](commands/skenv_vendor_update.md) | [Update pinned skills](manage-skills.md#update-pinned-skills) |
| Remove a pinned skill | [`skenv vendor remove`](commands/skenv_vendor_remove.md) | [Remove a skill](manage-skills.md#remove-a-skill) |
| Link skills without pulling (after creating one) | [`skenv link`](commands/skenv_link.md) | [Create a skill](create-skill.md#3-make-it-available-to-your-agents) |
| Take over skills installed another way | `skenv sync --adopt` | [Resolve conflicts](conflicts.md) |

`--dry-run` previews `init`, `clone`, `use`, `import`, `sync`, `link`,
`vendor` and `repo init|apply`; its limits (fetches into the clone cache,
no pull) are in [What `--dry-run` shows](conflicts.md#what---dry-run-shows).
Every command that reads the manifest accepts `--manifest FILE`; see
[where the manifest is found](manifest.md#where-the-manifest-is-found).

## Write skills

| Task | Command | Guide |
| ---- | ------- | ----- |
| Scaffold a skill | [`skenv new <name> --dir .`](commands/skenv_new.md) | [Create a skill](create-skill.md) |
| Check skills; the publication check | [`skenv lint`](commands/skenv_lint.md), `skenv lint --publish` | [Validation and publication](lint.md) |
| Set up hooks and CI for a skills repository (optional) | [`skenv repo init`](commands/skenv_repo_init.md) | [Repository checks and CI](harness.md) |
| Move a repository to the current templates | [`skenv repo apply`](commands/skenv_repo_apply.md) | [Harness versions](harness.md#harness-versions) |
| Compare the managed files with the templates | [`skenv repo check`](commands/skenv_repo_check.md) | [Repository checks and CI](harness.md#skenv-repo-check) |

## Projects

Inside a git repository whose skenv file has a `[project]` section, `sync`
and `doctor` work on the skills committed with the project, and the
`vendor` commands edit `[project]` with `--project`:

```sh
skenv sync                                              # inside a project: its [project]
skenv doctor --project                                  # fail outside a project, e.g. in CI
skenv vendor add <owner>/<repo> --path <skill-dir> --project
skenv import --project                                  # adopt the skills-lock.json of npx skills
skenv sync --manifest ~/src/my-skills                   # the machine, from inside a project
```

See [Project skills](project-skills.md).

## Machine

| Task | Command | Guide |
| ---- | ------- | ----- |
| Sync at login and hourly | [`skenv autostart enable`](commands/skenv_autostart_enable.md) | [Enable automatic sync](autostart.md) |
| Shell completion | [`skenv completion bash\|zsh\|fish`](commands/skenv_completion.md) | [Install](install.md#shell-completion) |
| JSON Schema of the skenv file or the tool config | [`skenv schema`](commands/skenv_schema.md) | [Editor support](editor-support.md) |
| Version, commit and build date | [`skenv version`](commands/skenv_version.md) | |

## Exit codes

Every command exits 0 on success, 1 when it found problems or reported
errors (a conflict in `sync`, for example) and 2 when it could not run (a usage error, a missing argument, an unreadable file).
Warnings do not change the exit code: `sync` exits 0 when it leaves an own
repository with uncommitted changes unpulled, for example. `doctor`, `lint`
and `repo check` exit 0 only when everything is in order, so use `doctor`
to confirm that a machine matches its manifest.

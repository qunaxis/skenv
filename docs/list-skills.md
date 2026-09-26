# List installed skills

Two commands tell you what a machine has: `skenv list` is the inventory,
`skenv doctor` is the check.

- [`skenv list`: what the manifest has](#skenv-list-what-the-manifest-has)
- [`skenv doctor`: does the machine match](#skenv-doctor-does-the-machine-match)
- [`doctor` classes](#doctor-classes)

## `skenv list`: what the manifest has

```sh
skenv list
skenv list --json
```

```text
manifest  ~/src/skills/skenv.toml
store     ~/.agents/skills
agents    ~/.claude/skills, ~/.pi/agent/skills

SKILL           KIND      SOURCE                VERSION       STATE
code-review     editable  example-org/skills    ~/src/skills  installed
commit-message  editable  example-org/skills    ~/src/skills  installed
diagrams        pinned    example-vendor/tools  27f221f8f2a4  installed
write-tests     editable  example-org/skills    ~/src/skills  not synced
1 not synced: run `skenv sync`
```

The header names the active manifest, the store (`~/.agents/skills`, which
Codex reads directly) and the agent directories found on this machine.
Then one line per skill of the manifest:

- **KIND**: `editable` for a skill of a checkout, linked from its git
  working copy; `pinned` for a dependency, a copy at a commit.
- **SOURCE**: the repository as the manifest writes it.
- **VERSION**: the commit of a pinned skill, the working copy of an
  editable one.
- **STATE**: `installed` (in the store and linked into every agent
  directory), `not synced` (recorded in the manifest, not installed yet:
  run `skenv sync`), `conflict` (something skenv did not create is in the
  way: see [Resolve conflicts](conflicts.md)), `not selected` (left out by
  `include`/`exclude` of its checkout) or `excluded on this machine` (by
  the [machine rules](manifest.md#machine-rules)).

`list` works offline, changes nothing and exits 0. It covers the manifest
of the machine only; the skills of a project are committed files, and
`skenv doctor --project` checks them (see [Project skills](project-skills.md)).
Reference: [skenv list](commands/skenv_list.md).

## `skenv doctor`: does the machine match

```sh
skenv doctor
skenv doctor --json
```

When everything matches:

```text
ok: 3 skills match ~/src/skills/skenv.toml (store ~/.agents/skills, targets ~/.claude/skills, ~/.pi/agent/skills)
```

Otherwise a table of discrepancies, each with the command that fixes it,
and exit code 1:

```text
CLASS           SKILL        PATH                          DETAIL
agent-mismatch  code-review  ~/.claude/skills/code-review  linked for some agents but not this one; run `skenv link`
1 discrepancies
```

`doctor` changes no skill, link or file, but runs `git fetch` in each
checkout (network access; it updates their remote-tracking branches) to
report `unpushed` and `behind`. Exit code 0: in sync, 1: discrepancies, 2:
could not run. `sync` exits 0 even when it leaves something undone with a
warning, so `doctor` is the check that the machine matches. Inside a
project it checks the project instead. Reference:
[skenv doctor](commands/skenv_doctor.md).

## `doctor` classes

| Class                         | Meaning                                                                                             |
| ----------------------------- | --------------------------------------------------------------------------------------------------- |
| `missing`                     | a skill is not in the store, or not linked for any agent; a checkout is not cloned                  |
| `agent-mismatch`              | a skill is linked for some agents but not all                                                       |
| `manifest-checkout`           | the manifest is not in the working copy its checkout names, so `sync` never pulls it: `skenv use` that working copy, or clone the repository there |
| `extra-managed`               | a path skenv created is no longer in the manifest, not selected, or excluded on this machine (`sync` removes it) |
| `unmanaged`                   | something in the store or an agent directory that is not from the manifest                          |
| `conflict`                    | a path the manifest needs is taken by something skenv did not create                                |
| `wrong-rev`                   | a dependency's copy does not match its `repo`, `skill_dir` or `commit`                             |
| `broken-link`                 | a managed symlink dangles or points elsewhere                                                       |
| `dirty`, `unpushed`, `behind` | a checkout has uncommitted changes, is ahead of or behind its upstream (after `git fetch`) |

`doctor` also warns about checkouts whose `template_version` is older than
the templates of the installed skenv; `skenv repo upgrade` there moves
them (see
[Repository checks and CI](harness.md)). Inside a project `doctor` has its
own classes; see
[`doctor` classes in a project](project-skills.md#doctor-classes-in-a-project).

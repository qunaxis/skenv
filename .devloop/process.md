# Development process

How agents work on skenv issues with the devloop skills. Board:
https://github.com/users/qunaxis/projects/3. Project values: `devloop.toml`.
Code rules: `AGENTS.md`.

## Roles

- **Executors**: one agent per issue, one worktree, one branch, one PR with
  `Closes #N`. They take a task to In review.
- **Coordinator**: a long-running session with `devloop-orchestrate`; runs
  waves of up to `max_workers` tasks, reviews, merges one PR at a time, moves
  the board.
- **Owner**: reads the board, answers Blocked tasks (label `needs-human`),
  decides anything marked "owner decision".

## Executor model

Each task carries a label `model:haiku|sonnet|opus`, chosen by the task's
author from its complexity and the size of the change:

| label | when |
|---|---|
| `model:haiku` | mechanical work with exact instructions: every place listed, commands given |
| `model:sonnet` | standard refactoring or feature work with local judgment |
| `model:opus` | design decisions, ambiguity, high blast radius, planning |

The coordinator launches the executor with that model (`pm.py claim` prints
it); an executor started by hand runs `pm.py next --model <its model>` and
skips stronger tasks. Never go below the label. If an executor fails on its
model, retry one level up and fix the label with a comment.

## Ready contract

A task is Ready when it has one observable result, exact files or symbols,
steps, acceptance criteria an agent can check, verification commands, "out of
scope", and a model label. Dependencies are written as `Blocked by: #N` and
as GitHub's native "blocked by" relationship; `pm.py next` honours both, so a
Ready task with open blockers simply waits. A placeholder task stays Backlog
until its instructions are written.

## Who writes which files

| file | who | where |
|---|---|---|
| `.devloop/status.md` | whoever merges (coordinator, or the executor after its own merge) | `main` only, via `devloop-status` |
| `docs/adr/` | only with the owner's decision | task branch |
| `CHANGELOG.md`, tags | nobody by hand: `make release` | — |
| everything else | the executor whose task changes it | task branch |

## Where results and decisions go

No journal or decision files. The PR description is the task's record: what
changed, how it was verified, tactical decisions ("chose X because Y"), and
follow-ups, each filed as a new issue and linked. `git blame` leads to the PR.

A decision that constrains future code must be visible where the next agent
works, not in a closed issue: a why-comment at the code it governs, or an ADR
in `docs/adr/` for formats and architecture (owner's approval). A reviewer
treats such a decision without a comment or ADR as a blocker.

## Merge queue

1. Merge one PR at a time (squash; the PR title is a Conventional Commit and
   lands in `main`). After each merge, check `mergeable` of the open PRs.
2. A conflicting PR is rebased by its executor, not in `main`.
3. `.devloop/status.md` is updated once per wave, after the wave's PRs.
4. Wave 1 of the simplification epic (#60 → #61 → #62) touches most files:
   while one of them is open, start no other code task.

## Review

The coordinator reviews every PR with the `review-plan` skill: its
`scripts/plan.py` picks the review kinds (`review-acceptance`,
`review-correctness`, `review-contract`, `review-tests`, `review-deps`,
`review-slop` and others) from the paths, importers and size of the diff and
from `review:<kind>` labels on the task. skenv's path overrides are in
`[review.paths]` of `devloop.toml`. The executor does not review its own PR:
it fixes every `blocker` in the review threads, answers in each thread with
the commit SHA and pushes; the next round reviews the delta, at most three
rounds, then the task goes to Blocked. Style and extra scope become follow-up
issues.

`MERGE_READY` means: CI is green (`gh pr checks <pr> --watch`), the
`review-plan` round has no open `blocker` thread (`publish.py gate` exits 0),
the title is a Conventional Commit with the issue number, all acceptance boxes
are ticked, and a test claimed to catch a defect was shown red on the code
before the fix.

## Definition of done

1. Acceptance criteria met and ticked in the issue.
2. Behaviour changes come with a failing-first test; structural changes keep
   tests untouched except for renamed identifiers.
3. `make check` passes on the PR head.
4. Docs impact handled: the PR lists updated files or says
   `Docs: none — <reason>` (see AGENTS.md).
5. Board status Done; a short result comment on the issue.

# Status

Updated: 2026-09-29 (orchestration closed).

## Где мы

- Epic #76 (docs harness, #77-#83) — Done, closed.
- Epic #53 (simplification) — Done, closed. All of #54-#75 merged: package
  structure, naming (`feature-sliced internal/`, `repository` terminology),
  the CLI layer, docs and a test safety net, plus the final complexity pass
  (#75, PR #114) that re-measured against v0.6.0 and closed out the
  remaining hotspots (`importUser`, `ImportProject`, `Explain`, `Doctor`,
  `syncMirror`, `planManifest`, `schemagen.skenv` split into per-`$def`
  functions).
- Notable breaking changes in this batch (marked `!`, no back-compat
  guarantee before 1.0): `internal/` reorganized into
  features/model/platform/devtools (#61), "repository" as the single term
  for the harness (#62), skenv/config files validated against the embedded
  JSON Schemas (#73).
- Metrics at close (v0.6.0 -> HEAD `05d4837`): gocognit findings 70 -> 50,
  dupl 14 -> 0, funlen 1 -> 0, cross-package coverage 87.9% -> 88.2%
  (explained: uncovered statement count flat at 841, denominator shrank
  after splitting `schemagen.skenv`). Full table in issue #75's closing
  comment.
- HEAD is 05d4837; range reviewed since last status: abb8407..HEAD.

## Доска

- Done: 31 (all of #54-#75, #76-#83).
- In progress / Backlog / Blocked: none.
- #110 (bug: `insertTable` double-blank-line before a new TOML section) is
  open but untriaged — filed as a follow-up during #69, not part of any
  epic, not on the board yet.

## Открытые вопросы

- None blocking. #110 needs owner triage (phase/priority) whenever picked
  up; it is a real but low-severity formatting bug, not urgent.

## Следующие шаги

- No Ready work queued. Next devloop-orchestrate session starts by checking
  for new issues/epics on the board.

## Договорённости

- devloop.toml has `journal = ""` and `decisions = ""` for this project: PR
  descriptions carry task detail and tactical decisions instead of per-task
  journal/decision files; lasting technical decisions go into why-comments
  or `docs/adr/`.
- #75 was delivered as a single PR (#114) covering all ten of its steps, an
  explicit owner exception to the epic's normal "one structural change per
  PR" rule, made because the steps were small, independently reviewed by
  commit, and closing out the epic mattered more than PR granularity here.

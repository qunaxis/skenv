# Status

Updated: 2026-09-29.

## Где мы

No open epics, no Ready or in-progress work on the board. `gh issue list` /
the project board are authoritative for what shipped; this file only tracks
what's still open or agreed and not yet visible elsewhere.

## Открытые вопросы

- #110 (bug: `insertTable` double-blank-line before a new TOML section) is
  open but untriaged — not on the board, no phase/priority yet.

## Договорённости

- devloop.toml has `journal = ""` and `decisions = ""` for this project: PR
  descriptions carry task detail and tactical decisions instead of per-task
  journal/decision files; lasting technical decisions go into why-comments
  or `docs/adr/`.

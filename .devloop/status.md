# Status

Updated: 2026-09-29.

## Где мы

- Epic #76 (docs harness, #77-#83) — Done, closed. Docs, AGENTS.md, README and
  the shipped skenv skill are kept in step with code changes by CI (#83) and
  the pinned-doc tests (#79-#82).
- Epic #53 (simplification) is effectively complete: all of #54-#74 are Done
  and merged, across package structure, naming (`feature-sliced internal/`,
  `repository` terminology), the CLI layer, docs and a test safety net. The
  epic issue itself still shows "In progress" on the board because it stays
  open until #75 is resolved.
- #75 is a placeholder ("final pass after the simplification epic") left in
  Backlog: it names an intended scope (re-run size/complexity metrics,
  decide on remaining hotspots like `importUser`, `ImportProject`, `Explain`,
  `Doctor`, `syncMirror`, `planManifest`, `schemagen.skenv`) but has no
  concrete instructions yet — see "Открытые вопросы".
- Notable breaking changes landed in this batch (marked `!`, no back-compat
  guarantee before 1.0): `internal/` reorganized into
  features/model/platform/devtools (#61), "repository" as the single term
  for the harness (#62), skenv/config files now validated against the
  embedded JSON Schemas (#73).
- HEAD is f96e7c6; range reviewed: 3d25b43e..HEAD (28 merged commits).

## Доска

- Done: 29 (all of #54-#74, #76-#83, plus #58/#59 docs pass).
- In progress: 1 (#53, the epic itself, open pending #75).
- Backlog: 1 (#75).
- Blocked: none.

## Открытые вопросы

- #75 needs the owner to either write concrete instructions for the final
  metrics/hotspot pass, or decide to close the epic without it. It is
  blocked by #72, #73, #74 (all now Done), so it is unblocked and just
  waiting on scope.

## Следующие шаги

1. Owner: write instructions for #75 (or close it) to let epic #53 close.
2. Once #75 is resolved, close epic #53.
3. No other Ready work queued from #53/#76; check the board for new issues
   before starting a fresh wave.

## Договорённости

- devloop.toml has `journal = ""` and `decisions = ""` for this project: PR
  descriptions carry task detail and tactical decisions instead of per-task
  journal/decision files; lasting technical decisions go into why-comments
  or `docs/adr/`.

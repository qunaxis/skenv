---
name: Task
about: One unit of work for an agent. One task = one branch = one PR.
labels: type:refactor
---

## Goal

<!-- one or two sentences: the result, not the process -->

## Context

<!-- exact files and symbols (file:line as a hint); facts the executor needs; no "read the whole package" -->

## Steps

1. …

## Acceptance

- [ ] …
- [ ] `make check` passes; command and result in the PR

## Verify

<!-- targeted commands first (go test ./internal/x -run TestY -count=1), then make check -->

## Out of scope

<!-- neighbouring changes this task must not make -->

## Docs impact

<!-- files to update (README, docs/…, AGENTS.md, skills/skenv, command help), or "none — <reason>" -->

## Executor model

<!-- add a label model:haiku | model:sonnet | model:opus; see .devloop/process.md -->

Blocked by: <!-- #N, #M or nothing -->

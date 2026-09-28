# User-facing changes require docs updates

PRs that change user-facing code (`internal/cli/**`, `internal/model/skenvfile/**`, `internal/model/config/**`, `schemas/**`, `internal/features/repository/templates/**`) must either:
- Update docs (`README.md`, `docs/**`, `AGENTS.md`, `skills/skenv/**`), or
- Add label `docs:none`, or
- Add one line to the body: `Docs: none — <reason>` (reason ≥10 characters)

---

Closes #

## Summary

<!-- what changed and why; structural and behavioural changes never share a PR -->

## Docs impact

<!-- files updated (README, docs/…, AGENTS.md, skills/skenv, command help), or one line:
Docs: none — <reason> -->

## Verification

<!-- commands, exit code, short result, head SHA -->

## Decisions and follow-ups

<!-- tactical decisions and why; lasting ones also as a why-comment or docs/adr/; follow-ups as linked issues -->

## Review

<!-- code-reviewer plus risk reviewers and why (.devloop/process.md) -->

- [ ] `.devloop/status.md` and `CHANGELOG.md` untouched

# AGENTS.md

Instructions for coding agents working on skenv. Do not add a `CLAUDE.md`:
it would stop Claude Code from loading this file.

- Go CLI, module `github.com/qunaxis/skenv`, binary `cmd/skenv`. Runtime
  dependency: `git` only. External optional tools: `gitleaks` (lint check), `lefthook`
  (repository init), OS service managers for autostart (`launchctl` on macOS, `systemctl` on Linux).
  No network services.
- Layout by layer: `internal/platform` (`internal/platform/atomicfile` for atomic writes, `internal/platform/buildinfo`,
  `internal/platform/docedit` for YAML/JSON editing, `internal/platform/gitx` for git utilities, `internal/platform/paths`
  for file resolution); `internal/model` (`internal/model/agents`, `internal/model/config`, `internal/model/skenvfile`,
  `internal/model/skillname`); `internal/cli` for flags and integration tests; `internal/features` (`internal/features/autostart`
  for service registration, `internal/features/lint` for L1-L6 checks, `internal/features/repository` for init/upgrade/templates,
  `internal/features/skills` with `internal/features/skills/state`); `internal/devtools` (`internal/devtools/clidocs` with
  `internal/devtools/clidocs/example` for docs, `internal/devtools/gendocs`, `internal/devtools/genschemas`, `internal/devtools/mdscan`
  for doc tests, `internal/devtools/release`, `internal/devtools/schemagen` for JSON Schema).
- Layers, enforced by depguard in `.golangci.yml`: `platform` imports no internal
  package, `model` only `platform` and `model`, features never import each other,
  and only `devtools` and tests import `devtools`.
- One template set is embedded, version `skenvfile.LatestTemplates`. A template change
  bumps `skenvfile.LatestTemplates` to the release that ships it; `skenv repo check`
  then reports older repositories and `skenv repo upgrade` moves them
  (`repository.template_version` is desired state: `repo apply` never
  edits it).
- The template version doubles as the skenv release that generated CI
  installs (`SKENV_VERSION` in `check.yml` and `gitlab-ci.yml`): release
  it before any repository runs `repo upgrade`; lint changes reach CI only
  through a new template version.
- No backward-compatibility guarantee before 1.0: breaking CLI, config and
  file-format changes are allowed and marked `!` (see
  `docs/adr/0001-cli-and-config-framework.md`; the format of the skenv file:
  `docs/adr/0002-config-format.md`).
- Safety rules: never delete or replace a path that is not recorded in
  `state.json` unless the user passed `--adopt`; never touch
  `~/.claude/skills/synced`; mask credentials in any URL that reaches output.
- Tests must use a temporary `$HOME` and local bare repositories (see
  `internal/cli/world_test.go`). Never run `sync`, `link`, `init`, `clone`,
  `use` or `autostart` against a real home directory from tests or scripts.
- Before committing: `make check` (go vet, staticcheck, golangci-lint,
  `go test -race`, commit messages).
- Commits: Conventional Commits, English, the body explains why. The
  lefthook `commit-msg` hook runs `scripts/check-commit-msg.sh`. Releases are
  cut only by `make release`; never edit `CHANGELOG.md` or create tags by
  hand, never release 1.0.0 without the owner's decision.
- This repository is public: no secrets, tokens, private paths or internal
  data in code, tests, fixtures or commit messages.
- Work on issues follows `.devloop/process.md` (board, executor model
  labels `model:*`, merge queue, definition of done); config in `devloop.toml`.

## Definition of done

Full contribution workflow: `docs/contributing.md`. Change → update in the
same PR:

| change | update |
|---|---|
| new/changed command, flag, argument, output | `Long`/`Example` in `internal/cli`, `make examples`, `make docs`, the task page in `docs/` that describes the workflow, `skills/skenv/` if agents use it |
| skenv file or config key | Go types → `make schemas`, `docs/skenv-file.md` or `docs/configuration.md`, legacy error in `skenvfile/legacy.go` if a key is renamed/removed, `skills/skenv/` |
| new/moved/removed package | this file's layout list above |
| breaking change | `!` in the commit, docs, `skills/skenv/` |
| repository templates | the template version bump rules above |
| any user-facing code change | docs update (README.md, docs/*, AGENTS.md, skills/skenv/*), or label `docs:none`, or line in PR body: `Docs: none — <reason>` (≥10 chars) |

Run `make check`; the docs consistency tests fail on stale commands,
flags, keys and package lists.

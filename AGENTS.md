# AGENTS.md

Instructions for coding agents working on skenv. Do not add a `CLAUDE.md`:
it would stop Claude Code from loading this file.

- Go CLI, module `github.com/qunaxis/skenv`, binary `cmd/skenv`. Runtime
  dependency: `git` only. No network services, no other executables.
- Layout: `internal/agents`, `internal/atomicfile` (atomic file writes),
  `internal/autostart`, `internal/buildinfo`, `internal/cli` (flags, integration tests),
  `internal/clidocs` (command reference and man pages), `internal/cliexample` (command examples),
  `internal/cli/new.go` (`skenv new`), `internal/config` (tool config),
  `internal/docedit` (YAML/JSON document editing), `internal/engine`
  (sync, link, doctor, list, vendor, init, clone, use, import; `project*.go`: `[project]` in a
  project repository), `internal/fileformat` (`--format` of new files; no write changes a file's
  format, `TestWritesKeepFormat` covers every write path), `internal/gitx`,
  `internal/harness` (`[repository]`, `repo init|apply|upgrade|check`, templates in
  `internal/harness/templates/`), `internal/lint` (L1-L6; `publish.go`: P1 publication check,
  stop-list phrases are never printed), `internal/manifest` (`[user]` and `[project]`
  parsing and in-place editing), `internal/paths` (file location resolution), `internal/release`
  (tests for `cliff.toml` and the commit check), `internal/schemagen` (JSON Schema generation),
  `internal/skenvfile` (the skenv file: `[repository]`, `[user]` and `[project]`,
  TOML/YAML/JSON; `legacy.go`: errors for the keys before 0.6), `internal/skillname` (skill name validation),
  `internal/state`, `internal/tools/gendocs` (command reference generation tool),
  `internal/tools/genschemas` (schema generation tool).
- One template set is embedded, version `harness.Latest`. A template change
  bumps `harness.Latest` to the release that ships it; `skenv repo check`
  then reports older repositories and `skenv repo upgrade` moves them
  (`repository.template_version` is desired state: `repo apply` never
  edits it).
- The harness version doubles as the skenv release that generated CI
  installs (`SKENV_VERSION` in `check.yml` and `gitlab-ci.yml`): release
  it before any repository runs `repo upgrade`; lint changes reach CI only
  through a new harness version.
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

Run `make check`; the docs tests (docs harness epic) fail on stale commands,
flags, keys and package lists.

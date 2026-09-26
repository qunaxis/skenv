# AGENTS.md

Instructions for coding agents working on skenv. Do not add a `CLAUDE.md`:
it would stop Claude Code from loading this file.

- Go CLI, module `github.com/qunaxis/skenv`, binary `cmd/skenv`. Runtime
  dependency: `git` only. No network services, no other executables.
- Layout: `internal/cli` (flags, integration tests), `internal/engine`
  (sync, link, doctor, list, vendor, init, clone, use, import; `project*.go`: `[project]` in a
  project repository), `internal/cli/new.go` (`skenv new`), `internal/skenvfile` (the skenv file: `[repository]`, `[user]` and `[project]`,
  TOML/YAML/JSON; `legacy.go`: errors for the keys before 0.6), `internal/manifest` (`[user]` and `[project]`
  parsing and in-place editing), `internal/config` (tool config), `internal/fileformat` (`--format`
  of new files; no write changes a file's format, `TestWritesKeepFormat`
  covers every write path), `internal/agents`, `internal/state`,
  `internal/autostart`, `internal/gitx`, `internal/buildinfo`,
  `internal/lint` (L1-L6; `publish.go`: P1 publication check, stop-list
  phrases are never printed), `internal/harness` (`[repository]`, `repo
  init|apply|upgrade|check`, templates in `internal/harness/templates/`),
  `internal/release` (tests for `cliff.toml` and the commit check).
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

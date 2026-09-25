# AGENTS.md

Instructions for coding agents working on skenv. Do not add a `CLAUDE.md`:
it would stop Claude Code from loading this file.

- Go CLI, module `github.com/qunaxis/skenv`, binary `cmd/skenv`. Runtime
  dependency: `git` only. No network services, no other executables.
- Layout: `internal/cli` (flags, integration tests), `internal/engine`
  (sync, link, doctor, vendor, init), `internal/manifest` (env.toml parsing
  and in-place editing), `internal/agents`, `internal/state`,
  `internal/autostart`, `internal/gitx`, `internal/buildinfo`,
  `internal/lint` (L1-L6), `internal/harness` (skenv.toml, `repo
  init|apply|check`, templates in `internal/harness/templates/<harness>/`),
  `internal/release` (tests for `cliff.toml` and the commit check).
- Templates are versioned: never change a released template set in place.
  A template change goes into a new `templates/<version>/` directory and
  `harness.Latest` moves to it, so repositories upgrade explicitly with
  `skenv repo apply --upgrade`.
- The harness version doubles as the skenv release that generated CI
  installs (`SKENV_VERSION` in `check.yml`). Name a new template directory
  after the release that ships it, and release it before any repository
  runs `repo apply --upgrade`; lint changes reach CI only through a new
  harness version.
- Safety rules: never delete or replace a path that is not recorded in
  `state.json` unless the user passed `--adopt`; never touch
  `~/.claude/skills/synced`; mask credentials in any URL that reaches output.
- Tests must use a temporary `$HOME` and local bare repositories (see
  `internal/cli/world_test.go`). Never run `sync`, `link`, `init` or
  `autostart` against a real home directory from tests or scripts.
- Before committing: `make check` (go vet, staticcheck, golangci-lint,
  `go test -race`, commit messages).
- Commits: Conventional Commits, English, the body explains why. The
  lefthook `commit-msg` hook runs `scripts/check-commit-msg.sh`. Releases are
  cut only by `make release`; never edit `CHANGELOG.md` or create tags by
  hand, never release 1.0.0 without the owner's decision.
- This repository is public: no secrets, tokens, private paths or internal
  data in code, tests, fixtures or commit messages.

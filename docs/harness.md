# Harness of a skills repository

`skenv repo` generates and verifies the tooling around a repository of
skills: git hooks, the CI workflow, linter configs and the rules for coding
agents. It works in any git repository with skills under `skills/<name>/`.

- [`skenv.toml`](#skenvtoml)
- [Commands](#commands)
- [Managed files](#managed-files)
- [Skill tests](#skill-tests)
- [Harness versions](#harness-versions)

## `skenv.toml`

A repository describes its harness in `skenv.toml`:

```toml
harness    = "0.3.0"                               # version of the templates
visibility = "private"                             # private | public
runner     = ["self-hosted", "linux", "docker"]    # runs-on, private only
```

## Commands

| Command                                        | What it does                                                                                                                                                         |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `skenv repo init --visibility private\|public` | Create `skenv.toml` and every managed file, then `lefthook install`. Refuses if `skenv.toml` exists.                                                                 |
| `skenv repo apply [--upgrade]`                 | Regenerate the managed files and blocks for the `harness` version; `--upgrade` first moves `harness` to the newest templates of this skenv. Then `lefthook install`. |
| `skenv repo check`                             | Compare with the templates; any drift, and a `CLAUDE.md` or `.claude/CLAUDE.md` (it disables `AGENTS.md` in Claude Code), is listed with exit code 1.                |

All commands take `--dir` (default: the current repository); `init` and
`apply` take `--dry-run`, and `--force` to replace existing files that skenv
does not manage yet (without it they refuse and list them, so a
hand-written workflow or `.claude/settings.json` is never lost silently).
When `lefthook` is not installed, `init` and `apply` still write the files
and print a warning.

## Managed files

Managed files start with `managed by skenv <harness> — do not edit`:

- `lefthook.yml` — pre-commit: `skenv lint --staged`, ruff (format, check)
  on staged `*.py`, shellcheck on staged `*.sh` and shell executables,
  gitleaks on the staged diff; pre-push: `check` of changed skills and, in
  public repositories, `skenv lint --publish` (needs the local stop-list),
  so nothing is pushed before the publication check.
- `.github/workflows/check.yml` — `skenv repo check`, `skenv lint`, ruff,
  pyright, shellcheck, markdownlint, gitleaks, and the skill checks
  (Python 3.9 and 3.12). Private repositories run on `runner` with caching
  off and uv's cache in `$RUNNER_TOOL_CACHE`; public ones on
  `ubuntu-latest`. Every tool version is pinned in the template.
  Public repositories also run `skenv lint --publish` with the stop-list
  from the `SKENV_DENYLIST` Actions secret (written to a temporary file,
  never echoed; the job fails when the secret is missing).
- `ruff.toml`, `pyrightconfig.json`, `.editorconfig`, `.markdownlint.yaml`.
- `.claude/settings.json` (harness 0.3.0) — the
  [Claude Code hook](claude-code-hook.md).

`AGENTS.md` (repository rules) and `.gitignore` get a managed block between
`<!-- skenv:begin … -->` / `<!-- skenv:end -->` (`# skenv:begin` / `# skenv:end`);
text outside the block is yours and is kept by `apply`.

## Skill tests

An executable `skills/<name>/check` runs from the skill directory, in CI for
skills changed in the PR (against the base) or push (against the previous
head; all skills on a first push) and in pre-push for skills changed against
the upstream branch. Third-party Python imports for pyright go to
`requirements-dev.txt`.

## Harness versions

The templates are embedded in the binary per harness version: 0.2.0 and
0.3.0 (adds `.claude/settings.json` and the publication check in CI).
`skenv repo apply --upgrade` moves a repository to the newest one. `skenv
doctor` warns about own repositories whose `harness` is older than the
newest templates of the installed skenv.

The publication check and the lint rules are described in
[commands](commands.md#skenv-lint-path---staged---publish).

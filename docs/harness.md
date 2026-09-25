# Harness of a skills repository

`skenv repo` generates and verifies the tooling around a repository of
skills: git hooks, the CI workflow, linter configs and the rules for coding
agents. It works in any git repository with skills under `skills/<name>/`.

- [`[repo]` in `skenv.toml`](#repo-in-skenvtoml)
- [Commands](#commands)
- [Managed files](#managed-files)
- [Skill tests](#skill-tests)
- [Harness versions](#harness-versions)

## `[repo]` in `skenv.toml`

A repository describes its harness in the `[repo]` section of its skenv file
(`skenv.toml`, or `skenv.yaml`/`skenv.yml`/`skenv.json`; see
[the skenv file](skenv-file.md)):

```toml
[repo]
harness    = "0.4.0"                               # version of the templates
visibility = "private"                             # private | public
runner     = ["self-hosted", "linux", "docker"]    # runs-on, private only
```

The repository that holds your manifest has an `[environment]` section in
the same file. A public repository must not: `skenv repo init` refuses and
`skenv repo check` fails, because the manifest is personal (home paths, host
names, which skills you use).

> [!WARNING]
> In a private repository the CI jobs run on `runner`, which defaults to a
> self-hosted runner (`["self-hosted", "linux", "docker"]`). Without such a
> runner the jobs wait in the queue and GitHub fails them after 24 hours; set
> `runner = ["ubuntu-latest"]` to use GitHub-hosted runners instead. Public repositories always run on
> `ubuntu-latest` and ignore `runner`, so pull requests from forks never
> execute on your own machines.

## Commands

### `skenv repo init`

```sh
skenv repo init --visibility private   # or: public
```

Adds `[repo]` and the schema directive (creating `skenv.toml` if there is
no skenv file, or `skenv.yaml` or `skenv.json` with `--format yaml|json`)
and every managed file, then runs `lefthook install`. Refuses if `[repo]` already
exists. An existing skenv file gets `[repo]` in its own format; `--format`
that disagrees with it is an error (see
[Creating the file](skenv-file.md#creating-the-file)). Reference: [skenv repo init](commands/skenv_repo_init.md).

### `skenv repo apply`

```sh
skenv repo apply
```

Regenerates the managed files and blocks from the templates of this skenv,
moving an older `harness` to it, then runs `lefthook install`. It also
points the schema directive of the skenv file at the schema of that
version, adding the directive when it is missing (see
[Editor support](editor-support.md)). Reference:
[skenv repo apply](commands/skenv_repo_apply.md).

### `skenv repo check`

```sh
skenv repo check
```

Compares the repository with the templates. Any drift, and a `CLAUDE.md` or
`.claude/CLAUDE.md` (it disables `AGENTS.md` in Claude Code), is listed with
exit code 1. A skenv file without a schema directive, or with one for
another version than `harness`, is a warning on stderr that does not change
the exit code; `skenv repo apply` fixes it. Reference: [skenv repo check](commands/skenv_repo_check.md).

### Common flags

All commands take `--dir` (default: the current repository); `init` and
`apply` take `--dry-run`, and `--force` to replace existing files that skenv
does not manage yet (without it they refuse and list them, so a
hand-written workflow or `.claude/settings.json` is never lost silently).

> [!NOTE]
> `skenv repo check` fails on a `CLAUDE.md` or `.claude/CLAUDE.md` in the
> repository, because it disables `AGENTS.md` in Claude Code. Keep
> repository rules in `AGENTS.md`.
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
- `.claude/settings.json` — the
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

skenv embeds one template set, the harness version of its release (0.4.0).
`skenv repo check` reports a repository on an older `harness`, and
`skenv repo apply` moves it to the current one. `skenv doctor` warns about
own repositories whose `harness` is older than the templates of the
installed skenv. The CI workflow installs the skenv release named by
`harness`.

The publication check and the lint rules are described in
[commands](commands.md#skenv-lint-path---staged---publish).

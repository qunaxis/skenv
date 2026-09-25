# ADR 0001: CLI and configuration framework

- Status: accepted
- Date: 2026-09-25
- Issue: [#6](https://github.com/qunaxis/skenv/issues/6)

## Context

skenv's command line was built on the standard library `flag` package with a
small `newFlags` helper (`internal/cli/{cli,harness,new}.go`, 713 non-blank,
non-comment lines). Every command carried a hand-written synopsis string, and
the README repeated them by hand. skenv's own configuration was one TOML file,
`~/.config/skenv/config.toml`, with one key, `manifest`, which `--manifest`
and `$SKENV_MANIFEST` override.

Issue #6 asks whether to move to [cobra](https://github.com/spf13/cobra) and
[viper](https://github.com/spf13/viper), with three goals:

1. skenv's configuration readable from TOML, YAML or JSON, with one precedence
   chain: flag, then environment variable, then config file, then default.
2. A command reference generated from the command definitions, so it cannot
   drift from `--help`.
3. Less hand-written flag and usage plumbing, ideally.

Constraints:

- Every invocation in the harness templates 0.2.0 and 0.3.0 (lefthook, CI,
  AGENTS.md, the Claude Code hook), the autostart units (`skenv sync --quiet`)
  and the docs must keep working.
- Exit codes 0/1/2, `--dry-run`, `--json` and the short English messages
  stay as they are.
- `vendor add|bump|remove` edits `env.toml` in place and keeps its comments
  and order. That must not regress.
- The only runtime dependency is `git`.

## Options

CLI:

- **A.** cobra, with `cobra/doc` for the reference.
- **B.** kong ([alecthomas/kong](https://github.com/alecthomas/kong)).
- **C.** urfave/cli v3 ([urfave/cli](https://github.com/urfave/cli)).
- **D.** Keep stdlib `flag` and add a small doc generator and completion
  scripts over a command table.

Configuration:

- **V.** viper.
- **K.** koanf ([knadh/koanf](https://github.com/knadh/koanf)).
- **P.** A plain loader: the TOML and YAML libraries skenv already uses, plus
  `encoding/json`.

## Spike

Each CLI option was prototyped on its own branch from `main`, and each spike
was kept on its branch without being merged:

- cobra: `spike/cli-framework`. The whole command tree was migrated.
- kong: `spike/cli-kong`, which migrated `sync`, `link`, `vendor add|bump|remove` and `repo init|apply|check`.
- urfave/cli v3: `spike/cli-urfave`, with the same commands as kong.
- viper, koanf and the plain loader: `spike/config-viper`, `spike/config-koanf` and `spike/config-plain`.

Every spike passes the existing test suite unchanged.

### Measurements

The binary sizes use the goreleaser flags (`CGO_ENABLED=0 -trimpath -ldflags "-s -w"`).
"Modules linked" counts the modules compiled into `./cmd/skenv`, not counting
skenv itself. Startup is the median of 300 runs of `skenv --help` on an Apple M-series
laptop, with every binary run in the same interleaved session. The spread between sessions is
about ±0.5 ms, so startup differences under 1 ms are noise.

CLI options. The cobra column is the full implementation, including the config loader. kong
and urfave cover only the commands that were migrated; the rest of their
tree is a thin passthrough.

| | stdlib (main) | cobra | kong v1.16 | urfave/cli v3.13 |
|---|---|---|---|---|
| darwin/arm64, bytes | 5 066 402 | 5 792 514 (+726 KB, +14.3%) | 5 580 434 (+10.1%) | 5 422 322 (+7.0%) |
| darwin/amd64 | 5 387 504 | 6 165 312 | 5 942 624 | 5 777 536 |
| linux/amd64 | 5 349 536 | 6 041 760 | 5 890 208 | 5 726 368 |
| linux/arm64 | 5 046 432 | 5 701 792 | 5 570 720 | 5 374 112 |
| modules linked into `skenv` | 2 | 4 (+cobra, pflag) | 3 | 3 |
| modules in `go.mod` graph | 3 | 8¹ | 7² | 4 |
| `go.sum` lines | 6 | 16 | 14 | 8 |
| `skenv --help`, median ms | 4.74 | 5.17 | 5.62 | 5.19 |

¹ cobra, pflag, mousetrap (Windows only), plus go-md2man and blackfriday. The last two are
linked only into the docs generator, and `gopkg.in/check.v1` is test-only.
² Three of these are test-only.

Configuration loaders, measured on `main` plus each loader:

| | plain loader | koanf v2.3, stock parsers | koanf, own decoders | viper v1.21 |
|---|---|---|---|---|
| darwin/arm64 delta | +0.1 KB | +542 KB | +182 KB | +2.49 MB (+49%) |
| modules linked (all) | 2 (3) | 16 (23) | 9 (17) | 14 (27) |
| `go.sum` lines | 6 | 47 | 33 | 49 |
| `skenv --help`, median ms | ≈ baseline | ≈ baseline | 5.03 | 6.89 |
| loader code, lines | 141 | 162 | 186 | 118, plus its own file discovery |

Lines of code are non-blank, non-comment lines.

- The CLI layer (`internal/cli/*.go` without tests) was 713 lines. It is
  800 after the full cobra migration: 776 for the flag layer plus the
  completion and compatibility plumbing. The kong spike came to 776 lines
  and the urfave spike to 807 lines, each for a partial migration.
- **No framework made the code shorter.** Each command gains a `Short`
  summary and a structured definition, and in return help, usage,
  completion and docs come from that one definition.

### Flag syntax compatibility

Each invocation was tested on the built binaries, not assumed:

| Invocation | stdlib (before) | cobra/pflag | kong | urfave v3 |
|---|---|---|---|---|
| `sync --quiet`, `lint --staged`, `repo check`, … (every template form) | ok | ok | ok | ok |
| `sync -quiet`, `repo init -visibility public` (single-dash long flag) | ok | **error**; ok after the rewrite below | **error** | ok |
| `sync --dry-run=false`, `--quiet=true` | ok | ok | ok | ok |
| flags after positionals, `--` terminator | ok | ok | ok | ok |
| `skenv` with no command | usage on stderr, exit 2 | same (kept explicitly) | same | same |
| `sync --help` | help on stderr, exit 0 | help on stdout, exit 0 | stdout, 0 | stdout, 0 |
| unknown flag or command | exit 2 | exit 2 | exit 2 | exit 2 |

pflag reads `-quiet` as the shorthand group `-q -u -i -e -t`. The cobra
implementation rewrites a single-dash word to `--word` before parsing, but
only when `word` is the long name of a real flag. That takes about 25 lines,
and everything after `--` is left alone. No shipped template, unit or doc
uses the single-dash form. The rewrite exists for users' own scripts.

The Claude Code hook in harness 0.3.0 checks whether skenv supports the hook by running
`skenv lint --help 2>&1 | grep -q -- -hook`. It keeps working because the
help still names `--hook`, and the `2>&1` covers the move from stderr to stdout.

### Generated docs

- cobra: `cobra/doc` writes one Markdown file per command, with synopsis,
  usage, options, inherited options and links to the parent and children. It
  can write man pages the same way. See `docs/commands/` in the
  implementation PR.
- kong: nothing built in. A DIY Markdown walker over `kong.Model` took about
  40 lines. Man pages need a third-party package (`mango-kong`).
- urfave v3: the separate module `urfave/cli-docs/v3` renders only the
  whole app. For a subcommand it drops the positional argument and labels
  the flags "GLOBAL OPTIONS", so it would need a custom template.
- stdlib: about 50 lines of generator once the commands become a table,
  plus that restructuring.

The page cobra generates for `vendor add`:

````markdown
## skenv vendor add

Pin a third-party skill in the manifest and sync it

### Synopsis

Pin a third-party skill in the manifest (HEAD of the default branch unless
--rev) and sync it. The manifest change is not committed.

```
skenv vendor add <owner/repo> [flags]
```

### Options

```
      --adopt             move conflicting unmanaged paths to the backup directory and replace them
      --dry-run           print the plan, change nothing
  -h, --help              help for add
      --manifest string   path to env.toml
      --name string       skill name (default: last element of --path)
      --path string       directory with SKILL.md inside the repository ("." for the root)
      --rev string        commit to pin (default: HEAD of the default branch)
```

### SEE ALSO

* [skenv vendor](skenv_vendor.md)	 - Pin, bump and remove third-party skills
````

### Shell completion

- cobra: built in, `skenv completion bash|zsh|fish|powershell`. It is
  dynamic, and flag values can be completed too (for example
  `--visibility private|public`).
- urfave v3: built in (`EnableShellCompletion`).
- kong: third-party only (`kong-completion` or `kongplete`, both on
  `posener/complete`).
- stdlib: hand-written scripts for each shell, which we would maintain.

### Other findings

- kong calls `Exit(0)` from inside parsing for `--help` and `--version`.
  To return an exit code instead of exiting, the spike had to panic with a
  sentinel and recover it.
- urfave v3 calls `os.Exit` unless `ExitErrHandler` is replaced. It keeps the
  version and help printers in package-level variables, which gets in the
  way of parallel in-process tests. It also needs `OnUsageError` set on every
  command node, and it does not reject extra positional arguments.
- cobra reports only errors, not exit codes. A small `app` struct carries
  the 0/1/2 code of the command that ran. Parse errors, which never reach a
  command, map to 2.

## Configuration

- **viper:**
  - Keys are case-insensitive. `Manifest`, `MANIFEST` and `manifest` are the
    same key, and keys are written back in lower case.
  - With several config files it silently takes the first in its extension
    order (json before toml). It also reads a lone `config.env` as dotenv.
  - `WriteConfig` persists defaults and environment overrides into the file.
  - It reads the environment through `os.LookupEnv` with no injection point,
    so tests of `SKENV_*` need `t.Setenv` and cannot run in parallel. The
    spike had to apply the injected `Getenv` by hand.
  - Its TOML library (pelletier/go-toml v2) writes literal strings
    (`manifest = '~/…'`), which broke three existing init tests.
  - It pulls in fsnotify, afero, cast, x/text and more, and adds about 2 ms
    of startup.
- **koanf:**
  - Keys are case-sensitive and the environment source is injectable
    (`EnvironFunc`).
  - It has no file discovery, and it silently merges several files, last
    one wins.
  - Its stock file provider and TOML parser bring fsnotify and pelletier,
    with the same literal-string change as viper. Plugging in our own
    decoders avoids that, but koanf core, mapstructure and copystructure
    remain, which is a lot of machinery for one key.
- **Plain loader:**
  - About 150 lines, with no new modules.
  - It decodes into a map, so keys are case-sensitive in every format.
    Decoding into a struct would be case-insensitive in BurntSushi and
    encoding/json but not in yaml.v3.
  - A value that is not a string is an error.
  - Several config files are an error. A flag or `SKENV_*` value still wins
    without reading them, which is consistent with the precedence.
  - It tests with a temporary `$HOME` and an injected `Getenv`, in parallel.

### Which files become multi-format

| File | Role | Decision |
|---|---|---|
| `~/.config/skenv/config.{toml,yaml,yml,json}` | skenv's own settings | **Multi-format.** Only one file may exist; two or more is an error ("several config files …; keep one"). |
| `env.toml` (the manifest) | Data in git, edited in place by `vendor add\|bump\|remove` with comments and order preserved, mapped to `skills-lock.json` | **TOML only.** The in-place editor (`internal/manifest/edit.go`) works on TOML text. YAML would need a second editor on `yaml.Node`, and JSON has no comments to preserve. Two formats in shared repositories would split tooling and review for no user benefit. |
| `skenv.toml` (the repository harness) | Data in git, generated and compared by `skenv repo` | **TOML only**, for the same reasons. `repo check` compares it with templates. |

## Decision

**Adopt cobra for the CLI. Reject viper and koanf. Read the tool config with
a plain loader.** This is a partial adoption.

- **cobra** meets goals 2 and 3 of the issue better than the other options:
  - It generates per-command Markdown and man pages.
  - Its completion is dynamic and covers flag values.
  - It does not call `os.Exit` or rely on global printers, so the in-process
    tests keep working.
  - The one incompatibility, single-dash long flags, is closed by a small,
    tested rewrite.
  - Its cost is 0.7 MB (+14%) of binary and two linked modules (cobra and pflag;
    mousetrap is only linked on Windows, which skenv does not build). Startup
    is within noise.
- **urfave/cli v3 came second.** It is smaller (+0.36 MB), accepts
  single-dash flags natively, and has built-in completion. Its doc generator
  cannot render a subcommand properly, and its global printers and per-node
  usage handlers work against goal 2 and against the test design.
- **kong** has no docs or completion without third-party packages, and it
  breaks the single-dash form.
- **Keeping stdlib** means writing and owning three completion scripts plus a
  generator. It saves about 0.7 MB, which does not matter for a CLI that
  shells out to `git`.
- **viper** is rejected. Its dependency and size cost is large for a config
  with one key. It lowercases keys, and parallel tests need care with its
  instance setup. None of its features (watching, remote config, flag
  binding) are needed.
- **koanf** is lighter than viper, but it still adds modules for a job that
  about 150 lines over the existing TOML/YAML libraries and `encoding/json`
  already do.
- **The plain loader** implements the precedence `flag > SKENV_<KEY> > config
  file > default`, with case-sensitive keys and one config file at most.

## Consequences

- New direct dependencies: `github.com/spf13/cobra` and
  `github.com/spf13/pflag`. `cobra/doc` and its Markdown/man dependencies
  (go-md2man, blackfriday) are only linked into the docs generator
  (`internal/tools/gendocs`), not into `skenv`.
- **The runtime dependency is still only `git`.** Go libraries are compiled
  into the static binary, and nothing new is needed on the machine.
- `make docs` writes `docs/commands/*.md`. A test regenerates the reference
  and fails when the committed copy is stale, so CI catches a flag change
  made without `make docs`. The README link to the reference is a follow-up
  after the README rework (#5, PR #7) merges.
- `skenv completion bash|zsh|fish` is new.
- The tool config can be `config.toml`, `config.yaml`, `config.yml` or
  `config.json`. `skenv init` still creates `config.toml`, and it updates an
  existing YAML or JSON file in its own format rather than creating a second
  file. Comments in the config file are not kept when init rewrites it,
  which was already true of `config.toml`.
- **No breaking change to the CLI.** The observable differences:
  - Help requested with `--help` goes to stdout instead of stderr.
  - Usage errors print one `skenv: …` line with a pointer to `--help`
    instead of the full flag list.
  - `autostart` actions are subcommands, but they are spelled the same:
    `skenv autostart enable`.
  - New are `-v` for `--version`, `skenv help <command>` and `skenv completion`.
- A compatibility test pins every invocation from the harness templates
  0.2.0 and 0.3.0, the autostart units and the docs, in both the
  `--flag` and `-flag` forms, and checks the command and flag values each
  one parses to. A second test extracts the `skenv …` lines from the
  embedded templates and the rendered autostart units, and fails when one
  of them is missing from the pinned list.

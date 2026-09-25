# ADR 0001: CLI, configuration and the skenv file

- Status: accepted
- Date: 2026-09-25
- Issue: [#6](https://github.com/qunaxis/skenv/issues/6)

## Context

skenv's command line was built on the standard library `flag` package with a
small `newFlags` helper (`internal/cli/{cli,harness,new}.go`, 713 non-blank,
non-comment lines). Every command carried a hand-written synopsis string, and
the docs repeated them by hand. skenv's own configuration was one TOML file,
`~/.config/skenv/config.toml`, with one key, `manifest`. Two data files lived
in repositories: `env.toml`, the manifest of a user's machines, and
`skenv.toml`, the harness of a skills repository.

Issue #6 asks whether to move to [cobra](https://github.com/spf13/cobra) and
[viper](https://github.com/spf13/viper), with three goals:

1. skenv's configuration readable from TOML, YAML or JSON, with one precedence
   chain: flag, then environment variable, then config file, then default.
2. A command reference generated from the command definitions, so it cannot
   drift from `--help`.
3. Less hand-written flag and usage plumbing, ideally.

The owner later added a fourth: file names that say what the files are.

**No backward-compatibility guarantee before 1.0.** skenv is 0.x and has no
users besides its author. Breaking CLI, config and file-format changes are
allowed and marked `!` in the commit that makes them. Compatibility with
earlier releases therefore did not count for or against any option below.
Exit codes 0/1/2, `--dry-run`, `--json` and short English messages are
product rules and stay. The comment-preserving edit of the manifest by
`vendor add|bump|remove` also stays. The only runtime dependency is still `git`.

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

Data files: see [Configuration files and naming](#configuration-files-and-naming).

## Spike

Each option was prototyped on its own branch from `main`, and none of these
branches will be merged:

- cobra: `spike/cli-framework`. The whole command tree was migrated.
- kong: `spike/cli-kong`, which migrated `sync`, `link`, `vendor add|bump|remove` and `repo init|apply|check`.
- urfave/cli v3: `spike/cli-urfave`, with the same commands as kong.
- viper, koanf and the plain loader: `spike/config-viper`, `spike/config-koanf` and `spike/config-plain`.

### Measurements

The binary sizes use the goreleaser flags (`CGO_ENABLED=0 -trimpath -ldflags "-s -w"`).
"Modules linked" counts the modules compiled into `./cmd/skenv`, not counting
skenv itself. Startup is the median of 300 runs of `skenv --help` on an Apple
M-series laptop, with every binary run in the same interleaved session. The
spread between sessions is about ±0.5 ms, so differences under 1 ms are noise.

The CLI options are below. The cobra column is the implementation (#11)
without compatibility code, including the config loader. kong and urfave
cover only the migrated commands; the rest of their tree is a thin
passthrough.

| | stdlib (main) | cobra | kong v1.16 | urfave/cli v3.13 |
|---|---|---|---|---|
| darwin/arm64, bytes | 5 066 402 | 5 792 514 (+726 KB, +14.3%) | 5 580 434 (+10.1%) | 5 422 322 (+7.0%) |
| darwin/amd64 | 5 387 504 | 6 161 216 | 5 942 624 | 5 777 536 |
| linux/amd64 | 5 349 536 | 6 033 568 | 5 890 208 | 5 726 368 |
| linux/arm64 | 5 046 432 | 5 701 792 | 5 570 720 | 5 374 112 |
| modules linked into `skenv` | 2 | 4 (+cobra, pflag) | 3 | 3 |
| modules in `go.mod` graph | 3 | 8¹ | 7² | 4 |
| `go.sum` lines | 6 | 16 | 14 | 8 |
| `skenv --help`, median ms | 4.74 | 5.17 | 5.62 | 5.19 |

¹ cobra, pflag, mousetrap (Windows only), plus go-md2man and blackfriday. The
last two are linked only into the docs generator, and `gopkg.in/check.v1` is
test-only.
² Three of these are test-only.

These are the configuration loaders, each measured as `main` plus that loader:

| | plain loader | koanf v2.3, stock parsers | koanf, own decoders | viper v1.21 |
|---|---|---|---|---|
| darwin/arm64 delta | +0.1 KB | +542 KB | +182 KB | +2.49 MB (+49%) |
| modules linked (all) | 2 (3) | 16 (23) | 9 (17) | 14 (27) |
| `go.sum` lines | 6 | 47 | 33 | 49 |
| `skenv --help`, median ms | ≈ baseline | ≈ baseline | 5.03 | 6.89 |
| loader code, lines | 141 | 162 | 186 | 118, plus its own file discovery |

### Lines of code

Lines are non-blank and non-comment, counted per declaration with `go/ast`.
Compatibility code is not counted in any column: cobra's single-dash rewrite
was removed, kong never had one, and urfave accepts `-flag` natively.

| Scope | stdlib | cobra | kong | urfave v3 |
|---|---|---|---|---|
| `sync`/`link`, `vendor add\|bump\|remove`, `repo init\|apply\|check`, with shared helpers and repo logic | 176 | 232 | 201 | 243 |
| root and dispatch | 57 | 58 | 85 | 74 |
| all of `internal/cli/*.go` without tests | 713 | 766 (full migration) | 776 (partial) | 807 (partial) |

**No framework made the code shorter.** Each command gains a `Short` summary
and a structured definition. In return, help, usage, completion and docs all
come from that one definition. kong's struct tags are the most compact, about
30 lines less than cobra for the migrated commands, but they carry no
docs or completion.

### Generated docs

- cobra: `cobra/doc` writes one Markdown file per command, with synopsis,
  usage, options, inherited options and links to the parent and children. It
  can write man pages the same way.
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
      --manifest string   skenv file with the [environment] section, or its directory
      --name string       skill name (default: last element of --path)
      --path string       directory with SKILL.md inside the repository ("." for the root)
      --rev string        commit to pin (default: HEAD of the default branch)
```
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

- kong calls `Exit(0)` from inside parsing for `--help` and `--version`. To
  return an exit code instead of exiting, the spike had to panic with a
  sentinel and recover it.
- urfave v3 calls `os.Exit` unless `ExitErrHandler` is replaced. It keeps
  the version and help printers in package-level variables, which gets in
  the way of parallel in-process tests. It also needs `OnUsageError` set on
  every command node, and it does not reject extra positional arguments.
- cobra reports only errors, not exit codes. A small `app` struct carries the
  0/1/2 code of the command that ran. Parse errors, which never reach a
  command, map to 2.
- pflag rejects Go-style single-dash long flags (`-quiet`). With no
  compatibility promise this is accepted as is. Everything skenv generates
  or documents uses `--flag`.

## Configuration loaders

- **viper:**
  - Keys are case-insensitive. `Manifest`, `MANIFEST` and `manifest` are the
    same key, and keys are written back in lower case.
  - With several config files it silently takes the first in its extension
    order (json before toml). It also reads a lone `config.env` as dotenv.
  - `WriteConfig` saves defaults and environment overrides into the file.
  - It reads the environment through `os.LookupEnv` with no injection point,
    so tests of `SKENV_*` need `t.Setenv` and cannot run in parallel.
  - Its TOML library (pelletier/go-toml v2) writes literal strings
    (`manifest = '~/…'`), which broke three existing init tests.
  - It pulls in fsnotify, afero, cast, x/text and more, and adds about 2 ms
    of startup.
- **koanf:**
  - Keys are case-sensitive and the environment source is injectable
    (`EnvironFunc`).
  - It has no file discovery, and it silently merges several files; the
    last one wins.
  - Its stock file provider and TOML parser bring fsnotify and pelletier.
    Plugging in our own decoders avoids that, but koanf core, mapstructure
    and copystructure remain, which is a lot for one key.
- **Plain loader:**
  - About 150 lines, with no new modules.
  - It decodes into a map, so keys are case-sensitive in every format.
  - A value that is not a string is an error.
  - Several config files are an error.
  - It tests with a temporary `$HOME` and an injected `Getenv`, in parallel.

## Configuration files and naming

`env.toml` and `skenv.toml` said neither what they were nor how they
differed, and the repository holding the manifest needed both. Four
layouts were compared:

| Option | Files | For | Against |
|---|---|---|---|
| Keep | `env.toml` + `skenv.toml` | nothing to change | names explain nothing; two files in the manifest repository |
| Rename | `skenv.env.toml` + `skenv.repo.toml` | names say which is which | still two files and two loaders; long names |
| Hidden dir | `.skenv/repo.toml` + `.skenv/environment.toml` | tidy root | hidden from reviewers and editors; still two files |
| **One file, two sections** | `skenv.toml` with `[repo]` and `[environment]` | one well-known name per repository; one loader and one schema family (#8); the manifest repository needs one file | personal manifest and repository policy share a file, so a rule must keep the manifest out of public repositories |

**Decision: one skenv file per repository root, with two optional
sections.** The file is `skenv.toml`, `skenv.yaml`, `skenv.yml` or
`skenv.json`, and more than one in a directory is an error.

- `[repo]` is the harness of a skills repository: `harness`,
  `visibility`, `runner`. `skenv repo init` adds it (and creates
  `skenv.toml` when there is no file); `skenv repo apply` sets `harness`.
- `[environment]` is the manifest: `layout`, `own`, `vendor`, `host`. The
  selection fields planned in #9 go under `[[environment.own]]`, and the
  schemas planned in #8 are one per section.
- Nothing else may appear at the top level. Each section rejects unknown
  keys.
- **A public repository must not carry `[environment]`.** The manifest is
  personal: paths in your home directory, host names, the skills you use.
  `repo init --visibility public` refuses such a file and `repo check`
  reports it, so the public CI fails.
- The manifest is the skenv file that has `[environment]`. The tool config
  key keeps its name, `manifest`, and may name that file or its
  directory. The same applies to `--manifest` and `$SKENV_MANIFEST`.
- The tool config stays separate, in `~/.config/skenv/config.*`. It
  belongs to the machine, not to a repository.
- **Formats:** all four formats are read the same way.
  - `vendor add|bump|remove`, `repo init` and `repo apply` edit TOML as
    text, which keeps comments, order and formatting.
  - YAML and JSON are decoded, changed and encoded again, so YAML comments
    and key order are lost. skenv prints a note when it does this.
- **No migration command and no legacy reading.**
  - `env.toml` is an error that names the file and lists the table renames.
  - So is a `skenv.toml` with top-level `harness`, `visibility` or `runner`.
  - `docs/skenv-file.md` describes the move by hand.
- **One embedded harness template set** (0.4.0) instead of every released
  one.
  - Keeping 0.2.0 and 0.3.0 only served repositories that had not been
    upgraded, which is compatibility baggage.
  - `repo check` reports an older `harness`, and `repo apply` moves the
    repository to the current one, so `repo apply --upgrade` is gone.
  - The harness version still names the skenv release that the generated CI installs.

## Decision

**Adopt cobra for the CLI. Reject viper and koanf. Read the tool config with
a plain loader.** This is a partial adoption.

- **cobra** meets goals 2 and 3 best:
  - It generates per-command Markdown and man pages.
  - Its completion is dynamic and covers flag values.
  - It does not call `os.Exit` or use global printers, so the in-process
    tests keep working.
  - It costs 0.7 MB (+14%) of binary and two linked modules (cobra and
    pflag; mousetrap is only linked on Windows, which skenv does not build).
    Startup is within noise.
- **Without compatibility, the decision holds.** Removing the compat
  requirement took away urfave's one distinctive advantage (native `-flag`
  parsing) and kong's one extra disadvantage (it rejects `-flag`, as cobra
  does). The deciding differences remain:
  - Docs and completion are built into cobra; they are third-party or DIY
    for kong, and the urfave docs module cannot render subcommands.
  - kong exits the process during parsing; urfave does too unless
    `ExitErrHandler` is replaced, and it keeps global printers.
  - kong's ~30-line advantage over cobra in LOC does not pay for writing
    and maintaining a doc generator and completion.
- **Keeping stdlib** means writing and owning three completion scripts plus
  a generator to save 0.7 MB. That does not matter for a CLI that shells out
  to `git`.
- **viper** is rejected. Its dependency and size cost is large for a config
  with one key, and its case-folding and silent file choice are wrong for
  skenv. None of its features (watching, remote config, flag binding) are
  needed.
- **koanf** is lighter than viper, but it still adds modules for a job that
  about 150 lines over the existing TOML/YAML libraries do.
- **The plain loader** implements `flag > SKENV_<KEY> > config file >
  default`, with case-sensitive keys and one config file at most.
- **The data files** become one skenv file with `[repo]` and
  `[environment]` (see above).

## Consequences

- New direct dependencies: `github.com/spf13/cobra` and
  `github.com/spf13/pflag`. `cobra/doc` and its Markdown and man
  dependencies (go-md2man, blackfriday) are only linked into the docs
  generator (`internal/tools/gendocs`), not into `skenv`.
- **The runtime dependency is still only `git`.** Go libraries are compiled
  into the static binary, and nothing new is needed on the machine.
- `make docs` writes `docs/commands/*.md`, which the README links. A test
  regenerates the reference and fails when the committed copy is stale, so
  CI catches a flag change made without `make docs`.
- `skenv completion bash|zsh|fish` is new.
- The tool config can be `config.toml`, `config.yaml`, `config.yml` or
  `config.json`. `skenv init` creates `config.toml`, or updates an existing
  YAML or JSON file in its own format.
- **Breaking changes**, each marked `!` in its commit:
  - Single-dash long flags (`-quiet`, `-path=x`) are rejected; use
    `--quiet`, `--path=x`.
  - `env.toml` and top-level keys in `skenv.toml` are no longer read. The
    manifest is `[environment]` and the harness is `[repo]` of the skenv file.
  - `skenv repo apply --upgrade` is removed, and harness templates 0.2.0 and
    0.3.0 are no longer embedded. `repo apply` writes harness 0.4.0.
  - The Claude Code hook of harness 0.4.0 runs `skenv lint --hook`
    directly. It no longer probes `lint --help` for `--hook` support.
- **Other visible changes:**
  - `--help` output goes to stdout.
  - Usage errors print one `skenv: …` line with a pointer to `--help`.
  - `autostart enable|disable|status` are subcommands.
  - `-v` is a shorthand for `--version`.
  - `repo --dir X check` accepts `--dir` before the subcommand.
  - `version` rejects extra arguments.
  - A config value that is not a string is an error.
- Existing repositories are moved by hand after the release, following
  `docs/skenv-file.md`.

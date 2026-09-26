# Configuration

skenv's own configuration belongs to the machine, not to a repository. It is
separate from [the skenv file](skenv-file.md) (`skenv.toml` with
`[repository]`, `[user]` and `[project]`), which lives in a repository.

## Precedence

A setting comes from, highest first:

1. its command-line flag, for example `--manifest`;
2. the environment variable `SKENV_<KEY>`: the key in upper case with `-`
   replaced by `_`, for example `SKENV_MANIFEST`;
3. the config file;
4. the built-in default.

An empty flag or environment variable counts as not set.

## The config file

`~/.config/skenv/config.toml`, or `config.yaml`, `config.yml` or
`config.json` if you prefer. At most one of them may exist: skenv refuses to
guess between two. Keys are lowercase and case-sensitive in every format,
and an unknown key is an error that names the key and the file. `$schema`,
the schema URL for editors, is allowed and ignored.

`skenv init`, `skenv clone` and `skenv use` write it. Without a config file
they create `config.toml` with a schema directive for editors (see
[Editor support](editor-support.md)):

```toml
#:schema https://qunaxis.github.io/skenv/schemas/v0.4.0/config.schema.json
# skenv configuration, written by `skenv init`, `clone` or `use`
manifest = "~/src/my-skills/skenv.toml"
```

To create `config.yaml` or `config.json` instead, pass `--format`:

```sh
skenv clone <owner>/<skills-repo> --format yaml
skenv use ~/src/<skills-repo> --format yaml
```

`skenv init`, which starts a manifest in the current repository, creates the
config in the format of the skenv file it writes. skenv reads `config.yml`
but never creates it.

An existing file keeps its format and name: these commands change only
`manifest`, and comments, other keys and their order stay. They never
convert the file: `--format` that disagrees with it is an error (exit
code 2), raised before anything is cloned or written.

## Keys

| Key        | Flag         | Environment      | Default           |
| ---------- | ------------ | ---------------- | ----------------- |
| `manifest` | `--manifest` | `SKENV_MANIFEST` | none              |
| `machine`  | none         | `SKENV_MACHINE`  | from the hostname |

`manifest` names the skenv file or the directory that holds it. There is no
default location: when none of them is set, commands that need the manifest
stop with an error that suggests `skenv init`, `skenv clone <repo>`,
`skenv use <path>` or `--manifest`; inside a repository whose skenv file
has `[user]`, it suggests `skenv use .`.
See [where the manifest is found](manifest.md#where-the-manifest-is-found).

`machine` is the name of this machine for the
[machine rules](manifest.md#machine-rules) of the manifest. When set, the
manifest must have a `[user.machines.<name>]` entry for it (it may be
empty), or commands stop with an error that lists the names it has.
Without it, skenv uses the rules of the full hostname if the manifest has
them, else those of the short hostname. Set it when the hostname changes
(some laptops rename themselves on each network) or is shared:

```toml
manifest = "~/src/my-skills/skenv.toml"
machine  = "work-laptop"
```

skenv never writes `machine`; add it by hand.

## `skenv config show`

`skenv config show` prints the configuration as it applies on this
machine, and why each skill is installed or not. It changes nothing and
does not use the network. The raw configuration is the skenv file itself;
this is the effective view:

- the manifest and where its location came from (`--manifest`,
  `$SKENV_MANIFEST` or the tool config);
- the machine name and its source (the tool config `machine`,
  `$SKENV_MACHINE`, the full or the short hostname) and the
  [machine rules](manifest.md#machine-rules) that apply;
- `$HOME`, `$CLAUDE_CONFIG_DIR` and the store;
- each agent directory, on or off, and why (listed in `enabled`, detected,
  not detected);
- each checkout with its resolved directory, where that came from
  (`checkout_dir` or the machine's `checkout_dirs`), the branch `sync` keeps
  it on and its state;
- each skill of the checkouts and dependencies, selected or not, and why
  (`include`, `exclude`, machine rules).

```text
manifest  ~/src/skills/skenv.toml (tool config ~/.config/skenv/config.toml)
machine   laptop ($SKENV_MACHINE; rules user.machines.laptop)
home      ~ ($HOME)
store     ~/.agents/skills (default)

agents
  claude  ~/.claude/skills    on  detected: ~/.claude exists
  pi      ~/.pi/agent/skills  on  detected: ~/.pi/agent exists

checkouts
  skills  example-org/skills  ~/src/skills (checkout_dir ~/src/skills)  branch main (default branch of origin)  present

skills
  code-review     checkout skills  selected      include omitted: every skill
  commit-message  checkout skills  not selected  include omitted: every skill, but excluded by exclude "commit-*"
  diagrams        dependency       not selected  dependency example-vendor/tools at 27f221f8f2a4 (tools/diagrams); user.machines.laptop.include omitted: every skill, but excluded by user.machines.laptop.exclude "diagrams"

note: dependencies are pinned to a commit; checkouts follow their branch and local edits, and agent detection, the machine name and $HOME come from this machine, so the file alone does not reproduce the skills of checkouts
```

The closing note is the limit of the declarative model: dependencies are
pinned to a commit and reproduce exactly from the file, but checkouts
follow their branch and your local edits, and the machine name, agent
detection and `$HOME` come from the machine. `--json` prints the same as
JSON (`manifest`, `machine`, `home`, `claude_config_dir`, `store`,
`agents`, `checkouts`, `skills`, `unmanaged`, `reproducible`). Reference:
[skenv config show](commands/skenv_config_show.md).

The design is recorded in
[ADR 0001](adr/0001-cli-and-config-framework.md).

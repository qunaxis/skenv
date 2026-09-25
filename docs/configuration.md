# Configuration

skenv's own configuration belongs to the machine, not to a repository. It is
separate from [the skenv file](skenv-file.md) (`skenv.toml` with `[repo]` and
`[environment]`), which lives in a repository.

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
guess between two. Keys are lowercase and case-sensitive in every format.

`skenv init` writes it: it creates `config.toml`, or updates an existing
YAML or JSON file in its own format. Comments in the file are not kept.

## Keys

| Key        | Flag         | Environment      | Default |
| ---------- | ------------ | ---------------- | ------- |
| `manifest` | `--manifest` | `SKENV_MANIFEST` | none    |

`manifest` names the skenv file or the directory that holds it. There is no
default location: when none of them is set, commands that need the manifest
stop with an error that suggests `skenv init <owner/repo>` or `--manifest`.
See [where the manifest is found](manifest.md#where-the-manifest-is-found).

The design is recorded in
[ADR 0001](adr/0001-cli-and-config-framework.md).

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
guess between two. Keys are lowercase and case-sensitive in every format,
and an unknown key is an error that names the key and the file. `$schema`,
the schema URL for editors, is allowed and ignored.

`skenv init` writes it. Without a config file it creates `config.toml` with
a schema directive for editors (see [Editor support](editor-support.md)):

```toml
#:schema https://qunaxis.github.io/skenv/schemas/v0.4.0/config.schema.json
# skenv configuration, written by `skenv init`
manifest = "~/src/my-skills/skenv.toml"
```

To create `config.yaml` or `config.json` instead, pass `--format`:

```sh
skenv init <owner>/<skills-repo> --format yaml
```

`skenv init` without `<owner/repo>`, which starts a manifest in the current
repository, creates the config in the format of the skenv file it writes.
skenv reads `config.yml` but never creates it.

An existing file keeps its format and name: `skenv init` changes only
`manifest`, and comments, other keys and their order stay. It never
converts the file: `skenv init <owner/repo> --format` that disagrees with
it is an error (exit code 2), raised before anything is cloned or written.

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

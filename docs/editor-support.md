# Editor support

skenv publishes a JSON Schema for [the skenv file](skenv-file.md) and one
for [the tool config](configuration.md). With them an editor completes key
names, shows a description when you hover over a key, and marks unknown
keys and wrong values as you type: a short `rev`, a misspelt `visibility`,
`[environment]` in a public repository.

- [How the editor finds the schema](#how-the-editor-finds-the-schema)
- [Schema URLs](#schema-urls)
- [VS Code](#vs-code)
- [JetBrains IDEs](#jetbrains-ides)
- [Neovim](#neovim)
- [Zed](#zed)
- [What the schema cannot check](#what-the-schema-cannot-check)

## How the editor finds the schema

Every file skenv writes names its schema in a directive, so there is
nothing to set up:

| Format | Directive                                                  | Read by                                             |
| ------ | ---------------------------------------------------------- | --------------------------------------------------- |
| TOML   | first line `#:schema <url>`                                | Taplo: Even Better TOML, the taplo language server  |
| YAML   | first line `# yaml-language-server: $schema=<url>`         | yaml-language-server (VS Code YAML, Neovim, Zed, JetBrains) |
| JSON   | top-level key `"$schema": "<url>"`                         | VS Code, JetBrains, any JSON language server        |

A `skenv.toml` created by `skenv repo init` starts like this:

```toml
#:schema https://qunaxis.github.io/skenv/schemas/v0.5.0/skenv.schema.json
# Repository harness: `skenv repo apply` regenerates the managed files.
[repo]
harness    = "0.5.0"
visibility = "private"
ci         = "github"
runner     = ["self-hosted", "linux", "docker"]  # runs-on of the CI jobs
```

Who writes or updates the directive:

- `skenv init`, `skenv clone` and `skenv use` create
  `~/.config/skenv/config.toml` (or `config.yaml`,
  `config.json` with `--format`) with a directive for the config schema. In an existing config file it moves a skenv directive
  to its own version and adds none.
- `skenv repo init` and `skenv init` add the
  directive to the skenv file (creating `skenv.toml`, or the format of
  `--format`, if there is none), and `skenv repo apply` keeps it at the
  version of `repo.harness`. `skenv repo check` warns when it is missing or
  points at another version; the warning does not change the exit code,
  because the directive does not change what skenv does.
- `skenv vendor add|update|remove` keep the directive, like every comment,
  and move a skenv directive to the version described below.

A directive with a URL outside `https://qunaxis.github.io/skenv/schemas/`
(a local copy, a mirror) is your choice: skenv neither changes it nor
warns about it. You can also write the directive by hand in a file skenv
never touches.

## Schema URLs

| Schema          | Latest release                                              | One release                                                      |
| --------------- | ----------------------------------------------------------- | ---------------------------------------------------------------- |
| the skenv file  | `https://qunaxis.github.io/skenv/schemas/skenv.schema.json`  | `https://qunaxis.github.io/skenv/schemas/v<X.Y.Z>/skenv.schema.json`  |
| the tool config | `https://qunaxis.github.io/skenv/schemas/config.schema.json` | `https://qunaxis.github.io/skenv/schemas/v<X.Y.Z>/config.schema.json` |

The directive skenv writes is pinned to a release: in a repository with a
harness, the version in `repo.harness`, which is also the skenv its CI
installs; elsewhere, the version of the skenv that wrote the file, or the
latest URL when that is a development build. Versions before 0.4.0, the
first with schemas, get the latest URL too. A pinned schema never shows a key
as valid that the skenv reading the file does not know. The unversioned
URL follows the newest release, not unreleased changes on `main`.

The schemas also come with every
[GitHub release](https://github.com/qunaxis/skenv/releases), and
`skenv schema` prints those of the installed version:

```sh
skenv schema > skenv.schema.json
skenv schema config > config.schema.json
```

## VS Code

- TOML: install
  [Even Better TOML](https://marketplace.visualstudio.com/items?itemName=tamasfe.even-better-toml).
  It reads the `#:schema` line.
- YAML: install
  [YAML](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)
  by Red Hat. It reads the `yaml-language-server` comment.
- JSON: built in; it reads `"$schema"`.

For a file without a directive, map the schema in `settings.json`:

```json
{
  "evenBetterToml.schema.associations": {
    ".*/skenv\\.toml$": "https://qunaxis.github.io/skenv/schemas/skenv.schema.json"
  },
  "yaml.schemas": {
    "https://qunaxis.github.io/skenv/schemas/skenv.schema.json": ["skenv.yaml", "skenv.yml"]
  },
  "json.schemas": [
    { "fileMatch": ["skenv.json"], "url": "https://qunaxis.github.io/skenv/schemas/skenv.schema.json" }
  ]
}
```

To check that it works, open a `skenv.toml` written by `skenv repo init`:
typing inside `[repo]` or a new `[[environment.vendor]]` table offers the
keys, hovering over `rev` shows its description, and a short `rev` or an
unknown key is underlined.

## JetBrains IDEs

IntelliJ IDEA, GoLand, PyCharm and the others read `"$schema"` in JSON.
For YAML and TOML, and for files without a directive, add a mapping in
**Settings → Languages & Frameworks → Schemas and DTDs → JSON Schema
Mappings**: the schema URL, or a local file from
`skenv schema > skenv.schema.json`, and the file pattern `skenv.toml` (or
`skenv.yaml`, `skenv.json`). TOML files need the bundled TOML plugin.

## Neovim

Use the language servers through
[nvim-lspconfig](https://github.com/neovim/nvim-lspconfig):

- `taplo` for TOML reads the `#:schema` line;
- `yamlls` (yaml-language-server) reads the YAML comment;
- `jsonls` reads `"$schema"`.

```lua
vim.lsp.enable({ "taplo", "yamlls", "jsonls" })
```

For files without a directive, taplo reads rules from a `.taplo.toml` in
the project or your home directory:

```toml
[[rule]]
include = ["**/skenv.toml"]
[rule.schema]
url = "https://qunaxis.github.io/skenv/schemas/skenv.schema.json"
```

## Zed

Zed runs taplo for TOML (through the TOML extension), yaml-language-server
for YAML and a JSON language server out of the box, so the directives work
once the TOML extension is installed. Mappings for files without a
directive go into the `lsp` section of `settings.json`, as for VS Code
(`yaml-language-server` → `settings.yaml.schemas`, `json-language-server` →
`settings.json.schemas`).

## What the schema cannot check

Some rules need the file system or the whole file, so only skenv checks
them (`skenv doctor`, `skenv sync`):

- skill names are unique across own and vendor skills, and the skills of
  an own repository are the directories found in it;
- vendor names are unique;
- a `layout.ignore` pattern must be a valid glob and must not match a
  skill of the manifest;
- `repo.harness` must not be newer than the templates of the skenv that
  reads it;
- a directory holds only one skenv file, and `~/.config/skenv/` only one
  config file.

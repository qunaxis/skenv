Print the JSON Schema of this skenv version to stdout: "skenv" (default) for
the skenv file (skenv.toml, .yaml, .yml or .json with [repository], [user]
and [project]), "config" for the tool config ~/.config/skenv/config.*.

Files that skenv writes name their schema in a directive, so most editors
need no setup. Use this for offline work or a custom mapping, for example a
JSON Schema mapping in JetBrains IDEs or a rule in .taplo.toml. The same
schemas are published at https://qunaxis.github.io/skenv/schemas/.

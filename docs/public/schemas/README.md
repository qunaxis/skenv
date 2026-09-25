# JSON Schemas (published, not written by hand)

Everything in `docs/public/` is copied to the documentation site as is, so a
file here is served at `https://qunaxis.github.io/skenv/schemas/<file>`.

The JSON Schemas of the skenv file and the tool config are served from
here, so that the directives skenv writes (`#:schema`, the
yaml-language-server comment, `"$schema"`) use stable URLs:

- `v<X.Y.Z>/skenv.schema.json`, `v<X.Y.Z>/config.schema.json`: the schemas
  of one release, which never change once published;
- `skenv.schema.json`, `config.schema.json`: those of the newest release.

Nothing but this README is committed here. `make docs-site` (and the docs
workflow) runs `scripts/site-schemas.sh`, which copies `schemas/` of every
release tag into this directory and sets each file's `"$id"` to its
versioned URL. The schemas themselves are generated from the Go types into
`schemas/` by `make schemas`; do not edit them by hand.

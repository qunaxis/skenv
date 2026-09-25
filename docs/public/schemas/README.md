# JSON Schemas (published, not written by hand)

Everything in `docs/public/` is copied to the documentation site as is, so a
file here is served at `https://qunaxis.github.io/skenv/schemas/<file>`.

This directory is where the generated JSON Schemas of the skenv file go
(issue #8), so that `#:schema` directives can use stable URLs:

- `skenv.schema.json`: the latest schema,
  `https://qunaxis.github.io/skenv/schemas/skenv.schema.json`;
- `<version>/skenv.schema.json`: a copy per release, which never changes
  once published.

The schemas are generated; do not edit them here by hand.

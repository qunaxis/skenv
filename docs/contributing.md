# Contributing

<!--@include: ../README.md#contribute-->

## The documentation site

This site is built with [VitePress](https://vitepress.dev) from the Markdown
in `docs/`; the Install, Getting started and Contributing pages include the
matching sections of the README. It needs Node (the version in `.nvmrc`),
only for these targets:

```sh
make docs-serve   # regenerate the command reference, serve on localhost with live reload
make docs-site    # regenerate the command reference, build into docs/.vitepress/dist
```

Every pull request builds the site, and a link to
a page that does not exist fails the build. Every push to `main` publishes it
to GitHub Pages. Files in `docs/public/` are copied to the site as they are:
the JSON Schemas of the skenv file go to `docs/public/schemas/`.

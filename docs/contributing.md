# Contributing

<!-- github-only -->

> This page is part of the [documentation site](https://qunaxis.github.io/skenv/contributing).
> Its content is the [Contribute section of the README](../README.md#contribute).

<!-- /github-only -->

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
a page that does not exist fails the build. Every push to `main`, and every
release, publishes it to GitHub Pages. Files in `docs/public/` are copied to
the site as they are. Both targets first copy the JSON Schemas of every
release tag into `docs/public/schemas/` (`scripts/site-schemas.sh`; see
[its README](public/schemas/README.md)), so the site serves
`https://qunaxis.github.io/skenv/schemas/v<X.Y.Z>/…` for each release and
the newest release at the unversioned URLs.

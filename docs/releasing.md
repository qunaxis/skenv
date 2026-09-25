# Development and releases

- [Development](#development)
- [Commits](#commits)
- [Versions](#versions)
- [Cutting a release](#cutting-a-release)

## Development

```sh
make hooks     # lefthook: commit-msg check, gofmt
make check     # go vet, staticcheck, golangci-lint, go test -race, commit check
make snapshot  # local goreleaser build into dist/
make demo      # re-record docs/demo/demo.gif with vhs
```

`make help` lists every target. Integration tests run skenv against a
temporary `$HOME` with local bare repositories standing in for GitHub; they
never touch your real home.

The README demo is recorded with [vhs](https://github.com/charmbracelet/vhs)
from [`docs/demo/demo.tape`](demo/demo.tape). It runs in a throwaway `$HOME`
under `/tmp/skenv-demo` that [`docs/demo/setup.sh`](demo/setup.sh) builds:
skenv compiled from the checkout, and local bare repositories standing in
for GitHub (`example-org/…`, `example-vendor/…`), so the recording needs no
network and pins the same commits every time. Re-record it with `make demo`
when the output of the commands it shows changes.

## Commits

Commits follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):
`<type>(<scope>)?!?: <subject>` with type `feat`, `fix`, `perf`, `refactor`,
`docs`, `test`, `build`, `ci`, `chore` or `revert`, and scope the command or
subsystem (`sync`, `doctor`, `vendor`, `link`, `autostart`, `manifest`, …).
The lefthook `commit-msg` hook and the `conventional commits` CI job enforce
it for every commit, so pull requests are merged by squash or rebase only
(merge commits are disabled in the repository settings).

## Versions

Versions are computed by [git-cliff](https://git-cliff.org) (`cliff.toml`)
from the commits since the last tag. Until 1.0.0:

| Commits since the last tag           | Next version |
| ------------------------------------ | ------------ |
| breaking (`!` or `BREAKING CHANGE:`) | `0.(x+1).0`  |
| `feat`                               | `0.(x+1).0`  |
| `fix` or `perf`                      | `0.x.(y+1)`  |
| anything else                        | no release   |

## Cutting a release

`make release` computes the version, regenerates `CHANGELOG.md`, commits
`chore(release): vX.Y.Z`, creates an annotated tag, pushes, and runs
goreleaser with git-cliff release notes. It is safe to rerun until the tag
exists. Run it from the `release` workflow (Actions → release → Run
workflow) or locally:

```sh
GITHUB_TOKEN="$(gh auth token)" make release
```

> [!TIP]
> Running `make release` locally is the fallback when the `release`
> workflow cannot run (for example GitHub-hosted runners are unavailable).
> Pass the token only through the environment as above; never write it to
> a file in the repository or paste it into a command that is logged.

goreleaser builds darwin/linux × amd64/arm64 binaries into archives named
`skenv_<version>_<os>_<arch>.tar.gz` (the README install command depends on
that name) plus `checksums.txt`.

[`CHANGELOG.md`](../CHANGELOG.md) is generated; do not edit it by hand.

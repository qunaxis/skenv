Set up the repository templates of a skills repository: the [repository] section and the
schema directive of the skenv file, lefthook.yml, the CI pipeline, linter
configs and the managed blocks of AGENTS.md and .gitignore; then `lefthook
install`. Refuses if [repository] exists.

--visibility is a declared policy: skenv never reads or changes the access
setting on the hosting service. A public repository must not carry [user],
and its CI also runs `skenv lint --publish`.

--ci picks the CI system: github (.github/workflows/check.yml) or gitlab
(.gitlab-ci.yml). Without it, the host of origin decides: gitlab when origin
is on gitlab.com or on a host declared with provider "gitlab" in the manifest
(this repository's own [user], else the manifest in the config file),
github otherwise, also when there is no origin.

CI jobs of a public repository run on the hosted runners (ubuntu-latest on
GitHub, the shared runners on GitLab). Those of a private one run on --runner: the runs-on labels on GitHub, the
runner tags on GitLab; default self-hosted, linux, docker (a self-hosted
Docker runner). --runner ubuntu-latest picks the GitHub-hosted runners.
Afterwards repository.ci.github.runs_on (or repository.ci.gitlab.tags) holds
it; change it there and run `skenv repo apply`.

The generated git hooks need lefthook, uv and gitleaks on PATH; the output
says which of them are missing. Skill management and sync need none of
them: these repository templates are optional tooling for a repository you
publish or share.

Without a skenv file it creates skenv.toml, or skenv.yaml or skenv.json with
--format. An existing skenv file gets [repository] added in its own format;
--format that disagrees with it is an error, and nothing is written.

- Reads: the repository, its origin and skenv file, and the hosts declared
  in the manifest (to detect the CI system).
- Changes: the skenv file ([repository], created if absent), the managed
  files and blocks, and the git hooks (lefthook install).
- Network: none.
- Conflicts: a file that exists and that skenv does not manage yet is an
  error; --force replaces it.
- Preview: --dry-run writes nothing and does not run lefthook install.
- Next: commit the generated files; "skenv repo check" compares them
  with the templates later.

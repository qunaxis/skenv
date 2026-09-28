Regenerate the managed files and blocks from the templates of
repository.template_version; then `lefthook install`. apply never changes
the skenv file: template_version is what the repository asks for, and
this skenv embeds the templates of %s only. Another version
is an error: run `skenv repo upgrade` to move the repository to %s,
or use the skenv release it names.

The CI pipeline follows the table under repository.ci. To switch CI systems,
replace [repository.ci.github] with [repository.ci.gitlab] (or back) and run
apply: it writes the pipeline of the new one and removes the managed file
of the other (.github/workflows/check.yml or .gitlab-ci.yml).

- Reads: the skenv file ([repository]) and the managed files.
- Changes: the managed files and blocks, and the git hooks (lefthook
  install).
- Network: none.
- Conflicts: a file that exists and that skenv does not manage yet is an
  error; --force replaces it.
- Preview: --dry-run writes nothing and does not run lefthook install.
- Next: commit the changed files.

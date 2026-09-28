Set repository.template_version to %s, the templates of this skenv,
and point the schema directive of the skenv file at that version (comments
and formatting stay), then regenerate the managed files as `skenv repo
apply` does. The generated CI installs the skenv release of
template_version, so that release must exist before you push.

- Reads: the skenv file ([repository]) and the managed files.
- Changes: repository.template_version and the schema directive of the
  skenv file, the managed files and blocks, and the git hooks (lefthook
  install).
- Network: none.
- Conflicts: a file that exists and that skenv does not manage yet is an
  error; --force replaces it.
- Preview: --dry-run writes nothing and does not run lefthook install.
- Next: commit the skenv file and the changed files.

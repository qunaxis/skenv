Clone the repository that holds your manifest into <dir> (default ./<repo>
in the current directory, like git clone) and record its skenv file as
"manifest" in the config file, as `skenv use` does. When <dir> is a working copy of
<repo> already, it is used as it is, and an empty directory is cloned into;
another repository or a directory that is not a git working copy is an
error. <repo> is owner/repo (or github:owner/repo) on github.com,
gitlab:group/sub/repo, codeberg:owner/repo or a full git URL; hosts declared
in the manifest are not known before it is cloned, so a self-hosted
repository takes its URL.

It does not sync, whether it cloned or not: run `skenv sync --dry-run` to see what
the manifest would change on this machine, then `skenv sync`.

The manifest usually lists its own repository as a checkout. With
checkout_dir "." that is wherever the manifest is cloned. With another
checkout_dir, that path is the working copy sync keeps up to date: without
<dir>, a new clone goes there when nothing is there yet, and when the
manifest ends up elsewhere, clone warns: sync would keep a second working
copy at checkout_dir and never pull the manifest checkout, so changes
pushed from other machines would not arrive (sync warns and doctor reports
manifest-checkout until you `skenv use` the working copy there).

- Reads: the repository and its skenv file, and the config file.
- Changes: the new working copy <dir> and "manifest" in the config file
  (~/.config/skenv/config.toml unless a YAML or JSON one exists; a new one is
  YAML or JSON with --format). Nothing is synced.
- Network: git clone.
- Conflicts: a <dir> that is not a working copy of <repo> is an error.
- Preview: --dry-run clones nothing and writes nothing.
- Next: `skenv sync --dry-run`, then `skenv sync` (with --adopt when skills are installed
  here another way).

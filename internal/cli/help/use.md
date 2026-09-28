Record the manifest at <path>, a skenv file with a [user] section or
a directory that holds one (`skenv use .` in the root of your skills repository),
as "manifest" in the config file. A manifest recorded before is replaced,
and the output names it. skenv never switches manifests by itself: the
current directory does not select one.

The manifest usually lists its own repository as a checkout; when its
checkout_dir is not the checkout of <path>, use warns: sync would clone a
second working copy there.

- Reads: the skenv file and the config file.
- Changes: "manifest" in the config file (~/.config/skenv/config.toml unless
  a YAML or JSON one exists; a new one is YAML or JSON with --format).
  Nothing is synced.
- Network: none.
- Preview: --dry-run writes nothing.
- Next: `skenv sync --dry-run`, then `skenv sync` (with --adopt when skills are installed
  here another way).

Start a manifest in the git repository of the current directory (or --dir).
Its skenv file gets a [user] section with a commented skeleton, or
skenv.toml is created with one (skenv.yaml or skenv.json with --format); the
repository itself becomes its first checkout, with checkout_dir "." (the
repository that holds the manifest, wherever it is cloned) and repo
owner/repo for an origin on github.com, gitlab:... on gitlab.com,
codeberg:... on codeberg.org, the URL (without credentials) on any other
host; a local origin is left out. A repository without an origin yet names
its future remote with --remote (owner/repo, gitlab:group/repo,
codeberg:owner/repo or a full URL), written the same way; with an origin,
--remote is an error. It does not set up [repository]: "skenv repo init"
does, and picks the CI system from the host of origin. The file is recorded
as "manifest" in the config file (~/.config/skenv/config.toml unless a YAML
or JSON one exists; a new one in the format of the skenv file), and nothing
is synced. It refuses when the file has [user] already (`skenv use .` uses
that one) or its [repository] is public.

To use an existing manifest on this machine: `skenv clone <repo>`, or
`skenv use <path>` for a checkout you already have.

With --import: start the manifest, import the skills already installed on
this machine into it (see "skenv import") and run `skenv sync --adopt`: one
command to adopt an existing setup. Skills pinned without a matching commit
are recorded but left as installed.

An existing file keeps its format: --format that disagrees with it is an
error (exit code 2), and nothing is written.

- Reads: the git repository of the current directory (or --dir), its origin
  and skenv file; with --import, the installed skills and the lock of the
  skills CLI.
- Changes: the skenv file and "manifest" in the config file; with --import,
  the lock of the skills CLI and what `skenv sync --adopt` changes.
- Network: none; --import fetches the repositories of the installed skills.
- Conflicts: with --import, installed copies are moved to
  ~/.local/state/skenv/backup/<ts>/ and replaced, except skills pinned
  without a matching commit, which stay as installed.
- Preview: --dry-run writes nothing except, with --import, the clone cache.
- Next: commit and push the skenv file, then `skenv clone <repo>` on your other
  machines.

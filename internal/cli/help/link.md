Create store links for the skills of checkouts and agent links for every
skill of the manifest. Projects have no links to create: `skenv sync` updates
their mirrors.

- Reads: the manifest, the checkouts, the store, the agent directories and
  the state file.
- Changes: the store links of the skills of checkouts, the agent links and
  the state file; it pulls, copies and removes nothing.
- Network: none.
- Conflicts: an unmanaged path in the way is an error and stays; --adopt
  moves it to ~/.local/state/skenv/backup/<ts>/ and replaces it.
- Preview: --dry-run writes nothing.
- Next: "skenv doctor".

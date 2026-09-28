Move dependencies to a new commit and sync them: the named ones, or every
dependency without names. Each goes to HEAD of its default branch; --rev
pins a single named skill. Shows the log of the skill's directory.

With --project: move entries of [project] and sync the project. A skill of
a [project.from.<id>] entry moves the whole entry, whose skills share one
commit.

- Reads: the manifest (or [project]) and the repositories of the skills.
- Changes: the commit of each moved skill in the manifest (or [project]),
  its copy, its links (or mirrors) and the state file.
- Network: fetches each repository into the clone cache ~/.cache/skenv/repos.
- Conflicts: as vendor add: --adopt replaces an unmanaged path, after a
  backup.
- Preview: --dry-run writes nothing except the clone cache.
- Next: commit the skenv file; "skenv doctor".

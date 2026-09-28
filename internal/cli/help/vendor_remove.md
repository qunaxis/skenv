Remove a dependency from the manifest and its managed paths.

With --project: remove its [project.dependencies.<name>] table and sync the
project, which removes the copy and its mirrors. A skill of a
[project.from.<id>] entry is removed by editing the skills of that entry.

- Reads: the manifest (or [project]) and the state file.
- Changes: the manifest (or [project]), and removes the paths the state file
  records for the skill: its copy and links (or mirrors).
- Network: none.
- Conflicts: paths skenv does not manage are left alone.
- Preview: --dry-run writes and removes nothing.
- Next: commit the skenv file.

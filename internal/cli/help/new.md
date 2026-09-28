Create skills/<name>/ with SKILL.md (frontmatter) and references/ in the git
repository at --dir, or in a checkout of the manifest: the only one, else
the one whose [repository] section has --visibility (default private).
Neither needs repository templates ("skenv repo init").

A skill in a checkout of the manifest reaches your agents with "skenv
link": the skills of checkouts are linked from the working copy, nothing
to pull.

- Reads: the manifest and the skenv file of the target repository.
- Changes: creates skills/<name>/ in the target repository; nothing else.
- Network: none.
- Conflicts: an existing skills/<name> is an error.
- Preview: none.
- Next: fill in SKILL.md, "skenv lint", then "skenv link"; commit the skill.

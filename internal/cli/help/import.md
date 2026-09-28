Add the skills installed on this machine that the manifest does not have
yet, so adopting skenv on a machine with skills is one command. What it
changes:

- The manifest: a skill installed by the vercel skills CLI becomes
  `[user.dependencies.<name>]`, pinned to a commit; a link into a git
  working copy becomes `[user.checkouts.<id>]` (with `include = [...]` when
  only some of its skills are linked; the ID is the repository name). The
  diff is printed and the file written in place, comments kept; a skenv
  file without [user] gets one, as "skenv init" adds it. The change is not
  committed.
- The lock of the skills CLI, ~/.agents/.skill-lock.json (or
  `$XDG_STATE_HOME/skills/.skill-lock.json`): the skills now in the manifest
  leave it, so `npx skills update` no longer changes what skenv manages. The
  lock as it was goes to ~/.local/state/skenv/backup/<ts>/ first.
- Nothing installed: the copies and links stay until `skenv sync --adopt`
  backs them up and replaces them.

The report lists what becomes managed, grouped by how the commit of each
dependency was found: exact (the commit has the hash recorded in the
lock), same files (no commit has the hash, one has the files of the
installed copy) and unmatched (neither: pinned to the tip of the branch, so
the installed copy may differ; a warning). Then what is not imported, with
the reason: a directory neither in the lock nor a link into a working copy,
a lock entry from a source that is not a git repository or not installed.
Skipped without a word: ~/.claude/skills/synced, skills of Claude Code
plugins, user.unmanaged matches and skills in the manifest. A second run
imports nothing.

With --sync, `skenv sync --adopt` follows and takes over the exact and
same-files skills and the checkouts. The unmatched ones are recorded
but left as installed; the report after the sync lists what was recorded
and what was taken over, and for each unmatched skill the two ways to
decide: `skenv sync --adopt` replaces it with the pinned commit,
`skenv vendor remove <name>` drops the entry.

How the commit is found: the hash of the lock (skillFolderHash: a git tree
id for GitHub installs, a sha256 of the files otherwise) is compared with
the skill folder of each commit on the ref of the lock (or the default
branch), newest first from updatedAt back; then the files of the installed
copy; then HEAD.

With --project: the same for the git repository of the current directory
and the skills-lock.json of the skills CLI in its root. Each skill of the
lock becomes `[project.dependencies.<name>]` of the repository's skenv file (a
skenv file without [project] gets one, a repository without a skenv file a
skenv.toml), matched with its computedHash the same way (without dates:
every commit is a candidate). The imported entries leave skills-lock.json
(the file goes when none are left), after a copy to the backup directory.
Project-own skills in dir, the mirrors, .agents/skills, .claude/skills and
.pi/skills are reported and never changed; one that is in several of them
with different files is a warning, and --sync does not run until you pick
the version to keep. --sync leaves unmatched skills as installed here too
(`skenv vendor remove --project <name>` drops one).

- Reads: the manifest (or [project]), the store and agent directories, the
  lock of the skills CLI.
- Changes: the manifest (or [project]) and the lock, after a backup; with
  --sync, what `skenv sync --adopt` changes, except for unmatched skills.
- Network: fetches the repository of each skill of the lock into the clone
  cache ~/.cache/skenv/repos to find its commit.
- Conflicts: none without --sync; with it, installed copies of exact and
  same-files skills are backed up to ~/.local/state/skenv/backup/<ts>/ and
  replaced, unmatched ones are left as installed.
- Preview: --dry-run writes nothing except the clone cache.
- Next: `skenv sync --adopt` to take the installed copies over, then commit
  the skenv file.

Apply the manifest: pull the checkouts (editable git working copies of your
skills), copy each dependency at its commit, link everything into the
store and the agent directories, and remove managed paths that left the
manifest. It never changes the skenv file: pins, selections, template
versions and agents change only by an edit or by "skenv vendor", "skenv
import" and "skenv repo upgrade".

In a project, a git repository whose skenv file has a [project] section
(checked at the root of the repository of the current directory), sync
works on the project instead: it copies every pinned skill of [project]
into its dir at its commit, removes copies whose entry is gone, and gives
every skill of dir to each mirror. It changes a skill authored in dir only
with --adopt, after a backup.
--manifest syncs the machine from there; --project requires a project.

A checkout is fast-forwarded from origin only when it is clean and on its
branch (branch in the manifest, else the default branch of origin). sync
never resets, switches, stashes or re-clones: a checkout on another branch
or a detached HEAD, with uncommitted changes or diverged from origin is
local development state, printed as "unresolved:" and linked as it is;
exit code 0. A checkout_dir that is not a working copy of its repo (another
origin, no git) is an unresolved error: its skills are not linked, links it
had are kept, nothing in it changes, exit code 1. The summary counts the
unresolved. `skenv doctor` exits 0 only when the machine matches the
manifest. With --dry-run nothing is pulled, so the plan uses the checkouts
(and a manifest inside one) as they are now.

- Reads: the manifest, the checkouts, the store (user.storage.dir, default
  ~/.agents/skills), the agent directories and the state file
  ~/.local/state/skenv/state.json.
- Changes: the checkouts (clone, pull --ff-only), the store, the agent
  links and the state file; in a project, its dir and mirrors.
- Network: git clone and pull of checkouts, fetches of dependencies into
  the clone cache ~/.cache/skenv/repos.
- Conflicts: an unmanaged path in the way is an error and stays; --adopt
  moves it to ~/.local/state/skenv/backup/<ts>/ and replaces it.
- Preview: --dry-run writes and pulls nothing, so upstream changes are not
  in the plan.
- Next: "skenv doctor".

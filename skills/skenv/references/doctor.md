# skenv doctor: what to do for each class

Run `skenv doctor` (or `skenv doctor --json`). Fix the cause, then run
`skenv doctor` again until it exits 0.

| Class | Meaning | What to do |
| --- | --- | --- |
| `missing` | A skill is not in the store or not linked for any agent; or a checkout is not cloned. | `skenv sync`. If it persists, check access to the repository (ssh key or credential helper) and the output of sync. |
| `agent-mismatch` | Linked for some agents but not all. | `skenv link`. |
| `manifest-checkout` | The manifest is not in the working copy its checkout names, so `sync` never pulls it. | `skenv use` that working copy, or clone the repository there. |
| `extra-managed` | skenv created the path, but the skill left the manifest, is not selected, or is excluded on this machine. | `skenv sync` removes it. If the skill should stay, put it back into the manifest instead. |
| `unmanaged` | Something in the store or an agent directory that is not from the manifest (installed by hand or by `npx skills`). | Decide: add it to the manifest (`skenv vendor add`, or move it into a checkout) or remove it by hand. If another tool owns it, add a pattern to `user.unmanaged`. skenv never deletes it on its own. |
| `conflict` | A path the manifest needs is taken by something skenv did not create. | Look at the path. If the manifest version should win, `skenv sync --adopt` moves the old content to `~/.local/state/skenv/backup/<timestamp>/` and links the manifest version. |
| `wrong-rev` | A dependency's copy does not match its `repo`, `skill_dir` or `commit`. | `skenv sync`. |
| `broken-link` | A managed symlink dangles or points elsewhere. | `skenv link`; if the target is a checkout skill that was renamed or deleted, fix the repository or the manifest first. |
| `dirty` | A checkout has uncommitted changes (sync will not pull it). | Commit or stash the changes in that repository. |
| `unpushed` | A checkout is ahead of its upstream. | Push it, so other machines get the change. |
| `behind` | A checkout is behind its upstream. | `skenv sync` (fast-forward only; resolve divergence by hand). |
| `wrong-branch` | A checkout is on another branch (or a detached HEAD) than the one `sync` keeps it on, so `sync` does not update it. | `git switch <branch>`, or set `branch` of the checkout. |
| `wrong-origin` | A `checkout_dir` is not a working copy of the checkout's `repo` (another origin, no origin, no git); its skills are not linked. | Fix `checkout_dir` or `repo`. |

Warnings (not discrepancies) that may appear:

- `template_version X is older than Y of this skenv` — run
  `skenv repo upgrade` in that repository, review and commit the
  regenerated files.
- `git fetch failed` — offline or no access; `unpushed`/`behind` may be stale.

List the manifest, the store and the agent directories, then every skill of
the manifest: KIND is editable for a skill of a checkout (linked from a git
working copy, VERSION is its path) and pinned for a dependency (a copy at a
commit, VERSION is the commit). STATE is:

- installed: in the store and linked into every agent directory;
- not synced: in the manifest, but its copy or a link is missing or out of
  date: run `skenv sync`;
- conflict: a path skenv does not manage is in the way: `skenv sync --adopt`
  backs it up and replaces it;
- not selected: a skill of a checkout left out by its include or exclude;
- excluded on this machine: left out by the rules of this machine
  (user.machines.<name>).

Checkouts that are not cloned yet are listed below the table. It is
read-only and offline and exits 0: `skenv doctor` is the check. Project skills
are files committed with the project; `skenv doctor --project` checks them.

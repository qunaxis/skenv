Show the configuration as it applies on this machine: the manifest and where
its location came from (--manifest, $SKENV_MANIFEST or the tool config),
the machine name and its source (the tool config "machine",
$SKENV_MACHINE, the full or the short hostname) with the machine rules
that apply, $HOME and $CLAUDE_CONFIG_DIR, the store, each agent directory
with why it is on or off (listed, detected, not detected), each checkout
with its resolved directory, the branch sync keeps it on and its state,
and each skill of the checkouts and dependencies with why it is installed
or not (include, exclude, machine rules). The raw configuration is the
skenv file itself.

The file alone does not reproduce everything: dependencies are pinned to
a commit, but checkouts follow their branch and local edits, and agent
detection, the machine name and $HOME come from the machine. The output
ends with that note.

- Reads: the tool config, the manifest, the checkouts (local git commands)
  and the agent directories.
- Changes: nothing.
- Network: none.
- Next: "skenv list" for what is installed, "skenv doctor" to check.

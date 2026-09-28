Compare the machine with the manifest. It changes no skill, link or file,
but runs `git fetch` in each checkout (network access; it updates their
remote-tracking branches) to report unpushed and behind.
Classes: missing, extra-managed, unmanaged, wrong-rev, broken-link, conflict,
dirty, unpushed, behind, agent-mismatch, manifest-checkout (the manifest is
not in the working copy its checkout names, so sync never pulls it),
wrong-origin (a checkout_dir that is not a working copy of its repo) and
wrong-branch (a checkout not on the branch sync keeps it on).

In a project (a git repository whose skenv file has [project]), doctor
compares the project with its [project] section instead, offline, so it can
run in CI. Classes: missing, wrong-rev, modified (a copy edited locally),
extra-managed, conflict, broken-mirror, mirror-drift, unmanaged (a skill
only in a mirror). --manifest checks the machine from there; --project
requires a project.

Exit code: 0 in sync, 1 discrepancies, 2 error. Exit code 0 is the check
that a sync converged: sync itself exits 0 with warnings.

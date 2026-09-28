skenv installs agent skills (Claude Code, Codex, pi) from a manifest you
keep in git, and keeps every machine in line with it. The manifest is the
[user] section of skenv.toml in a git repository: skills of editable git
working copies ("checkouts") are linked, other skills are copies pinned to
a commit ("dependencies").

First steps, by situation:
- No manifest yet: `skenv init` in a git repository starts one.
- Skills already installed (npx skills, copies): `skenv init --import`
  starts one, records them and takes over those it can match to a commit.
- Another machine: `skenv clone <repo>` clones your manifest repository
  and uses it; `skenv use .` in a checkout you already have.

Then `skenv sync` applies the manifest, `skenv list` shows the skills and
whether they are installed, `skenv vendor add <repo>` installs a third-party
skill and `skenv doctor` checks the machine.

In a project repository whose skenv file has a [project] section, sync and
doctor work on the skills of the project instead. The manifest location and
the config file: https://qunaxis.github.io/skenv/configuration and
"skenv schema config".

Exit codes: 0 success, 1 problems found, 2 error. Warnings do not change
the exit code; "skenv doctor" exits 0 only when the machine matches.

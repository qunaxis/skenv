Pin a third-party skill in the manifest (HEAD of the default branch unless
--rev) and sync it. The manifest change is not committed.

--path is the directory of the skill inside the repository; it can be left
out when the repository has exactly one SKILL.md. The skill is installed
under --name, by default the last element of that directory (the
repository name when the skill is at its root), lowercased.

It adds a [user.dependencies.<name>] table with repo, skill_dir and commit.
<repo> is written as given: owner/repo (or github:owner/repo) on github.com,
gitlab:group/sub/repo, codeberg:owner/repo, <alias>:path of a host declared
under [user.git_hosts.<alias>], or a full git URL; a relative local path is
written absolute. An unknown prefix is an error. See
https://qunaxis.github.io/skenv/git-hosts

With --project: add a [project.dependencies.<name>] table to the skenv file
of the current repository and sync the project, which copies the skill into
its dir and mirrors. Its hosts are the ones declared under
[project.git_hosts.<alias>]. Commit the file and the copies with the
project.

- Reads: the manifest (or [project]) and the repository of the skill.
- Changes: the manifest (or [project]), the copy of the skill in the store
  (or the project), its agent links (or mirrors) and the state file.
- Network: fetches the repository into the clone cache ~/.cache/skenv/repos.
- Conflicts: an unmanaged path with the skill's name is an error and stays;
  --adopt moves it to ~/.local/state/skenv/backup/<ts>/ and replaces it.
- Preview: --dry-run writes nothing except the clone cache.
- Next: commit the skenv file; "skenv doctor".

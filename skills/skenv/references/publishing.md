# Publishing a skill

A skill moves from the private repository (the repository whose
`skenv.toml` has `[repository]` `visibility = "private"`) to the public one
(`visibility = "public"`) only in this order; every step blocks the next.

1. **Source.** `metadata.source` is not `book`, `internal` or
   `third-party-copy`. Skills derived from a book are not published; only a
   rewrite into own material (no chapter structure, quotes, examples or
   figures from the book, the book cited as a source) can be.
2. **No internal data.** No names of internal projects, processes, domains
   or teams of the owner or an employer. Check with
   `skenv lint --publish` (the stop-list lives in
   `~/.config/skenv/denylist.txt` or `$SKENV_DENYLIST`, never in a public
   repository) and read every file yourself.
3. **License.** A `LICENSE` file in the skill directory or a `license`
   field in the frontmatter.
4. **History.** Move only the history of the skill directory:

   ```sh
   # in the private repository
   git subtree split --prefix skills/<name> -b publish-<name>
   git log -p publish-<name>            # review for step 2 and secrets
   gitleaks git --log-opts=publish-<name> --redact .
   # on findings: git filter-repo on that branch until it is clean
   # in the public repository; <private> is the private repository's path
   git subtree add --prefix skills/<name> <private> publish-<name>
   ```

5. **Remove the private copy.** Delete the skill from the private repository
   in a commit that names the new path, then `skenv sync`; the links switch
   to the public repository.
6. **Making a repository public is the owner's decision.** Only when the
   repository has at least one skill, `visibility = "public"` in
   `[repository]` of `skenv.toml` (and no `[user]` there) plus `skenv repo apply` (CI moves to GitHub-hosted runners),
   `skenv lint --publish` and gitleaks over the whole history are clean,
   and the self-hosted runner is removed. An agent prepares these steps but
   never runs `gh repo edit --visibility public` itself.

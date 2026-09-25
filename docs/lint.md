# Validation and publication

`skenv lint` checks skills before they reach your agents or the public:
the format of `SKILL.md`, links, sizes and secret-like files, and, with
`--publish`, what must hold before a repository goes public. It works in
any directory; hooks and CI of a [harness](harness.md) run the same checks.

- [Checking skills](#checking-skills)
- [Rules](#rules)
- [Publication check](#publication-check)
- [While an agent edits](#while-an-agent-edits)

## Checking skills

```sh
skenv lint                    # every skill under the current directory
skenv lint skills/my-skill    # one skill
skenv lint --staged           # skills with files in the git index
skenv lint --publish          # before the repository goes public
```

A skill is a directory with `SKILL.md`. Inside git only the files git would
commit are checked. Each problem is one line with its rule ID; the summary
`lint: N skills, N problems` goes to stderr. Exit code 0: clean, 1:
problems, 2: could not run. Reference: [skenv lint](commands/skenv_lint.md).

## Rules

| Rule | Check                                                                                                                         |
| ---- | ----------------------------------------------------------------------------------------------------------------------------- |
| L1   | YAML frontmatter is valid and has `name` and `description`                                                                    |
| L2   | `name` equals the directory name and is lowercase letters, digits and single hyphens, with no hyphen at the start or end       |
| L3   | Agent Skills limits: `name` ≤ 64 characters; `description` not empty, ≤ 1024; `license` a string, `metadata` a map of strings |
| L4   | relative links in `SKILL.md` and `references/*.md` point to existing files inside the skill                                   |
| L5   | no file larger than 10 MB; no `.env`, `*.pem`, `*.key`, `.credentials*`                                                       |
| L6   | executable files start with a shebang                                                                                         |

## Publication check

`--publish` adds the publication check P1, which CI of public repositories
requires:

- every skill has a license: a non-empty `LICENSE*` file in the skill or a
  `license` field in the frontmatter;
- `metadata.source` is not `book`, `internal` or `third-party-copy`;
- no file path or text in the whole repository (every file git would
  publish, not only the skills) contains a phrase of the stop-list: one
  phrase per line, `#` comments, matched case-insensitively with any run of
  whitespace (line breaks and no-break spaces included) treated as one
  space, so wrapped phrases are found. The stop-list is read from
  `$SKENV_DENYLIST` or `~/.config/skenv/denylist.txt`, lives outside public
  repositories and is never printed: findings cite the stop-list line number
  and redact matching path components. Without a stop-list `--publish`
  refuses to run (exit 2);
- [gitleaks](https://github.com/gitleaks/gitleaks) finds no secret in the
  whole git history (redacted report with file, commit and rule). gitleaks
  must be installed for `--publish`.

> [!CAUTION]
> `--publish` fails closed: without a stop-list, with an empty one, or
> without gitleaks it exits 2 instead of passing. Run it before the first
> push to a public repository, not after: once pushed, content stays in
> the git history, forks and clones even if you delete it later.

## While an agent edits

`skenv lint --hook` is the Claude Code PostToolUse mode: it reads the hook
event on stdin and lints the skill of the edited file. It takes no paths,
`--staged` or `--publish`. The harness installs it in
`.claude/settings.json`; see [Claude Code hook](claude-code-hook.md).

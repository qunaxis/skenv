# Create a skill

A skill is a directory with a `SKILL.md`. In a skills repository it lives
in `skills/<name>/`. `skenv new` writes one and checks it; no hooks, CI or
other repository setup is needed.

- [1. Scaffold it](#1-scaffold-it)
- [2. Write and check it](#2-write-and-check-it)
- [3. Make it available to your agents](#3-make-it-available-to-your-agents)
- [4. Commit and push](#4-commit-and-push)
- [Optional: repository checks and CI](#optional-repository-checks-and-ci)

## 1. Scaffold it

In your skills repository:

```sh
cd ~/src/<skills-repo>
skenv new my-skill --dir .
```

```text
created ~/src/<skills-repo>/skills/my-skill (SKILL.md, references/notes.md)
next steps:
  - fill in the description and instructions, then run `skenv lint`
  - run `skenv link` to make it available to your agents
```

`--dir` is any directory inside the target git repository. Without it,
`skenv new` uses the only own repository of the manifest; with several, the
one whose `[repo]` has `--visibility` (default `private`). Reference:
[skenv new](commands/skenv_new.md).

## 2. Write and check it

Fill in `description` in the frontmatter of `SKILL.md` (what the skill
does and when the agent should use it) and the instructions below it; put
long material in `references/` and link it. Then:

```sh
skenv lint skills/my-skill
```

`skenv lint` checks the frontmatter, the name, the Agent Skills size
limits, relative links, file sizes, secret-like files and shebangs, and
exits 1 with one line per problem (see
[Validation and publication](lint.md)).

## 3. Make it available to your agents

The last line of `skenv new` says what to do:

- **`run skenv link`**: the repository is an own repository of your
  manifest. Own skills are linked from the working copy, so there is
  nothing to pull:

  ```sh
  skenv link
  skenv list   # my-skill: editable, installed
  ```

- **`... is not an own repository of the manifest`**: add the repository
  under `[[environment.own]]` (see
  [Add a repository of your own skills](manage-skills.md#add-a-repository-of-your-own-skills))
  and run `skenv sync`.
- **`... lists its skills in skills, without my-skill`** or **`matches
  exclude`**: the entry selects only some skills; add the name to `skills`,
  or change `exclude`, in the manifest.
- **`no manifest is configured`**: start one with `skenv init` (see
  [Create your first environment](first-environment.md)).

Start a new agent session to see the skill.

## 4. Commit and push

skenv never commits. Other machines get the skill after you push it and
they run `skenv sync`:

```sh
git add skills/my-skill
git commit -m "feat(skills): add my-skill"
git push
```

Before a repository goes **public**, add a license to each skill and run
the publication check, `skenv lint --publish`, *before the first push*
(see [Validation and publication](lint.md#publication-check)).

## Optional: repository checks and CI

`skenv repo init` sets up git hooks that lint staged skills and scan for
secrets, a CI pipeline (GitHub Actions or GitLab CI) and linter configs.
It needs extra tools (lefthook, gitleaks, uv) and, for private
repositories, a choice of CI runner. It is useful once a repository has
several skills or several authors, and not needed to create or use a
skill. See [Repository checks and CI](harness.md).

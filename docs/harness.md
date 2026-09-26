# Repository checks and CI

`skenv repo` sets up and verifies optional tooling around a repository of
skills, called the harness: git hooks, the CI pipeline (GitHub Actions or
GitLab CI), linter configs and the rules for coding agents. It works in any
git repository with skills under `skills/<name>/`.

You do not need it to install, sync or create skills: `skenv new`,
`skenv lint` and everything in the manifest work without it. Set it up when
a repository has several skills or several authors, or before it goes
public.

- [Before you start](#before-you-start)
- [Set it up](#set-it-up)
- [`[repository]` in `skenv.toml`](#repository-in-skenvtoml)
- [Commands](#commands)
- [Managed files](#managed-files)
- [CI](#ci)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)
- [Skill tests](#skill-tests)
- [Harness versions](#harness-versions)

## Before you start

The generated hooks and pipeline need, on each machine that commits to the
repository:

| Tool | Used by |
| ---- | ------- |
| [lefthook](https://github.com/evilmartians/lefthook) | installs and runs the git hooks (`lefthook install`) |
| skenv | the pre-commit hook runs `skenv lint --staged`; the Claude Code hook runs `skenv lint --hook` |
| [uv](https://docs.astral.sh/uv/) | the pre-commit hook runs ruff and shellcheck through `uvx` |
| [gitleaks](https://github.com/gitleaks/gitleaks) | the pre-commit hook scans staged changes for secrets; `skenv lint --publish` scans the history |

A **public** repository also needs a stop-list
(`~/.config/skenv/denylist.txt` or `$SKENV_DENYLIST`): its pre-push hook
runs the publication check (see [Validation and publication](lint.md)).
`skenv repo init` lists which of lefthook, uv and gitleaks it found on
`PATH` and warns about the missing ones; without them, commits in the
repository fail.

Decide where the CI jobs of a **private** repository run: by default on a
self-hosted runner with the labels (GitHub) or tags (GitLab) `self-hosted`,
`linux`, `docker`. Without such a runner the jobs wait in the queue. On
GitHub, `--runner ubuntu-latest` uses the GitHub-hosted runners instead;
see [Runners](#runners). Public repositories always run on the hosted
runners: GitHub-hosted `ubuntu-latest` on GitHub, the shared runners on
GitLab.

## Set it up

```sh
cd ~/src/<skills-repo>
skenv repo init --visibility private --runner ubuntu-latest --dry-run
skenv repo init --visibility private --runner ubuntu-latest
skenv repo check        # exit 0: the managed files match the templates
git add -A && git commit -m "chore: set up the skenv harness" && git push
```

`ubuntu-latest` is a GitHub runner. For a repository on GitLab, pass the
tags of your runners instead (`saas-linux-small-amd64` for the GitLab.com
instance runners), or leave `--runner` out for the self-hosted default.

`repo init` lists the files it creates, the CI system it picked, where the
private CI jobs run and how to change that, the hook tools it found and
missed, then runs `lefthook install`:

```text
create skenv.toml
create lefthook.yml
create .github/workflows/check.yml
...
harness 0.6.0 (private, ci github) set up in ~/src/<skills-repo>
CI jobs run on runners ubuntu-latest (repository.ci.github.runs_on); to change them, edit it and run `skenv repo apply`
git hooks need lefthook, uv and gitleaks: found lefthook, uv, gitleaks; missing none
lefthook install: hooks active
```

The rest of this page describes the settings, the managed files and the
pipelines.

## `[repository]` in `skenv.toml`

A repository describes its harness in the `[repository]` section of its
skenv file (`skenv.toml`, or `skenv.yaml`/`skenv.yml`/`skenv.json`; see
[the skenv file](skenv-file.md)):

```toml
[repository]
template_version = "0.6.0"     # desired template version; `skenv repo upgrade` changes it
visibility       = "private"   # declared publication policy: private | public

[repository.ci.github]         # or [repository.ci.gitlab]
runs_on = ["self-hosted", "linux", "docker"]   # private only; GitLab: tags = [...]
```

| Key                              | Values                    | Default                               | Meaning |
| -------------------------------- | ------------------------- | ------------------------------------- | ------- |
| `template_version`               | a version such as `0.6.0` | required                              | The template version the repository asks for, and the skenv release its CI installs. Only `skenv repo init` and `skenv repo upgrade` write it; see [Harness versions](#harness-versions). |
| `visibility`                     | `private`, `public`       | required                              | The declared publication policy. It decides the runners, the publication check and whether `[user]` is allowed. skenv never reads or changes the access setting on the hosting service: keep the two in line yourself. |
| `[repository.ci.github]`         | a table                   | used when neither table exists        | Generate GitHub Actions (`.github/workflows/check.yml`). |
| `[repository.ci.gitlab]`         | a table                   |                                       | Generate GitLab CI (`.gitlab-ci.yml`). Both tables at once are an error. See [Choosing the CI system](#choosing-the-ci-system). |
| `ci.github.runs_on`              | a list of strings         | `["self-hosted", "linux", "docker"]`  | The `runs-on` labels of the jobs of a **private** repository. |
| `ci.gitlab.tags`                 | a list of strings         | `["self-hosted", "linux", "docker"]`  | The runner `tags:` of the jobs of a **private** repository. |

`runs_on` and `tags` in a public repository are an error: its CI always
runs on the runners of the host (see [Runners](#runners)).

The repository that holds your manifest has a `[user]` section in the same
file. A public repository must not: `skenv repo init` refuses and
`skenv repo check` fails, because the manifest is personal (home paths,
machine names, which skills you use).

> [!WARNING]
> In a private repository the CI jobs run on `runs_on` (GitHub) or `tags`
> (GitLab), which default to a self-hosted runner
> (`["self-hosted", "linux", "docker"]`). Without such a runner the jobs
> wait in the queue: GitHub fails them after 24 hours, and GitLab leaves
> them pending. See [Runners](#runners) for the hosted alternatives.
> Public repositories always run on the runners of the host, so pull and
> merge requests from forks never execute on your own machines.

## Commands

### `skenv repo init`

```sh
skenv repo init --visibility private                          # CI detected from origin
skenv repo init --visibility private --runner ubuntu-latest   # GitHub-hosted runners
skenv repo init --visibility public --ci gitlab               # or: --ci github
```

Adds `[repository]` with `template_version = "0.6.0"` and the schema
directive (creating `skenv.toml` if there is no skenv file, or `skenv.yaml`
or `skenv.json` with `--format yaml|json`) and every managed file, then
runs `lefthook install`. Refuses if `[repository]` already exists. An
existing skenv file gets `[repository]` in its own format; `--format`
that disagrees with it is an error (see
[Creating the file](skenv-file.md#creating-the-file)). `--ci` picks the CI
system; without it, the host of `origin` decides, and the output says what
was detected, and writes `[repository.ci.github]` or
`[repository.ci.gitlab]`. `--runner` (comma-separated or repeated) sets
`runs_on` or `tags` of a private repository and is an error for a public
one; the output names the
runners and lists the hook tools found and missing on `PATH`. Reference:
[skenv repo init](commands/skenv_repo_init.md).

### `skenv repo apply`

```sh
skenv repo apply
```

Regenerates the managed files and blocks from the templates of
`template_version`, then runs `lefthook install`. It never changes the
skenv file. skenv embeds the templates of one version (0.6.0); when
`template_version` is another one, `apply` stops:

```text
error: skenv.toml: template_version 0.5.0 is not the template set of this skenv (0.6.0); run `skenv repo upgrade` to move the repository to 0.6.0, or use skenv 0.5.0
```

The pipeline follows the table under `repository.ci`; to switch, see
[Switching the CI system](#switching-the-ci-system). Reference:
[skenv repo apply](commands/skenv_repo_apply.md).

### `skenv repo upgrade`

```sh
skenv repo upgrade --dry-run
skenv repo upgrade
```

Moves the repository to the templates of the installed skenv: sets
`template_version` to 0.6.0 and points the schema directive at that
version (comments and formatting of the skenv file stay), then regenerates
the managed files as `apply` does. The generated CI installs the skenv
release named by `template_version`, so that release must exist before you
push. Commit the skenv file together with the regenerated files.
Reference: [skenv repo upgrade](commands/skenv_repo_upgrade.md).

### `skenv repo check`

```sh
skenv repo check
```

Compares the repository with the templates of `template_version`. Each
difference is listed with exit code 1:

```text
skenv.toml: template_version 0.5.0; this skenv generates 0.6.0: run `skenv repo upgrade`
lefthook.yml: generated by the 0.5.0 templates, template_version is 0.6.0: run `skenv repo apply`
ruff.toml: differs from the 0.6.0 template (edited by hand?): run `skenv repo apply`
```

So are a missing managed file or block, a `CLAUDE.md` or
`.claude/CLAUDE.md` (it disables `AGENTS.md` in Claude Code), and a managed
pipeline of the CI system that `repository.ci` does not name, left over
after a switch. A skenv file without a schema directive, or with one for
another version than `template_version`, is a warning on stderr that does
not change the exit code; `skenv repo upgrade` writes the directive.
Reference: [skenv repo check](commands/skenv_repo_check.md).

### Common flags

All commands take `--dir` (default: the current repository); `init`,
`apply` and `upgrade` take `--dry-run`, and `--force` to replace existing files that skenv
does not manage yet (without it they refuse and list them, so a
hand-written workflow or `.claude/settings.json` is never lost silently).

> [!NOTE]
> `skenv repo check` fails on a `CLAUDE.md` or `.claude/CLAUDE.md` in the
> repository, because it disables `AGENTS.md` in Claude Code. Keep
> repository rules in `AGENTS.md`.
When `lefthook` is not installed, `init`, `apply` and `upgrade` still write the files
and print a warning.

### `skenv init` and the harness

`skenv init` starts a manifest (`[user]`) and
does not set up `[repository]`, so it has no `--ci`: run `skenv repo init`
afterwards, which detects the CI system as above. In a repository without
an `origin` yet, `skenv init --remote <repo>` names the future remote of
the repository's own checkout (see
[Git hosts](git-hosts.md#starting-a-manifest-with-skenv-init)); `skenv repo init`
then has no origin to detect from, so pass `--ci gitlab` for GitLab.

## Managed files

Managed files start with `managed by skenv <version> — do not edit`, the
template version that generated them:

- `lefthook.yml` — pre-commit: `skenv lint --staged`, ruff (format, check)
  on staged `*.py`, shellcheck on staged `*.sh` and shell executables,
  gitleaks on the staged diff; pre-push: `check` of changed skills and, in
  public repositories, `skenv lint --publish` (needs the local stop-list),
  so nothing is pushed before the publication check.
- The CI pipeline of `repository.ci`: `.github/workflows/check.yml`
  (GitHub Actions) or `.gitlab-ci.yml` (GitLab CI). See [CI](#ci).
- `ruff.toml`, `pyrightconfig.json`, `.editorconfig`, `.markdownlint.yaml`.
- `.claude/settings.json` — the
  [Claude Code hook](claude-code-hook.md).

`AGENTS.md` (repository rules) and `.gitignore` get a managed block between
`<!-- skenv:begin … -->` / `<!-- skenv:end -->` (`# skenv:begin` / `# skenv:end`);
text outside the block is yours and is kept by `apply`. The `AGENTS.md`
block names the pipeline of `repository.ci` and, for a public repository,
the name of the stop-list secret or variable.

## CI

### Choosing the CI system

The table under `repository.ci` chooses the CI system:
`[repository.ci.github]` for GitHub Actions (`.github/workflows/check.yml`),
`[repository.ci.gitlab]` for GitLab CI (`.gitlab-ci.yml`). Neither means
GitHub Actions; both is an error. `skenv repo init --ci github|gitlab`
writes the table; without `--ci`, the CI system comes from the host of
`origin`, resolved like a `repo` value of the manifest (see
[Git hosts](git-hosts.md)):

| `origin`                                                                 | CI table   |
| ------------------------------------------------------------------------ | ---------- |
| on gitlab.com (https or ssh)                                             | `gitlab`   |
| on a host declared with `provider = "gitlab"`                            | `gitlab`   |
| on github.com, codeberg.org, another declared or unknown host            | `github`   |
| no `origin`                                                              | `github`   |

Declared hosts are read from the repository's own `[user.git_hosts]` when
it has a `[user]` section, else from the manifest in the tool config. The
origin URL is never printed; the output only says what decided.

### What the pipeline runs

Both pipelines run the same checks, on every push and every pull or merge
request:

| Check                                   | GitHub Actions job | GitLab CI job      |
| --------------------------------------- | ------------------ | ------------------ |
| `skenv repo check`, `skenv lint`        | `skenv`            | `skenv`            |
| gitleaks over the whole history         | `gitleaks`         | `gitleaks`         |
| `skenv lint --publish` (public only)    | `gitleaks`         | `gitleaks`         |
| ruff format and check, pyright, shellcheck, markdownlint | `lint` | `lint`         |
| `check` of changed skills, Python 3.9 and 3.12 | `changes` + `skills` (matrix skill × Python) | `skills` (matrix over Python, the changed skills in turn) |

Every tool version is pinned in the templates and changes only with a new
harness version:

| Tool                     | Version                          |
| ------------------------ | -------------------------------- |
| skenv                    | the `template_version`           |
| gitleaks                 | 8.30.1                           |
| ruff                     | 0.16.9                           |
| pyright                  | 1.1.414                          |
| shellcheck (shellcheck-py) | 0.11.0.1                       |
| markdownlint-cli2        | 0.23.3                           |
| uv                       | 0.12.10                          |
| Node.js                  | 22 (GitHub: `actions/setup-node`; GitLab: the `node:22-bookworm` image) |

::: code-group

```text [GitHub Actions]
.github/workflows/check.yml
- on: push, pull_request
- private: runs-on = runs_on; setup actions run with caching off, uv's cache
  and Python builds in $RUNNER_TOOL_CACHE (the runner's shared tool cache,
  not the Actions storage quota)
- public: runs-on ubuntu-latest
- skills with ./check are found in a `changes` job, then a matrix job runs
  each skill on Python 3.9 and 3.12
```

```text [GitLab CI]
.gitlab-ci.yml
- workflow rules: merge request pipelines; branch pipelines only for
  branches without an open merge request (no duplicate pipelines); tags
- every job runs in the node:22-bookworm image: the runner needs the Docker
  (or Kubernetes) executor; tools are downloaded at the pinned versions
- GIT_DEPTH 0: the full history, for gitleaks and the changed skills
- private: default tags = tags; public: no tags (the runners the
  project allows for untagged jobs, see Runners)
- one `skills` job per Python version (parallel:matrix PYTHON 3.9, 3.12)
  runs ./check of every changed skill; it fails if any of them fails
```

:::

### Runners

`runs_on` and `tags` apply to private repositories only. A public repository runs on
the runners of the host, never on self-hosted ones, so code from a fork
never runs on your machines. The same rule holds for both CI systems. On
GitHub the workflow says `runs-on: ubuntu-latest`. On GitLab the jobs of a
public repository have no tags, and GitLab gives an untagged job to any
runner the project allows for untagged jobs: in **Settings → CI/CD →
Runners** of a public project, turn off group runners and do not keep
project runners with "Run untagged jobs". On a self-managed instance, the
instance runners are the administrator's machines; the jobs run where the
administrator allows.

::: code-group

```toml [GitHub]
# runs_on is the runs-on of every job: labels of a self-hosted runner,
# or a GitHub-hosted runner.
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.github]
runs_on = ["self-hosted", "linux", "docker"]   # default
# runs_on = ["ubuntu-latest"]                  # GitHub-hosted
```

```toml [GitLab]
# tags are the tags: of every job (under default:). A runner picks a job
# only when it has all of these tags.
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.gitlab]
tags = ["self-hosted", "linux", "docker"]      # default
# tags = ["saas-linux-small-amd64"]            # GitLab.com instance runners
```

:::

On GitLab, register the runner with the Docker executor and exactly the
tags of `tags` (the default expects `self-hosted`, `linux` and `docker`).

### Changed skills

A skill's `check` runs when a file under `skills/<name>/` changed. The base
of the comparison:

::: code-group

```text [GitHub Actions]
pull request   github.event.pull_request.base.sha
push           github.event.before
fallback       every skill with ./check when the base is empty, all zeros
               (first push of a branch) or not in the history
```

```text [GitLab CI]
merge request  CI_MERGE_REQUEST_TARGET_BRANCH_SHA in merged results
               pipelines, else CI_MERGE_REQUEST_DIFF_BASE_SHA
               (CI_PIPELINE_SOURCE = merge_request_event)
push           CI_COMMIT_BEFORE_SHA
fallback       every skill with ./check when the base is empty, all zeros
               (first push of a branch, tags, scheduled or manual
               pipelines) or not in the history
```

:::

The job log prints the base and the list, for example
`all=false base=4f2c… skills=[write-tests]`. On GitHub the matrix job is
skipped when no skill changed; on GitLab the two `skills` jobs still start
and finish without running a check.

### Public repositories: the stop-list

A public repository also runs `skenv lint --publish`, which needs the
publication stop-list (one phrase per line, the file
`~/.config/skenv/denylist.txt` on your machine). CI reads it from a secret
or variable, writes it to a temporary file and never prints it; skenv
reports a match by line number only. The job fails when it is missing.

::: code-group

```sh [GitHub]
# Actions secret SKENV_DENYLIST, the file as it is
gh secret set SKENV_DENYLIST < ~/.config/skenv/denylist.txt
```

```sh [GitLab]
# CI/CD variable SKENV_DENYLIST_B64, the file base64-encoded on one line:
# GitLab masks only single-line values
base64 < ~/.config/skenv/denylist.txt | tr -d '\n' | glab variable set SKENV_DENYLIST_B64 --masked --hidden
```

:::

On GitLab you can create the variable in **Settings → CI/CD → Variables**
instead: key `SKENV_DENYLIST_B64`, visibility **Masked and hidden** (or
**Masked**), the base64 text as the value. **Protect** it if pipelines run
only on protected branches and tags: GitLab passes a protected variable to
those pipelines only, and on any other branch or merge request the job
fails with "is not set". Protecting it is the safer choice for a public
project: an unprotected variable reaches every pipeline in the project,
including a fork's merge request that a maintainer runs in the parent
project, and only the base64 text is masked, so the fork's
`.gitlab-ci.yml` or `skills/*/check` could print the decoded stop-list.
The trade-off is that the publication check then runs only on protected
branches and tags (the pre-push hook still runs it locally). Never print
the stop-list or its base64 in a job, and never commit either.

### Switching the CI system

`skenv repo apply` generates the pipeline of the table under
`repository.ci`. To move a repository from GitHub to GitLab, or back,
replace the table and apply:

```toml
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.gitlab]   # was [repository.ci.github]; runs_on becomes tags
tags = ["self-hosted", "linux", "docker"]
```

```sh
skenv repo apply --dry-run   # shows the plan
skenv repo apply
```

For `[repository.ci.gitlab]` the plan is:

```text
would create .gitlab-ci.yml
would update AGENTS.md
would remove .github/workflows/check.yml
```

The managed pipeline of the other system is removed (and `.github/workflows`
and `.github` when that leaves them empty); a file there that skenv does
not manage, such as a hand-written `.gitlab-ci.yml` or other workflows, is
kept. Until you apply, `skenv repo check` reports the leftover pipeline.
Move the stop-list to the other system too (see above) and delete the old
secret or variable.

## Examples

Each example is complete: the `[repository]` section in each format, then
the commands that create it. `skenv repo init` writes the section and adds
the schema directive; in YAML and JSON `repository` goes to the top of the
file.

### GitHub, private, self-hosted runner

::: code-group

```toml [skenv.toml]
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.github]
runs_on = ["self-hosted", "linux", "docker"]
```

```yaml [skenv.yaml]
repository:
  template_version: 0.6.0
  visibility: private
  ci:
    github:
      runs_on: [self-hosted, linux, docker]
```

```json [skenv.json]
{
  "repository": {
    "template_version": "0.6.0",
    "visibility": "private",
    "ci": { "github": { "runs_on": ["self-hosted", "linux", "docker"] } }
  }
}
```

:::

```sh
skenv repo init --ci github --visibility private
git add -A && git commit -m "chore: skenv harness"
```

The runner must carry the labels `self-hosted`, `linux` and `docker`.

### GitHub, public

::: code-group

```toml [skenv.toml]
[repository]
template_version = "0.6.0"
visibility       = "public"

[repository.ci.github]
```

```yaml [skenv.yaml]
repository:
  template_version: 0.6.0
  visibility: public
  ci:
    github: {}
```

```json [skenv.json]
{
  "repository": {
    "template_version": "0.6.0",
    "visibility": "public",
    "ci": { "github": {} }
  }
}
```

:::

```sh
skenv repo init --ci github --visibility public
gh secret set SKENV_DENYLIST < ~/.config/skenv/denylist.txt
git add -A && git commit -m "chore: skenv harness"
```

### GitLab.com, private, tagged runner

::: code-group

```toml [skenv.toml]
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.gitlab]
tags = ["self-hosted", "linux", "docker"]
```

```yaml [skenv.yaml]
repository:
  template_version: 0.6.0
  visibility: private
  ci:
    gitlab:
      tags: [self-hosted, linux, docker]
```

```json [skenv.json]
{
  "repository": {
    "template_version": "0.6.0",
    "visibility": "private",
    "ci": { "gitlab": { "tags": ["self-hosted", "linux", "docker"] } }
  }
}
```

:::

```sh
skenv repo init --ci gitlab --visibility private
git add -A && git commit -m "chore: skenv harness"
```

Create a project runner (**Settings → CI/CD → Runners**) with the tags
`self-hosted`, `linux` and `docker`, and register it with the Docker
executor:

```sh
gitlab-runner register --url https://gitlab.com --token <runner authentication token> --executor docker --docker-image node:22-bookworm
```

With an origin on gitlab.com, `--ci gitlab` can be left out.

### GitLab.com, public

::: code-group

```toml [skenv.toml]
[repository]
template_version = "0.6.0"
visibility       = "public"

[repository.ci.gitlab]
```

```yaml [skenv.yaml]
repository:
  template_version: 0.6.0
  visibility: public
  ci:
    gitlab: {}
```

```json [skenv.json]
{
  "repository": {
    "template_version": "0.6.0",
    "visibility": "public",
    "ci": { "gitlab": {} }
  }
}
```

:::

```sh
skenv repo init --ci gitlab --visibility public
base64 < ~/.config/skenv/denylist.txt | tr -d '\n' | glab variable set SKENV_DENYLIST_B64 --masked --hidden
git add -A && git commit -m "chore: skenv harness"
```

The jobs run on the GitLab.com instance runners, without tags.

### Self-hosted GitLab with a host alias

The repository holds the manifest, which declares the server as `work`
(see [Git hosts](git-hosts.md#declaring-a-self-hosted-host)); its origin is
on that server, so `skenv repo init` detects GitLab CI.

::: code-group

```toml [skenv.toml]
[repository]
template_version = "0.6.0"
visibility       = "private"

[repository.ci.gitlab]
tags = ["skills", "docker"]

[user.git_hosts.work]
base_url = "https://git.example.com"
provider = "gitlab"

[user.checkouts.skills]
repo         = "work:platform/skills"
checkout_dir = "."
```

```yaml [skenv.yaml]
repository:
  template_version: 0.6.0
  visibility: private
  ci:
    gitlab:
      tags: [skills, docker]
user:
  git_hosts:
    work:
      base_url: https://git.example.com
      provider: gitlab
  checkouts:
    skills:
      repo: work:platform/skills
      checkout_dir: .
```

```json [skenv.json]
{
  "repository": {
    "template_version": "0.6.0",
    "visibility": "private",
    "ci": { "gitlab": { "tags": ["skills", "docker"] } }
  },
  "user": {
    "git_hosts": {
      "work": { "base_url": "https://git.example.com", "provider": "gitlab" }
    },
    "checkouts": {
      "skills": { "repo": "work:platform/skills", "checkout_dir": "." }
    }
  }
}
```

:::

```sh
git remote add origin git@git.example.com:platform/skills.git
skenv repo init --visibility private   # prints: ci gitlab: detected from origin, on a GitLab host
```

`skenv repo init` writes the default `tags`; replace them with the tags of
your runner (here `skills` and `docker`) and run `skenv repo apply`. A
separate skills repository on the same server finds `work` in the manifest
of the tool config, so its `skenv repo init` detects GitLab CI as well.

## Troubleshooting

**Jobs stay queued or pending.** No runner has the labels of `runs_on` or
the tags of `tags`. GitHub shows "Waiting for a runner to pick up this job"; GitLab
says the job is stuck because no runner has the tags. Register a runner
with exactly those labels or tags (on GitLab with the Docker executor), or
set `runs_on` or `tags` to hosted runners (`["ubuntu-latest"]` on GitHub,
`["saas-linux-small-amd64"]` on GitLab.com) and run `skenv repo apply`.
Public repositories use hosted or shared runners only: on a self-managed
GitLab instance, enable instance runners for the project.

**No merge request pipeline on GitLab.** The workflow rules run a pipeline
for merge requests, and for branches only while they have no open merge
request. Check that CI/CD is enabled for the project (**Settings → General
→ Visibility**) and that `.gitlab-ci.yml` is in the source branch. A merge
request from a fork runs its pipeline in the fork, without your CI/CD
variables, so the publication check fails there. A maintainer can run it
in the parent project, but that pipeline uses the fork's `.gitlab-ci.yml`
and `skills/*/check` with your variables: review those files in the merge
request first, or the fork can print the stop-list.

**gitleaks fails.** The report is redacted. Remove the secret from the
history (rotate it first), or, for a false positive, add its fingerprint to
`.gitleaksignore`. Run `gitleaks git --redact .` locally to see the same
report.

**The publication check fails.** "SKENV_DENYLIST … is not set" or
"SKENV_DENYLIST_B64 … is not set": create the secret or variable (see
[the stop-list](#public-repositories-the-stop-list)); on GitLab a
protected variable is missing on unprotected branches. "not valid base64":
recreate the variable from `base64 < ~/.config/skenv/denylist.txt | tr -d '\n'`.
A finding names the stop-list line, never the phrase: run
`skenv lint --publish` locally, where the file is.

## Skill tests

An executable `skills/<name>/check` runs from the skill directory, in CI for
skills changed in the pull or merge request (against the base) or push
(against the previous head; all skills on a first push, see
[Changed skills](#changed-skills)) and in pre-push for skills changed
against the upstream branch. CI sets `CHECK_PYTHON` and `UV_PYTHON` to 3.9
and 3.12. Third-party Python imports for pyright go to
`requirements-dev.txt`.

## Harness versions

skenv embeds one template set, the harness version of its release (0.6.0).
Two versions describe a repository:

- **Desired**: `repository.template_version`, the version the repository
  asks for, and the skenv release its CI installs. Only `skenv repo init`
  and `skenv repo upgrade` write it; `sync`, `repo apply` and `repo check`
  never change the skenv file.
- **Last applied**: the `managed by skenv <version>` header of each
  generated file and block, written by the run that generated it.

`skenv repo apply` generates the files of the desired version and stops
when that is not the embedded one. `skenv repo check` reports a desired
version older than the embedded one ("run `skenv repo upgrade`") and files
whose header names another version or whose content was edited ("run
`skenv repo apply`"). A `template_version` newer than the installed skenv
is an error: upgrade skenv. `skenv doctor` warns about checkouts whose
`template_version` is older than the templates of the installed skenv.

To move a repository to a new skenv release: install it, run
`skenv repo upgrade`, and commit the skenv file with the regenerated
files.

The publication check and the lint rules are described in
[Validation and publication](lint.md).

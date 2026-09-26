# ADR 0002: Configuration format and the reconciliation contract

- Status: proposed
- Date: 2026-09-26
- Issue: [#38](https://github.com/qunaxis/skenv/issues/38), sections
  "Configuration structure, field names, and selection semantics" and
  "Declarative-design validation"
- Builds on: [ADR 0001](0001-cli-and-config-framework.md)

## Context

The skenv file has three sections, `[repo]`, `[environment]` and
`[project]` ([ADR 0001](0001-cli-and-config-framework.md), #25/#36). Their
keys grew out of the implementation: `own`, `vendor`, `layout`, `host`,
`hosts`, `harness`, `runner`. Two different keys are called `path`, `skills`
names an entity while `exclude` names an operation, `host` and `hosts` mean
a machine and a git server, and `layout.ignore` is an ownership boundary
rather than a layout.

The audit in #38 proposes a new vocabulary and asks for decisions on each
name and on seven gaps in the declarative contract: what the file means
when reconciliation cannot reach it. This ADR records those decisions.

Constraints carried over from ADR 0001 and the repository rules:

- **No backward compatibility before 1.0**, and the owner decided: **no
  migrate command and no legacy reading**. An old key is an error that
  names the new key; `docs/skenv-file.md` describes the move by hand.
- Strict unknown-key validation, generated JSON Schemas, and comment- and
  order-preserving edits in TOML, YAML and JSON stay.
- Runtime dependency: `git` only. No hosting-service API.
- Safety rules stay: never delete or replace a path that is not recorded
  as skenv's (`state.json` at user level, `.skenv` markers in a project)
  without `--adopt`.

## Decision summary

**Keep one skenv file** (`skenv.toml`, `.yaml`, `.yml` or `.json`, one per
repository root). Its top-level sections become:

| Section      | Replaces        | What it declares                                                  |
| ------------ | --------------- | ----------------------------------------------------------------- |
| `user`       | `environment`   | Skills for the agents of the current OS user, across projects     |
| `project`    | `project`       | Skills committed with one project repository (keys renamed below) |
| `repository` | `repo`          | Development tooling of a skills repository: templates, CI, policy |

The arguments of ADR 0001 for one file still hold (one well-known name,
one loader, one schema family), and nothing in #38 argues for splitting
it. Installation scope (`user`, `project`) and repository tooling
(`repository`) stay separate sections, as the audit asks.

A complete `user` section:

```toml
[user]
unmanaged = ["peon-ping-*"]              # names owned by other tools

[user.checkouts.personal]                # an editable working copy
repo         = "example/my-skills"
checkout_dir = "."                       # relative to this file: its own repository
include      = ["*"]                     # default
exclude      = ["experimental-*"]

[user.checkouts.team]
repo         = "work:platform/shared-skills"
checkout_dir = "~/src/shared-skills"
branch       = "main"                    # default: the remote's default branch
include      = ["deploy", "review-*"]

[user.dependencies.diagram]              # a pinned skill; the key is its name
repo      = "example/diagram-tools"
skill_dir = "skills/diagram"             # default "."
commit    = "<full 40-character commit SHA>"

[user.git_hosts.work]
provider = "gitlab"
base_url = "ssh://git@git.example.com"   # the scheme is the transport

[user.agents]
enabled    = ["claude", "pi"]            # omitted: autodetect; [] : none
extra_dirs = []                          # added, never replacing

[user.agents.paths]
claude = "~/.claude/skills"              # per-agent override

[user.storage]
dir = "~/.agents/skills"                 # default

[user.machines.laptop]
exclude = ["deploy"]                     # narrows, never widens

[user.machines.laptop.checkout_dirs]
team = "~/work/shared-skills"            # per-machine location of a checkout
```

`[repository]`:

```toml
[repository]
template_version = "0.6.0"   # desired; only `skenv repo upgrade` changes it
visibility       = "private" # declared policy, not read from the host

[repository.ci.github]       # or [repository.ci.gitlab] with tags
runs_on = ["ubuntu-latest"]
```

`[project]`:

```toml
[project]
dir     = ".agents/skills"
mirrors = [".claude/skills"]

[project.dependencies.karpathy-coder]
repo      = "example/claude-skills"
skill_dir = "engineering/karpathy-coder"
commit    = "<full 40-character commit SHA>"

[project.from.personal]
repo   = "example/my-skills"
skills = ["anti-slop-code", "commit-message"]
commit = "<full 40-character commit SHA>"
```

The tool config `~/.config/skenv/config.*` gains one key, `machine`.

## Decisions on the proposed key mapping

Every row of the "Proposed key mapping" table of #38:

| Current key | Proposed | Decision | New key | Reason |
| --- | --- | --- | --- | --- |
| `environment` | `user` | **Adopt** | `user` | Says where skills apply; `global` could mean system-wide. |
| `own` | `checkouts.<id>` | **Adopt** | `user.checkouts.<id>` | Behavior (editable working copy), not ownership; the ID lets machine rules refer to a checkout. |
| `vendor` | `dependencies.<name>` | **Adopt** | `user.dependencies.<name>`, `project.dependencies.<name>` | The key is the installed name, so `name` and the duplicate-name check disappear. |
| `own.path` | `checkout_dir` | **Adopt** | `checkout_dir` | Removes one of the two `path`s; relative values resolve against the skenv file's directory (see Path resolution). |
| `vendor.path` | `skill_dir` | **Adopt** | `skill_dir` | Removes the other `path`; symmetric with `skills_dir`. |
| `vendor.rev` | `commit` | **Adopt** | `commit` (also `project.from.<id>.commit`) | It only accepts a full SHA; `rev` suggested branches and tags. |
| `own.skills` / `own.exclude` | `include` / `exclude` | **Adopt** | `include`, `exclude` | One selection vocabulary for checkouts and machines; semantics below. |
| `hosts` | `git_hosts` | **Adopt** | `user.git_hosts.<alias>`, `project.git_hosts.<alias>` | Git servers, not computers. |
| `host` | `machines` | **Adopt** | `user.machines.<name>` | Machine-specific rules; selection of the name below. |
| `host.<name>.skip` | `machines.<name>.exclude` | **Adopt**, and add `include` | `machines.<name>.include`, `.exclude` | Same vocabulary; both only narrow. |
| `layout.ignore` | `unmanaged` | **Adopt** | `user.unmanaged` | An ownership boundary, not a layout. |
| `layout.targets` | `agents.enabled`, path overrides, `extra_dirs` | **Adopt** | `user.agents.enabled`, `.paths.<agent>`, `.extra_dirs` | Separates choosing agents, moving one, and adding directories; `targets` replaced the whole table. |
| `layout.store` | `storage.dir` | **Adopt** | `user.storage.dir` | Explicit; the default location does not change (see Store and Codex). |
| `repo.harness` | `repository.template_version` | **Adopt**, as desired state | `repository.template_version` | A version the repository asks for; it no longer advances by itself (gap 1). |
| `repo.runner` | provider-specific CI keys | **Adopt** | `repository.ci.github.runs_on`, `repository.ci.gitlab.tags` | The two lists mean different things; the table present selects the CI system and replaces `repo.ci`. |
| — (`repo`) | `repository` | **Adopt** | `repository` | Not renamed to `project`: it configures tooling, not installation. |

Keys not in the table:

| Key | Decision | New key | Reason |
| --- | --- | --- | --- |
| `own.repo`, `vendor.repo` | Keep | `repo` | Already means "the git repository", in every form of Git hosts. |
| `own.skills_dir` | Keep | `skills_dir` | Clear, and consistent with `skill_dir`. |
| — | **Add** | `user.checkouts.<id>.branch` | Gap 2: the branch sync maintains. |
| — | **Add** | `user.machines.<name>.checkout_dirs.<id>` | A checkout at a different place on one machine without a second manifest; the stable ID is what makes it possible. |
| `hosts.<alias>.url` | **Adapt** | `git_hosts.<alias>.base_url` | Its scheme now selects the transport (behavioral decision 5). |
| `hosts.<alias>.type` | **Adapt** | `git_hosts.<alias>.provider` | "Type" of what; `provider` names the software. Same values. |
| `hosts.<alias>.ssh` | **Remove** | — | `base_url = "ssh://…"` clones over ssh; the https/ssh counterpart is derived for remote recognition. |
| `repo.visibility` | **Keep the name**, redefine | `repository.visibility` | Gap 4: documented as a declared policy that skenv never verifies. `--visibility` of `new` and `repo init` was chosen in #41; renaming the key alone would split the vocabulary. |
| `repo.ci` (string) | **Replace** | `repository.ci.github` / `.gitlab` tables | One place per provider for its settings. |
| `project.vendor` | **Adopt** as in user | `project.dependencies.<name>` | Same concept as in `user`. |
| `project.hosts` | **Adopt** | `project.git_hosts` | Same concept as in `user`. |
| `project.from` (array) | **Adapt** | `project.from.<id>` table with `repo`, `skills_dir`, `skills`, `commit` | Keyed like the rest; `skills` stays a required list of names (not `include`): every copy is committed, so the list is an inventory, not a selection rule with defaults. |
| `project.dir`, `mirrors`, `mirrors_mode` | Keep | same | Already describe a project directory and its mirrors. |
| tool config `manifest` | Keep | `manifest` | Unchanged. |
| — | **Add** | tool config `machine`, `SKENV_MACHINE` | Explicit machine name (behavioral decision 2). |
| `.skenv` marker `repo`/`path`/`rev`, `state.json` | Keep | same | Observed state, not configuration; renaming would rewrite every vendored copy for no user benefit. |

## Decisions on the open naming and semantic questions

1. **`user` / `project` versus `global` / `local`: adopt `user` / `project`.**
   `local` can mean a checkout, a machine override or the current project.
2. **`[repo]` is not renamed to `[project]`: adopt `repository`.** Project
   installation already exists as `[project]` (#36), so the separation is
   concrete, not reserved.
3. **`source.owned` / `source.external`: reject.** Ownership does not decide
   whether a repository is a working copy or a pin: your own repository can
   be pinned (`project.from`), a fork can be edited.
4. **A `sources` wrapper: reject.** `user.checkouts` and `user.dependencies`
   are shorter and lose nothing; a level added for symmetry only lengthens
   every header.
5. **Checkout IDs identify sources, dependency keys are skill names.** IDs
   match `^[a-z0-9][a-z0-9_-]{0,63}$`; `skenv init`, `clone` and `import`
   derive them from the repository name. A skill name defined by two
   sources stays an error.
6. **`include` / `exclude`:**
   - Both take skill names and glob patterns over names (no `/`).
   - Omitted `include` means `["*"]`; `include = []` selects nothing, no
     error and no fallback; omitted `exclude` means `[]`.
   - Include first, exclude second; exclude wins.
   - A literal name in a checkout's `include` that is not a skill of the
     checkout is an error (the typo guard of today's `skills`); a pattern
     that matches nothing is fine.
   - `machines.<name>.include` and `.exclude` apply to the selected set of
     the whole scope (checkouts and dependencies) and only narrow it: a
     machine rule cannot select a skill its source excluded.
   - `unmanaged` is not selection: it protects paths of other tools. A
     selected skill that matches `unmanaged` is an error.
   - Excluding an installed skill removes only skenv's links and copies on
     the next sync; the source files stay in their checkout.
7. **`vendor` commands keep their names.** `vendor` is the verb (copy a
   pinned dependency in; decision 3 of #38's status comment); help and docs
   say they edit `dependencies`.
8. **`github:` prefix: adopt.** `github:owner/repo` is accepted like
   `gitlab:` and `codeberg:`. skenv keeps writing the shorter `owner/repo`.

## Behavioral decisions

1. **Path resolution.** A relative local path in a skenv file resolves
   against the directory of that file: `checkout_dir`,
   `machines.<name>.checkout_dirs`, `storage.dir`, `agents.paths`,
   `agents.extra_dirs`, and a `repo` that is a local path. `~/` resolves
   against the current `$HOME`. `skills_dir`, `skill_dir` and project
   paths are inside their repository. Relative path arguments on the
   command line resolve against the invocation directory; `vendor add` with
   a relative local path writes it absolute. `checkout_dir = "."` is the
   repository that holds the manifest, wherever it is cloned; `skenv init`
   writes it. The tool config records the manifest as an absolute path
   (unchanged). This fixes today's behavior, where a relative `own.path`
   depends on the working directory of the command.
2. **Machine rules.** The machine name is `$SKENV_MACHINE`, else the tool
   config `machine` (the usual precedence: environment over config file),
   else the hostname: the rules of
   `machines."<full hostname>"` when present, otherwise those of
   `machines."<short hostname>"`. Rules of the two are never merged. An
   explicitly configured name with no `machines.<name>` entry is an error
   that lists the names the manifest has; a hostname without rules is
   fine. A `checkout_dirs` key that is not a checkout ID is an error.
3. **Agent selection.** Built-in agents: `claude` (`$CLAUDE_CONFIG_DIR/skills`,
   else `~/.claude/skills`) and `pi` (`~/.pi/agent/skills`). Omitted
   `enabled` detects them by their base directory; an explicit list selects
   exactly those agents whether or not they are detected; `[]` selects none.
   An unknown name is an error. `paths.<agent>` replaces the directory of
   that agent only; `extra_dirs` adds directories. `skenv config show`
   prints the resolved destinations and why each was chosen.
4. **Store and Codex: adapt.** The store stays `~/.agents/skills` by default.
   It is not a Codex-private directory: Codex and other agents that follow
   the `~/.agents/skills` convention read it directly, and moving the
   default would re-home every installation. So `codex` is not an agent
   name (the error says why). Someone who wants a neutral store sets
   `storage.dir = "~/.local/share/skenv/skills"` and, to keep Codex, adds
   `~/.agents/skills` to `extra_dirs`; then leaving it out really disables
   Codex. Changing `storage.dir` makes the next sync create the new store
   and remove only the managed entries of the old one; unmanaged entries
   stay where they are.
5. **Git transport.** `base_url` selects the transport: `https://` (or
   `http://`) clones `<base_url>/<path>.git`, `ssh://user@host[:port][/prefix]`
   clones over ssh. For remote recognition (import, `init`, `clone`) the
   counterpart is derived: `git@<host>:` for an https base, `https://<host>`
   for an ssh one. The `url.<base>.insteadOf` hint is printed only for an
   https base. Built-in prefixes stay https.
6. **Repository policy.** See gap 4. Settings that would be ignored are
   errors: `runs_on` or `tags` in a public repository, and both
   `ci.github` and `ci.gitlab` at once.
7. **Effective configuration: adopt as `skenv config show`** (`--json`).
   It prints the effective view only, since the raw view is the file:
   the manifest and where its location came from, the machine name and
   its source, the matched machine rules, `$HOME` and
   `$CLAUDE_CONFIG_DIR`, the store, each agent with its directory and why
   it is on or off, each checkout with its resolved directory, branch
   target and whether it exists, and each skill with its source and why it
   is selected, excluded or unmanaged. It states that editable checkouts
   follow their branch, so only dependencies are reproducible from the
   file alone.

## The seven declarative gaps

The model of #38 is the contract:

```text
desired state = resolve(manifest, explicit machine context, resolved source content)
plan          = compare(desired state, observed state, ownership records)
apply(plan)   = reconcile safely, reporting anything not brought into the desired state
```

### 1. Desired versus last-applied template version

`repository.template_version` is **desired state**. `skenv repo apply`
generates the files of that version and never edits the skenv file. skenv
embeds one template set; if `template_version` is not that version, `apply`
fails and names the two ways out: install the matching skenv, or run
**`skenv repo upgrade`**, the new explicit operation that sets
`template_version` to the embedded version (comment-preserving) and applies
it. A newer `template_version` than skenv knows stays an error.

The **last-applied** version is observed state and lives where it already
is: the `managed by skenv <version>` header of every generated file.
`skenv repo check` compares both: a desired version older than the embedded
one ("run `skenv repo upgrade`"), and files whose header or content differ
from the desired version ("run `skenv repo apply`"). The CI of a
repository installs the skenv release named by `template_version`, which
therefore must be one that reads this format: `harness.Latest` becomes
0.6.0.

### 2. Checkout revision and origin policy

`user.checkouts.<id>.branch` names the branch sync maintains; omitted, it is
the remote's default branch (`origin/HEAD`, else `git ls-remote --symref`).
For each checkout sync:

| Observed | Result | Skills linked from it | Changes |
| --- | --- | --- | --- |
| missing | clone (`--branch` when set) | yes, after the clone | clone |
| not a git working copy | unresolved (error) | no | none |
| `origin` is another repository | **unresolved (error)** | no | none |
| on another branch or detached | unresolved (warning): local development state | yes, as checked out | none |
| uncommitted changes | unresolved (warning): local development state | yes, as checked out | none |
| diverged from the branch | unresolved (warning) | yes, as checked out | none |
| default branch unknown (offline) | unresolved (warning) | yes, as checked out | none |
| clean, on the branch | fast-forward from `origin/<branch>` | yes | pull |

Nothing is reset, checked out, stashed or re-cloned. A checkout whose origin
is wrong contributes no skills and turns off pruning for the run, so links
it had are not removed as stale. `sync` prints each unresolved checkout and
counts them in its summary; an error exits 1, warnings exit 0 as before
(editing skills in the working copy is the point of a checkout). `doctor`
reports `wrong-origin` and `wrong-branch` next to `dirty`, `unpushed` and
`behind`. An explicit branch never makes sync switch branches: switching is
the user's action.

### 3. Environmental inputs

The inputs outside the file are the machine name, `$HOME`,
`$CLAUDE_CONFIG_DIR`, agent detection, remote branch heads and the content
of editable checkouts. `skenv config show` resolves and prints them
(behavioral decision 7); `list` and `doctor` print the manifest, store and
agent directories they used. The docs claim reproducibility only for
dependencies pinned to a commit.

### 4. Policy versus a remote property

`repository.visibility` is a **declared publication policy**. It selects
local safeguards: a public repository must not carry `[user]`, its
generated CI runs `skenv lint --publish` on the hosted runners, and
`skenv new --visibility` uses it to pick the target checkout. skenv
does not read or change the hosting service's access setting, and the
schema, `repo init` and the docs say so. The key keeps its name (see the
table); verifying it would need a hosting API, which skenv does not use.

### 5. Removal and disabled-state semantics

- Removing a dependency, or excluding a skill by `include`, `exclude` or a
  machine rule, removes only its skenv-managed store entry and links.
- Disabling an agent, or dropping an `extra_dirs` entry, removes only the
  links skenv recorded in that directory.
- Removing or renaming a checkout, or changing its `checkout_dir`, never
  deletes the working copy: skenv does not own it. Its links go; the
  directory stays. A checkout ID has no on-disk identity, so renaming it
  moves nothing.
- Changing `storage.dir` removes the managed entries of the old store and
  creates the new one; unmanaged entries are untouched.
- In a project, removal is limited to copies with a `.skenv` marker whose
  content still matches their hash, as today.

### 6. Reconciliation never rewrites intent

`sync`, `link`, `doctor`, `list`, `config show` and `repo apply` never
write the skenv file. Only explicit desired-state editors do: `init`,
`import`, `vendor add|update|remove`, `repo init` and `repo upgrade`. Tests
assert that the file is byte-identical after `sync` and `repo apply`.

### 7. Scope boundaries between `user` and `project`

Project support exists, so the rules are concrete:

- **Identity and ownership.** A user-scope resource is a path in the store
  or an agent directory recorded in `state.json`. A project-scope resource
  is a directory in the project's `dir` or a mirror entry, owned when it
  carries a `.skenv` marker (copy) or is the symlink skenv places (mirror).
  The two record sets never refer to each other's paths.
- **No overlap.** A project's `dir` and mirrors must not be, contain or lie
  inside the user store or a user agent directory (the defaults and, when
  a manifest is configured, its resolved ones). A home directory that is
  itself a git repository with `[project]` hits this: `project sync` stops
  with an error before changing anything.
- **Pruning is per scope.** User sync prunes only paths in `state.json`;
  project sync prunes only marked copies in its `dir` and mirror entries.
  Neither reads the other's section.
- **Same name in both scopes is allowed.** skenv installs both and does
  not rank them; the agent decides which one it uses, by its own rules.
  `skenv doctor` in a project reports each such name as a warning, so the
  shadowing is visible.
- **Command scope** is chosen as today: `sync` and `doctor` inside a project
  work on the project, `--project` requires one, `--manifest` selects the
  user scope.

## Errors for the old format

Parsing stops on any old key with one error that lists every old key in
the file with its replacement, so one pass fixes the file, for example:

```text
skenv.toml: this skenv file uses keys of skenv before 0.6; rename them (https://qunaxis.github.io/skenv/skenv-file#moving-to-the-0-6-format):
  [environment] → [user]
  environment.own → user.checkouts.<id>, one table per checkout keyed by an ID ([user.checkouts.<id>])
  environment.own.path → checkout_dir
```

Every row of the mapping above has such a line, including `skills`
(→ `include`), `host`/`hosts` (→ `machines`/`git_hosts`) and the two
`path`s (→ `checkout_dir`, `skill_dir`). `docs/skenv-file.md` gets a manual
migration table with examples in TOML, YAML and JSON.

## Consequences

- Every write path produces the new format: `init`, `clone`/`use` (tool
  config), `import`, `import --project`, `vendor add|update|remove` with
  and without `--project`, `repo init|apply|upgrade`, `new`. The TOML editor
  works on `[user.dependencies.<name>]` tables instead of arrays; YAML and
  JSON keep their docedit path.
- New commands: `skenv config show`, `skenv repo upgrade`. New doctor
  classes: `wrong-origin`, `wrong-branch`. `sync` prints unresolved
  checkouts.
- `harness.Latest` becomes 0.6.0: generated CI installs a skenv that reads
  this format, so 0.6.0 must be released before a repository runs `skenv
  repo upgrade`.
- The schemas, the generated command reference, the examples and every
  guide change. External formats (`skills-lock.json`) keep their names.
- **Breaking:** every existing skenv file must be rewritten by hand; no
  release reads both formats. Relative `own.path` values change meaning
  (they now follow the file, not the working directory).
- **Not done here:** a neutral store by default, verifying visibility
  against a host, per-agent skill selection, a separate lock file.

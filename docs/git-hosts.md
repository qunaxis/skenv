# Git hosts

The `repo` of an own or vendor entry names a git repository. skenv reads
GitHub, GitLab and Codeberg short forms, aliases for self-hosted servers
that you declare in the manifest, and any git URL. The same forms work on
the command line: `skenv vendor add gitlab:example-org/team/tools`.

- [Forms of `repo`](#forms-of-repo)
- [Declaring a self-hosted host](#declaring-a-self-hosted-host)
- [Why aliases live in the manifest](#why-aliases-live-in-the-manifest)
- [Authentication](#authentication)
- [The canonical URL](#the-canonical-url)
- [Errors and fixes](#errors-and-fixes)
- [Starting a manifest with `skenv init`](#starting-a-manifest-with-skenv-init)

## Forms of `repo`

| Form                         | Example                                     | Resolves to                                           | Use it for                                                                 |
| ---------------------------- | ------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------- |
| `owner/repo`                 | `example-org/skills`                        | `https://github.com/example-org/skills.git`           | github.com                                                                 |
| `gitlab:group/repo`          | `gitlab:example-org/skills`                 | `https://gitlab.com/example-org/skills.git`           | gitlab.com                                                                 |
| `gitlab:group/sub/.../repo`  | `gitlab:example-org/team/ai/skills`         | `https://gitlab.com/example-org/team/ai/skills.git`   | gitlab.com subgroups, any depth                                            |
| `codeberg:owner/repo`        | `codeberg:example-org/skills`               | `https://codeberg.org/example-org/skills.git`         | codeberg.org                                                               |
| `<alias>:path`               | `work:platform/skills`                      | `https://git.example.com/platform/skills.git`         | a host declared under [`[environment.hosts.<alias>]`](#declaring-a-self-hosted-host) |
| `https://...`                | `https://git.example.com/platform/skills.git` | as written                                          | any host over https, including one that is not declared                    |
| `ssh://...`                  | `ssh://git@git.example.com:2222/platform/skills.git` | as written                                   | ssh on a non-default port, or an ssh host alias without a dot              |
| `[user@]host:path` (scp)     | `git@git.example.com:platform/skills.git`   | as written                                            | ssh, when every machine has the key; prefer https and `insteadOf` (below)  |
| `file://...` or a local path | `/srv/git/skills.git`, `./skills`           | as written; a relative path is made absolute          | a repository on this machine; the manifest then works only here            |

- **Trailing `.git`** is optional in the short forms: `example-org/skills`
  and `example-org/skills.git` are the same repository. For a host of type
  `generic` the path is used as written, so write `.git` when the server
  needs it.
- **Paths** in the short forms are segments of letters, digits, `.`, `_`
  and `-`. GitHub, Codeberg and hosts of type `github` or `gitea` take
  exactly `owner/repo`; GitLab takes `group/repo` with any number of
  subgroups between them; `generic` takes any path.
- **Prefix or scp?** `name:path` is a prefix when `name` has no `.` or `@`
  and `path` does not start with `/`. `git@host:path` and
  `host.example:path` stay scp-like ssh addresses. An ssh host alias
  without a dot (`myserver:skills.git`) reads as a prefix: write
  `ssh://myserver/skills.git` or `git@myserver:skills.git` instead.
- **An unknown prefix is an error**, never a fallback to GitHub. The
  prefixes are case-sensitive: `GitLab:` is unknown.
- **Three or more segments without a prefix** (`a/b/c`) are a local path.
  For a GitLab subgroup write `gitlab:a/b/c`.

The manifest keeps what you wrote; skenv resolves it every time it runs.

## Declaring a self-hosted host

Declare the server once, under an alias of your choice, in the
`[environment]` section of the skenv file. After that, `work:group/repo`
works in `[[environment.own]]`, `[[environment.vendor]]` and
`skenv vendor add`.

```toml
[environment.hosts.work]
url  = "https://git.example.com"
type = "gitlab"
# ssh = "git@git.example.com"   # optional, this is the default

[[environment.own]]
repo = "work:platform/skills"
path = "~/src/skills"

[[environment.vendor]]
name = "deploy"
repo = "work:platform/team/tools"
path = "deploy"
rev  = "<full 40-character commit SHA>"
```

The same in YAML (`skenv.yaml`):

```yaml
environment:
  hosts:
    work:
      url: https://git.example.com
      type: gitlab
      # ssh: git@git.example.com
  own:
    - repo: work:platform/skills
      path: ~/src/skills
  vendor:
    - name: deploy
      repo: work:platform/team/tools
      path: deploy
      rev: "<full 40-character commit SHA>"
```

And in JSON (`skenv.json`):

```json
{
  "environment": {
    "hosts": {
      "work": { "url": "https://git.example.com", "type": "gitlab" }
    },
    "own": [
      { "repo": "work:platform/skills", "path": "~/src/skills" }
    ],
    "vendor": [
      {
        "name": "deploy",
        "repo": "work:platform/team/tools",
        "path": "deploy",
        "rev": "<full 40-character commit SHA>"
      }
    ]
  }
}
```

| Key    | Type   | Default                 | Meaning                                                                                                                                                                                    |
| ------ | ------ | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `url`  | string | required                | The https base of the server: `<alias>:group/repo` is cloned from `<url>/group/repo.git`. It may include a path (`https://git.example.com/scm`). It must not carry credentials.             |
| `type` | string | `"generic"`             | The software of the server; see the table below.                                                                                                                                             |
| `ssh`  | string | `git@<host of url>`     | The ssh address of the server, `user@host` or `ssh://user@host:port`, plus the path prefix of repositories over ssh when it differs from the one in `url` (`git@git.example.com:scm`). skenv clones over https; this address is recognised in remotes and named in the hint when a clone fails. |

The alias is lowercase letters, digits and `-`, starting with a letter.
`github`, `gitlab` and `codeberg` are built in and cannot be declared.
`hosts` (git servers) is unrelated to `[environment.host."<hostname>"]`
(per-machine overrides, see [the manifest](manifest.md#rules)).

| `type`    | Servers                  | Accepted paths                        | Clone URL                 |
| --------- | ------------------------ | ------------------------------------- | ------------------------- |
| `github`  | GitHub Enterprise Server | `owner/repo`                          | `<url>/owner/repo.git`    |
| `gitlab`  | GitLab                   | `group/repo`, subgroups of any depth  | `<url>/group/.../repo.git` |
| `gitea`   | Gitea, Forgejo           | `owner/repo`                          | `<url>/owner/repo.git`    |
| `generic` | anything else            | any path                              | `<url>/<path>` as written |

Besides the paths, skenv records the type for features that depend on the
server software: the default CI system of
[`skenv repo init`](harness.md#choosing-the-ci-system) (GitLab CI for
`gitlab`) and imports from other tools.

## Why aliases live in the manifest

The manifest is shared by all your machines, so everything it needs to
resolve a `repo` is in the manifest too. Aliases in the per-machine
[tool config](configuration.md) would make a manifest that resolves on one
machine and fails on the next. Whatever differs between machines, such as
ssh keys and credentials, stays in git's own configuration (see
[Authentication](#authentication)).

A second machine needs nothing but access to the server. The manifest
repository itself is given by URL, because its aliases are known only once
it is cloned:

```sh
cd ~/src
skenv clone https://git.example.com/platform/skills.git
skenv sync
```

`skenv clone` clones it and records its manifest; `skenv sync` reads
`[environment.hosts]` and syncs the `work:...` entries exactly as on the
first machine: the same clone URLs, the same `.skenv` markers, the same
links.

A project declares its own hosts under `[project.hosts.<alias>]`, with the
same keys, for the `repo` values of `[project]`; it never uses the hosts of
a manifest. See [Project skills](project-skills.md#the-project-section).

## Authentication

skenv clones every short form over https. It never stores or asks for
credentials: git does, with the settings you already use. Never put a token
in the manifest; skenv rejects a `url` with credentials in it.

- **https** works with a
  [git credential helper](https://git-scm.com/docs/gitcredentials), for
  example the macOS keychain or your forge's CLI.
- **ssh**: map the https base to ssh once per machine with git's
  [`url.<base>.insteadOf`](https://git-scm.com/docs/git-config#Documentation/git-config.txt-urlltbasegtinsteadOf).
  The manifest keeps the short form, and git fetches over ssh:

  ```sh
  git config --global url."git@git.example.com:".insteadOf "https://git.example.com/"
  ```

  The same for the built-in hosts:

  ```sh
  git config --global url."git@github.com:".insteadOf "https://github.com/"
  git config --global url."git@gitlab.com:".insteadOf "https://gitlab.com/"
  git config --global url."git@codeberg.org:".insteadOf "https://codeberg.org/"
  ```

  ssh on a non-default port:

  ```sh
  git config --global url."ssh://git@git.example.com:2222/".insteadOf "https://git.example.com/"
  ```

- **A private CA**: point git at the certificate of the server with
  [`http.sslCAInfo`](https://git-scm.com/docs/git-config#Documentation/git-config.txt-httpsslCAInfo),
  for that server only:

  ```sh
  git config --global http."https://git.example.com/".sslCAInfo ~/.config/certs/example-ca.pem
  ```

To see the URL git really fetches from after your `insteadOf` rules:

```sh
git ls-remote --get-url https://git.example.com/platform/skills.git
```

## The canonical URL

Every `repo` resolves to one clone URL, the canonical URL: the short form
expanded on its host, a full URL as written. skenv uses it wherever it
identifies a repository:

- **Vendor cache**: the directory in `~/.cache/skenv/repos` is derived from
  the canonical URL after your `insteadOf` rules (see
  [where the cache lives](manifest.md#where-the-cache-lives)).
- **`.skenv` marker**: a vendored copy in the store records the canonical
  URL as `repo`. `sync` compares it after normalisation (host in lower
  case, no scheme, credentials or `.git`), so rewriting `gitlab:g/tools` as
  `https://gitlab.com/g/tools.git` or `git@gitlab.com:g/tools.git` does not
  copy the skill again.
- **`doctor`** shows the canonical URL: `own repo work:platform/skills
  (https://git.example.com/platform/skills.git) is not cloned`, and the
  `wrong-rev` detail compares the URLs of the store and the manifest.

**Moving to a mirror.** When you point an entry at another server, by
changing `repo` or the `url` of its host, the canonical URL changes:

- vendored skills are copied again from the new server on the next `sync`,
  into a new cache directory. `rev` must exist there too, or `sync` stops
  with `commit ... not found`. The old cache directory can be deleted;
- an own working copy that already exists keeps its `origin`, and `sync`
  keeps pulling from it. Point it at the new server yourself:

  ```sh
  git -C ~/src/skills remote set-url origin https://git.example.com/platform/skills.git
  ```

## Errors and fixes

**Unknown prefix.**

```text
repo "acme:platform/tools": unknown host prefix "acme:" (known: gitlab:, codeberg:, work:); declare it under [environment.hosts.acme], or write owner/repo for github.com or a full git URL
```

The prefix is neither built in nor declared. Fix a typo, declare the host
under `[environment.hosts.acme]`, or write the full URL. An ssh host alias
without a dot needs `ssh://` (see [Forms of `repo`](#forms-of-repo)). A
manifest with this error is rejected as a whole, so `sync` and `doctor`
stop before changing anything.

**An alias passed to `skenv clone`.**

```text
repo "work:platform/skills": unknown host prefix "work:" (known: gitlab:, codeberg:); ...; a host declared in the manifest is not known before it is cloned, so pass the full URL
```

Pass the URL of the manifest repository: `skenv clone
https://git.example.com/platform/skills.git`.

**A path the host does not accept.**

```text
repo "gitlab:skills": a gitlab repository is group/repo or group/subgroup/.../repo, got "skills"
```

Check the path against the `type` of the host (see the table above).

**A host declaration that is not valid.**

```text
hosts.work.url: must not carry credentials; use a git credential helper
hosts.work.type: "bitbucket" must be one of github, gitlab, gitea, generic
hosts.gitlab: "gitlab" is built in and cannot be declared
```

**The server cannot be reached, or refuses you.**

```text
error: clone work:platform/skills: git clone --quiet https://git.example.com/platform/skills.git ...: exit status 128: fatal: ... (check access to the repository: ssh key or git credential helper; to clone over ssh: git config --global url."git@git.example.com:".insteadOf "https://git.example.com/")
```

Run `git ls-remote <canonical URL>` to see the same error without skenv.
`Could not resolve host` means the `url` of the host is wrong or the server
is not reachable from this machine (VPN?). An authentication error means
git has no credentials for it: set up a credential helper, or map the host
to ssh with the command in the message. A certificate error means the
server uses a private CA: set `http.sslCAInfo` (see
[Authentication](#authentication)).

## Starting a manifest with `skenv init`

`skenv init` starts a manifest in the current
repository and adds the repository as its first `[[environment.own]]`
entry, written in the short form of its `origin`:

| `origin`                                              | `repo` written                                   |
| ----------------------------------------------------- | ------------------------------------------------ |
| `https://github.com/example-org/skills.git`           | `example-org/skills`                             |
| `git@gitlab.com:example-org/team/skills.git`          | `gitlab:example-org/team/skills`                 |
| `https://codeberg.org/example-org/skills`             | `codeberg:example-org/skills`                    |
| `git@git.example.com:platform/skills.git`             | `git@git.example.com:platform/skills.git`        |
| `https://user:token@git.example.com/platform/skills.git` | `https://git.example.com/platform/skills.git` (credentials dropped) |
| a local path or `file://` URL                         | no own entry                                     |

A new manifest declares no hosts yet, so an `origin` on a self-hosted
server is written as its URL. Once you declare the host, you can shorten
the entry to `work:platform/skills`; the canonical URL, and so the
[identity](#the-canonical-url) of the repository, stays the same if the
host's `url` matches the origin.

A repository without an `origin` yet takes its future remote from
`--remote`, in any form above except a declared alias (the new manifest
declares none), and it is written the same way:

```sh
skenv init --remote gitlab:example-org/team/skills
skenv init --remote https://git.example.com/platform/skills.git
```

`--remote` with an `origin` is an error: the origin is the remote. It does
not add a git remote either; run `git remote add origin …` when the
repository exists on the server.

The host type also decides the CI system of
[`skenv repo init`](harness.md#choosing-the-ci-system): GitLab CI for an
`origin` on gitlab.com or on a declared host with `type = "gitlab"`,
GitHub Actions otherwise.

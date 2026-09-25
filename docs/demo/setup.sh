#!/usr/bin/env bash
# Builds the sandbox for docs/demo/demo.tape (`make demo`): a throwaway
# $HOME with Claude Code and pi "installed", skenv built from this checkout,
# and local bare repositories that stand in for GitHub, so the recording is
# offline, deterministic and free of anything from the real home directory.
#
# Usage: docs/demo/setup.sh [SANDBOX]   (default /tmp/skenv-demo)
# Then:  source SANDBOX/.demorc
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sandbox="${1:-/tmp/skenv-demo}"
remotes="$sandbox/.remotes"

case "$sandbox" in /tmp/* | /private/tmp/* | /var/folders/*) ;; *)
	echo "setup.sh: refusing to use $sandbox (not under /tmp)" >&2
	exit 2
	;;
esac
rm -rf "$sandbox"
mkdir -p "$sandbox/.local/bin" "$sandbox/.claude" "$sandbox/.pi/agent" "$sandbox/src" "$remotes"

(cd "$root" && go build -trimpath -o "$sandbox/.local/bin/skenv" ./cmd/skenv)

# Fixed identity and dates: the pinned SHAs are the same on every run.
export HOME="$sandbox" GIT_CONFIG_NOSYSTEM=1
unset GIT_CONFIG_GLOBAL XDG_CONFIG_HOME
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com
export GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com
export GIT_AUTHOR_DATE="2026-01-01T12:00:00Z" GIT_COMMITTER_DATE="2026-01-01T12:00:00Z"
git config --global init.defaultBranch main
git config --global advice.detachedHead false
# owner/repo resolves to https://github.com/owner/repo.git; serve it locally.
git config --global url."file://$remotes/".insteadOf https://github.com/

skill() { # skill DIR NAME DESCRIPTION
	mkdir -p "$1"
	printf -- '---\nname: %s\ndescription: %s\n---\n\n# %s\n' "$2" "$3" "$2" >"$1/SKILL.md"
}
publish() { # publish WORKDIR OWNER/REPO: commit WORKDIR and push it to the remote
	git -C "$1" init -q
	git -C "$1" add -A
	git -C "$1" commit -q -m "initial commit"
	git clone -q --bare "$1" "$remotes/$2.git"
	git -C "$1" rev-parse HEAD
}

work="$sandbox/.work"
skill "$work/pdf-tools/pdf-tools" pdf-tools "Extract text and tables from PDF files."
pdf_rev="$(publish "$work/pdf-tools" example-vendor/pdf-tools)"
skill "$work/commit-helper" commit-helper "Write Conventional Commit messages."
publish "$work/commit-helper" example-vendor/commit-helper >/dev/null

skill "$work/agent-skills/skills/release-notes" release-notes "Draft release notes from merged pull requests."
skill "$work/agent-skills/skills/sql-style" sql-style "Review SQL against the team style guide."
cat >"$work/agent-skills/env.toml" <<TOML
[[own]]                              # our skills, kept as a git working copy
repo = "example-org/agent-skills"
path = "~/src/agent-skills"

[[vendor]]                           # someone else's skill, pinned to a commit
name = "pdf-tools"
repo = "example-vendor/pdf-tools"
path = "pdf-tools"
rev  = "$pdf_rev"
TOML
publish "$work/agent-skills" example-org/agent-skills >/dev/null
rm -rf "$work"

cat >"$sandbox/.demorc" <<RC
export HOME="$sandbox" PATH="$sandbox/.local/bin:\$PATH" GIT_CONFIG_NOSYSTEM=1
unset CLAUDE_CONFIG_DIR SKENV_MANIFEST XDG_CONFIG_HOME GIT_CONFIG_GLOBAL PROMPT_COMMAND
export PS1='\$ ' LC_ALL=C
cd "$sandbox/src"
RC

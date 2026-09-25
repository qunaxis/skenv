#!/usr/bin/env bash
# Check every commit in a revision range with check-commit-msg.sh (PRD V2).
# Usage: check-commits.sh <range>   e.g. origin/main..HEAD, or a single ref
# for "all commits reachable from it".
set -euo pipefail

range=${1:?usage: check-commits.sh <range>}
dir=$(cd "$(dirname "$0")" && pwd)
out=$(mktemp)
trap 'rm -f "$out"' EXIT
# List the commits first: a failure inside `done < <(git rev-list ...)` is
# not caught by set -e, and an unknown or unreachable revision would pass as
# "0 commits checked".
if ! shas=$(git rev-list "$range" --); then
	printf 'check-commits: cannot list commits in range %s\n' "$range" >&2
	printf '  every revision in it must exist in this clone: fetch the missing history\n' >&2
	printf '  (e.g. git fetch --unshallow, or actions/checkout with fetch-depth: 0),\n' >&2
	printf '  and for a root commit pass a single ref (HEAD) instead of <sha>^..HEAD\n' >&2
	exit 2
fi
bad=0
count=0
for sha in $shas; do
	count=$((count + 1))
	if ! git log -1 --format=%B "$sha" | "$dir/check-commit-msg.sh" - 2>"$out"; then
		printf '%s: ' "$(git rev-parse --short "$sha")"
		cat "$out"
		bad=$((bad + 1))
	fi
done
if ((bad > 0)); then
	echo "$bad of $count commits are not Conventional Commits" >&2
	exit 1
fi
echo "$count commits checked, all Conventional Commits"

#!/usr/bin/env bash
# Check every commit in a revision range with check-commit-msg.sh (PRD V2).
# Usage: check-commits.sh <range>   e.g. origin/main..HEAD, or a single ref
# for "all commits reachable from it".
set -euo pipefail

range=${1:?usage: check-commits.sh <range>}
dir=$(cd "$(dirname "$0")" && pwd)
out=$(mktemp)
trap 'rm -f "$out"' EXIT
bad=0
count=0
while read -r sha; do
	count=$((count + 1))
	if ! git log -1 --format=%B "$sha" | "$dir/check-commit-msg.sh" - 2>"$out"; then
		printf '%s: ' "$(git rev-parse --short "$sha")"
		cat "$out"
		bad=$((bad + 1))
	fi
done < <(git rev-list "$range")
if ((bad > 0)); then
	echo "$bad of $count commits are not Conventional Commits" >&2
	exit 1
fi
echo "$count commits checked, all Conventional Commits"

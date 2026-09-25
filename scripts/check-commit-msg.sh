#!/usr/bin/env bash
# Validate a commit message against Conventional Commits 1.0.0 as used by
# skenv (PRD V1). Usage: check-commit-msg.sh <file>   (lefthook commit-msg)
#                        check-commit-msg.sh -        (message on stdin)
set -euo pipefail

types='feat|fix|perf|refactor|docs|test|build|ci|chore|revert'
header_re="^(${types})(\([a-z0-9][a-z0-9._/-]*\))?!?: [^ ].*$"

src=${1:?usage: check-commit-msg.sh <file|->}
if [[ $src == - ]]; then
	msg=$(cat)
else
	msg=$(cat -- "$src")
fi
# Drop comment lines that git adds to the editor template.
msg=$(printf '%s\n' "$msg" | grep -v '^#' || true)
header=$(printf '%s\n' "$msg" | sed -n '1p')
second=$(printf '%s\n' "$msg" | sed -n '2p')

fail() {
	printf 'commit message rejected: %s\n  header: %s\n' "$1" "$header" >&2
	printf '  expected: <type>(<scope>)?!?: <subject>, type one of %s\n' "${types//|/, }" >&2
	exit 1
}

[[ -n $header ]] || fail "empty message"
[[ $header =~ $header_re ]] || fail "header is not a Conventional Commit"
((${#header} <= 100)) || fail "header is longer than 100 characters"
[[ -z $second ]] || fail "the header must be followed by a blank line"

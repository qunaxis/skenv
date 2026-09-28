#!/usr/bin/env bash
# Verify that PRs changing user-facing code update documentation.
# Usage: BASE=<ref> HEAD=<ref> PR_BODY='...' PR_LABELS='...' scripts/check-docs-impact.sh
# Environment variables:
#   BASE: base commit (defaults to origin/main)
#   HEAD: head commit (defaults to HEAD)
#   PR_BODY: pull request body text
#   PR_LABELS: comma-separated label names
set -euo pipefail

base=${BASE:-origin/main}
head=${HEAD:-HEAD}
pr_body=${PR_BODY:-}
pr_labels=${PR_LABELS:-}

# User-facing paths: changes to these require docs updates
user_facing=(
	"internal/cli"
	"internal/model/skenvfile"
	"internal/model/config"
	"schemas"
	"internal/harness/templates"
)

# Docs paths: changes to these count as documentation updates
docs_paths=(
	"README.md"
	"docs/"
	"AGENTS.md"
	"skills/skenv/"
)

# Get all changed files between BASE and HEAD.
# Use three-dot syntax to compare against the merge base: files changed
# in the PR only, not commits that landed on base after the branch was cut.
if ! changed=$(git diff --name-only "$base...$head" 2>/dev/null); then
	printf 'check-docs-impact: cannot diff %s...%s\n' "$base" "$head" >&2
	exit 1
fi

# Filter user-facing changes (exclude test files)
user_facing_changed=()
while IFS= read -r file; do
	# Skip test files and testdata (including nested testdata/ directories)
	if [[ "$file" == *"_test.go" ]] || [[ "$file" == "testdata/"* ]] || [[ "$file" == */testdata/* ]]; then
		continue
	fi
	# Check if file matches any user-facing path
	for pattern in "${user_facing[@]}"; do
		if [[ "$file" == "$pattern"* ]]; then
			user_facing_changed+=("$file")
			break
		fi
	done
done <<< "$changed"

# If no user-facing changes, pass
if [ ${#user_facing_changed[@]} -eq 0 ]; then
	exit 0
fi

# Check if any docs paths changed
docs_changed=false
while IFS= read -r file; do
	for pattern in "${docs_paths[@]}"; do
		if [[ "$file" == "$pattern"* ]]; then
			docs_changed=true
			break 2
		fi
	done
done <<< "$changed"

if [ "$docs_changed" = true ]; then
	exit 0
fi

# Check for "Docs: none — <reason>" or "Docs: none - <reason>" with reason of 10+ chars
while IFS= read -r line; do
	line=${line%$'\r'}
	if [[ "$line" =~ ^Docs:[[:space:]]*none[[:space:]]*(—|-)[[:space:]]+(.{10,})$ ]]; then
		exit 0
	fi
done <<< "$pr_body"

# Check for docs:none label
if [[ ",$pr_labels," == *",docs:none,"* ]]; then
	exit 0
fi

# Fail: user-facing changes without docs update or exemption
printf 'PR changes user-facing code without docs update:\n' >&2
for file in "${user_facing_changed[@]}"; do
	printf '  %s\n' "$file" >&2
done
printf '\nAdd one of:\n' >&2
printf '  • Update docs (README.md, docs/*, AGENTS.md, skills/skenv/)\n' >&2
printf '  • Add label: docs:none\n' >&2
printf '  • Add to PR body: Docs: none — <reason> (reason ≥10 chars)\n' >&2
exit 1

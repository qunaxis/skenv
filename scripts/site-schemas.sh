#!/usr/bin/env bash
# Collect the JSON Schemas of every skenv release into the documentation
# site, which serves docs/public/ as is:
#   <out>/v<X.Y.Z>/<name>  the schemas of release vX.Y.Z, from its tag;
#   <out>/<name>           the schemas of the newest release (not of main),
#                          so the unversioned URL never shows unreleased keys.
# The "$id" of each copy is its versioned URL. Releases from before the
# schemas existed are skipped. The tags must be fetched (a shallow clone
# publishes nothing and says so).
#   scripts/site-schemas.sh [out]   (default docs/public/schemas)
set -euo pipefail

base=https://qunaxis.github.io/skenv/schemas
out=${1:-docs/public/schemas}
cd "$(git rev-parse --show-toplevel)"
mkdir -p "$out"
# Only what this script writes; README.md stays.
rm -rf "$out"/v[0-9]* "$out"/*.schema.json

releases=0 latest=""
while read -r tag; do
	[[ -n $tag ]] || continue
	files=$(git ls-tree --name-only "$tag" schemas/ | grep '\.schema\.json$' || true)
	[[ -n $files ]] || continue
	mkdir -p "$out/$tag"
	for path in $files; do
		name=${path#schemas/}
		git show "$tag:$path" | sed "s|\"\\\$id\": \"$base/$name\"|\"\\\$id\": \"$base/$tag/$name\"|" >"$out/$tag/$name"
	done
	releases=$((releases + 1))
	if [[ -z $latest && $tag != *-* ]]; then
		latest=$tag
		cp "$out/$tag"/*.schema.json "$out/"
	fi
done < <(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname)

if [[ -z $latest ]]; then
	echo "site-schemas: no release with schemas/ among the tags (none yet, or a shallow clone); publishing none" >&2
else
	echo "site-schemas: schemas of $releases releases, latest $latest"
fi

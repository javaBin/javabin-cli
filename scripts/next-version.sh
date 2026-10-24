#!/bin/sh
# Print the next release version from Conventional Commits since the last
# v* tag, or nothing if no commit warrants a release.
#
#   feat           -> minor
#   fix, perf      -> patch
#   type!: / BREAKING CHANGE -> major (minor while < 1.0.0)
#   anything else (docs, ci, chore, test, ...) -> no release
#
# Usage: next-version.sh            (in a full clone: fetch-depth 0 + tags)

set -eu

LAST=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || echo "v0.0.0")
RANGE="${LAST}..HEAD"
[ "$LAST" = "v0.0.0" ] && RANGE="HEAD"

SUBJECTS=$(git log --format=%s "$RANGE")
BODIES=$(git log --format=%b "$RANGE")

BUMP=""
if echo "$SUBJECTS" | grep -qE '^[a-z]+(\([^)]*\))?!:' || echo "$BODIES" | grep -q '^BREAKING CHANGE'; then
  BUMP="major"
elif echo "$SUBJECTS" | grep -qE '^feat(\([^)]*\))?:'; then
  BUMP="minor"
elif echo "$SUBJECTS" | grep -qE '^(fix|perf)(\([^)]*\))?:'; then
  BUMP="patch"
fi
[ -z "$BUMP" ] && exit 0

V=${LAST#v}
MAJOR=${V%%.*}; REST=${V#*.}; MINOR=${REST%%.*}; PATCH=${REST#*.}
case "$BUMP" in
  major) if [ "$MAJOR" -eq 0 ]; then MINOR=$((MINOR + 1)); PATCH=0; else MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0; fi ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  patch) PATCH=$((PATCH + 1)) ;;
esac
echo "v${MAJOR}.${MINOR}.${PATCH}"

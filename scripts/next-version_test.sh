#!/bin/sh
# Tests for next-version.sh in throwaway repos. Run: sh scripts/next-version_test.sh
set -eu
SCRIPT="$(cd "$(dirname "$0")" && pwd)/next-version.sh"
FAIL=0

check() { # <tag or -> <expected or -> <subject> [body]
  D=$(mktemp -d); (
    cd "$D" && git init -q && git -c user.email=t@t -c user.name=t commit -q --allow-empty -m "chore: init"
    [ "$1" != "-" ] && git tag "$1"
    git -c user.email=t@t -c user.name=t commit -q --allow-empty -m "$3" -m "${4:-}"
    GOT=$(sh "$SCRIPT"); WANT=$2; [ "$WANT" = "-" ] && WANT=""
    if [ "$GOT" != "$WANT" ]; then echo "FAIL: tag=$1 '$3' -> '$GOT', want '$WANT'"; exit 1; fi
  ) || FAIL=1
  rm -rf "$D"
}

check v0.2.0 v0.3.0 "feat(secret): add command"
check v0.2.0 v0.2.1 "fix: handle empty value"
check v0.2.0 v0.2.1 "perf(status): cache"
check v0.2.0 -      "docs: readme"
check v0.2.0 -      "ci: workflow"
check v0.2.0 v0.3.0 "feat!: drop flag"
check v0.2.0 v0.3.0 "fix: x" "BREAKING CHANGE: flag removed"
check v1.4.2 v2.0.0 "refactor(api)!: new shape"
check v1.4.2 v1.5.0 "feat: y"
check -      v0.1.0 "feat: first"
[ "$FAIL" = 0 ] && echo "next-version: all cases pass"
exit "$FAIL"

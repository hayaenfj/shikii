#!/usr/bin/env bash
# Duplicate-code check for the Go sources (tests, generated code, testdata and web/ are left out).
#   jscpd: share of duplicated lines in the whole tree; over "threshold" in .jscpd.json fails.
#   dupl : clones of DUPL_TOKENS tokens or more; the number of clone groups must equal DUPL_GROUPS.
#          More fails (new duplication); fewer fails until DUPL_GROUPS is lowered (freed room cannot be refilled).
# Both tools are required. A missing tool fails, because a skipped check would read as a pass:
#   npm i -g jscpd
#   go install github.com/mibk/dupl@latest
set -euo pipefail
cd "$(dirname "$0")/.."

DUPL_TOKENS=100
# Clone groups that exist today
DUPL_GROUPS=0

for tool in jscpd dupl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "check-dup: $tool is not installed (see the top of scripts/check-dup.sh)" >&2
    exit 2
  fi
done

status=0

echo "== jscpd"
# Exits 1 when the duplicated-line share is over the threshold in .jscpd.json
jscpd . --config .jscpd.json --silent || status=1

echo "== dupl (-t $DUPL_TOKENS)"
# Keep these exclusions in step with "ignore" in .jscpd.json. dupl exits 0 even when it finds clones, so read its count.
out=$(git ls-files -co --exclude-standard '*.go' \
  | grep -Ev '(_test|\.pb|\.gen)\.go$|(^|/)(vendor|testdata|web|\.claude)/' \
  | dupl -t "$DUPL_TOKENS" -files) || { printf '%s\n' "$out"; echo "check-dup: dupl failed" >&2; exit 2; }
groups=$(printf '%s\n' "$out" | sed -n 's/^Found total \([0-9][0-9]*\) clone groups\.$/\1/p')
if [ -z "$groups" ]; then
  printf '%s\n' "$out"
  echo "check-dup: could not read the clone-group count from dupl" >&2
  exit 2
fi
if [ "$groups" -gt "$DUPL_GROUPS" ]; then
  printf '%s\n' "$out"
  echo "check-dup: $groups clone groups, more than the allowed $DUPL_GROUPS" >&2
  status=1
elif [ "$groups" -lt "$DUPL_GROUPS" ]; then
  echo "check-dup: $groups clone groups, fewer than the allowed $DUPL_GROUPS; lower DUPL_GROUPS to $groups in scripts/check-dup.sh" >&2
  status=1
else
  echo "$groups clone groups (allowed $DUPL_GROUPS)"
fi

exit "$status"

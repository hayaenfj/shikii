#!/usr/bin/env bash
# Duplicate-code check for the Go sources (tests, generated code, testdata and web/ are left out).
#   jscpd: share of duplicated lines in the whole tree; over "threshold" in .jscpd.json fails. The number of clones and the
#          number of duplicated lines must also equal JSCPD_CLONES and JSCPD_LINES.
#   dupl : clones of DUPL_TOKENS tokens or more; the number of clone groups must equal DUPL_GROUPS.
#   For each count, more fails (new duplication, or an existing one that grew) and fewer fails until the constant is lowered
#   (freed room cannot be refilled). A count that is not a number fails too. A change that shrinks one clone and grows
#   another by the same amount is not seen; that is the room left by counting totals, not individual clones.
# Both tools are required. A missing tool fails, because a skipped check would read as a pass:
#   npm i -g jscpd
#   go install github.com/mibk/dupl@latest
# JSCPD_CLONES and JSCPD_LINES were taken with jscpd 5.4.3; another version may count differently, and then the numbers
# are taken again with it (lower or raise them to what this script prints).
set -euo pipefail
cd "$(dirname "$0")/.."

DUPL_TOKENS=100
# Clones, duplicated lines and clone groups that exist today
JSCPD_CLONES=49
JSCPD_LINES=438
DUPL_GROUPS=0

for tool in jscpd dupl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "check-dup: $tool is not installed (see the top of scripts/check-dup.sh)" >&2
    exit 2
  fi
done

status=0

# compare_count <what> <found> <allowed> <constant>: the found count must equal the allowed one
compare_count() {
  case "$2" in
    '' | *[!0-9]* | ?????????????*) # not digits only, or over 12 digits (the shell compares only what fits in 64 bits)
      echo "check-dup: the $1 count is not a number I can compare: '$2'" >&2
      exit 2
      ;;
  esac
  if [ "$2" -gt "$3" ]; then
    echo "check-dup: $2 $1, more than the allowed $3" >&2
    status=1
  elif [ "$2" -lt "$3" ]; then
    echo "check-dup: $2 $1, fewer than the allowed $3; lower $4 to $2 in scripts/check-dup.sh" >&2
    status=1
  else
    echo "$2 $1 (allowed $3)"
  fi
}

echo "== jscpd"
# A report left by an earlier run must not stand in for this one
rm -f .jscpd-report/jscpd-report.json
# Exits 1 when the duplicated-line share is over the threshold in .jscpd.json
jscpd . --config .jscpd.json --silent || status=1
jscpd_total() {
  python3 -c 'import json, sys; print(json.load(open(".jscpd-report/jscpd-report.json"))["statistics"]["total"][sys.argv[1]])' "$1" || {
    echo "check-dup: could not read $1 from .jscpd-report/jscpd-report.json" >&2
    exit 2
  }
}
compare_count "clones" "$(jscpd_total clones)" "$JSCPD_CLONES" JSCPD_CLONES
compare_count "duplicated lines" "$(jscpd_total duplicatedLines)" "$JSCPD_LINES" JSCPD_LINES

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
fi
compare_count "clone groups" "$groups" "$DUPL_GROUPS" DUPL_GROUPS

exit "$status"

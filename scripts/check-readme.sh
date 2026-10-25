#!/usr/bin/env bash
# Checks that the READMEs are consistent (also run in CI). Exits with 1 on any violation.
#  1. Every README_<lang>.md in the language switcher on the first line exists, and every version in docs/readme/ is in the switcher
#  2. The first line of each language version has the same switcher (its own language in bold, the others as links)
#  3. Relative links (files) in README.md, each language version, docs/ docs/examples/ integrations/ exist
#  4. The tests badge count equals the measured count  5. The Go version does not contradict go.mod  6. The receipt example has the real format  7. The figures equal the output of the generator scripts
set -eu
cd "$(dirname "$0")/.."
fail=0
err() { echo "check-readme: $*" >&2; fail=1; }

first=$(head -1 README.md)
codes=$(grep -o 'docs/readme/README_[A-Za-z-]*\.md' <<<"$first" | sed 's#docs/readme/README_##; s#\.md##' | sort -u)
[ -n "$codes" ] || err "the first line of README.md has no language switcher"

# 1. switcher ↔ files
for c in $codes; do
  [ -f "docs/readme/README_$c.md" ] || err "in the switcher but missing: docs/readme/README_$c.md"
done
for f in docs/readme/README_*.md; do
  c=$(basename "$f" .md); c=${c#README_}
  grep -qx "$c" <<<"$codes" || err "the file exists but is not in the README.md switcher: $f"
done

# 2. first line of each language version
for f in docs/readme/README_*.md; do
  line=$(head -1 "$f")
  grep -q '\*\*' <<<"$line" || err "$f: the first line has no current language in bold"
  grep -q '](../../README.md)' <<<"$line" || err "$f: the first line has no link back to English (../../README.md)"
  for c in $codes; do
    own=$(basename "$f" .md); own=${own#README_}
    [ "$c" = "$own" ] && continue
    grep -q "README_$c.md)" <<<"$line" || err "$f: the first line has no link to README_$c.md"
  done
done

# 3. relative links exist (http, #anchors and mailto are not checked)
check_links() {
  local f=$1 dir; dir=$(dirname "$f")
  grep -o '\]([^)#][^)]*)' "$f" | sed 's/^](//; s/)$//' | while read -r target; do
    case "$target" in http://*|https://*|mailto:*) continue;; esac
    target=${target%%#*}
    [ -e "$dir/$target" ] || echo "$f: link target missing: $target"
  done
}
for f in README.md docs/readme/README_*.md docs/*.md docs/examples/*.md integrations/*.md; do
  [ -e "$f" ] || continue
  out=$(check_links "$f")
  [ -z "$out" ] || { echo "$out" >&2; fail=1; }
done

# 4. badge test count = measured count (counted with go test -list; the same number in every language's README)
if command -v go >/dev/null; then
  actual=$(for p in $(go list ./... 2>/dev/null); do go test -list '^Test' "$p" 2>/dev/null | grep -c '^Test' || true; done | awk '{s+=$1} END {print s}')
else
  actual=""; err "go is missing, so the tests cannot be counted"
fi
for f in README.md docs/readme/README_*.md; do
  n=$(grep -oE 'tests-[0-9]+%20go' "$f" | head -1 | grep -oE '[0-9]+' | head -1 || true)
  [ -n "${n}" ] || { err "$f: no tests badge"; continue; }
  [ "${n}" = "${actual}" ] || err "$f: the tests badge says ${n}, measured ${actual}"
done
# The "There are N Go tests" in the body is the same number too (prevents fixing only the badge and leaving the body stale; README and the long English guide)
for f in README.md docs/guide.md; do
  while IFS= read -r n; do
    [ "${n}" = "${actual}" ] || err "$f: \"${n} Go tests\" in the body differs from the measured ${actual}"
  done < <(grep -oE 'are [0-9]+ Go tests' "$f" | grep -oE '[0-9]+' || true)
done

# 5. Go version: go.mod's version matches "Go 1.xx" in the README body (prevents contradictions such as badge 1.27 but body 1.24)
gover=$(grep -m1 '^go ' go.mod | awk '{print $2}' | cut -d. -f1,2)
for f in README.md docs/readme/README_*.md; do
  if grep -oE 'Go[ -]1\.[0-9]+' "$f" | grep -v "1\.${gover#1.}\b" | grep -q .; then
    err "$f: go.mod says Go $gover but another version is written: $(grep -oE 'Go[ -]1\.[0-9]+' "$f" | grep -v "1\.${gover#1.}\b" | sort -u | tr '\n' ' ')"
  fi
done

# 6. The receipt lines in the README have the same form as the real output of shikii decide (the Fprintf in main.go)
for f in README.md docs/readme/README_ja.md; do
  while read -r line; do
    [ -z "${line}" ] && continue
    grep -qE '^receipt: line [a-z]+ fitted [0-9-]+ on [0-9]+ truths \([0-9]+ human\); human-checked act decisions [0-9]+, precision [0-9.]+' <<<"${line}" \
      || err "$f: the receipt line is not in the form of shikii decide's output: ${line}"
  done < <(grep -o 'receipt: .*' "$f" || true)
done

# 7. The four README figures can be regenerated from the same script. Compare without rewriting the SVGs in the working tree.
if command -v python3 >/dev/null; then
  assets=(shikii-conversation.svg shikii-conversation-ja.svg shikii-architecture.svg shikii-architecture-en.svg)
  have_assets=1
  for asset in "${assets[@]}"; do
    [ -f "docs/assets/$asset" ] || { err "generated figure missing: docs/assets/$asset"; have_assets=0; }
  done
  if [ "$have_assets" -eq 1 ]; then
    tmp=$(mktemp -d)
    if ! python3 - "$tmp" <<'PYGEN'
import runpy, sys
from pathlib import Path
runpy.run_path("scripts/gen-architecture-svg.py")["generate"](Path(sys.argv[1]))
PYGEN
    then err "cannot regenerate the README figures"; fi
    for asset in "${assets[@]}"; do
      cmp -s "$tmp/$asset" "docs/assets/$asset" || err "docs/assets/$asset differs from the generator script's output"
    done
    # the top image and the "what grows" figure (scripts/gen-hero-svg.py)
    if ! python3 - "$tmp" <<'PYGEN'
import runpy, sys
from pathlib import Path
runpy.run_path("scripts/gen-hero-svg.py")["generate"](Path(sys.argv[1]))
PYGEN
    then err "cannot regenerate the top image"; fi
    # For the top image and the "what grows" figure, the Japanese README points only to the 2 Japanese versions, and English (and other languages) only to the 2 English versions (allowlist; one extra image fails too)
    for f in README.md docs/readme/README_*.md; do
      if [ "$f" = docs/readme/README_ja.md ]; then want="shikii-grows-ja.svg shikii-hero-ja.svg"; else want="shikii-grows.svg shikii-hero.svg"; fi
      got=$(grep -oE 'assets/shikii-(hero|grows)(-ja)?\.svg' "$f" | sed 's#assets/##' | sort -u | tr '\n' ' ' | sed 's/ $//')
      [ "$got" = "$want" ] || err "$f: the top image and the what-grows figure must point only to \"$want\" (now \"$got\")"
    done
    for asset in shikii-hero.svg shikii-hero-ja.svg shikii-grows.svg shikii-grows-ja.svg; do
      cmp -s "$tmp/$asset" "docs/assets/$asset" || err "docs/assets/$asset differs from the generator script's output"
    done
    rm -rf "$tmp"
  fi
else
  err "python3 is missing, so the figure check (7) cannot run"
fi

# 8. curl examples that send a body to /v1 set Content-Type (curl's default is form, so without it serve rejects with 415; a first-time user typed the README example as is and got stuck)
for f in README.md docs/*.md docs/readme/README_*.md integrations/*.md skills/shikii/SKILL.md skills/shikii/references/*.md; do
  [ -e "$f" ] || continue
  while IFS= read -r line; do
    case "$line" in *"Content-Type: application/json"*) ;; *) err "$f: a curl example sending to /v1 lacks -H 'Content-Type: application/json': ${line}";; esac
  done < <(grep -E 'curl .*/v1/.* (-d|--data)' "$f" || true)
done

if [ "$fail" -ne 0 ]; then exit 1; fi
echo "check-readme: ok ($(( $(wc -w <<<"$codes") + 1 )) languages: English and $(tr '\n' ' ' <<<"$codes"| sed 's/ $//'))"

#!/usr/bin/env bash
# README の整合性を検査する(CI でも回す)。違反があれば 1 で終わる。
#  1. 先頭の言語切替行に並ぶ README_<lang>.md が全部あり、逆に docs/readme/ にある版は全部切替行に載っている
#  2. 各言語版の先頭行にも同じ切替行がある(自分の言語は太字、他はリンク)
#  3. README.md と各言語版の相対リンク(ファイル)が実在する
set -eu
cd "$(dirname "$0")/.."
fail=0
err() { echo "check-readme: $*" >&2; fail=1; }

first=$(head -1 README.md)
codes=$(grep -o 'docs/readme/README_[A-Za-z-]*\.md' <<<"$first" | sed 's#docs/readme/README_##; s#\.md##' | sort -u)
[ -n "$codes" ] || err "README.md の先頭行に言語切替が無い"

# 1. 切替行 ↔ ファイルの対応
for c in $codes; do
  [ -f "docs/readme/README_$c.md" ] || err "切替行にあるが無い: docs/readme/README_$c.md"
done
for f in docs/readme/README_*.md; do
  c=$(basename "$f" .md); c=${c#README_}
  grep -qx "$c" <<<"$codes" || err "ファイルはあるが README.md の切替行に無い: $f"
done

# 2. 各言語版の先頭行
for f in docs/readme/README_*.md; do
  line=$(head -1 "$f")
  grep -q '\*\*' <<<"$line" || err "$f: 先頭行に太字の現在言語が無い"
  grep -q '](../../README.md)' <<<"$line" || err "$f: 先頭行から英語(../../README.md)へ戻るリンクが無い"
  for c in $codes; do
    own=$(basename "$f" .md); own=${own#README_}
    [ "$c" = "$own" ] && continue
    grep -q "README_$c.md)" <<<"$line" || err "$f: 先頭行に README_$c.md へのリンクが無い"
  done
done

# 3. 相対リンクの実在(http と #アンカー と mailto は見ない)
check_links() {
  local f=$1 dir; dir=$(dirname "$f")
  grep -o '\]([^)#][^)]*)' "$f" | sed 's/^](//; s/)$//' | while read -r target; do
    case "$target" in http://*|https://*|mailto:*) continue;; esac
    target=${target%%#*}
    [ -e "$dir/$target" ] || echo "$f: リンク先が無い: $target"
  done
}
for f in README.md docs/readme/README_*.md; do
  out=$(check_links "$f")
  [ -z "$out" ] || { echo "$out" >&2; fail=1; }
done

if [ "$fail" -ne 0 ]; then exit 1; fi
echo "check-readme: ok ($(wc -w <<<"$codes" | tr -d ' ') languages)"

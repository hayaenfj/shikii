#!/usr/bin/env bash
# README の整合性を検査する(CI でも回す)。違反があれば 1 で終わる。
#  1. 先頭の言語切替行に並ぶ README_<lang>.md が全部あり、逆に docs/readme/ にある版は全部切替行に載っている
#  2. 各言語版の先頭行にも同じ切替行がある(自分の言語は太字、他はリンク)
#  3. README.md と各言語版、docs/ docs/examples/ integrations/ の相対リンク(ファイル)が実在する
#  4. tests のバッジの数が実測と同じ  5. Go の版が go.mod と矛盾しない  6. receipt の例が実物の書式  7. 図が生成スクリプトの出力と同じ
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
for f in README.md docs/readme/README_*.md docs/*.md docs/examples/*.md integrations/*.md; do
  [ -e "$f" ] || continue
  out=$(check_links "$f")
  [ -z "$out" ] || { echo "$out" >&2; fail=1; }
done

# 4. バッジのテスト数 = 実測(go test -list で数える。全言語の README が同じ数)
if command -v go >/dev/null; then
  actual=$(for p in $(go list ./... 2>/dev/null); do go test -list '^Test' "$p" 2>/dev/null | grep -c '^Test' || true; done | awk '{s+=$1} END {print s}')
else
  actual=""; err "go が無いのでテスト数を数えられない"
fi
for f in README.md docs/readme/README_*.md; do
  n=$(grep -oE 'tests-[0-9]+%20go' "$f" | head -1 | grep -oE '[0-9]+' | head -1 || true)
  [ -n "${n}" ] || { err "$f: tests のバッジが無い"; continue; }
  [ "${n}" = "${actual}" ] || err "$f: tests のバッジは ${n}、実測は ${actual}"
done
# 本文の「There are N Go tests」も同じ数(バッジだけ直して本文が古いまま、を防ぐ。README と英語の長い案内)
for f in README.md docs/guide.md; do
  while IFS= read -r n; do
    [ "${n}" = "${actual}" ] || err "$f: 本文の「${n} Go tests」は実測 ${actual} と違う"
  done < <(grep -oE 'are [0-9]+ Go tests' "$f" | grep -oE '[0-9]+' || true)
done

# 5. Go の版: go.mod の版と、README の本文の「Go 1.xx」が一致(バッジは 1.27 なのに本文が 1.24、のような矛盾を防ぐ)
gover=$(grep -m1 '^go ' go.mod | awk '{print $2}' | cut -d. -f1,2)
for f in README.md docs/readme/README_*.md; do
  if grep -oE 'Go[ -]1\.[0-9]+' "$f" | grep -v "1\.${gover#1.}\b" | grep -q .; then
    err "$f: go.mod は Go $gover だが別の版が書かれている: $(grep -oE 'Go[ -]1\.[0-9]+' "$f" | grep -v "1\.${gover#1.}\b" | sort -u | tr '\n' ' ')"
  fi
done

# 6. README の receipt の行は shikii decide の実物の書式(main.go の Fprintf)と同じ形
for f in README.md docs/readme/README_ja.md; do
  while read -r line; do
    [ -z "${line}" ] && continue
    grep -qE '^receipt: line [a-z]+ fitted [0-9-]+ on [0-9]+ truths \([0-9]+ human\); human-checked act decisions [0-9]+, precision [0-9.]+' <<<"${line}" \
      || err "$f: receipt の行が shikii decide の出力の形ではない: ${line}"
  done < <(grep -o 'receipt: .*' "$f" || true)
done

# 7. READMEの図4枚は同じスクリプトから再生成できる。作業中のSVGを書き換えずに比較する。
if command -v python3 >/dev/null; then
  assets=(shikii-conversation.svg shikii-conversation-ja.svg shikii-architecture.svg shikii-architecture-en.svg)
  have_assets=1
  for asset in "${assets[@]}"; do
    [ -f "docs/assets/$asset" ] || { err "図の生成物が無い: docs/assets/$asset"; have_assets=0; }
  done
  if [ "$have_assets" -eq 1 ]; then
    tmp=$(mktemp -d)
    if ! python3 - "$tmp" <<'PYGEN'
import runpy, sys
from pathlib import Path
runpy.run_path("scripts/gen-architecture-svg.py")["generate"](Path(sys.argv[1]))
PYGEN
    then err "READMEの図を再生成できない"; fi
    for asset in "${assets[@]}"; do
      cmp -s "$tmp/$asset" "docs/assets/$asset" || err "docs/assets/$asset は生成スクリプトの出力と違う"
    done
    # 先頭の画像と「何が育つか」の図(scripts/gen-hero-svg.py)
    if ! python3 - "$tmp" <<'PYGEN'
import runpy, sys
from pathlib import Path
runpy.run_path("scripts/gen-hero-svg.py")["generate"](Path(sys.argv[1]))
PYGEN
    then err "先頭の画像を再生成できない"; fi
    for asset in shikii-hero.svg shikii-hero-ja.svg shikii-grows.svg shikii-grows-ja.svg; do
      cmp -s "$tmp/$asset" "docs/assets/$asset" || err "docs/assets/$asset は生成スクリプトの出力と違う"
    done
    rm -rf "$tmp"
  fi
else
  err "python3 が無いので図の検査(7)ができない"
fi

# 8. /v1 に本文を送る curl の例は Content-Type を付けている(curl の既定は form なので、無いと serve が 415 で蹴る。初めての利用者が README の例をそのまま打って止まった)
for f in README.md docs/*.md docs/readme/README_*.md integrations/*.md skills/shikii/SKILL.md skills/shikii/references/*.md; do
  [ -e "$f" ] || continue
  while IFS= read -r line; do
    case "$line" in *"Content-Type: application/json"*) ;; *) err "$f: /v1 に送る curl の例に -H 'Content-Type: application/json' が無い: ${line}";; esac
  done < <(grep -E 'curl .*/v1/.* (-d|--data)' "$f" || true)
done

if [ "$fail" -ne 0 ]; then exit 1; fi
echo "check-readme: ok ($(wc -w <<<"$codes" | tr -d ' ') languages)"

#!/bin/sh
# 鍵も GPU も無しで、decide → label → calibrate → check → drift → status → gc の輪を一周する。
# 偽のモデル(cmd/fake-systemone)を使うので、数字は本物ではない。手順と出力の形を見るためのもの。
#   scripts/demo.sh            # 普通の偽モデル
#   scripts/demo.sh --overconfident   # 確率を端に寄せた偽モデル(生と較正後の混同表を見比べる)
set -eu
cd "$(dirname "$0")/.."
go build -o bin/shikii ./cmd/shikii
go build -o bin/fake-systemone ./cmd/fake-systemone

PORT=8099
WORK="$(mktemp -d)"
SERVER=""
cleanup() {
  if [ -n "$SERVER" ]; then kill "$SERVER" 2>/dev/null || true; wait "$SERVER" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT
bin/fake-systemone --port $PORT "$@" >"$WORK/server.log" 2>&1 &
SERVER=$!
until curl -sf "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; do sleep 0.2; done

SPEC="$WORK/intent.yaml"
cp examples/prompt-intent.yaml "$SPEC"   # 較正無しの例から始める(書き戻しはこのコピーに入る)
STORE="$WORK/store"
D="bin/shikii"
L="--local http://127.0.0.1:$PORT --store $STORE"

step() { printf '\n\033[1m== %s\033[0m\n' "$*"; }

step "1. 判定(較正が無いので act は出ない)"
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md'
$D decide "$SPEC" $L -i 'message=この関数は何をしているのか説明して'

step "2. 正解を付ける(直近の記録に。本番では利用者の操作や結果から拾う)"
$D label --store "$STORE" --spec-id prompt-intent --last --source ask question

step "3. ラベル付きファイル(62 件)で較正。identity / vector を 5-fold CV で比べて spec に書き戻す"
$D calibrate "$SPEC" --labeled testdata/prompt-intent-labeled.json $L --model fake-systemone/1 --write | grep -E '^(spec=|raw:|at act|calibrated:|vector 5-fold|choose|wrote|WARNING)'

step "4. 較正後の判定(act が出るようになる)"
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md' | head -2

step "5. 契約の検査(CI 向け)"
$D check --store "$STORE" "$SPEC"

step "6. 文言を 1 語変えると較正は失効し、check が落ちる"
sed -i.bak 's/What is the user asking the assistant to do?/What is the user asking the assistant to do? Be strict./' "$SPEC" && rm -f "$SPEC.bak"
$D check --store "$STORE" "$SPEC" || true
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md' | head -2

step "6b. 人が付けた正解は消えていない。ファイル無しで calibrate を回すと、残っている本文を新しい文言で聞き直して線が戻る"
$D calibrate "$SPEC" --labeled none $L --model fake-systemone/1 --write | grep -E '^(replayed|spec=|choose|wrote|WARNING|note:)'
$D check --store "$STORE" "$SPEC"
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md' | head -2

step "7. 棚卸しと劣化の監視(6b で引き直した線に対して、直近の正解で劣化していないか)"
$D status --store "$STORE" "$SPEC"
$D drift "$SPEC" --store "$STORE" --model fake-systemone/1

step "8. 掃除(表示だけ。ラベル付きの記録は残る)"
$D gc --store "$STORE" --older-than 0 "$SPEC"

printf '\n完了。偽モデルなので数字に意味はない。本物に向けるには --local に llama.cpp(/v1/systemone)か、鍵を置いて --hosted を使う。\n'

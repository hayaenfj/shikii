#!/bin/sh
# 比較ベンチ: 同じ課題(prompt-intent、62 件の正解)・同じモデルで、聞き方を変えた腕を並べる(報告だけ。spec には書かない)。
#   scripts/bench/run.sh <base-url> <short-name>
# 腕 A: 測った聞き方(v2)+ 較正(shikii がいつもやること)
# 腕 B: RAG 風。同じ質問に、似た例 5 件(正解付き)を本文に足す(scripts/bench/rag-rows.py が作る)
# 腕 C: LoRA は別手順(docs/bench.md)。この script では回さない。
set -eu
URL="$1"; NAME="$2"
BIN="${BIN:-bin/shikii}"
pick() { grep -E "^(spec=|raw:|calibrated:|at act|vector 5-fold|choose|WARNING)" || true; }

python3 scripts/bench/rag-rows.py 5
echo "=== A: measured wording + calibration (decisions/prompt-intent-v2.yaml)"
"$BIN" calibrate decisions/prompt-intent-v2.yaml --labeled testdata/prompt-intent-labeled.json \
  --local "$URL" --tainted --store ".shikii-eval/bench-$NAME/a" 2>&1 | pick
echo "=== B: same question + 5 retrieved examples (scripts/bench/prompt-intent-rag.yaml)"
"$BIN" calibrate scripts/bench/prompt-intent-rag.yaml --labeled testdata/prompt-intent-rag.json \
  --local "$URL" --tainted --store ".shikii-eval/bench-$NAME/b" 2>&1 | pick

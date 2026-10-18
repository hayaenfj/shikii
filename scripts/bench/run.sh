#!/bin/sh
# 比較ベンチ: 同じ課題・同じモデルで、聞き方を変えた腕を並べる(報告だけ。spec には書かない)。
#   scripts/bench/run.sh <base-url> <short-name> [base-spec] [rag-spec] [labeled.json]
#   既定: decisions/prompt-intent-v2.yaml  scripts/bench/prompt-intent-rag.yaml  testdata/prompt-intent-labeled.json
#   例(はい/いいえ): scripts/bench/run.sh http://127.0.0.1:8092 qwen3-4b decisions/same-issue.yaml scripts/bench/same-issue-rag.yaml testdata/same-issue-labeled.json
# 腕 A: 測った聞き方 + 較正。spec に variants(例: layout: plain)があれば tune が全部の候補を測る(勝者の数字が A)
# 腕 B: RAG 風。同じ質問に、似た例 5 件(正解付き)を本文に足す(scripts/bench/rag-rows.py が作る。例はその行の分割の外から)。rag spec の variants も測る
# 腕 C: LoRA は scripts/bench/lora/run.sh(docs/bench.md)。この script では回さない。
set -eu
URL="$1"; NAME="$2"
BASE="${3:-decisions/prompt-intent-v2.yaml}"
RAG="${4:-scripts/bench/prompt-intent-rag.yaml}"
LABELED="${5:-testdata/prompt-intent-labeled.json}"
BIN="${BIN:-bin/shikii}"
RAGROWS="${LABELED%.json}"; RAGROWS="testdata/$(basename "${RAGROWS%-labeled}")-rag.json"
pick() { grep -E "^(spec=|raw:|calibrated:|at act|vector 5-fold|platt 5-fold|choose|WARNING|tune|holdout|winner)|top1=" || true; }

python3 scripts/bench/rag-rows.py "$LABELED" "$RAGROWS" 5 5
echo "=== A: measured wording + calibration ($BASE; tune scores every candidate, calibrate reports the current one)"
"$BIN" calibrate "$BASE" --labeled "$LABELED" --local "$URL" --tainted --store ".shikii-eval/bench-$NAME/a" 2>&1 | pick
"$BIN" tune "$BASE" --labeled "$LABELED" --local "$URL" --tainted --store ".shikii-eval/bench-$NAME/a-tune" 2>&1 | pick
echo "=== B: same question + 5 retrieved examples ($RAG)"
"$BIN" calibrate "$RAG" --labeled "$RAGROWS" --local "$URL" --tainted --store ".shikii-eval/bench-$NAME/b" 2>&1 | pick
"$BIN" tune "$RAG" --labeled "$RAGROWS" --local "$URL" --tainted --store ".shikii-eval/bench-$NAME/b-tune" 2>&1 | pick

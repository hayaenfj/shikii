#!/bin/sh
# Comparison bench: same task, same model, arms that ask in different ways side by side (report only; nothing is written to the spec).
#   scripts/bench/run.sh <base-url> <short-name> [base-spec] [rag-spec] [labeled.json]
#   defaults: decisions/prompt-intent-v2.yaml  scripts/bench/prompt-intent-rag.yaml  testdata/prompt-intent-labeled.json
#   example (yes/no): scripts/bench/run.sh http://127.0.0.1:8092 qwen3-4b decisions/same-issue.yaml scripts/bench/same-issue-rag.yaml testdata/same-issue-labeled.json
# Arm A: the measured wording + calibration. If the spec has variants (e.g. layout: plain), tune measures every candidate (the winner's numbers are A)
# Arm B: RAG style. The same question with 5 similar examples (with truths) added to the body (built by scripts/bench/rag-rows.py; examples come from outside the row's fold). The rag spec's variants are measured too
# Arm C: LoRA is scripts/bench/lora/run.sh (docs/bench.md). This script does not run it.
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

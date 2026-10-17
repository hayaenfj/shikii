#!/bin/sh
# 同じ spec・同じラベルで別のモデルを測る(報告だけ。spec には書かない)。
#   scripts/eval-model.sh <base-url> <short-name>
# 例: scripts/eval-model.sh http://127.0.0.1:8082 openjev
# ラベルは評価ごとに別の store に残す(合算されて数字が混ざらないように)。
set -eu
URL="$1"; NAME="$2"
BIN="${BIN:-bin/shikii}"
pick() { grep -E "^(spec=|raw:|at act|at ask|calibrated:|vector 5-fold|platt 5-fold|brier|separation|choose|WARNING)" || true; }

for f in prompt-intent-labeled prompt-intent-labeled-ja prompt-intent-labeled-en; do
  echo "=== prompt-intent-v2 / $f"
  "$BIN" calibrate decisions/prompt-intent-v2.yaml --labeled "testdata/$f.json" \
    --local "$URL" --tainted --store ".shikii-eval/$NAME/$f" 2>&1 | pick
done
for s in same-issue same-inquiry; do
  echo "=== $s / 32 pairs (ja)"
  "$BIN" calibrate "decisions/$s.yaml" --local "$URL" --tainted --store ".shikii-eval/$NAME/$s" 2>&1 | pick
done

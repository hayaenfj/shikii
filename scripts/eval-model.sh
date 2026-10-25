#!/bin/sh
# Measure another model with the same spec and the same labels (report only; nothing is written to the spec).
#   scripts/eval-model.sh <base-url> <short-name>
# Example: scripts/eval-model.sh http://127.0.0.1:8082 openjev
# Labels are kept in a separate store per evaluation (so the numbers do not get added up and mixed).
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

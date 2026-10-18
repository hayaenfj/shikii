#!/bin/sh
# LoRA の腕(docs/bench.md の C)を回す。Apple silicon + mlx-lm。
#   scripts/bench/lora/run.sh <venv> <work-dir> [model] [spec.yaml] [labeled.json] [act]
# 1. 5 分割の材料  2. 分割ごとに LoRA を学習  3. その分割の test を当てる(土台だけでも当てて対照に)  4. 生の物差し  5. shikii calibrate で較正後の物差し
set -eu
VENV="$1"; WORK="$2"; MODEL="${3:-Qwen/Qwen3-4B}"
SPEC="${4:-decisions/prompt-intent-v2.yaml}"; LABELED="${5:-testdata/prompt-intent-labeled.json}"; ACT="${6:-0.75}"
PY="$VENV/bin/python"
ITERS="${ITERS:-200}"
BIN="${BIN:-bin/shikii}"
mkdir -p "$WORK"
"$PY" scripts/bench/lora/make_folds.py "$MODEL" "$WORK/data" "$SPEC" "$LABELED"
rm -f "$WORK/lora.jsonl" "$WORK/base.jsonl"
for k in 0 1 2 3 4; do
  "$VENV/bin/mlx_lm.lora" --model "$MODEL" --train --data "$WORK/data/fold$k" --iters "$ITERS" --batch-size 4 \
    --num-layers 8 --learning-rate 1e-4 --adapter-path "$WORK/adapters/fold$k" --steps-per-report 50 --steps-per-eval 100 --save-every 1000 2>&1 | grep -E "Iter|loss|Saved" | tail -4
  "$PY" scripts/bench/lora/score.py "$MODEL" "$WORK/adapters/fold$k" "$WORK/data/fold$k" "$WORK/lora.jsonl" "$SPEC"
  "$PY" scripts/bench/lora/score.py "$MODEL" - "$WORK/data/fold$k" "$WORK/base.jsonl" "$SPEC"
done
echo "=== C: LoRA (5-fold, out-of-sample, raw)"; python3 scripts/bench/lora/report.py "$WORK/lora.jsonl" "$ACT"
echo "=== base model, same prompt, no LoRA (control, raw)"; python3 scripts/bench/lora/report.py "$WORK/base.jsonl" "$ACT"
# 較正後の物差し: raw 付きの行を JSON 配列にして calibrate に読ませる(モデルは呼ばない)
for arm in lora base; do
  python3 -c "import json; print(json.dumps([json.loads(l) for l in open('$WORK/$arm.jsonl') if l.strip()], ensure_ascii=False))" > "$WORK/$arm.json"
  echo "=== $arm: shikii calibrate on the scored raw (5-fold CV, no model calls)"
  "$BIN" calibrate "$SPEC" --labeled "$WORK/$arm.json" --model "lora-bench-$arm" --store "$WORK/store-$arm" 2>&1 | grep -E "^(spec=|raw:|calibrated:|at act|vector 5-fold|platt 5-fold|choose|WARNING)" || true
done

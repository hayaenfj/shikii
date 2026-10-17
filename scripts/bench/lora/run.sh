#!/bin/sh
# LoRA の腕(docs/bench.md の C)を回す。Apple silicon + mlx-lm。
#   scripts/bench/lora/run.sh <venv> <work-dir> [model]
# 1. 5 分割の材料を作る  2. 分割ごとに LoRA を学習  3. その分割の test を当てる(土台だけでも当てて対照にする)  4. 物差しで読む
set -eu
VENV="$1"; WORK="$2"; MODEL="${3:-Qwen/Qwen3-4B}"
PY="$VENV/bin/python"
ITERS="${ITERS:-200}"
mkdir -p "$WORK"
"$PY" scripts/bench/lora/make_folds.py "$MODEL" "$WORK/data"
rm -f "$WORK/lora.jsonl" "$WORK/base.jsonl"
for k in 0 1 2 3 4; do
  "$VENV/bin/mlx_lm.lora" --model "$MODEL" --train --data "$WORK/data/fold$k" --iters "$ITERS" --batch-size 4 \
    --num-layers 8 --learning-rate 1e-4 --adapter-path "$WORK/adapters/fold$k" --steps-per-report 50 --steps-per-eval 100 --save-every 1000 2>&1 | grep -E "Iter|loss|Saved" | tail -4
  "$PY" scripts/bench/lora/score.py "$MODEL" "$WORK/adapters/fold$k" "$WORK/data/fold$k" "$WORK/lora.jsonl"
  "$PY" scripts/bench/lora/score.py "$MODEL" - "$WORK/data/fold$k" "$WORK/base.jsonl"
done
echo "=== C: LoRA (5-fold, out-of-sample)"; python3 scripts/bench/lora/report.py "$WORK/lora.jsonl"
echo "=== base model, same prompt, no LoRA (control)"; python3 scripts/bench/lora/report.py "$WORK/base.jsonl"

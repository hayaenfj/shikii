#!/usr/bin/env python3
"""Scores the LoRA arm (for any spec). Predicts fold k's test.json with fold k's adapter.

It does not generate. It reads the log probability of the token after the prompt at the first token of each option id (id for choice, yes/no for noul, the number for score),
and turns them into a distribution with softmax (the same idea as shikii-readout's plain layout). The output is rows of {"inputs":…, "raw":{id: p}, "truth":…};
joining all folds gives out-of-sample raw values. shikii calibrate --labeled reads it as is (rows whose raw is an object).

Usage: venv/bin/python scripts/bench/lora/score.py <model> <adapter-path or -> <fold-dir> <out.jsonl (appended)> <spec.yaml>
Passing - as the adapter scores with the base model only (no LoRA) (the control for arm C).
"""
import json
import sys

import mlx.core as mx
from mlx_lm import load

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from make_folds import load_spec, option_ids, prompt, truth_id  # noqa: E402


def main():
    model_id, adapter, fold, out, spec_path = sys.argv[1:6]
    spec = load_spec(spec_path)
    model, tok = load(model_id, adapter_path=None if adapter == "-" else adapter)
    opts = option_ids(spec)
    ids = [tok.encode(o, add_special_tokens=False)[0] for o in opts]
    if len(set(ids)) != len(ids):
        sys.exit(f"option ids share a first token: {ids}")
    rows = json.load(open(f"{fold}/test.json", encoding="utf-8"))
    with open(out, "a", encoding="utf-8") as f:
        for r in rows:
            x = mx.array(tok.encode(prompt(tok, spec, r["inputs"]), add_special_tokens=False))[None]
            logits = model(x)[0, -1]
            probs = mx.softmax(mx.take(logits, mx.array(ids)).astype(mx.float32))
            mx.eval(probs)
            raw = {opts[i]: float(probs[i]) for i in range(len(opts))}
            row = {"inputs": r["inputs"], "raw": raw, "truth": truth_id(spec, r["truth"])}
            if "weight" in r:
                row["weight"] = r["weight"]
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
    print(f"{fold}: scored {len(rows)} rows with adapter={adapter}")


if __name__ == "__main__":
    main()

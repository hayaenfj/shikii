#!/usr/bin/env python3
"""LoRA の腕の採点(どの spec でも)。分割 k の adapter で、その分割の test.json を当てる。

生成はしない。prompt の次のトークンの対数確率を選択肢 id(choice は id、noul は yes/no、score は番号)の最初のトークンで読み、
softmax で分布にする(shikii-readout の plain 配置と同じ考え方)。出力は {"inputs":…, "raw":{id: p}, "truth":…} の行で、
全部の分割をつなげると out-of-sample の raw になる。shikii calibrate --labeled がそのまま読める(raw が object の行)。

使い方: venv/bin/python scripts/bench/lora/score.py <model> <adapter-path or -> <fold-dir> <out.jsonl(追記)> <spec.yaml>
adapter に - を渡すと土台だけ(LoRA 無し)で採点する(腕 C の対照)。
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

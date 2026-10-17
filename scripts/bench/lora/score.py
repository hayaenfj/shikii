#!/usr/bin/env python3
"""LoRA の腕の採点。分割 k の adapter で、その分割の test.json を当てる。

生成はしない。prompt の次のトークンの対数確率を 5 つの選択肢 id の最初のトークンで読み、softmax で分布にする(shikii-readout と同じ考え方)。
出力は {"inputs":…, "raw":{id: p}, "truth":…} の行で、全部の分割をつなげると 62 件の out-of-sample の raw になる。

使い方: venv/bin/python scripts/bench/lora/score.py <model> <adapter-path or -> <fold-dir> <out.jsonl(追記)>
adapter に - を渡すと土台だけ(LoRA 無し)で採点する(腕 C の対照)。
"""
import json
import sys

import mlx.core as mx
from mlx_lm import load

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from make_folds import OPTIONS, prompt  # noqa: E402


def main():
    model_id, adapter, fold, out = sys.argv[1:5]
    model, tok = load(model_id, adapter_path=None if adapter == "-" else adapter)
    ids = []
    for opt, _ in OPTIONS:
        t = tok.encode(opt, add_special_tokens=False)
        ids.append(t[0])
    if len(set(ids)) != len(ids):
        sys.exit(f"option ids share a first token: {ids}")
    rows = json.load(open(f"{fold}/test.json", encoding="utf-8"))
    with open(out, "a", encoding="utf-8") as f:
        for r in rows:
            p = prompt(tok, r["inputs"]["message"])
            x = mx.array(tok.encode(p, add_special_tokens=False))[None]
            logits = model(x)[0, -1]
            sel = mx.take(logits, mx.array(ids)).astype(mx.float32)
            probs = mx.softmax(sel)
            mx.eval(probs)
            raw = {OPTIONS[i][0]: float(probs[i]) for i in range(len(OPTIONS))}
            f.write(json.dumps({"inputs": r["inputs"], "raw": raw, "truth": r["truth"]}, ensure_ascii=False) + "\n")
    print(f"{fold}: scored {len(rows)} rows with adapter={adapter}")


if __name__ == "__main__":
    main()

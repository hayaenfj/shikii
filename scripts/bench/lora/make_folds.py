#!/usr/bin/env python3
"""LoRA の腕(docs/bench.md の C)の材料を作る(どの spec でも)。

正解の貯金(labeled json)を 5 分割(行番号 % 5)し、分割ごとに
  <out>/fold<k>/train.jsonl  … その分割以外の行。mlx_lm.lora の "text" 形式(prompt + 正解の id + <|im_end|>)
  <out>/fold<k>/valid.jsonl  … train の末尾 4 行(損失の表示用)
  <out>/fold<k>/test.json    … その分割の行(inputs と truth)。score.py がこれを当てる
を書く。prompt は spec の質問・選択肢(choice は id: 説明、noul は yes/no、score は段階)・state の template を平文にしたもので、
Qwen3 のチャット形式(thinking 無し)で囲む。学習と採点で同じ文字列を使う。

使い方: venv/bin/python scripts/bench/lora/make_folds.py <model> <out> <spec.yaml> <labeled.json>
"""
import json
import os
import re
import sys

import yaml
from transformers import AutoTokenizer


def load_spec(path):
    return yaml.safe_load(open(path, encoding="utf-8"))


def option_ids(spec):
    t = spec["type"]
    if t == "choice":
        return [o["id"] for o in spec["options"]]
    if t == "noul":
        return ["yes", "no"]
    return [str(i) for i in range(len(spec["levels"]))]


def truth_id(spec, truth):
    if spec["type"] == "noul":
        return "yes" if truth in (True, "yes", "true") else "no"
    return str(truth)


def render(spec, inputs):
    text = spec["state"]["template"]
    for k, v in inputs.items():
        text = re.sub(r"\{\{\s*" + re.escape(k) + r"\s*\}\}", str(v), text)
    return text.strip()


def question_text(spec):
    q = spec["question"].strip()
    t = spec["type"]
    if t == "choice":
        opts = "\n".join(f"- {o['id']}: {o['description']}" for o in spec["options"])
        return f"{q}\n\nOptions:\n{opts}\n\nInput:\n{{state}}\n\nAnswer with the option id only."
    if t == "noul":
        return f"Is the following statement true of the input?\nStatement: {q}\n\nOptions:\n- yes\n- no\n\nInput:\n{{state}}\n\nAnswer yes or no only."
    scale = "\n".join(f"- {i}: {lv}" for i, lv in enumerate(spec["levels"]))
    return f"{q}\n\nScale (lowest first):\n{scale}\n\nInput:\n{{state}}\n\nAnswer with the number only."


def prompt(tok, spec, inputs):
    msgs = [{"role": "user", "content": question_text(spec).replace("{state}", render(spec, inputs))}]
    return tok.apply_chat_template(msgs, tokenize=False, add_generation_prompt=True, enable_thinking=False)


def main():
    model, out, spec_path, labeled = sys.argv[1:5]
    tok = AutoTokenizer.from_pretrained(model)
    spec = load_spec(spec_path)
    rows = json.load(open(labeled, encoding="utf-8"))
    folds = 5
    for k in range(folds):
        d = os.path.join(out, f"fold{k}")
        os.makedirs(d, exist_ok=True)
        train = [r for i, r in enumerate(rows) if i % folds != k]
        test = [r for i, r in enumerate(rows) if i % folds == k]
        line = lambda r: json.dumps({"text": prompt(tok, spec, r["inputs"]) + truth_id(spec, r["truth"]) + "<|im_end|>"}, ensure_ascii=False) + "\n"
        with open(os.path.join(d, "train.jsonl"), "w", encoding="utf-8") as f:
            f.writelines(line(r) for r in train)
        with open(os.path.join(d, "valid.jsonl"), "w", encoding="utf-8") as f:
            f.writelines(line(r) for r in train[-4:])
        json.dump(test, open(os.path.join(d, "test.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=1)
        print(f"fold{k}: train {len(train)} test {len(test)}")
    print("prompt example:\n" + prompt(tok, spec, rows[0]["inputs"]))


if __name__ == "__main__":
    main()

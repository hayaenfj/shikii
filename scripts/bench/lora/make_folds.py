#!/usr/bin/env python3
"""Builds the material for the LoRA arm (C in docs/bench.md) (for any spec).

Splits the saved truths (labeled json) into 5 folds (row number % 5) and, for each fold, writes
  <out>/fold<k>/train.jsonl  … rows outside that fold. mlx_lm.lora "text" format (prompt + correct id + <|im_end|>)
  <out>/fold<k>/valid.jsonl  … the last 4 rows of train (for showing the loss)
  <out>/fold<k>/test.json    … the rows of that fold (inputs and truth). score.py predicts these
The prompt is the spec's question, options (id: description for choice, yes/no for noul, levels for score) and the state template as plain text,
wrapped in Qwen3's chat format (no thinking). Training and scoring use the same string.

Usage: venv/bin/python scripts/bench/lora/make_folds.py <model> <out> <spec.yaml> <labeled.json>
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

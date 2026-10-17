#!/usr/bin/env python3
"""LoRA の腕(docs/bench.md の C)の材料を作る。

正解の貯金(testdata/prompt-intent-labeled.json、62 件)を 5 分割(行番号 % 5)し、分割ごとに
  <out>/fold<k>/train.jsonl  … その分割以外の行(約 50 件)。mlx_lm.lora の "text" 形式(prompt + 正解の id + <|im_end|>)
  <out>/fold<k>/valid.jsonl  … train の末尾 4 行(損失の表示用。学習には使わない)
  <out>/fold<k>/test.json    … その分割の行(inputs と truth)。score.py がこれを当てる
を書く。prompt は v2 の spec と同じ質問・選択肢の説明を平文にしたもので、Qwen3 のチャット形式(thinking 無し)で囲む。
学習と採点で同じ文字列を使うので、ずれは無い。

使い方: venv/bin/python scripts/bench/lora/make_folds.py <model-dir-or-id> <out>
"""
import json
import os
import sys

from transformers import AutoTokenizer

OPTIONS = [
    ("change_request", "Change something in the repository - edit, fix, add, remove, rename, refactor, write tests or docs. Includes terse commands like \"fix the typo\" or \"直して\"."),
    ("question", "Explain, investigate, compare or give an opinion, without changing anything. Includes \"why\", \"what does this do\", \"where is\", \"調べて\"."),
    ("review", "Evaluate changes or code that already exist - review a diff or PR, check for problems, rate quality."),
    ("operation", "Run or operate something without editing code - run tests, build, commit, push, deploy, restart, install, show logs."),
    ("other", "Not a request at all - greetings, thanks, acknowledgements (\"ok\", \"了解\"), small talk, or ending the session."),
]
QUESTION = (
    "A user sent the following message to a coding assistant. What is the user asking the assistant to do?\n"
    "Short messages like \"fix it\" or \"why?\" are still requests; pick the closest request type.\n\n"
    "Request types:\n" + "\n".join(f"- {i}: {d}" for i, d in OPTIONS) + "\n\nMessage:\n{message}\n\nAnswer with the request type id only."
)


def prompt(tok, message):
    msgs = [{"role": "user", "content": QUESTION.format(message=message)}]
    return tok.apply_chat_template(msgs, tokenize=False, add_generation_prompt=True, enable_thinking=False)


def main():
    model, out = sys.argv[1], sys.argv[2]
    tok = AutoTokenizer.from_pretrained(model)
    rows = json.load(open("testdata/prompt-intent-labeled.json", encoding="utf-8"))
    folds = 5
    for k in range(folds):
        d = os.path.join(out, f"fold{k}")
        os.makedirs(d, exist_ok=True)
        train = [r for i, r in enumerate(rows) if i % folds != k]
        test = [r for i, r in enumerate(rows) if i % folds == k]
        with open(os.path.join(d, "train.jsonl"), "w", encoding="utf-8") as f:
            for r in train:
                f.write(json.dumps({"text": prompt(tok, r["inputs"]["message"]) + r["truth"] + "<|im_end|>"}, ensure_ascii=False) + "\n")
        with open(os.path.join(d, "valid.jsonl"), "w", encoding="utf-8") as f:
            for r in train[-4:]:
                f.write(json.dumps({"text": prompt(tok, r["inputs"]["message"]) + r["truth"] + "<|im_end|>"}, ensure_ascii=False) + "\n")
        json.dump(test, open(os.path.join(d, "test.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=1)
        print(f"fold{k}: train {len(train)} test {len(test)}")
    print("prompt example:\n" + prompt(tok, "fix the typo"))


if __name__ == "__main__":
    main()

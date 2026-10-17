#!/usr/bin/env python3
"""比較ベンチの RAG の腕の材料を作る。

正解の貯金(testdata/prompt-intent-labeled.json)の各行に、その行以外から似た例 k 件(正解付き)を付けて
testdata/prompt-intent-rag.json に書く。似ている度は文字 2-gram の Jaccard(日本語と英語を同じ物差しで測るため)。
検索の元は「その行以外の全部」(leave-one-out)。calibrate の中の 5 分割とは揃えていないので、RAG の腕にやや有利(docs/bench.md に明記)。

使い方: python3 scripts/bench/rag-rows.py [k]
"""
import json
import sys


def grams(text):
    t = "".join(text.lower().split())
    return {t[i : i + 2] for i in range(len(t) - 1)} or {t}


def jaccard(a, b):
    return len(a & b) / len(a | b) if a | b else 0.0


def main():
    k = int(sys.argv[1]) if len(sys.argv) > 1 else 5
    rows = json.load(open("testdata/prompt-intent-labeled.json", encoding="utf-8"))
    gs = [grams(r["inputs"]["message"]) for r in rows]
    out = []
    for i, r in enumerate(rows):
        ranked = sorted((j for j in range(len(rows)) if j != i), key=lambda j: -jaccard(gs[i], gs[j]))[:k]
        examples = "\n".join(f'- "{rows[j]["inputs"]["message"]}" → {rows[j]["truth"]}' for j in ranked)
        out.append({"inputs": {"message": r["inputs"]["message"], "examples": examples}, "truth": r["truth"]})
    with open("testdata/prompt-intent-rag.json", "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=1)
    print(f"wrote testdata/prompt-intent-rag.json: {len(out)} rows, k={k} examples each (leave-one-out)")


if __name__ == "__main__":
    main()

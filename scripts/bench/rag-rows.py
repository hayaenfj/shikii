#!/usr/bin/env python3
"""Builds the material for the RAG arm of the comparison bench (for any spec).

Writes each row of the saved truths (labeled json) with k similar examples (with truths) added as examples.
Similarity is the Jaccard of character 2-grams over all input strings joined (to measure Japanese and English with the same yardstick).
The search pool is "the rest, minus the rows in the same fold as that row (row number % folds)". With the same proportions as calibration's 5 folds (fit on 4, test on 1),
this keeps the rows being tested out of the search pool. folds=1 means leave-one-out (only that row is removed; favors RAG).

Usage: python3 scripts/bench/rag-rows.py <labeled.json> <out.json> [k=5] [folds=5]
"""
import json
import sys


def grams(text):
    t = "".join(text.lower().split())
    return {t[i : i + 2] for i in range(len(t) - 1)} or {t}


def jaccard(a, b):
    return len(a & b) / len(a | b) if a | b else 0.0


def truth_text(t):
    if isinstance(t, bool):
        return "yes" if t else "no"
    return str(t)


def main():
    src, out = sys.argv[1], sys.argv[2]
    k = int(sys.argv[3]) if len(sys.argv) > 3 else 5
    folds = int(sys.argv[4]) if len(sys.argv) > 4 else 5
    rows = json.load(open(src, encoding="utf-8"))
    gs = [grams(" ".join(str(v) for v in r["inputs"].values())) for r in rows]
    result = []
    for i, r in enumerate(rows):
        pool = [j for j in range(len(rows)) if j != i and (folds <= 1 or j % folds != i % folds)]
        ranked = sorted(pool, key=lambda j: -jaccard(gs[i], gs[j]))[:k]
        examples = "\n".join("- " + " / ".join(f"{kk}: {vv}" for kk, vv in rows[j]["inputs"].items()) + f" → {truth_text(rows[j]['truth'])}" for j in ranked)
        inputs = dict(r["inputs"])
        inputs["examples"] = examples
        result.append({"inputs": inputs, "truth": r["truth"], **({"weight": r["weight"]} if "weight" in r else {})})
    with open(out, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=1)
    how = "leave-one-out" if folds <= 1 else f"examples drawn outside the row's fold ({folds} folds)"
    print(f"wrote {out}: {len(result)} rows, k={k} examples each ({how})")


if __name__ == "__main__":
    main()

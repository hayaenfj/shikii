#!/usr/bin/env python3
"""比較ベンチの RAG の腕の材料を作る(どの spec でも)。

正解の貯金(labeled json)の各行に、似た例 k 件(正解付き)を examples として足して書く。
似ている度は入力の文字列を全部つないだものの文字 2-gram の Jaccard(日本語と英語を同じ物差しで測るため)。
検索の元は「その行と同じ分割(行番号 % folds)を除いた残り」。較正の 5 分割(4 つで当てて 1 つで試す)と同じ割合で、
試す側の行が検索の元に入らないようにする。folds=1 なら leave-one-out(その行だけ除く。RAG に有利)。

使い方: python3 scripts/bench/rag-rows.py <labeled.json> <out.json> [k=5] [folds=5]
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

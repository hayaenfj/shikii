#!/usr/bin/env python3
"""score.py の出力(62 件の raw)を A・B と同じ物差しで読む: top-1、Brier(top)、act 閾値 0.75 での precision / recall。

全部の行が「それを見ていない adapter」の答えなので、ここでの数字は out-of-sample(較正の 5 分割 CV に相当)。
線(較正)は掛けない: shikii の calibrate が choice の raw 付きの行をファイルから読む口が無いため、A・B の「生」の列と比べる。

使い方: python3 scripts/bench/lora/report.py <scored.jsonl>
"""
import json
import sys


def main():
    rows = [json.loads(l) for l in open(sys.argv[1], encoding="utf-8") if l.strip()]
    n = len(rows)
    hit = brier = tp = fp = fn = 0
    for r in rows:
        top = max(r["raw"], key=r["raw"].get)
        p = r["raw"][top]
        ok = top == r["truth"]
        hit += ok
        brier += (p - (1.0 if ok else 0.0)) ** 2
        if p >= 0.75:
            tp += ok
            fp += not ok
        else:
            fn += 1
    prec = tp / (tp + fp) if tp + fp else 0.0
    rec = tp / (tp + fn) if tp + fn else 0.0
    print(f"n={n} top-1={hit / n:.3f} brier(top)={brier / n:.4f} at act>=0.75: precision={prec:.3f} recall={rec:.3f} (tp={tp} fp={fp} fn={fn})")


if __name__ == "__main__":
    main()

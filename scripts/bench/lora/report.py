#!/usr/bin/env python3
"""score.py の出力(raw 付きの行)を手早く読む: top-1、Brier(top)、act 閾値での precision / recall(生、未較正)。

較正後の数字(5 分割 CV の prec / rec、選ばれた線)は shikii calibrate --labeled <この出力を JSON 配列にしたもの> で出す
(scripts/bench/lora/run.sh がやる)。ここは生の確認用。

使い方: python3 scripts/bench/lora/report.py <scored.jsonl> [act=0.75]
"""
import json
import sys


def main():
    rows = [json.loads(l) for l in open(sys.argv[1], encoding="utf-8") if l.strip()]
    act = float(sys.argv[2]) if len(sys.argv) > 2 else 0.75
    n = len(rows)
    hit = brier = tp = fp = fn = 0
    for r in rows:
        top = max(r["raw"], key=r["raw"].get)
        p = r["raw"][top]
        ok = top == str(r["truth"])
        hit += ok
        brier += (p - (1.0 if ok else 0.0)) ** 2
        if p >= act:
            tp += ok
            fp += not ok
        else:
            fn += 1
    prec = tp / (tp + fp) if tp + fp else 0.0
    rec = tp / (tp + fn) if tp + fn else 0.0
    print(f"n={n} top-1={hit / n:.3f} brier(top)={brier / n:.4f} at act>={act}: precision={prec:.3f} recall={rec:.3f} (tp={tp} fp={fp} fn={fn})")


if __name__ == "__main__":
    main()

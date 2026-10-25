# For ordinal scales, set the band from P(level ≥ k)

status: confirmed

## When it applies

- Type: score. With 3 or more levels, when probability splits across neighboring levels (almost always)
- Models: all

## How to write it

Write `thresholds.level: k` in the spec. The band is set by the summed probability of level k and above, not by the top level's probability. Accuracy is measured the same way: "P(≥k) against truth ≥ k".

## Evidence

- relatedness-level, same-bug pairs (Ternary-Bonsai-27B readout): top level's probability 0.544 (stops at ask), P(≥3) = 0.950 (act). Unrelated pairs: 0.008
- The same shape on the old material (top level 0.52 → P(≥3) 0.96, Kev-4B, 2026-10-16)

"Bands for score" in [docs/guide.ja.md](https://github.com/hayaenfj/shikii/blob/main/docs/guide.ja.md)

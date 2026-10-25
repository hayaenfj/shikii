# How to ask a score (ordered levels)

## Basics

- 2 to 10 levels, from low to high. One sentence per level on "what belongs here"
- Probability usually splits across neighboring levels. **Do not set the band from the top level's probability.** Use P(level ≥ k) with `thresholds.level: k` → [ordinal-band-on-mass](../patterns/ordinal-band-on-mass.md)
- In the description of a borderline level ("circumstantial", "same area"), write how it differs from the level above

## Evidence

- relatedness-level (same bug / different bug on the same screen / unrelated): with the Ternary-Bonsai-27B readout, P(≥3) = 0.950 / 0.048 / 0.008. The top level's probability was 0.544, which on its own stops at ask (2026-10-17)

# Kev-4B (ggml-org/Kev-4B-GGUF, llama.cpp's decision model)

- A trained decision model. Its probabilities swing all the way to the ends (1.0 / 0.0 are common)
- Fast: 0.25 seconds per question (Q4_K_M, Metal). Suits "once per request" uses such as hooks
- Listed as English only, but with the v2 wording it reaches top-1 0.913 on Japanese requests too (v1 drifted to other: 0.68). **Wording matters more than language**
- Calibration: on prompt-intent (choice), vector won in CV (correcting the drift to other). On same-issue / same-inquiry (noul), platt wins slightly (0.038 → 0.035, 0.030 → 0.020)
- Order bias: 0 flips and shifts ≤ 0.024 over 100 reorderings. Negligible
- Weak on a 5-level score (P(≥3) = 0.418 on same-bug pairs). Ordinal scales are better left to 27B-class models

Evidence: [docs/model-comparison.md](https://github.com/hayaenfj/shikii/blob/main/docs/model-comparison.md)

---
claim: When reading a plain chat model (not trained as a decision model) with shikii-readout, the plain layout (no system message, options as a list of "- id", reading the id's first token) scores better than state-last
status: tentative
applies_to:
  models: Plain chat models (confirmed on Qwen3-4B). Decision models ignore the layout. The gap is small on 27B (Ternary-Bonsai)
  type: choice
evidence:
  - date: "2026-10-18"
    model: qwen3-4b-q8+state-last
    result: plain 0.903 vs current 0.774 (top-1 on 62 truths; 8 better / 1 worse, p=0.020)
    verdict: supports
    run: 2026-10-18-prompt-intent-qwen3-4b-q8-2
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-last
    result: plain 0.984 vs current 0.968 (top-1 on 62 truths; 1 better / 0 worse, p=0.500)
    verdict: inconclusive
retest:
  spec: .shikii/decisions/prompt-intent-v2.yaml
  better: plain
  than: current
---
# Use the plain layout for plain chat models (no system message, read the id as is)


## When it applies

- Models: when reading a plain chat model that was not trained as a decision model with `shikii-readout` (confirmed on Qwen3-4B)
- Does not help / not needed: decision models (Kev-4B and others) ignore the layout. The 27B readout (Ternary-Bonsai) already reaches 0.968 with state-last, 0.984 with plain (Brier 0.0136 → 0.0095), and 1.000 with plain + a shorter question. No harm, but the holdout was 1 win, 0 losses (p=0.5), which does not pass the gate, so current is kept ([run](https://github.com/hayaenfj/shikii/blob/main/wiki/runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-3.md)). More truths will settle it

## How to write it

Put `layout: plain` in the spec or a variant. The readout drops the system message and lays out: the question → a list of `- id: description` → the text after `Input:` → "Answer with the option id only.", and reads the probability of **each id's first token** instead of a letter (A, B, …). The layout goes into the hash and the model name (`…+plain`), so a separate line is drawn.

## Evidence

Qwen3-4B Q8_0 (upstream llama.cpp 0.7.0 + `shikii-readout`), 62 prompt-intent items, same weights:

| Candidate | top-1 | Brier | Notes |
|---|---|---|---|
| state-last (default, v2 wording) | 0.774 | 0.2216 | everything sticks at 0.75 or above |
| state-last + shorter question | 0.855 | 0.1394 | a wording-only improvement |
| **plain** | **0.903** | **0.0959** | holdout 0.887 vs 0.774, 8 wins 1 loss, p=0.020 |
| plain + shorter question | 0.887 | 0.0975 | |
| plain-text prompt in mlx (control, bf16) | 0.903 | 0.0967 | same number as readout's plain → the difference is the layout |

run: [2026-10-18-prompt-intent-qwen3-4b-q8-2](https://github.com/hayaenfj/shikii/blob/main/wiki/runs/2026-10-18-prompt-intent-qwen3-4b-q8-2.md). For the context of the comparison bench, see [docs/bench.md](https://github.com/hayaenfj/shikii/blob/main/docs/bench.md).

## Reasoning (a guess)

The layout meant for decision models ("answer with a letter" in the system message, a `<state>` tag, A/B/C) suits models trained that way, but for a plain chat model "answer with the option's name as is" is more natural, and mapping to letters breaks the probabilities. plain shapes the answer to fit the model's habits.

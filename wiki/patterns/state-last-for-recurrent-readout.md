---
claim: When reading an ordinary chat model with shikii-readout, putting the text (state) after the options, right before the answer (state-last), scores better than putting it first (state-first)
status: tentative
applies_to:
  models: The Qwen3.5 / 3.8 family with recurrent layers (confirmed on Ternary-Bonsai-27B). No change on Bonsai 2 (fork)
  type: choice
evidence:
  - date: "2026-10-17"
    model: ternary-bonsai-27b-q2g64
    result: state-last 0.968 vs state-first 0.919 (top-1 on 62 truths; the 23 Japanese items 1.000 vs 0.870)
    verdict: supports
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-first
    result: current 0.968 vs state-first 0.919 (top-1 on 62 truths; 3 better / 0 worse, p=0.125)
    verdict: inconclusive
retest:
  spec: .shikii/decisions/prompt-intent-v2.yaml
  better: current
  than: state-first
---
# Put the text (state) right before the answer (readout of ordinary chat models)


## When it applies

- Models: when reading an ordinary chat GGUF that is not a decision model with `shikii-readout`. Especially the Qwen3.5 / 3.6 / 3.8 family (Bonsai)
- Type: all (confirmed on choice; the readout works the same way for noul / score)

## How to write it

Order: question → options → **text** → "Reply with one letter." Do not put the text first.
This is `shikii-readout`'s default layout, and `+state-last` is added to the response's `model` (a different layout means a different calibration key).

## Evidence

- Ternary-Bonsai-27B Q2_g64: state-first top-1 0.919 / Japanese 0.870 → state-last 0.968 / 1.000 (same weights, 62 items). [integrations/readout.md](https://github.com/hayaenfj/shikii/blob/main/integrations/readout.md)
- Bonsai 2 27B (fork): unchanged at 0.968. It does not get worse

## Side effect

Every question of the same spec shares a prefix, so `cache_prompt` helps: 4 seconds → 0.6 to 0.8 seconds per question. The same change gained both accuracy and speed

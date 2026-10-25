**English** · [日本語](model-comparison.ja.md)

# Comparing local decision models (same specs, same truths)

Measured: 2026-10-16 to 17. Tool: `scripts/eval-model.sh <url> <name>` (reports only; writes nothing to the spec; a separate store per evaluation).
Environment: Mac 128GB, llama.cpp from brew 0.6.0 (Kev-4B, OpenJev) and 0.7.0 (Ternary-Bonsai-27B). Each model is Q4_K_M (the Bonsai family is ternary).

The numbers are "raw" (before correction) and the value of the correction chosen by 5-fold CV. llama-server's parallel slots make floating point wobble,
and on the same 62 items the result can move by one item (0.016), so read them within that range.

## Evaluation sets (all made up)

| Set | Items | Content |
|---|---|---|
| prompt-intent-v2 / all | 62 | 5 options for a request text to an AI (39 English, 23 Japanese). Truths labeled by the worker |
| prompt-intent-v2 / ja, en | 23, 39 | The above, Japanese only and English only |
| same-issue | 32 | Duplicate detection for bug reports (12 same bug, 12 different, and 8 different bugs on the same screen as boundary cases, false with weight 0.5). Yes/no, Japanese |
| same-inquiry | 32 | Whether two inquiries are the same (12 same case, 12 different, 8 different cases of the same kind). Yes/no, Japanese |

## Results

| Model | Size | License | all top-1 | ja | en | all after calibration CV prec/rec@act | same-issue Brier raw → chosen correction | same-inquiry same | Per question | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|
| **Kev-4B** | 4B | Apache 2.0 | 0.919 | 0.913 | 0.923 | 0.962 / 0.895 (vector) | 0.038 → **0.035 (platt)** | 0.030 → **0.020 (platt)** | 0.25 s | Reasonable as the default. Fast. Almost no gap in Japanese |
| Julia-1 | 0.1B | Apache 2.0 | 0.129 | — | — | 0.000 / 0.000 | (22 inversions on the old material) | — | — | **Unusable** (below 0.2, the chance level for 5 options) |
| **OpenJev** | 27B | **CC BY-NC 4.0** | **0.984** | **1.000** | 0.974 | 1.000 / 0.952 (vector) | **0.007** (identity) | 0.037 (identity) | 3.8 s | A head above in precision. Needs no correction. Non-commercial |
| **Bonsai 2 27B** (through BonJev) | 27B ternary | Apache 2.0 | 0.968 | **1.000** | 0.949 | 0.982 / 0.949 (vector) | 0.049 → **0.019 (platt)** | 0.084 → **0.028 (platt)** | 2.5 s | An ordinary LLM + logit readout. Correction makes it 2.5 to 3 times better |
| **Bonsai 2 27B** (`shikii-readout` state-last, forked llama-server) | 27B ternary | Apache 2.0 | 0.968 | 1.000 | 0.949 | identity (raw **1.000 / 1.000**) | 0.024 → **0.016 (platt)** | 0.059 → **0.014 (platt)** | 0.83 s | Our own adapter. Same ranking as BonJev, sharper probabilities |
| **Ternary-Bonsai-27B** Q2_g64 (`shikii-readout` state-last, **upstream llama.cpp 0.7.0**) | 27B ternary | Apache 2.0 | **0.968** | **1.000** | 0.949 | identity (raw 1.000 / 0.983) | 0.048 → **0.024 (platt)** | 0.033 → **0.020 (platt)** | **0.58 s** | **No fork, commercial use allowed, fastest**. Arrangement moves top-1 by 5 points (state-first is 0.919) |

On the two yes/no sets, positives and negatives separated completely in all 10 combinations (5 interfaces × 2 sets) (no inversions). The only difference is the quality of the probabilities (Brier, smaller is better).

## How to read it

- **top-1** is whether the ranking is right. Wording problems show up here (Kev-4B goes from 0.682 with the v1 wording to the 0.90s with v2)
- **After calibration CV prec/rec@act** is "may act be allowed" and "how much reaches act". The result of comparing identity / vector / platt with 5-fold CV and choosing
- **Brier raw → correction** is how good the yes/no probabilities are as they come, and how far correction improves them. Whether correction is needed is decided by numbers per (question, model), not by what a model claims:
  OpenJev is identity on both sets, Kev-4B gains slightly with platt, and ordinary LLMs get 2 to 4 times better with platt
- An **inversion** is when the lowest positive < the highest negative (excluding weak labels). Calibration does not fix it. Change the question text or the model

## How to choose (for classifying request kinds)

1. If commercial use and speed are needed, **Kev-4B**. ja 0.913, 0.25 s per question. Enough for the hook use (once per request)
2. If non-commercial is fine and precision comes first, **OpenJev**. ja 1.000, yes/no needs no correction. But ≈ 3.8 s per question and 16GB resident. Suited to batches
3. This table itself is the framework's claim: **with the same specs and the same truths, models can be chosen by numbers, including whether calibration is needed**. Which model is better depends on the use (speed, license, language) and is not decided here
4. To **use an ordinary GGUF with upstream llama.cpp alone**, use `shikii-readout` ([`../integrations/readout.md`](../integrations/readout.md)).
   Ternary-Bonsai-27B Q2_g64 gives all 0.968 / ja 1.000, 0.58 s per question, Apache 2.0, no fork. In this table it is **the closest to a default that fits any use**.
   An external implementation on the same principle, [BonJev](https://github.com/ChezRD/bonjev) (Bonsai only, FFI into the Prism fork), is in [`../integrations/bonjev.md`](../integrations/bonjev.md).
   The meaning of these rows is that **"an uncalibrated model + the framework's calibration" reaches the same place as "a calibrated decision model"**

Note: on 2026-10-17 the yes/no material was replaced (earlier numbers are on different material and are not published). The prompt-intent numbers did not change.

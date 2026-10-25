**English** · [日本語](bench.ja.md)

# Comparison bench: measured wording + calibration / RAG / LoRA

This is where we test the README's hypothesis: "for typed decisions, measured wording plus calibration, on the same base model, may reach usable precision with far fewer examples than fine-tuning (LoRA), and may give more honest probabilities than adding context by search (RAG)". **It is not a claim yet.** The numbers here are observations on 1 task, 62 items and 2 models.

## What is held the same

- Task: `prompt-intent` (5 options for a request text to an AI)
- Truths: the 62 items in `testdata/prompt-intent-labeled.json` (39 English, 23 Japanese, labeled by a person)
- Model: the same in all three arms. Calibration is `calibrate`'s 5 folds (fit on 4, test on 1)
- Measures: top-1, Brier (top), precision / recall at the act threshold (0.75) (after calibration, the 5-fold CV values)

## The three arms

| Arm | What it does | How to run |
|---|---|---|
| **A. Measured wording + calibration** | Ask once with the v2 wording chosen by `tune`, and `calibrate` draws the line. What shikii always does | `scripts/bench/run.sh <url> <name>` |
| **B. RAG-style** | For the same question, add 5 similar examples (with truths) before the body. Similarity is Jaccard over character 2-grams. The search pool is **the 4/5 excluding the row's own fold (of 5)**, the same ratio as calibration's 5 folds (fit on 4, test on 1 = 20%) (rows on the test side are not in the search pool) | Same as above (`scripts/bench/rag-rows.py` makes the material, asked with `scripts/bench/prompt-intent-rag.yaml`) |
| **C. LoRA** | Fine-tune the base model on 4/5 of the 5 folds (about 50 items) and read the probabilities of the option tokens | `scripts/bench/lora/run.sh <venv> <work>` (mlx-lm, Apple silicon) |

Note on B: at first the search pool was leave-one-out (the 61 items other than that row). This favors RAG, and the 27B's Brier came out at 0.0092. With the current form, drawing from outside the fold, it is 0.0179. The table below uses the current form.

## Results (2026-10-18)

| Model | Arm | top-1 (raw) | Brier (raw) | After calibration CV prec / rec @act | Line chosen |
|---|---|---|---|---|---|
| Ternary-Bonsai-27B (`shikii-readout` state-last, upstream llama.cpp) | A | 0.968 | 0.0136 | 1.000 / 0.951 | identity (raw 1.000 / 0.983) |
| Same | B | **0.984** | 0.0179 | 1.000 / 0.951 | identity (raw 1.000 / 0.951) |
| Kev-4B Q4_K_M | A | 0.919 | 0.0864 | 0.927 / 0.895 | vector |
| Same | B | **0.935** | **0.0813** | **0.963 / 0.929** | vector |

How to read it:

- On the 27B, B gets 1 more of the 62 right, but A's Brier is better (0.0136 vs 0.0179). CV after calibration is the same. "Measured wording gives more honest probabilities" is one observation on 62 items and remains a hypothesis. "RAG is more accurate" is also by 1 item
- On the 4B, B is ahead on all of top-1, Brier, and precision and recall after calibration. The smaller the model, the more examples help
- Removing the condition that favored RAG (leave-one-out) moved the 27B's Brier from 0.0092 to 0.0179. How the search pool is cut moves the result, so a bench must always state it
- Both arms can be calibrated with the same tool. RAG is not a mechanism outside shikii but one way of asking, putting examples into `state.template`, and can be measured on the same savings. So A and B are not opposed; they can be lined up on the same measure. But B needs the caller to build and pass an input called `examples`, and `tune` does not build inputs, so the current `tune` variants (wording and arrangement only) cannot express it. To make it a tune candidate, tune needs a step that "pulls examples from the savings"
- Cost: B's body is 5 examples longer. With the 27B readout, one question goes from 0.58 s to about 1.1 s

## Three arms on the same base (Qwen3-4B, 2026-10-18)

The ternary 27B cannot be trained, so the base for lining up the three arms is the ordinary model `Qwen/Qwen3-4B` (the lineage Kev-4B is based on). A and B used the GGUF of the same model (Q8_0) with upstream llama.cpp + `shikii-readout`; C trained and scored the HF version (bf16) with mlx-lm. The steps are in `scripts/bench/lora/run.sh` (making folds `make_folds.py`, scoring `score.py`, measures `report.py`).

- C's training: 62 items in 5 folds, in each fold 200 steps on the remaining ~50 (batch 4, LoRA on 8 layers, lr 1e-4). A few minutes per fold
- C's scoring: no generation; read the log probabilities of the prompt's next token at the first token of each of the 5 ids and softmax them (the same idea as `shikii-readout`). Each row is answered by "an adapter that has not seen it", so all 62 are out-of-sample
- Control: score the same prompt on the base without LoRA (to separate the differences between C's prompt and A's wording and arrangement)

| Arm | top-1 | Brier | prec / rec at act 0.75 | Notes |
|---|---|---|---|---|
| A: v2 wording as is (readout) | 0.774 | 0.2216 | 0.774 / 1.000 (no line can be drawn) | Everything sticks at 0.75 or above |
| **A: wording `tune` chose on this base** (shorter-question, readout) | **0.855** | 0.1394 | holdout 0.855, p=0.031 | Out of 7 candidates. Not adopted (for evaluation) |
| B: 5 similar examples (readout) | **0.855** | **0.1199** | after calibration CV 0.931 / 0.964 | Examples from outside the fold |
| Control: plain prompt, no LoRA (mlx) | 0.903 | 0.0967 | 0.903 / 1.000 (raw) | 13 points above the readout arrangement |
| **C: LoRA ~50 items × 5 folds (mlx)** | **0.952** | **0.0375** | 0.952 / 1.000 (raw, uncalibrated) | Everything at 0.75 or above |

How to read it:

- **On this task and this base, the hypothesis is not supported.** LoRA on about 50 items is 10 points above the measured wording (0.855), with a quarter of the Brier. We cannot say it "reaches it with far fewer examples than fine-tuning". LoRA's number of examples (50) is already small
- B, which adds similar examples, ties the measured wording on top-1 and is ahead on Brier. On a small base, showing examples helps more than measuring wording
- The gap between the control (0.903) and A's v2 (0.774) is not the wording but the **arrangement** (`shikii-readout`'s System One ordering versus plain text). 13 points with the same weights. On the 27B, arrangement moved it by 5 points (model-comparison.md). The readout arrangement is meant for decision models and may not suit a bare 4B. To make arm A fair, the arrangement also has to be a `tune` candidate (now only the template and wording)
- Fairness note: A and B are Q8 GGUF, C and the control are bf16. The way probabilities are read also differs (readout is System One's interface, C is the softmax of the first token). The precision of C and the control is uncalibrated and everything sticks at 0.75 or above, so it cannot be read as "act precision" (it equals top-1)
- On the 27B (table above), A and B are nearly the same, and LoRA could not be measured (ternary cannot be trained)

### After making arrangement a tune candidate (same day)

We made the arrangement (`layout: plain`) writable as a spec candidate and measured the same 9 candidates with `tune`. On Qwen3-4B, **plain won with 0.903 (Brier 0.0959)**, holdout 0.887 vs 0.774, 8 wins 1 loss, p=0.020. These match the mlx plain-text control (0.903 / 0.0967), confirming that the 13-point gap was the arrangement. Arm A, as "the way of asking tune chose on this base", is now **0.903**, above B (0.855) and 5 points short of C's LoRA (0.952).

| Arm (Qwen3-4B, updated) | top-1 | Brier | prec / rec at act 0.75 | After calibration CV prec / rec |
|---|---|---|---|---|
| A: way of asking tune chose (plain arrangement) | 0.903 | 0.0959 | 0.903 / 1.000 | (tune does not output CV calibration) |
| B: 5 similar examples, state-last arrangement | 0.887 | 0.1136 | 0.883 / 0.964 | 0.931 / 0.964 |
| **B: 5 similar examples, plain arrangement** | **0.952** | 0.0482 | 0.952 / 1.000 | (measured as a tune candidate) |
| **C: LoRA ~50 items** | **0.952** | **0.0375** | 0.952 / 1.000 | 0.966 / 0.966 (vector) |
| Control: plain prompt, no LoRA | 0.903 | 0.0967 | 0.903 / 1.000 | 0.934 / 1.000 |

B's state-last going from 0.855 to 0.887 is because the way examples are written (`message: … → truth`) changed when `rag-rows.py` was made to work for any spec. These are numbers measured again under the same conditions.

Conclusion: **with the arrangement lined up, B with similar examples ties LoRA on top-1 (0.952)**. LoRA has the better Brier (0.0375 vs 0.0482). A, with measured wording alone, is 0.903, 5 points below. All a person did was write 2 lines of candidates; a machine did the choosing. The after-calibration numbers for C and the control were produced by making `calibrate --labeled` read rows with raw (choice / score as `{id: p}`) (without calling the model). On the 27B, plain gives 0.984 and plain + a shorter question 1.000 (current 0.968), so no harm, but holdout was 1 win 0 losses (p=0.5), which did not pass the gate, so the current one stays.

Pitfall (same day): if you measure without restarting the converter with the new build, the old converter does not know `layout` and ignores it, and plain gets **the same numbers** as current. The first 27B run was invalid for that reason (deleted). Be suspicious when identical numbers line up. Pattern: [plain-layout-for-small-chat-models](../wiki/patterns/plain-layout-for-small-chat-models.md).

## The same three arms on a yes/no task (same-issue, 32 pairs, Japanese, Qwen3-4B, 2026-10-18)

Duplicate detection for bug reports. The 8 boundary pairs have weight 0.5 (calibrate is weighted; `report.py`'s raw numbers are unweighted). act is 0.70.

| Arm | top-1 | Brier | After calibration | Notes |
|---|---|---|---|---|
| A: current wording (readout, state-last) | 0.911 | 0.0786 | platt CV prec 0.952 / Brier 0.0488 | plain arrangement also 0.911 (no arrangement difference) |
| B: 5 similar examples (readout) | 0.929 | 0.0686 | platt CV 0.952 / 0.0546 | plain also 0.929, Brier 0.0500 |
| Control: plain prompt, no LoRA (mlx) | **0.969** | **0.0318** | identity, weighted Brier 0.0183 | |
| C: LoRA ~26 items × 5 folds (mlx) | 0.938 | 0.0513 | identity, weighted Brier 0.0309 | **Worse than the base** |

How to read it:

- For yes/no, the arrangement (letters or yes/no) makes no difference. That choice moved by 13 points was because mapping the 5 ids to letters had a quirk; with 2 options, the mapping quirk is small
- **LoRA on about 26 items fell below the base** (0.969 → 0.938, Brier 0.0318 → 0.0513). An example of moving weights on small data landing on the broken side. Together with the win on choice (about 50 items), "LoRA wins" depends on the number of examples and the type
- The 6-point gap between A and B through readout (0.911 / 0.929) and mlx's plain prompt (0.969) cannot be explained by the arrangement (plain gives the same). The remaining differences are Q8 vs bf16, details of the converter's text ("Answer with the option id only" vs "Answer yes or no only"), and how tokens are read. Unexplained. This too is the kind of difference (wording) that can be a `tune` candidate, so next we make the converter's noul text a candidate and measure it
- B with similar examples is better than a bare single question on yes/no too (+1.8 points, better Brier). One of the few things that helps across types

What to do next: (1) make the noul converter's text a tune candidate and split the 6-point gap, (2) a model-swap bench (apply the same spec and truths to Kev-4B / Qwen3-4B / 27B, and show the line comes back with 0 human truths; LoRA would be retrained for each model), (3) make pulling similar examples a dimension of tune candidates.

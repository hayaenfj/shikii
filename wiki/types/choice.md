# How to ask a choice (pick one option)

## Basics

- Give every option **both an id and a description**. With an id alone, the model picks by how the word sounds (the llama.cpp blog reported the same with Julia-1)
- Describe both "what belongs" and "what does not belong". The vaguer an option's boundary (other, misc, unknown), the more you should spell out what does not belong → [other-is-not-a-request](../patterns/other-is-not-a-request.md)
- **State how short inputs are handled in the question**, for example that short inputs ("直して", "why?") still count as requests. Otherwise short inputs drift to other
- Measure bias from option order with `shikii bias` before fixing it. For a trained decision model (Kev-4B) it was negligible (0 flips in 100 runs)

## Reading the errors

- If the confusion table shows errors **piling up on one option**, it is a wording problem, not a calibration one. Tighten that option's description
- If the errors are spread out, the question is hard or the options are cut badly. Try decomposition: write a chain of yes/no questions in the spec's `steps:` (each option is exactly one leaf; middle steps settle one side, the last step settles both). All steps are asked in one go, and the product of the branch probabilities becomes the distribution over the original options. It can also be written as a `tune` candidate (`variants[].steps`), and the proposer can be asked to make one

## Evidence

- [prompt-intent v1 → v2](https://github.com/hayaenfj/shikii/blob/main/wiki/specs/prompt-intent.md): making the descriptions concrete and limiting other took top-1 from 0.682 to 0.903 (Kev-4B, 62 items)
- Decomposition is not a cure-all: a model that is good at multiple choice does not need it. Splitting prompt-intent into 4 yes/no steps on Ternary-Bonsai-27B (readout) gave top-1 0.790 (multiple choice: 0.968). A version whose first step was written as a list of negatives gave 0.306 → [noul-define-yes-before-contrast](../patterns/noul-define-yes-before-contrast.md). We expect decomposition to help models that break down on multiple choice (small models, many options), but this is not measured yet ([run](https://github.com/hayaenfj/shikii/blob/main/wiki/runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-2.md))

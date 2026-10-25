# How to ask a noul (yes / no)

## Basics

- List the conditions for "yes" concretely, and **also write the confusing cases that should be "no"** → [same-vocabulary-is-no](../patterns/same-vocabulary-is-no.md)
- Order: **the definition of yes first, then the contrast after it with "as opposed to …"**. A sentence that ends with a list of negatives weakens the yes side (0.42 → 0.97 on an ordinary chat model) → [noul-define-yes-before-contrast](../patterns/noul-define-yes-before-contrast.md)
- Each step of a decomposition (choice's `steps:`) is also a noul. If the first step (such as "is it a request?") is weak, the products of all later steps collapse, so write each step in the order above
- For a question comparing two things, lay them out in the text under named headings (`Report 1:` / `Report 2:`). If it is unclear which is which, the probability splits
- Borderline examples (a different bug on the same screen, a different case of the same kind) get the truth `false` with weight 0.5, because they are examples that must not be treated as "the same" automatically

## Calibration

- Trained decision models (Kev-4B, OpenJev) often do fine with raw probabilities (identity). Probabilities read out of an ordinary chat model lean to the extremes, so Platt helps (0.049 → 0.019, for example) → [calibrated-models-may-not-need-platt](../patterns/calibrated-models-may-not-need-platt.md)
- If the lowest positive is below the highest negative (an inversion), calibration cannot fix it. Change the question

## Evidence

- [same-issue / same-inquiry](https://github.com/hayaenfj/shikii/blob/main/wiki/specs/same-issue.md): positives and negatives separated perfectly in all 10 combinations of 5 endpoints × 2 sets (2026-10-17)

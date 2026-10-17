# tune prompt-intent on ternary-bonsai-27b-q2g64+state-last (2026-10-17)

spec: [prompt-intent](../specs/prompt-intent.md) · model page: [ternary-bonsai-27b-q2g64](../models/ternary-bonsai-27b-q2g64.md) · type: [choice](../types/choice.md) · truths: 62 · hash before: df1367887c4970a7

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.968 | 0.0136 | 1.000 | 0.983 | (as is) |
| shorter-question | 0.968 | 0.0121 | 1.000 | 0.983 | question: The state is a message a user sent to a coding assistant. Wh… |
| other-tight | 0.984 | 0.0157 | 1.000 | 0.967 | other: Not a request at all - greetings, thanks… |
| tighten-review-vs-operation | 1.000 | 0.0126 | 1.000 | 0.952 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| clarify-review-purpose | 0.984 | 0.0095 | 1.000 | 0.967 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| add-review-example | 0.984 | 0.0085 | 1.000 | 0.984 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |

holdout (pick on k-1, measure on 1): 0.968 vs keeping current 0.968

**Decision:** kept current (holdout does not beat it)

**Lesson:** tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it

Proposer's rationales:
- tighten-review-vs-operation: Explicitly include "Does this look correct?" in the review option to prevent the model from misclassifying correctness c…
- clarify-review-purpose: Use the word "verify correctness" to directly match the phrasing of the misclassified examples and improve boundary clar…
- add-review-example: Adding a concrete example of a correctness check in the review option helps the model distinguish evaluation requests fr…

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

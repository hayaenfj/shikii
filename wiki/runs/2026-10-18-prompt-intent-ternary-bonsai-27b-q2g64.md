# tune prompt-intent on ternary-bonsai-27b-q2g64+state-last (2026-10-18)

spec: [prompt-intent](../specs/prompt-intent.md) · model page: [ternary-bonsai-27b-q2g64](../models/ternary-bonsai-27b-q2g64.md) · type: [choice](../types/choice.md) · truths: 62 · hash before: df1367887c4970a7

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.968 | 0.0136 | 1.000 | 0.983 | (as is) |
| shorter-question | 0.968 | 0.0121 | 1.000 | 0.983 | question: The state is a message a user sent to a coding assistant. Wh… |
| other-tight | 0.984 | 0.0157 | 1.000 | 0.967 | other: Not a request at all - greetings, thanks… |
| tighten-review-vs-operation | 1.000 | 0.0126 | 1.000 | 0.952 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| clarify-review-purpose | 0.984 | 0.0095 | 1.000 | 0.967 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| add-review-example | 0.984 | 0.0085 | 1.000 | 0.984 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| two-step | 0.306 | 0.3879 | 0.345 | 0.526 | decomposed into 4 yes/no steps: Is this message asking the assistant to do or answ… |

holdout (pick on k-1, measure on 1): 0.968 vs keeping current 0.968

**Decision:** kept current (holdout 0.968 vs 0.968, 0 better / 0 worse, p=1.000: not enough evidence)

**Lesson:** tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout (0.968 vs 0.968; 0 better, 0 worse, p=1.000) does not rule out chance on 62 truths; more truths will settle it

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

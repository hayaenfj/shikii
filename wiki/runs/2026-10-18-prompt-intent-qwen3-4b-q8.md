# tune prompt-intent on qwen3-4b-q8+state-last (2026-10-18)

spec: [prompt-intent](../specs/prompt-intent.md) · model page: [qwen3-4b-q8](../models/qwen3-4b-q8.md) · type: [choice](../types/choice.md) · truths: 62 · hash before: df1367887c4970a7

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.774 | 0.2214 | 0.774 | 1.000 |  |
| shorter-question | 0.855 | 0.1394 | 0.855 | 1.000 | question: The state is a message a user sent to a coding assistant. Wh… |
| other-tight | 0.774 | 0.2186 | 0.783 | 0.979 | other: Not a request at all - greetings, thanks… |
| tighten-review-vs-operation | 0.774 | 0.2099 | 0.783 | 0.979 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| clarify-review-purpose | 0.790 | 0.2005 | 0.803 | 1.000 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| add-review-example | 0.774 | 0.2190 | 0.774 | 1.000 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| two-step | 0.323 | 0.6236 | 0.322 | 0.950 | decomposed into 4 yes/no steps: Is this message a request (something the assistant… |

holdout (pick on k-1, measure on 1): 0.855 vs keeping current 0.774

**Decision:** shorter-question may be adopted once a person confirms the meaning is unchanged (page, or --write)

**Lesson:** shorter-question: question: The state is a message a user sent to a coding assistant. Wh… → top-1 0.774 → 0.855, brier 0.2214 → 0.1394, holdout 0.855 > current 0.774 (5 better / 0 worse, p=0.031) on 62 truths

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

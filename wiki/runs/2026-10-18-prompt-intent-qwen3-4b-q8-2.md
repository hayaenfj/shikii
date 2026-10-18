# tune prompt-intent on qwen3-4b-q8+state-last (2026-10-18)

spec: [prompt-intent](../specs/prompt-intent.md) · model page: [qwen3-4b-q8](../models/qwen3-4b-q8.md) · type: [choice](../types/choice.md) · truths: 62 · hash before: df1367887c4970a7

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.774 | 0.2216 | 0.774 | 1.000 |  |
| plain | 0.903 | 0.0959 | 0.903 | 1.000 | layout: plain |
| plain-shorter | 0.887 | 0.0975 | 0.902 | 1.000 | question: The input is a message a user sent to a coding assistant. Wh…; layout: plain |
| shorter-question | 0.855 | 0.1394 | 0.855 | 1.000 | question: The state is a message a user sent to a coding assistant. Wh… |
| other-tight | 0.774 | 0.2186 | 0.783 | 0.979 | other: Not a request at all - greetings, thanks… |
| tighten-review-vs-operation | 0.774 | 0.2099 | 0.783 | 0.979 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| clarify-review-purpose | 0.790 | 0.2005 | 0.803 | 1.000 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| add-review-example | 0.774 | 0.2190 | 0.774 | 1.000 | question: The state is a message a user sent to a coding assistant. Wh…; review: Evaluate changes or code that already ex… |
| two-step | 0.323 | 0.6236 | 0.322 | 0.950 | decomposed into 4 yes/no steps: Is this message a request (something the assistant… |

holdout (pick on k-1, measure on 1): 0.887 vs keeping current 0.774

**Decision:** plain may be adopted once a person confirms the meaning is unchanged (page, or --write)

**Lesson:** plain: layout: plain → top-1 0.774 → 0.903, brier 0.2216 → 0.0959, holdout 0.887 > current 0.774 (8 better / 1 worse, p=0.020) on 62 truths

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

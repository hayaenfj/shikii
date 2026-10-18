# tune prompt-intent-rag on qwen3-4b-q8+state-last (2026-10-18)

spec: [prompt-intent-rag](../specs/prompt-intent-rag.md) · model page: [qwen3-4b-q8](../models/qwen3-4b-q8.md) · type: [choice](../types/choice.md) · truths: 62 · hash before: 59b354c3ce2d673f

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.887 | 0.1136 | 0.883 | 0.964 |  |
| plain | 0.952 | 0.0482 | 0.952 | 1.000 | layout: plain |

holdout (pick on k-1, measure on 1): 0.952 vs keeping current 0.887

**Decision:** kept current (holdout 0.952 vs 0.887, 5 better / 1 worse, p=0.109: not enough evidence)

**Lesson:** plain wins on the full bank (0.952 vs 0.887) but the holdout (0.952 vs 0.887; 5 better, 1 worse, p=0.109) does not rule out chance on 62 truths; more truths will settle it

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

# tune same-issue-rag on qwen3-4b-q8+state-last (2026-10-18)

spec: [same-issue-rag](../specs/same-issue-rag.md) · model page: [qwen3-4b-q8](../models/qwen3-4b-q8.md) · type: [noul](../types/noul.md) · truths: 32 · hash before: 75af9097a1d9c00d

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.929 | 0.0686 | 0.927 | 0.981 |  |
| plain | 0.929 | 0.0500 | 0.945 | 1.000 | layout: plain |

holdout (pick on k-1, measure on 1): 0.893 vs keeping current 0.929

**Decision:** kept current (holdout 0.893 vs 0.929, 0 better / 2 worse, p=1.000: not enough evidence)

**Lesson:** plain wins on the full bank (0.929 vs 0.929) but the holdout (0.893 vs 0.929; 0 better, 2 worse, p=1.000) does not rule out chance on 32 truths; more truths will settle it

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

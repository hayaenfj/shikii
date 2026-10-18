# tune same-issue on qwen3-4b-q8+state-last (2026-10-18)

spec: [same-issue](../specs/same-issue.md) · model page: [qwen3-4b-q8](../models/qwen3-4b-q8.md) · type: [noul](../types/noul.md) · truths: 32 · hash before: 39e5d9072ae0c8c2

| candidate | top-1 | brier | prec@act | rec@act | what changes |
|---|---|---|---|---|---|
| current | 0.911 | 0.0786 | 0.911 | 1.000 |  |
| plain | 0.911 | 0.0801 | 0.927 | 1.000 | layout: plain |

holdout (pick on k-1, measure on 1): 0.911 vs keeping current 0.911

**Decision:** kept current (nothing beats it; ties keep it)

**Lesson:** none of 1 candidates beat the current wording (top-1 0.911) on 32 truths

<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->

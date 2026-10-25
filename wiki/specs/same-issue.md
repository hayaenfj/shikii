# same-issue (duplicate detection for bug reports, yes/no)

## Rules
<!-- written by people -->
- Keep the sentence saying "only the same screen, feature or technology is no"
- Borderline examples (a different bug on the same screen) stay false with weight 0.5. They must not be treated as "the same" automatically

## Wording history

- v1 (`decisions/same-issue.yaml`, 2026-10-17): positives and negatives separated perfectly on all 5 endpoints. Brier: OpenJev 0.007, Kev-4B 0.038 → platt 0.035, Ternary-Bonsai-27B 0.048 → platt 0.024

Patterns in use: [same-vocabulary-is-no](../patterns/same-vocabulary-is-no.md)

## Runs
- [2026-10-18 qwen3-4b-q8](../runs/2026-10-18-same-issue-qwen3-4b-q8.md) — kept current (nothing beats it; ties keep it). Lesson: none of 1 candidates beat the current wording (top-1 0.911) on 32 truths

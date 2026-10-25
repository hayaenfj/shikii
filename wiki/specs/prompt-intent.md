# prompt-intent (the kind of request to an AI, 5 choices)

## Rules
<!-- things every wording of this decision must keep; written by people -->
- Do not change the option ids (change_request / question / review / operation / other). The hook and the calibration rely on these ids
- Keep "short requests are requests too" ("fix it", "直して") in the question
- Decomposition (steps) loses on the 27B readout (0.790 vs 0.968, 2026-10-18). If proposed, aim it at small models. Write the positive definition first in the first yes/no step (a version ending with a list of negatives scored 0.306)
- Inputs arrive in both Japanese and English. Include examples in both
- The context step (`state.context`, 2026-10-18) adds the assistant's previous message `previous`. It only takes effect when the caller (the hook) passes it. The context step's line is drawn separately from the minimal question's. Adding context does not always raise confidence ("and the README?" split). When measuring candidate wordings, tune measures them without the context

## Wording history

- v1 (`decisions/prompt-intent.yaml`): short descriptions. Top-1 0.682 on Kev-4B; 19 of 21 errors went to other
- v2 (`decisions/prompt-intent-v2.yaml`): concrete descriptions, other limited to "not a request". Kev-4B 0.903 to 0.919, Ternary-Bonsai-27B 0.968, OpenJev 0.984

Patterns in use: [other-is-not-a-request](../patterns/other-is-not-a-request.md), [state-last-for-recurrent-readout](../patterns/state-last-for-recurrent-readout.md) (with readout)

## Runs
- [2026-10-17 ternary-bonsai-27b-q2g64](../runs/2026-10-17-prompt-intent-ternary-bonsai-27b-q2g64.md) — kept current (holdout does not beat it). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it
- [2026-10-17 ternary-bonsai-27b-q2g64](../runs/2026-10-17-prompt-intent-ternary-bonsai-27b-q2g64-2.md) — kept current (holdout does not beat it). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it
- [2026-10-18 ternary-bonsai-27b-q2g64](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64.md) — kept current (holdout 0.968 vs 0.968, 0 better / 0 worse, p=1.000: not enough evidence). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout (0.968 vs 0.968; 0 better, 0 worse, p=1.000) does not rule out chance on 62 truths; more truths will settle it
- [2026-10-18 ternary-bonsai-27b-q2g64](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-2.md) — kept current (holdout 0.968 vs 0.968, 0 better / 0 worse, p=1.000: not enough evidence). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout (0.968 vs 0.968; 0 better, 0 worse, p=1.000) does not rule out chance on 62 truths; more truths will settle it
- [2026-10-18 qwen3-4b-q8](../runs/2026-10-18-prompt-intent-qwen3-4b-q8.md) — shorter-question may be adopted once a person confirms the meaning is unchanged (page, or --write). Lesson: shorter-question: question: The state is a message a user sent to a coding assistant. Wh… → top-1 0.774 → 0.855, brier 0.2214 → 0.1394, holdout 0.855 > current 0.774 (5 better / 0 worse, p=0.031) on 62 truths
- [2026-10-18 qwen3-4b-q8](../runs/2026-10-18-prompt-intent-qwen3-4b-q8-2.md) — plain may be adopted once a person confirms the meaning is unchanged (page, or --write). Lesson: plain: layout: plain → top-1 0.774 → 0.903, brier 0.2216 → 0.0959, holdout 0.887 > current 0.774 (8 better / 1 worse, p=0.020) on 62 truths
- [2026-10-18 ternary-bonsai-27b-q2g64](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-3.md) — kept current (holdout 0.984 vs 0.968, 1 better / 0 worse, p=0.500: not enough evidence). Lesson: plain-shorter wins on the full bank (1.000 vs 0.968) but the holdout (0.984 vs 0.968; 1 better, 0 worse, p=0.500) does not rule out chance on 62 truths; more truths will settle it

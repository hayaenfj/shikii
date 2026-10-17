# prompt-intent（コーディングエージェントへの依頼の種類、5 択）

## Rules
<!-- この判定のどの文言も守ること。人が書く -->
- 選択肢の id（change_request / question / review / operation / other）は変えない。hook と較正がこの id を見ている
- 「短い依頼も依頼」（"fix it"、「直して」）は質問文に残す
- 日本語と英語の両方の入力が来る。例は両方入れる

## 文言の履歴

- v1（`decisions/prompt-intent.yaml`）: 説明が短い。Kev-4B で top-1 0.682、誤り 21 件中 19 件が other
- v2（`decisions/prompt-intent-v2.yaml`）: 説明を具体化、other を「依頼ではないもの」に限定。Kev-4B 0.903〜0.919、Ternary-Bonsai-27B 0.968、OpenJev 0.984

使っているパターン: [other-is-not-a-request](../patterns/other-is-not-a-request.md)、[state-last-for-recurrent-readout](../patterns/state-last-for-recurrent-readout.md)（readout の時）

## Runs
- [2026-10-17 ternary-bonsai-27b-q2g64](../runs/2026-10-17-prompt-intent-ternary-bonsai-27b-q2g64.md) — kept current (holdout does not beat it). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it
- [2026-10-17 ternary-bonsai-27b-q2g64](../runs/2026-10-17-prompt-intent-ternary-bonsai-27b-q2g64-2.md) — kept current (holdout does not beat it). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it

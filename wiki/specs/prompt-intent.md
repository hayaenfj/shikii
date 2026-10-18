# prompt-intent（コーディングエージェントへの依頼の種類、5 択）

## Rules
<!-- この判定のどの文言も守ること。人が書く -->
- 選択肢の id（change_request / question / review / operation / other）は変えない。hook と較正がこの id を見ている
- 「短い依頼も依頼」（"fix it"、「直して」）は質問文に残す
- 分解（steps）は 27B readout では負ける（0.790 vs 0.968、2026-10-18）。提案するなら小さいモデル向け。はい/いいえ の 1 段目は肯定の定義を先に書く（否定の列挙で終わる版は 0.306）
- 日本語と英語の両方の入力が来る。例は両方入れる
- 文脈の段（`state.context`、2026-10-18）は直前のアシスタントの発言 `previous` を足す。呼ぶ側（hook）が渡した時だけ効く。文脈の段の線は最小の質問とは別に引かれる。文脈を足すと確信が上がるとは限らない（「and the README?」は割れた）。候補の文言を測る時は tune が文脈を外して測る

## 文言の履歴

- v1（`decisions/prompt-intent.yaml`）: 説明が短い。Kev-4B で top-1 0.682、誤り 21 件中 19 件が other
- v2（`decisions/prompt-intent-v2.yaml`）: 説明を具体化、other を「依頼ではないもの」に限定。Kev-4B 0.903〜0.919、Ternary-Bonsai-27B 0.968、OpenJev 0.984

使っているパターン: [other-is-not-a-request](../patterns/other-is-not-a-request.md)、[state-last-for-recurrent-readout](../patterns/state-last-for-recurrent-readout.md)（readout の時）

## Runs
- [2026-10-17 ternary-bonsai-27b-q2g64](../runs/2026-10-17-prompt-intent-ternary-bonsai-27b-q2g64.md) — kept current (holdout does not beat it). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it
- [2026-10-17 ternary-bonsai-27b-q2g64](../runs/2026-10-17-prompt-intent-ternary-bonsai-27b-q2g64-2.md) — kept current (holdout does not beat it). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout 0.968 does not beat keeping current 0.968: within noise on 62 truths; more truths will settle it
- [2026-10-18 ternary-bonsai-27b-q2g64](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64.md) — kept current (holdout 0.968 vs 0.968, 0 better / 0 worse, p=1.000: not enough evidence). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout (0.968 vs 0.968; 0 better, 0 worse, p=1.000) does not rule out chance on 62 truths; more truths will settle it
- [2026-10-18 ternary-bonsai-27b-q2g64](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-2.md) — kept current (holdout 0.968 vs 0.968, 0 better / 0 worse, p=1.000: not enough evidence). Lesson: tighten-review-vs-operation wins on the full bank (1.000 vs 0.968) but the holdout (0.968 vs 0.968; 0 better, 0 worse, p=1.000) does not rule out chance on 62 truths; more truths will settle it
- [2026-10-18 qwen3-4b-q8](../runs/2026-10-18-prompt-intent-qwen3-4b-q8.md) — shorter-question may be adopted once a person confirms the meaning is unchanged (page, or --write). Lesson: shorter-question: question: The state is a message a user sent to a coding assistant. Wh… → top-1 0.774 → 0.855, brier 0.2214 → 0.1394, holdout 0.855 > current 0.774 (5 better / 0 worse, p=0.031) on 62 truths
- [2026-10-18 qwen3-4b-q8](../runs/2026-10-18-prompt-intent-qwen3-4b-q8-2.md) — plain may be adopted once a person confirms the meaning is unchanged (page, or --write). Lesson: plain: layout: plain → top-1 0.774 → 0.903, brier 0.2216 → 0.0959, holdout 0.887 > current 0.774 (8 better / 1 worse, p=0.020) on 62 truths
- [2026-10-18 ternary-bonsai-27b-q2g64](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-3.md) — kept current (holdout 0.984 vs 0.968, 1 better / 0 worse, p=0.500: not enough evidence). Lesson: plain-shorter wins on the full bank (1.000 vs 0.968) but the holdout (0.984 vs 0.968; 1 better, 0 worse, p=0.500) does not rule out chance on 62 truths; more truths will settle it

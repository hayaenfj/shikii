# same-issue（不具合報告の重複判定、はい/いいえ）

## Rules
<!-- 人が書く -->
- 「同じ画面・機能・技術というだけでは no」の一文は残す
- 境界の例（同じ画面の別の不具合）は false・重み 0.5 のまま。自動で「同じ」と扱ってはいけない

## 文言の履歴

- v1（`decisions/same-issue.yaml`、2026-10-17）: 5 つの口全部で正負が完全に分かれた。Brier は OpenJev 0.007、Kev-4B 0.038 → platt 0.035、Ternary-Bonsai-27B 0.048 → platt 0.024

使っているパターン: [same-vocabulary-is-no](../patterns/same-vocabulary-is-no.md)

## Runs

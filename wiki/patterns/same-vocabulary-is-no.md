# 「語彙が似ているだけなら no」を質問文に書く（同一判定の noul）

status: tentative

## いつ効くか

- 型: noul。2 つの記録が「同じもの」かを聞く時（重複の不具合報告、同じ案件の問い合わせ、同一の案件）
- モデル: Kev-4B、OpenJev、Bonsai 系で確認

## 書き方

「はい」の条件（同じ原因、一方が他方の続き）を書いた上で、**「いいえ」にすべき紛らわしい場合**を一文足す:

> Answer "no" if they merely concern the same screen, feature or technology.

境界の例（同じ画面の別の不具合、同じ種類の別案件）を貯金に重み 0.5 の false として入れると、線がその側に引かれる。

## 証拠

- same-issue / same-inquiry（各 32 組、境界 8 組）: 5 つの口 × 2 セットの 10 通り全部で正負が完全に分かれた（2026-10-17）。この一文が無い版との比較はまだ無いので tentative

## 反証

なし。ただし比較の run が無い

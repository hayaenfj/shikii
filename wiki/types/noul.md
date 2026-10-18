# noul（はい / いいえ）の聞き方

## 基本

- 「はい」の条件を具体的に列挙し、**「いいえ」にすべき紛らわしい場合も書く** → [same-vocabulary-is-no](../patterns/same-vocabulary-is-no.md)
- 書く順は **はい の定義を先に、対比は "as opposed to …" で後ろに**。否定の列挙で終わる文は はい の側が弱くなる（普通のチャット用モデルで 0.42 → 0.97）→ [noul-define-yes-before-contrast](../patterns/noul-define-yes-before-contrast.md)
- 分解（choice の `steps:`）の各段も noul。1 段目（依頼かどうか等）が弱いと後ろの段の積が全部崩れるので、段ごとに上の順で書く
- 2 つのものを比べる質問は、本文に名前付きの見出し（`Report 1:` / `Report 2:`）を付けて並べる。どちらがどちらか分からないと確率が割れる
- 境界の例（同じ画面の別の不具合、同じ種類の別案件）は正解を `false` にして重み 0.5。自動で「同じ」と扱ってはいけない例だから

## 較正

- 学習済みの決定モデル（Kev-4B、OpenJev）は生の確率で足りることが多い（identity）。普通のチャット用モデルを読み出した確率は端に寄るので Platt が効く（0.049 → 0.019 など）→ [calibrated-models-may-not-need-platt](../patterns/calibrated-models-may-not-need-platt.md)
- 正例の下限が負例の上限より下（逆転）なら較正では直らない。質問文を変える

## 証拠

- [same-issue / same-inquiry](https://github.com/hayaenfj/shikii/blob/main/wiki/specs/same-issue.md): 5 つの口 × 2 セットの 10 通り全部で正負が完全に分かれた（2026-10-17）

# choice（選択肢から 1 つ選ぶ）の聞き方

## 基本

- 選択肢には **id と説明**の両方を付ける。id だけだとモデルは語感で選ぶ（llama.cpp のブログも Julia-1 で同じ報告）
- 説明は「何が入るか」と「何が入らないか」を両方書く。境界が曖昧な選択肢（other、その他、不明）ほど「入らないもの」を明示する → [other-is-not-a-request](../patterns/other-is-not-a-request.md)
- 短い入力（「直して」「why?」）も依頼として扱う、のように**短い入力の扱いを質問文に書く**。書かないと短い入力が other に逃げる
- 選択肢の順序による偏りは `shikii bias` で測ってから直す。学習済みの決定モデル（Kev-4B）では無視できた（100 回で反転 0）

## 誤りの読み方

- 混同表で誤りが**1 つの選択肢に集まっている**なら、較正ではなく文言の問題。その選択肢の説明を締める
- 誤りが散っているなら、質問が難しいか、選択肢の切り方が悪い。分解（2 つの はい/いいえ に割る）を試す

## 証拠

- [prompt-intent の v1 → v2](../specs/prompt-intent.md): 説明を具体化して other を限定し、top-1 0.682 → 0.903（Kev-4B、62 件）
- 分解は万能ではない: 多択が上手なモデルには要らない。Ternary-Bonsai-27B（readout）で prompt-intent を 4 段の はい/いいえ に割ると top-1 0.790（多択は 0.968）。1 段目を否定の列挙で書いた版は 0.306 → [noul-define-yes-before-contrast](../patterns/noul-define-yes-before-contrast.md)。分解が効くのは多択で崩れるモデル（小さいモデル、選択肢が多い時）と見込むが、まだ測っていない（[run](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-2.md)）

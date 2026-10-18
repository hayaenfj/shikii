# other を「依頼ではないもの」と明示する

status: tentative  <!-- 2026-10-18: 効いたのは Kev-4B の 1 組だけ。別のモデルか別の正解の組で効いたら confirmed に(人が変える) -->

## いつ効くか

- 型: choice。選択肢に「その他」「不明」「どれでもない」のような受け皿がある時
- モデル: Kev-4B で確認。他の決定モデルでも同じ理屈（受け皿は説明が曖昧なほど吸い込む）

## 書き方

受け皿の説明を「上のどれでもない」ではなく、**何がそこに入るかを具体的に**書く。例:

> other: Not a request at all - greetings, thanks, acknowledgements ("ok", "了解"), small talk, or ending the session.

同時に、他の選択肢の説明に短い入力の例を入れる（"fix the typo"、「直して」）。短い入力が受け皿に逃げるのを防ぐ。

## 証拠

- prompt-intent v1 → v2（Kev-4B、62 件）: 誤り 21 件のうち 19 件が other に落ちていた → 説明を具体化して top-1 0.682 → 0.903、other への誤り 0。[specs/prompt-intent](https://github.com/hayaenfj/shikii/blob/main/wiki/specs/prompt-intent.md)
- prompt-intent v2 で other をさらに締める候補（other-tight）: Ternary-Bonsai-27B で 0.968 → 0.984 だが holdout 0.952 で不採用（62 件では有意でない）。効き方は頭打ちに近い

## 反証

なし（2026-10-17 時点）

## 人のメモ

受け皿を締めすぎると、本当に受け皿に入るべき入力を他の選択肢に押し出す。混同表で other 行だけでなく other 列も見る。

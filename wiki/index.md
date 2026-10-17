# 聞き方の wiki

決定モデルに**どう聞くと通るか**の知識。1 人の経験ではなく、証拠付きの共有財。`shikii tune --propose` はここを読んで候補を作り、`runs/` に結果を書き戻す。

| 場所 | 何を書くか | 誰が書くか |
|---|---|---|
| [`types/`](types/) | 質問の型（choice / noul / score）ごとの聞き方の基本 | 人 |
| [`models/`](models/) | モデルの系統ごとの癖（較正の要否、本文の置き場所、言語） | 人（tune の run を見て） |
| [`patterns/`](patterns/) | 1 パターン 1 ページ。いつ効くか・書き方・証拠・反証・状態 | 人。状態（confirmed / tentative / rejected）を変えるのは人だけ |
| [`specs/`](specs/) | 判定ごとの決まり（Rules、人が書く）と run の一覧（tune が足す） | 両方 |
| [`runs/`](runs/) | 証拠。候補・数字・holdout・採否・一行の学び | tune |

決まり:

- パターンは **2 つ以上の独立した観察（別のモデルか別の正解の組での評価、または tune の run）で効いた**ものだけ `status: confirmed`。1 つなら `tentative`。状態を変えるのは人だけで、証拠を付ける
- 機械が書くのは runs/ と specs/ の Runs の節だけ。人の文は上書きしない
- 例文を載せるのは `pii: false` の判定だけ。本文に個人情報が入り得る判定は数字だけ

入口: [choice の聞き方](types/choice.md) · [noul の聞き方](types/noul.md) · [score の聞き方](types/score.md) · [パターン一覧](patterns/)

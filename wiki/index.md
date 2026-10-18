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

## パターンのページの形（2026-10-18 から）

パターンは文章ではなく**検証できる主張**として書く。ページの先頭に機械が読む前書き（`---` で囲む）を置き、本文は人が書く。

```yaml
---
claim: 一文で言える主張
status: tentative            # tentative / confirmed / rejected。変えるのは人だけ
applies_to:                  # 効く範囲(型、モデルの系統、言語など。自由な文)
  type: choice
  models: 素のチャット用モデル
evidence:                    # 機械が書き足す。1 run 1 件、日付・モデル・数字・判定(supports / contradicts / inconclusive)
  - date: "2026-10-18"
    model: qwen3-4b-q8+state-last
    result: plain 0.903 vs current 0.774 (top-1 on 62 truths; 8 better / 1 worse, p=0.020)
    verdict: supports
retest:                      # 再検証の手順: この spec で、候補 better が候補 than を top-1 で上回れば支持
  spec: decisions/prompt-intent-v2.yaml
  better: plain
  than: current
---
```

`shikii wiki verify [pattern.md ...] --local URL` が retest を今のモデルで回し（対の勝敗と片側の符号検定、p ≤ 0.05 で支持）、証拠を 1 件足す。支持するモデルが 2 つ以上あって反証が無ければ「confirmable」と言うが、状態を confirmed にするのは人。別のプロジェクトの人が自分のモデルで verify を回せば、その結果がこの主張の新しい証拠になる。これが「育った知識は人のあいだで共有される」の実体。前書きの無いページは「まだ検証できない主張」として verify が飛ばす。

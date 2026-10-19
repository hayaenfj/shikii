---
name: shikii
description: 決定モデル(はい/いいえ・多択・段階を確率で答える AI)を業務で使い続けるための道具 shikii の使い方。判定の spec を作る、判定して帯(act / ask / discard / abstain)で振り分ける、正解を付ける、線(較正)を引く、聞き方を tune で育てる、wiki の主張を確かめる、審判を使う時に読む。判定そのものは shikii の CLI か MCP の道具(shikii_decide 等)に任せ、この skill は「いつ・どう使うか」の作法だけを持つ。
---

# shikii の作法

shikii は「人が出すのは意図と正解だけ。聞き方も信頼も段階も機械が育てる」道具。あなた(エージェント)の役目は、判定を**呼ぶ**こと、帯に**従う**こと、正解を**人から受け取って**記録すること。数字を自分で決めない。

## いつ使うか

**使う場面**(同じ種類の判断を繰り返し、答えが「はい / いいえ」「どれ」「どの程度」に落ちるもの):

- 業務の振り分け: 問い合わせや issue をどの窓口に回すか、2 つの報告が同じ不具合か、依頼は変更か質問か、PR がある領域に触るか
- 開発中の繰り返しの判断: テストの失敗の種類(環境 / 既存の不具合 / 今の変更)、変更の重さの段階、レビューの指摘の種類
- 既に `.shikii/decisions/` に spec があるプロジェクトでは、自分で判断する前に `shikii decide` か MCP の `shikii_decide` を使う。帯に従う

**使わない場面**: 一度きりの判断、文章や設計を作る仕事、理由づけが要る判断(この設計でよいか、このバグの原因は何か)、入力が 1 つの文に決まらないもの。これらは自分で考える。

**spec を作る合図**: 同じ種類の判断を 2 回したら、3 回目の前に `shikii new`(質問に答えるだけで spec ができる。白紙から YAML を書かない)。質問と選択肢の文は人に一度読んでもらう(それが「意図」)。`pii` は分からなければ `true`。

**hook が入っている時**(`shikii hook install`、Claude Code と Codex): あなた自身の考え方は hook が 2 点で黙って測っている。依頼を受けた直後(この依頼で結局ファイルを変えるか、着手前に進め方を合わせるべきか)と、ターンを閉じる時(変えたものは頼まれた範囲か、頼まれたことを終えたか)。依頼の直後に文脈(「shikii: …」)が渡されたら従う。ターンの終わりの文は人に向くので、人が「まだ終わっていない」と言ったらそれに従う。答え合わせは人が画面でする。

## 守ること(順番)

1. **帯に従う。** `act` = 自動で動いてよい(線があり、確率が act 閾値以上)。`ask` = 人に確かめる。`discard` = 動かない(はい/いいえ では「いいえ」、多択では「どれにも届かない」)。`abstain` = 入力か送れる先が無い。帯以外の数字(生の確率)で判断しない
2. **線は人の正解からしか引けない。** `calibration:` ブロックを手で書かない。`shikii calibrate --write` だけが書く。人の正解が `min_n` に届くまでは `act` は出ない。それが正しい挙動
3. **正解は人(または結果)が決める。** あなたが「きっとこうだ」と思っても正解にしない。人の答えは `shikii label <trace> <truth>` か MCP の `shikii_label`。結果から分かる時(ファイルが変わった、など)は `--source outcome`
4. **聞き方は tune が選ぶ。** 文言・選択肢の説明・配置(`layout: plain`)・分解(`steps`)は spec の `variants:` に候補として書き、`shikii tune` が同じ正解で測る。勝っても採用は人が「意味は同じ」と確かめてから(`tune --write` か画面)。あなたが文言を直接書き換えない
5. **知識は wiki に。** 勝った候補は主張の下書き(`.shikii/wiki/patterns/drafts/`)になる。あなたは下書きを一般化した文に書き直せるが、状態(tentative / confirmed / rejected)は変えない。`shikii wiki verify` で再検証できる
6. **本文は外に出さない判定がある。** spec の `pii: true` と、本番データを読んだセッション(`--tainted`)は手元のモデルだけ。提案役・審判も同じ

## よくある流れ

| したいこと | すること |
|---|---|
| 判定する | `shikii decide .shikii/decisions/<id>.yaml -i key=value --local http://127.0.0.1:8080` または MCP `shikii_decide`。迷った時の文脈(直前の発言など)が spec の `state.context` にあれば、その入力も渡す |
| 正解を付ける | 人の答えを `shikii label <trace_id> <truth>`。画面(`shikii serve`、http://127.0.0.1:8095)でも |
| 過去の表を使う | `shikii import past.csv --spec .shikii/decisions/<id>.yaml` → `calibrate --labeled` |
| 線を引く | `shikii calibrate .shikii/decisions/<id>.yaml --local URL --write`(人の正解が min_n 以上の時だけ書かれる) |
| 聞き方を育てる | spec の `variants:` に候補 → `shikii tune .shikii/decisions/<id>.yaml --local URL`(`--propose 3 --proposer URL` で AI に候補を作らせる) |
| 大きいモデルに正解を出させる | `shikii judge .shikii/decisions/<id>.yaml --local <審判の URL>`(人の正解との一致率が act に届く時だけ) |
| CI で守る | `shikii check .shikii/decisions/*.yaml`(文言が変わった線、手書きの線、精度不足を落とす) |
| 状態を見る | `shikii status .shikii/decisions/*.yaml`(1000 判定あたりの人の判断、線の状態、抜き取り) |

## 読む順

- 帯と領収書の読み方: [references/bands.md](references/bands.md)
- spec の書き方(型、入力、文脈、配置、候補): [references/spec.md](references/spec.md)
- 正解・較正・審判の約束: [references/truths.md](references/truths.md)
- 全コマンドと詳細: `shikii --help`、repo の README と docs/

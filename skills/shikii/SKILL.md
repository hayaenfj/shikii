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

**人が「〜を shikii で判定して育てたい」と言ったら**(業務の判定の立ち上げ。人にコマンドを打たせない): (1) プロジェクトルートに `.shikii/` が無ければ `shikii init`(MCP の設定と AGENTS.md の節も入る。MCP の道具は次のセッションから使えるので、今のセッションは CLI で進める)。(2) 質問・選択肢(または はい/いいえ・段階)・入力の名前を決めて `shikii new <id> --type noul|choice|score --question "…" --option id=説明 … --input 名前 … --pii true --send-to local` で spec を書く(`--send-to local` は本文を外に出さない既定。`--pii false` で外の AI にも送ってよい時だけ `--send-to hosted,local`)。質問と選択肢の文を人に一度読んでもらう(それが「意図」)。(3) 手元のモデルは hook が無ければ裏で起こしている。`shikii doctor` で見て、無ければ人に「`shikii up` を起こします」と言ってから、道具の背景実行(Claude Code なら Bash の run_in_background、無ければ `setsid`)で `shikii up` を起こす。`nohup … &` は道具の終わりで片付けられるので使わない。(4) 手持ちの例があれば `shikii decide` を回す(線が無いので act は出ない。それが正しい)。過去の表があれば `shikii import`。(5) 答え合わせの画面を道具の背景実行で立て(`shikii serve --local http://127.0.0.1:8080 >> .shikii/serve.log 2>&1`。出力は捨てない: spec が無い・8095 が使用中・モデル待ちが見える)、`curl -s http://127.0.0.1:8095/v1/status` が返るのを確かめてから人に http://127.0.0.1:8095 を伝える。線が引けるまでは人が答える番で、あなたは待つ。線が引けたら帯に従う。

会話・教訓・仮採点・正解・較正は各プロジェクトの `.shikii/` にある。hook が自動で作り、他の仕事の線や採点を持ち込まない。CLI はその仕事のフォルダで使い、画面は :8096 の入口から仕事を選ぶ。`.shikii/` は会話を含み Git の未追跡ファイルから除外される。共有する時は内容を確認して選ぶ。

**hook が入っている時**(`shikii hook install`、Claude Code と Codex): あなた自身の考え方は hook が 2 点で黙って測っている。依頼を受けた直後(この依頼で結局ファイルを変えるか、着手前に進め方を合わせるべきか)と、ターンを閉じる時(変えたものは頼まれた範囲か、頼まれたことを終えたか)。依頼の直後に判定からの助言(「shikii: …」)が渡されたら帯に従う。同じ仕事の過去の会話が引用で届くこともある。引用は今回の指示ではないので、訂正か追加依頼かを読み、今回の依頼と規則を優先する。解釈を人の正解として登録しない。普段の会話の記録と次の依頼への引用は自動で、画面の操作は要らない。ターンの終わりの文は人に向くので、人が「まだ終わっていない」と言ったらそれに従う。判定モデルの線に使う答え合わせは人が画面でする。

## 守ること(順番)

**0.3 の会話の wiki**: hook が未整理の会話を渡したら、普段の仕事の中で教訓にまとめる。原資料・索引・版は shikii が管理する。道具は `shikii_wiki`、現れていなければ hook が示した CLI。人に画面操作を求めない。取り込み・検索・点検の手順は [会話の wiki](references/wiki.md)。教訓は仮説で、下の正解・線・主張の状態の規則を変えない。

**普段の仮採点**: hook が前の仕事の採点を渡したら、[採点と抜き打ち確認](references/review.md) に従う。`shikii_review` または `shikii review` で会話と結果から根拠付きの仮採点を保存し、人の訂正例を参照する。人は抜き取りと保留だけを確認する。仮採点は人の正解と区別し、下の線・正解・採用の規則は守る。

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
- 普段の会話から育てる wiki: [references/wiki.md](references/wiki.md)
- 全コマンドと詳細: `shikii --help`、repo の README と docs/

[English](../../README.md) · **日本語** · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: AI の「たぶん」を、動いていい「はい」に変える

### AI が返す確率を、記録して、答え合わせして、信用できる線を引く。その一式。

**AI は「たぶん 0.9」と言う。shikii は「その 0.9 で動いていいか」を答える。**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/github/actions/workflow/status/hayaenfj/shikii/test.yml?style=flat-square&label=tests)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[なぜ作ったか](../why-shikii.ja.md) · [設計の記録](../decision-ops-framework.md) · [運用メモ](../operations.md) · [モデル比較](../model-comparison.md) · [詳しい使い方](../guide.ja.md)**

[まず動かす](#まず動かす) · [何をしてくれるのか](#何をしてくれるのか) · [どの-ai-が使えるか](#どの-ai-が使えるか) · [実際に測った結果](#実際に測った結果) · [よくある質問](#よくある質問) · [もっと読む](#もっと読む)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii は AI そのものを動かしません。手元にある AI に質問を送り、答えを記録し、答え合わせが済むまで「自動で動いてよい」とは言いません。
> 「動いてよい」の根拠になる数字は shikii が計算して書き込むもので、人が手で書くものではありません。

---

## どういう話か

最近の AI には、文章を書くのではなく、**選ぶ**ことに特化したものがあります。「この依頼は、ファイルを書き換える依頼ですか？」と聞くと、文章ではなく「はい 0.92、いいえ 0.08」のような確率で答えます。速くて安く、迷いが数字で見えるので、プログラムの中で「0.9 を超えたら自動で進める」のように使えます。

ところがその 0.92 は、そのまま信用していい数字ではありません。AI が変われば同じ 0.92 の意味が変わり、質問の文言を少し変えても変わります。作っている会社も「あなたのデータで確かめてから使ってください」と注意書きを付けています。確かめる道具は、誰も出荷していません。

shikii はその「確かめて、線を引いて、守り続ける」部分だけをやります。

1. 質問を 1 枚の設定ファイルに書く（質問文、選択肢、どこまでなら自動で動いてよいかの線）
2. AI に聞く。返ってくるのは生の確率ではなく、**自動で動く / 人に聞く / 捨てる** の三択
3. あとで「本当はどうだったか」を教える（正解を 1 行付ける）
4. 正解が溜まったら、確率がどれくらい当てになるか測り、「この線から上は自動で動いてよい」を決めて設定ファイルに書き戻す
5. 質問の文言か AI が変わったら、その許可は自動で消える。古い根拠で勝手に動くことはない。ただし人が付けた正解は残るので、もう一度 4 を回せば（AI に聞き直すだけで）線は戻る

付属の小さなプログラム `shikii-readout` を使うと、普通のチャット用の AI モデル（GGUF ファイル）もこの仕組みに乗せられます。手元の計測では、普通の 27B モデルがこの経路で、専用に学習された決定モデルと同じ精度に、1 問 0.6 秒で届きました。

> 名前は「閾」（しきい）。確率に引く線のことです。コマンドの名前も `shikii`。

## まず動かす

Go 1.24 以上があれば、それ以外は要りません。AI も API の鍵も無しで一周できます。

### 2 分で全部見る

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # 偽の AI で: 聞く → 正解を付ける → 線を引く → 検査 → 劣化の監視 → 掃除
scripts/demo.sh --overconfident # 自信過剰な偽 AI。確率を補正する様子が見える
```

### 本物の AI に向ける

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) 手元のパソコンで動かす(初回は Kev-4B というモデル約 2.5GB を取得)
make server                                       # llama-server が :8080 で待つ
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='README の誤字を直して'

# b) TypeSafe 社のホスト版 Jev。鍵は環境変数で渡す(ファイルには置かない)
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='README の誤字を直して'

# c) OpenRouter 経由
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

返ってくるのは三択の答え（`act` = 自動で動く、`ask` = 人に聞く、`discard` = 捨てる）と、追跡用の番号（trace id）と、確率です。
答え合わせがまだ済んでいない AI では、答えは高くても `ask` 止まりです。

### 答え合わせをして、線を引く

```sh
bin/shikii label <trace_id> change_request          # さっきの判定に正解を付ける。または label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <答えに入っていた AI の名前> --write   # 正解が溜まったら線を引いて書き込む
bin/shikii check examples/prompt-intent.yaml        # 文言が変わっていないか、線がまだ正しいか。だめなら exit 1(CI 向け)
```

### または、プログラムと人のための 1 プロセスを立てる

```sh
bin/shikii serve --specs decisions --local http://127.0.0.1:8080        # http://127.0.0.1:8095/
curl -s localhost:8095/v1/decide -d '{"spec":"prompt-intent","inputs":{"message":"誤字を直して"}}'
```

プログラムはどの言語からでも `POST /v1/decide` と `POST /v1/label` を叩くだけ。人はブラウザで画面を開くと、答え合わせ待ちの判定が `ask` から順に並び、選択肢ごとにボタンがあります。押せば正解が記録され、`eval.min_n` に届いた spec はサーバーが自分で線を引いて書き込みます。trace id を打つ人はいません。画面は外部の読み込み無しの HTML 1 枚なので、ネットの無い社内のノート PC でも開けます。言語の切替があり、spec に `display:`（言語ごとの題名・質問・選択肢や段階の名前）を書けば、その言語で判定が表示されます。`display` は AI には送らず、線の鍵にも入りません。

### 普通のチャット用モデルを使う

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # 本家 llama.cpp の llama-server、:8086
make readout                                               # その前に立つ変換役、:8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### 入れる・更新する

```sh
make install          # bin/shikii を ~/.local/bin/shikii に置く
git pull && make install
```

## 何をしてくれるのか

### 質問は 1 枚のファイル

```yaml
id: prompt-intent
type: choice                       # 選択肢から選ぶ質問。ほかに はい/いいえ(noul)と段階(score)がある
question: What is the user asking the assistant to do?
options:
  - id: change_request
    description: Change something in the repository - edit, fix, add, remove, rename...
  - id: question
    description: Explain, investigate, compare or give an opinion, without changing anything.
state:
  template: "{{ message }}"        # AI に見せる本文。ここに依頼文が入る
  required: [message]              # 本文が無ければ AI に聞かずに「判定なし」
policy:
  send_to: [hosted, local]         # 外の AI に送ってよいか、手元だけか
  pii: true                        # 本文に個人情報が入り得る → 外の AI(hosted)には送らない
thresholds:
  act: 0.75                        # ここから上は自動で動く。ただし答え合わせが済んでから
  ask: 0.60                        # ここから上は人に聞く。それ未満は捨てる
```

### 一周の流れ

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

### 「線」はどう決まり、いつ消えるか

線を引くときの材料は、**質問文・選択肢・本文の型のハッシュ**と、**AI が名乗った名前**の組み合わせです。
質問を一語でも変えると、あるいは AI の版が上がると、この組み合わせが変わるので、前に引いた線は使われなくなります。
答えは `ask`（人に聞く）に戻るだけで、古い線で勝手に動くことはありません。
線を戻すのは機械の仕事です。正解は入力ごとに、文言にも AI にも依らない形で残っているので、本文も一緒に残っているので、`calibrate` が新しい組み合わせで全部聞き直します。

`shikii-readout` を使うときは、AI に見せる文の並べ方まで名前に含めます（`…+state-last` のように）。
同じモデルでも、本文を選択肢の前に置くか後ろに置くかで正解率が 5 ポイント動いたからです。

### 補正は要るのか、要らないのか

`calibrate` は 3 つのやり方を比べて、一番よかったものを書き込みます。比べ方は、正解を 5 つに分けて「4 つで学び、残り 1 つで試す」を 5 回繰り返す、ごく普通の交差検証です。

| やり方 | 中身 | いつ選ばれるか |
|---|---|---|
| `identity` | 補正しない。「この AI の数字はそのまま信用できる」と記すだけ | 生の確率で、線の上の正答率が十分なとき。学習済みの決定モデルはだいたいこれ |
| `platt` | はい/いいえ の確率を、傾きとずれの 2 つの数で補正 | 普通のチャット用モデルを読んだときなど、確率が強すぎる・弱すぎるとき |
| `vector` | 選択肢ごとに「この選択肢に逃げやすい」などの癖を補正 | 選択肢の質問で、特定の選択肢に偏るとき |

正解は「それではなかった」という部分的な形（`!option`）でも付けられます。間違いの数には入りますが、補正の学習には使いません。

### 壊してから直す、を全部に

「自動で動いてよい」に関わる分岐（外に送らない判断、答え合わせ前は動かない、文言が変わったら線を消す、正解の形の検査、確率の逆転の検出、掃除の表示だけモード、…）には、
**わざと壊してテストが落ちるのを見てから直した**記録があります。Go のテストは 8 パッケージ 63 本。2 分のデモも CI で毎回回ります。

## どの AI が使えるか

shikii は「決定モデル」向けの共通の呼び方（`POST /v1/systemone`）で話します。それを喋るものなら何でもつながり、喋らないものは変換役を間に置きます。

| つなげる先 | 何が返るか | 要るもの・手順 |
|---|---|---|
| **TypeSafe Jev**（ホスト版） | 選択肢・段階・はい/いいえ の確率と、`jev-1.13.0` のような名前 | 環境変数 `TYPESAFE_API_KEY`。`--hosted` の既定 |
| **llama.cpp の決定モデル**（Kev-4B、OpenJev、Clef、…） | 同じものを手元で。名前は `/v1/models` から | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M`（`make server`）。llama.cpp 0.6.0 以上 |
| **OpenRouter Decisions API** | 同じものを OpenRouter 経由で | `DECISIONS_API_KEY`、`--hosted https://openrouter.ai/api/alpha/decisions` |
| **普通のチャット用モデル**（GGUF）を `shikii-readout` で | 選択肢の文字の出やすさを確率にしたもの。補正は持たない | 後ろに本家 `llama-server` 0.7.0。[`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** を [BonJev](https://github.com/ChezRD/bonjev) で | 同じ。BonJev 独自の聞き方と補正付き | Rust のビルドと Prism 版の llama.cpp。[`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **偽の AI**（`bin/fake-systemone`） | 決まった確率。デモとテスト用 | 何も要らない。`make fake` |
| **Claude Code の hook**（claude-jev） | 依頼ごとに種類を判定し、作業の終わりに結果から正解を付ける | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

本番のデータに触った作業中なら `--tainted` を付けます（hook が付けてもよい）。すると外の AI には送らず、手元の AI が無ければ「判定なし」になります。

## 実際に測った結果

同じ質問、同じ正解で、4 つの AI を比べました。題材は全部作り物で、依頼文の種類あて（英語 39 件・日本語 23 件）、不具合報告の重複判定（日本語 32 組）、問い合わせの同一判定（日本語 32 組）です。全表は [`docs/model-comparison.md`](../model-comparison.md)。

| AI | ライセンス | 正解率（全体 / 日本語） | はい/いいえ の確率の質、生 → 補正後（不具合報告 / 問い合わせ。小さいほど良い） | 1 問の秒数 |
|---|---|---|---|---|
| Kev-4B（専用に学習された決定モデル） | Apache 2.0 | 0.919 / 0.913 | 0.038 → 0.035 platt / 0.030 → 0.020 platt | 0.25 |
| OpenJev 27B（同） | CC BY-NC 4.0 | **0.984 / 1.000** | **0.007** 補正なし / 0.037 補正なし | 3.8 |
| Bonsai 2 27B を BonJev で | Apache 2.0 | 0.968 / 1.000 | 0.049 → **0.019** platt / 0.084 → 0.028 platt | 2.5 |
| **Ternary-Bonsai-27B を `shikii-readout` + 本家 llama.cpp で** | Apache 2.0 | 0.968 / 1.000 | 0.048 → 0.024 platt / 0.033 → 0.020 platt | **0.58** |

分かったことを 3 つだけ。

**文言のほうが補正より効く。** 依頼文の種類あてでは、間違い 21 件のうち 19 件が「その他」に落ちていました。選択肢の説明を書き直しただけで正解率が 0.68 から 0.90 に上がり、補正はまだ何もしていません。補正が効くのは「順番は合っているが自信が足りない」ときだけで、どちらなのかは `calibrate` が出す表で分かります。

**補正が要るかどうかは、触れ込みではなく数字で決まりました。** はい/いいえ の 2 セットで、OpenJev は生の確率で足りて 2 回とも「補正なし」、Kev-4B は補正でわずかに良くなり、普通のチャット用モデルは 2〜4 倍良くなりました。10 通り（4 つの AI × 2 セット、BonJev と readout を別に数えて）全部で正と負はきれいに分かれていて、違いは確率の正直さだけです。`calibrate` が（質問, AI）ごとに交差検証で比べて、勝った方を書き込みます。

**普通のモデルが、専用モデルの隣に並んだ。** 27B のチャット用モデル、本家 llama.cpp、小さな変換役が 1 つ。改造版も再学習も要りません。Apache ライセンスの専用モデルの中で一番良いものと同じ正解率、日本語は全問正解、1 問 0.58 秒。本文を選択肢の前から答えの直前に動かしただけで 0.919 から 0.968 に上がったので、並べ方も「線」の材料に入れてあります。

## よくある質問

**shikii は AI を動かすのですか。** 動かしません。自分で立てるか借りた AI に質問を送り、答えの後に起きることを全部引き受けます。`shikii-readout` だけは少し違って、チャット用モデルの出力を決定モデルと同じ形に変換しますが、計算しているのはやはり llama.cpp です。

**なぜいつまでも「自動で動く」にならないのですか。** その質問とその AI の組み合わせで、まだ線が引かれていないからです。正解を付けて（`label`、またはファイルで `--labeled`）、`calibrate --write` を回すと、`status` に `calibrated: …` と出ます。線の上の正答率が確かめられていない判定は、自動で動けません。それがこの道具の要点です。

**質問を一語変えたら線が消えました。** 正しい動きで、かかるのは数分の計算であって、人の手間ではありません。線は古い文言に対して引いたものなので信用しませんが、人が決めた正解（この依頼は変更依頼だった、この 2 件は同じ不具合だった）は文言にも AI にも依りません。もう一度 `calibrate --write` を回すと、残っている正解を新しい文言と今の AI に全部聞き直して、線を引き直して書き込みます。人は要りません。そのために、判定の本文は常に手元の記録に残しています。本文の無い古い正解があれば、`calibrate` が「何件が聞き直せなかったか」を言います。

**どの AI を使えばいいですか。** ライセンス・速さ・言語で決まることで、shikii は決めません。上の表と [`docs/model-comparison.md`](../model-comparison.md) が答えで、同じ質問と同じ正解なら、補正の要否まで含めて数字で選べます。新しい AI は `scripts/eval-model.sh <url> <name>` で同じ表が作れます。

**日本語でも使えますか。** 使えます。差が出るのはむしろ日本語です。Kev-4B は日本語の依頼で 0.913、27B のモデルは全問正解。日本語の はい/いいえ 2 セット（不具合報告の重複、問い合わせの同一判定）は、試したどの AI でも正と負がきれいに分かれ、補正は確率を鋭くしただけでした。

**データはどこに送られますか。** AI に見せる本文だけが、設定の `send_to` に書いた先へ行きます。どこへ送ってよいかを決めるのはあなたで、shikii は何が個人情報かを推測しません。spec には `pii: true`（本文に個人情報が入り得る → `send_to` に何が書いてあっても外の AI には送らない）か `pii: false` を必ず書きます。既定はありません。判定の本文は常に手元の記録に残るので、人が画面で答えられ、文言や AI を変えた後の聞き直しもできます。`--tainted` ならどの設定でも何も外に出ません。鍵は環境変数 `DECISIONS_API_KEY` か `TYPESAFE_API_KEY` からだけ読みます。

**記録が増え続けます。** `shikii gc` が、30 日より古くて正解の付かなかった記録を消します。正解付きの記録と正解そのものは、線を引く材料なのでずっと残ります。`--apply` を付けるまでは数えるだけです。

**選択肢の順番で答えが変わりませんか。** 直す前に測ります。`shikii bias <spec> --permutations 100` が選択肢を並べ替えて、答えが入れ替わった回数と確率のずれを出します。Kev-4B は 100 回で入れ替わり 0、ずれ 0.024 以下でした。チャット用モデルは違うかもしれないので、このコマンドがあります。

## もっと読む

- [`docs/guide.ja.md`](../guide.ja.md): 全コマンドと、実機で一周した例を載せた日本語の長い手引き。
- [`docs/why-shikii.ja.md`](../why-shikii.ja.md): なぜ作ったか、何を決めたか。初めて読む人向けの読み物版。
- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): 設計の記録。数字と出典まで入った、作った本人向けの書き方。
- [`docs/operations.md`](../operations.md): 動いているものの止め方・戻し方、おかしいときの見分け方、まだ無いもの。
- [`docs/model-comparison.md`](../model-comparison.md): 比較表と、同じ表を作る手順。
- [`integrations/readout.md`](../../integrations/readout.md)、[`integrations/bonjev.md`](../../integrations/bonjev.md)、[`integrations/claude-code.md`](../../integrations/claude-code.md): 実際に動かしている 3 つのつなぎ方。
- [`examples/`](../../examples/) と [`testdata/`](../../testdata/README.md): 線を引く前の設定ファイルの例と、テストに使う作り物の正解データ。

コマンド一覧:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]   # 聞く
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]   # 正解を付ける
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]   # 線を引く
shikii check     <spec>...  [--store DIR]                 # 線がまだ正しいか(CI 向け。だめなら exit 1)
shikii drift     <spec> --model M [--window N]            # 最近の正解で、線を引いたときより悪くなっていないか
shikii bias      <spec> [--rows K] [--permutations N]     # 選択肢の順番で答えが変わるか
shikii status    [spec...] [--store DIR]                  # 記録・正解・線の溜まり具合
shikii gc        [spec...] [--older-than 30d] [--apply]   # 古くて正解の無い記録を消す(既定は数えるだけ)
shikii hash      <spec>
shikii serve     [--port 8095] [--specs DIR] [--store DIR] [--local URL] [--hosted URL]   # HTTP の口 + 答え合わせの画面。自動で線を引く
```

## 手伝ってくれる人へ

issue と pull request を歓迎します。決まりは一つだけ。動きを守るテストは、まず落ちる所を見せてください。わざと壊して、テストを走らせ、どう落ちたかを書き、直して通ることを書く。docs には分岐ごとにその記録が並んでいます。この一覧を正直に保ってください。

## ライセンス

[Apache 2.0](../../LICENSE)。AI モデルのライセンスは別です。Kev-4B と Bonsai 系は Apache 2.0、OpenJev は CC BY-NC 4.0（商用不可）、ホスト版 Jev は TypeSafe 社のサービス。shikii が持つのはあなたの確率と正解だけで、モデルのものは何も持ちません。

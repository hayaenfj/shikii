[English](../../README.md) · **日本語** · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: 人は意図と正解だけを出す。聞き方も信頼も機械が育てる

### 決定モデル（AI に「はい/いいえ」「どれ？」を確率で答えさせる仕組み）を、人の手間を増やさずに業務で使い続けるための道具。

**AI は「たぶん 0.9」と言う。shikii は「その 0.9 で動いていいか」を答え、聞き方を自分で良くしていく。**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/github/actions/workflow/status/hayaenfj/shikii/test.yml?style=flat-square&label=tests)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[なぜ作ったか](../why-shikii.ja.md) · [聞き方の wiki](../../wiki/index.md) · [設計の記録](../decision-ops-framework.md) · [運用メモ](../operations.md) · [モデル比較](../model-comparison.md) · [詳しい使い方](../guide.ja.md)**

[目指すもの](#目指すもの) · [人がすること](#人がすること) · [まず一周する](#まず一周する) · [機械が育てるもの](#機械が育てるもの) · [どの-ai-が使えるか](#どの-ai-が使えるか) · [実際に測った結果](#実際に測った結果) · [よくある質問](#よくある質問) · [もっと読む](#もっと読む)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii は推論そのものは行いません。手元やホスト版の決定モデルに質問を送り、答えを記録し、答え合わせが済むまで「自動で動いてよい」とは言いません。
> 「動いてよい」の根拠の数字も、聞き方の候補も、機械が作って書き込むもので、人が手で書くものではありません。

---

## 目指すもの

**人が出すのは意図と正解だけ。聞き方も、信頼も、段階も機械が育て、育った知識は人のあいだで共有される。**

最近の AI には、文章を書くのではなく**選ぶ**ことに特化したもの（決定モデル）があります。「この依頼はファイルを書き換える依頼ですか？」と聞くと、「はい 0.92、いいえ 0.08」のように確率で答える。速くて安く、迷いが数字で見えます。

ところがその 0.92 はそのまま信用できません。AI が変われば意味が変わり、質問の文言を少し変えても変わる。作っている会社も「あなたのデータで確かめてから使ってください」と書いています。そして、確かめるには人が正解を付け、良い聞き方を人が考え、線を人が引き直す必要がある、というのが今の普通です。

shikii はそこを逆にします。人が持つべきものは 2 つだけ、**意図**（決めたいこと）と**正解**（起きた事実）。それ以外の、質問の文言、選択肢の説明、本文の並べ方、閾値、較正、劣化の監視、文言やモデルが変わった時の引き直しは**技術**で、技術は機械の仕事です。そして「この型の質問には、このモデルにはこう聞くと通る」という学びは、1 人の経験ではなく、証拠付きの共有の wiki に溜まります。

名前は「閾」（しきい）。確率に引く線のこと。コマンドの名前も `shikii`。

## 人がすること

| いつ | すること | どれくらい |
|---|---|---|
| 最初 | 決めたいことを質問に書く（`shikii new` が質問に答えるだけで作る） | 1 回 |
| 日々 | 「本当はどうだったか」を答える。画面のボタン 1 つ。結果から自動で拾える場合は 0 | 1 件につき 1 回 |
| たまに | 機械が提案した言い換えが「まだ同じことを聞いているか」を見る。ボタン 1 つ | 提案が出た時だけ |

それ以外はありません。線を引く、引き直す、候補を作る、測る、劣化を見張る、記録を掃除する、は全部コマンドかサーバーが勝手にやります。

## まず一周する

Go 1.24 以上があれば、AI も API の鍵も無しで一周できます。

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii && make build
scripts/demo.sh                 # 偽の AI で: 聞く → 正解を付ける → 線を引く → 検査 → 文言を変えて線が消える → 聞き直して戻る → 掃除
```

本物に向けるなら、この順です。

```sh
bin/shikii up                                   # 手元に Kev-4B を起こす(llama-server を探し、初回は約 2.5GB を取得、応答まで待つ)
bin/shikii new my-decision --type choice        # 質問にいくつか答えると decisions/my-decision.yaml ができる
bin/shikii serve --specs decisions --local http://127.0.0.1:8080 --proposer http://127.0.0.1:8087   # http://127.0.0.1:8095/
```

あとはプログラムが `POST /v1/decide` を叩き（どの言語からでも）、人は画面で答え合わせをするだけ。

- 答えは `act`（自動で動く）/ `ask`（人に聞く）/ `discard`（捨てる）/ `abstain`（判定していない）の 4 つ。生の確率はプログラムに渡さない
- 答え合わせが `eval.min_n` 件に届くと、サーバーが自分で線を引いて spec に書く。以後 `act` が出る
- 画面の「聞き方」の欄で「AI に候補を作らせて測る」を押すと、裏で候補が作られ、溜まった正解で全部採点される。勝った候補が出たら「意味は同じ → 採用」を押す。これで新しい版と線が同時にできる

エージェント（Claude Code など）からは MCP で同じことができます（`.mcp.json` が同梱）。

```sh
bin/shikii doctor         # 何が動いていて何が足りないか、直すコマンドまで 1 画面で
```

## 機械が育てるもの

### 聞き方

正解率を動かすのは補正より文言です。手元の数字で、選択肢の説明を書き直しただけで 0.68 → 0.90、本文を答えの直前に置き換えただけで 27B のモデルが 0.919 → 0.968。

人が付けた正解は文言に依らないので、聞き方の候補をいくつ作っても**新しい正解なしで全部採点できます**。`shikii tune` は候補ごとに正解率・確率の質・`act` での精度と再現率を出し、さらに「候補を選ぶこと自体がまぐれでないか」を k 分割の holdout で測ります。採用するのは holdout が「今の文言のまま」を上回る時だけ。62 件中 1 件多く当てた程度では通りません（実際にそうなりました）。

候補は人が書いてもよく、チャット用の AI に作らせることもできます（`--propose`）。AI には今の聞き方・どこで間違えているか・外した実例・そして [wiki](../../wiki/index.md)（spec のフォルダの隣の `wiki/` に自動でできる markdown のフォルダ。設定は要りません）の該当ページ（この型の聞き方、このモデルの系統の癖、確認済みのパターン、この判定の決まり）を渡します。結果は wiki の `runs/` に数字と採否と一行の学びとして書き戻され、次の提案がそれを読みます。

ただし**意味は機械には確かめられません**。言い換えが「まだ同じことを聞いているか」は人が画面で確認し、「違う」と言えばそれも記録されます。数字は機械、意味は人。

### 信頼

- 線（較正）の鍵は「質問文・選択肢・本文の型のハッシュ」と「AI が名乗った名前」。どちらかが変われば線は消え、答えは `ask` に落ちる。古い根拠で勝手に動くことはない
- 消えた線を戻すのは機械の仕事。正解は本文と一緒に残っているので、`calibrate` が新しい組み合わせで全部聞き直す。人は要らない
- 補正の方法（そのまま / Platt / vector）は交差検証で比べて選ぶ。「較正済み」と謳うモデルでも数字で決める
- `check` は CI で、文言が変わった線・手書きの線・精度が足りない線を落とす。`drift` は最近の正解で劣化を見る
- 安全に関わる分岐は全部、**わざと壊してテストが落ちるのを見てから直した**記録があります。Go のテストは 8 パッケージ 105 本

### 段階（これから）

モデルを増やすのではなく、聞き方に段を作ります。まず最小の質問、迷えば文脈を足すか質問を分解し、最後に人。上の段の答えは下の段の正解になるので、一番安い聞き方が通用する範囲が広がっていく。進み具合は「1000 判定あたりの人の判断の数」で見ます。

## どの AI が使えるか

決定モデル向けの共通の呼び方（`POST /v1/systemone`）を話すものなら何でも。話さない普通のチャット用モデルは、付属の変換役 `shikii-readout` を前に置きます（本家 llama.cpp だけで動く。改造も学習も要らない）。

| つなげる先 | 要るもの |
|---|---|
| **手元の決定モデル**（Kev-4B、OpenJev、Clef、…）を llama.cpp で | `shikii up`（または `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M`）。llama.cpp 0.6.0 以上 |
| **普通のチャット用モデル**（GGUF）を `shikii-readout` で | `shikii up --gguf <file>` が自動で変換役を前に立てる。本家 llama.cpp 0.7.0 |
| **TypeSafe Jev**（ホスト版） | 環境変数 `TYPESAFE_API_KEY` |
| **OpenRouter Decisions API** | `DECISIONS_API_KEY`、`--hosted https://openrouter.ai/api/alpha/decisions` |
| **PrismML Bonsai** を [BonJev](https://github.com/ChezRD/bonjev) で | Rust のビルドと Prism 版の llama.cpp。[`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **偽の AI**（`bin/fake-systemone`） | 何も要らない。デモとテスト用 |
| **Claude Code の hook**（claude-jev） | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

本文を外に出してよいかは、あなたが決めます。spec の `pii: true` は「本文に個人情報が入り得る → `send_to` に何が書いてあっても外の AI には送らない」。`pii: false` は `send_to` のとおり。既定は無く、必ずどちらかを書きます。本番のデータに触った作業中は `--tainted` で、その本文は後の聞き直しでも外に出ません。

## 実際に測った結果

同じ質問、同じ正解で 4 つの AI を比べました。題材は全部作り物: 依頼文の種類あて（英語 39 件・日本語 23 件）、不具合報告の重複判定（日本語 32 組）、問い合わせの同一判定（日本語 32 組）。全表は [`docs/model-comparison.md`](../model-comparison.md)。

| AI | ライセンス | 正解率（全体 / 日本語） | はい/いいえ の確率の質、生 → 補正後（小さいほど良い） | 1 問の秒数 |
|---|---|---|---|---|
| Kev-4B（専用に学習された決定モデル） | Apache 2.0 | 0.919 / 0.913 | 0.038 → 0.035 / 0.030 → 0.020 | 0.25 |
| OpenJev 27B（同） | CC BY-NC 4.0 | **0.984 / 1.000** | **0.007** / 0.037（補正なし） | 3.8 |
| Bonsai 2 27B を BonJev で | Apache 2.0 | 0.968 / 1.000 | 0.049 → **0.019** / 0.084 → 0.028 | 2.5 |
| **Ternary-Bonsai-27B を `shikii-readout` + 本家 llama.cpp で** | Apache 2.0 | 0.968 / 1.000 | 0.048 → 0.024 / 0.033 → 0.020 | **0.58** |

分かったことを 3 つ。

**文言のほうが補正より効く。** 間違い 21 件のうち 19 件が「その他」に落ちていた質問は、選択肢の説明を書き直しただけで 0.68 → 0.90。補正はその後の「順番は合っているが自信が足りない」にだけ効きます。

**補正が要るかどうかは、触れ込みではなく数字で決まる。** OpenJev は生の確率で足りて 2 回とも「補正なし」、Kev-4B はわずかに補正が勝ち、普通のチャット用モデルは 2〜4 倍良くなった。10 通り全部で正と負はきれいに分かれていて、違いは確率の正直さだけ。

**普通のモデルが、専用モデルの隣に並んだ。** 27B のチャット用モデル、本家 llama.cpp、変換役が 1 つ。改造も再学習も Rust も要らず、Apache ライセンスの専用モデルで最良のものと同じ正解率、日本語は全問正解、1 問 0.58 秒。

これは**仮説**で、まだ主張ではありません: 型の付いた判定なら、測った聞き方 + 較正は、同じ土台のモデルで、微調整（LoRA）よりずっと少ない例で使える精度に届き、検索で文脈を足す方法（RAG）より確率が正直かもしれない。確かめ方は公開します: 同じ課題、同じ正解、同じモデルで 3 つの方法を並べ、正解率と確率の質を比べる。

## よくある質問

**結局、人は何をするのですか。** 最初に決めたいことを書く。日々は画面のボタンで「本当はこうだった」を答える（結果から拾える判定なら 0 回）。機械が言い換えを提案したら「意味は同じ」か「違う」を押す。それだけです。

**なぜ `act` にならないのですか。** その質問とその AI の組み合わせで、まだ線が引かれていないからです。正解が `eval.min_n` 件に届けばサーバーが自分で引きます。`shikii status` か画面に「集めている n/50」と出ます。

**質問を変えたら線が消えました。** 正しい動きで、かかるのは計算だけで、人の手は要りません。もう一度 `calibrate --write` を回すか、画面で候補を採用すれば、残っている正解を新しい文言で聞き直して線が戻ります。

**AI に聞き方を変えさせて大丈夫ですか。** 採用は 3 重の門をくぐります。溜まった正解での数字、k 分割の holdout が「今のまま」を上回ること、そして人の「意味は同じ」。どれか一つでも欠けたら今の文言のままです。却下した理由も残り、次の提案が読みます。

**データはどこに送られますか。** AI に見せる本文だけが `send_to` に書いた先へ行きます。何が個人情報かを shikii は推測せず、`pii: true/false` をあなたが書きます（`true` なら外には送らない）。判定の本文は常に手元の記録に残るので、人が答えられ、聞き直しもできます。鍵は環境変数からだけ読み、キーチェーンには触りません。

**どのモデルを使えばいいですか。** ライセンス・速さ・言語で決まることで、shikii は決めません。上の表と `scripts/eval-model.sh <url> <name>` で、新しい AI でも同じ表が作れます。

**記録が増え続けます。** `shikii gc` が、30 日より古くて正解の付かなかった記録を消します。正解付きの記録と正解は線の材料なので残ります。`--apply` を付けるまでは数えるだけ。

## もっと読む

- [`docs/why-shikii.ja.md`](../why-shikii.ja.md): なぜ作ったか、何を決めたか。初めて読む人向け
- [`wiki/`](../../wiki/index.md): 聞き方の知識。型ごと・モデルの系統ごとの癖、証拠付きのパターン、判定ごとの決まりと run
- [`docs/guide.ja.md`](../guide.ja.md): 全コマンドと、実機で一周した例を載せた長い手引き
- [`docs/operations.md`](../operations.md): 動いているものの止め方・戻し方、おかしいときの見分け方、まだ無いもの
- [`docs/model-comparison.md`](../model-comparison.md): 比較表と再現の手順
- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): 設計の記録（数字と出典まで）
- [`integrations/`](../../integrations/mcp.md): MCP、readout、BonJev、Claude Code の hook のつなぎ方
- [`examples/`](../../examples/README.md): ビジネス・法律・ドローンの spec の見本

コマンド一覧:

```
shikii new       <id> [--type choice|noul|score] [--question ...] [--option id=desc ...] [--from spec.yaml]   # 質問に答えて spec を作る
shikii up        [--hf repo:quant | --gguf file --alias name] [--port 8080]   # 手元のモデルを起こす。普通の GGUF なら変換役を前に
shikii serve     [--port 8095] [--specs DIR] [--store DIR] [--local URL] [--proposer URL]   # HTTP の口 + 答え合わせと聞き方の画面
shikii mcp       [--specs DIR] [--store DIR] [--local URL] [--proposer URL]   # 同じものを MCP の道具として(採用の道具は無い)
shikii doctor    [--specs DIR] [--store DIR] [--local URL] [--hosted URL]   # 何が動いていて何が足りないか
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--json]   # 聞く(プログラム向け)
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   # 正解を付ける(端末から)
shikii calibrate <spec> [--labeled file] [--model M] [--write]   # 線を引く。古い正解は新しい文言で聞き直す
shikii tune      <spec> [--propose N --proposer URL] [--write]   # 聞き方の候補を採点し、holdout が勝てば新しい版に。結果は wiki/runs/ へ
shikii check     <spec>... [--store DIR]   # CI: 文言が変わった線、手書きの線、精度不足の線を落とす
shikii drift     <spec> --model M [--window N]   # 最近の正解で劣化を見る
shikii bias      <spec> [--rows K] [--permutations N]   # 選択肢の順番で答えが変わるか
shikii status    [spec...] [--store DIR]   # 記録・正解・線の溜まり具合
shikii gc        [spec...] [--older-than 30d] [--apply]   # 古くて正解の無い記録を消す(既定は数えるだけ)
shikii hash      <spec>
```

## 手伝ってくれる人へ

issue と pull request を歓迎します。決まりは 2 つ。動きを守るテストは、まず落ちる所を見せてください（わざと壊して、落ちたことを書き、直して通ることを書く）。wiki のパターンの状態（confirmed / tentative / rejected）を変えるのは人だけで、証拠の run を付けてください。

## ライセンス

[Apache 2.0](../../LICENSE)。AI モデルのライセンスは別です。Kev-4B と Bonsai 系は Apache 2.0、OpenJev は CC BY-NC 4.0（商用不可）、ホスト版 Jev は TypeSafe 社のサービス。shikii が持つのはあなたの確率と正解と聞き方だけで、モデルのものは何も持ちません。

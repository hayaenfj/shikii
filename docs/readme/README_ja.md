[English](../../README.md) · **日本語** · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: 問いと正解を資産にする。モデルは使い捨て、知識は自分で育つ

### AI の判定（はい/いいえ、どれ、どの程度）を業務で使い続けるための道具。人が出すのは、意図と、抜き取りの正解と、主張の意味の確認だけ。

**AI は「たぶん 0.9」と言う。shikii は「その 0.9 で動いていいか」を根拠付きで答え、聞き方を自分で良くし、学んだことを検証できる主張として wiki に残す。モデルを替えても、問いと正解と主張はそのまま使える。**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-161%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[構造の設計](../architecture.md) · [比較ベンチ](../bench.md) · [聞き方の wiki](../../wiki/index.md) · [既製の判定](../../decisions/README.md) · [運用メモ](../operations.md) · [詳しい使い方](../guide.ja.md)**

[目指すもの](#目指すもの) · [何が違うか](#何が違うか) · [最短の入り口](#最短の入り口) · [人がすること](#人がすること) · [機械が育てるもの](#機械が育てるもの) · [測った結果](#測った結果) · [もっと読む](#もっと読む)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

> [!IMPORTANT]
> shikii は推論そのものは行いません。手元やホスト版のモデルに質問を送り、答えを記録し、答え合わせが済むまで「自動で動いてよい」とは言いません。
> 「動いてよい」の根拠の数字も、聞き方の候補も、wiki の主張の証拠も、機械が測って書き込むもので、人が手で書くものではありません。状態（この主張は確かか）を決めるのだけが人です。

---

## 目指すもの

**問いと正解を資産にする。モデルは使い捨てで、知識は検証できる主張として wiki に溜まり、測るたびに自分で育つ。人が出すのは意図と、抜き取りの正解と、主張の意味の確認だけ。**

最近の AI には、文章を書くのではなく**選ぶ**ことに特化したもの（決定モデル）があります。「この依頼はファイルを書き換える依頼ですか？」と聞くと「はい 0.92、いいえ 0.08」のように確率で答える。速くて安く、迷いが数字で見えます。普通のチャット用モデルも、`shikii-readout` を前に置けば同じ口で答えます。

ところが業務で使い続けるには、確率だけでは足りません。0.9 が本当に 9 割当たるのか、モデルを替えた瞬間に嘘にならないか、聞き方を 1 語変えたら何が起きるか、誰も見ていない自動の判定が正しいまま走っているか。それを人が毎回確かめるなら、AI を使う意味が薄れます。

shikii は、その確かめる仕事を機械に渡し、人の仕事を 3 つに絞ります。

1. **意図を書く**: 何を、どの選択肢で判定したいか（spec）。質問に答えるだけで作れます
2. **抜き取りの正解に答える**: 機械が迷った判定と、自動で動いた判定の 20 件に 1 件。画面でボタンを押すだけ
3. **主張の意味を確かめる**: 機械が「この聞き方の方が当たる」と測って下書きした主張を、人の言葉で一般化し、確かか決める

残りは機械がやります。確率を直す線、聞き方の候補の採点と採用、文脈を足す段、別のモデルへの乗り換え、学んだことの記録。

## 何が違うか

<p align="center"><img src="../assets/shikii-architecture.svg" alt="shikii の構造: 人がすること 3 つ、資産(問い・正解・領収書)、機械が育てる輪、検証できる wiki、使い捨てのモデル" width="100%"></p>

**1. 検証できる wiki。** 「聞き方」の知識は文章ではなく、**主張 + 効く範囲 + 証拠 + 再検証の手順**として溜まります。`shikii wiki verify` が主張を今のモデルで測り直し、証拠を足し、独立したモデル 2 つが支持すれば「確かにできる」と印を付けます。状態を変えるのは人だけ。tune が勝った候補を見つけるたびに主張の下書きが生まれ、別のモデルで測るたびに証拠が厚くなる。別の人が自分のモデルで同じ主張を回せば、その結果も証拠になる。文章の wiki は読んで更新されますが、こちらは**測って昇格・降格します**。

**2. 問いと正解は資産、モデルは使い捨て。** 知識を重みに入れる（微調整）と、モデルを替えた時に全部やり直しです。shikii は知識を**問いの側**（文言、選択肢の説明、配置、分解、文脈、似た例の引き方）と**正解の貯金**に置きます。モデルを替えたら、`calibrate` が貯金を聞き直して線を引き直すだけで、人の手は 0。ベンチでは、文言と配置を機械が選び、似た例を足した聞き方が、約 50 件の LoRA と同じ正解率に届きました（下の表）。

**3. 決定ごとに領収書。** 自動で動いた判定には、どの線を、いつ、何件の正解で引いたか、そのうち人が確かめた分と、その精度が付きます。「95% の確信」ではなく「この種の判定は人が検査した 48 件で精度 0.96」と言える。審判（別の大きいモデル）に正解の多くを出させる時も、人の正解との一致率を先に測り、届いた審判だけを使い、人の精度には数えません。

## 最短の入り口

- **Docker**: Go もモデルの準備も無いなら `docker compose up`。llama.cpp（Kev-4B）と shikii が立ち、答え合わせの画面が http://127.0.0.1:8095 に出ます。`decisions/`、`wiki/`、`.shikii/` はホストのまま（[integrations/docker.md](../../integrations/docker.md)）
- **Claude Code / Cursor**: `.mcp.json` に 3 行足すと `shikii_decide` / `shikii_label` / `shikii_tune` が道具として使えます。この repo は Claude Code の plugin でもあり、skill（`skills/shikii/SKILL.md`）がエージェントに「いつ判定を呼ぶか、帯にどう従うか、正解と状態は人のもの」という作法を教えます
  ```json
  { "mcpServers": { "shikii": { "command": "shikii", "args": ["mcp", "--specs", "decisions", "--store", ".shikii", "--local", "http://127.0.0.1:8080"] } } }
  ```
- **1 行で入れる**: `curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh` のあと、プロジェクトの根で `shikii init`。フォルダ、`.mcp.json`、skill の配置が 1 回で済みます（rtk や rea と同じ流儀）
- **端末**: 既製の判定（[`decisions/`](../../decisions/README.md) の 6 つ。依頼の種類、不具合の重複、問い合わせの同一判定、…）をそのまま使うか、`shikii new --from decisions/prompt-intent-v2.yaml` で写して書き換える
  ```sh
  make install && shikii up                       # 手元のモデルを起こす(Kev-4B を自動で取る)
  shikii decide decisions/prompt-intent-v2.yaml -i message="fix the typo" --local http://127.0.0.1:8080
  shikii serve --specs decisions --store .shikii --local http://127.0.0.1:8080   # 答え合わせの画面 http://127.0.0.1:8095
  ```
- 過去の判定の表があるなら `shikii import past.csv --spec decisions/<id>.yaml` で正解の貯金にできます。最初の線を引く 50 件を手で答えなくてよい。CI では `uses: hayaenfj/shikii@main` が PR ごとに `shikii check` を回します

基本構成は、実行する場所に `decisions/`（判定）、`wiki/`（知識）、`.shikii/`（記録と正解）。置き場所のオプションはありません。

## 人がすること

答え合わせの画面（`shikii serve`）は、IT に詳しくない人が迷わないように作っています。判定ごとのタブ、人の言葉の質問、「AI の予想: 変更の依頼（自信 82%）」、答えのボタン（1〜9 のキー、Enter で AI と同じ）、「まだ学習中: 12 / 50 件」の進み具合。「くわしく」を押すと主張の下書きや聞き方の候補が出ます。

人が書くものは、spec（意図）と wiki の細則（その判定の決まり）だけ。細則は人が正解を付ける時の手引きで、審判が読む手引きでもあります。

## 機械が育てるもの

- **聞き方**: 文言・選択肢の説明・配置・分解の候補を `tune` が同じ正解で採点し、holdout と符号検定を越え、人が「意味は同じ」と確かめた時だけ採用。勝った候補は wiki の主張の下書きになる
- **信頼**: 線は「問いの指紋 × モデル」ごと。人の正解が `min_n` 件以上ないと書かない。act の 20 件に 1 件を人が抜き取りで確かめ、`check` が CI で守る。act の判定には領収書
- **段階**: 最小の質問 → 迷えば文脈を足す → それでも迷えば人。上の段の答えは下の段の正解に
- **知識**: 主張の下書き → 別のモデルの証拠 → confirmable → 人が決める。`shikii wiki verify` で測り直せる

詳しくは [構造の設計](../architecture.md)。

## 測った結果

同じ 62 件・同じ土台 Qwen3-4B で 3 つの方法を並べた（[手順と注意](../bench.md)）。

| 方法 | top-1 | Brier |
|---|---|---|
| 測った聞き方（tune が配置を選ぶ） | 0.903 | 0.0959 |
| 似た例 5 件を足す | 0.952 | 0.0482 |
| LoRA 約 50 件 | 0.952 | 0.0375 |

文言と配置を機械が選び、似た例を足すと、重みを触らずに LoRA と同じ正解率に届いた。確率は LoRA が良い。はい/いいえ の 32 件では LoRA が土台を下回り、似た例は効いた。微調整が勝つかは型と例の数で変わる。

## もっと読む

- [構造の設計](../architecture.md): 4 つの柱と、できたもの・まだのもの
- [詳しい使い方](../guide.ja.md): 全コマンド、spec の書き方、FAQ
- [既製の判定](../../decisions/README.md) · [聞き方の wiki](../../wiki/index.md) · [比較ベンチ](../bench.md) · [運用メモ](../operations.md)
- [つなぎ方](../../integrations/mcp.md): MCP、変換役、Docker、Claude Code の hook と skill

**手伝ってくれる人へ**: 安全に関わる分岐は、わざと壊してテストが落ちるのを見てから直す。数字は全部スクリプトから出す。wiki の主張の状態を変えるのは人だけ。新しいオプションは足さず、基本構成と固定の規則で決める。Go のテストは 8 パッケージ 169 本。

## ライセンス

[Apache 2.0](../../LICENSE)。AI モデルのライセンスは別です。Kev-4B と Bonsai 系は Apache 2.0、OpenJev は CC BY-NC 4.0（商用不可）、ホスト版 Jev は TypeSafe 社のサービス。shikii が持つのはあなたの問いと正解と主張だけで、モデルのものは何も持ちません。

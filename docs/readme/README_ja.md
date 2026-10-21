[English](../../README.md) · **日本語** · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: AI の「たぶん」を、仕事で使える「大丈夫」に変える

### 普段の会話から出典付きの教訓を育て、AI の判断を答え合わせで確かめる道具。依頼と訂正はいつもどおり、wiki の整理は AI が行います。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-219%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)
[![Version](https://img.shields.io/badge/version-0.3.0-00ADD8?style=flat-square)](../../CHANGELOG.md)

[困りごと](#困りごと) · [使い方 1: AI 自身の考え方を測る](#使い方-1-ai-自身の考え方を測る) · [使い方 2: 業務の判定を任せる](#使い方-2-業務の判定を任せる) · [画面でできること](#画面でできること) · [インストール](#インストール) · [なぜ AI を替えても良いか](#なぜ-ai-を替えても良いか) · [もっと読む](#もっと読む)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## 困りごと

AI に「この問い合わせは前のと同じ案件？」と聞くと「たぶん 0.9」と返ってきます。仕事ではこれだけでは使えません。その 0.9 は本当に 10 回に 9 回当たるのか。AI を新しいのに替えた日から、黙って嘘になっていないか。誰も見ていない所で自動で動いた判断は、正しいままか。これを人が毎回確かめるなら、AI を使う意味が薄れます。

shikii は、あなたの答え合わせから「ここから上なら任せてよい」という**線**を引きます。線が引けるまでは任せません。線が引けたら、自動で動いた判断に「何件の答え合わせで、どれだけ当たっていたか」の根拠が付きます。線は AI ではなく、問いと答え合わせの側に残るので、AI を替えても人の手は要りません。

使い方は 2 つあります。コードを書く AI の考え方を測る使い方(いちばん軽い)と、業務の判定を任せる使い方です。

## 使い方 1: AI 自身の考え方を測る

Claude Code か Codex を使っている人向けです。コードを書く AI が、依頼を受けた時と作業を終えた時にしている判断を、黙って測ります。

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

これだけです。実行ファイルを入れ、Claude Code と Codex に hook を 2 つ足し、エージェントの作法(skill)を置き、手元の小さな AI(Kev-4B)を裏で起こします(llama.cpp が無ければ入れ方を 1 行示します)。新しいターミナルで普段どおり AI に仕事を頼むだけで、測るのは 4 つ(Codex は最初に `/hooks` で shikii の hook を一度 trust します)。

| いつ | 問い | 正解の出どころ |
|---|---|---|
| 依頼を受けた時 | この依頼で、結局ファイルを変えることになるか | 自動(次の依頼までに作業ツリーが変わったか) |
| 依頼を受けた時 | 手を動かす前に、進め方を人と合わせるべきか | あなたが画面で |
| 作業を終えた時 | 変えたものは、頼まれた範囲の中だったか | あなたが画面で |
| 作業を終えた時 | 頼まれたことを終えたか | あなたが画面で |

ターンの終わりに `shikii: このセッションの判定 3 件が答え待ち: http://127.0.0.1:8096/` と 1 行出るので、手の空いた時に開いて答えます(画面も hook が裏で立てます)。1 件数秒です。答えが 50 件たまると線が引け、そこから AI の振る舞いが変わります。進め方を合わせた方がよさそうな依頼は AI への一言になり、終えていない・範囲の外に見えるターンはあなたへの 1 行になります。どちらにも「線は正解 61 件、人が確かめた精度 0.96」のような根拠が付きます。線が引けるまでは記録だけで、hook がツールを止めることはありません。

置き場は `~/.shikii/agent` の 1 つで、プロジェクトをまたいで線は 1 本です(問いは AI の考え方についてのもので、どの repo でも同じ基準で答えが付くから)。記録にはどの repo の判定かの印が付き、画面で仕事ごとに見られます。詳しくは [hook の説明](../../integrations/claude-code.md)。

**0.3: 普段の会話から wiki が育ちます。** 「まだ終わっていない」「そこは頼んでいない」と伝えると、元の依頼・返答・その後の発言が一組で残ります。普段使っているエージェントが、当てはまる場面と出典を付けて教訓にまとめます。次の似た依頼ではその教訓を引用し、変更前の版も残します。索引は自動で更新され、出典やリンクは一日一回点検されます。wiki のための画面操作や追加のモデルは要りません。「知識」画面から教訓と原文を読めます。

**普段の採点も AI、人は抜き打ち確認。** 次の返事を受けて、通常のエージェントが「頼まれた範囲か」「頼まれたことを終えたか」を根拠付きで仮採点し、分からないものは保留します。人は無作為の抜き取りと保留だけを確認し、訂正は次の採点へ戻ります。仮採点は正解ラベルや較正と分けて保存します。[採点の作法](../../skills/shikii/references/review.md)。

原資料を残し、AI が取り込み・検索・定期点検を回す [カーパシーの LLM Wiki の発想](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f)を取り入れています。教訓は出典付きの仮説として扱い、会話の解釈を正解や精度の数字には数えません。[0.3 の変更内容](../../CHANGELOG.md)。

## 使い方 2: 業務の判定を任せる

「この問い合わせは同じ案件か」「この依頼は変更の依頼か」のように、仕事の中で繰り返す判定を AI に任せ、答え合わせで育てる使い方です。ここでも人が打つコマンドはありません。エージェントに言うだけです。

> 問い合わせが前のと同じ案件かどうかを shikii で判定して、答え合わせで育てていきたい

エージェントは作法(skill)に沿って進めます。プロジェクトに `.shikii/` を作り(`shikii init`)、質問・選択肢・入力を決めて判定の定義ファイルを書き(`shikii new`。質問と選択肢の文はあなたに一度読んでもらいます。それが「意図」です)、判定を呼び(`shikii decide`)、答え合わせの画面(http://127.0.0.1:8095)を立ててあなたに知らせます。線が無いうちは `act`(任せてよい)は出ません。あなたが画面で答えていくと、判定ごとに決めた数(この例は 20 件)で線が自動で引かれ、次からはこうなります。

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (自動で動いてよい。線は 61 件の正解で引き、うち 48 件は人の答え。人が答えた正解のうち、この線なら自動で動く側に入る 25 件で 96% が正しかった)
```

`band` は 4 つだけ。`act` = 任せてよい、`ask` = 人に確かめて、`discard` = 動かない、`abstain` = 入力が足りない。生の確率で判断する場面はありません。

- **自分で打つなら**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`。全部 [詳しい使い方](../guide.ja.md) に
- **エージェントから**: `shikii init` が MCP の設定を書くので、Claude Code・Cursor・OpenCode・Codex は道具 `shikii_decide` と `shikii_label` をそのまま呼べます([MCP](../../integrations/mcp.md))
- **自分のプログラムから**: `shikii serve` を立てて `POST /v1/decide`(判定)と `POST /v1/label`(正解)。本文は JSON で、`-H 'Content-Type: application/json'` が要ります([口の一覧](../guide.ja.md#serve-の-http-api))
- **既製の判定**: この repo の `decisions/` に 6 つ(依頼の種類、不具合の重複、問い合わせの同一判定、…)。`shikii new --from <repo>/decisions/prompt-intent-v2.yaml` で写して書き換えます([一覧](../../decisions/README.md))
- **過去の表がある**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` で答え合わせの材料にできます。最初の 50 件を手で答えなくてよい
- **Docker**: Go もモデルの準備も無いなら `docker compose up`。手元の AI と shikii が立ち、画面は http://127.0.0.1:8095 ([詳しく](../../integrations/docker.md))
- **別の人の例**: [問い合わせメールの振り分け](../examples/inquiry-route.md)。初めて使う人が README だけで一周した記録(定義ファイルと 22 件の入力つき)

## 画面でできること

答え合わせの画面(`shikii serve`)で人がすることは 3 つです。それ以外は機械がやります。

1. **答える。** 「AI が迷った判断」と「自動で動いた判断の 20 件に 1 件」が並ぶので、本当はどうだったかをボタンで答えます。1〜9 のキーと Enter(AI と同じ)で流せます。「回答済み」で前の答えを直せます(いちばん新しい答えが使われます)
2. **聞き方の案を書く。** 「こう聞いた方が当たりそう」と思ったら、「聞き方」の欄に自分の言葉で書きます。機械が同じ答え合わせの材料で今の聞き方と比べ(AI 自身の判定では、答えが 50 件増えるごとに自動で)、本当に良くなった時だけ「採用できます」と出します。AI に案を作らせることもできます。ファイルは開きません
3. **採用か却下を押す。** 意味が変わっていないかを見て押します。数字は機械が確かめ、意味はあなたが確かめます

「くわしく」にすると、モデルごとの線の状態、「AI にもう一度聞く」(聞き方や線を変えた後に同じ本文をもう一度判定して前と今を並べる)、知識の欄(「こう聞くと当たる」という主張と、その証拠)が出ます。記録は消えず(追記だけ)、新しい判定が元の代わりに並びます。

## インストール

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 以上。無ければ Docker(上)
```

install.sh がすること: 実行ファイル(`shikii` と `shikii-readout`)を入れ、Claude Code と Codex に hook を 2 つ足し、エージェントの作法(skill)を `~/.claude/skills/shikii` に置き、llama.cpp があれば手元の AI(Kev-4B、:8080)を裏で起こします。ホスト版の Jev や OpenRouter を使うなら鍵は環境変数で([接続先](../guide.ja.md#接続先))。何が動いていて何が足りないかは `shikii doctor`。

プロジェクトごとの準備(`shikii init`)はエージェントが打ちます(自分で打ってもよい)。作るのはフォルダ `.shikii/` の 1 つだけです(中に `decisions/` = 判定の定義、`wiki/` = 知識、`records/` と `labels/` = 記録と答え合わせ)。プロジェクトのルートに他のものは増えません。`decisions/` `wiki/` `labels/` は AI を替えても使う資産なので commit を勧め、`records/` は増え続けるので任意です(`shikii gc` で古いものを捨てられます)。エージェントの設定(Claude Code の `.mcp.json`、Cursor の `.cursor/mcp.json`、OpenCode の `opencode.json`、Codex の `~/.codex/config.toml`)にも shikii の項を足しますが、あなたが書いた項は触りません。もう一度打っても、あるものは触りません。

コマンドの出力と hook の文は英語です(Claude Code の設定の言語が日本語なら、hook の文は日本語も併記されます)。画面には日本語があります。外の AI だけで使う時の設定、`AGENTS.md` と `CLAUDE.md` の扱い、画面そのものを直す時の Node などの細かい注意は [詳しい使い方](../guide.ja.md) にあります。

## なぜ AI を替えても良いか

<p align="center"><img src="../assets/shikii-architecture.svg" alt="shikii の構造: 人がすること 3 つ、資産(問い・答え合わせ・根拠)、機械が育てる輪、測って育つ wiki、使い捨ての AI" width="100%"></p>

- **知識は AI ではなく、問いと答え合わせの側に溜まる。** 答え合わせの材料は AI とは別に残るので、新しい AI にそのまま使い、線を引き直せます。実測では、聞き方と並べ方を機械が選んで似た例を足すと、AI の重みを微調整する方法(LoRA)と同じ正解率に届きました([数字](../bench.md))
- **自動の判断に根拠が付く。** 「95% の確信」ではなく「人が確かめた 48 件で精度 0.96」と言えます
- **wiki は読むものではなく、測って育つもの。** 「こう聞くと当たる」が数字つきの知見として溜まり、新しい AI で試し直せます。確かだと決めるのは人だけです

## もっと読む

- [詳しい使い方](../guide.ja.md): 全コマンド、serve の HTTP API、よくある質問 · [定義ファイルの書き方](../../skills/shikii/references/spec.md)
- [hook の説明](../../integrations/claude-code.md): AI 自身の考え方を測る仕組み、帯 → 振る舞いの表、中の形
- [構造の設計](../architecture.md): 仕組みの全体(上の図の詳しい版)
- [既製の判定](../../decisions/README.md) · [聞き方の wiki](../../wiki/index.md) · [測った数字](../bench.md) · [運用メモ](../operations.md)
- [つなぎ方](../../integrations/mcp.md): MCP、普通のチャット用 AI を使う変換役、Docker

**手伝ってくれる人へ**: テストは、わざと壊して落ちるのを見てから直す。数字は全部スクリプトから出す。wiki の知見を「確か」と決めるのは人だけ。設定の項目は増やさず、置き場所と固定の決まりで済ませる。

## ライセンス

[Apache 2.0](../../LICENSE)。AI モデルのライセンスは別です(Kev-4B と Bonsai 系は Apache 2.0、OpenJev は CC BY-NC 4.0、ホスト版 Jev は TypeSafe 社のサービス)。

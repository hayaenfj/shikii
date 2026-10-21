[English](../../README.md) · **日本語** · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="96"></p>

# shikii: 会話の訂正を、次の仕事に活かす

いつものエージェントが、依頼・返答・訂正から出典付きの教訓を育てます。次の似た依頼で思い出し、行動にどう反映されたかまで振り返ります。人は普段どおりに仕事を頼み、抜き取りと保留を確認します。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-260%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)
[![Version](https://img.shields.io/badge/version-0.4.2-00ADD8?style=flat-square)](../../CHANGELOG_ja.md)

0.4.2では、`shikii update` で次回からの更新を行えます。訂正の追跡、短い記憶とまとめた振り返りも引き継いでいます。[変更履歴](../../CHANGELOG_ja.md)。

[何が変わるか](#何が変わるか) · [必要なもの](#必要なもの) · [始め方](#始め方) · [普段の会話から学ぶ](#普段の会話から学ぶ) · [人が確認すること](#人が確認すること) · [役立ったかを確かめる](#役立ったかを確かめる) · [プロジェクトごとの記憶](#プロジェクトごとの記憶) · [業務の判定にも使う](#業務の判定にも使う) · [もっと読む](#もっと読む)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii：モデルの確率を、任せてよい判断へ" width="100%"></p>

## 何が変わるか

「前にも同じことを伝えた」「覚えているようだけれど、仕事ぶりが変わらない」。shikiiが減らしたいのは、こうした説明や訂正のやり直しです。

たとえば、案内を日本語にする仕事で「まだ英語が残っています」と伝えた場合。

| 場面 | shikiiとエージェントが残す・行うこと |
|---|---|
| 訂正を受けた | 元の依頼・返答・人の発言を出典として保持する |
| 教訓にまとめる | 「案内を日本語化する時は全文を確認する」と、当てはまる場面・訂正の原文・次に変える行動を結ぶ |
| 次の似た依頼 | 短い引用が届き、エージェントが今回の条件を確かめて作業へ反映する |
| 振り返る | 全文を確認した記録があるか、同じ指摘が再発したかを、判断と確認結果に結び付ける |

<p align="center"><img src="../assets/shikii-conversation-ja.svg" alt="普段の依頼と訂正から教訓と次の行動をまとめ、次の似た依頼で使い、結果を振り返る流れ" width="100%"></p>

目指すのは「使うほど同じ訂正が減る」ことです。現時点で用意しているのは、それを実利用から確かめる仕組みです。記憶の保存数やAIの自己採点だけで改善したとは扱いません。

## 必要なもの

使い方ごとの組み合わせです。会話を整理するエージェントと、4つの問いや業務の判定に答えるモデルは別の役割です。

| やりたいこと | 必要なもの |
|---|---|
| 普段の会話から学ぶ | Claude CodeまたはCodex、shikii、利用するプロジェクトの初期化 |
| 既製の4問を自動で判定する | 標準構成はllama.cpp（`llama-server`）とKev-4B。hookは `127.0.0.1:8080` を使う |
| 業務の判定に別のモデルを使う | 下の一覧から、System One形式に対応する接続先とモデルを選ぶ |
| ソースからインストールする | Go 1.27以上、Git、sh。画面は同梱済みでNode.jsは不要 |
| コンテナで業務のAPI・画面を動かす | DockerとCompose、またはPodmanとCompose provider。Go・Node.jsはビルド用コンテナが用意する |
| 鍵やGPUなしで流れを試す | Go 1.27以上と同梱の偽モデル（`scripts/demo.sh`） |
| 画面や文書を開発する | 画面はNode.js 24とnpm、文書の図生成・検査はPython 3 |

業務のCLI・MCP・`serve`で選べるモデルの接続先は次のとおりです。

| 接続先 | 準備するもの・確認状況 |
|---|---|
| llama.cpp | Kev-4Bなどの決定モデル。通常のチャットGGUFは同梱の `shikii-readout` を介してつなぐ。[実行例](../../integrations/readout.md) |
| Ollama | v0.35.0以上のSystem One対応APIと決定モデル。公式仕様の `/v1/systemone` とshikiiの形式を照合済み、今回の実機推論は未検証。[公式仕様](https://docs.ollama.com/api/systemone) |
| LM Studio | Jev互換の `/v1/systemone` と、読み込み済みの決定モデル。公式仕様との照合済み、今回の実機推論は未検証。[公式仕様](https://lmstudio.ai/docs/developer/jev-compat/systemone) |
| Jev（TypeSafe AI） | 外部APIの利用権と `TYPESAFE_API_KEY`。外部送信を許可した判定で使う。[公式仕様](https://docs.typesafe.ai/introduction/quickstart) |

Ollama・LM Studio・Jevは、既製hookのllama.cpp構成を自動で置き換える設定ではありません。業務の接続先として使う時はURLと決定モデル名を指定します。チャットAPIがあるだけでは判定用の接続条件を満たしません。[接続先の詳しい条件](../guide.ja.md#判定モデルの接続先)。

Podmanの `compose` は外部のCompose providerを呼び出します。[Podman公式の説明](https://docs.podman.io/en/latest/markdown/podman-compose.1.html)。このリポジトリにはDockerfileとCompose構成がありますが、Podmanでの実行はこの更新では検証していません。コンテナは業務のAPI・画面用で、ホストのエージェントへのhook設定は別です。[Docker・Podmanの準備](../../integrations/docker.md)。

## 始め方

自動で会話を記録する使い方はClaude CodeとCodexに対応しています。インストール後、利用するプロジェクトで一度初期化し、普段どおり仕事を頼みます。

### 1. Goとllama.cppを用意する

macOSでHomebrewを使う場合は次のとおりです。[Go](https://formulae.brew.sh/formula/go)と[llama.cpp](https://formulae.brew.sh/formula/llama.cpp)の公式formulaを使います。Homebrewが無ければ[公式サイト](https://brew.sh/)で先に準備します。

```sh
brew install go llama.cpp
go version
llama-server --version
```

Go 1.27以上と `llama-server` が使えることを確認します。macOS以外のllama.cppの導入は[公式の手順](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md)を参照してください。

基本モデルは **Kev-4BのQ4_K_M版**（`ggml-org/Kev-4B-GGUF:Q4_K_M`）です。初回起動時に約2.5GBを取得し、手元の `:8080` で4問の判定に使います。会話の教訓をまとめる役は普段のエージェントが担当します。

### 2. shikiiをインストールする

Go 1.27以上があれば、次の1行でインストールできます。

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

ソースから入れる場合は、cloneしたshikiiのフォルダで次を実行します。

```sh
sh install.sh
```

install.shは実行ファイル・hook（依頼の前と作業の後の2点）・エージェントの作法を入れます。手元の判定用モデルにはllama.cppを使い、無ければ準備方法を案内します。インストール先がPATHに無い場合も案内が出ます。Codexは最初に `/hooks` でhookをtrustします。

### 3. 利用するプロジェクトを初期化する

新しいターミナルでエージェントを開き、使いたいプロジェクトで頼みます。

> このプロジェクトでshikiiを使いたい

エージェントが `shikii --init` を実行します（自分で実行しても同じです）。初期化した場所だけで有効になり、既製の4問・wiki・MCPの設定・作法と画面への入口が揃います。再初期化しても既存データを保持します。導入の状態は `shikii doctor`、記録の画面は http://127.0.0.1:8096 から確認できます。

### 次回からの更新

```sh
shikii update
```

最新の安定版を確認し、配布物のハッシュを照合して、本体とreadoutを同じ場所へ更新します。更新にGoは不要で、会話・記憶・正解を保持します。導入済みのhookと作法も更新します。起動中の画面・MCPは再起動すると新版になります。[詳しい動作](../guide.ja.md#shikii本体を更新する)。

## 普段の会話から学ぶ

1. **依頼と返答を残す。** 次の人の発言が来ると、直前の依頼・返答・その発言が一組になります。
2. **いつものエージェントが整理する。** 普段の作業の中で、出典と適用する場面を付けて教訓をまとめます。明示的な訂正には原文と次の行動を添えます。
3. **必要な場面へ短く届ける。** 同じプロジェクトの似た依頼に最大2件を引用します。同じセッションへ同じ版を一時間繰り返さず、更新した版はすぐ届けます。
4. **結果をまとめて振り返る。** 未処理が2会話になった時、または1会話が一日経った後の依頼で、採点・wiki整理・届いた記憶の評価をまとめます。人が毎回操作する必要はありません。

教訓は出典付きの仮説です。今回の依頼と規則を優先し、元の会話や前の版も残します。出典・版・リンクの定期点検と索引の修復も行います。会話の整理に追加のモデルは起こさず、利用中のエージェントが担当します。

原資料を保ち、AIが取り込み・検索・点検を行う[カーパシーのLLM Wikiの発想](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f)を取り入れています。[詳しい作法](../../skills/shikii/references/wiki.md)。

## 人が確認すること

| 仕事 | 普段の担当 | 人の関わり方 |
|---|---|---|
| 会話から教訓を整理する | 利用中のエージェント | 普段の会話で訂正する。専用の整理操作は不要 |
| 範囲を守ったか・依頼を終えたか | エージェントが結果と次の発言から仮採点 | 無作為の抜き取りと保留を確認する |
| 着手前に進め方を合わせるべきか | 判定モデルが見立てる | 必要な正解を画面で付ける |
| 業務の判定の正解・聞き方の意味 | 人や確認できた結果 | 正解を付け、聞き方の採用時に意味を確認する |

AIの仮採点は人の正解とは分けて保存します。人が抜き取りで直した例は次の採点へ戻ります。仕事の範囲と完了の仮採点を、モデルに任せるための線へ自動で流し込むことはありません。[採点と抜き取り](../../skills/shikii/references/review.md)。

画面の「知識」では教訓・出典・記憶の振り返りを読めます。「聞き方」では候補を測り、意味を確認して採用できます。日常の会話を覚えさせるために、この画面を毎回開く必要はありません。

## 役立ったかを確かめる

届いた引用と版を依頼ごとに保持し、普段の振り返りで使った判断・確認結果と結びます。観察は2つに分けます。

| 見ること | 記録する分類 |
|---|---|
| 記憶が判断にどう関わったか | 役立った・使わなかった・妨げた・不明 |
| 届いた訂正を次の行動へ反映できたか | 訂正に沿った・同じ指摘が再発・今回は対象外・不明 |

「知識」画面には届いた数と未評価も表示します。訂正に従ったことと記憶が役立ったことは別に観察し、成功・追加依頼・沈黙だけでは効果を決めません。これは確認結果に基づくAIの仮評価で、記憶が無い場合と比べた改善率ではありません。

## プロジェクトごとの記憶

会話・教訓・仮採点・正解・モデルに任せるための線は、その仕事の `.shikii/` に残ります。同名の別プロジェクトとは混ぜず、同じプロジェクトのサブフォルダでは同じ保存先を使います。

<p align="center"><img src="../assets/shikii-architecture.svg" alt="初期化したプロジェクト内に、会話の教訓と仮評価、業務の問いと正解を分けて保存する構造。共通側は型・道具・プロジェクトの入口" width="100%"></p>

中心になる保存先は4つです。

```text
.shikii/
  decisions/  判定する問い
  wiki/       教訓・適用する場面・過去の版
  records/    会話・判定・仮採点・届いた記憶と確認結果
  labels/     人や結果から得た正解
```

共通の `~/.shikii/agent` は型・道具・プロジェクトの入口を持ちます。私的な会話も入るため、`.shikii/` は初期化時にGitの未追跡ファイルから除外します。共有する資料は内容を確認して選びます。保存された記憶はモデルの重みとは別なので、エージェントや判定モデルを替えても参照できます。

## 業務の判定にも使う

問い合わせの振り分けや不具合報告の重複など、繰り返す判断も任せられます。

> 問い合わせが前のと同じ案件かどうかをshikiiで判定し、答え合わせで育てたい

エージェントが質問・選択肢・入力を準備し、判定と答え合わせの画面を立てます。人が質問の意味と正解を確かめ、その実績から「ここから上なら任せてよい」という線を引きます。線がない間は自動実行を許す `act` は出ません。

| 判定 | すること |
|---|---|
| `act` | 線の条件を満たしたので自動で進める |
| `ask` | 人に確かめる |
| `discard` | 動かない |
| `abstain` | 入力や利用できる接続先を揃える |

線は問いとモデルごとに持ち、質問やモデルが変われば測り直します。自動の判断には、何件の正解と人の確認で線を引いたかの記録が付きます。会話からまとめた仮説や仮採点だけで、この線を引くことはありません。

- [既製の判定](../../decisions/README.md)・[業務の例](../examples/inquiry-route.md)・[全コマンドとHTTP API](../guide.ja.md)
- [MCPから使う](../../integrations/mcp.md)・[Dockerで動かす](../../integrations/docker.md)・[測った数字](../bench.md)

鍵やGPUなしで業務の判定の流れを試すなら、cloneしたshikiiのフォルダで次を実行します（Go 1.27以上）。偽モデルの数字は機能の説明用です。

```sh
scripts/demo.sh
```

## もっと読む

- [詳しい使い方](../guide.ja.md): 会話の振り返り、CLI、HTTP API
- [hookの説明](../../integrations/claude-code.md): 依頼の前と作業の後に行うこと
- [構造の設計](../architecture.md): 会話の学習と業務の判定、それぞれの保存と役割
- [エージェントの作法](../../skills/shikii/SKILL.md)・[変更履歴](../../CHANGELOG_ja.md)・[運用メモ](../operations.md)

**開発に参加する人へ**: テストはわざと壊して落ちるのを見てから直す。観察と正解を分け、効果を数で主張する時は測定結果を示す。基本構成と固定の規則で済ませ、新しい設定を増やさない。

## ライセンス

[Apache-2.0 OR MIT](../../LICENSE)。利用者が[Apache 2.0](../../LICENSE-APACHE)と[MIT](../../LICENSE-MIT)のどちらかを選べます。第三者のコード・依存・モデル・外部サービスのライセンスや利用条件は別です。ソースや実行ファイルとして配る時は、この3つのライセンスファイルと必要な第三者の通知を同梱します。既存の0.4.0配布物は元のApache-2.0表記のままです。

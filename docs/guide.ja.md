# shikii(閾) — 詳しい使い方(日本語)

名前は閾値の「閾」。確率を act / ask / discard の帯に切る閾が、この道具の芯だから。コマンドの名前も `shikii`。
モジュールは `github.com/hayaenfj/shikii`、ライセンスは Apache-2.0 OR MIT（どちらかを選択）([`LICENSE`](../LICENSE))。英語の README は [`../README.md`](../README.md)、日本語の短い版は [`readme/README_ja.md`](readme/README_ja.md)。

設計は [`docs/decision-ops-framework.md`](decision-ops-framework.md)。

文書・調査・業務にも同じ保存・検索・振り返りを使える。新規プロジェクトの既製4問は開発に限定しない表現で、既存の質問文・較正は保持する。`edit`の結果と変更範囲の判定は観測できたローカルファイルが対象。外部操作や業務の可否は専用の質問と根拠で扱い、Gitの比較ができない時に空の一覧から変更なしという正解を作らない。

明示的な検索は全記事を対象にし、`search.total`（有効な一致記事数）と`search.candidates`を返す。応答が大きければJSONのデータ量で分割し、同じ検索語や出典IDと、CLIの`continue:CURSOR`またはMCP・HTTPの`cursor`で続きを読む。プロジェクト・条件・結果が変わったカーソルは拒否する。知識画面は記事一覧の続きを自動取得する。1件だけ大きい原資料は切らずに返す。記事・原資料の取得・引用・関連を固定件数で打ち切らない。語の一致による検索なので話題が違えば検索語を見直し、同じ作業では同じプロジェクト・版の原文を再利用する。新しい依頼・版の変更・文脈の欠落時は読み直し、引用元のハッシュ照合を省略しない。
最初の試作は **spec / decide / calibrate** の 3 つだけだった(設計 §11)。今は label / check / drift / bias / status / gc / serve / mcp / new / up / doctor / update / wiki / reflect / guard も動く。この手引きは CLI の中身を順に説明する。

```sh
go build -o bin/shikii ./cmd/shikii

# 以下の例で使う問いを、このプロジェクトへコピーする
bin/shikii new prompt-intent --from examples/prompt-intent.yaml
bin/shikii new same-issue --from decisions/same-issue.yaml

# 仕様のハッシュ(較正の鍵)
bin/shikii hash .shikii/decisions/same-issue.yaml

# 判定。ホスト版は TYPESAFE_API_KEY があれば使う。--tainted なら hosted に送らない
TYPESAFE_API_KEY=... bin/shikii decide .shikii/decisions/prompt-intent.yaml -i message='README の誤字を直して'
bin/shikii decide .shikii/decisions/same-issue.yaml --local http://127.0.0.1:8080 --tainted \
  -i report1='ログイン画面でパスワードを 3 回間違えると 500 エラーになる' -i report2='パスワード誤入力の回数制限に達した直後にサーバーエラー(500)'

# 判定は ./.shikii/records/ に本文ごと残る。ask で選んだ答えをラベルにする
bin/shikii label <trace_id> change_request --source ask
bin/shikii label --last --spec-id prompt-intent question --source ask   # 直近の記録に付ける(trace 不要)

# 較正。noul は identity と Platt、choice/score は identity と vector(選択肢ごとの bias + 温度)を 5-fold CV で比べて勝った方を書く。
# ファイルのラベルと store のラベル((spec_hash, model) が一致するもの)を合わせて使う
bin/shikii calibrate .shikii/decisions/same-issue.yaml --model 'ternary-bonsai-27b-q2g64+state-last'   # 応答の model(鍵があれば Jev の名前)
```

## shikii本体を更新する

**0.7.0 への更新は、初期化済みの各プロジェクトの `.shikii/` の中を全部消して初期化し直す**(プロジェクトの決定)。以前の会話・教訓・仮採点・正解・較正は消え、既製の問い・wiki の雛形・プロジェクトの印は作り直される。残したいものは先に控える。0.7.0 を跨がない更新は全部残す。0.6.0 から更新する時は、消去は古い本体の最後の手順の中で走り、その表示は出ない(0.6.0 が出力を捨てる)。先に `--yes` 無しの `shikii hook install` で消す対象を確かめる。

引数の要らない更新コマンドで、最新の安定版へ更新する。

```sh
shikii update
```

GitHub Releasesの最新安定版から、利用中のOS・CPU向けの本体とreadoutを取得する。Go・curl・GitHub CLIは不要。SHA256SUMSと添付のハッシュ、起動する本体の版を確認し、実行中のshikiiの実体と同じフォルダへ更新する。PATHの別の場所へ入れ直すことはない。現在の版と同じか古い配布物には置き換えない。

本体のライセンスと第三者の通知は、実行ファイルの隣の `shikii-licenses/` へ残す。配布物の欠落やハッシュ不一致では置き換えず、書き込み途中の失敗では本体とreadoutを元へ戻す。同時更新は拒否する。保存先の書き込み権限が無い場合は、その旨を表示する。0.7.0 を跨ぐ一度の消去(上)を除き、会話・教訓・正解・較正の保存先を初期化しない。

この実行ファイルを使うhookが導入済みなら、新しい本体からhookと作法を更新する。導入していないhookは追加しない。この反映だけに失敗した場合は本体の更新済みを明示し、`shikii hook install --yes` で再試行できる。起動中の画面・MCPは停止しないため、再起動して新版へ切り替える。 更新したskillとhookの案内文は新しいエージェントのセッションで使う。0.7.0 を跨ぐ消去を除き、既存プロジェクトの質問文と較正は保持し、新しく初期化するプロジェクトには新版の既製プロンプトを用意する。

## 普段の会話で使う

Claude Code・Codexの自動記録は、インストール後に利用するプロジェクトで `shikii --init` を一度実行して有効にする。未初期化の場所ではhookは動かない。インストールは[日本語README](readme/README_ja.md#始め方)のcurlの1行で行う。ソースから入れる場合はcloneしたshikiiのフォルダで `sh install.sh`。Go 1.27以上が必要。

依頼・返答・次の人の発言を読み、利用中のエージェントが同じ話題の知識へ統合する。検索用の短い要約と、背景・判断理由・未確定点を残す本文を分け、関連ページを結ぶ。`shikii wiki query lesson:<ページID>` で全文を読め、話題の検索では本文も対象になる。[エージェント向けの編集手順](../skills/shikii/references/wiki.md)を参照。同じプロジェクトの似た依頼へ最大2件、1800バイト以内の短い引用を届け、同じ版は同じセッションへ一時間繰り返さない。会話の整理に別のモデルは起こさない。

0.7.3 から、Claude Code のサブエージェントが始まる時(`SubagentStart`)にも、親の依頼に近い教訓を同じ形で届ける(親の控えは変えず、配送は別の ID で記録。Codex には無い event なので足さない)。届いた教訓が行動を変えたかは、振り返りの観察(helpful / unused / harmful / unknown)を `shikii status` の `memory:` の行で数える(届けた数、観察待ち、直近 7 日、訂正の配送)。

機械で確かめられる訂正は「止める検査」にする(`.shikii/guards/<id>.yaml`)。種類は `paired-files`(片方が変わったらもう片方も)、`forbidden-words`(足した行に使わない語)、`commit-body`(そのターンのコミットの本文に含む語)の 3 つ。Stop で有効な検査を変わったファイル・足した行・依頼の時の HEAD からのコミットに当て、引っかかれば `decision: block` と理由を返してエージェントに直させる。止めた後の Stop では止めず、直ったかを `records/guards.jsonl` に記録する。

```sh
shikii guard add < guard.json       # 草稿(無効)。JSON: id, kind, title, reason, source, pairs/words/except/allow/required
shikii guard enable <id>            # 負例で止まる(RED)・正例で通る(GREEN)を見せてから有効にする
shikii guard disable <id>           # 無効に戻す(ファイルは残る)
shikii guard list                   # 状態と最後の結果
shikii guard try                    # 今の作業ツリーに当てて見せる(コミットは見ない)
```

草稿はエージェントが置き、有効にするのは人(作法は [skill の guards.md](../skills/shikii/references/guards.md))。`shikii status` の `guards:` の行に有効な数・止めたターン・直った数が出る。

未処理が2会話、または1会話が一日経った後の依頼で、採点・wiki・記憶の振り返りをまとめる。人が普段の整理のために実行する必要はないが、CLIは次の形で呼べる。

```sh
shikii reflect query
shikii reflect submit < reflection.json
```

`reflection.json` は取得した対象に対する `reviews`・`wiki`・`memory`・`maintenance` を含むJSON。`complete`と各項目の`error`を確認し、失敗分だけ直して再送する。原文、届いた引用と版、関連する教訓、人の訂正例を読んでから保存する。[入力例と作法](../skills/shikii/references/review.md)。

0.4.1の訂正の追跡では、wikiの `correction: {source_id, quote, action}` が人の返答の原文と次に変える行動を結ぶ。次に届いた時の `memory` へ `result`（役立ち方）と `correction_result`（沿った・再発・対象外・不明）、判断と確認結果を残す。両方を区別し、成功・追加依頼・沈黙だけで改善にしない。未評価も知識画面に残す。この部分は[変更履歴](../CHANGELOG_ja.md)の0.4.1の項目。

## 確認結果を原資料にし、知識を検索・見直す

0.5.0では会話のほか、エージェントが実際に確認したコード・テスト・Releaseなども原資料にできる。以下の操作は通常のエージェントが行い、人が毎回整理する必要はない。

```sh
shikii wiki capture < observation.json
shikii wiki query "返金 適用条件"
shikii wiki query "返金 適用条件" continue:CURSOR
shikii wiki query "lesson:<ページID>"
shikii wiki query "<出典ID>" "<別の出典ID>"
shikii wiki query "lesson:<ページID>@<版>"
shikii wiki ingest < article.json
shikii wiki maintain < maintenance.json
shikii wiki lint
```

`observation.json`には`kind`（`human`・`agent`・`code`・`test`・`release`）、`title`・`content`・`reference`・`version`・`observed_at`（RFC3339）を書く。本文は32KiB、JSON全体は64KiBまで。確認した抜粋や結果と、その対象の版を残す。`reference`は参照先の説明で、URL取得やコマンド実行は行わない。返されたIDを記事の`sources`へ使う。同じ観察は重複登録せず、変わった内容や版は別の原資料になる。

検索は題・適用条件・要約・本文を調べ、一致した欄と語、候補の版を返す。候補だけでは意味の関連性を確定できないため、本文と必要な原資料を確認する。`CURSOR`は直前の応答の`next_cursor`に置き換え、同じ検索語または出典IDで続きを取得する。末尾へ着いたことと、必要な知識を見つけたことは別。応答が大きければ分割されるが、記事数や出典数で探索を打ち切らない。同じプロジェクト・同じ版の実際に読んだ内容は作業中に再利用する。

`article.json`の短い`lesson`と詳しい`body`を分け、背景・根拠・適用条件・未確定点を既存記事へ統合する。`related`で関連をつなぎ、意味の編集には`change_reason`を残す。利用できる長い会話原文は引用時に別のハッシュとともに保持し、元の判定記録を消した後も参照できる。原資料や原文が欠けている時は、その限界を残す。

`wiki query`の一覧や`reflect query`には最大2記事の`maintenance`対象がある。本文・原資料・関連ページを確認し、必要な編集を保存した後、最新の`page_id`・`revision`・`key`と確認理由`reason`を`maintenance.json`へ渡す。振り返りの`maintenance`配列へまとめてもよい。記事や関連する版が変われば古い見直し結果は拒否し、未変更でも30日後に再確認する。本文やリンクがないことは確認のきっかけであり、不要な文章や関係を追加する指示ではない。

着手前の`hook plan query`は`pages`で必要な本文を選べる。判断には候補・読んだ記事・引用した版・当時の利用可能な版と取得量が残る。過去の比較は記録された版だけで行い、後から更新されたwikiや解決結果を混ぜない。保存・検索・引用の件数と、判断に役立ったという観察は分ける。詳しい契約は[wikiの作法](../skills/shikii/references/wiki.md)を参照する。

### 今あるメモリを wiki へ移す

エージェントがすでにメモリを持っている(Claude Code の自動メモリのフォルダ: `MEMORY.md` と 1 事実 1 ファイル、Codex のメモ)なら、「メモリを shikii の wiki へ移して」の一言で移せます。skill の [import-memory.md](../skills/shikii/references/import-memory.md) が手順です: 各ファイルを原資料として残し(`shikii wiki capture`)、規則ごとに適用条件付きの教訓を 1 本ずつ下書きし、振り返りの `place` で既存の記事に合流させて重複を作らず、メモリのフォルダは wiki の記事を指す薄い索引にします。どの依頼にも効く規則(見える文の言語、使わない語、push する時)は、wiki が依頼との近さで教訓を届けるため、索引にも 1 行ずつ残します。消すものはありません(古いファイルは本人が納得してから消します)。手順は `shikii --init` が置く skill の一部なので、初期化したどのプロジェクトでも使えます。0.7.6 からは依頼前の hook も、そのフォルダ(と Codex の共通の `~/.codex/memories` のうちこのプロジェクトを名指しするファイル)に `.shikii/wiki <記事の id>` の指し先が無いファイルが残っていれば、セッションに 1 回気付いて、今の依頼の後に移すようエージェントへ伝えます。全プロジェクト共通のファイルは本文を残し、指す行を足すだけです。

## 推奨のモデル(TypeSafe の Jev)

```sh
export TYPESAFE_API_KEY=...       # 鍵は環境変数だけから読む。shikii は Keychain を読まない
shikii doctor                     # hosted に「key present」と出れば、hook・既製の問い・pii: false の判定は Jev へ
```

推奨は TypeSafe の Jev。鍵があれば hook と既製の問いは Jev へ送り、手元のモデルは起こさない(`shikii up` も断る。1 問 約 0.2 秒。取得するものは無い)。鍵を置いたまま手元のモデルを使うなら `SHIKII_MODEL=local`、Jev に固定するなら `SHIKII_MODEL=hosted`(CLI・MCP・`serve`・hook で同じ)。`pii: true` の判定は鍵があっても手元にしか送らないので、鍵を置いたまま手元のモデルで答えさせるには `SHIKII_MODEL=local shikii up` で立てる。手元のモデルとの比較は[下の表](#jevと手元のモデルの比較)。

## 鍵を置けない時の手元のモデル(llama.cpp)

```sh
brew install llama.cpp            # Homebrewの公式formula
llama-server --version
shikii up                         # llama-server を :8086 に、shikii-readout を :8080 に立てる(初回は約 7.6 GB を取得)
```

基本モデルは `prism-ml/Ternary-Bonsai-27B-gguf` の `Ternary-Bonsai-27B-Q2_g64.gguf`(普通のチャット用モデル。`shikii-readout` が選択肢の文字の確率を読み、決定モデルの速さで答える。1 問 約 0.6 秒)。導入は[Homebrewのformula](https://formulae.brew.sh/formula/llama.cpp)、他のOSは[公式の手順](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md)を参照する。`shikii up` と既製hookも、鍵が無ければこのモデルを使う。動作中のメモリは約 10 GB で、普通の PC では重いことがある。OpenJev のような学習済みの決定モデルは `shikii up --hf <repo:quant>`(readout はその答えをそのまま通す)。Kev-4B は使えない(2026-10-23 の決定)。

モデル名は応答の `model`が較正の鍵になる。`--model` で送った名前ではない。

## 判定モデルの接続先

業務の `decide`・MCP・`serve` は、System One形式の `POST /v1/systemone` を使う。OpenAI互換のチャットAPIと同じ意味ではない。CLIで指定するURLにパスがあればそれを使い、パスが無ければ `/v1/systemone` を付ける。応答には問いごとの `answers` と確率が必要。`shikii-readout` はllama.cppの `/apply-template`・`/tokenize`・`/completion` を使う変換役で、任意のチャットAPIに対応する変換役ではない。

| 接続先 | 条件 | URLの例 |
|---|---|---|
| Jev(推奨) | `TYPESAFE_API_KEY`、外部送信を許可するspec(`pii: false`) | 指定不要(`--hosted https://api.typesafe.ai` が既定) |
| llama.cpp | 鍵を置けない時。通常のGGUF(手元の標準はTernary-Bonsai-27B)を `shikii-readout` 越しに。OpenJevなどの決定モデルはそのまま通す | `--local http://127.0.0.1:8080`(`shikii up` が立てる readout の口) |
| Ollama | v0.35.0以上、System One対応の決定モデル。今回の実機推論は未検証 | `--local http://127.0.0.1:11434 --model nimble` |
| LM Studio | Jev互換のSystem One対応API、読み込み済みの決定モデル。今回の実機推論は未検証 | `--local http://127.0.0.1:1234 --model <読み込み済みのモデルID>` |

参照: [OllamaのSystem One](https://docs.ollama.com/api/systemone)、[LM StudioのSystem One](https://lmstudio.ai/docs/developer/jev-compat/systemone)、[TypeSafeのAPI](https://docs.typesafe.ai/introduction/quickstart)。2026-10-21に公式仕様とクライアントを照合したもので、接続先の実機推論の成功を保証する表ではない。ローカルのクライアントはAuthorizationを付けないため、認証を必須にした構成は別途確認が必要。

既製のhookは、鍵があれば Jev、無ければ `127.0.0.1:8080` の固定構成で、OllamaやLM Studioへ自動で切り替わらない。会話の整理と参照は判定用モデルが止まっていても動く。OllamaはローカルURL経由でクラウドモデルも呼べるため、手元だけで扱う判定にはローカルのモデルを選ぶ。[Ollamaのクラウドモデルの説明](https://docs.ollama.com/capabilities/decision)。

## Jevと手元のモデルの比較

0.7.0 の既製の問い(wiki の差し込み先・矛盾、依頼に効く教訓、着手前に聞くべきだったか)を、この repo の wiki(16 記事)と人が答えた記録で、手元の標準(Ternary-Bonsai-27B)と Jev(jev-1.14.0)に同じ入力で聞いた(2026-10-23)。数は小さく、線は実際の使用で貯めて引く。「並べ方の良さ」は正しい組を間違った組より上に置いた割合(AUC)。

| 試験 | 手元の標準 | Jev |
|---|---|---|
| 差し込み先(古い版の草稿 4 本 × 既存 16 本)の並べ方 / 正しい先が 1 位 | 0.98 / 3 of 4 | 0.99 / 3 of 4 |
| 矛盾(手書きの食い違い 4 + 食い違わない 4) | 完全に分離(0.75〜0.90 対 0.21〜0.37) | 完全に分離(0.94〜0.98 対 0.09〜0.33) |
| 食い違う草稿の差し込み先が 1 位 | 3 of 4 | 4 of 4 |
| 依頼に効く教訓(10 依頼 × 16 本)上位 2 件に効く数 / 1 件も出せない依頼 | 6 / 3(文字一致は 7 / 3) | 10 / 1 |
| 同上、全組の並べ方 | 0.81 | 0.91 |
| 始める前に聞くべきだったか(人の答え 59 件)の並べ方 | 0.57 | 0.63 |
| 依頼の種類「ファイルが変わるか」(結果の正解 116 件)の並べ方 | 0.50 | 0.55 |
| 1 問の所要 | 約 0.6 秒(Mac、Metal) | 約 0.2 秒 |

差し込み先・矛盾・依頼に効く教訓は Jev の方がはっきり分かれ、依頼に効く教訓では今の文字一致も上回った。「ファイルが変わるか」はどちらも当てずっぽうに近く、依頼文からは決まらない。この表は推奨の根拠で、線の代わりではない。

## 構成

| 場所 | 役割 |
|---|---|
| `internal/spec` | Decision Spec の読み書き。較正の鍵 `Hash()`(質問・テンプレ・選択肢から。閾値・policy は含めない) |
| `internal/systemone` | System One 形式(`POST /v1/systemone`)の薄いクライアント。Jev / llama.cpp はベース URL の差だけ |
| `internal/decide` | 仕様 + 入力 → 帯。required 欠落は abstain、tainted は hosted を外す、**較正が無い判定は act を出さない** |
| `internal/calib` | Platt 較正(`calib.go`)、vector scaling(`vector.go`)、Brier / ECE / 信頼度図、正負の逆転検出 |
| `cmd/shikii` | CLI |
| `cmd/shikii-readout` | **普通の GGUF を System One にするアダプタ** `shikii-readout`。後ろの llama-server の `/apply-template` と `/completion` で選択肢の文字の確率を読む。推論も較正も持たない |
| `cmd/fake-systemone` | 鍵も GPU も無しで輪を回すための偽モデル |
| `decisions/*.yaml` | spec の見本(この repo。自分のプロジェクトでは `.shikii/decisions/` に写す) |
| `testdata/` | 作り物のラベル付きデータ(依頼文 62 件、不具合報告と問い合わせの各 32 組)と、CI 用の較正の裏付け(`store/labels/`) |

## score の帯: `thresholds.level`

順序尺度では隣り合うレベルに確率が割れるので、最上位の確率で帯を決めると実際より弱く見える。
`thresholds.level: k` を置くと、帯は **P(level ≥ k)** で決まる(top と期待値 score はそのまま出す)。
較正の鍵には入らない(閾値と同じ扱い)。報告の precision も「P(level ≥ k) と正解レベル ≥ k」で測る。

実測(Ternary-Bonsai-27B を `shikii-readout` で、`relatedness-level.yaml`、k=3「直接関連」):

| ペア | top の確率 | P(level ≥ 3) | 期待値 |
|---|---|---|---|
| 同一の不具合(別の言い方の 2 報告) | 0.544(→ ask 止まり) | **0.950** | 3.32 |
| 同じ画面の別の不具合 | 0.712 | 0.048 | 1.18 |
| 無関係 | 0.929 | 0.008 | 0.09 |

## 判定の定義ファイル(spec)の書き方

`shikii new <id>` が対話(またはフラグ `--type --question --option id=説明 --input name --pii true|false --send-to local`)で作る。項目の意味と書き方の全部は [skills/shikii/references/spec.md](../skills/shikii/references/spec.md)(`shikii init` が `~/.claude/skills/shikii/references/spec.md` にも置く)。

正解が `eval.min_n`(既定 50)に足りないうちは `calibrate --write` も `tune` も書かない。手持ちが少ないうちに試すなら、spec の `eval.min_n` を手持ちの件数に下げる。`--force` で書いた線は `check` が「calibrate なら書かなかった線」として落とすので、試しの道には使わない。

## serve の HTTP API

本文は JSON。`-H 'Content-Type: application/json'` が無いと 415 で蹴る。`spec` は `.shikii/decisions/<id>.yaml` の id。

| 口 | 体 | 返すもの |
|---|---|---|
| `POST /v1/decide` | `{"spec","inputs":{…},"tainted":false}` | 判定(band、top、p、trace_id、receipt) |
| `POST /v1/label` | `{"spec","trace_id","truth","weight":1,"source":"ui"}` | 付けた正解(文脈の段を通った判定は 2 段に付く) |
| `GET /v1/queue?spec=<id>` | | 未回答(ask と抜き取り。やり直した元は出ない) |
| `GET /v1/answered?spec=<id>&limit=30` | | 回答済み(人の正解だけ) |
| `POST /v1/redo` | `{"spec","trace_id"}` | 今の聞き方と線で同じ本文を再判定し、前と今を並べて返す(新しい記録、人の答えは写る) |
| `GET /v1/status` | | spec ごとの状態(線の有無、正解の数、人の言葉の質問) |
| `GET /v1/tune?spec=<id>` · `POST /v1/tune/run` · `POST /v1/tune/variant` · `POST /v1/tune/adopt` | `{"spec","propose"}` · `{"spec","name","question","options"}` · `{"spec","run","meaning_same":true}` | 聞き方の改善の最新と今の候補・裏で回す・人が書いた候補を足す(検証してから。採用はしない)・採否 |
| `GET /v1/wiki` · `POST /v1/wiki/claim` | | wiki の主張の一覧 · 主張の状態を変える(promote / delete。変えるのは人だけ) |
| `POST /v1/wiki/notebook` | `operation: query / ingest / capture / maintain / lint` | プロジェクトのwiki。原資料と記事の検索・登録・見直し・点検。教訓は仮説 |
| `POST /v1/review` | `operation: query / submit` | 会話と結果から仮採点。原文・対象・訂正例を取得し、根拠付きで保存。人の確認は抜き取りと保留 |
| `POST /v1/reflect` | `operation: query / submit`、`reviews` / `wiki` / `memory` / `maintenance` | 最大2原資料の採点・wiki整理・届いた記憶と訂正の観察、最大2記事の見直しをまとめて確認・保存。`complete`と各`error`を確認する |
| `GET /v1/specs` | | 読み込んでいる spec の一覧 |

```sh
curl -s localhost:8095/v1/decide -H 'Content-Type: application/json' -d '{"spec":"same-issue","inputs":{"report1":"…","report2":"…"}}'
curl -s localhost:8095/v1/label  -H 'Content-Type: application/json' -d '{"spec":"same-issue","trace_id":"<decide の返事の trace_id>","truth":"yes"}'
```

## choice の順序バイアス: `shikii bias`

```sh
bin/shikii bias .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8080 --rows 20 --permutations 5   # state は calibrate と同じ貯金(--store の正解の本文、または --labeled)
```

選択肢の並びを入れ替えて同じ state を聞き直し、最上位が変わる率(flip rate)と、位置ごとの確率のずれ
(spec の並びで聞いた時との差)を出す。flip rate > 0.10 か位置のずれの幅 > 0.10 で警告・exit 1。
生成しないモデルなので順列を回すのは安い(20 行 × 5 順列 = 120 問、Kev-4B で約 1 分)。

選択肢は spec に書いた並びのまま送る(`systemone.OrderedCriteria`。Go の map だと鍵がソートされてしまう)。

実測(Kev-4B、prompt-intent-v2): flip 0/100、位置のずれ −0.014〜+0.010。**順序バイアスは無視できる**。

## 契約の検査と劣化の監視

```sh
# CI で回す。較正の spec_hash が今の文言と合うか、ラベル上で act での precision が act 以上か。違反で exit 1
bin/shikii check .shikii/decisions/*.yaml

# 直近 N 件のラベルで、較正時の Brier から +0.05 以上悪化 / precision が act 未満なら警告して exit 1
bin/shikii drift .shikii/decisions/prompt-intent.yaml --model 'ternary-bonsai-27b-q2g64+state-last' --window 50
```

較正の方法は 3 つ。どれも `calibrate --write` が生成し、鍵は (spec_hash, model)。

| method | 対象 | 中身 |
|---|---|---|
| `platt` | noul | σ((Δ−bias)/T) |
| `vector` | choice / score | softmax((log p_k + b_k)/T)。選択肢ごとの癖(短い state で other に逃げる等)を直す |
| `identity` | どれでも | 定数なし。生の分布で act での precision が足りると検証した印(Jev のような較正済みモデル向け) |

## 実機で一周した例(2026-10-16、Kev-4B Q4_K_M、この repo の `decisions/prompt-intent-v2.yaml`)

```
calibrate --labeled testdata/prompt-intent-labeled.json --local http://127.0.0.1:8080 --tainted --write
  raw:        top-1 0.903  precision@act 1.000 recall@act 0.732
  vector:     T=0.449  biases={other:-0.53, operation:+0.59, …}
  5-fold CV:  precision@act 0.963 recall@act 0.912   → choose vector(recall が上がる)
  persisted 62 queried labels to .shikii
check decisions/*.yaml   → ok(stale なし、ラベル上の precision@act ≥ act)
drift … --model '…Kev-4B…' → ok(Brier 0.0469 → 0.0481、precision 0.976)
```

`calibrate --write` は `calibration` キーだけを差し替え、spec のコメントと文言はそのまま残す。

同じラベル付きファイルで再実行すると、store に残った同じ入力のラベルを再利用してモデルを呼ばない
(`reused 32 stored labels …; queried 0`)。モデルを止めたままでも較正・check・drift が回る。

はい/いいえ の 2 セット(same-issue: 不具合報告の重複 32 組、same-inquiry: 問い合わせの同一判定 32 組、日本語)では、
OpenJev は生の確率で足りて両方 `identity`、Kev-4B は Platt がわずかに勝って両方 `platt`(生 Brier 0.038 → CV 0.035、0.030 → 0.020)、
学習済みの決定モデルではない **Bonsai 2 27B**(普通の LLM。BonJev でロジットを読む)では Platt が大きく効いた(0.049 → 0.019、0.084 → 0.028)。
較正の要否は触れ込みではなく (質問, モデル) ごとの CV で決まる、という分かれ方が実機で出た。境界(w=0.5)のペアは
逆転の判定から外れる(学習には重み付きで入る)。モデル別の数字は [`docs/model-comparison.md`](model-comparison.md)。

## 棚卸し: `shikii status`

```sh
bin/shikii status --store .shikii .shikii/decisions/prompt-kind.yaml
# prompt-kind: records=1 labels=1
#   by source: outcome=1   by truth: !edit=1
#   ternary-bonsai-27b-q2g64+state-last @ 68ed82631e63f2a2   1  collecting (1/50)
```

spec ごとに記録とラベルの数、ラベルの出どころ(ask / outcome / file)と中身の偏り、(model, spec_hash) ごとの件数と状態
(`calibrated: vector` / `ready to calibrate` / `collecting (n/min_n)` / `stale hash`)を出す。運用で最初に見る画面。

0.6.0では `person's work` の行が付く。今の規則で人が答える判定(結果が答えない判定の迷い、線のある自動判定の抜き取り、AIの仮採点の抜き取りと保留)の数を、全体と直近7日の1000判定あたりで示し、正解を人と結果のどちらが決めたかを分けて数える。`rungs` の `left to a person` は迷った数で、結果が答える判定の迷いは人を待たない。人の手間を減らす変更の効果は、この行の前後で比べる。

## 掃除: `shikii gc`

記録(`records/`)は判定のたびに増える。ラベルは較正の材料なので捨てないが、ラベルの付かなかった古い記録は要らない。

```sh
bin/shikii gc --store .shikii                 # 表示だけ(既定: 30d より古くラベルの無い記録が候補)
# prompt-kind: would remove 120 unlabeled record(s) older than 30d; keep 48 labeled, 15 recent, 0 unparsable
# dry run: nothing was changed. Re-run with --apply to remove them.
bin/shikii gc --store .shikii --apply         # 実際に消す
bin/shikii gc --older-than 1w .shikii/decisions/prompt-intent-v2.yaml       # spec と年齢を指定(d / w / h。0 なら年齢を問わない)
```

消すのは「`--older-than` より古く、ラベルが 1 つも付いていない記録」だけ。ラベル付きの記録・ラベル・JSON として読めない行は年齢に関係なく残す。
書き換えは同じディレクトリに書いてから rename で置き換えるので、途中で止まっても元のファイルは壊れない。
テストは 7 つの変異(ラベル付きも消す・表示だけのはずが書き換える・読めない行を捨てる・年齢の向きが逆・既定で消す・spec 無しで store を見ない・負の年齢を通す)で全部 RED を確認してから GREEN(2026-10-17)。

`make status` / `make drift` / `make check` / `make gc` / `make install` / `make server` も用意した(`Makefile`)。

## 2 分で一周(鍵も GPU も無し)

```sh
scripts/demo.sh                  # 偽モデル(cmd/fake-systemone)で decide → label → calibrate → check → drift → status
scripts/demo.sh --overconfident  # 確率を端に寄せた偽モデル。較正が効く様子を見る
```

## 接続先

`--hosted` は URL にパスがあればそのまま使う。鍵は環境変数 `DECISIONS_API_KEY`(または `TYPESAFE_API_KEY`)。

| 先 | 例 |
|---|---|
| TypeSafe Jev | `--hosted https://api.typesafe.ai --model jev-latest` |
| OpenRouter Decisions API | `--hosted https://openrouter.ai/api/alpha/decisions --model typesafe/jev-1.13` |
| llama.cpp(ローカル) | `--local http://127.0.0.1:8080` |
| **`shikii-readout`**(普通の GGUF。後ろは任意の llama-server) | `--local http://127.0.0.1:8090`([`integrations/readout.md`](../integrations/readout.md)) |
| BonJev(同じ原理の外部実装。Bonsai 専用) | `--local http://127.0.0.1:8083`([`integrations/bonjev.md`](../integrations/bonjev.md)) |
| 偽モデル | `--local http://127.0.0.1:8099`(`make fake`) |

## 普通の GGUF を本家 llama.cpp だけで決定モデルにする: `shikii-readout`

**fork も Rust も学習も要らない。** `brew install llama.cpp` の llama-server に好きな GGUF を載せ、前にアダプタを 1 つ置けば、
`shikii` からは Kev や Jev と同じ決定モデルに見える(2026-10-17、Ternary-Bonsai-27B Q2_g64 を brew の llama.cpp 0.7.0、build 11669 で確認)。

本家 llama.cpp の `/v1/systemone` は学習済みの決定モデル(Kev / OpenJev / Clef …)しか受けない。普通のチャット用 GGUF を使うには、
選択肢に A, B, … を割り当てて 1 トークンだけ回し、その文字の確率を読めばよい(had-scorer と同じ原理)。それを System One 形式で出すのが
`shikii-readout`。推論は持たず、後ろの llama-server(本家でも PrismML の fork でも)に HTTP で聞くだけ。

```sh
llama-server -m <any>.gguf --port 8086 --alias my-model      # 本家でも fork でも
bin/shikii-readout --upstream http://127.0.0.1:8086 --port 8090
bin/shikii decide .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8090 -i message='README の誤字を直して'
```

- 起動時に `/tokenize` で全ラベルが 1 トークンかを確かめ、違えば起動しない
- `/apply-template` でそのモデルの chat template を使い、Qwen3 系の `<think>` は `enable_thinking: false` で潰す(受けない upstream には送り直す)
- 返す確率は**素の softmax**。較正はしない。応答の `model` は llama-server の `--alias` で決まり、それが較正の鍵になる
- 上位 `--n-probs` に入らなかった選択肢は床の確率(1e-6)を得る(0 にすると較正の log が壊れる)
- state は prompt の最後に置き、spec ごとの接頭辞(質問・選択肢)を先に 1 回流してチェックポイントを作る。再帰層を持つ Qwen3.5/3.8 系
  (Bonsai の土台)はこれが無いと `cache_prompt` が効かない。1 問 4 秒 → 0.6〜0.8 秒(27B、Metal)

実測は [`integrations/readout.md`](../integrations/readout.md)。

## 接続例

Claude CodeとCodexは、依頼の前と作業の後のhook(Claude Codeはサブエージェントの開始にも。計3つ、Codexは2つ)で会話の記録・記憶の参照・既製の問いの判定を行う。初期化したプロジェクトの `.shikii/` へ会話・仮採点・正解・線を保存し、共通の `~/.shikii/agent` には型・道具・仕事の入口を置く。線もプロジェクトごとで、別の仕事の実績は持ち込まない。

会話の整理と範囲・完了の仮採点は通常のエージェントが担当し、人は抜き取りと保留を確認する。仮採点は正解・較正とは別。判定用モデルが使えなくても、会話の記録と参照は働く。手元のモデルは `:8080`、共通画面は `:8096`。ツールを止めず、実行許可を出す仕組みではない。[hookの詳しい説明](../integrations/claude-code.md)。

## まだ無いもの

- 等張回帰
- 鍵の取り出し(環境変数で渡す。このプロセスはキーチェーンに触らない)
- `ask` の答えを拾う口は `shikii serve` の画面(ボタンで `/v1/label`)で済んだ。hook の判定も Stop が件数と URL を 1 行出し、同じ画面(:8096)で答える

## プロジェクトごとの有効化

インストール後、利用するプロジェクトで `shikii --init`（`shikii init`も同じ）を一度実行する。エージェントに「このプロジェクトでshikiiを使いたい」と頼んでもよい。既製の問い(hookの4問とwikiの2問)・wiki・MCP・作法と画面への入口を揃え、最後に `.shikii/project.json` を書く。hookはこの印がある場所だけで動き、未初期化の場所には保存先もGitの除外も作らない。サブフォルダからも同じプロジェクトルートを使い、再初期化しても既存の設定・会話・正解を保持する。

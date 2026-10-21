# shikii(閾) — 詳しい使い方(日本語)

名前は閾値の「閾」。確率を act / ask / discard の帯に切る閾が、この道具の芯だから。コマンドの名前も `shikii`。
モジュールは `github.com/hayaenfj/shikii`、ライセンスは Apache-2.0 OR MIT（どちらかを選択）([`LICENSE`](../LICENSE))。英語の README は [`../README.md`](../README.md)、日本語の短い版は [`readme/README_ja.md`](readme/README_ja.md)。

設計は [`docs/decision-ops-framework.md`](decision-ops-framework.md)、止め方・戻し方・残っているものは [`docs/operations.md`](operations.md)。
最初の試作は **spec / decide / calibrate** の 3 つだけだった(設計 §11)。今は label / check / drift / bias / status / gc / serve / mcp / new / up / doctor も動く。この手引きは CLI の中身を順に説明する。

```sh
go build -o bin/shikii ./cmd/shikii

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
bin/shikii calibrate .shikii/decisions/same-issue.yaml --model 'ggml-org/Kev-4B-GGUF:Q4_K_M'
```

## ローカルの決定モデル(llama.cpp)

```sh
brew install llama.cpp            # 0.6.0 (build 11429) で /v1/systemone が使えた
llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M --port 8080 --host 127.0.0.1
```

モデル名は応答の `model`(例 `ggml-org/Kev-4B-GGUF:Q4_K_M`)が較正の鍵になる。`--model` で送った名前ではない。
Kev-4B は英語のみと表記されているが、v2 の文言なら日本語の依頼でも top-1 0.913(v1 の文言では `other` に逃げて 0.68 だった。文言の方が効く)。

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
| `POST /v1/wiki/notebook` | `operation: query / ingest / lint` | 会話から育つ wiki。出典付きの教訓と原文の検索・取り込み・点検。教訓は仮説 |
| `POST /v1/review` | `operation: query / submit` | 会話と結果から仮採点。原文・対象・訂正例を取得し、根拠付きで保存。人の確認は抜き取りと保留 |
| `POST /v1/reflect` | `operation: query / submit`、`reviews` / `wiki` | 最大2会話の採点とwiki整理をまとめて確認・保存。`complete`と各`error`を確認する |
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
bin/shikii drift .shikii/decisions/prompt-intent.yaml --model 'ggml-org/Kev-4B-GGUF:Q4_K_M' --window 50
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
bin/shikii status --store ~/.shikii/agent ~/.shikii/agent/decisions/prompt-kind.yaml
# prompt-kind: records=1 labels=1
#   by source: outcome=1   by truth: !edit=1
#   ggml-org/Kev-4B-GGUF:Q4_K_M @ 68ed82631e63f2a2   1  collecting (1/50)
```

spec ごとに記録とラベルの数、ラベルの出どころ(ask / outcome / file)と中身の偏り、(model, spec_hash) ごとの件数と状態
(`calibrated: vector` / `ready to calibrate` / `collecting (n/min_n)` / `stale hash`)を出す。運用で最初に見る画面。

## 掃除: `shikii gc`

記録(`records/`)は判定のたびに増える。ラベルは較正の材料なので捨てないが、ラベルの付かなかった古い記録は要らない。

```sh
bin/shikii gc --store ~/.shikii/agent                 # 表示だけ(既定: 30d より古くラベルの無い記録が候補)
# prompt-kind: would remove 120 unlabeled record(s) older than 30d; keep 48 labeled, 15 recent, 0 unparsable
# dry run: nothing was changed. Re-run with --apply to remove them.
bin/shikii gc --store ~/.shikii/agent --apply         # 実際に消す
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

Claude Code と Codex の hook(`shikii hook install`): AI 自身の考え方を、考える前(この依頼で結局ファイルを変えるか、着手前に進め方を合わせるべきか)と考え終わった後(変えたものは頼まれた範囲か、頼まれたことを終えたか)の 2 点で測る。ツール呼び出しごとの hook は無い(コマンドの分類は考え方ではなく雑音で、許可は Claude Code の仕事)。置き場は `~/.shikii/agent`(基本構成。プロジェクトをまたいで 1 つ、線も 1 本。記録に仕事の印 `project`(repo のルートの名)が付き、画面は仕事ごとに絞り込め、`status` は仕事ごとに数える)、モデルは :8080、答え合わせの画面は :8096。どちらも無ければ hook が裏で起こす(`shikii up`、`shikii serve --store ~/.shikii/agent --port 8096`。10 分に 1 回まで)。install.sh が `shikii hook install --yes`(hook・skill・モデル)まで流すので、人が打つのは 1 行。spec に `variants:`(候補の言い方)があれば、人の正解が 50 件増えるごとに Stop が `tune` を裏で回す(`--write` 無し。採用は人。結果は `wiki/runs/`)。線が無いうちは記録だけ(影の期間)。線が引けたら、方針合わせが要る依頼は AI への文脈、終えていない・範囲の外に見えるターンは人への 1 行、迷った判定は「あとで答える → URL」を端末に 1 行、ターンの終わりに答え待ちの件数を 1 行。ツールは止めず、許可の判定も出さない。失敗しても止めない(hook.log に理由)。帯 → 振る舞いの表と中の形は [`integrations/claude-code.md`](../integrations/claude-code.md)。
他のエージェントに付けるときの最小手順もそこに。

## まだ無いもの

- 等張回帰
- 鍵の取り出し(環境変数で渡す。このプロセスはキーチェーンに触らない)
- `ask` の答えを拾う口は `shikii serve` の画面(ボタンで `/v1/label`)で済んだ。hook の判定も Stop が件数と URL を 1 行出し、同じ画面(:8096)で答える

## プロジェクトごとの有効化

インストール後、利用するプロジェクトで `shikii --init`（`shikii init`も同じ）を一度実行する。エージェントに「このプロジェクトでshikiiを使いたい」と頼んでもよい。既製の4問・wiki・MCP・作法と画面への入口を揃え、最後に `.shikii/project.json` を書く。hookはこの印がある場所だけで動き、未初期化の場所には保存先もGitの除外も作らない。サブフォルダからも同じプロジェクトルートを使い、再初期化しても既存の設定・会話・正解を保持する。

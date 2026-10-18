# 普通の GGUF を本家 llama.cpp だけで決定モデルにする（`shikii-readout`）

学習済みの決定モデルが無くても、手元の GGUF を `shikii` の判定に使う経路。fork も Rust も学習も要らない。

```
shikii ──/v1/systemone──▶ shikii-readout ──/apply-template, /completion──▶ llama-server(本家でも fork でも) + 任意の GGUF
                                 選択肢に A, B, … を振り、1 トークンだけ回して文字の確率を読む
```

## 入れ方

```sh
# --alias が応答の model の元(アダプタが "+state-last" のような配置名を付ける) = 較正の鍵
# -fa on(flash attention)、-ctk/-ctv q8_0(KV を 8bit)、-np 1(スロットを割らない)は速さのため。make server-gguf GGUF=… ALIAS=… でも同じ
llama-server -m <model>.gguf --port 8086 --host 127.0.0.1 -c 4096 -ngl 99 -fa on -ctk q8_0 -ctv q8_0 -np 1 --alias <name>
bin/shikii-readout --upstream http://127.0.0.1:8086 --port 8090                   # 起動時に /tokenize でラベルを検査
bin/shikii decide decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8090 -i message='README の誤字を直して'
scripts/eval-model.sh http://127.0.0.1:8090 <name>                                   # 他のモデルと同じ 5 セット
```

止めるのは `pkill -x shikii-readout`（llama-server は別）。

## 中身（`cmd/shikii-readout/main.go`、約 600 行、依存は標準ライブラリと `internal/systemone` の並び保持だけ）

| 段 | 何をするか | 壊れ方の検査 |
|---|---|---|
| 要求の解釈 | `questions` は名前→質問の object。choice の criteria は **JSON の並びのまま**読む（Go の map に入れない）。score は level の配列、noul は criteria 無しか true/false の説明 | 2〜26 択以外・不明な型・instructions 無しは 422 |
| プロンプト | system + user。質問、`A. id: 説明` の列、**最後に** `<state>…</state>`、「Reply with one letter.」。state を最後に置くので、同じ spec の全問で先頭が共通になり llama-server の `cache_prompt` が効く。`--state-first` で旧配置。配置名は応答の `model` に `+state-last` / `+state-first` として付く（並びが変われば確率が変わるので較正の鍵も変える） | テストが提示順・配置・接尾辞を固定 |
| 配置の切替（`layout`） | 要求の `layout`（spec の `layout:` から来る）で要求ごとに配置を変える: `state-last`（既定）/ `state-first` / `plain`。plain は system 無し、質問、`- id: 説明` の列、`Input:` の後ろに state、「Answer with the option id only.」で、文字ではなく **各 id の最初のトークン**（`/tokenize` の `with_pieces`）の確率を読む。2 つの id が同じ最初のトークンなら 422。既定以外の配置の応答 `model` は `<土台>+<配置>`（較正の鍵が分かれる）。知らない名前は 422 | テストが plain の並び・id の読み・model 名・422 を固定（変異: 常に文字で読む → 落ちる） |
| chat template | upstream の `/apply-template` に `chat_template_kwargs: {enable_thinking: false}` を付けて渡す。4xx で蹴られたら外して送り直し、以後は付けない | 送り直しの有無をテスト |
| 接頭辞の warm-up | Bonsai の土台 Qwen3.5/3.8 系は再帰層（Gated DeltaNet）を持ち、KV を途中まで巻き戻せない。接頭辞のちょうど末尾にチェックポイントが無いと `cache_prompt` が効かず毎回全部 prefill する（実測 `cache_n=0`、1 問 4 秒）。そこで spec の接頭辞（`<state>` の手前まで）が変わった時だけ、それを `n_predict: 0` で先に流してチェックポイントを作る。応答の `cache_n` が接頭辞に届かなければ次回また流す | 新しい接頭辞で 1 回・同じ接頭辞で 0 回・cache miss 後に再度、をテスト |
| 読み出し | `/completion` を `n_predict: 1`、温度 1、切り捨て無し、`n_probs` で回し、ラベル文字の確率を拾って正規化。上位に入らなかった文字は床 1e-6 | 床と合計 1 をテスト |
| 応答 | llama.cpp の決定モデルと同じ形（choice: probabilities/choice/confidence、score: "0".."n-1" + legend + 期待値、noul: P(yes)） | 型ごとにテスト |
| 起動時 | `/v1/models` から model 名、`/tokenize` で 26 文字が全部 1 トークンか | 2 トークンの文字があれば起動しない |

較正は持たない。返す確率は文字の softmax そのままで、`shikii calibrate` が (spec_hash, model) ごとに当てる。

テスト 9 本は、9 つの変異（criteria をソート・noul を P(no) に・床を 0 に・送り直しを止める・score の見出しを変える・
配置を逆にする・model の接尾辞を落とす・warm-up を送らない・cache miss で再 warm-up しない）でそれぞれ RED になることを確認してから GREEN（2026-10-17）。

### 速さ（2026-10-17、同じ 4 問を続けて。1 問目はシェーダ構築と warm-up を含む）

| 構成 | 1 問目 | 2 問目以降 |
|---|---|---|
| state-first、warm-up 無し（最初の版） | ≈ 5 秒 | ≈ 4 秒（毎回 280 トークン prefill、`cache_n=0`） |
| state-last + warm-up、fork の llama-server（Bonsai 2 PQ2_0）、`-fa on -ctk/-ctv q8_0 -np 1` | 4.8 秒 | **0.83 秒** |
| state-last + warm-up、**本家** llama.cpp 0.7.0（Ternary-Bonsai-27B Q2_g64）、同フラグ | 4.0 秒 | **0.58 秒** |

確率は配置と warm-up の前後で同じ値（0.999 / 0.996 / 0.997 / 0.998）。速くなっただけで答えは変わっていない。
本家の方が速いのは、この Mac では Prism fork の PQ2_0 の Metal カーネルより本家の Q2_0 の方が prefill が速いため（66 vs ≈ 130 tok/s）。

## 実測（2026-10-17、同じ spec・同じラベル。比較表は `../docs/model-comparison.md`）

### Ternary-Bonsai-2-27B PQ2_0（PrismML fork の llama-server、Metal）

| セット | 生 | 較正後 CV | 選ばれた方法 | BonJev（同じモデル、別実装） |
|---|---|---|---|---|
| prompt-intent-v2 / 62 | top-1 **0.968**、prec@act 0.983 / rec 0.983 | vector 0.967 / 0.967 | **identity** | 0.968、生 1.000 / 0.900 → vector 0.982 / 0.949 |
| ja 23 | top-1 1.000、rec@act 0.957 | 1.000 / 0.955 | identity | 1.000 |
| en 39 | top-1 0.949 | 1.000 / 1.000 | identity | 0.949 |
| はい/いいえ 32 組 | 当時の題材は差し替えたので数字は載せない。差し替え後は下の表 | | | 同左 |
| 速さ | 1 問 ≈ 5 秒（156 問 13 分、prefix cache は system までしか効かない） | | | ≈ 2.5 秒 |

読み方:

- **順位は BonJev と同じ**（top-1 が prompt-intent の 3 セットとも一致）。prompt の工夫や内蔵の較正が無くても、文字の読み出しそのものは同じ所に着く
- **確率は BonJev より鋭い**（生の recall@act 0.983 vs 0.900）。文字の softmax は端に寄るので、act の閾値 0.75 に生で届く。
  代わりに はい/いいえ では分離が極端すぎて、`calib.Fit` の素の Newton 法が発散した（T=5e-7、in-sample の Brier 0.107）。
  CV がそれを捕まえて identity に戻していたが、これは最適化の不具合で、同日に直線探索付きに直した（その 32 件がテストになっている）
- **較正の要否はモデルではなく「モデル + 読み出し方」で決まる。** 同じ Bonsai 2 でも BonJev 経由では Platt が勝ち、アダプタ経由では identity。
  鍵が (spec_hash, model) で model が実装ごとに違う名前になる設計が、ここで意味を持つ

### Ternary-Bonsai-27B Q2_g64（**本家** llama.cpp 0.7.0 build 11669、brew、Metal。fork 無し）

| セット | 生 | 較正後 CV | 選ばれた方法 |
|---|---|---|---|
| prompt-intent-v2 / 62 | top-1 0.919、prec@act 0.966 / rec 0.982 | vector 0.934 / 1.000 | vector（recall が上がる） |
| ja 23 | top-1 0.870、prec@act 0.952 / rec 1.000 | 0.909 / 1.000 | identity |
| en 39 | top-1 0.949 | 1.000 / 0.947 | identity |
| はい/いいえ 32 組 | 当時の題材は差し替えたので数字は載せない。差し替え後は下の表 | | |
| 速さ | 1 問 ≈ 5 秒（156 問 13.5 分、state-first 配置、fa/KV 既定） | | |

読み方:

- **本家 llama.cpp だけで普通の GGUF が決定モデルになった**、という経路の証明がこの表。精度は Kev-4B と同じ（all 0.919）で、
  日本語は 0.870 と Bonsai 2 の 1.000 に劣る（土台が Qwen3.6 と Qwen3.8 の差）
- Bonsai 2 に本家対応の版が出れば、こちらに差し替えるだけで日本語も取れる見込み

### 配置を state-last + warm-up にした後（2026-10-17、同じセット、`-fa on -ctk/-ctv q8_0 -np 1`）

| セット | Bonsai 2 PQ2_0（fork） | Ternary-Bonsai-27B Q2_g64（**本家 0.7.0**） |
|---|---|---|
| prompt-intent-v2 / 62 top-1 | 0.968（state-first と同じ）、生 prec@act **1.000 / rec 1.000** | **0.968**（state-first は 0.919）、生 1.000 / 0.983 |
| ja 23 | 1.000 | **1.000**（state-first は 0.870） |
| en 39 | 0.949 | 0.949 |
| same-issue / same-inquiry 32 組 Brier（生 → Platt CV） | 0.024 / 0.059 → **0.016 / 0.014** | 0.048 / 0.033 → **0.024 / 0.020**。どちらも正負は完全分離 |
| 選ばれた方法 | 全部 identity | 全部 identity |
| 156 問の合計 | **1 分 53 秒**（state-first は 13 分 0 秒） | **2 分 13 秒**（state-first は 13 分 35 秒） |

読み方:

- **state を最後に置いたら、本家側の 27B が Kev-4B 並み（0.919）から Bonsai 2 並み（0.968）に上がり、日本語は 0.870 → 1.000 になった。**
  速さのための配置変更が、精度も動かした。読み出しの対象（state）が答えの直前にある方が、普通の LLM には効く
- 同じモデル・同じ重みでも prompt の配置で top-1 が 5 ポイント動く。だから配置名を応答の `model` に付けて較正の鍵を分けている
- 順位の改善は較正では得られない種類のもの（設計 §7.2）。普通の GGUF を使うなら、較正より先に配置と文言を測る

## 注意

- ラベルは裸の 1 文字（A〜Z）。chat template が改行で終わる前提で、先頭トークンが裸の文字になる。数字は多くのトークナイザで 2 トークンになるので使わない
- 選択肢の並びで確率が変わる（位置バイアス）可能性は学習済みモデルより高い。`shikii bias` で測ってから補正を決める
- `n_probs` の上位にラベルが入らない時は床の確率になり、ログに出る。選択肢が多い spec では `--n-probs` を上げる
- 普通の LLM なので、質問文と選択肢の説明の書き方で順位が動く。順位の誤りは較正では直らない（設計 §7.2）

# 接続例: 普通の LLM をロジット読み出しで使う（BonJev + Bonsai 2 27B）

学習済みの決定モデル（Kev / OpenJev）ではなく、普通のチャット LLM を決定モデルとして使う例。
本家 llama.cpp 0.6.0 の `/v1/systemone` は普通の GGUF を `501 This model is not a decision model` で蹴るので、
選択肢の文字の logit を 1 回の forward で読んで softmax にし、System One 形式で返す [BonJev](https://github.com/ChezRD/bonjev)（Rust、MIT）を間に置く。
`shikii` 側の変更は無い。`--local` を向けるだけ。

## 入れ方（macOS Apple Silicon、2026-10-17 に確認）

```sh
git clone --depth 1 https://github.com/ChezRD/bonjev ~/git/third-party/bonjev
cd ~/git/third-party/bonjev
sh scripts/fetch-prism.sh cpu      # PrismML の llama.cpp fork のビルド済みライブラリ(macOS 版には Metal も入っている)
cargo build --release               # 約 40 秒
hf download prism-ml/Ternary-Bonsai-2-27B-gguf Ternary-Bonsai-2-27B-PQ2_0.gguf   # 7.2GB、Apache 2.0
./target/release/bonjev serve --model bonsai2 --no-vision --ctx 4096 --threads 8 --port 8083
```

```sh
bin/shikii decide decisions/prompt-kind.yaml --local http://127.0.0.1:8083 --tainted -i request='fix the typo in README'
#  model=ternary-bonsai-2-27b  top=edit p=0.950  band=ask   ← 較正が無いので act は出ない(設計どおり)
scripts/eval-model.sh http://127.0.0.1:8083 bonsai2       # Kev-4B / OpenJev と同じ 4 セット
```

止めるのは `pkill -x bonjev`。モデルは `~/.cache/huggingface/hub/models--prism-ml--Ternary-Bonsai-2-27B-gguf/` に残る。

## 形の対応

| System One | BonJev | 備考 |
|---|---|---|
| `questions` は名前をキーにした object | 同じ | 配列ではない |
| choice の `criteria` は id → 説明 | 同じ。選択肢は W, X, Y, Z, A, … の 1 文字に割り当てて logit を読む | 並びは request の順を保つ |
| score の `criteria` は level の配列 | 同じ | |
| noul は `criteria` 無しでよい | 無ければ true / false の既定を使う | `true` / `false` 以外のキーは 422 |
| 応答の `model` | 起動時に載せたモデル id（`ternary-bonsai-2-27b`） | 較正の鍵になる。request の `model` は無視される |
| `probabilities` | 文字の logit の softmax（+ BonJev 内蔵の temperature / bias） | **較正されていない**。`calibrate` の出番 |

## 実測（2026-10-17、Ternary-Bonsai-2-27B PQ2_0、Metal、1 問 ≈ 2.5 秒）

| セット | 生 | 較正後（5-fold CV） | 選ばれた方法 |
|---|---|---|---|
| prompt-intent-v2 / 62 | top-1 0.968、precision@act 1.000 / recall 0.900 | precision 0.982 / recall 0.949 | vector |
| prompt-intent-v2 / ja 23 | top-1 1.000、recall@act 0.826 | 0.950 / 0.905 | vector |
| prompt-intent-v2 / en 39 | top-1 0.949 | 1.000 / 0.923 | identity（生で足りる） |
| same-issue / same-inquiry（noul、各 32 組） | 生 Brier 0.049 / 0.084、正負は分離 | Platt CV **0.019 / 0.028** | **platt** |

Kev-4B（学習済み、較正済み）では Platt が CV で負けて identity になったが、Bonsai 2 では Platt が勝つ。
「較正が要るモデル」（設計 §7.1）の最初の実例で、フレームワークの較正が初めて値を生んだ測定でもある。

## 注意

- 必要なのは Prism の fork。本家 llama.cpp の `ggml-*` と混ぜない（fork の README の警告）
- 1-bit の Q1_0 は BonJev 非対応。ternary の PQ2_0 だけ
- 確率は BonJev の内蔵較正を通っているが、それはモデル全体の温度であって **spec ごとの較正ではない**。(spec_hash, model) の鍵で `calibrate` を当て直す前提は変わらない

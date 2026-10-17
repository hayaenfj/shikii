# 比較ベンチ: 測った聞き方 + 較正 / RAG / LoRA

README の仮説「型の付いた判定なら、測った聞き方と較正の組は、同じ土台のモデルで、微調整（LoRA）よりずっと少ない例で使える精度に届き、検索で文脈を足す方法（RAG）より確率が正直かもしれない」を確かめる場所。**まだ主張ではない。** ここにある数字は 1 課題・62 件・2 モデルの観察。

## 揃えるもの

- 課題: `prompt-intent`（コーディングアシスタントへの依頼文の 5 択）
- 正解: `testdata/prompt-intent-labeled.json` の 62 件（英 39・日 23、人が付けた）
- モデル: 3 つの腕で同じもの。較正は `calibrate` の 5 分割（4 つで当てて 1 つで試す）
- 物差し: top-1、Brier（top）、act 閾値（0.75）での precision / recall（較正後は 5 分割 CV の値）

## 3 つの腕

| 腕 | 何をするか | 回し方 |
|---|---|---|
| **A. 測った聞き方 + 較正** | `tune` で選んだ v2 の文言で 1 問聞き、`calibrate` が線を引く。shikii がいつもやること | `scripts/bench/run.sh <url> <name>` |
| **B. RAG 風** | 同じ質問に、似た例 5 件（正解付き）を本文の前に足す。似ている度は文字 2-gram の Jaccard、検索の元はその行以外の 61 件（leave-one-out） | 同上（`scripts/bench/rag-rows.py` が材料を作り、`scripts/bench/prompt-intent-rag.yaml` で聞く） |
| **C. LoRA** | 土台のモデルを 62 件で微調整し、選択肢のトークンの確率を読む | 未実施（下の手順） |

B の注意: 検索の元が leave-one-out なので、較正の 5 分割（試す 1 割は当てる側に入らない）より RAG に少し有利。揃えるには検索の元も分割ごとに切る必要があり、まだやっていない。

## 結果（2026-10-18）

| モデル | 腕 | top-1（生） | Brier（生） | 較正後 CV prec / rec @act | 選ばれた線 |
|---|---|---|---|---|---|
| Ternary-Bonsai-27B（`shikii-readout` state-last、本家 llama.cpp） | A | 0.968 | 0.0136 | 1.000 / 0.951 | identity（生で 1.000 / 0.983） |
| 同 | B | **0.984** | **0.0092** | 1.000 / 0.951 | identity（生で 1.000 / 0.967） |
| Kev-4B Q4_K_M | A | 0.919 | 0.0864 | 0.927 / 0.895 | vector |
| 同 | B | 0.919 | **0.0734** | **0.981** / 0.897 | vector |

読み方:

- 27B では B が 62 件中 1 件多く当て、Brier も良い。差は 1 件分で、B に有利な条件（上の注意）の中。「RAG より確率が正直」は**この課題・このモデルでは言えない**
- 4B では top-1 は同じ、B の方が較正後の precision が高い（0.927 → 0.981）。小さいモデルほど例が効く
- どちらの腕も較正は同じ道具で引けている。RAG は shikii の外の仕組みではなく、`state.template` に例を入れる聞き方の 1 つとして同じ貯金で測れる。つまり A と B は対立ではなく、`tune` の候補として並べられる（例を足す template の variant）
- 費用: B は本文が 5 例分長い。27B readout で 1 問 0.58 秒 → 約 1.1 秒

## C（LoRA）の手順（未実施）

土台は学習できる形（bf16 / 4bit の safetensors）が要る。三値の 27B は学習できないので、同じ土台で 3 腕を揃えるなら Qwen3-4B（Kev-4B の土台）のような普通のモデルを使い、A と B もそのモデルで回し直す。

1. 土台: `Qwen/Qwen3-4B`（HF、約 8 GB）。A・B 用に同じモデルの GGUF を `shikii up --gguf` で `shikii-readout` 越しに
2. 学習データ: 62 件を 5 分割。各分割で 4/5（約 50 件）を `{"messages":[{"role":"user","content":"<質問 + 本文>"},{"role":"assistant","content":"<正解の id>"}]}` の形に
3. 学習: `mlx_lm.lora --model Qwen/Qwen3-4B --train --data <fold>/ --iters 200 --batch-size 4 --adapter-path adapters/<fold>`（Apple silicon、1 分割あたり数分）
4. 評価: 残り 1/5 の各行で、質問 + 本文の次トークンの対数確率を 5 つの選択肢の id の最初のトークンで読み、正規化して分布にする（生成はしない）。これを 5 分割分つなげて 62 件の raw にする
5. 比較: その raw を `{"inputs":{"message":…},"raw":{…},"truth":…}` の形で labeled file に書き、`shikii calibrate --labeled` で A・B と同じ物差し（top-1、Brier、CV prec/rec）を出す

やるなら機械の時間は 1 時間ほど、ダウンロードは 8 GB。結果が出たら上の表に行を足す。

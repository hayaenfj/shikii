**English** · [日本語](bonjev.ja.md)

# Connection example: using an ordinary LLM through logit readout (BonJev + Bonsai 2 27B)

An example of using an ordinary chat LLM as the decision model, instead of a trained decision model (Kev / OpenJev).
Upstream llama.cpp 0.6.0's `/v1/systemone` rejects an ordinary GGUF with `501 This model is not a decision model`,
so [BonJev](https://github.com/ChezRD/bonjev) (Rust, MIT) sits in between: it reads the logits of the option letters in one forward pass, applies softmax, and answers in the System One format.
Nothing changes on the `shikii` side. You only point `--local` at it.

## Installing (macOS Apple Silicon, checked on 2026-10-17)

```sh
git clone --depth 1 https://github.com/ChezRD/bonjev ~/git/third-party/bonjev
cd ~/git/third-party/bonjev
sh scripts/fetch-prism.sh cpu      # prebuilt libraries of PrismML's llama.cpp fork (the macOS build includes Metal too)
cargo build --release               # about 40 seconds
hf download prism-ml/Ternary-Bonsai-2-27B-gguf Ternary-Bonsai-2-27B-PQ2_0.gguf   # 7.2GB, Apache 2.0
./target/release/bonjev serve --model bonsai2 --no-vision --ctx 4096 --threads 8 --port 8083
```

```sh
bin/shikii decide .shikii/decisions/prompt-kind.yaml --local http://127.0.0.1:8083 --tainted -i request='fix the typo in README'
#  model=ternary-bonsai-2-27b  top=edit p=0.950  band=ask   ← no calibration yet, so act does not appear (as designed)
scripts/eval-model.sh http://127.0.0.1:8083 bonsai2       # the same 5 sets as Kev-4B / OpenJev
```

Stop it with `pkill -x bonjev`. The model stays in `~/.cache/huggingface/hub/models--prism-ml--Ternary-Bonsai-2-27B-gguf/`.

## How the shapes map

| System One | BonJev | Notes |
|---|---|---|
| `questions` is an object keyed by name | same | not an array |
| choice `criteria` is id → description | same. The options are assigned one letter each, W, X, Y, Z, A, …, and their logits are read | keeps the request's order |
| score `criteria` is an array of levels | same | |
| noul may omit `criteria` | without it, the default true / false is used | keys other than `true` / `false` give 422 |
| the response's `model` | the model id loaded at startup (`ternary-bonsai-2-27b`) | becomes the calibration key. The request's `model` is ignored |
| `probabilities` | softmax of the letter logits (+ BonJev's built-in temperature / bias) | **not calibrated**. This is where `calibrate` comes in |

## Measured (2026-10-17, Ternary-Bonsai-2-27B PQ2_0, Metal, ≈ 2.5 seconds per question)

| Set | Raw | After calibration (5-fold CV) | Method chosen |
|---|---|---|---|
| prompt-intent-v2 / 62 | top-1 0.968, precision@act 1.000 / recall 0.900 | precision 0.982 / recall 0.949 | vector |
| prompt-intent-v2 / ja 23 | top-1 1.000, recall@act 0.826 | 0.950 / 0.905 | vector |
| prompt-intent-v2 / en 39 | top-1 0.949 | 1.000 / 0.923 | identity (raw is enough) |
| same-issue / same-inquiry (noul, 32 pairs each) | raw Brier 0.049 / 0.084, positives and negatives separate | Platt CV **0.019 / 0.028** | **platt** |

On the replaced material Platt wins slightly even for Kev-4B (0.038→0.035), but for Bonsai 2 it works on a different scale, 0.049→0.019. Whether calibration is needed is decided by numbers, not by claims.
This is the first real example of "a model that needs calibration" (design §7.1), and also the first measurement where the framework's calibration produced value.

## Cautions

- You need Prism's fork. Do not mix it with upstream llama.cpp's `ggml-*` (a warning in the fork's README)
- BonJev does not support 1-bit Q1_0. Only ternary PQ2_0
- The probabilities go through BonJev's built-in calibration, but that is a temperature for the whole model and **not calibration per spec**. The premise of re-applying `calibrate` under the (spec_hash, model) key does not change

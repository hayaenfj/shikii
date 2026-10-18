# 素のチャット用モデルには plain 配置（system 無し・id をそのまま読む）

status: tentative  <!-- 2026-10-18: Qwen3-4B Q8 の 1 組。別の素のモデルで効いたら confirmed に(人が変える) -->

## いつ効くか

- モデル: 決定モデルとして学習されていない、素のチャット用モデルを `shikii-readout` で読む時（Qwen3-4B で確認）
- 効かない / 要らない: 決定モデル（Kev-4B 等）は配置を見ない。27B readout（Ternary-Bonsai）は state-last で既に 0.968、plain で 0.984（Brier 0.0136 → 0.0095）、plain + 短い質問で 1.000。害は無いが holdout は 1 勝 0 敗（p=0.5）で門を越えず現状維持（[run](../runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-3.md)）。正解が増えれば決まる

## 書き方

spec か variant に `layout: plain`。変換役は system の文を外し、質問 → `- id: 説明` の列 → `Input:` の後ろに本文 → 「Answer with the option id only.」と並べ、文字（A, B, …）ではなく**各 id の最初のトークン**の確率を読む。配置は指紋と model 名（`…+plain`）に入るので線は別に引かれる。

## 証拠

Qwen3-4B Q8_0（本家 llama.cpp 0.7.0 + `shikii-readout`）、prompt-intent の 62 件、同じ重み:

| 候補 | top-1 | Brier | 備考 |
|---|---|---|---|
| state-last（既定、v2 の文言） | 0.774 | 0.2216 | 全部 0.75 以上に張り付く |
| state-last + 短い質問 | 0.855 | 0.1394 | 文言だけの改善 |
| **plain** | **0.903** | **0.0959** | holdout 0.887 vs 0.774、8 勝 1 敗、p=0.020 |
| plain + 短い質問 | 0.887 | 0.0975 | |
| mlx で平文の prompt（対照、bf16） | 0.903 | 0.0967 | readout の plain と同じ数字 → 差は配置 |

run: [2026-10-18-prompt-intent-qwen3-4b-q8-2](../runs/2026-10-18-prompt-intent-qwen3-4b-q8-2.md)。比較ベンチの文脈は [docs/bench.md](../../docs/bench.md)。

## 理屈（推測）

決定モデル向けの並べ方（system で「文字で答えよ」、`<state>` タグ、A/B/C）は、そう学習されたモデルには合うが、素のチャット用モデルは「選択肢の名前をそのまま答える」方が自然で、文字への写しで確率が崩れる。plain は答えの形をモデルの癖に合わせている。

# Kev-4B（ggml-org/Kev-4B-GGUF、llama.cpp の決定モデル）

- 学習済みの決定モデル。確率は端まで振れる（1.0 / 0.0 が普通に出る）
- 速い: 1 問 0.25 秒（Q4_K_M、Metal）。hook のような「依頼ごとに 1 回」に向く
- 英語のみと表記されているが、v2 の文言なら日本語の依頼でも top-1 0.913（v1 では other に逃げて 0.68）。**文言の方が言語より効く**
- 較正: prompt-intent（choice）は vector が CV で勝った（other への逃げの補正）。same-issue / same-inquiry（noul）は platt がわずかに勝つ（0.038 → 0.035、0.030 → 0.020）
- 順序バイアス: 100 回並べ替えて反転 0、ずれ ≤ 0.024。無視できる
- 5 段の score は弱い（同一の不具合の組で P(≥3) = 0.418）。順序尺度は 27B 級に任せる方がよい

証拠: [docs/model-comparison.md](../../docs/model-comparison.md)

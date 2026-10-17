# OpenJev（ggml-org/OpenJev-GGUF、27B、CC BY-NC 4.0）

- 学習済みの決定モデル。精度は頭一つ上: prompt-intent top-1 0.984、日本語 1.000
- noul は生の確率で足りる（same-issue 0.007、same-inquiry 0.037 で両方 identity）。Platt は CV で改善しない
- 遅い: 1 問 3.8 秒、常駐 16GB。バッチ向き。hook の 1 問には重い
- 非商用。手元の既定には使えない

証拠: [docs/model-comparison.md](../../docs/model-comparison.md)

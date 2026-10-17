# 較正の要否は触れ込みではなく (質問, モデル) ごとの数字で決める

status: confirmed

## いつ効くか

- 型: noul（choice / score は vector で同じ話）
- モデル: 全部。「較正済み」を謳う決定モデルでも、普通のチャット用モデルでも

## 書き方

文言の話ではなく運用の話: `calibrate` の CV の結果に従う。identity が勝てば線は「検証済み」の印だけ、platt が勝てば定数を書く。
普通のチャット用モデルの読み出しは確率が端に寄る（0.999 / 0.003）ので Platt の当てはめが暴れやすい。`calib.Fit` は直線探索付きになっている。

## 証拠

- OpenJev: same-issue 0.007、same-inquiry 0.037 で両方 identity（Platt は CV で悪化）
- Kev-4B: 旧題材では identity、差し替え後の 2 セットでは platt がわずかに勝つ（0.038 → 0.035、0.030 → 0.020）。**同じモデルでも質問で変わる**
- Bonsai 2 27B（BonJev）: 0.049 → 0.019、0.084 → 0.028。普通の LLM には大きく効く
- Ternary-Bonsai-27B（readout）: 0.048 → 0.024、0.033 → 0.020

[docs/model-comparison.md](../../docs/model-comparison.md)

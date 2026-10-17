# score（順序のある段階）の聞き方

## 基本

- 段階は低い順に 2〜10。各段階に「何がそこに入るか」を一文で
- 隣り合う段階に確率が割れるのが普通。**最上位の確率で帯を決めない**。`thresholds.level: k` で P(level ≥ k) を使う → [ordinal-band-on-mass](../patterns/ordinal-band-on-mass.md)
- 境界の段階（「状況証拠」「同じ領域」）の説明に、上の段階と何が違うかを書く

## 証拠

- relatedness-level（同一の不具合 / 同じ画面の別の不具合 / 無関係）: Ternary-Bonsai-27B の readout で P(≥3) = 0.950 / 0.048 / 0.008。最上位の確率は 0.544 で、それだけ見ると ask 止まり（2026-10-17）

# 順序尺度は P(level ≥ k) で帯を決める

status: confirmed

## いつ効くか

- 型: score。段階が 3 つ以上で、隣り合う段階に確率が割れる時（ほぼ常に）
- モデル: 全部

## 書き方

spec に `thresholds.level: k` を書く。帯は最上位の段階の確率ではなく、k 以上の確率の和で決まる。正解率も「P(≥k) と正解 ≥ k」で測る。

## 証拠

- relatedness-level、同一の不具合の組（Ternary-Bonsai-27B readout）: 最上位の確率 0.544（ask 止まり）、P(≥3) = 0.950（act）。無関係の組は 0.008
- 旧題材でも同じ形（最上位 0.52 → P(≥3) 0.96、Kev-4B、2026-10-16）

[docs/guide.ja.md](https://github.com/hayaenfj/shikii/blob/main/docs/guide.ja.md) の「score の帯」

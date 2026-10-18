---
claim: 普通のチャット用モデルを shikii-readout で読む時、本文(state)は選択肢の後ろ・答えの直前に置く(state-last)方が、先頭に置く(state-first)より当たる
status: tentative
applies_to:
  models: 再帰層を持つ Qwen3.5 / 3.8 系(Ternary-Bonsai-27B で確認)。Bonsai 2(fork)は変わらず
  type: choice
evidence:
  - date: "2026-10-17"
    model: ternary-bonsai-27b-q2g64
    result: state-last 0.968 vs state-first 0.919 (top-1 on 62 truths; 日本語 23 件は 1.000 vs 0.870)
    verdict: supports
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-first
    result: current 0.968 vs state-first 0.919 (top-1 on 62 truths; 3 better / 0 worse, p=0.125)
    verdict: inconclusive
retest:
  spec: decisions/prompt-intent-v2.yaml
  better: current
  than: state-first
---
# 本文（state）は答えの直前に置く（普通のチャット用モデルの読み出し）


## いつ効くか

- モデル: 決定モデルではない普通のチャット用 GGUF を `shikii-readout` で読む時。特に Qwen3.5 / 3.6 / 3.8 系（Bonsai）
- 型: 全部（choice で確認。noul / score でも読み出しの仕組みは同じ）

## 書き方

質問 → 選択肢 → **本文** → 「Reply with one letter.」の順。本文を先頭に置かない。
`shikii-readout` の既定がこの配置で、応答の `model` に `+state-last` が付く（配置が変わると較正の鍵も変わる）。

## 証拠

- Ternary-Bonsai-27B Q2_g64: state-first で top-1 0.919 / 日本語 0.870 → state-last で 0.968 / 1.000（同じ重み、62 件）。[integrations/readout.md](../../integrations/readout.md)
- Bonsai 2 27B（fork）: 0.968 で変わらず。悪くはならない

## 副作用

同じ spec の全問で接頭辞が共通になるので `cache_prompt` が効き、1 問 4 秒 → 0.6〜0.8 秒。精度と速さが同じ変更で取れた

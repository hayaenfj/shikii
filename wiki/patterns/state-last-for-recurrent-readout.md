# 本文（state）は答えの直前に置く（普通のチャット用モデルの読み出し）

status: tentative  <!-- 2026-10-18: 効いたのは Ternary-Bonsai-27B(Qwen3.5 系)の 1 組。Bonsai 2(fork)は変わらず。再帰層のある別のモデルで効いたら confirmed に(人が変える) -->

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

# Ternary-Bonsai-27B Q2_g64（PrismML、本家 llama.cpp 0.7.0 で動く普通のチャット用モデル。`shikii-readout` で読む）

- 決定モデルではない。選択肢に A, B, … を振って 1 トークンだけ回し、文字の確率を読む（readout）
- **本文の置き場所で順位が動く**: state を選択肢の前に置くと top-1 0.919 / 日本語 0.870、答えの直前（state-last）に置くと 0.968 / 1.000。同じ重み → [state-last-for-recurrent-readout](../patterns/state-last-for-recurrent-readout.md)
- 土台が Qwen3.6 系（再帰層あり）なので、接頭辞のチェックポイントが無いと `cache_prompt` が効かない。アダプタが接頭辞を先に流す。1 問 0.58 秒
- 文字の読み出しは確率が端に寄る（0.999 / 0.003）。noul は Platt が効く（same-issue 0.048 → 0.024、same-inquiry 0.033 → 0.020）。prompt-intent（choice）は生で precision 1.000 / recall 0.983 なので identity
- 5 段の score が得意（同一の不具合 P(≥3) 0.950、無関係 0.008）
- 商用可（Apache 2.0）、fork 不要。この wiki では「用途を選ばない既定」に最も近いモデル

証拠: [integrations/readout.md](../../integrations/readout.md)、[docs/model-comparison.md](../../docs/model-comparison.md)

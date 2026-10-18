---
claim: はい/いいえ の質問は、はい になるものを先に肯定で定義し、対比(いいえ になるもの)は "as opposed to …" で後ろに付ける。否定の列挙や否定の指示で終わる文は、はい の側が弱くなる
status: tentative
applies_to:
  models: 普通のチャット用モデルを shikii-readout で読む時に確認(Ternary-Bonsai-27B)。決定モデルは未確認
  type: noul
evidence:
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-last
    result: prompt-intent の分解の 1 段目で、否定の列挙で終わる文 0.306 → 肯定の定義 + as opposed to 0.790(top-1、62 件)。同じ 5 文の P(yes) は 0.42〜0.62 → 0.96〜0.97
    verdict: supports
    run: 2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-2
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-last
    result: yes-first 1.000 vs current 0.964 (top-1 on 32 truths; 1 better / 0 worse, p=0.500)
    verdict: inconclusive
retest:
  spec: decisions/same-issue.yaml
  better: yes-first
  than: current
---
# はい/いいえ は「はい の定義」を先に、対比は後に


## いつ効くか

- 型: noul。特に分解(`steps:`)の 1 段目のように「依頼かどうか」「該当するかどうか」を切る質問
- モデル: 普通のチャット用モデルを `shikii-readout` で読む時に確認。決定モデルでも同じ理屈と思われる(未確認)

## 書き方

**はい になるものを先に肯定で定義し、対比(いいえ になるもの)は "as opposed to …" で後ろに付ける。**
「〜していますか？ A、B、C は違います」のように、否定の列挙で終わる文は、はい の側が弱くなる。

良い: `Is this message a request (something the assistant should do or answer), as opposed to a greeting, thanks, an acknowledgement or small talk?`
悪い: `Is this message asking the assistant to do or answer anything at all? Greetings, thanks, "ok" and small talk are not requests.`

## 証拠

Ternary-Bonsai-27B(readout、state-last)で同じ 5 文に 4 通りの聞き方。P(yes):

| 入力 | 否定の列挙で終わる | 肯定の列挙 | 肯定の定義 + as opposed to | 短い肯定 |
|---|---|---|---|---|
| fix the typo in README | 0.42 | 0.91 | **0.97** | 0.83 |
| why does the build fail? | 0.62 | 0.72 | **0.97** | 0.92 |
| run the tests | 0.61 | 0.93 | **0.96** | 0.76 |
| thanks, that is all | 0.06 | 0.02 | **0.01** | 0.02 |
| 了解 | 0.16 | 0.05 | **0.05** | 0.20 |

否定の列挙で終わる文では、明らかな依頼 3 つが 0.42〜0.62 で、分解の 1 段目としては使えない(prompt-intent の two-step が top-1 0.306 に落ちた run: [2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64](https://github.com/hayaenfj/shikii/blob/main/wiki/runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64.md))。

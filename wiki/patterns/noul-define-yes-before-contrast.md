---
claim: A yes/no question should first define what counts as yes in positive terms, and put the contrast (what counts as no) after it with "as opposed to …". A sentence that ends with a list of negatives or a negative instruction weakens the yes side
status: tentative
applies_to:
  models: Confirmed when reading an ordinary chat model with shikii-readout (Ternary-Bonsai-27B). Not checked on decision models
  type: noul
evidence:
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-last
    result: In the first step of prompt-intent's decomposition, a sentence ending with a list of negatives 0.306 → positive definition + as opposed to 0.790 (top-1, 62 items). P(yes) on the same 5 sentences 0.42 to 0.62 → 0.96 to 0.97
    verdict: supports
    run: 2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64-2
  - date: "2026-10-18"
    model: ternary-bonsai-27b-q2g64+state-last
    result: yes-first 1.000 vs current 0.964 (top-1 on 32 truths; 1 better / 0 worse, p=0.500)
    verdict: inconclusive
retest:
  spec: .shikii/decisions/same-issue.yaml
  better: yes-first
  than: current
---
# For yes/no, define yes first and put the contrast after


## When it applies

- Type: noul. Especially questions that draw a line such as "is it a request?" or "does it apply?", like the first step of a decomposition (`steps:`)
- Models: confirmed when reading an ordinary chat model with `shikii-readout`. Likely the same for decision models (not checked)

## How to write it

**Define what counts as yes first in positive terms, and put the contrast (what counts as no) after it with "as opposed to …".**
A sentence that ends with a list of negatives, like "Is it doing X? A, B and C are not", weakens the yes side.

Good: `Is this message a request (something the assistant should do or answer), as opposed to a greeting, thanks, an acknowledgement or small talk?`
Bad: `Is this message asking the assistant to do or answer anything at all? Greetings, thanks, "ok" and small talk are not requests.`

## Evidence

Four ways of asking about the same 5 sentences on Ternary-Bonsai-27B (readout, state-last). P(yes):

| Input | Ends with a list of negatives | List of positives | Positive definition + as opposed to | Short positive |
|---|---|---|---|---|
| fix the typo in README | 0.42 | 0.91 | **0.97** | 0.83 |
| why does the build fail? | 0.62 | 0.72 | **0.97** | 0.92 |
| run the tests | 0.61 | 0.93 | **0.96** | 0.76 |
| thanks, that is all | 0.06 | 0.02 | **0.01** | 0.02 |
| 了解 | 0.16 | 0.05 | **0.05** | 0.20 |

With the sentence ending in a list of negatives, the 3 obvious requests score 0.42 to 0.62, which is unusable as the first step of a decomposition (the run where prompt-intent's two-step fell to top-1 0.306: [2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64](https://github.com/hayaenfj/shikii/blob/main/wiki/runs/2026-10-18-prompt-intent-ternary-bonsai-27b-q2g64.md)).

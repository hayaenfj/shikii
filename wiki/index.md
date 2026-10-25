# The wiki of ways to ask

Knowledge of **how to ask a decision model so it gets things right**. Not one person's experience, but a shared asset with evidence. `shikii tune --propose` reads it to make candidates and writes results back to `runs/`.

| Place | What goes there | Who writes it |
|---|---|---|
| [`types/`](types/) | The basics of asking, per question type (choice / noul / score) | People |
| [`models/`](models/) | Habits of each model family (whether calibration is needed, where the text goes, language) | People (after looking at tune runs) |
| [`patterns/`](patterns/) | One pattern per page: when it applies, how to write it, evidence, counter-evidence, status | People. Only people change the status (confirmed / tentative / rejected) |
| [`specs/`](specs/) | Each decision's rules (Rules, written by people) and its list of runs (added by tune) | Both |
| [`runs/`](runs/) | Evidence: candidates, numbers, holdout, adopted or not, a one-line lesson | tune |

Rules:

- A pattern is `status: confirmed` only if it **worked in 2 or more independent observations (evaluations on another model or another set of truths, or tune runs)**. With one, it is `tentative`. Only people change the status, and they attach evidence
- The machine writes only runs/ and the Runs section of specs/. It never overwrites people's text
- Example sentences appear only for decisions with `pii: false`. For decisions whose text may contain personal data, numbers only

Start here: [asking a choice](types/choice.md) · [asking a noul](types/noul.md) · [asking a score](types/score.md) · [list of patterns](patterns/)

## Shape of a pattern page (since 2026-10-18)

A pattern is written as a **claim that can be checked**, not as an essay. The top of the page has front matter (enclosed in `---`) that the machine reads; the body is written by people.

```yaml
---
claim: A claim you can state in one sentence
status: tentative            # tentative / confirmed / rejected. Only people change it
applies_to:                  # where it applies (type, model family, language and so on; free text)
  type: choice
  models: plain chat models
evidence:                    # added by the machine. One per run: date, model, numbers, verdict (supports / contradicts / inconclusive)
  - date: "2026-10-18"
    model: qwen3-4b-q8+state-last
    result: plain 0.903 vs current 0.774 (top-1 on 62 truths; 8 better / 1 worse, p=0.020)
    verdict: supports
retest:                      # how to retest: on this spec, supported if candidate better beats candidate than on top-1
  spec: .shikii/decisions/prompt-intent-v2.yaml
  better: plain
  than: current
---
```

Front-matter fields the machine does not know (such as `author:`) are written back as read, but comments inside the front matter are lost when the machine rewrites it (put comments in the body). `shikii wiki verify [pattern.md ...] --local URL` runs the retest with the current model (pairwise wins and losses with a one-sided sign test; supported at p ≤ 0.05) and adds one piece of evidence. When 2 or more models support it and there is no counter-evidence, it says "confirmable", but setting the status to confirmed is up to a person. When someone in another project runs verify with their own model, that result becomes new evidence for the claim. This is what "the knowledge that grows is shared between people" actually means. verify skips pages without front matter as "claims that cannot be checked yet".

### It grows automatically

Each time a `tune` run ends, the machine grows the wiki (without calling the model again): a candidate that significantly beat current (pairwise wins and losses, p ≤ 0.05) is drafted as a **draft claim** in `patterns/drafts/<spec>-<candidate>.md` (status draft), and the run's scores are added as evidence to existing claims that have that spec as their retest. What a person does is rewrite a draft as a general claim, move it to `patterns/`, and give it a status. Delete drafts that are not needed.

What a person does can be done on the page (the "Knowledge" panel of `shikii serve`): rewrite a draft as a general claim and move it to `patterns/`, mark it confirmed / rejected, or delete the draft. The machine only adds the "supported by 2 independent models" mark.

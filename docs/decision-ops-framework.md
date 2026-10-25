**English** · [日本語](decision-ops-framework.ja.md)

# A framework for operating decision models — design notes

Written: 2026-10-16
Status: concept, before implementation. (Added to since then while implementing.)

> A readable version for first-time readers is [`why-shikii.ja.md`](why-shikii.ja.md) (Japanese). This document is a record that keeps numbers and sources, and uses terms without explaining them.

## 1. In one sentence

A layer for **building decision models into business work and keeping them running** (decision models such as Jev / d1-ultra / OpenJev, which generate no text and return only typed probabilities). Calling the model uses someone else's format (System One) as is, and we own only **the decision's lifecycle**.

```
define → run → record → collect labels → calibrate → contract test → deploy → watch drift
```

The selling points are "calibration" and "the label loop". We do not build the model readout part.

This is a new design. The existing claude-jev / had-scorer (§2.4) are taken into account only as grounds for the design;
their code is not reused.

## 2. Background

### 2.1 What a decision model is

- Input: `state` (text / JSON / image) and one or more questions
- Output: a probability distribution per question. 0 generated tokens, 1 forward pass
- There are 3 question types, and every vendor, llama.cpp and OpenRouter converged on the same shape (the System One format)
  - `noul`: the probability p that a proposition is true
  - `choice`: a probability per option + confidence (how concentrated the distribution is, `(p_max − 1/n)/(1 − 1/n)`)
  - `score`: probabilities over ordered levels + the expected value `Σ(level × p)` (takes values between levels)
- How it is built: softmax over only the option tokens read from the logits at the LLM's last position (= using a classification head)
  + distillation SFT + calibration training (TypeSafe calls it RLCD). Every vendor keeps the details private

### 2.2 What already exists as of 2026-10 (what we do not build)

| Layer | What already exists |
|---|---|
| Hosted APIs | TypeSafe Jev, Liquid d1 / d1-ultra, OpenAI gpt-6-luna-decisions, Perplexity, Cloudflare Clef, Microsoft Decision-1 … |
| A common interface | OpenRouter Decisions API (`POST /api/alpha/decisions`, 14 models) |
| Local execution | llama.cpp `/v1/systemone` (added 2026-10-02). Existing clients only change the base URL |
| Open weights | Julia-1 (144M), Laya (421M), Kev-4B (Apache 2.0), lev, OpenJev (27B, handles Japanese, CC BY-NC), Clef (27B, Apache 2.0) |
| The same mechanism on ordinary GGUF | GitLuci/local-typed-decisions, allcodernet/llama.cpp-decide, NotXf1le/choosekit |

**Conclusion: the part that "returns typed decisions by reading logits" is already a product. We do not enter it.**

### 2.3 The gap everyone disclaims (what we build)

What the vendor, llama.cpp and OpenRouter documents all say:

- "The probabilities are not calibrated on your data"
- "Thresholds do not carry over between models or between questions"
- "Confidence is how concentrated the distribution is, not correctness"

They only say it; nobody ships a tool to deal with it.

### 2.4 Existing implementations we drew on (for reference, not parts)

This framework is **designed from scratch**. The code of claude-jev and had-scorer is not reused;
only the fact that the same structure was put together by hand in each separately, and the lessons learned there, are the grounds for the design.

| Element | claude-jev (a Claude Code hook) | had-scorer (automatic linking of records in another project) |
|---|---|---|
| Question definition | 5-option choice. Confirmed on 12 examples to be more accurate than yes/no | yes/no. Question text fixed to "is this the same case?" |
| Building the state | **Always** passes the report and the diff together. Never decides on the prose alone | Lines up the two records in a labeled template |
| Calibration | None (left to Jev) | `σ((Δ−bias)/T)`. Per question and per model. Fitted on 78 pairs |
| Threshold → action | ≥0.75 automatic / 0.6–0.75 confirm / <0.6 discard | ≥0.7 auto / ≥0.5 suggest |
| Pinning the question and its constants together | — | Contract test (prompt verbatim + 4 boundary points of the constants) |
| Control over whether it may be sent | Does not send outside in a tainted session | Local only |
| Measuring false alarms and withdrawing | Removed the post-tool-use decision for "0 hits" | Rejected a domain-neutral question text because "boundary and related pairs inverted" |

Each row of this table becomes a part as is.

Lessons had-scorer left (things the framework should prevent **by its structure**):

1. Calibration is specific to a question. Reusing the similarity calibration for relevance put almost every query at 0
2. The prompt and the calibration constants are a pair. Changing only one breaks things silently
3. Some failures cannot be fixed by calibration (changing the question text moved the lower bound of related pairs from Δ=2.54 to 1.80, inverted against the boundary pairs at 1.82)
4. A chat template mismatch breaks decisions before the model's own strength matters (Qwen's `<think>` took the first token, and the yes/no logits were leftovers)
5. Always recalibrate after changing the model. Applying a 0.6B's constants to an 80B put everything at 1.00 and auto-linked even the boundary cases

## 3. Scope

### 3.1 What we own

- The decision's **spec** (one declarative unit)
- **Runtime**: spec + state → band (act / ask / discard). Switching endpoints, control over whether it may be sent
- **Records**: keep every decision with its raw probabilities
- **Label collection**: pick up the user's actions as truth labels
- **Calibration**: fit constants per (spec, model) from labels. Brier / ECE / reliability diagram
- **Contract tests**: generate tests that pin spec, constants and prompt together
- **Drift watching and withdrawal**: warnings for decisions whose hit rate dropped

### 3.2 What we do not own

- The model call format (we ride on System One; we do not invent our own format)
- Training or fine-tuning models
- Implementing the logit readout (left to llama.cpp / vendor APIs)
- Evaluating generative models (a different problem)

## 4. The decision spec (Decision Spec)

1 decision = 1 file. It keeps the model-independent part (question, state, thresholds) apart from the model-dependent part (calibration).
Calibration is keyed by `(spec content hash, model ID)`, and becomes invalid when either changes.

```yaml
# .shikii/decisions/same-issue.yaml
id: same-issue
version: 1
type: noul                          # noul | choice | score

question: |
  Do these two bug reports describe the same underlying defect
  (one is a duplicate of the other, or both are symptoms of the same root cause)?
  Answer "no" if they merely concern the same screen, feature or technology.

state:
  template: |
    Report 1: {{ report1 }}

    Report 2: {{ report2 }}
  required: [report1, report2]       # if any is missing, do not decide (the generalization of "never decide on the prose alone")

policy:
  send_to: [local, hosted]           # local | hosted. A tainted session drops hosted automatically
  pii: true                          # whether state may contain personal information (if true, never send to hosted). No default. The body always stays local

thresholds:                          # named. Raw probability values do not leak into business code
  act: 0.70                          # ≥ act  → run automatically
  ask: 0.50                          # ≥ ask  → confirm with the user
                                     # < ask  → discard

eval:
  labeled: testdata/same-issue-labeled.json   # (state, truth) pairs
  min_n: 20                                   # below this, do not calibrate

calibration:                         # generated by the calibrate command. Never write it by hand
  - model: ggml-org/Kev-4B-GGUF:Q4_K_M
    spec_hash: 39e5d9072ae0c8c2      # hash of question + state.template + type
    method: platt
    bias: -0.404
    temperature: 0.874
    fitted_on: testdata/same-issue-labeled.json
    n: 32
    brier: 0.031
    ece: 0.095
    fitted_at: "2026-10-17"
```

For `choice` and `score`:

```yaml
type: choice
options:
  - id: change_request
    description: asks for a change to code or settings
  - id: question
    description: asks for an explanation or investigation, not a change
  - id: review
    description: asks for an evaluation of an existing diff
  - id: operation
    description: asks for an operation such as commit, deploy or run
  - id: other
    description: none of the above
# order bias is measured with a separate command, `shikii bias` (it was not made a spec field)
```

```yaml
type: score
levels:                              # 2 to 10, lowest first
  - unrelated
  - same feature
  - related
  - strongly related
  - same defect
```

Design decisions:

- **Thresholds are kept by name**. Business code looks at `band == act`, not at 0.7
- **Options get descriptions**. Labels alone cause misrouting (the llama.cpp blog reports the same with Julia-1)
- **A state missing required is not decided**. claude-jev's "always the report and the diff together", promoted into the spec
- **The calibration block is generated**. Editing it by hand makes the contract test fail

## 5. Runtime

```
decide(spec, inputs) → Decision
```

```go
type Decision struct {
    SpecID     string
    SpecHash   string
    Model      string
    Endpoint   string            // where it was sent
    Raw        Distribution      // the probabilities the model returned
    Calibrated Distribution      // after calibration. Without calibration, same as Raw and Calibrated=false
    Band       Band              // act | ask | discard | abstain
    Confidence float64           // choice/score only
    TraceID    string
    At         time.Time
}
```

Behavior:

1. If `inputs` lacks something in `required`, `abstain` (stating explicitly that no decision was made)
2. Choose the endpoint from `policy.send_to` and the session's taint state. If tainted and there is no local, `abstain`
3. Send in the System One format. Hosted APIs / OpenRouter / llama.cpp differ only in the base URL
4. If there is a calibration matching `(spec_hash, model)`, apply it. If not, return the raw values and set `Calibrated=false`
5. Decide the band. A decision without calibration **never gives act** (it stops at ask). "Never run automatically on uncalibrated probabilities" is the default
6. Record it (§6)

Swapping endpoints is an operations matter; business code is not touched.
The first concrete use: take the decisions claude-jev now sends to hosted Jev, and drop only tainted sessions to llama.cpp's Kev-4B.

## 6. Records and labels

A decision model's output has a fixed type, so **labels are cheap**. This is the decisive difference from evaluating generative models.

What is recorded: the whole `Decision` + the body and hash of inputs (the body is always kept. Until 2026-10-17 `pii` decided whether to keep it, but without it people could not give truths, and the tool would be the one judging what is personal information, so that was dropped. `pii: true/false` decides only whether it may be sent outside).

How labels are picked up (we never tell the user "please rate this"):

| Action | Label |
|---|---|
| The answer the user chose in the `ask` band | That answer is the truth |
| An `act` was undone later | The model's answer was wrong |
| An `act` was not undone for a certain period | Right (a weak label, handled with a weight) |
| The user ran a `discard`ed one by hand | Discarding it was wrong |

Labels are stored as `(spec_id, spec_hash, model, inputs_hash, raw, truth, weight, at)`.
When the spec changes, the hash changes, so old labels drop out of calibration automatically.

Added in the prototype (2026-10-16): labels that `calibrate` gets by asking the model about rows of a labeled file are also kept in the store in the same shape
(`source: file`). From then on, `check` / `drift` run without calling the model. The same input (inputs_hash) is not counted twice and does not grow on a rerun.

Added in the prototype (2026-10-17, **saving truths and asking again**): the answer to the question "does a person have to judge again every time one word of the wording changes?".
A label's truth depends neither on the wording nor on the model (it is a fact a person decided). Only raw depends on them, and raw can be taken again. So:

- `policy.pii` became `true / false` (required, no default). If true, nothing is sent outside even if `send_to` has hosted (`PickEndpoint` rejects it). The body is always kept in the local records and labels (material for asking again)
- Labels also got `inputs`, because truths calibrated from a file have no record
- For inputs with no label under the current (spec_hash, model), `calibrate` takes the latest truth per input from all of the spec's labels in the store (old hashes, other models),
  and if there is a body, asks the current model again with the current wording, making a `source: replay` label (persisted; from then on check / drift are offline). No body, no interface, or a truth not in the current spec are each counted and the reason is given
- `PutCalibration` replaces the line of the same model's old hash (if left, check keeps failing as stale; found in the demo)

Result: changing the wording or the model becomes "a few minutes of computation" instead of "a person's judgment". What a person decides is one truth per input.
Tests: after changing the wording, the line is redrawn without human labels / it fails with a reason when there is no body / truths from a file can also be asked again /
pii: true does not go to hosted / the same model's old line is replaced — 5 in all. Each was confirmed to fail before implementation, then GREEN. Visible in step 6b of the demo.

## 7. Calibration

```
shikii calibrate <spec> --model <id>
```

1. Collect, from `eval.labeled` and the labels gathered in §6, those whose `spec_hash` and `model` match
2. If `n < eval.min_n`, do not calibrate (had-scorer's 78 pairs are the rough guide for this floor)
3. Methods
   - `noul`: Platt (bias, temperature). Isotonic regression can be chosen once labels grow
   - `choice`: one temperature + a bias per option (vector scaling; `method: vector` in the prototype.
     Fitting it to 4 items on a real Kev-4B gave `other` −2.2 and `change_request` +2.3 —
     the habit of "escaping to other on a short state" shows up directly in the bias. Separating position bias by permutation is not implemented)
   - `score`: one temperature + a bias per level. The expected value is recomputed on the calibrated distribution
4. Outputs
   - Constants (written back to the spec's `calibration` block)
   - Brier, ECE, reliability diagram (confidence vs accuracy; the same graph d1-ultra shows)
   - precision / recall at `thresholds.act` / `ask`
   - **Detecting failures calibration cannot fix**: if the lowest positive and the highest negative are inverted, output no constants and say "change the question text" (a reproduction of had-scorer's Δ=1.80 vs 1.82).
     Weak labels with weight below 1 (boundary cases, "it was just not undone", and so on) are left out of the inversion check. In measurement (32 yes/no pairs),
     the negatives at the edge were all boundary pairs with w=0.5 (examples like "a different bug on the same screen"). Calling "inversion" on examples known to be ambiguous is a false alarm
   - **Comparison with identity**: platt / vector look good in-sample, so they are measured with 5-fold CV, and if keeping the raw distribution (identity) satisfies act and is no worse on Brier, identity is chosen.
     On the old material, Kev-4B's noul had raw Brier 0.046, platt got worse in CV, and it was identity. On the replaced same-issue / same-inquiry, platt wins slightly (0.038→0.035, 0.030→0.020). Whether it is needed is decided per (question, model)
5. Generate contract tests (§8)

### 7.1 Models that need calibration, and models that need verification

Models fall into 2 kinds. Confirmed on real hardware (2026-10-16, sending prompt-intent to the hosted jev-1.13.0):

```
raw: {"change_request":1,"operation":0,"other":0,"question":0,"review":0}
```

Jev is calibrated with RLCD, so its probabilities **swing all the way to the ends**. As logits, `Logit(1.0)` is clamped to about ±13.8,
and the Δ fed into Platt collapses to nearly two values. There is no room to fit bias/T.

| Kind | Examples | Shape of output | What to do |
|---|---|---|---|
| Needs calibration | Ordinary GGUF (a real case: Bonsai 2 27B read through BonJev; on same-issue, raw Brier 0.049 → Platt 0.019, 2026-10-17), raw output of small decision models | Intermediate probabilities appear | Fit Platt / isotonic (§7 body) |
| Needs verification | Models that claim to be calibrated, such as hosted Jev / d1 | Almost 0 or 1 | Measure "the accuracy when it said 1.0" and decide **whether act may be allowed** |

For the latter, `calibrate` outputs no constants, only precision / recall per threshold and a reliability diagram (the equivalent of `verify`).
The decision to allow act rests on that accuracy being at or above `thresholds.act`. The calibration block written to the spec in this case is
`method: identity` (no constants; a mark that it was verified), keyed by the same `(spec_hash, model)`.

### 7.2 What calibration fixes, and what wording fixes (measured)

We asked Kev-4B prompt-intent (5 options) on 62 items (mixed English and Japanese, labels made by us) (2026-10-16):

| | v1 (short option descriptions) | v2 (descriptions made concrete, other limited to "things that are not requests") |
|---|---|---|
| top-1 accuracy (raw) | 0.682 | **0.903** |
| precision@act / recall@act (raw) | 1.000 / 0.156 | 1.000 / **0.732** |
| Where errors went | 19 of 21 to `other` | 0 errors to `other` |

v1's errors are **ranking errors** (`other` above the truth). A bias can push them back,
but that bias "always lowers `other`", so it drags in inputs that really are other. Fixing the wording makes the errors themselves disappear.

Rule: **if errors pile up on one option in the confusion table, fix the wording before calibrating.** Use calibration
only for "the ranking is right but confidence is short" (the remainder of v2's recall 0.732).

The calibration key is `(spec_hash, model)`. This makes the following **structurally** impossible:

- Reusing question A's calibration for question B (the hash differs)
- Changing the model and keeping the constants (the model differs)
- Fixing the prompt and keeping the constants (the hash changes, calibration expires, and act stops being given)

## 8. Contract tests

In the prototype, this became **a check command `shikii check <spec>...` instead of generating test files**
(2026-10-16). It does not depend on a language, runs in CI, and checks with the same code that reads the spec. A generalization of had-scorer's
`calibration_test.go`. What it checks:

- The calibration's `spec_hash` matches the current hash (the wording changed but the constants were kept → stale)
- `method` is known and its keys are complete (rejects hand-written calibration blocks)
- On the labels (store + raw rows of `eval.labeled`), precision at the calibrated act threshold is at or above `thresholds.act`
- noul has no inversion of positives and negatives

Below is the original plan (generating tests). The content maps to the above.

- Verbatim match of the spec's `question` / `state.template` / `options` (no drift from the wording calibration was fitted on)
- Boundaries of the calibration constants: in the label set, "the highest negative" is below `ask`, "the lowest positive" at or above `act`, and boundary cases below `act`
- Monotonicity: as probability rises, the band does not go down
- The pair of model name and constants (pinned by a test, not just stated on stderr at startup)
- Missing `required` gives `abstain` (a negative case)
- Tainted does not send to hosted (a negative case; actually start a mock endpoint and confirm it is not called)

The tests are **generated and placed in the repository**, in a form that fails in CI.

## 9. Drift watching and withdrawal

The prototype is `shikii drift <spec> --model M --window N` (2026-10-16). On the last N labels, it warns and exits 1 if Brier is worse by +0.05 or more than
the Brier recorded at calibration, or if precision at act is below `thresholds.act`.
It also prints the agreement rate on labels from `ask` (the share of times the user chose the model's top answer).

- Per (spec, model), print the agreement rate in the `ask` band and the undo rate of `act` over the last N
- Warn if Brier gets worse than at calibration by a certain amount. Whether to stop `act` and drop to `ask` is a spec setting
- For decisions whose "hits" (times the user went along) over a certain period fall below a threshold, say "remove it". This automates claude-jev removing the post-tool-use decision at 0 hits
- When a new model comes out, calibrate on the same label set and line up the Brier. Material for deciding whether to swap

## 10. Verification plan (what to pass before publishing)

Whether the abstraction is right is decided by **whether the 2 existing decisions can be expressed again in the same spec**. If a special case is needed, the abstraction is wrong.
Rather than calling the existing code, rebuild the same decisions on top of the new framework.

1. Rewrite claude-jev's `prompt-intent` (5-option choice) as a spec
   - Run it on both hosted Jev and llama.cpp Kev-4B
   - Confirm with a negative case that a tainted session does not go to hosted
2. Write had-scorer's `similarity` (noul) in the same spec
   - `calibrate` outputs constants equivalent to the current bias/T
   - The generated contract tests pin the same 4 boundaries as the current `calibration_test.go`
   - Change one word of the question text on purpose and confirm calibration expires and act stops being given (RED)
3. Once both pass, close the loop of record → label → recalibrate
   - The answer chosen in `ask` takes effect in the next `calibrate`
4. Getting this far makes it worth publishing

Progress (2026-10-16): steps 1 to 3 passed.
- prompt-intent: wrote back a vector calibration for Kev-4B with the v2 wording (62 items, CV precision 0.963)
- same-issue / same-inquiry (32 pairs each; duplicate bug reports, and whether two inquiries are the same): Kev-4B is platt on both (raw Brier 0.038 → CV 0.035, 0.030 → 0.020),
  OpenJev is identity on both. Measured again after replacing the material on 2026-10-17 (the earlier yes/no numbers are on different material and are not kept in published material)
- The loop: claude-jev's hook decides with `prompt-kind.yaml` → at Stop, attaches `edit` / `!edit` from changes in the working tree; this is running.
  The source of labels became the **outcome** (whether something changed) rather than "the answer the user chose in ask". The partial label `!x` was added for this
- The band for score is `thresholds.level: k` with `P(level ≥ k)` (added the same day). On a pair of the same defect, top 0.54 → P(≥3) 0.95 (Ternary-Bonsai-27B, readout). The reported precision is measured on the same event
- Confirming the goal (same day): the goal is **a framework other people can use**; the precision of same-issue or prompt-intent is only an example.
  To match that, we added `scripts/demo.sh`, which goes one round with no key and no GPU (fake model `cmd/fake-systemone`), the OpenRouter interface, an English README and CI
- Measuring order bias for choice is `shikii bias` (added the same day). Kev-4B: flip 0/100, position shift ≤ 0.024, negligible.
  The "option order bias" listed in Jev's jaggedness may not appear in trained decision models — measure before deciding on a correction
- The ordinary LLM path (2026-10-17): BonJev + Bonsai 2 27B worked just by pointing `--local` at it. top-1 0.968 on prompt-intent-v2 (Kev-4B 0.919),
  and for yes/no, Platt was chosen by CV for the first time (0.049 → 0.019 on the replaced same-issue). The first real case of "a model that needs calibration" (§7.1). `integrations/bonjev.md`
- Our own adapter (2026-10-17): `cmd/shikii-readout` (`shikii-readout`). Turns an ordinary GGUF into System One. It has no inference of its own and reads the probabilities of the option letters through the backing llama-server's
  `/apply-template` + `/completion`. Same principle as BonJev, but with neither FFI into a fork nor built-in calibration.
  It keeps "the framework does not own inference" (§3.2) and adds only "format conversion" to what it owns. Numbers are in `integrations/readout.md`
- Cleaning records (2026-10-17): `shikii gc`. It deletes only "old records without labels"; labeled records and labels are kept as calibration material.
  The default only shows (`--apply` rewrites), and rewriting uses a temporary file + rename. Since it is a "discard" operation, RED was confirmed with 7 mutations
- Saving truths and asking again (2026-10-17): even when the wording or model changes, truths people gave stay with their bodies, and `calibrate` asks again for the new (hash, model) (§6).
  It started from the point "won't a person have to judge again every time something small changes?". The strictness of the calibration key stays; the cost of going back moved from people to the model
- The interface that picks up the answer the user chose in the `ask` band (2026-10-17): the `shikii serve` page. Remaining: isotonic regression
- Optimizing the way of asking (2026-10-17, `shikii tune`): it started from the point "you focus only on the answer (calibration), but what works is the way of asking". Truths do not depend on wording, so
  candidate wordings can be scored on the savings without people (the same mechanism as asking again). The criteria are top-1 → Brier → a tie keeps the current one. How well the choice holds up is given by k-fold holdout.
  Adoption is a new version in a separate file (the original is kept), and calibration is drawn at the same time as adoption. An automation of §7.2's "fix ranking errors with wording"

## 11. Risky parts

- **A framework fails more easily than a library**. If the abstraction does not fit, nobody uses it. Do not skip §10
- **Do not build big**. Until the spec format + `decide` + `calibrate` work, build nothing else
- **The market is one month old**. Ride thinly on the System One format. Inventing our own call format would leave us behind llama.cpp and OpenRouter updates
- **Label bias**. If labels come only from the `ask` band, calibration in the high-confidence region cannot be verified. Make up for it with `act` undos and weak labels from elapsed time. It is still biased, so attach "the distribution of labels" to calibration results
- **The weight of weak labels**. "Not undone = right" is lenient. Lower its weight. Reflect the weights in the confidence interval of calibration results
- **OpenJev is non-commercial**. The local default is Kev-4B or Clef (Apache 2.0)

## 12. What we have not confirmed

Confirmed as of 2026-10-16:

- llama.cpp 0.6.0 (brew, build 11429) `/v1/systemone` **worked with a dedicated decision model (Kev-4B)**. Whether it works with an ordinary chat GGUF is unconfirmed
- **Japanese**: Kev-4B (labeled English only), with the v1 wording, tended to let probability escape to `other` on Japanese request texts (top-1 0.68; 0.913 with v2), and on Japanese yes/no pairs too,
  3 positives sank to around p≈0.6. Making the wording concrete improves it a lot (0.90), but for serious use in Japanese, OpenJev (handles Japanese, non-commercial) or a re-evaluation is needed
- **Position bias**: Kev-4B flip 0/100, position shift ≤ 0.024. Negligible for trained decision models (measure with `shikii bias` before deciding)
- **Weak labels + Platt**: fit cleanly on 32 pairs (8 boundary cases at w=0.5). Weak labels had to be left out of the inversion check (§7)
- **Platt on calibrated models**: no improvement (Brier worse in CV). A mechanism comparing against identity with CV is needed (§7.1)

- **`/v1/systemone` with an ordinary chat GGUF**: does not pass on upstream llama.cpp. Qwen3-8B Q4_K_M gives `501 This model is not a decision model`.
  Putting BonJev, which outputs logit readout in the System One format, in between makes it pass (2026-10-17, confirmed with Bonsai 2 27B; no change to `shikii`).
  Our own readout path became insurance for environments that have only upstream llama.cpp
- **Uncalibrated models + Platt**: works. Bonsai 2 27B on 32 same-issue pairs: raw Brier 0.049 → CV 0.019. The contrast with OpenJev (identity on both) showed up on real hardware
- **Platt on saturated probabilities**: plain Newton's method diverges (2026-10-17; with `shikii-readout`'s Bonsai 2 it flew to T=5e-7, in-sample Brier 0.02 → 0.11).
  The CV comparison went back to identity and prevented an accident, but `calib.Fit` was fixed to use a line search that shrinks the step until the loss goes down (RED: reproduced on those 32 items, GREEN: Brier 0.018).
  Reading letters pushes probabilities to the ends, so this shape will be the norm with ordinary LLMs
- **Prompt arrangement is part of the calibration key**: with `shikii-readout`, just moving the state from before the options to after them moved Ternary-Bonsai-27B's top-1
  from 0.919 to 0.968, and Japanese from 0.870 to 1.000 (2026-10-17). Even with the same weights, the arrangement changes the ranking. The adapter appends the arrangement name to the response's `model`
  (`+state-last`) and splits the calibration key by arrangement. Ranking improvements cannot be had from calibration (§7.2), so with ordinary GGUF, measure arrangement and wording first
- **Julia-1 (144M, multilingual)**: unusable. top-1 0.129 on prompt-intent-v2 (below 0.2, the chance level for 5 options), and 22 inversions on yes/no (on the 32 pairs of the time).
  "Handles Japanese" on the label alone is not a basis for choosing. Measure and drop it

Not yet confirmed:

- How mature GitLuci/local-typed-decisions and the like are (stars, maintenance)
- The n at which isotonic regression beats Platt (not tried on the 32 to 66 we have)
- How many false alarms `drift` raises once `!edit` weak labels number in the hundreds (the share of "just did not change anything" among outcome labels)

## 13. References

### Primary and official sources

- [Liquid AI: d1-ultra](https://www.liquid.ai/blog/d1-ultra)
- [ggml-org: New in llama.cpp: Decision Models](https://huggingface.co/blog/ggml-org/decision-models-in-llamacpp)
- [OpenRouter: openrouter-decisions skill](https://openrouter.ai/skills/openrouter-decisions)
- [OpenRouter: Jev Tutorial](https://openrouter.ai/docs/guides/community/jev-tutorial)

### Explanations and summaries

- [TypeSafe Jev project reference (gist)](https://gist.github.com/pjburnhill/adf8d28efcad9df037bfdece178ef965)
- [Firecrawl: What Is Jev?](https://www.firecrawl.dev/blog/what-is-jev)
- [benchlm: What Is Jev? and Its Limits](https://benchlm.ai/blog/posts/what-is-jev)
- [lalten.org: From Token Likelihoods to Choice Probabilities](https://lalten.org/drafts/llm_choice_models/) (a real example of calibrating temperature + position bias)

### OSS implementing the same mechanism on ordinary GGUF

- [GitLuci/local-typed-decisions](https://github.com/GitLuci/local-typed-decisions)
- [allcodernet/llama.cpp-decide](https://github.com/allcodernet/llama.cpp-decide)
- [NotXf1le/choosekit](https://github.com/NotXf1le/choosekit)
- [ChezRD/bonjev](https://github.com/ChezRD/bonjev) — for Bonsai (PrismML's ternary Qwen). It exposes `/v1/systemone`, so `shikii` could use it as is (2026-10-17)

### Local implementations we drew on (reference only, not reused)

- had-scorer (another project, private) — noul readout + Platt calibration + separating 2 tasks, the prototype of contract tests, the first design of the readout approach
- `~/dotfiles/claude-jev/README.md` — real examples of running thresholds, taint control and withdrawal decisions

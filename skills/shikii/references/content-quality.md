# Check content, not length

Use this when changing shikii's instructions or how agents write knowledge and
replies. It is not an additional review for ordinary tasks. Use the active
agent and existing work records; no extra judging service is required.

## Matched comparison

1. Keep the request, evidence, existing article and completed artifact identical.
   Change one instruction or one coherent instruction change at a time. Record
   both versions, model/agent versions and the actual inputs and outputs. Keep
   full transcripts in `.shikii/handoff/<work>/`; use synthetic data for shared
   fixtures. Do not send private project records to an external model.
2. Decide what the request and evidence require before generating replies.
   Include ordinary completion with no new knowledge, a narrow observation,
   a reply with several usage conditions, an unresolved failure and an update
   that must preserve an exception. Keep expectations out of the generating
   agent's prompt. In this repository, `testdata/content-quality.json` supplies
   fixed inputs and separate expectations; other projects use their own cases.
3. Read each output against the original input. Assess the dimensions below
   separately as pass, fail or unknown, citing the exact output passage and
   the relevant input. For a missing item, name that item and inspect the full
   output. Do not infer quality from a phrase match, tool success or the absence
   of complaints. Optional fields and equivalent wording are not failures.
4. Check the assessment itself with a deliberately damaged copy: remove a
   required exception or turn a limited observation into a general claim.
   It must fail the corresponding dimension (RED); the supported original must
   pass that check (GREEN). This validates that check, not the model as a whole.
5. Try another task not used to tune the wording, then check an ordinary complete
   workflow in an isolated project outside the real repository. Keep real wiki,
   configuration and credentials out of the test workspace; use the existing
   agent's supported authentication without displaying or copying secrets.
   Report which instruction paths and workflow operations were actually tested.
   A fixed-input writing comparison alone does not test hooks or persistence.

| Dimension | What to check |
|---|---|
| Intent and meaning | The requested result, relevant conditions, exceptions, failures and unresolved points remain. Existing article content outside the change is preserved. |
| Support | Claims stay within the evidence. Human decisions, observations, hypotheses and proposals remain distinguishable. A citation alone is not support. |
| Relevance | The reply answers the request and provides what the person needs to use the result. Internal steps do not displace that information. |
| Repetition | Each repeated point contributes a distinct condition, evidence or action. Unchanged knowledge does not become another lesson. |

Do not combine these dimensions into a score that compensates for lost meaning
or unsupported claims. An unresolved dimension stays unknown. Prefer a candidate
only when the targeted defect improves without a regression; otherwise retain
the earlier wording or investigate. Repeat uncertain cases and report mixed
results instead of declaring a general improvement. Word, byte and token counts
are not quality grades; latency and cost, if measured, are separate observations.

Record actual results through the existing `work record` observations and
result. Reuse `trial` and `comparison` only when they help the decision. A saved
comparison does not establish a human truth or authorize deployment.

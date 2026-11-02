# Use project experience while working

The host agent owns interpretation. These operations collect the project's evidence for a decision; they do not generate a verdict, permission or a success label. Use the same project on every MCP call.

## Check a proposed approach

Before settling an approach, pass the current task and your proposed approach to `shikii wiki consult` on stdin:

```json
{"request":"Route invoices for the new tenant","proposal":"Add a form before each routing decision"}
```

MCP uses `shikii_wiki` with `operation: "consult"` and the same fields; HTTP uses `POST /v1/wiki/notebook`. Each text field is at most 16000 UTF-8 bytes. CLI JSON is at most 1 MiB, including escaping; all valid field values fit. Read `checks`: the article's conditions, body, rationale, sources and changed premises, plus linked project guidance. Explain which evidence supports the approach, which gives a counterexample or exception, and what is still unknown. Quote exact sources and keep the current request first. A topic match or a link is not semantic agreement. When configured, the existing lesson-applies model contributes a bounded applicability check; it never supplies approval or a truth. An exclusion remains visible for inspection. The relevance helper accepts a request of 1500 bytes and a combined proposal/conditions target of 400 bytes; when the full context does not fit, `model_notice` explicitly skips that supplemental judgment instead of silently dropping conditions. The host still receives the full fields.

At most three topic candidates and six articles in all are returned, through at most two link hops. Bodies share a 16000-byte budget, at most 2400 bytes per article. Long articles provide exact `body_excerpts` with byte offsets: prioritized interpreted quotations and up to three unclassified excerpts share the same budget. Use them first; `body_omitted` and `omitted` name full revisions to read only when a material condition remains unresolved. No match means insufficient evidence, not freedom to infer a rule.

Consult reserves an independent search lane for up to two `counterexample_candidate` articles, within its six-article total. Candidates come from explicit `counterexample` body passages or source-backed failure outcomes. Failures are ranked individually against this request and proposal, so newer unrelated results do not hide an older matching failure. Returned `outcomes` retain conditions, the original procedure revision, corrections and checked evidence. An annotated counterexample and a topic match remain agent interpretations; neither establishes semantic contradiction, approval or success. Read each article's `orientation` before its mixed history and recheck changed premises.

## Turn a missed discovery into improvement

If a manually found article changes your action, record the usual discovery in reflection. When diagnostics show that the same revision did not arrive for this work, add that recorded `case_id`:

```json
{"discoveries":[{"lookup_id":"<lookup>","page_id":"<article>","revision":"<read revision>","context":"Actual work context","decision":"What changed in the action","evidence":"What was checked","case_id":"<diagnostic case>"}]}
```

Use `diagnose` to choose the case for the actual work; do not attach the latest unrelated case. Only complete, same-project diagnostics containing the exact live revision are accepted. Reading alone is not useful evidence. Cases of actual delivery or session suppression do not establish a miss. Missing history, an incomplete inventory or an old revision cannot be filled from today's knowledge.

Reflection's `maintenance` returns `missed-delivery`, the frozen reason and up to three examples. Read the article, sources and actual use; diagnose, capture evidence, compare a proposed applicability patch, then patch or record a reasoned no-change review. Normal review keys settle the same evidence and reopen for new evidence. A linked case and article revision identify one actual use even when another manual query returned the same article; changing the lookup alone cannot reopen review. Never change an article just to reset suppression. A better search candidate is not proof of actual delivery or usefulness: assess later actual work separately. Discoveries and these reviews create no human truths or calibration labels.

## Assemble a procedure for current conditions

For recurring work, pass `request` and current `conditions` to `shikii wiki reuse` (or MCP operation `reuse`, HTTP `POST /v1/wiki/notebook`):

```json
{"request":"Prepare the next release","conditions":"Linux and macOS; the installer changed since the earlier release"}
```

Read `steps` and `checks`, then assemble an ordered procedure for this task. For each step name the source, prerequisites, verification result, rejected approach and changed premise when supported. The service inspects at most 32 cited snapshots within a 2 MiB candidate-data budget per procedure, preferring recent test/release snapshots, and returns howtos, reasons and up to three cited source excerpts per procedure, not an assertion that every howto succeeded. Explicitly state absent or inconclusive success evidence. Long sources provide exact `content_excerpts` within 1200 bytes. `content_cut` means to read the source if an omitted portion is needed to establish the outcome. Current work records are supplementary context (`current_unverified`), not the immutable evidence of an earlier successful run.

Only a source reference starting with `.shikii/handoff/` is read as a work record, through the existing path, link and read-limit checks. Source excerpts are at most 1200 bytes; current record excerpts are at most 600 bytes, with `record_cut` when incomplete. An unavailable reference stays unavailable; other references are not fetched or executed. This operation never runs commands. Historical publication authorization never carries forward.

# Prepare knowledge, then record content

Use this as the ordinary workflow from 0.19. `work prepare` gathers the current conversation, related project knowledge and pending reflection. It retains the session, turn, source identifiers and exact article revisions. You interpret the evidence; `work record` handles storage details and calls the existing planning, reflection and wiki operations. No new settings, model or human form is needed.

## Prepare once

With the current request's hook identifiers, pipe this to `shikii work prepare`:

```json
{"session_id":"<current hook session>","turn_id":"<current hook turn>","query":"CSV export filters"}
```

Choose topic words in the project's vocabulary, not the whole request. Read `task`, `conversation`, `knowledge`, `pending`, `reflection_context`, `observations`, `maintenance` and `how_to_record`. The supporting `reflection_context` retains earlier human scoring corrections, related knowledge and memory statistics, including when scoring is settled but wiki work remains; read the corrections before interpreting or scoring. They remain historical evidence, never current permission or a new truth. Hook knowledge entries retain the article ID, revision and an exact-revision `query`. When `body_included` is false, read that query before interpreting the article; automatic body budgets remain unchanged. The response's `receipt` retains the storage context. Knowledge and quotations use local references such as `current`, `previous` and `knowledge1`; reflection uses `source1`, `review1`, `memory1` and `evidence1`.

Without a hook, use `{"query":"CSV export filters","request":"Check the CSV export"}`. Read `knowledge_context` as well as `knowledge`: it retains the ordinary query's search total and candidates, matches, issues, lookup receipt and remaining classification instructions. Bodies already supplied in `knowledge` are not repeated. Each candidate carries an exact-revision `query`; use it for bodies that were not supplied. When `next_cursor` is present, `knowledge_query` reads the next ordinary search page. A complete candidate list does not mean every body was read; finish any classification the query names. The query keeps its existing page budget, and the supporting metadata fits 128 KiB or preparation fails explicitly with the individual query workflow. This is explicitly an agent-supplied task. It can save observations, notes and prior reflection, but cannot create a human agreement or satisfy a hook planning ritual for another request. With a hook, the saved request takes priority over any agent-supplied request text.

A new preparation for the same hook turn invalidates the previous receipt. Keep using one receipt during the work. It expires with the hook session (7 days), and cannot be transferred to another project, turn or closed request. A receipt is a local reference, never authorization or a secret that grants access to another project.

## Record what you judged

Pipe content to `shikii work record`:

```json
{
  "receipt":"<returned receipt>",
  "agreed":{
    "summary":"Preserve the existing CSV filters.",
    "quotes":[{"ref":"current","text":"<verbatim words from the current request>"}]
  },
  "open":{"unknown":true},
  "observations":[{"kind":"test","title":"CSV filter check","content":"The synthetic CSV preserved tenant and date filters.","reference":"handoff/csv-check/result.txt"}],
  "notes":[{"title":"CSV filters","lesson":"Keep tenant and date filters in the CSV export.","applies_to":"CSV export","kind":"howto","scene":"exporting CSV","sources":["new1"]}]
}
```

This is the JSON shape, not fabricated evidence: substitute the actual observed result and exact human quotation. Agreement needs the person's own words. Keep each summary within 600 UTF-8 bytes and use one to three verbatim quotations, each at most 240 bytes. `open` means a concrete choice blocking this task; omit it or use unknown when none is established. `continues_previous` is true only for a continuation with no new request, false for an addition, and omitted when unsure. Unknown facts cannot contain a summary or quotations. Inspect `plan` for the existing decision result and advice; this organizing is never human truth or permission.

Observations are checked source snapshots, never commands to execute or references to fetch. `kind` defaults to agent; accepted kinds are human, agent, code, test and release. Supply the actual title and content; reference and version are optional. Shikii supplies capture time, a descriptive reference and its **recording** version by default, not an inferred version of the tested artifact. Set version when the artifact's actual version matters. Each observation's content fits 64 KiB of UTF-8; JSON encoding may grow within the 1 MiB canonical input limit. Retain longer reports in handoff and capture relevant evidence. `new1` refers to the first observation **in this call**, `new2` to the second. Responses return stable `observed1` references for later calls with the same receipt.

Notes carry the existing draft fields and prepared source references. Reading alone never creates a helpful assessment. New notes run through placement internally. When candidates need interpretation, `complete` is false and `pending` returns the note reference, candidate article references, their content and the available same-topic/conflict judgments. Read them and record either:

```json
{"receipt":"<receipt>","notes":[{"ref":"note1","target":"new"}]}
```

or the pending note's reference, a returned article target and exact edits. The choice is the agent's editorial work; do not ask the person to choose identifiers. Resolving a pending ref accepts only ref, target and exact edits for an existing article. A target is required. Draft metadata on a pending ref, or edits for a new article, are refused before publication. Each pending ref is bound to the resolved evidence and the complete input, so new1 from another batch cannot silently reuse earlier evidence. If placement fails, fix the reported cause or use the existing deeper wiki workflow; a failed model call is not a finding of no overlap.

A pending note with relationships, watched premises or a correction cannot be merged through exact text edits: record rejects it explicitly. Use ordinary wiki ingest for structural changes, or target new to retain the pending draft as a separate article.

For an existing article, use its prepared reference and precise edits:

```json
{"receipt":"<receipt>","notes":[{"target":"knowledge1","sources":["observed1"],"change_reason":"The checked behavior now includes date filters.","edits":[{"field":"body","before":"Preserve tenant filters.","after":"Preserve tenant and date filters."}]}]}
```

The recorded revision is checked atomically. Other sections, citations and relationships remain. A stale revision requires reading and reconciling with a fresh preparation; it never becomes permission to overwrite. Use the existing patch/ingest operations for structural changes such as replacements, related links or watched-file baselines. Keep current body limits and provenance requirements.

## Reflect without rebuilding identifiers

Use the prepared content to record:

- `reviews:[{ref,answer:"yes|no|unknown",reason}]` for provisional scores. Read the original question, inputs, model answer and human reply. Each prepared memory also retains its actual delivery request, agent, tool, phrase and time; distinguish these from the parent conversation. Agreement with a model is an agreed machine outcome, never a person's answer.
- `memory:[{ref,result,decision,evidence,correction_result?}]` for delivered memory. Use the existing result values (helpful, unused, harmful, unknown); a helpful result needs a concrete decision and checked effect. Preserve the first assessment. Never infer helpfulness from delivery, successful tests, silence or a save.
- `reviewed:["source1","evidence1"]` when you read a source and it supports no lasting lesson. Save any intended knowledge first.
- `maintenance:[{ref,reason}]` after checking the displayed task against its sources. A changed article or neighborhood invalidates the retained key.

The oldest two conversations include their source material and saved `originals`/`text_status` independently of whether scoring is still pending. Read these originals before turning a conversation into knowledge. Later heads are explicitly shortened and have no recording references: settle the complete sources and prepare again for the next ones. `more_pending` counts additional conversations. Each conversation or observation has a 128 KiB encoded source budget. Oversized, unreadable or limited historical originals are explicitly incomplete and have no recording reference; `source_query` gives the actual source ID and continuation cursor for the existing `shikii_wiki` query or `wiki query <id> continue:<cursor>`. Join `original_part` JSON fragments in order and use individual reflect/wiki operations after reading; preserve unknown when the evidence cannot be recovered. The supporting reflection context also fits 128 KiB: large related articles become exact-revision queries, and any remaining articles are counted. Human scoring corrections are kept whole; an oversized correction context fails preparation explicitly and points to individual reflection. Never assess shortened material as though it were complete.

## Check the result and retry

Inspect `complete`, `saved`, `errors`, `pending`, `plan` and `reflection`. Partial success is retained. Correct failed content and resend with the **same receipt**. Identical observations retain their capture time and ID, and published notes retain their revision even if a later checkpoint failed. Pending placement is resolved by its note reference. Resending the original content after resolving placement returns the saved result. Changed content is a new request to record; it does not overwrite an immutable assessment. Planning and explicitly requested reflection return their input-specific saved replies. A note's automatic remaining-work check runs fresh and consumes no extra durable key, including when the context is full.

`complete:true` means the named batch and its touched reflection obligations are saved. It is not completion of every pending conversation or of the person's task. A turn change or checkpoint failure stops remaining operations; a source or article already published before the interruption is reported separately. Model calls remain outside the session commit lock. Finish the rest of the prepared work and honor reflection's `left`; do not mark sources reviewed as a shortcut. Hook completion checks continue to inspect the original planning, review and wiki records. They are not bypassed by a receipt.

Fixed limits: canonical work input 1 MiB; observation content 64 KiB of UTF-8; CLI JSON transport 8 MiB (allows escaped text); at most 4 observations, 2 notes, 128 scores, 256 memory assessments, 64 reviewed sources and 8 maintenance reviews per batch. A receipt retains at most 32 observations and 128 saved or reserved recording keys within the existing 1 MiB session limit. Published-note recovery reserves its keys before publication, so later batches cannot consume them. New entries are checked before their storage effects; identical saved retries remain available at the limit. Split work or prepare again when asked. The MCP/HTTP transport retains its existing JSON limits.

## MCP and HTTP

MCP uses `shikii_wiki` with `operation:"prepare"` or `"record"`, and the payload above inside `work`. Pass the absolute working folder as `project` on every MCP call. HTTP uses the same operation and work object at **POST `/v1/wiki/notebook`**, with `Content-Type: application/json`, under the server's existing authentication and project boundary.

Individual `hook plan query/submit`, `reflect query/place/submit`, `wiki capture/ingest/patch` and diagnostics remain available for deeper operations and older integrations. Do not mix processes of different versions while using a prepared receipt: older session writers cannot preserve the new work context. Reopen agent sessions and restart the page/MCP after updating.

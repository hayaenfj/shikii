# Prepare knowledge, then record content

Use this as the ordinary workflow from 0.19. `work prepare` gathers the current conversation, related project knowledge and pending reflection. It retains the session, turn, source identifiers and exact article revisions. You interpret the evidence; `work record` handles storage details and calls the existing planning, reflection and wiki operations. No new settings, model or human form is needed.

## Prepare once

With the current request's hook identifiers, pipe this to `shikii work prepare`:

```json
{"session_id":"<current hook session>","turn_id":"<current hook turn>","query":"CSV export filters"}
```

Choose topic words in the project's vocabulary, not the whole request. Read `task_context` with `task`: stored request text includes joined human messages, while `conversation` contains bounded planning excerpts for exact quotations. `conversation_context` retains each reference's user/assistant role, source turn and known history order. Read history by its recorded order, never by reference names or random turn IDs; unknown order stays explicit. Stored request text can be truncated or unavailable in older sessions; read the original human message in the host conversation when the status says so. Task and conversation context each fit 128 KiB after JSON encoding or preparation fails explicitly; a failed preparation does not install a new receipt. Task text is context rather than an extra quote reference, permission or human truth. Read `task`, `conversation`, `knowledge`, `pending`, `reflection_context`, `observations`, `maintenance` and `how_to_record`. The supporting `reflection_context` retains earlier human scoring corrections, related knowledge and memory statistics, including when scoring is settled but wiki work remains; read the corrections before interpreting or scoring. They remain historical evidence, never current permission or a new truth. Hook knowledge entries retain the article ID, revision and an exact-revision `query`. When `body_included` is false, use the exact `body_excerpts` first: at most three passages and 2400 bytes per article, within a 16000-byte excerpt budget. They preserve byte offsets, the cited revision and rationale; they are incomplete evidence, not a generated conclusion. Follow the exact query only when a material condition or exception remains unresolved. Automatic full-body budgets remain unchanged. The response's `receipt` retains the storage context. Knowledge and quotations use local references such as `current`, `previous` and `knowledge1`; reflection uses `source1`, `review1`, `memory1` and `evidence1`.

Without a hook, use `{"query":"CSV export filters","request":"Check the CSV export"}`. Read `knowledge_context` as well as `knowledge`: it retains the ordinary query's search total and candidates, matches, issues, lookup receipt and remaining classification instructions. Bodies already supplied in `knowledge` are not repeated. Each candidate carries an exact-revision `query`; use it for bodies that were not supplied. When `next_cursor` is present, `knowledge_query` reads the next ordinary search page. A complete candidate list does not mean every body was read; finish any classification the query names. The query keeps its existing page budget, and the supporting metadata fits 128 KiB or preparation fails explicitly with the individual query workflow. This is explicitly an agent-supplied task. It can save observations, notes and prior reflection, but cannot create a human agreement or satisfy a hook planning ritual for another request. With a hook, the saved request takes priority over any agent-supplied request text.

A new preparation **without a receipt**, or an individual `hook plan query`, for the same hook turn replaces the quotation context and invalidates the previous receipt. An in-flight planning result bound to that receipt is refused after replacement. Keep using one receipt during the work. It expires with the hook session (7 days), and cannot be transferred to another project, turn or closed request. A receipt is a local reference, never authorization or a secret that grants access to another project.

## Add knowledge during the same work

These additions are available from 0.20.0. Keep the original receipt and pass one of the following to `work prepare`:

```json
{"receipt":"<receipt>","query":"invoice ownership"}
```

```json
{"receipt":"<receipt>","proposal":"Keep invoice ownership checks in the export"}
```

```json
{"receipt":"<receipt>","conditions":"Linux, ordinary user; the installer changed"}
```

A query searches or reads an exact `lesson:<id>@<revision>`. Follow `knowledge_query` with its `cursor` for another page. A proposal runs consult against the retained task; conditions run reuse. Do not combine these three modes or replace session, turn or task fields. `experience` contains the same conditions, rationale, applicability checks and evidence as the individual operations; `knowledge[].experience_check` identifies its article's check without repeating the passages. New article revisions get distinct `knowledgeN` references; repeated reads reuse their reference. Earlier observations, pending notes, quotation references, saves and retries remain intact. At most 64 article revisions are retained; an addition that exceeds a response or session limit fails explicitly rather than replacing the receipt. Task role, recorded truncation and agent-supplied status remain explicit. Article references indicate supplied evidence, never usefulness or approval.

Initial preparation and mid-work additions register supplied passages through the same quotation path. The returned article summaries and exact passages can be quoted with their `knowledgeN` reference in `agreed` or `open`. Only supplied text is quoteable; omitted bodies require an exact-revision read. Quote sources and work state are saved together, preserving earlier quotations and the session size limit. The retained knowledge context is cumulative: added full bodies join `read`, while excerpts alone do not. Human agreement still requires the person's words, and stale, deleted or replaced revisions are refused for new planning evidence.

Consult and reuse accept at most 16000 UTF-8 bytes per text field, including the retained task. A longer task accepted by initial preparation cannot use these receipt modes. Use the individual wiki operations with an explicitly scoped request within their limits; retain unresolved conditions and do not silently truncate the original task.

When an article changes an actual decision, use `work record` with `discoveries:[{ref,context,decision,evidence,case_id?}]`. The recorder retains the article identity and records the normal lookup/discovery internally. Each text is 1–1000 UTF-8 bytes. A diagnostic `case_id` must still name the actual missed delivery; the existing validation and maintenance workflow applies. The recorder binds discovery identity to the actual work turn internally, so separate work can use the same article without a collision; preparing again in the same turn does not invent a new use. Unchanged work retries recover an already published discovery even after article edits, deletion or replacement. New uses still require a live revision. Definite validation refusals consume no saved/reserved capacity, including a revision change between preflight and publication; uncertain storage errors keep a recovery slot. Direct lookup submissions retain their existing live-revision requirement.

## Return execution results to the next procedure

After using a referenced howto, save the checked evidence and the execution together:

```json
{
  "receipt":"<receipt>",
  "observations":[{"kind":"test","title":"Permission check","content":"The synthetic check passed under an ordinary user."}],
  "outcomes":[{
    "ref":"knowledge1",
    "conditions":"Linux, ordinary user",
    "action":"Run the permission check",
    "failure":"The earlier run as root did not test denial",
    "correction":"Repeat under an ordinary user",
    "result":"The expected denial was observed",
    "sources":["new1"]
  }]
}
```

Use actual results, not this example as evidence. Conditions, action and result are required; failure and correction are optional, each field at most 1000 UTF-8 bytes. An outcome references the same live howto revision and one to four prepared captured-observation references. The recorder captures an immutable agent report and indexes its evidence under the existing search records. Reuse returns up to two recent outcomes per selected howto: prior conditions, action, failure, correction, reported result, original revision and cited evidence. It flags a changed procedure revision and keeps current conditions separate. Evidence excerpts retain the normal 1200-byte bound; agent reports without test/release snapshots explicitly lack success evidence. Compare these records with the current conditions, interpret the evidence, and save supported howto changes through notes. No outcome generates success labels, human truths or calibration; reading a test snapshot still requires judging what it proves.

The outcome index retains at most 128 records within the search journal's existing 1 MiB bound; immutable source snapshots remain in wiki-sources. Saved/reserved outcomes consume the normal receipt capacity. The internal execution snapshot also consumes one of the 32 observation slots; its availability is checked before a new outcome reserves capacity. Definite publication rejection releases its reservation; uncertain storage errors retain recovery capacity. Unchanged retries preserve their snapshot and can recover a published outcome even after its howto changes. New writes from an old revision are refused. Update all writers sharing these records before using these operations: older writers do not preserve the new context and outcome index.

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

The recorded revision is checked atomically. Other sections, citations and relationships remain. A stale revision requires reading and reconciling the current article, using the same receipt for an additional lookup when possible; it never becomes permission to overwrite. Use the existing patch/ingest operations for structural changes such as replacements, related links or watched-file baselines. Keep current body limits and provenance requirements.

## Reflect without rebuilding identifiers

Use the prepared content to record:

- `reviews:[{ref,answer:"yes|no|unknown",reason}]` for provisional scores. Read the original question, inputs, model answer and human reply. Each prepared memory also retains its actual delivery request, agent, tool, phrase and time; distinguish these from the parent conversation. Agreement with a model is an agreed machine outcome, never a person's answer.
- `memory:[{ref,result,decision,evidence,correction_result?}]` for delivered memory. Use the existing result values (helpful, unused, harmful, unknown); a helpful result needs a concrete decision and checked effect. Preserve the first assessment. Never infer helpfulness from delivery, successful tests, silence or a save.
- `reviewed:["source1","evidence1"]` when you read a source and it supports no lasting lesson. Save any intended knowledge first.
- `maintenance:[{ref,reason}]` after checking the displayed task against its sources. A changed article or neighborhood invalidates the retained key.

The oldest two conversations include their source material and saved `originals`/`text_status` independently of whether scoring is still pending. Read these originals before turning a conversation into knowledge. Bounded conversation fields without retained originals remain incomplete even when scoring has no targets; source_query can inspect a stored excerpt but cannot restore an omitted tail. Read the host conversation or a verified archive, or keep missing conditions unknown. Later heads are explicitly shortened and have no recording references: settle the complete sources and prepare again for the next ones. `more_pending` counts additional conversations. Each conversation or observation has a 128 KiB encoded source budget. Oversized, unreadable or limited historical originals are explicitly incomplete and have no recording reference; `source_query` gives the actual source ID and continuation cursor for the existing `shikii_wiki` query or `wiki query <id> continue:<cursor>`. For conversation originals, join `original_part.json` pieces in order and parse the combined JSON. For observations, follow `observation_part.from` and join `observations[].content` from every part, including the last. Use individual reflect/wiki operations after reading; preserve unknown when the evidence cannot be recovered. The supporting reflection context also fits 128 KiB: large related articles become exact-revision queries, and any remaining articles are counted. Human scoring corrections are kept whole; an oversized correction context fails preparation explicitly and points to individual reflection. Never assess shortened material as though it were complete.

## Check the result and retry

Inspect `complete`, `saved`, `errors`, `pending`, `plan` and `reflection`. Partial success is retained. Correct failed content and resend with the **same receipt**. Identical observations retain their capture time and ID, and published notes retain their revision even if a later checkpoint failed. Pending placement is resolved by its note reference. Resending the original content after resolving placement returns the saved result. Changed content is a new request to record; it does not overwrite an immutable assessment. Planning and explicitly requested reflection return their input-specific saved replies. A note's automatic remaining-work check runs fresh and consumes no extra durable key, including when the context is full.

`complete:true` means the named batch and its touched reflection obligations are saved. It is not completion of every pending conversation or of the person's task. A turn change or checkpoint failure stops remaining operations; a source or article already published before the interruption is reported separately. Model calls remain outside the session commit lock. Finish the rest of the prepared work and honor reflection's `left`; do not mark sources reviewed as a shortcut. Hook completion checks continue to inspect the original planning, review and wiki records. They are not bypassed by a receipt.

Fixed limits: canonical work input 1 MiB; observation content 64 KiB of UTF-8; CLI JSON transport 8 MiB (allows escaped text); at most 4 observations, 2 notes, 128 scores, 256 memory assessments, 64 reviewed sources and 8 maintenance reviews, 4 discoveries and 2 procedure outcomes per batch. A receipt retains at most 32 observations and 128 saved or reserved recording keys within the existing 1 MiB session limit. New notes freeze the validated watched-premise hashes and exact preview revision for publication; later file changes remain visible as recheck. Published-note recovery reserves its keys before publication, so later batches cannot consume them. New entries are checked before their storage effects; identical saved retries remain available at the limit. Split work or prepare again when asked. The MCP/HTTP transport retains its existing JSON limits.

## MCP and HTTP

MCP uses `shikii_wiki` with `operation:"prepare"` or `"record"`, and the payload above inside `work`. Pass the absolute working folder as `project` on every MCP call. HTTP uses the same operation and work object at **POST `/v1/wiki/notebook`**, with `Content-Type: application/json`, under the server's existing authentication and project boundary.

Individual `hook plan query/submit`, `reflect query/place/submit`, `wiki capture/ingest/patch` and diagnostics remain available for deeper operations and older integrations. Do not mix processes of different versions while using a prepared receipt: older session writers cannot preserve the new work context. Reopen agent sessions and restart the page/MCP after updating.

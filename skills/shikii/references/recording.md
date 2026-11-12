# Prepare knowledge, then record content

Use this as the ordinary workflow from 0.19. `work prepare` gathers the current conversation, related project knowledge and pending reflection. It retains the session, turn, source identifiers and exact article revisions. You interpret the evidence; `work record` handles storage details and calls the existing planning, reflection and wiki operations. No new settings, model or human form is needed.

Built-in instructions use English independently of the person's display language. Keep requests, wiki quotations, reported reasons and other source text in their original language. Configured-language question/answer labels are display evidence, not translated instructions. Write replies and work-record prose in the person's requested language.

## Ordinary work takes two calls

1. Call prepare once with the host's hook identifiers and topic words. Without
   hooks, supply an agent task with query/request. Preparation saves a timestamped
   start, not an invented AI judgment or human agreement.
2. At the end, record once with its receipt and the following content. Combine
   any needed notes, provisional reviews, memory and maintenance in that call.

```json
{"receipt":"<receipt>","result":{
  "changed":"Checked the existing export; no new lasting knowledge.",
  "checked":"The synthetic two-tenant export retained the owner filter.",
  "evidence":["new1"],"remaining":""
},"observations":[{"kind":"test","title":"Export check",
  "content":"Only the requested tenant's synthetic row was exported."}]}
```

`changed` is required (12000 UTF-8 bytes); checked (12000), evidence (up to four
prepared refs) and remaining (4000) are optional. Unknown/unobserved checks stay
absent or explicitly unknown. Write an execution report once: `new1` and later
`observedN` (or the id a reply gave for a source this receipt saved) reference
captured evidence, and an article this receipt edited stays citable by its ref; `result` references the immutable agent
report and can supply sources for notes, and for outcomes when the result cites evidence of its own. A result is a report, not
an observation: evidence never cites it, nor another report of a work or a page every source of which is one. An observation
of kind agent (or of no kind) cannot take a title shikii gives its own records (the result, the ending review, a trial, a measured
reading); an observation of another kind keeps its title. A reusable wiki lesson may
summarize its implication without repeating the complete result. No new knowledge
means no wiki draft. Important corrections, failures and new procedures still
need source-backed notes; short messages do not make them optional.

The host supplies project/session/turn and retains the receipt; never invent
missing metadata or collect shell history, environment values or secret-bearing
logs to fill it. Capture only checked, redacted evidence. Human quotations stay
human sources; result is an agent report, never a human truth or proof of success.
Continued work can send only its new result and notes under the same receipt.

Inspect storage.status: saved means the original result and requested work were
saved; saved-pending means the original exists but some organization/review remains;
unconfirmed means persistence was not confirmed. complete concerns the submitted
recording, not success of the underlying task. Retry failed/pending refs only,
without restating a saved result. Required-note recovery retains its original
payload and evidence across preparation. Missing sources and stale revisions need
explicit recovery; neither is an evaluated success. The existing Stop checks keep
unresolved saves visible. Legacy explicit reasoning start/update/finish remains
supported; the prepared start lets an ordinary ending report close the review.

### Automatic improvement of loop procedures

Stalled work can generate a bounded procedure candidate without another agent
report, model or setting. Candidates can change only the timing of a method-change
suggestion and its concise guidance. Source recovery, required checks, permissions
and evaluation rules stay outside the candidate's control.

When an actual check is available, optionally add `sufficient:true` or `false` to
result and reuse its existing captured evidence. Omission remains unknown; do not
infer quality from task completion or delivery. A result and reading report in the
same batch can share the checked quality/evidence. Conflicting grades are refused.
The same execution's result and reading evidence are checked together before
positive procedure support, including ref recovery. Another task's backlog stays
separate. An unavailable auxiliary check cannot add positive support or hide an
actual checked negative.
An empty or unsupported quality result is saved as `ungraded` or `ineligible`,
with a short notice, and contributes no support. An unsupported explicit grade
leaves the ending assessment unresolved. It does not create a permanent storage
retry: report a real later check when one is available. Missing or changed source
material remains a recoverable error. Actual safety or cost regressions still
restore the previous procedure.
The host keeps task IDs, revisions, selected procedure and paired guidance-byte
cost; no separate trial plan, comparison, long reflection or human grade is needed.

A candidate's generating task is excluded from validation. At most six task
reservations can try it, and two different tasks with distinct checked results and
lower guidance cost are required before adoption. Source renaming, transport
metadata and equivalent JSON numbers do not multiply support. A quality failure,
higher guidance cost, or lost supporting evidence rejects the candidate or restores
its exact previous procedure, including descendants that depended on it. A failed
recipe is retired; the next different recipe uses more held-out checks and a more
conservative method-change timing. The current recipe space has two levels.

`attention.procedure` appears briefly when its advice is used; unchanged notices
and the full recording contract are not repeated on receipt extensions. `procedure`
in the ending result gives status, policy, support count and an immutable execution
source. Inspect that source only when needed. The original report, pinned evidence
bindings and procedure reference remain in the immutable ending review after the
session expires. Reported quality is evidence, not independently established truth
or general LLM quality. Guidance bytes are not token or elapsed-time savings.

Original reports are saved before procedure indexing. If indexing is interrupted,
prepare with the same receipt to retry and finish an ordinary saved report
without restating it. Unindexed feedback also survives the next human request and
is resumed during preparation. Required wiki repairs and explicit custom judgments
keep their ordinary checks. Used test/code evidence is retained without another
manual reflection; human statements and general observations keep semantic review.

The journal stops new optimization at its fixed scope/report/storage bounds and
uses the default procedure while preserving earlier evidence. No original source,
human preference or history is deleted. Use matching updated binaries and skills
for all session writers, then reopen sessions and restart MCP; older writers do
not preserve this new session progress.

### Bounded reading and memory upgrades

prepare accepts optional max_bytes, 8192–393216 UTF-8 JSON bytes, for the complete
response (default 65536; 28672 bytes on the CLI, a size an agent's shell shows whole; a longer request is not cut, and the reply grows to hold it; the hook keeps 64 KiB of a request, and `task_context` says when it kept less).
output reports omitted fields and recovery directions.
Use exact article/source queries only when necessary. Omitted text is not supplied
or read; unavailable conditions, counterexamples and premise warnings remain
unresolved. Quoted excerpts keep sentence/line boundaries and byte offsets; a unit
that cannot fit is omitted instead of returning a heading fragment. Same-receipt
full bodies and identical excerpts are reused for repeated broad reads; excerpt
reuse never establishes a full-body read. Explicit pinned recovery, changed
premises and mandatory checks retain priority. Repeated searches or challenged
trials without progress suggest changing the method, never skipping required work.

Repeated host reading notices may appear once, verbatim, in reading_notices.
An entry's reading_notice then points to /reading_notices/noticeN in the same
response. Read that shared notice with each entry's incomplete status and exact
query. Article text, quotations and source fields keep their original values.
Sharing is used only when the complete transport response becomes smaller.

Successful preparations carry a receipt-local `delivery` number. On repeated
broad reads, `metadata_from:{delivery,fields}` reuses only the named fields from
the same article ref in that earlier response. It may omit unchanged summaries,
applicability text, rationale and current/history interpretations after the body
or exact excerpts were already supplied. Unclassified text, recheck material,
counterexamples, incomplete status and changed premises remain visible during this
reference substitution. All preparation responses still obey `max_bytes`: budget
reduction may replace article text or orientation with an incomplete marker and
the exact revision query. Treat those omitted conditions as unread and unresolved.
Match
the same receipt, ref, article revision and delivery before reusing a field;
use the exact revision query if that earlier response is unavailable. An explicit
recovery bypasses reuse, but complete text and metadata need a sufficient response
budget or the individual exact-revision query. References do not establish a full read,
usefulness, agreement or permission, and mixed delivery cannot teach reading depth.
The actual transport must shrink; state is retained only after final sizing.
`task` and `task_context` are never dropped to meet `max_bytes`: a budget too
small for the current request fails explicitly before replacing its receipt.

When every selected article is already reusable under the same receipt, an
unchanged host-authored `collective.notice` may also name an earlier `delivery`
in `notice_from`. Its short notice names that field and its recovery path. Only
the fixed explanation is shared; updates, contrasts, investigations, omissions
and recovery references retain their values. If the earlier response is no
longer available, repeat the preparation with the same receipt/query and
`reasoning:{depth:focused}` to recover the full notice. Exact-revision queries,
explicit reading depths, changed notice text and a new receipt retain full
guidance. Sharing is used only when the complete transport becomes smaller;
failed or omitted deliveries never establish a reusable notice.

A shipped memory-policy epoch starts a source-preserving review of existing active
wiki pages, with English progress "Upgrading memory…". Routine patch releases do
not restart a completed epoch. At a break in work, prepare with the same receipt
and query memory-upgrade for at most two full pinned articles. Compare original
sources with current behavior, retain still-valid knowledge, correct obsolete
conditions, classify superseded procedures as history, and consolidate duplicates
through ordinary source-preserving edits/replaces. Preserve explicit preferences
and historical human decisions. Review the latest returned maintenance ref after
an edit; reading alone never advances progress. Review one batch per ordinary task,
then resume on the next task; durable progress survives restarts. An explicit request
to finish the upgrade may continue batches, renewing context at its article limit. Unavailable
sources stay blocked and must not be deleted to claim completion. Hosts running
assigned child agents do not automatically schedule this project-wide migration.

Related-page version lineage alone does not reopen a reviewed article when all
stored content, conditions, sources and edit reasons are exactly unchanged. The
edited article itself still needs review. New evidence, changed premises and the
30-day review remain active; older checkpoints without comparison evidence keep
the previous exact-version behavior. Review submissions still require the latest
revision and key, including current related revisions.

## Keep records brief

- Use short bullets in agent-authored judgment, note and report text; one point per bullet. Reuse context and cite evidence once.
- Explain a failure with the relevant expectation, observation, cause, correction/next check and evidence. Label an unverified cause as a hypothesis or unknown; omit inapplicable items.
- Preserve deciding conditions, exceptions, uncertainty and source/revision refs. Keep exact quotations and original source material unchanged.
- Keep the existing JSON fields, types and byte limits. Bullets belong inside text values, not new fields or arrays. Small replies need only the required short review; trials and comparisons remain optional.

## Prepare once

With the current request's hook identifiers, pipe this to `shikii work prepare`:

```json
{"session_id":"<current hook session>","turn_id":"<current hook turn>","query":"CSV export filters"}
```

`shikii work --help` prints these forms, runnable as written. A reply starts with `receipt`, then `output` when something was left out, the task, `how_to_record`, the lessons and what is pending; the rest follows from small to large, so the head of a long reply is enough to act. A refused call names its cause and the next call; an unknown field comes back with the object that held it and the fields that object takes.

Choose topic words in the project's vocabulary, not the whole request. Read `task_context` with `task`: stored request text includes joined human messages, while `conversation` contains bounded planning excerpts for exact quotations. `conversation_context` retains each reference's user/assistant role, source turn and known history order. Read history by its recorded order, never by reference names or random turn IDs; unknown order stays explicit. Stored request text can be truncated or unavailable in older sessions; read the original human message in the host conversation when the status says so. The task takes up to the largest reply less 16 KiB after JSON encoding; a task beyond that (nearly all characters JSON escapes) comes cut, and `task_context.text_status` says `stored_request_truncated`. Conversation context fits 128 KiB after JSON encoding or preparation fails explicitly; a failed preparation does not install a new receipt. Task text is context rather than an extra quote reference, permission or human truth. Read `task`, `conversation`, `knowledge`, `pending`, `reflection_context`, `observations`, `maintenance` and `how_to_record`. The supporting `reflection_context` retains earlier human scoring corrections, related knowledge and memory statistics, including when scoring is settled but wiki work remains; read the corrections before interpreting or scoring. They remain historical evidence, never current permission or a new truth. Hook knowledge entries retain the article ID, revision and an exact-revision `query`. When `body_included` is false, use the exact `body_excerpts` first: up to three unclassified passages plus prioritized interpreted quotations, within the effective `attention` excerpt budgets described below. They preserve byte offsets, the cited revision and rationale; they are incomplete evidence, not a generated conclusion. Follow the exact query only when a material condition or exception remains unresolved. Automatic full-body budgets remain unchanged. The response's `receipt` retains the storage context. Knowledge and quotations use local references such as `current`, `previous` and `knowledge1`; reflection uses `source1`, `review1`, `memory1` and `evidence1`.

Without a hook, use `{"query":"CSV export filters","request":"Check the CSV export"}`. Read `knowledge_context` as well as `knowledge`: it retains the ordinary query's search total and candidates, matches, issues, lookup receipt and remaining classification instructions. Bodies already supplied in `knowledge` are not repeated. Each candidate carries an exact-revision `query`; use it for bodies that were not supplied. When `next_cursor` is present, `knowledge_query` reads the next ordinary search page. A complete candidate list does not mean every body was read; finish any classification the query names. The query keeps its existing page budget, and the supporting metadata fits 128 KiB or preparation fails explicitly with the individual query workflow. This is explicitly an agent-supplied task. It can save observations, notes and prior reflection, but cannot create a human agreement or satisfy a hook planning ritual for another request. With a hook, the saved request takes priority over any agent-supplied request text.

A new preparation **without a receipt**, or an individual `hook plan query`, for the same hook turn replaces the quotation context and invalidates the previous receipt. An in-flight planning result bound to that receipt is refused after replacement. Keep using one receipt during the work. It expires with the hook session (7 days), and cannot be transferred to another project, turn or closed request. A receipt is a local reference, never authorization or a secret that grants access to another project.

## Required review while doing the work

Every request hook keeps a turn-specific recording obligation. Preparation saves
its start automatically. The compact result above normally supplies the ending
review. Optional explicit reasoning records describe a consequential judgment;
no separate start call is required for an ordinary answer.
Record only a concise decision summary and checkable grounds, not private
step-by-step deliberation. Combine it with the usual planning or observations:

```json
{"receipt":"<receipt>","reasoning":{
  "phase":"start",
  "focus":"Could the export lose an existing ownership condition?",
  "assessment":"retain",
  "reason":"Compare the current request and the procedure's actual conditions.",
  "evidence":["current","knowledge1"],
  "next_action":"Check the synthetic export under the existing owner condition."
}}
```

`phase` is `start`, `update`, or `finish`; assessment is `revise`, `retain`, or
`unresolved`. Focus, reason, next_action and a finishing result each fit 1200
UTF-8 bytes. Use one to four evidence refs: `current`, supplied articles, or
captured observations (`new1` can name an observation in the same batch). The
recorder verifies their source availability and current article revisions and
pins their actual identities. Conditions and semantic relevance remain the
agent's responsibility. Unknown, incomplete and stale references are refused.

If using explicit judgments, start before the work. Use update when evidence changes the premise or
next action. After checking the result, record finish with `result`, the same
other fields and the relevant evidence. Finish accepts the prepared start or an explicit saved starting judgment. New saved
observations, uses, outcomes or notes reopen the ending review; replaying an old
finish does not certify later evidence. Each ending review keeps an immutable
agent source containing the start, latest judgment and result so the next
request can inspect it. It creates no success label, human answer or permission.
The current request keeps one source identity throughout its turn. Replay keys
retain their first generation in project records across preparations and receipt
capacity; evidence aliases and ordering do not turn an old finish into a new one.

A batch's top-level `complete` means its submitted operations were saved;
`reasoning.complete` concerns this review. Neither establishes that the original
task is done. Retaining the approach and recording an unresolved condition are
valid. Small conversational tasks need only short judgments about the actual
request; do not invent an experiment, discovery, article or procedure outcome.
Reusable findings and new project procedures still go to the wiki as usual.

Resolve failed or pending wiki notes and captures before finishing. Saved retry metadata alone
is not pending work. Replacing preparation preserves the start, latest review
and pending obligation. When drafts or publication bindings exist, it first
archives their original content and reference bindings as an immutable source;
`reasoning.pending_source` names it for `wiki query`. Read that source and restore
still-needed edits using their original sources and current article revisions.
Use the ordinary wiki operations for structural recovery and record the checked
note with the current receipt. A fresh preparation alone never resolves the
obligation. `reasoning.pending_notes` maps each outstanding required-note ref to
its immutable original intent and prepared bindings. An error returns that ref.
`pending_note_targets` supplies original article IDs to read with prepare before
repairing their current revisions, including when the archive is unavailable.
Retry the same content with corrected sources, or send the required ref with the
full corrected note and a current target. A ref-only retry with target and edits
restores the original draft and known source bindings. An existing-article repair
must still address the original article. Placement refs continue to accept only
ref, target and edits. Correcting one note never resolves another failed note.
Placement refs belong to one preparation and are not reused for a different
note within it. Across preparations, use the stable required-note ref returned
in `reasoning.pending_notes`.
If the note was published before its checkpoint failed, retrying the same intent
confirms that exact published revision and finishes its source/checkpoint work
without creating another article. A changed or retired publication needs a
current target for the original article. When publication never occurred, only
the same intent may retry after the original base and missing history are checked.
At most 128 notes may remain outstanding; resolve them before adding more.
`reasoning.pending_captures` retains each incomplete capture's original content,
reference, recording version and time across preparations. Retry that observation
to confirm the same source, including when the source was written but its
checkpoint failed. A new finish cannot skip an unconfirmed capture.
If reserving required work exceeds the session limit, the recorder archives its
original context, retires that receipt and retains the obligations on the same
turn. Prepare again before retrying. If archiving also fails, the compact turn
state retains the original payload until the source store recovers.
This also permits recovery at the session-size limit without losing
the old drafts. A different turn cannot reuse another turn's judgments. `Stop` checks the required saves and ending article revisions before
judging completion and uses the existing progress-bounded recovery. The common
article lock covers each ending-revision check and completion publication,
including the final session write; it is released during model inference.
The common hooks verify saved records, not the internal thought process or every tool's
execution order. No new model, setting or person scoring is needed. A standalone
agent-supplied preparation can record the same review, citing `newN` or
`knowledgeN` (it has no `current`), but cannot satisfy another hook turn. Update the binary and skills and reopen agent sessions together before
using this contract; older writers do not preserve this progress.

## Expectations, execution and the next check (0.27.0)

Use a trial when an observable result can change the current decision. Keep the
ordinary short review for small conversational tasks. A trial records a plan and
an agent-reported execution; it never runs the method or grants permission.
After reasoning start, send a plan through `work record`:

```json
{"receipt":"<receipt>","trial":{"plan":{
  "case":"owner-fixture-v1","question":"Does the export preserve the owner condition?",
  "expected":"Only the requested owner's rows remain.",
  "method":"Run the same synthetic two-owner fixture.",
  "conditions":"UTF-8 CSV, two owners, fixed fixture v1",
  "decision":"Repair the filter before reuse if another owner's row remains.",
  "effort":"small","evidence":["knowledge1"]
}}}
```

Every text field fits 1200 UTF-8 bytes; effort is small, medium or large and
one to four evidence refs are required. The response returns an immutable
`trial:...` ref in `saved` and `reasoning.trials`. Plan refs belong to the current
turn, survive renewed preparation, and keep the exact article/source identities.
`reasoning.next_trial` suggests the smallest reported effort among unresolved
checks with current evidence; ties use stable ref order. Its stated decision
explains what could change. It is not an optimal information-gain estimate.
Use `reasoning.next_trial` on an update to select another current unresolved ref.
Changed or unavailable plan evidence appears in `stale_trials`; read and replan.

Actually perform the authorized check before reporting its result. For example:

```json
{"receipt":"<receipt>","observations":[{
  "kind":"test","title":"Owner fixture result","content":"<actual checked output>"
}],"trial":{"ref":"<trial ref>","result":{
  "assessment":"challenges","observed":"<what actually differed>",
  "evidence":["new1"],"measurements":{"missed_conditions":1}
}}}
```

Assessment is supports, challenges or inconclusive. Observed text fits 1200
bytes; evidence has one to four refs. At most four nonnegative finite values up
to 1e15 can be reported, using short lowercase metric names that include units
when needed (for example `elapsed_ms`). Use the actual observation, not an
example or an earlier execution. Each trial accepts one immutable result. A
new condition, evidence basis or work turn needs a new trial.

A challenged result reopens the ending review. Cite its trial ref in reasoning
update or finish and state the supported decision; retaining a decision or an
unresolved result is valid. No new article is required without a reusable
finding. Pending plan/result captures retain their original payload; recover with
`{"receipt":"<receipt>","trial":{"ref":"<trial ref>"}}` after preparing again if
necessary. An unrelated result cannot repair them. Identical result retries do
not create observations or advance the evidence generation. Eight trials fit a
turn. Results remain immutable project sources after the turn ends.

For a matched comparison, perform both executions and record their results with
the same case, question, conditions, method, expected outcome and nonempty metric
names/units. Record changed knowledge or implementation as their evidence rather
than changing those fixed case fields. Then send:

```json
{"receipt":"<receipt>","comparison":{
  "before":"<first trial ref>","after":"<second trial ref>",
  "reason":"Compare the same fixture using the two recorded procedures."
}}
```

The response's `comparison` returns before/after/delta for each metric and an
immutable source. Before/after can also be captured-source refs or returned
execution source IDs from this project, allowing later work to compare an earlier
execution. The original saved plan and execution evidence are checked; unrelated
cases, absent evidence and the same execution are refused. Eight comparisons fit
a turn. Recover an interrupted comparison with only its `ref`. Cite the returned
comparison ref as reasoning evidence. This differs from `wiki compare`, which
only compares retrieval candidates. Paired reported values establish neither
causality nor improvement; check conditions, units, model/tool versions and
execution order. They never create human truths or automatically promote rules.

`reasoning.depth` accepts brief, focused (default) or deep. To choose it on the
first preparation, include only `reasoning:{"depth":"brief"}` alongside the usual
query and hook identifiers. Prepare records a reading intent, not a judgment;
start is still required after reading. The same depth-only shape works with a
receipt to adjust the next read. Other reasoning fields belong to record. Brief
supporting reflection articles are exact revision references instead of repeated
whole bodies; human corrections and pending original material are retained.
Prepare returns
`attention` with the effective depth, its reasons and exact-excerpt budgets:
600/2400, 1200/8000 or 2400/16000 bytes per article/total respectively. A broad
brief search supplies exact passages instead of whole bodies. Explicit pinned
`lesson:<id>@<revision>` reads retain full recovery. Unresolved judgments,
unconfirmed saves, unreviewed discrepancies or changed premises require deep
checking. Planning, original-source recovery and mandatory reflection are never
omitted by this policy. Use the matching binary and skills for every writer of
these new turn fields; old writers do not preserve them.

## Add knowledge during the same work

### Read the current knowledge before its chronology

Prepared articles carry `orientation`: `summary` and `conditions` are the current article summary, while `current`, `history`, `recheck` and `counterexamples` contain the host agent's explicit interpretations of exact body quotations. Each has byte offsets, conditions and a source-backed reason. These roles are hypotheses. Unclassified text remains under `unclassified`; `needs_interpretation`, `omitted` and `premise_changed` must be checked before relying on the interpretation. Dates never determine which statement is valid.

After reading the sources, save up to eight `passages` on a note. Each entry is `{role,quote,conditions,reason}`: role is `current|history|recheck|counterexample`, quote is 1-600 UTF-8 bytes and must match the body exactly once, and conditions and reason are each 1-400 bytes. Quotes cannot overlap. On an existing target, sources and change_reason plus passages may be sent without text edits. Omission preserves previous roles; `[]` clears them. Body edits that invalidate a quote must update passages in the same revision-checked patch. A new note uses its draft body; a pending note's structural metadata must be reconciled rather than silently discarded. Interpreted passages share the 2400-byte per-article excerpt budget; ordinary unclassified excerpts retain their three-passage limit. Source roles, old revisions and human agreement boundaries remain unchanged.

```json
{"receipt":"<receipt>","notes":[{"target":"knowledge1","sources":["observed1"],"change_reason":"Separate the checked current condition from earlier context.","passages":[{"role":"current","quote":"Exact text already in the article body.","conditions":"Conditions checked in the cited source.","reason":"Why the cited evidence supports this interpretation."}]}]}
```

Update all writers sharing a wiki and restart its agent sessions, page and MCP before using interpreted passages. Older binaries cannot validate the new article metadata and refuse saves; they do not remove the saved sources or history.

### Extend the receipt

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

An explicit pinned full read of a current article can refresh an already prepared maintenance reference. The article entry returns `maintenance_reviews` with the same ref and its latest revision, key, dependencies and feedback. Inspect the changed conditions and original evidence before recording that review. Historical revisions, omitted bodies and articles outside the prepared review set do not refresh references. The new key is retained only after the full response passes its budget and the context is saved. This recovery does not complete a review or expand the two-article upgrade batch.

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

Record only a procedure you actually executed, using the conditions and observations of that execution. Do not copy conditions or a result from an earlier run; leave outcomes empty when no procedure was executed. Conditions, action and result are required; failure and correction are optional, each field at most 1000 UTF-8 bytes. An outcome references the same live howto revision and one to four prepared captured-observation references. The recorder captures an immutable agent report and indexes its evidence under the existing search records.

Reuse returns at most two candidate outcomes per selected howto, with the earlier execution's conditions, action, failure, correction, reported result, original revision and cited evidence. It matches content terms from the current request and conditions against each earlier outcome's conditions first, then its action/failure/correction; recency breaks ties. If the first two candidates contain no failure, an earlier failure can take one slot when its positive condition-term score equals the top candidate's. Empty or unmatched context keeps recent-first order. Term matches do not establish that the actual conditions agree: compare them, the evidence, unknowns and revision warnings before using the result. Evidence excerpts retain the normal 1200-byte bound; agent reports without test/release snapshots explicitly lack success evidence. A snapshot's kind alone is not proof of success. Save supported howto changes through notes. No outcome generates success labels, human truths or calibration.

The outcome index retains at most 128 records within the search journal's existing 1 MiB bound; immutable source snapshots remain in wiki-sources. Saved/reserved outcomes consume the normal receipt capacity. The internal execution snapshot also consumes one of the 32 observation slots; its availability is checked before a new outcome reserves capacity. Definite publication rejection releases its reservation; uncertain storage errors retain recovery capacity. Unchanged retries preserve their snapshot and can recover a published outcome even after its howto changes. New writes from an old revision are refused. Update all writers sharing these records before using these operations: older writers do not preserve the new context and outcome index.

## Record what you judged

An outcome with both failure and correction immediately returns pending `procedure_reviews`, also available from ordinary maintenance as `outcome-feedback`. Each review has a short `ref`, the current procedure revision, original action/conditions/evidence, exact current text candidates and the reported proposed correction. Compare these before editing; a temporary failure or a different condition does not establish that the procedure is wrong. Record a source-backed note; its response returns updated `procedure_reviews`. Check the saved change and record `maintenance:[{ref,reason}]` with that latest ref to complete review. An edit alone does not complete review and the old ref is stale. For a checked no-change conclusion, record the reason with the original ref. Keys cover the article, related knowledge and indexed execution evidence: reviewed unchanged evidence does not recur, while a new result reopens review. If a procedure changed, read its current revision with the same receipt before patching. Additional older outcomes are counted but only two are supplied per review; inspect their sources when needed. A report whose saved report or evidence is missing or was changed is listed as unreadable: its `unknown` names the sources to recover (`<source id> (missing)`, `(changed)` or `(unreadable)`), none of its content is shown, and `maintenance` for that article is refused until they are back. Tell the person which sources to recover; do not review or edit from a report you could not read. The rest of the list is unaffected, and while other work waits such articles take at most one of its two places.

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

When the person sends a message while you are still working, the hook starts a new request and the earlier receipt stops working. Prepare once with the ids of the new notice: `carried` in the reply names the pages (`knowledgeN`) and the saved sources (`observedN`) of the interrupted work that stay valid with the new receipt, so cite them without reading the pages again, and edit the pages that work did not edit. A page that work edited is read again before another edit, as within one receipt. The result, plan and reviews are the new request's own. A request you finished hands nothing over. When you prepare the same request again because you no longer hold its receipt, `carried` names what the earlier preparation read and saved in the same way, and its result and reviews go on.

`complete:true` means the named batch and its touched reflection obligations are saved. It is not completion of every pending conversation or of the person's task. A turn change or checkpoint failure stops remaining operations; a source or article already published before the interruption is reported separately. Model calls remain outside the session commit lock. Finish the rest of the prepared work and honor reflection's `left`; do not mark sources reviewed as a shortcut. Hook completion checks continue to inspect the original planning, review and wiki records. They are not bypassed by a receipt.

Fixed limits: canonical work input 1 MiB; observation content 64 KiB of UTF-8; CLI JSON transport 8 MiB (allows escaped text); at most 4 observations, 2 notes, 128 scores, 256 memory assessments, 64 reviewed sources and 8 maintenance reviews, 4 discoveries and 2 procedure outcomes per batch. A receipt retains at most 32 observations and 128 saved or reserved recording keys within the existing 1 MiB session limit. New notes freeze the validated watched-premise hashes and exact preview revision for publication; later file changes remain visible as recheck. Published-note recovery reserves its keys before publication, so later batches cannot consume them. New entries are checked before their storage effects; identical saved retries remain available at the limit. Split work or prepare again when asked. The MCP/HTTP transport retains its existing JSON limits.

## External assignments

A host may add `assignment` to preparation and recording. Omit it for ordinary work; existing hooks and CLI/MCP callers need no host integration. These records use the existing project's `.shikii/records/assignments/` and require a binary containing this extension. They do not change older wiki article formats or decision calibration. Keep the same binary version across assignment writers.

```json
{"assignment":{"id":"case-a:run-1:research","group":"case-a","role":"researcher","stage":"research","purpose":"Check invoice ownership","question":"Which input sizes were checked?","conditions":"synthetic local inputs","evidence_scope":"plan-a"},"query":"invoice ownership"}
```

`id` identifies one immutable host execution; use a new ID when its conditions, role or implementation revision change. `group` connects related executions across sessions. Required identity, group, role and stage each fit 200 UTF-8 bytes without control characters. Purpose fits 8000 bytes; question 1500; conditions 2000; branch and optional evidence_scope 200. Optional commit is a full lowercase 40- or 64-character Git hash; code_state is provisional, integrated or reverted. Neither a host's role nor its event grants permission.

Preparation adds `teamwork`: changes from inspected article revisions, recent related coverage, unresolved questions and procedure-review evidence. Topic matches are retrieval candidates. Read conditions and sources; do not infer that a prior task succeeded or that different reports are independent. Inspect `omitted` and the returned assignment/source references when context was limited. `evidence_scope` withholds other scopes' original report text from automatic context; `shareable:true` permits a summary to cross scopes. This is context selection within a shared project, not access control over its store. The host should only share summaries appropriate for that project. Scoped preparation supplies bounded peer reports through teamwork and omits automatic raw reflection observations. Read a necessary original explicitly through its source query; this also avoids mixing additional observations whose raw source has no execution scope.

Keep the receipt and the identical assignment when extending preparation with a query, proposal, conditions or cursor. Record inspected knowledge refs and supported notes through the normal contract. An event becomes the first observation in its batch (`new1`); at most three additional observations fit alongside it.

```json
{"assignment":{"id":"case-a:run-1:research","group":"case-a","role":"researcher","stage":"research","purpose":"Check invoice ownership","question":"Which input sizes were checked?","conditions":"synthetic local inputs","evidence_scope":"plan-a"},"receipt":"<returned receipt>","reviewed":["knowledge1"],"event":{"id":"result","state":"completed","summary":"The small synthetic input preserved ownership; the large input is untested.","unknowns":["Does the large input preserve ownership?"],"method":"Compare two bounded inputs with the same owner."}}
```

An event's ID and content are immutable. Its state is observed, completed, failed, cancelled, integrated or reverted. An active/stale execution accepts observations and a completed/failed/cancelled result. Completed may become integrated, reverted or cancelled; failed may become cancelled; integrated may become reverted. After 24 hours without a checkpoint an active execution is shown as stale, which does not cancel or restart it. Commit and code_state may accompany an event to identify the actual observed revision. Summary fits 24000 bytes, method 2000, reference 1000; unknowns and contradictions each have at most 16 nonempty strings of 1000 bytes. The combined assignment/event JSON must also fit the ordinary 64 KiB observation limit; JSON escaping can expand text. Oversize content is rejected before reserving the event ID, so narrow the report and resend it. An interrupted capture cannot use an additional observation as the event source.

Use ordinary notes with exact supplied targets, sources and edits for procedure corrections; use `learning.probe` and `learning.practice` to retain a distinguishing question, observed results and reusable steps. An event is the host's report, never a verified success or human label. Do not manufacture a useful assessment from inspection.

Check complete/errors/pending. An event stays bound to its original receipt, including before capture completes. With complete:false, keep that receipt and the same event while correcting failed notes or resolving their placement; another receipt cannot finish the event. For a pending event saved by an earlier binary without receipt ownership, first resend its unchanged original recording once to establish ownership, then correct the failed notes with the same receipt. Once complete:true is cached, a retry must have identical recording content. Record additional notes separately without that completed event. An interrupted event whose source is not checkpointed must be retried before another event can advance that execution. Assignment recording is serialized per execution. Ordinary source, revision and receipt checks still apply.

Fixed assignment limits: 512 executions per project store, 8 preparation receipts per execution, 64 events and 256 KiB per execution record. These are explicit capacity errors, with no automatic deletion. Reuse a receipt for additional searches. Additional teamwork context fits 24 KiB: up to 8 coverage entries, 8 questions and 4 procedure-review candidates, with older entries omitted when needed. Existing collective and work input limits still apply. Hook session/turn identifiers and human agreement fields cannot be mixed with an assignment. No extra service, model, command-line option or chiten dependency is required.

## MCP and HTTP

### Reading MCP results once

Read the shared workflow in `shikii_wiki`'s tool description before using any
shikii tool. The initialization instructions retain this entry point, the trust
boundary, the prohibition on labeling your own judgment and the project path.
The complete workflow is supplied once rather than repeated in every tool's
description by clients that append server instructions to each tool.

For programmatic handling, prefer `structuredContent` when present, retaining
`isError` and any other relevant result metadata. Otherwise retain the original
`content`; it can contain Markdown, text, images or resource references and must
not be assumed to be JSON. When passing the result on to a model, choose one
complete view instead of displaying both representations of the same data.
Preserve all conditions, exceptions, quotes, sources, revisions, recovery fields
and relevant non-text content. A structured-only view is suitable only when it
contains all required content; preserve any additional content otherwise.

The server still returns both its complete structured result and its text view.
Changing a client's choice of view requires checking its actual model delivery
and judgment accuracy; value equality alone does not establish equal reading
quality. Keep recovery and error paths in the comparison, and measure complete
delivery rather than inferring token or latency savings from UTF-8 bytes.

MCP uses `shikii_wiki` with `operation:"prepare"` or `"record"`, and the payload above inside `work`. Pass the absolute working folder as `project` on every MCP call. HTTP uses the same operation and work object at **POST `/v1/wiki/notebook`**, with `Content-Type: application/json`, under the server's existing authentication and project boundary.

Individual `reflect query/place/submit`, `wiki capture/ingest/patch` and diagnostics remain available for deeper operations, and `hook plan query/submit` for older integrations (not beside `work prepare` for the same request: `hook plan query` replaces its receipt). Do not mix processes of different versions while using a prepared receipt: older session writers cannot preserve the new work context. Reopen agent sessions and restart the page/MCP after updating.

## Learn a reading preference from checked tasks

`work prepare` measures the UTF-8 JSON bytes of the knowledge it actually supplies, including pinned full reads and raw source recovery through receipt-bearing `prepare` queries. Its `reading` trace retains the host stage even when the first search is empty; the first nonempty article search establishes its topic, revisions and depth without discarding earlier delivery cost. Starting with a pinned full read permanently excludes that execution from teaching a default. A later different broad search or depth also excludes it. Source IDs and continuation cursors can be read through the same receipt to retain recovery cost.

After checking whether that material answered the task, use optional `work record` content:

```json
{"receipt":"<prepared receipt>","observations":[{"kind":"test","title":"Checked answer","content":"The requested condition was recovered from the cited revision."}],"reading":{"check":"Recover the requested condition without omissions.","sufficient":true,"evidence":["new1"]}}
```

Compare an explicit `reasoning:{depth:"focused"}` preparation with another depth on the same task, topic, article revisions, host stage and quality check. One favorable pair creates a candidate. Two different tasks with separate work and non-overlapping reported result content can adopt it for later matching preparations. Observation kinds, source IDs, titles, timestamps, JSON key order and unrelated host envelope fields do not establish another quality result. Structured evidence must declare its actual result in `observed`, separate from all provenance and other envelope fields. Untagged `{observed:...}` accepts text or a JSON result value; `reading-quality/v1` additionally validates its check, explicit sufficient result and textual observed payload before acceptance. Ambiguous structured evidence without this boundary is retained as a source but rejected for reading learning. All textual results, including the contents of observed, use the same parser. Malformed JSON-looking structured evidence is rejected rather than treated as plain text. Plain text remains usable. JSON numbers are compared exactly as a coefficient and exponent without floating-point rounding or expanding huge powers; equivalent spellings share an identity, while different values remain distinct. Nonzero exponents fit a fixed 128-character comparison budget. Its original envelope remains available for inspection. These are bounded agent reports, not proof of statistical independence, human truths or general performance. The next check in `attention.learning` changes with the reported results.

An omission suspends that depth under the same scope, including after a later successful report. A verified alternative can replace it. Explicit depth, unresolved work, pending saves, changed premises and exact full-source recovery retain priority. A new article revision, topic or stage does not inherit the preference. Host stage names retain the same 200 UTF-8 byte bound used by assignment preparation. The search journal keeps at most 128 reading executions within its existing 1 MiB bound; losing old supporting evidence can return the selection to the default. Counterexamples are retained before successful reports; if distinct counterexamples exceed the fixed retention budget, automatic preferences are disabled conservatively. No background experiments, model calls or calibration labels are created.

Recover an interrupted execution using only `reading:{ref:"<returned reading ref>"}` with the same active work receipt; it retains the original cost, evidence, source and time. The execution remains pending until its index is confirmed. Quality feedback is optional and does not replace the required reasoning review. Accepted pending executions survive a subsequent human turn in the same project: prepare the current turn, then recover the original `reading:{ref}` with its new receipt. The original details, source binding and capture time remain attached to the original execution; the old receipt stays invalid. Snapshot capture and index publication share one saved-response slot. A matching chiten adapter supplies the reading choice/cost, measures raw-source recovery through the same receipt, and returns the checked quality report with its ordinary observed result. If an additional delivery fails, including an aggregate output limit or a later missing source, that bundle loses reading-quality eligibility. Recovery remains available; later successful reads keep the exclusion. Prepare fresh work for a new measured comparison. This conservatively excludes attempted host reads that were not completely delivered to the child. For a raw source, chiten follows cursors on the host, returns all pages together and measures every page under the same receipt. Recovery has fixed limits of 32 pages per source and 384 KiB for the combined encoded delivery (matching the existing work-response budget), with no extra model turn or new setting. A nonprogressing cursor, an exhausted bound or an incomplete delivery fails explicitly and excludes that bundle from reading comparisons.

### Repair evidence after a new request

Unresolved report bindings remain under `reasoning.pending_results`, keyed by
original source ID and first turn. Repair with
`result:{ref:"<original source ID>",evidence:["new1"]}` and the actual missing
check in the same batch. The original narrative and quality are retained; the
recovery diagnostic pins the supplied evidence. A confirmed retry needs only
`result:{ref:"<original source ID>"}`. A changed or missing proof is refused.
Repair of an earlier task does not finish the current request. Saved procedure
feedback must also finish indexing before completion. Recent successful repair
receipts are bounded to 64; unresolved originals are never evicted to make room.

`max_bytes` bounds the final successful JSON response, including CLI/HTTP newlines
and MCP's text, complete `structuredContent` and JSON-RPC envelope. Transport
sizing happens before quote/read registration. If even the reference inventory
cannot fit, preparation fails explicitly. Advice omitted from the response cannot
supply a procedure validation or claimed savings. Hitting the procedure cost bound
selects the safe fallback, without inventing quality or retrying an invalid sample.
Unicode canonical equivalents share a comparison identity; their originals remain
unchanged. Retiring an upgrade target does not substitute for source checks and
its actual semantic maintenance.
Deleted upgrade targets with valid retained sources are reported separately as
`withdrawn`, not semantically reviewed. Missing originals remain blocked after
deletion. Frozen completion retains this distinction after restart.
Session cleanup retains accepted unresolved reports, publication/review work and
procedure feedback beyond receipt expiry. Prepare a fresh receipt to recover it;
expired receipts stay invalid. Cleanup shares the writer lock and retains records
it cannot verify. Resolved inactive sessions still expire. A ready saved report
can also finish by its source ref after a new preparation; changed work needs a
new report rather than replaying an old finish.

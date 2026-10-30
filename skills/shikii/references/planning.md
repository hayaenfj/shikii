# Prepare context before work

The agent owns interpretation: identify the current task, find relevant conversation and wiki evidence, and distinguish an unresolved user decision from a delegated task detail. This applies to writing, research, business support and software. The program retrieves records, checks quotes and scope, and measures the resulting decision. Do not ask the person to fill in a context form or add another model.

## Recover the current task

When the hook supplies `session_id` and `turn_id`, pass them as JSON on stdin to `shikii hook plan query`. Use the exact executable path supplied by the hook when available.

Read `source_material` with the active conversation. Identify:

- What the person wants **now**: an answer, investigation, comparison, change, or operation.
- Which approach and constraints have already been selected, and which request that selection belongs to.
- A concrete decision the person must resolve **before that task can begin**, if one exists.

An information question needs an answer; it does not inherently need a separate clarification. Explaining options or brainstorming can begin before a later implementation is selected. An accepted proposal does not need the same agreement again. Missing background and agent-owned implementation details are not automatically blocking user choices.

A short reply can refer to earlier work. The query includes up to four recent turns from this session, with each historical field bounded to 1200 bytes. Read their sequence rather than joining their text into a new user statement. A historical answer remains historical even when a new reply arrives before the agent finishes.

## Retrieve relevant wiki evidence

If an earlier decision or project constraint matters, choose concrete topic words from that task and query again, for example:

```json
{"session_id":"the supplied ID","turn_id":"0123456789abcdef","query":"CSV export columns and filters"}
```

The optional `query` searches this project's wiki, including lesson bodies, and returns up to two applicable lessons with their revision and source IDs. Refine the topic if the results do not apply; do not treat delivery as relevance or usefulness. This lookup is independent of the hook's one-hour suppression of repeated memory quotations.

Choose short topic terms and plausible alternative wording rather than copying the whole request into keyword search. For example, a receipt question can concern a refund policy, while a personal-name correction can concern publication rules. If the returned topic is wrong, refine the query before citing it. For comparing more candidates and reusing retrieved evidence, follow [wiki.md](wiki.md#search-and-reuse-evidence).

Use the knowledge to reason: identify what the project is trying to achieve, why a past choice was made, and which conditions or exceptions matter now. Separate the person's explicit rules from an agent's working hypotheses. If the current evidence differs, explain the difference and inspect the sources; do not turn a past choice into a universal rule or treat a link as permission. When writing back, preserve supported reasons and exceptions and connect the procedure to its criterion through `related`. Do not invent a rationale just to fill these fields.

When the work moves to another area mid-task (another part of the code, a release, a document), search the wiki again with that area's topic words (`shikii wiki query <topic>`) before acting there: the lessons delivered before work were chosen by the first request's words.

Read each lesson's applicability and kind. Planning returns complete summaries and conditions, and follows the selected pages' direct `related` links to at most two live `criteria` pages. `wiki_context.related` identifies the originating page; a link is a reason to inspect a criterion, not proof that it applies. When `pages` is omitted, up to two complete bodies are supplied as `article:<id>:<revision>`, criteria first, within 8000 body bytes in total. Read these bodies now instead of fetching the same revision again. A body that does not fit is omitted whole; use `shikii wiki query lesson:<page_id>` for missing bodies and related pages. If its meaning or origin is unclear, use `shikii wiki query <source_id>` to inspect the original exchange. Prefer current instructions over historical agreements; a wiki lesson is a hypothesis, never current approval. If no usable evidence exists, keep the missing context unknown.

When a needed body is not already supplied, add `pages: ["<page_id>"]` to planning query (up to two). It returns the selected full text as `article:<id>:<revision>` in `source_material`; cite an exact quote and pass the same `pages` and search text during submit. Follow related pages selectively instead of loading the entire wiki. The quote still needs current human evidence to support agreement. A changed cited revision during the decision is rejected; re-read the source rather than carrying its old meaning forward.

The query reply returns `available_count` instead of repeating every available revision. Recorded planning decisions still retain the full inventory in `knowledge_context`: ranked candidate revisions, linked criteria, supplied article bodies, actual wiki quotations, and the inventory of available article revisions at preparation time. The last successful query in the same turn pins that material for a submit with the same search text and `pages`; unrelated later edits cannot become body reads. A direct submit without a matching query records no body reads. Quoted related criteria must still be live when the decision is saved. Search time and UTF-8 context bytes measure retrieval cost, not model tokens or usefulness. A returned or opened article is not automatically a used citation. The knowledge view counts these separately from the existing evidence-based memory assessments.

For a comparison with and without memory, freeze the task inputs and this available-revision inventory. Retrieve an old article with `shikii wiki query lesson:<page_id>@<revision>` and follow only revisions in that inventory. Do not substitute current pages, later sources, or newly resolved outcomes into an earlier judgment. Keep synthetic expectations, agent assessments and observable/human outcomes separate. Compare information questions, already-approved work and genuinely blocking choices separately; retrieval and quotation counts alone do not establish fewer unnecessary questions or higher accuracy.

## Submit grounded interpretations

Submit is not optional: when the hook asked for this planning and the turn changed files, the Stop hook stops the turn once until `hook plan submit` was called for the turn (agreed and open may be `unknown:true` when the conversation gives no evidence). The miss is recorded either way. The Stop hook also stops once when the person's message is short (under 40 characters) and not a bare acknowledgement and nothing was submitted, even if no file changed: it then asks only for `continues_previous`, and the minimal submit `{"session_id":…,"turn_id":…,"agreed":{"unknown":true},"open":{"unknown":true},"continues_previous":false}` (or `true`) is enough. The turn is judged at the Stop after that, with the grouping you gave.

The reply of `hook plan query` carries `how_to_submit`, the exact shape of submit (the hook text itself names only the command and this turn's ids, 0.11). Pass the same IDs, the wiki query used for any cited lesson, and `agreed` / `open` to `shikii hook plan submit`:

```json
{
  "session_id":"the supplied ID",
  "turn_id":"0123456789abcdef",
  "query":"CSV export columns and filters",
  "agreed":{
    "summary":"The current task is to implement the selected export while preserving its filters.",
    "evidence":[{"source":"current_user","quote":"an exact short quote from the returned text"}]
  },
  "open":{"unknown":true}
}
```

Set `continues_previous` to `true` when this message only continues or answers the previous request (an acknowledgement or a go-ahead that asks for nothing new); a message that asks for any work, even a short addition to the same work ("add a check too", "write it in the README too"), or repeats an earlier request is `false` when it is a new request, however short; leave it out when unsure. You know the conversation and its language, so read the message in its context: "go on", "OK, let's do that" or a thumbs-up continue the previous request, while "merge main" is a new one. The hook first decides with a fixed list of acknowledgements and corrects it with your answer for this turn only: a continuation is attached to the previous request and a new request is judged on its own. Either way the previous outcome is settled from the person's message by fixed rules, not from your answer. The answer only decides how messages are grouped; it is not a truth label. It is ignored once this turn's Stop has judged the request.

When the message restates an earlier request of this session (not only the previous one), set `restates` to that request's turn id: the `<id>` of a `history:<id>:user` key that query returned, or the id of any earlier request of this session you know (there is no limit on how far back; query still shows only the recent turns). shikii looks the request up in its own record of this session and project and compares the message with that request's own text by the same fixed rule (very similar: that request was not done; otherwise nothing), so naming a turn can neither make nor erase a truth; an unknown id, a turn of another session or project, or a request that already has a truth is ignored and the usual comparison with the previous request applies. Leave it out when the message restates nothing.

Write summaries in the language that makes the task and its constraints clear. Preserve original quotations without translating them. Each summary is at most 600 bytes, with one to three quotes of at most 240 bytes each. The input JSON is at most 16 KB. Wiki search text is at most 1500 bytes.

Use the exact source keys returned by query:

- `current_user`, `previous_user`, `previous_assistant`: the immediate exchange.
- `history:...`: a specific older field from this session, supplied as reference.
- `lesson:<id>:<revision>`: a wiki hypothesis with its applicability and provenance.
- `article:<id>:<revision>`: the selected article body with source references, also a hypothesis.

Agreement needs evidence from `current_user` or `previous_user`; wiki and history alone cannot establish current agreement. For a short acceptance, also cite the proposal or prior constraint it refers to. Latest withdrawals and changed conditions override earlier agreement. Do not broaden acceptance to a different request.

For `open`, describe only a concrete unresolved choice that blocks the **current** task. When nothing blocks it (the task can start as specified), or the evidence is insufficient, use only `{"unknown":true}`; do not write a reason why nothing blocks (0.11: a filled `open` is read as "the person has something to decide first"). Do not put the question being answered, a possible later design decision, or an unspecified helper name in that category, and do not invent a summary or quote. shikii reads `open` as your own answer to needs-plan (0.11): a filled `open` is "yes, the person has something to decide first", `{"unknown":true}` is "no". When it matches the decision model's answer, the person is not asked and the agreement is kept as a machine truth (source `agreed`, never the person's); when it differs, the person is asked and sees both answers.

## Read the result

- `recorded:true` means a decision record was saved. AI interpretations and regression expectations are not human labels or calibration data.
- `shadow:true` means there is no valid calibration line. Use no raw score to recommend confirmation; `advice` stays empty.
- Read a nonempty `advice` as calibrated guidance, within the current instructions and existing execution permissions. Evidence, an empty advice, and a no prediction do not grant permission.
- If both facts are unknown, no model is called and `recorded:false` is returned. Missing internal context alone is not a reason to give the person another form.
- Stale or closed turns are rejected. Re-query when the wiki revision changes or a newer request arrives. Repeating the same submission preserves the first prediction rather than appending duplicates.

Existing project questions are retained. Do not reinterpret their old labels for a new question. Measure information questions, approved work, and genuinely blocking choices separately; an always-no predictor is not an improvement merely because most examples are no.

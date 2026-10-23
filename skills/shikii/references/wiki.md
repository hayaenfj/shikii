# Build knowledge from conversations

Apply Karpathy's [LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f): retain sources, integrate them into connected knowledge, and revisit it through queries and maintenance. The active agent edits the knowledge; shikii checks provenance and stores revisions. Do not start another model or ask the person to organize it.

## Read and reconcile before writing

The hook sends a reflection reminder after two pending conversations, or on a request after one has waited a day. Prioritize the current task; before finishing, process up to two conversations through [reflection](review.md). Individual operations support deeper editing.

1. Read each original exchange. If truncated or ambiguous, use `shikii wiki query <source_id>` or MCP `shikii_wiki` with `operation: "query", sources: [source_id]`, in the current project's directory. Use the executable supplied by the hook. Distinguish the original request, the agent's proposal or reported result, and the following human reply. A proposal is not a decision, and a historical implementation status is not the current state.
2. Search the topic with `shikii wiki query "topic words"`. Read matching pages and their sources. Follow related pages with `shikii wiki query lesson:<page_id>`, including superseded pages. Source IDs and page IDs identify different objects.
3. Integrate the new evidence into an existing topic when possible. Retain useful earlier reasoning and limits. Separate chosen policy, observed behavior, interpretation and unknowns; attribute important statements to source IDs in the body. Do not invent rationales or permanent preferences to fill a template.
4. Keep `lesson` concise for retrieval; write `body` as reusable knowledge. Include background, supported decisions or findings, reasons when known, applicability, exceptions and uncertainty where the evidence supports them. Use paragraphs and optional Markdown headings. A narrow fact need not fill every heading; a one-line instruction is insufficient when its reasoning or limits matter. Do not copy a whole conversation or pad an article.
5. Connect relevant pages with `related`. Reserve `replaces` for supersession. For contradictions or changed policy, explain what changed, which source supports it and what remains unresolved. Update other affected pages when their claims also change; do not silently declare one account correct. Keep local observations within this project.
6. Re-read the saved page: could another agent distinguish the decision, support, applicability and unknowns? Run `wiki lint` after structural edits. Passing provenance checks or tests does not establish semantic quality or improved decision accuracy.

For greetings or follow-up requests with no reusable finding, submit `sources` only. Silence and thanks are not success truths. For explicit corrections, attach `correction: {source_id, quote, action}`: the exact human reply and the next action to change. See [correction reflection](review.md).

## Search and reuse evidence

Keyword search matches text; it does not establish semantic relevance. Choose topic words from the current task and consider common alternative terms. If a query returns nothing or an unrelated topic, try the relevant policy or subject terms instead of treating that result as proof that knowledge is absent. `wiki query` searches all stored articles and returns `search.total` and lightweight `search.candidates` with titles, applicability, revisions and matching fields/terms. Compare these before changing the query; open a lower-ranked candidate with `wiki query lesson:<id>`. `search.total` counts matching valid articles, not all potentially relevant knowledge. Automatic recall stays bounded separately.

Large search, article-list and source responses include `next_cursor`. Repeat the **same** query or source IDs with `continue:CURSOR` appended to the CLI arguments, or with `cursor: "..."` in MCP/HTTP. Continue until the needed evidence is found or no cursor remains; a page ending is not a search ending. No candidate, source-lookup or stored-article count cuts off exploration. Responses are divided by JSON data size; a single large source is returned intact on its own. A cursor binds the project, request and result snapshot. If results change or a cursor is rejected, start a fresh lookup and reconcile revisions instead of combining mismatched pages. Automatic recall and reflection work batches remain small independently of manual exploration.

Check shared rules as well as the task's subject: an event article can supply the schedule while a publication article governs the draft's headings or privacy. Follow relevant connections or search that shared topic before finalizing the deliverable; finding the subject article alone does not establish complete coverage.

Within the active task, retain the actual text with its project, article ID/revision and source IDs/hashes. When a returned article includes its full `body`, do not fetch that identical article again just to read its body. Candidates without a body can be opened directly. Read original sources only when they are missing from context or their meaning or provenance needs checking. Fetch missing sources together: `shikii wiki query <source_id> <source_id> ...`, or MCP query with `sources: ["...", "..."]`. Reuse this evidence across related cases in the same task instead of repeating topic, article and source lookups for every answer.

Start a new task or changed topic with a current topic lookup. Re-read changed article revisions, new citations and any evidence no longer available after context loss; an ID alone is not the source text. Writes or a stale-revision rejection require a fresh lookup. Keep the actual cited revision for historical comparisons. This reuse rule does not skip the program's provenance checks or turn an earlier agreement into current permission.

## Ingestion contract

MCP tool: `shikii_wiki`, operations `query` / `ingest` / `lint`. CLI `shikii wiki ingest` accepts the same JSON on stdin without `operation`. Use the CLI if MCP is unavailable; do not ask the person to run it.

- `sources`: observed source IDs from this project, at least one. Read each source you use. Existing citations are retained; references are bounded by article size, not a fixed count.
- `title`, `lesson`, `applies_to`: topic, short retrieval summary, applicable conditions; limits are 200 / 2000 / 1000 UTF-8 bytes.
- `body`: edited knowledge, up to 16 KiB of UTF-8. Explicit search includes it; automatic quotations still use the summary. The UI displays headings and paragraphs as text without executing embedded HTML.
- `related`: other page IDs in this project, each with valid sources. Links can point both ways.
- `id`: the existing page to update. Original sources and earlier revisions remain available.
- `replaces`: older pages superseded by this one; no supersession cycles.

Omitting `body`, `related` or `replaces` on update preserves it. Explicit `body: ""`, `related: []` or `replaces: []` clears it. A summary update does not refresh a preserved body: check they still agree. Old pages without these fields remain readable without migration.

The same draft fields work in `shikii reflect submit`'s `wiki` array. Use individual ingestion calls for deeper cross-topic editing while reflection handles pending observations. Save the intended editorial work before marking a source reviewed.

## Place a new lesson before saving it

`place` takes one or two drafts per call; pass a longer list in several calls.

Before submitting a draft that has a `lesson`, pass it to reflection's `place` operation (`shikii_reflect` with `operation: "place"` and the draft in `wiki`, or `shikii reflect place` on stdin). Shikii compares the draft with every live page using the decision model (Jev when a key is set, otherwise the local model) and returns `placements`, one per draft, each with `candidates` ordered by `same_topic.p`, each with `page_id`, `revision`, `title`, `applies_to` and, for the top three, a `conflict` judgement. Until a line is drawn these are hints, never decisions: read the candidate bodies, then set `id` to merge into the same topic, `replaces` when the new lesson supersedes conflicting guidance, or neither for a genuinely new topic. A flagged conflict means read both and decide which rests on the newer evidence; do not keep both silently. What you then save becomes the outcome truth for those judgements (merged or replaced → same topic; replaced → conflict), so no person labels them. With no live pages the result just says a new page will be created.

## Preserve observed evidence

Use `shikii wiki capture` with the observation object itself on stdin (the CLI does not take the `observation:` wrapper; the error message of a wrong shape mentions `ingest`), or `shikii_wiki` with `operation: "capture", observation: {...}`, to retain a source you actually inspected. Fields are `kind` (`human`, `agent`, `code`, `test`, or `release`), `title`, `content`, `reference`, `version`, and `observed_at` (RFC3339). Include the relevant excerpt or result, an exact file/line, command or release reference, and the commit/artifact version or observation context. Content is at most 32 KiB and the encoded object at most 64 KiB. Keep secrets and private material local. Capture does not fetch a URL, execute a command or establish the correctness of an agent's report.

The returned ID can be used in `sources` during ordinary ingestion. Repeating the same observation is idempotent; a changed result or version creates another immutable source. Unprocessed captures appear in reflection's `observations` within its two-source budget. Read and integrate them into existing topics, or mark them reviewed with a source-only draft. The pending queue has no fixed count ceiling; integrate useful observations during normal reflection.

`wiki query <source_id>` returns conversations in `source_material`, captured evidence in `observations`, and available long conversation text in `originals`. Original text includes its record ID and truncation/unavailability status. Ingestion archives available long text with a separate citation hash, preserving it even if the decision record is later removed. Missing historical text cannot be reconstructed from a summary; keep that limit explicit. Separate a person's chosen policy, the agent's proposal, and a version-specific observed result in the article. Neither a `human` kind nor historical evidence supplies current permission or a truth label.

## Retrieve and maintain

`wiki query` without text and `reflect query` include up to two `maintenance` tasks. They identify the page revision, connected revisions, and a `key` for that exact neighborhood. Work on these small tasks during normal reflection: read the article and sources, search nearby topics for duplicates, reconcile stale or conflicting claims, and check both incoming and outgoing links. `summary-only` and `unlinked` are prompts for inspection, not errors requiring padded content or invented relationships. Unresolved conflicts stay attributed and unresolved.

For a semantic edit, include a concise `change_reason` (at most 1000 bytes) in the ingestion draft; it is retained with the article revision. Then re-query maintenance because an edit changes its key. A review that finds no supported edit is useful too. Save it with `shikii wiki maintain` on stdin, `shikii_wiki` operation `maintain` with a `maintenance` object, or reflection's `maintenance` array (up to two): `{"page_id":"…","revision":"…","key":"…","reason":"What was checked against which sources, what changed or why no change is supported"}`. This records the review, not semantic correctness. Changed page or connected revisions invalidate the checkpoint; unchanged neighborhoods return after 30 days. Stale keys are rejected. No new user labeling or model is needed.

Lessons are hypotheses. Current requests and rules take priority; wiki entries never supply current permission, human truth labels, calibration or `confirmed` status.

Automatic recall selects up to two lessons by title and applicability within 1800 bytes after JSON escaping. It suppresses a revision for one hour in a session; updates arrive immediately. When no lesson applies, at most one similar unprocessed conversation is returned. Query the full article when needed; do not enlarge automatic quotations into transcript dumps.

Assess a delivered quotation's effect on an actual decision through [reflection](review.md)'s `memory` field. Delivery, article length, passing tests and task success alone do not demonstrate usefulness. Retain the delivered revision instead of substituting a later article.

Immutable sources live under `.shikii/records/wiki-sources/`; generated pages, index, log and revisions under `.shikii/wiki/lessons/`. Use the tools instead of editing generated files. Missing or changed sources exclude affected pages from retrieval. `lint` checks provenance, revisions, supersession and related links and repairs only the generated index; it also runs at most daily. Semantic maintenance belongs to the agent: inspect contradictions, stale claims, disconnected topics and missing explanations against source evidence, then revise through ingestion.

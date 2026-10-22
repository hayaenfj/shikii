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

## Ingestion contract

MCP tool: `shikii_wiki`, operations `query` / `ingest` / `lint`. CLI `shikii wiki ingest` accepts the same JSON on stdin without `operation`. Use the CLI if MCP is unavailable; do not ask the person to run it.

- `sources`: 1–8 observed source IDs from this project. Read each source you use. Existing citations are retained, up to 16 total.
- `title`, `lesson`, `applies_to`: topic, short retrieval summary, applicable conditions; limits are 200 / 2000 / 1000 UTF-8 bytes.
- `body`: edited knowledge, up to 16 KiB of UTF-8. Explicit search includes it; automatic quotations still use the summary. The UI displays headings and paragraphs as text without executing embedded HTML.
- `related`: up to 8 other page IDs in this project, each with valid sources. Links can point both ways.
- `id`: the existing page to update. Original sources and earlier revisions remain available.
- `replaces`: up to 4 older pages superseded by this one; no supersession cycles.

Omitting `body`, `related` or `replaces` on update preserves it. Explicit `body: ""`, `related: []` or `replaces: []` clears it. A summary update does not refresh a preserved body: check they still agree. Old pages without these fields remain readable without migration.

The same draft fields work in `shikii reflect submit`'s `wiki` array. Use individual ingestion calls for deeper cross-topic editing while reflection handles pending observations. Save the intended editorial work before marking a source reviewed.

## Preserve observed evidence

Use `shikii wiki capture` with JSON on stdin, or `shikii_wiki` with `operation: "capture", observation: {...}`, to retain a source you actually inspected. Fields are `kind` (`human`, `agent`, `code`, `test`, or `release`), `title`, `content`, `reference`, `version`, and `observed_at` (RFC3339). Include the relevant excerpt or result, an exact file/line, command or release reference, and the commit/artifact version or observation context. Content is at most 32 KiB and the encoded object at most 64 KiB. Keep secrets and private material local. Capture does not fetch a URL, execute a command or establish the correctness of an agent's report.

The returned ID can be used in `sources` during ordinary ingestion. Repeating the same observation is idempotent; a changed result or version creates another immutable source. Unprocessed captures appear in reflection's `observations` within its two-source budget. Read and integrate them into existing topics, or mark them reviewed with a source-only draft. At most 128 unprocessed captures may wait; finish useful integration before capturing more.

`wiki query <source_id>` returns conversations in `source_material`, captured evidence in `observations`, and available long conversation text in `originals`. Original text includes its record ID and truncation/unavailability status. Ingestion archives available long text with a separate citation hash, preserving it even if the decision record is later removed. Missing historical text cannot be reconstructed from a summary; keep that limit explicit. Separate a person's chosen policy, the agent's proposal, and a version-specific observed result in the article. Neither a `human` kind nor historical evidence supplies current permission or a truth label.

## Retrieve and maintain

Lessons are hypotheses. Current requests and rules take priority; wiki entries never supply current permission, human truth labels, calibration or `confirmed` status.

Automatic recall selects up to two lessons by title and applicability within 1800 bytes after JSON escaping. It suppresses a revision for one hour in a session; updates arrive immediately. When no lesson applies, at most one similar unprocessed conversation is returned. Query the full article when needed; do not enlarge automatic quotations into transcript dumps.

Assess a delivered quotation's effect on an actual decision through [reflection](review.md)'s `memory` field. Delivery, article length, passing tests and task success alone do not demonstrate usefulness. Retain the delivered revision instead of substituting a later article.

Immutable sources live under `.shikii/records/wiki-sources/`; generated pages, index, log and revisions under `.shikii/wiki/lessons/`. Use the tools instead of editing generated files. Missing or changed sources exclude affected pages from retrieval. `lint` checks provenance, revisions, supersession and related links and repairs only the generated index; it also runs at most daily. Semantic maintenance belongs to the agent: inspect contradictions, stale claims, disconnected topics and missing explanations against source evidence, then revise through ingestion.

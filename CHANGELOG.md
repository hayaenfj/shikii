# Changelog

**English** · [日本語](CHANGELOG_ja.md)

## 0.5.0 — Evidence that reaches decisions

- Preserve command names and identifiers in wiki search, prioritize titles and applicability, and explain matching fields and terms. Keep automatic recall concise.
- Capture inspected human or agent text, code, test results and release observations with their type, version, time and reference. Preserve available long conversation originals and their citation hashes independently of decision-record retention.
- Bring up to two article reviews into normal reflection. Track connected revisions, reject stale reviews, retain editorial reasons, and revisit unchanged neighborhoods after 30 days.
- Let planning select article bodies and cite their exact revisions. Retain ranked candidates, read articles, quotations, available revisions and retrieval costs. Reject cited sources that change during a decision and make historical revisions available for comparisons without future knowledge.
- Show retrieval, quotation and existing memory assessments separately, together with source evidence and upcoming article reviews. Refresh the English skill, English/Japanese documentation and architecture diagrams.

## 0.4.4 — Connected knowledge and agent guidance

- Include full knowledge bodies, related pages, and English agent skills for context preparation and wiki editing, alongside clearer review and calibration displays.
- Verify that updating from 0.4.3 installs the embedded skills and refreshes existing hooks while preserving project questions, calibration and answers. New projects receive current built-in prompts. Start a new agent session and restart running UI or MCP processes after updating.
- Align CLI, MCP and plugin versions at 0.4.4 so installations already reporting 0.4.3 can receive these changes through `shikii update`.

## 0.4.3 — Clearer reviews and less repeated loading

- Separate concise retrieval summaries from knowledge bodies and related pages. Search full articles, follow exact page references, and keep earlier sources, revisions and legacy pages readable. Update the agent workflow to reconcile evidence into existing topics and show article details and related navigation in the UI.

- Move task-context interpretation into the English agent skill; make recent conversation and explicitly searched wiki evidence available to planning with scoped, revision-checked citations.

- Prepare approach decisions from the current request, agreement evidence, and open questions; reject unsupported quotes and stale submissions without treating AI interpretations as human truths.

- Distinguish insufficient truths from prediction quality when calibration is refused; show a concise calibration summary and keep diagnostic logs collapsed.

- Clarifies the target of before-work and after-work cards. Shows the current request first and folds the AI reply from before that request into conversation context. For yes/no decisions, the displayed prediction, its probability and the matching answer button now use the more likely answer, including after a retry.
- Redesigns the UI with navigation grouped by before-work and after-work decisions, counts, a prominent question, distinct conversation blocks and larger answer buttons. Adds responsive layouts, light and dark colors, and clearer project cards. Opens a pending decision on first use while preserving an existing selection, and prevents navigation keystrokes from submitting an answer.
- Fixes evidence details in the knowledge view by using consistent JSON field names. Missing evidence fields no longer appear as `undefined`.
- Limits original-text loading to the records returned by the queue API, preserving priority order and excluding superseded decisions.
- Refreshes only the visible queue or answered records. Loads wording details and knowledge when opened, keeps counts and project filters available, and ignores late responses after navigation. Removes unused UI styles.
- Removes unused declarations and moves test-only helpers into test files while preserving hook behavior and test coverage.

## 0.4.2 — Update to the latest release with one command

- `shikii update` checks GitHub Releases for the latest stable version and downloads the package for macOS or Linux on arm64 or amd64. Go, curl and the GitHub CLI are not required.
- Verifies SHA256SUMS and the asset hashes, then updates shikii and readout beside the running executable. Leaves the installation unchanged if the current version is the same or newer.
- Checks the package contents and executable version before replacing files, and restores the existing executables if replacement fails. Preserves conversations, memories, human answers and calibration, and refreshes existing hooks and agent instructions. Restart a running UI or MCP server to use the new version.
- Makes `CHANGELOG.md` the English changelog and keeps the Japanese history in `CHANGELOG_ja.md`, with links between them.

## 0.4.1 — Connect corrections to the next action

- Changes the license for shikii itself to `Apache-2.0 OR MIT`, letting users choose either license. Terms for third-party code, dependencies, models and services remain unchanged.
- Lets lessons retain the exact wording of an explicit correction and the action to change next, with a source reference. Delivers them within the existing short quotation for a similar request.
- Records whether an agent followed a correction, repeated the issue, found it inapplicable or could not determine the result, together with the decision and supporting evidence. Keeps this separate from memory usefulness, human answers and calibration.
- Shows delivered corrections, their outcomes, pending assessments and examples in the knowledge view. Preserves existing memories and assessments without adding settings, models or human input.
- Updates the English and Japanese READMEs and architecture diagrams to match current use. Documents required tools, Homebrew installation and the default Kev-4B Q4_K_M model. Preserves the original hero image and places the conversation diagram in the body.
- Aligns the CLI, MCP server and plugin versions at 0.4.1.

## 0.4.0 — Deliver concise memories and batch reflection

- Quotes up to two lessons whose titles and applicability match the request, within 1,800 bytes after JSON escaping. Suppresses the same revision for one hour in a session while delivering updated revisions immediately.
- Falls back to at most one similar, unprocessed conversation when no lesson matches. Does not repeat source material already processed. Manual wiki search can still search lesson bodies.
- Sends a short reflection reminder when two conversations are pending, or on a request after one conversation has waited a day. Avoids repeating source material and assessment targets in separate contexts.
- Adds `shikii reflect query / submit`, MCP `shikii_reflect` and HTTP `POST /v1/reflect` to inspect and save assessments and wiki entries together, starting with the oldest pending conversations.
- Returns errors per item when part of a submission fails, retaining successful items for safe retries. Existing individual operations remain available. Keeps human answers and calibration separate.
- Aligns the CLI, MCP server and plugin versions at 0.4.0. Keeps existing data usable without deleting it.

### Track how memories contribute to decisions

- Saves the quotation and revision actually delivered for each request, so later reflection uses the content seen at the time.
- Uses the normal `reflect query / submit` flow to assess each memory as helpful, unused, harmful or unknown, tied to a decision and supporting evidence.
- Shows deliveries, the four outcomes, pending assessments and recent decisions with evidence in the knowledge view. Does not count task success alone as a memory effect or infer effects for past deliveries.
- Keeps these as provisional AI assessments. Does not automatically change search ranking, human answers or calibration, or add settings, models or human input.

### Initialize only the projects you choose

- `shikii --init` (also `shikii init`) enables automatic recording for that project. Prepares the four built-in questions, wiki, MCP integration, agent instructions and UI entry in one step.
- Writes the completion marker last. Hooks create nothing in uninitialized or failed projects, and reject markers or links belonging elsewhere.
- Resolves initialization from subdirectories to the same project root and preserves settings and conversations on repeated initialization. Keeps initialization messages concise and in Japanese.

### Let AI assess routine work and humans spot-check

The regular agent provisionally assesses whether work stayed within the requested scope and whether it completed the request, using conversation and execution evidence. Humans review random samples and cases the agent cannot assess.

- After the next human reply, hooks combine assessment and wiki preparation and send a short reminder when needed. `shikii review query / submit` and MCP `shikii_review` support inspection and submission.
- Uses `unknown` when evidence is insufficient and leaves the case for human review. Additional requests, greetings and silence alone do not establish success or failure.
- Samples one in five assessments. After 20 consecutive agreements in random checks for the same assessment type, reduces sampling to one in twenty; a disagreement restores the previous rate. Tracks uncertain cases separately from random samples.
- Shows humans the original conversation without highlighting the AI's answer. Returns human corrections as examples for subsequent assessors.
- Stores provisional assessments, evidence and source material under `records/reviews/`. Does not rewrite the original AI assessment after seeing a human answer or count it as a truth label or calibration sample.

### Read long replies through to the end

Separates bounded decision inputs from the original text shown in the UI. Stores long replies separately so pending and completed views, and assessment `query` results, can show the full ending. Adding original text does not change the original decision, hash, human answer or calibration. The UI identifies records affected by earlier storage limits or the original-text storage limit.

### Separate conversations and trust history by project

Stores conversations, lessons, provisional assessments, human answers and calibration in each project's `.shikii/`, with hook preparation. The shared UI lists projects and reads or updates only the selected project's records. CLI and MCP use the same location. Does not import old shared conversations or calibration.

### Fix project-isolation audit findings

- Keeps reports separate when calibration, tuning or adoption run concurrently in different projects, without changing process-wide output streams.
- Uses the same project root for init, new and tune from subdirectories. Stops processing when the storage location is rejected.
- Rejects symlinks in intermediate directories and files for records, human answers, conversations, provisional assessments and original text.
- Preserves decision and record selections when redirecting a UI URL to add a trailing slash.
- Does not leave an empty spec at its final filename when initialization fails to write it.

## 0.3.0 — Grow a wiki from everyday conversations

The regular agent turns requests, AI replies and subsequent human messages into lessons with applicability conditions. shikii manages sources, the index, earlier revisions and history, then quotes lessons for similar requests. No wiki-specific UI work or additional resident model is required.

- **Ingest:** hooks provide up to two unprocessed conversations for the agent to organize through MCP `shikii_wiki` or `shikii wiki ingest`. Conversations without a reusable lesson, such as an additional request, can be archived as processed while retaining their source text.
- **Search:** `query` returns lessons, unprocessed conversations and original sources from the same project. Short search terms can find long lessons. The knowledge view also shows lessons and sources.
- **Update:** updating a lesson preserves its sources and previous revisions. Replacing an old lesson makes references use the new one. Replacement can be undone, and reverting content still leaves a history entry.
- **Check:** `lint` verifies source hashes, generated pages, revisions and links, and repairs the generated index. Runs once a day at the end of a turn as well.
- **Distinguish certainty:** all lessons are hypotheses with sources. Excludes lessons whose sources are missing or changed, and does not count AI interpretations as human answers or calibration samples.
- **Check versions:** aligns `shikii version`, MCP and the plugin at `0.3.0`.

The workflow of retaining sources, having AI edit a wiki, and running ingestion, search and periodic checks draws on [Andrej Karpathy's LLM Wiki idea](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f). Combined with shikii's claims, evidence and retesting, it distinguishes conversational interpretation from verified results.

Existing users can update the executables and agent instructions to use this through the existing two hooks. New MCP tools appear when the server next starts. Until then, agents can use the CLI commands supplied by the hooks.

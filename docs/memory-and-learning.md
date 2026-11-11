**English** · [日本語](memory-and-learning.ja.md)

# How shikii relates to memory

## In short

Remembering what you said before is something shikii shares with the memory in ChatGPT and Codex. Beyond that, shikii does three things.

1. **It brings what it remembers to the agent when it is needed.** Before a request starts, or right before a file is changed, it hands the agent the knowledge that applies.
2. **It reviews whether that knowledge helped, and corrects it.** Knowledge that does not help stops being delivered, and knowledge that is wrong is corrected after checking its sources.
3. **It decides from real results how much to leave to the AI.** It compares the AI's decisions with your answers and with results that could be checked, and separates the cases the AI may decide alone from the cases it should ask you about.

The aim is to spare you repeating the same explanations and corrections, and to grow a way of working that fits your project.

## Example: you say "update the Japanese and English manuals together"

Memory keeps these words and uses them as a reference in later conversations. It cannot tell whether both manuals were actually updated.

shikii handles it like this:

- It saves the rule as an article, with when to use it and its source (what you said and when).
- Before work on the manuals, it delivers that article to the agent.
- After the work, the agent reviews and records whether the article helped.
- What a machine can check, such as "were both files changed", is checked at the end of the work by a check you enabled. If only one manual was updated, the agent cannot finish.

## Side by side

| | What memory does | What shikii adds |
| --- | --- | --- |
| Remember | Keeps the context of earlier conversations, preferences and project conventions | Keeps it as articles, with when to use each and its source |
| Use | Uses it as a reference in later conversations and work | Delivers the relevant articles before a request and right before a file is changed |
| Enforce | Instructions that must always apply are to be written in `AGENTS.md` and similar files | Checks rules a machine can verify at the end of the work |
| Correct | Lets you see and delete what it remembers | Stops delivering or corrects articles, based on reviews of whether they helped |
| Delegate | Answers with the remembered context in mind | Compares the AI's decisions with real answers to decide what the AI may decide alone |
| Share | Each app has its own storage and settings | Claude Code, Codex and OpenCode connected to the same project use the same knowledge |

## The two loops inside shikii

### 1. Use knowledge, and maintain it

1. **Keep.** The goals you stated, the approaches you chose, your corrections and the results that were checked are saved in the project's `.shikii/`. Each article says when it applies and where it came from, so its reasons can be checked later.
2. **Deliver.** At the start of work, and before related operations, the agent receives the articles it needs.
3. **Review.** The agent records how the delivered articles affected what it actually did. "It was delivered" or "the work succeeded" alone does not count as helping.
4. **Correct.** An article that keeps going unused in the same situation stops being delivered there. Mistakes and outdated parts are corrected after checking the sources. You can also edit and delete articles on the page.

Your past answers, shown before similar requests, are handled the same way. An answer that went unused two or more times and never helped is no longer shown, and it appears under "Past answers the AI judged not useful" in the knowledge section of the page. You can undo that decision with "Undo" on the page.

On top of this loop, shikii has added the following over time:

- What the agent learns in the middle of the work can be added to the record of that same work.
- A procedure used before is read together with its conditions, failures and fixes at that time, and compared with this time's conditions. An earlier success is not taken as this time's success.
- When knowledge did not arrive, the agent can find out why from the records of that time (it did not match the search, it had already been delivered, and so on), correct the article and try the correction on the past situations.
- When several agents work together, one agent's finding reaches the others, is checked under other conditions and comes back into the same knowledge. When the work passes from research to implementation to review, the conditions checked and the open questions are handed on.
- How much to read, and how to go about an investigation, are chosen again from real results. A new way is adopted only after it is confirmed on two separate tasks, and the earlier way comes back if things get worse.

### 2. Measure decisions, and change what is delegated

- For decisions that repeat, such as "is this request done", shikii compares the probability the AI gives with your answers and with real results.
- Once enough records exist and the AI's confidence matches how often it is right, shikii draws a line: above it, the AI may go ahead alone. Until such a line can be drawn, nothing proceeds automatically.

What grows is the project's knowledge and its standards for decisions. The AI model itself is not retrained.

## What shikii does not claim

- A review is the agent's assessment. It is kept apart from your answers and from any measurement that performance improved.
- Reading a lot of knowledge is not evidence that the work got better.
- Checks cover only what a machine can verify. "Were both files changed" can be checked; whether the text is easy to understand cannot.
- Knowledge and the AI's decisions never grant permission for an operation. The current request and the agent's permission settings come first.
- Changes that make shikii faster or lighter reduce how much knowledge it hands over and how often it reads. They do not mean the model's overall response time or answer quality improved.

## About OpenAI's memory (what was compared)

In OpenAI's public description, memory carries the context of earlier conversations into later work, not only preferences but also repeated tasks and project conventions. ChatGPT's memory and Codex's local memory have separate storage and settings, and Codex's local memory includes summaries and sources based on earlier conversations. So "OpenAI remembers only preferences" and "only shikii stores things locally" are not accurate distinctions.

OpenAI also advises keeping team instructions that must always apply in `AGENTS.md` and similar files, rather than relying on memory alone.

This comparison is with the public description of the memory feature as checked on November 10, 2026. It is not a comparison with OpenAI's products as a whole, including their instructions, tools and permission controls. ([Memory](https://learn.chatgpt.com/docs/customization/memories), [Personalization](https://learn.chatgpt.com/docs/personalize))

## What each version added

| Version | Added |
| --- | --- |
| 0.7.3 | Your corrections become checks you can enable. The effect of delivered memories is recorded. |
| 0.8 | Checks that the planning before the work and the review after it were saved. |
| 0.10 | Before delegating a decision, measures whether the AI can tell the answers apart. |
| 0.11 | Delivers knowledge in the middle of the work and right before a file is changed. |
| 0.12 | Feeds the reviews of whether memories helped back into how they are delivered. |
| 0.13 | Uses the reviews as a reason to revisit articles and their sources. |
| 0.14 | Reads related decision standards and article bodies, not only procedures. |
| 0.14.1 | When required work was not saved, returns how to fix it, and completes only after confirming the fix. |
| 0.15 | Articles can be edited and deleted on the page (history and sources are kept). |
| 0.16 | Knowledge found by hand that helped becomes easier to find later. Reasons for decisions and changed premises are delivered too. |
| 0.17 | Part of an article can be corrected. Why knowledge did not arrive can be found out, and a correction can be tried on past situations. |
| 0.18 | Compares a plan with the knowledge and assembles a procedure that fits this time's conditions (shipped with 0.19). |
| 0.19 | Combines reading the request, conversation and knowledge with saving decisions, observations and articles into one flow. |
| 0.20 | Reads the needed parts of long articles. Knowledge can be added during the work, and the results of using a procedure come back next time. |
| 0.21 | Separates current knowledge from its history, looks for counterexamples, and revisits procedures from failures and fixes. |
| 0.22 | Connects pieces of knowledge, checks them on a small scale, and grows what worked into procedures. |
| 0.23 | Delivers changes made by others, compares evidence, checks questions in other work, and merges simultaneous edits. |
| 0.24 | When the person in charge changes, hands on the conditions checked, the versions read and the state of the implementation. |
| 0.25 | Chooses past results that fit this time's conditions. Reports back whether knowledge was actually used and what happened when a procedure was run. |
| 0.25.1 | Makes the guidance for agents clearer, matching what actually happens. |
| 0.26 | Revisits decisions at the start and end of the work, and does not finish until the required saves are done. |
| 0.27 | Compares expectations before a check with the actual result, and chooses what to check next. |
| 0.28 | Chooses how much to read from real results. |
| 0.28.1–0.29.2 | Does not make the agent read the same content repeatedly. Memory cleanup goes two articles per request. |
| 0.29 | Records the work twice, at its start and end. Improves how investigations are done, after confirming on separate tasks. |
| 0.29.3 | Reduces repeated explanations while keeping the content of this request. |
| 0.30.0–0.30.4 | Makes reading and display lighter while keeping content and evidence. Recovers from writes that stopped halfway. Knowledge stays in the original project even when working in a worktree. |
| 0.30.5 | History cleanup and saving at the same time no longer damage records. |
| 0.30.7 | Past answers of yours that never helped are no longer shown to the agent. |
| 0.30.8 | That decision can be undone on the page. Lines you were writing before a request are no longer treated as the agent's. |
| 0.31.0 | An agent's calls to its memory fail less often. A message you send mid-work no longer costs the agent what it had read. |

What was thought at each version is under "Looking back at each version" at the end. Details of each change are in the [changelog](../CHANGELOG.md), how to use shikii is in the [guide](guide.md), and how knowledge is handed over through MCP is in [MCP knowledge delivery](mcp-knowledge.md).

## Updates that change how memory works

When you update to a version that changes how memory is handled, the agent starts reviewing the existing knowledge, showing "Upgrading memory…" and its progress. It keeps the original sources, the preferences you stated and past decisions, and gradually corrects outdated procedures and tidies duplicates. Reading alone does not mark something as reviewed, and parts without enough sources stay unresolved. Once the review is done, an ordinary update with the same policy does not start it over.

## Looking back at each version

For each version, what it added and what was thought at the time, oldest first. It says more than the one line in the table, and describes the changes from the user's side rather than the way the changelog does.

### In 0.17, a missed article can lead to a precise correction

Suppose an article says to update both language versions of a guide, but it did not arrive during an update. The agent can improve the knowledge in three steps.

1. **Find the reason.** Inspect the record from that moment: did the article fail to match, arrive already, or get excluded by prior assessments or the decision model? Today's search result is not substituted for the past.
2. **Correct the relevant passage.** If the condition did not describe the actual work, propose a source-backed text edit with a reason. An intervening edit causes a conflict instead of overwriting other sections or history.
3. **Try it before saving.** Compare retrieval candidates in past situations, including whether the change displaces another article. Later additions to the wiki stay out of the old comparison.

This lets the agent investigate missing knowledge before asking the person to explain it again. The person does not have to prepare examples or score the comparisons.

The comparison establishes **changes to retrieval candidates**. It does not rerun suppression or the decision model, or guarantee delivery or usefulness. It saves no article, assessment or human truth. Diagnostic history retains at most 32 attempts / 4 MiB; missing or incomplete history is explicitly unavailable for comparison. Whether the knowledge actually helps remains a separate observation from later work.

### In 0.18–0.20, use knowledge during work and return results to the next task

Memory available at the start of a task does not by itself establish how newly discovered conditions or failed attempts will inform later work. shikii connects using prior knowledge and recording the current experience through the same work record.

1. **Consult the proposed approach.** Read prior reasoning, rejected alternatives and applicability conditions, then consider whether they fit the current proposal. For recurring work, assemble a procedure with sources and the current conditions. Consultation and reuse were implemented in 0.18 and shipped with 0.19.
2. **Read the relevant original text.** In 0.19, the agent can read the request, conversation and knowledge together, then save judgments and observations with sources and article revisions retained. Human and AI messages stay distinct, and incomplete material identifies what is missing. In 0.20, even a long article can supply relevant exact passages with their revision and position; the full article can be fetched when a condition or exception remains unresolved.
3. **Add what emerges during work.** In 0.20, further searches, consultations and procedure reuse can join the same work record. Earlier articles and quotation sources remain available while the agent records discoveries that actually informed a decision. If part of a save fails, it can retry without duplicating the saved items.
4. **Return execution results to later work.** Link a used procedure to its execution conditions, actions, failures, corrections and reported results, with sources such as validation records. Later reuse can compare those past results with current conditions. If the procedure article has changed, it also flags that its revision differs from the one used previously.

For example, a previous release may have validation showing that knowledge and sources survived an update. The next release can read those conditions and results alongside the procedure to decide what to verify now. A different OS or starting version calls for checking those differences. A past success does not establish success for the current task.

These execution reports are evidence for the agent to assess. Saving a report does not create a success truth or a person's answer, and stays separate from measurements of judgment accuracy or long-term improvement.

### In 0.21, connect current guidance and counterexamples to procedure improvements

The agent reads original evidence and assigns roles to exact quotations, separating current conditions from earlier context. Unclassified text and changed premises stay explicit. Consultation searches interpreted counterexamples and failure reports independently, preserving the conditions and sources to compare with the current proposal. Recording a corrected failure returns current text and a proposed correction for procedure review immediately, rather than waiting for later reuse. The agent checks the evidence before updating knowledge; no extra person scoring is needed. Interpretations and reviews remain hypotheses: dates alone never establish validity, and recorded results never become automatic success truths.

### In 0.22: connect, check and grow project practices

An insight can connect exact passages and conditions from distinct articles and keep a small check with its reported results. Useful methods become sourced, conditional project practices that a later agent can receive, use and revise. Original evidence and revisions remain available, and results never become human truths.

When knowledge grows too long, preview a split or merge and save destinations before editing their sources. The aim goes beyond shared reading to later use, verification and improvement. See the [guide](guide.md) for operations and limits.

### In 0.23: build collective knowledge through actual work

A shared memory store gives agents access to earlier context. shikii also connects how one agent's discovery reaches another, gets checked under new conditions, and returns to the same project knowledge.

1. **Receive a peer's changes.** Compare the article revision actually supplied to the work with its current revision, including changed premises and sources. Confirming inspection records what was read; usefulness needs separate evidence.
2. **Compare the evidence.** Read explicit support, counterexamples and differing procedure reports with their exact conditions and sources. Shared sources remain visible. Different source IDs do not establish independent corroboration, and a majority does not establish truth.
3. **Carry the question forward.** A saved probe can appear in another relevant task. The agent chooses a small check within the authorized work and adds the observed result to the same question, retaining earlier observations and their conditions.
4. **Improve knowledge together.** Compare the original article, the current article and the proposed edit without saving. Nonoverlapping changes can produce a combined proposal. Conflicts remain explicit; saving still checks the current revision so a later edit cannot be silently overwritten.

For example, an agent writing a CSV tool notices a delimiter condition and records a question. Another agent checks it on separate synthetic inputs, records the cases that matched and the assumptions still unresolved, and preserves the first agent's observations. A later task can read both the changed article and those limits. The knowledge grows through different work, rather than treating repeated wording as corroboration.

This is a workflow for collective knowledge, not a claim of measured long-term improvement. Inspection, use, reported observations and human answers remain distinct. No model weights change, and a saved question grants no permission to run an experiment. See the [workflow and limits](guide.md#share-changes-and-compare-concurrent-work-0230).

### In 0.24, carry knowledge through an assignment's stages

An optional host integration gives research, planning, implementation and audit their own assignment context. A later agent can compare earlier coverage, unknowns and inspected revisions with current conditions. Implementation reports retain their code revision and integration state. Disagreements can return as a small distinguishing question and method, while checked failures can support a correction to the same procedure.

For example, a researcher records that small input passed and large input remains unchecked. The implementation agent sees that limit, reports its observed revision, and an auditor can add a contrary result and a bounded comparison method. A subsequent assignment receives those conditions and changes rather than starting from an isolated summary. Shikii retains the evidence; the host chooses what to run and integrate. Existing users need no external host or assignment input. See the [integration contract](guide.md#connect-external-assignments).

This version adds the shared API for external hosts. Carrying assignment conditions and implementation state is distinct from the chiten reports of actual use and execution described under 0.25 below. Assignment context selects relevant evidence within the same project; it is not an access-control boundary.

### In 0.25, select experience for current conditions and return use and results

Remembering a procedure does not establish the same outcome under a different OS, input format or implementation revision. Instead of taking only the latest reports, 0.25 selects candidates by comparing terms from the current request and conditions with earlier execution conditions, then with actions, failures and corrections. It returns at most two per procedure, uses recency to break ties and can retain an earlier failure with the same positive condition-term score as the top candidate.

Retrieval rank does not establish that the actual conditions agree. The agent reads the original conditions, sources and unknowns, and rechecks a changed article revision. Attaching a report or validation source does not establish success for the current task either.

A compatible chiten integration can return assignment reports to shikii while distinguishing an inspected article, knowledge that changed a decision and a procedure actually executed. Actual use records its contribution and evidence; execution reports keep the current conditions, action, failure, correction, result and sources. Saving a report does not automatically establish usefulness, success or a human truth.

For example, suppose a later worker applies a CSV procedure checked on Linux to quoted input on macOS. If the candidates include a Linux result and an earlier failure to handle quoting, the worker reads their conditions to decide what needs checking now. Only after actually trying the procedure does the worker report this input, environment and observed result. Planning alone produces no execution outcome. The next worker can read that report and its sources with the procedure, then check another condition.

chiten is a separately distributed, optional integration. Projects without it can record knowledge use and procedure outcomes through CLI, MCP or HTTP and use the same condition-based reuse. The comparison with shared memory concerns the workflow of selecting conditional experience and returning use and observations to their sources, alongside recalling earlier context. It does not claim measured long-term reductions in work.

### In 0.25.1, align skill and MCP guidance with these distinctions

0.25.1 is a patch that aligns the skill and MCP instructions with the existing behavior. It distinguishes human requests and decisions from agents' observations and checks as wiki sources. It explains that an earlier report must not be copied as the current execution, and reading or planning alone produces no execution outcome. MCP preparation uses one search, consult or reuse mode per call; field descriptions separate agreement and open choices from unverified execution results. Storage formats and runtime operations remain unchanged.

Update the binary and installed skills to load the revised guidance. Without hooks, rerun `shikii --init` in an existing initialized project after updating the binary; this refreshes skills while preserving project data and leaving hooks disabled. Then reopen agent sessions and restart the page and MCP. See the [update instructions](guide.md).

### In 0.26, review judgments during work and verify their saved results

Alongside carrying knowledge into the next task, 0.26 requires a brief starting judgment and a checked ending review for the current task. The request hook keeps the turn obligation, and the agent links its judgments to the request, article revisions and captured observations. It updates the judgment when the premise or next action changes.

For example, an earlier CSV procedure may require preserving ownership. The agent checks that condition at the start. If quoted input reveals a different problem, it revises what to check next. At the end it saves what was verified and which conditions remain unknown, with sources available to later work. Retaining the approach is valid; a new discovery or article is not required each time.

The ending hook verifies the required saves. Another successful save or a fresh preparation cannot resolve a failed correction or capture. Replaying an old finish cannot certify later evidence, and changed articles require a fresh revision check. Recovery retries continue as required saves decrease; a retry with no progress ends explicitly incomplete.

This adds review and save verification tied to the current conditions. It does not request a transcript of private deliberation or establish judgment quality or long-term improvement. Whether a later task avoids a missed condition remains a separate observation. Model weights stay unchanged, with no additional model or person scoring. See the [inputs and recovery workflow](../skills/shikii/references/recording.md#required-review-while-doing-the-work).

### In 0.27, test expectations and choose the next check

- Record the expected observation, conditions, method and decision before an authorized check. Link the actual reported result to the same immutable trial, then cite challenged results in an updated judgment.
- Select a small unresolved check with current evidence. Compare two saved executions of the same case, question, conditions, method and expected outcome using paired measurements.
- Adjust source-reading depth while keeping required reviews and exact-revision recovery. Unresolved judgments, pending saves and changed premises deepen the read.
- Check enabled guards before supported publication calls observed by Claude Code's Bash hook. Codex retains turn-end checks; OpenCode uses MCP and skills without automatic hooks.
- Keep judgments and failure explanations in concise bullets, preserving conditions and sources. Reports and paired values remain agent evidence, with no automatic human truth or model-weight change.

The aim is for an observation to change what the agent checks next, alongside carrying context forward. A saved plan does not run an experiment; matched values need interpretation. See the [workflow and limits](../skills/shikii/references/recording.md#expectations-execution-and-the-next-check-0270).

### In 0.28, learn how much knowledge to read

Alongside carrying earlier knowledge forward, shikii uses measured delivery and source-backed checks to choose **how much of that knowledge the current work needs**. It feeds experience from separate tasks into the reading depths introduced in 0.27.

1. **Measure actual delivery.** Record the UTF-8 JSON bytes of supplied knowledge, read counts and full-source recoveries. Include later source reads instead of comparing only the first excerpts.
2. **Compare the same task.** Compare the standard `focused` read with another depth, keeping article revisions, topic, host stage and quality check matched. Report whether the required conditions were recovered, with evidence. A deeper read that recovers conditions missed by focused can also become a candidate.
3. **Check another task.** One favorable comparison creates a candidate. Matching comparisons on two distinct tasks, with separate work and non-overlapping reported results, can change the next default under the same conditions. Renaming source IDs, changing provenance, reordering JSON keys or respelling the same JSON number does not add support.
4. **Return omissions.** A reported missing condition suspends that reading choice. A later success alone does not erase it; use a verified alternative or a deeper check. A changed article revision, topic or stage does not inherit the old preference.

For example, consider a long procedure for preserving data during an update. If a short excerpt omits a recovery condition and the agent then reads the full source, that recovery counts too. A smaller first delivery alone does not establish an improvement. When separate tasks support a reading choice that recovers the required conditions with less delivery than focused, the next worker can use it under matching conditions. A new omission suspends the choice and calls for rechecking.

Explicit depth, deeper reading for unresolved conditions or changed premises, mandatory reviews and exact-revision source recovery take priority. Quality feedback is optional. chiten 0.9.3 or later can return it with ordinary consultation reports; projects without chiten can use the same CLI, MCP or HTTP workflow.

What adapts is the project's knowledge-delivery method. Model weights and decision permissions stay unchanged. The measurements cover knowledge delivery and reading, not a general reduction in model tokens, calls or response time, or an improvement in answer quality. Two tasks are a fixed adoption condition, not proof of statistical independence. See [the inputs and limits](../skills/shikii/references/recording.md#learn-a-reading-preference-from-checked-tasks).

### 0.29: improve investigation procedures

Alongside choosing how to use remembered knowledge, 0.29 adds **a loop that selects investigation procedures from actual checks to reduce repeated work**.

1. **Generate a candidate.** When repeated investigation makes no progress, create a candidate that adjusts when to suggest a different method and how briefly to present that guidance.
2. **Check separate tasks.** Exclude the task that generated the candidate. Collect the agent's actual checks and evidence, together with measured guidance bytes.
3. **Adopt supported procedures.** Initially, two different tasks must report the required quality with less guidance. The candidate can then become the next procedure under matching conditions.
4. **Restore on regression.** A missed condition, greater guidance cost or unavailable supporting evidence rejects the candidate or restores its previous procedure. Retire a failed recipe; a different candidate needs more checks.

In 0.30.4, status, source pagination and limited history reads reduce repeated processing while preserving original evidence and decision conditions. The sections below also explain rereading reductions and interrupted-write recovery added in intervening releases.

For example, suppose a CSV investigation keeps searching the same specification without changing its result. shikii can propose suggesting a different check sooner. When separate CSV tasks report that the required conditions were checked with less guidance, the candidate can be adopted. A later reported omission restores the earlier procedure. Recalling a past suggestion becomes connected to validation on other tasks and withdrawal when it fails.

Ordinary recording uses one initial `prepare` and one ending `record`. Send what changed, the evidence checked and what remains once. If quality was actually checked, optionally add `result.sufficient` and reuse that evidence. A conversation with no new lasting knowledge needs no long reflection or wiki article. Additional source reads and important corrections during work can be recorded when needed.

Unknown quality stays unknown. Empty or unsupported quality results contribute no support; the original report is saved and its unverified grade stays unresolved without repeated storage retries. Result and reading checks for the same execution must agree on the check, quality and evidence before contributing support. Renaming the same proof does not create another validation task. Original executions and evidence bindings remain available after session expiry.

Candidates change only the fixed guidance procedures. Required checks, original-source recovery, evaluation rules and permissions stay outside their control, with bounded exploration and storage. This changes project procedure selection without retraining a model or permitting arbitrary code changes. Fewer guidance bytes do not establish general improvements in elapsed time, model tokens or answer quality. See the [recording contract](../skills/shikii/references/recording.md#automatic-improvement-of-loop-procedures).

### In 0.28.1–0.29.2, reduce rereading while retaining what was read

In 0.28.1, repeated long MCP strings share their original text and retain their original locations. Complete JSON remains available, so a shorter display does not remove source material. In 0.29.1, identical excerpts can be reused within the same task. Omitted text is not treated as read; changed conditions and explicit full recovery still take priority.

In 0.29.2, repeated notices share text within a response, and only quotations and excerpts actually returned in the final response count as delivered. A related article's earlier review can be reused when every content and evidence field is identical and only version lineage differs. Edited articles themselves, new evidence and changed premises still require review.

After a memory-policy change, routine maintenance covers one batch of at most two articles per task. Saved progress survives interruption, while an explicit request to complete maintenance can continue the remaining batches. Preferences, original sources and past judgments remain; unavailable sources are not marked as reviewed.

### In 0.29.3, preserve the current intent while reducing repeated delivery

This version adds delivery cost control to the flow of saving knowledge and using it in later work. For a request such as “keep the owner condition, do not publish, and leave the unresolved choice open,” preparation retains the request text and its provenance while reducing other response content. If they cannot fit the response budget, it fails before saving rather than silently omitting the request.

Within the same task, unchanged article summaries, applicability conditions and current/history interpretations can refer to an earlier delivery. Reference substitution retains unclassified material, rechecks, counterexamples and incomplete status. Changed conditions or source revisions and explicit full reads bypass reuse. The overall response budget can still omit article text or orientation; keep those conditions unread and unresolved, then use a sufficient budget or the individual exact-revision query for complete recovery. This also restores the original text when the earlier response is unavailable.

The addition controls the cost of delivering stored knowledge under the current conditions. It does not infer or summarize the person's purpose or agreement; the original request and condition references remain available. Reuse applies only when the complete transport becomes smaller, with no extra model or setting. Reduced delivery bytes are checked separately from actual judgment quality or total task time. See the [input and reference contract](../skills/shikii/references/recording.md#bounded-reading-and-memory-upgrades).

### In 0.30.0, reduce the work of reading stored knowledge

While shikii is running, it can reuse article parsing within a fixed memory budget. It still reads the current file bytes before reuse, checks sources and watched premises, and revalidates the preparation before saving. Article text stays shared while each caller receives its own mutable metadata, reducing copying without shortening the request, evidence or unresolved conditions.

This temporary cache disappears when the process ends. The project's durable knowledge remains in its source-backed wiki, with the same review and recovery requirements.

Within one operation, shikii can also share decoded source records, conversation-history aggregates and measurements of an exactly identical response. It skips work that cannot contribute an excerpt and avoids holding unrelated vocabulary during ordinary search. These changes preserve the original request, search rankings, complete-source recovery and commit-time change detection. Reusing work inside an operation does not summarize or replace the project's durable knowledge.

Tied articles also keep a stable order for the same search. Term weights and delivery conditions remain the same.

### In 0.30.1, share explanations while retaining complete recovery

The common MCP workflow remains complete in the `shikii_wiki` description. Initialization points agents there before using tools and retains immediate safeguards. This avoids repeating the full workflow on every tool in clients that attach server guidance to each description.

Within the same task, a fixed `collective.notice` can refer to its earlier delivery when all selected articles are already reusable, its text is unchanged and the complete transport becomes smaller. Updates, contrasts, investigations, missing material and recovery references remain. If the earlier response is unavailable, repeat preparation with the same receipt/query and `reasoning:{depth:focused}` for full guidance; explicit source recovery and new receipts also keep the full notice.

Programmatic clients can read `structuredContent` while retaining error state and additional content; models should receive one complete view of a result. The server keeps both representations. These changes reduce repeated presentation without summarizing the request or evidence. Value preservation and delivery size remain separate from actual model judgment quality. See [MCP knowledge delivery](mcp-knowledge.md).

### In 0.30.2, keep interrupted records usable by later work

Reusing memory requires a complete new line to remain readable after an earlier write fails. In 0.30.2, an unfinished tail is separated from the next line, and a failed append batch is rolled back when possible. A failed rollback is reported, and the first subsequent successful append remains readable. Outcome-label deduplication and append share the same lock.

When answering the final question on the page, the result stays with its original question and card instead of changing another question's counts. These changes preserve recovery and answer identity; failed saves and agent reports are not recreated as human truths.

### In 0.30.3, keep worktree experience in the same project

A dedicated worktree is a code checkout. The wiki and shared work records stay in the original project, so an agent does not start a separate knowledge history for each checkout. Reports name the worktree, branch, checked commit and shared evidence location. This clarifies where existing project memory is kept; it does not change model weights or the evidence requirements for learning.

### In 0.30.4, make status and reading lighter while retaining evidence

When one status response shows counts for multiple decisions, label calculations are shared within that request. Human-label precedence, latest results per input, threshold-specific counts and rejected outcome provenance remain intact. Old counts are not carried into another request.

When a long source is read in parts, size measurement avoids an unnecessary encoded copy and fitting starts near the response budget. The final host response is still checked against its limit. Exact UTF-8 text, offsets and continuation checks remain; changed sources or request conditions still cause recovery to be refused.

A history read limited to the latest records keeps only that many candidates. It still scans and decodes every line, preserving equal-time row order and invalid-row handling. Small histories that fit the limit are sorted directly. This reduces repeated status, delivery and history processing while retaining project memory and evidence. Model weights, learning adoption criteria and decision authority do not change.

### In 0.30.7, past answers that did not help are no longer shown

shikii shows the agent, for reference, what you answered before to similar requests. An answer that never helps, though, adds to what the agent reads every time and takes the place of lessons that do help.

In 0.30.7, an answer that the agent's review marked "not used" two or more times, and that never helped, is no longer shown. It leaves the "Answered" list on the page and appears under "Past answers the AI judged not useful" in the knowledge section. The answer itself stays as a correct answer and still counts when lines for decisions are drawn. If, across the project, past answers go unused ten times and never help, shikii stops showing past answers altogether.

The AI's review makes this call, and it is not always right. So 0.30.8 lets a person undo it.

### In 0.30.8, a person can undo the AI's decision

An answer the AI judged not useful in 0.30.7, and the decision to stop showing past answers, can be undone with "Undo" in the knowledge section of the page. After an undo, the earlier assessments no longer count, and the decision is made again only from the times the answer is shown after the undo. Your answer itself is not touched. The AI decides, and a person can take the decision back.

The same version also lets the checks that stop the agent tell the agent's lines from your unfinished ones. Lines you wrote before a request and had not committed are not treated as the agent's. A line the agent wrote and a check stopped is remembered until it is deleted or committed, and it is stopped again in later work. This is not about memory as such, but it carries "who wrote this line" correctly from one piece of work to the next.

### In 0.31.0, fewer failed calls to the memory

The versions so far grew what is remembered and what is delivered. 0.31.0 looked at how an agent's calls to that memory fail, and reduced the failures. Counting this machine's conversation records showed agents asking for help and getting one line of usage, long replies cut in the middle by the agent's tool, and refusals that did not say what to send next. A memory that has grown is not used when calling it keeps failing.

So help now prints forms that run as written, a refusal says its cause and what to send next, and a reply is kept within what the agent's tool can show. When it does not fit, what the next step needs stays: the top search candidates and the titles of the lessons.

The other change is for a message you send while the agent is working. Until now that started a new request, and the agent read the same pages again. In 0.31.0 the next preparation takes over the pages it had read and the sources it had saved. Only references go over: the result is reported as the new request's own.

Before and after were compared by sending the same small request to real agents. Each was measured a few times, so this does not say every request improves by the same amount.

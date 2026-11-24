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
| 0.32.0 | Checks wiki wording against sources while preserving meaning; delivers prepared context and saved articles for review. |
| 0.33.0 | Agents read only the section of a reference they need, and the MCP tool definition is smaller. |
| 0.34.0 | OpenCode 2.x also gets lessons through hooks and is asked for the record at the end of the work. |
| 0.35.0 | The memory upgrade can set aside a page whose original evidence cannot be read. Asking for a memory optimization reviews those pages again. A go-ahead message counts as a reply to the previous request. The question about a decision asks what you wanted, and has three answers. |
| 0.36.0 | Codex gets lessons right before a command or a file change (the agent reads them with the result of that call), and a guard can refuse a command there. A subagent assesses the lessons it received, in Claude Code, Codex and OpenCode 2.x. The assessment of a delivery is asked only of the session that received it. |
| 0.36.1 | An agent assesses a delivered lesson in the same request it received it, so a lesson delivered in the last request of a session can be assessed too. |
| 0.37.0 | An agent working in a worktree kept under a project gets that project's lessons and guards, and its records go to that project. |
| 0.37.1 | No change in how shikii remembers. The libraries behind the web screen and the GitHub Action are updated. |
| 0.37.2 | A subagent gets the rules for every request when it starts, instead of lessons picked from the text of its task. |
| 0.37.3 | No change in how shikii remembers. The hooks start git much less often, so they run faster. |
| 0.37.4 | No change in how shikii remembers. The hooks also remember git's answers in a worktree and a submodule, so they run faster there too. |
| 0.38.0 | What an agent writes as left in its work record, and the areas it did not examine, reach the next task. Words the wiki could not answer are remembered, and when they find nothing again, the record of the task that was open when they found nothing before is pointed to. |
| 0.39.0 | When a search of several words finds nothing, the reply names the words that find an article alone. The pointer for words that found nothing before leads only to a work record that speaks of those words. |
| 0.40.0 | Each project chooses whether TypeSafe's Jev or the local model answers its decisions; the choice comes before the environment variables. A tool can ask the ready-made question quote-supports. |
| 0.40.1 | When old memory is moved into the wiki, an agent reviews up to 10 articles at a time instead of 2. A search from the command that finds nothing is tied to the session that ran it. |
| 0.40.2 | A search from the command that finds nothing is tied to its session in OpenCode too. |
| 0.41.0 | A person can remove what a finished agent session left in the project, and a tool can withdraw the articles it made from removed work, kept until the person restores or deletes them; removal from the command line or, after shikii asks them directly, over MCP. |
| 0.42.0 | At most once a day, the agent goes through its memory: articles whose underlying files changed are checked against the files again, new or changed articles against their sources, and the person's recent replies are read for corrections the wiki does not hold yet. shikii edits no article itself. |

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

For example, suppose a CSV investigation keeps searching the same specification without changing its result. shikii can propose suggesting a different check sooner. When separate CSV tasks report that the required conditions were checked with less guidance, the candidate can be adopted. A later reported omission restores the earlier procedure. Recalling a past suggestion becomes connected to validation on other tasks and withdrawal when it fails.

Ordinary recording uses one initial `prepare` and one ending `record`. Send what changed, the evidence checked and what remains once. If quality was actually checked, optionally add `result.sufficient` and reuse that evidence. A conversation with no new lasting knowledge needs no long reflection or wiki article. Additional source reads and important corrections during work can be recorded when needed.

Unknown quality stays unknown. Empty or unsupported quality results contribute no support; the original report is saved and its unverified grade stays unresolved without repeated storage retries. Result and reading checks for the same execution must agree on the check, quality and evidence before contributing support. Renaming the same proof does not create another validation task. Original executions and evidence bindings remain available after session expiry.

Candidates change only the fixed guidance procedures. Required checks, original-source recovery, evaluation rules and permissions stay outside their control, with bounded exploration and storage. This changes project procedure selection without retraining a model or permitting arbitrary code changes. Fewer guidance bytes do not establish general improvements in elapsed time, model tokens or answer quality. See the [recording contract](../skills/shikii/references/recording.md#automatic-improvement-of-loop-procedures).

### In 0.28.1–0.29.2, reduce rereading while retaining what was read

In 0.28.1, repeated long MCP strings share their original text and retain their original locations. Complete JSON remains available, so a shorter display does not remove source material. In 0.29.1, identical excerpts can be reused within the same task. Omitted text is not treated as read; changed conditions and explicit full recovery still take priority.

In 0.29.2, repeated notices share text within a response, and only quotations and excerpts actually returned in the final response count as delivered. A related article's earlier review can be reused when every content and evidence field is identical and only version lineage differs. Edited articles themselves, new evidence and changed premises still require review.

In these versions, after a memory-policy change, routine maintenance covers one batch of at most two articles per task (the batch was later raised to 10 articles). Saved progress survives interruption, while an explicit request to complete maintenance can continue the remaining batches. Preferences, original sources and past judgments remain; unavailable sources are not marked as reviewed.

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

### In 0.32.0, check what memory says while reducing recording work

Ask the agent to "tidy shikii" to review existing articles against their original sources. The skill preserves conditions, exceptions, uncertainty and history, and repairs unsupported wording or repetition with the same meaning. It does not use length as a quality target or turn an unreported result into a claim that the work was not performed.

New or revised claims are checked against the original request and sources before saving. Interpretations, translations and combined sources receive a separate review when available; direct copying that meets the skill's conditions can be checked by the author. The agent reports any unavailable independent review. These instructions guide the agent; they do not guarantee the quality of every generated sentence.

Request hooks now deliver prepared context directly. After saving an edited article, shikii returns its text and source references for review, and distinguishes successful saving from completed review. The agent still checks the content and records its review. Stored knowledge keeps its sources and history.

### In 0.33.0, agents read less

0.31.0 and 0.32.0 reduced failed calls to the memory and the work of recording. 0.33.0 looked at how much an agent reads before that. Counting this machine's conversation records showed agents reading the whole recording guide at the start of a task, and the MCP tool definitions taking a large part of each session. Codex and OpenCode take those definitions whole in every session.

So the skill and the preparation reply now link to a section of a long reference, and the skill says how to read one section. The MCP tool definition leaves out operations that were almost never called, and a description repeated in several places is written once. The removed operations still run from the CLI, and calling one over MCP returns the way to do it.

After the change, the same small request was sent once each to Claude Code, Codex and OpenCode. All three finished the work without reading the recording guide whole. Each ran once, so the difference is not separated from variation between runs.

### In 0.34.0, OpenCode gets the hooks

Until now OpenCode had only the MCP settings, so shikii could not deliver lessons before a request or ask for a record at the end of the work. OpenCode 2.x lets a plugin follow a conversation, so shikii installs a plugin that runs the same hooks as in Claude Code and Codex.

One OpenCode server serves the conversations of several projects, and its notices reach the plugin of every project. Each conversation's hooks therefore run only in its own project, and the check at the end of the work runs once for each piece of work. The plugin went through five rounds of independent audit and was checked with a real OpenCode 2.3.0. The cases where lessons do not arrive, such as moving a conversation to another project in the middle of the work, are listed in the integration guide.

### In 0.35.0, the memory upgrade can set aside an unreadable page, and the question has three answers

The memory upgrade reviews old pages one batch at a time. A page whose original evidence cannot be read, or does not support its lesson, can now be declared `blocked` in its review record, with the reason. The batch skips that page and moves on, and the page comes back for review when its text changes.

Asking for a memory optimization reviews every page again, including pages declared blocked. A normal memory upgrade still skips them.

A go-ahead message, such as the Japanese phrase for "let's start," counts as a reply to the previous request when the organizing marks it as continuing that request (`continues_previous`); then the question about a decision is not asked, whatever its wording. Until the organizing comes, the fixed list of replies (ackReplies) joins a short go-ahead to the previous request provisionally; the organizing then decides.

The question about a decision that should have been asked first now says what the person wanted, and it has three answers: it should have asked, it was fine to go ahead, and the question does not apply. A new project gets the new wording. A project whose copy under `.shikii/decisions/` still has the built-in wording of 0.9.2 through 0.34.0 (or the 0.34.1 development build), with no edit and no comment of the person's, gets the new wording on the next update. An edited copy or one with a comment keeps its old wording; a copy that cannot be read or written is kept and tried again at the next seeding. Stored answers are not converted, and the calibration starts again.

### In 0.36.0, the one who used a lesson is the one who assesses it

shikii asks whether a delivered lesson was used, and that answer decides whether the lesson is delivered again and whether its article comes back for review. Until this version the answer often came from someone who could not know. A lesson delivered to a subagent was assessed by its parent, which sees only the subagent's report. A lesson delivered in one session could be listed in the review of another session. In a project with 145 articles, more than half of the assessments of lessons delivered to subagents were "unknown".

Now a subagent ends its final report with one line for each lesson it received, saying what it did because of the lesson or that it did not use it, and shikii saves those lines. shikii does not stop the subagent or change its report. The parent is no longer asked about those lessons, and a session is asked only about the lessons delivered to it. When the work followed a lesson but it is not clear that the lesson was the reason, the assessment can say so, and the delivery counts as one where the lesson applied.

Codex now gets what Claude Code had: a lesson right before a command or a file change, a guard that refuses a command before it runs, and the subagent's own assessment. A Codex subagent gets no lesson chosen by its task, because Codex does not let a hook read the task. It gets the rules for every request, how to report, and lessons right before its commands and file changes.

We ran real agents in a sandbox. In Codex, a subagent received a lesson right before its file change, read it together with the result of the change, ran the check the lesson asks for afterwards and reported it, and the report was saved; a forbidden command was refused before it ran. In Claude Code, a subagent received a lesson at its start and reported it as unused, and the report was saved. In OpenCode 2.x, a subagent received a lesson at its start and reported what it did, and the report was saved. These are single runs, so they show that the path works, not how often a model writes the line.

### In 0.36.1, a lesson is assessed in the request it was delivered in

Until this version shikii asked about a delivered lesson at the session's next request. A session that ended after one request was never asked, so the lessons delivered in it stayed unassessed. Many sessions end that way: one request, one piece of work, and the session is closed.

Now the agent assesses the lesson when it records its work, in the same request. It has just done the work and knows whether it used the lesson. If it records the work without the assessment, the reply names the lesson and asks for it before the agent answers you. What the agent still leaves out is asked about at the next request, as before. An assessment made in the same request is made before you have reacted to the work, so shikii marks it and it can be counted apart.

We ran real agents in a sandbox, each in a session of one request, with one task and one lesson. With this version the assessment was saved in 16 runs out of 16 (Claude Code 6, Codex 6, OpenCode 4), for a lesson delivered with the request and for one delivered right before a file change. With 0.36.0 it was saved in 0 runs out of 8. The runs show that the flow works with the three agents; they do not show the rate for other tasks, for long work, or when several lessons arrive.

### In 0.37.0, a worktree under the project uses the project's memory

Agents often work in a worktree kept under the project: Claude Code puts the worktrees of its subagents in `.claude/worktrees/`, and chiten puts each worker's in `.chiten/wt/`. Until this version shikii looked only at the top of the git working tree, and the top of such a worktree has no `.shikii/`. So shikii was not running there: no lesson was delivered, no guard stopped the work, and nothing was recorded. Work handed to another agent is the work that most needs the project's rules, and it did not get them.

Now shikii takes the nearest folder at or above the current folder that was initialized on this machine as the project, without asking git. Such a worktree gets no `.shikii/` of its own, so the wiki, the guards and the records stay in one place. `shikii guard try` in such a worktree checks what the worktree changed since it left the project's branch, with its commits, so another tool can apply the project's guards to a worker's finished work.

We ran real agents in a sandbox: a project with one guard that forbids a word, a worktree under it, and a request to add that word to a file. With this version Claude Code and Codex were each stopped by the project's guard, which named the file, in every run (one each with the released code, and a few with earlier forms of this change). Their records went to the project's `.shikii/`, and the worktree got no `.shikii/`. With 0.36.1 the same run in Claude Code was not stopped, and nothing was recorded. After the stop, Codex took the word out again every time; Claude Code sometimes took it out, and sometimes left it in and asked you to choose. This is one task and a small number of runs: they show that the hooks reach the worktree, not how often an agent then fixes the work. A worktree created outside the project folder is still not reached.

### In 0.37.1, how shikii remembers does not change

This version updates the libraries behind the web screen and the GitHub Action, and fixes a known issue in a library used only to build the screen. Nothing in how shikii stores, delivers or assesses lessons changed: the program code of shikii itself changed only in its version number and the updated Go library. The audit guide in the skill, which the program carries and hands to agents, has more text, but it does not touch how lessons are stored, delivered or assessed. We did not run real agents for this version for that reason.

### In 0.37.2, a subagent starts with the rules for every request

Since 0.9.1, when a subagent started, shikii picked lessons from the text of the task it was given and delivered them with the start. We counted how the agents assessed those deliveries in this project's records from 2026-11-14 on. The lessons picked from the task text were useful about one time in twelve: the task text says what to do, not the situation the lesson was written for, so the pick was often off. The rules that apply to every request, delivered to the same subagents, were useful about four times in ten. Taking out the picks would have lost a few useful deliveries (one in twenty of all useful deliveries in that period) and about a sixth of all unused ones, so the share of useful deliveries goes up.

So a subagent now starts with the rules for every request and how to report what it did. What its task needs, it looks up in the wiki itself, as about a quarter of the subagents in this project already did. The lessons are kept: they are still delivered to the main agent for a request, and, in Claude Code and Codex, right before a command or a file change, also to a subagent. While checking this version with a real Claude Code we found that such a lesson did not reach a subagent when the parent had already got it in the same request; this version fixes that. These numbers come from one project's records, counted before the change. Whether the share goes up in use is not measured yet.

### In 0.37.3, how shikii remembers does not change

This version makes the hooks start git less often. Nothing in how shikii stores, delivers or assesses lessons changed. Every hook asked git where the repository is, what the current commit is and what the working tree holds, several times over, and a git start can cost 10 to 15 ms on macOS. A request used to start 11 git processes, a tool call 2 and the end of a turn 9. Now a request starts 3, a tool call none and the end of a turn 3. The three that remain each need different data: the files the working tree would add, the files git tracks although a rule ignores them, and the listing of the result. The first request, before anything is remembered, starts one more. A few more are started after a command that changes the repository's settings (such as `git remote add`), while a guard is enabled, and while a Codex memory file waits to be moved to the wiki.

The answers are checked, not trusted. The remembered location of the repository is used for up to 30 minutes, and only while the nearest `.git` folder above is the one git answered about, and git is asked again for a worktree, a bare repository, a `.git` that is a file, a repository chosen through an environment variable, and anything else the folders cannot settle (0.37.4 remembers a worktree and a `.git` that is a file too: see the next section). The remembered answers are small files in `~/.shikii/agent/places/`, and files not written for a week are removed. The current commit is read from the repository's files only where those files tell the whole story, and compared in the tests with git's own answer in each state: before the first commit, a loose or packed branch, a detached HEAD, a branch with a slash in its name, and a subfolder. The listing of the working tree is the same as before, byte for byte, in the cases we tried. We ran a real Claude Code, a real Codex and a real OpenCode with a stand-in for git that counts the calls: in all three, git was started 3 times for a request, never for a tool call (OpenCode has no hook before a tool) and 3 times at the end of a turn. The new tests list the git calls a hook event makes on its usual path (without a guard enabled), so a call added there shows up.

### In 0.37.4, the hooks also remember the repository's location in a worktree and a submodule

This version extends 0.37.3. Nothing in how shikii stores, delivers or assesses lessons changed. In 0.37.3 the remembered location of the repository was used only where `.git` is a folder. In a worktree, a submodule, and a repository whose git folder was placed elsewhere with `--separate-git-dir`, `.git` is a file that points to the git folder, so git was asked every time: a request started 4 git processes, a tool call 1 and the end of a turn 4. Now a request starts 3, a tool call none and the end of a turn 3 there too, the same as in an ordinary repository.

The hook finds the git folder from the files: the `.git` file and, in a worktree, the `commondir` file that names the folder shared with the repository the worktree was made from. It uses the remembered answer only while all of these hold: the repository's `config` file (and, for a worktree, its `config.worktree` file) is the same file as when git answered, with the same inode, size, modification time and change time; the folders found are the same; and, where the settings name the working tree (`core.worktree`, which a submodule has), the folder they name is still the folder git answered with. That last check also fixes a case 0.37.3 had in an ordinary repository: when the settings reached the working tree through a link and the link was later pointed to another folder, the old folder was used for up to 30 minutes. The settings are read only in the form git itself writes them; a value with quotes, a continuation line or a second value is left to git. Before remembering anything, the hook compares what it found in the files with git's own answer (the top of the working tree, the objects folder, the exclude file and the current commit) and remembers only when they are the same, so a layout it reads wrongly costs one more git start, not a wrong answer. Git is still asked for a `.git` that is a link, a repository chosen through an environment variable, a bare repository, and any layout the files cannot settle. The current commit of a worktree is read from the worktree's own HEAD and the branch in the shared folder; a reftable, and a HEAD that names a reference belonging to one worktree alone (under `refs/worktree/`, `refs/bisect/` or `refs/rewritten/`), are left to git. In the tests, the current commit is compared with git's own answer in a worktree and a submodule too, and a test fails when most of these checks are removed. There are two exceptions. A configuration file's inode and size are covered by its modification time and change time, so removing them leaves every test passing. Where two checks guard the same thing, such as the git folder found and the shared folder found, a test fails only when both are removed.

We ran a real Claude Code, a real Codex and a real OpenCode, each in a worktree and in a submodule, with a stand-in for git that counts the calls: in all of them git was started 3 times for a request, never for a tool call (OpenCode has no hook before a tool) and 3 times at the end of a turn. All six runs used the code of this version.

### In 0.38.0, what was left and what the wiki could not answer reach the next task

This version closes two places where something known at the end of a task was lost before the next task. One is what was left because it could not be examined. The other is a topic that was searched for in the wiki and not found.

We first added a field in which the agent records the areas it examined (`coverage`). For each area it says reported, no problem found, left out, or not examined, and the areas that were not examined are delivered to later tasks. But when we gave a real Claude Code (sonnet) a request that leaves one point it cannot check, the agent wrote what was left in the existing `remaining` field and did not use the new field: not in the first run, and not in any of the three runs after the reply to its record told it how to write the new field. `remaining` did not reach the next task, so when a new session was asked to check what the last check could not look at, it answered all three times that there was no record of the last check and it could not tell what was meant.

So we stopped asking the agent for a new step and passed on what it already writes: `remaining` now goes to the next task as it is. It is delivered to the next task's preparation and is closed when that task records its result. If it is still left, the task that received it writes it again in its own `remaining`. After this change we ran it six times. In four runs what was left stayed in the records, reached the next session, the next session checked the point and gave the answer (one customer missing from the list), and the item was closed. The other two show that only the sentence the agent writes is delivered. In one run the agent did not write what was left in its record, so nothing was delivered. In the other it wrote only a few words about finding where the file is, so the next session checked where the file was and did not compare it with the list. An area the agent itself recorded in `coverage` as not examined stays open until a later task records that it examined the same area.

The topic the wiki could not answer went the same way. Listing the missed words in the reply to the record, with a sentence asking for a short note when the answer had been found, produced no note in twelve runs. Here too we used what is already written. The record of the earlier task's result says where it found the answer. So when the same words find nothing again, shikii now points to the record of the task that was open when they found nothing before (with sessions running side by side, it need not be the task that searched). After this change, in all four runs the next session read that record and answered, in substance, that the earlier task reported the closing day as the 25th, but the original document no longer exists and it could not be confirmed (in this trial the document was removed before the second session). What is pointed to is the earlier task's report, not a verified article, so this is the answer we wanted.

The trials also turned up a defect in MCP. The command `shikii wiki query <id>` returns a saved record, but the same id given to the MCP search was searched as a word and returned nothing. The agents that received the pointer went to read it over MCP all three times, and all three took the result to be empty. The handling of a search made only of ids is now in one place and is the same for the command, MCP and HTTP. We also added tests that go through the real MCP entrance.

Only Claude Code (sonnet) was tried. Codex and OpenCode were not. Each trial used one scenario. In the trial of the topic the wiki could not answer, the first request told the agent how to look: search the wiki first, and read the document if the wiki has nothing. What reaches the next task is only the sentence the agent wrote in `remaining`. If it writes nothing, nothing is delivered, and if it writes too little, the next task mistakes what is to be checked. An item made from `remaining` is closed when the task that was handed it records its result. If that task does not write it again, it does not reach the tasks after it.

### In 0.39.0, a search that finds nothing points somewhere only when that helps

Version 0.38.0 remembered the words the wiki could not answer and, when the same words found nothing again, pointed to the record of an earlier task. This version went back to the condition under which that pointer helps.

The pointer helps when the record it leads to speaks of the words. Version 0.38.0 tied the words to the record of the task that was open when they found nothing. So a task that searched for something beside its main work, and wrote nothing about it in its result, was tied too, and a later search was led to a record that said nothing about the words. The first idea was to remember which session searched and narrow the tie to it. A search that continues a task's preparation knows its session, so that was done. But `wiki query` does not know a session, and the record of the session that searched does not always speak of the words. So the condition itself is now checked. A record ties words only when those words find the text of that task's result. The rule is the wiki's own search by words, and only the text the agent wrote is searched (the names of the fields are not counted). Applied to the 58 results that real agents left in the trials of 0.38.0, the words found all 32 results on the topic they searched and none of the 26 on another topic.

Next came the search that finds nothing. To see whether agents should be asked to try other words when a search finds nothing, a wiki with one article was asked for a rule in words the article does not use. The article says "the deadline for invoices"; the question says "closing day". How to look was left to the agent. The agents tried other words without being asked. What tripped them was the form of the rewording. Alternative words listed in one search can find nothing even when one of them is in the article, because a search of two or more units needs at least two of them in one article (a search splits its words into units). With the program as it was in development before the change that names the words found alone, the first session could not answer for that reason in two of three runs.

The search knows which words find an article. So when a search of several words finds nothing, the reply now names the words that find an article alone. With this version the first session answered the rule in all three runs. In the two runs where a search of several words returned nothing, the agent searched the named word next and found the article. The second session also answered in all three runs (two of three before). In all three it searched "closing day", found nothing, was pointed to the record of the earlier task, read it and reached the article. The skill now says to search alternatives one word at a time.

One idea was left out: remembering, for words that found nothing, the article the agent found afterwards. Only a search that continues a task's preparation can tell what was found afterwards, and most searches in the trial were MCP searches that do not. It would not have applied in any run, so it was not added.

Four points that the audit of 0.38.0 left for the next version were fixed as well. Words that found nothing are remembered as the same words when they differ only in case or in how a character is composed. Words from a search that continues a task's preparation are reported only in the record of the session that searched. When the reply to a preparation is over its size limit, fewer areas that were not examined are named before the text of an article is dropped. Open areas stay in the list after the record of areas grows past about 4 MB.

Only Claude Code (sonnet) was tried, with one scenario and three runs each. Codex and OpenCode were not. A word is named only when one of the listed words is in an article (in this scenario, "invoice" was). When none of the alternatives is in an article, the search still finds nothing. The 58 results were all checked with the one word "closing day". The trial was run in Japanese; the words quoted here are translations.

### In 0.40.0 to 0.40.2, a project chooses the AI that answers, and a search that finds nothing is tied to its session

Version 0.40.0 let each project choose whether TypeSafe's Jev or the local model answers its decisions. It also added the ready-made question quote-supports, which a tool such as chiten asks to learn whether a passage supports a claim.

Versions 0.40.1 and 0.40.2 finished what 0.39.0 left. Version 0.39.0 reported the words of a search that continues a task's preparation only in the record of the session that searched, but `wiki query` still did not know its session. It turned out that agents pass the id of the current session to the shell commands they run, in an environment variable (`CLAUDE_CODE_SESSION_ID` in Claude Code, `CODEX_THREAD_ID` in Codex, `OPENCODE_SESSION_ID` in OpenCode). The id left in the MCP server, on the other hand, is the one it was started with, and differed from the current session. So only `wiki query` run as a command uses the id in the environment, and only when the hook kept a record of that session in the project. When the variables name different sessions, the search is tied to none, so that no guess is kept. Version 0.40.1 did this for Claude Code and Codex, and 0.40.2 for OpenCode. Searches through MCP and the HTTP API still belong to no session.

Version 0.40.1 also made wiki searches faster. One search checks each source the articles cite once. In a made-up wiki of 300 articles, a search that finds nothing went from about 98 ms to about 67 ms, and the list without search words from about 124 ms to about 77 ms. The replies are byte for byte the same as before. When old memory is moved into the wiki, an agent now reviews up to 10 articles at a time instead of 2.

For OpenCode, the variable passed to commands, and the hook keeping its record under the same id, were checked with the real OpenCode 2.3.2. A full run in which OpenCode's model runs the command could not be done, because the model's provider was overloaded, so the command was run by hand. An OpenCode subagent runs in a session of its own that the hook keeps no record of, so a search inside a subagent is read to belong to no session (this was not run with the real OpenCode).

### In 0.41.0, what a finished session left is removed after a person checks it

When 0.40.2 was checked, the real OpenCode was run in this project. Those trial sessions left about 30 files of real records: their work states, the deliveries and assessments of lessons, sources, decisions and machine answers, the ledger of areas not examined, and the records of checks. What they held was true, but trial records mix into the hints later work gets. shikii had no way to remove records, and records point at each other by id, so removing them by hand could break the real ones.

So `shikii forget` was added. What it removes is decided by following the fields that hold the session's id and the ids the session's own records hold. It never searches for the id as text and removes what it finds. A check showed that a text search finds less than half of what should go, and that one source it does find is cited by a wiki article and must stay. A session's work state also lists sources of other conversations that the session only read. So only what the session made is removed, and what it only read stays.

Removing records is for a person to decide. At first it was not on MCP, and a person ran it in a terminal. Later it was made to work over MCP too, with the person asked first, using MCP's elicitation: shikii asks the person directly, not through the agent, whether to remove the records, and removes them only when they choose yes. A yes that the agent passes as an argument of the tool removes nothing. If the records change while the person decides, nothing is removed.

What was checked is projects made inside the tests and the four trial sessions of this project. Having the real Claude Code, Codex and OpenCode show the MCP question was not tried. Claude Code and Codex are read to support it from the programs on this machine; OpenCode is not known.

Later audits changed how "still running" is decided several times. At first it was whether 10 minutes had passed since the last record. But long work can go more than 10 minutes without a record. So the hook now notes the agent's process, and the decision is whether that process is alive. Even with the application open, a session can be removed when it is not thinking and nothing it started is still running. Whether it is thinking can only be inferred from hook events, though. Each audit found one more case, such as a subagent in the background or another hook asking to go on, and each was settled on the side of not removing.

The same version added `shikii wiki withdraw`, with which a tool such as chiten takes back the articles it made from its own work. At first it deleted them. But the kind of a source is written freely by whoever saves it, so articles made from a person's words could be withdrawn too. So a withdrawn article is now kept, and the person checks it on the answer page and chooses to restore or delete it.

### In 0.42.0, the agent goes through its accumulated memory once a day

Claude Code has a feature that reorganizes accumulated memory in the background. shikii already had something close. Each request's reflection turns corrections into articles, and when the way articles are written changes, existing articles are reviewed a little at a time. But nothing decided when to review. Corrections that a reflection missed were never read again later either.

So shikii now asks the agent to review at the start of a session, when 24 hours have passed since the last notice and at least 5 pieces of work have ended since then. The 24 hours and the 5 are the same as Claude Code's defaults. For the name we chose `shikii nap`, the memory nap: it is friendly, and it is not the name of Claude Code's feature.

shikii only lists what to review; it never edits an article. The first version set the range of conversations to reread by the time of the notice. So when the agent got a notice and did not review, the conversations of that time fell out of the next range. Now shikii remembers how far `shikii nap` actually showed them, and the next nap carries on from there. Conversations come oldest first, 64 at a time.

Two changes keep it light without losing accuracy. First, each conversation names the articles that cite it as a source. The agent only checks the conversations that already became articles, and reads the others most closely. Second, when the files an article is based on have changed, that article is shown first. The agent's replies were cut at 400 bytes from the start, and we also measured cutting them shorter. But most replies are under 200 bytes, and cut, a person's reply no longer shows what it corrects, so we did not do it.

With the real Claude Code, the agent that got the notice ran `shikii nap` in a background subagent and added one procedure that no article held yet. It did not rewrite a correction that already was an article. Codex reviewed the same way when the request asked for work, but finished without reviewing when the request was only "that's all for today". OpenCode could not be checked: the model provider's usage limit was reached.

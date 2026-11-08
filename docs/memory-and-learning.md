**English** · [日本語](memory-and-learning.ja.md)

# How shikii relates to memory

**Remembering earlier work is common ground. shikii connects that memory to using it at work, reviewing whether it helped, correcting it when needed, and adjusting which decisions to delegate based on people's answers.** Its aim is to reduce repeated explanations and corrections as the project develops its own ways of working.

From 0.23 onward, this includes receiving peers' knowledge changes, carrying conditions through research, implementation and audit, selecting experience relevant to the current work, and returning actual use and results to later procedures. The record preserves checked conditions, what remains unknown and how the knowledge informed a decision.

From 0.26, brief judgments and checked results stay linked to the current evidence, and the ending hook verifies their required saves. From 0.27, expected observations and actual checks can inform the next judgment, with matched comparisons and adjustable source reading.

From 0.28, measured knowledge delivery and checks for missing conditions can inform how deeply the next task reads. Comparisons include later source recovery, and a reading choice checked on separate tasks can become the default when the conditions match.

Suppose you say, “When updating the guide, update both the English and Japanese versions.” Remembering that instruction alone does not tell you whether both were updated next time. shikii keeps it as an article with sources and delivers it during relevant work. The agent reviews whether it helped. A check enabled by a person can verify the parts that can be checked mechanically.

## 0.29: improve investigation procedures

Version 0.29 closes a bounded procedure-improvement loop:
observed repeated investigations generate a concise-guidance candidate; separate
tasks supply actual quality checks and measured guidance cost; supported candidates
are adopted, and a regression restores the previous procedure. After a failed
recipe, a different recipe requires more validation. This changes project procedure
selection, without retraining a model or permitting arbitrary code changes.

The agent can return an optional `result.sufficient` and reuse the evidence already
sent with its one ending report. Unknown quality stays unknown. Candidates cannot
edit the required checks, original evidence, evaluation rules or permissions.
Repeated proof under another name cannot establish another validation task. The
source-backed execution and evidence bindings remain available after session expiry.
The procedure journal has fixed exploration and storage bounds. See the
[recording contract](../skills/shikii/references/recording.md#automatic-improvement-of-loop-procedures).

## Where it overlaps with OpenAI memory

OpenAI describes memories as carrying context from earlier chats into future work, including preferences, recurring workflows and project conventions. ChatGPT memory and local Codex memory have separate stores and controls. Local Codex memory also includes summaries and supporting evidence from prior chats. [OpenAI's memory documentation](https://learn.chatgpt.com/docs/customization/memories), [personalization documentation](https://learn.chatgpt.com/docs/personalize)

So “OpenAI remembers only preferences” or “only shikii stores memory locally” would be inaccurate distinctions. This comparison concerns the published description of memory features checked on November 7, 2026, rather than the full instruction, tool and permission systems in OpenAI products.

| What you need | What memory provides | What shikii combines |
| --- | --- | --- |
| Carry earlier work forward | Recall past context | Project knowledge with conditions, reasons and sources |
| Use it when needed | Context for future chats and tasks | Relevant articles before requests and just before file operations |
| Maintain memory | Controls for managing memories | Review evidence and assessments, then correct a passage while preserving the rest |
| Investigate missing context | Refer to earlier context | Inspect recorded retrieval, suppression and capacity reasons from the actual attempt |
| Try a correction | Use context in later work | Freeze past requests and article revisions, then compare retrieval candidates before saving |
| Add knowledge during work | Refer to past context | Connect further searches and consultations to the same work record, retaining read revisions and quotation sources |
| Use experience in the next procedure | Carry earlier work's context forward | Select at most two outcome candidates per procedure using terms from the current request and conditions; inspect earlier failures, sources and revision differences |
| Learn together across agents | Carry earlier context into later work | Deliver peer changes, compare sourced conditions, revisit questions and preview concurrent edits |
| Carry work across roles and stages | Carry context into later work | An optional host supplies assignment conditions, inspected revisions, investigation coverage and implementation state |
| Return use and results to knowledge | Carry current context into later work | Distinguish inspection, actual decision use and execution, then return sourced reports through a compatible chiten build, CLI or MCP |
| Delegate decisions | Answers informed by context | People's answers and observable outcomes to measure where the AI can decide alone |
| Review judgments during work | Context from earlier work | Link a starting judgment and checked ending result to current evidence; resolve failed saves before completion |
| Choose how deeply the next task reads | Refer to relevant earlier context | Compare measured delivery and checked results; matching comparisons on two distinct tasks can change the next default |
| Return rereads and omissions to later work | Carry current context forward | Include source recovery in the measurement, suspend a choice after an omission and select a verified alternative |
| Check required work | Separate management of mandatory guidance | Enabled checks and verification that required records were saved |

OpenAI also recommends keeping mandatory team guidance in `AGENTS.md` or checked-in documentation rather than relying on memory alone. [OpenAI's memory documentation](https://learn.chatgpt.com/docs/customization/memories)

## How experience returns to the next task

1. **Keep it.** Goals, selected approaches, corrections and verified results live in the project's `.shikii/`. Articles retain their sources and the conditions where they apply, so their reasoning can be checked later.
2. **Deliver it.** Relevant knowledge reaches the agent before work and related operations. Linked decision criteria can be read too. Claude Code, Codex and OpenCode connected to the same project can share it.
3. **Review it.** The agent records how a delivered memory affected its actions. Delivery alone, or a successful task, is not counted as evidence that memory helped.
4. **Maintain it.** Unused or harmful articles prompt a review against their sources. Repeatedly unused articles are withheld from automatic delivery under the same conditions. People can also edit or delete articles on the Knowledge page.
5. **Adjust delegation.** For recurring decisions, shikii compares AI predictions with people's answers or observable outcomes. With enough records and adequate quality, it sets the ranges where the AI may decide alone and where it should ask a person.

These two loops—**using and maintaining knowledge, and measuring decisions to adjust delegation**—are central to shikii. What grows is the project's knowledge and decision criteria. It does not retrain the underlying AI's weights.

## In 0.17, a missed article can lead to a precise correction

Suppose an article says to update both language versions of a guide, but it did not arrive during an update. The agent can improve the knowledge in three steps.

1. **Find the reason.** Inspect the record from that moment: did the article fail to match, arrive already, or get excluded by prior assessments or the decision model? Today's search result is not substituted for the past.
2. **Correct the relevant passage.** If the condition did not describe the actual work, propose a source-backed text edit with a reason. An intervening edit causes a conflict instead of overwriting other sections or history.
3. **Try it before saving.** Compare retrieval candidates in past situations, including whether the change displaces another article. Later additions to the wiki stay out of the old comparison.

This lets the agent investigate missing knowledge before asking the person to explain it again. The person does not have to prepare examples or score the comparisons.

The comparison establishes **changes to retrieval candidates**. It does not rerun suppression or the decision model, or guarantee delivery or usefulness. It saves no article, assessment or human truth. Diagnostic history retains at most 32 attempts / 4 MiB; missing or incomplete history is explicitly unavailable for comparison. Whether the knowledge actually helps remains a separate observation from later work.

## In 0.18–0.20, use knowledge during work and return results to the next task

Memory available at the start of a task does not by itself establish how newly discovered conditions or failed attempts will inform later work. shikii connects using prior knowledge and recording the current experience through the same work record.

1. **Consult the proposed approach.** Read prior reasoning, rejected alternatives and applicability conditions, then consider whether they fit the current proposal. For recurring work, assemble a procedure with sources and the current conditions. Consultation and reuse were implemented in 0.18 and shipped with 0.19.
2. **Read the relevant original text.** In 0.19, the agent can read the request, conversation and knowledge together, then save judgments and observations with sources and article revisions retained. Human and AI messages stay distinct, and incomplete material identifies what is missing. In 0.20, even a long article can supply relevant exact passages with their revision and position; the full article can be fetched when a condition or exception remains unresolved.
3. **Add what emerges during work.** In 0.20, further searches, consultations and procedure reuse can join the same work record. Earlier articles and quotation sources remain available while the agent records discoveries that actually informed a decision. If part of a save fails, it can retry without duplicating the saved items.
4. **Return execution results to later work.** Link a used procedure to its execution conditions, actions, failures, corrections and reported results, with sources such as validation records. Later reuse can compare those past results with current conditions. If the procedure article has changed, it also flags that its revision differs from the one used previously.

For example, a previous release may have validation showing that knowledge and sources survived an update. The next release can read those conditions and results alongside the procedure to decide what to verify now. A different OS or starting version calls for checking those differences. A past success does not establish success for the current task.

These execution reports are evidence for the agent to assess. Saving a report does not create a success truth or a person's answer, and stays separate from measurements of judgment accuracy or long-term improvement.

## In 0.21, connect current guidance and counterexamples to procedure improvements

The agent reads original evidence and assigns roles to exact quotations, separating current conditions from earlier context. Unclassified text and changed premises stay explicit. Consultation searches interpreted counterexamples and failure reports independently, preserving the conditions and sources to compare with the current proposal. Recording a corrected failure returns current text and a proposed correction for procedure review immediately, rather than waiting for later reuse. The agent checks the evidence before updating knowledge; no extra person scoring is needed. Interpretations and reviews remain hypotheses: dates alone never establish validity, and recorded results never become automatic success truths.

## What has actually been checked?

An agent's review is a provisional assessment, stored separately from human truths and measured improvements. Reading more memory does not by itself establish that work got better.

Checks have a defined scope too. A program can check whether both language versions changed, but that does not guarantee that either is easy to understand. In 0.14.1, missing required planning or reflection records produce concrete recovery steps. shikii checks whether saving progressed. No progress or a verification error ends automatic continuation with the work explicitly incomplete.

Neither knowledge nor an AI decision grants permission. The current request and the host agent's permission rules take priority.

## In 0.22: connect, check and grow project practices

An insight can connect exact passages and conditions from distinct articles and keep a small check with its reported results. Useful methods become sourced, conditional project practices that a later agent can receive, use and revise. Original evidence and revisions remain available, and results never become human truths.

When knowledge grows too long, preview a split or merge and save destinations before editing their sources. The aim goes beyond shared reading to later use, verification and improvement. See the [guide](guide.md) for operations and limits.

## In 0.23: build collective knowledge through actual work

A shared memory store gives agents access to earlier context. shikii also connects how one agent's discovery reaches another, gets checked under new conditions, and returns to the same project knowledge.

1. **Receive a peer's changes.** Compare the article revision actually supplied to the work with its current revision, including changed premises and sources. Confirming inspection records what was read; usefulness needs separate evidence.
2. **Compare the evidence.** Read explicit support, counterexamples and differing procedure reports with their exact conditions and sources. Shared sources remain visible. Different source IDs do not establish independent corroboration, and a majority does not establish truth.
3. **Carry the question forward.** A saved probe can appear in another relevant task. The agent chooses a small check within the authorized work and adds the observed result to the same question, retaining earlier observations and their conditions.
4. **Improve knowledge together.** Compare the original article, the current article and the proposed edit without saving. Nonoverlapping changes can produce a combined proposal. Conflicts remain explicit; saving still checks the current revision so a later edit cannot be silently overwritten.

For example, an agent writing a CSV tool notices a delimiter condition and records a question. Another agent checks it on separate synthetic inputs, records the cases that matched and the assumptions still unresolved, and preserves the first agent's observations. A later task can read both the changed article and those limits. The knowledge grows through different work, rather than treating repeated wording as corroboration.

This is a workflow for collective knowledge, not a claim of measured long-term improvement. Inspection, use, reported observations and human answers remain distinct. No model weights change, and a saved question grants no permission to run an experiment. See the [workflow and limits](guide.md#share-changes-and-compare-concurrent-work-0230).

## In 0.24, carry knowledge through an assignment's stages

An optional host integration gives research, planning, implementation and audit their own assignment context. A later agent can compare earlier coverage, unknowns and inspected revisions with current conditions. Implementation reports retain their code revision and integration state. Disagreements can return as a small distinguishing question and method, while checked failures can support a correction to the same procedure.

For example, a researcher records that small input passed and large input remains unchecked. The implementation agent sees that limit, reports its observed revision, and an auditor can add a contrary result and a bounded comparison method. A subsequent assignment receives those conditions and changes rather than starting from an isolated summary. Shikii retains the evidence; the host chooses what to run and integrate. Existing users need no external host or assignment input. See the [integration contract](guide.md#connect-external-assignments).

This version adds the shared API for external hosts. Carrying assignment conditions and implementation state is distinct from the chiten reports of actual use and execution described under 0.25 below. Assignment context selects relevant evidence within the same project; it is not an access-control boundary.

## In 0.25, select experience for current conditions and return use and results

Remembering a procedure does not establish the same outcome under a different OS, input format or implementation revision. Instead of taking only the latest reports, 0.25 selects candidates by comparing terms from the current request and conditions with earlier execution conditions, then with actions, failures and corrections. It returns at most two per procedure, uses recency to break ties and can retain an earlier failure with the same positive condition-term score as the top candidate.

Retrieval rank does not establish that the actual conditions agree. The agent reads the original conditions, sources and unknowns, and rechecks a changed article revision. Attaching a report or validation source does not establish success for the current task either.

A compatible chiten integration can return assignment reports to shikii while distinguishing an inspected article, knowledge that changed a decision and a procedure actually executed. Actual use records its contribution and evidence; execution reports keep the current conditions, action, failure, correction, result and sources. Saving a report does not automatically establish usefulness, success or a human truth.

For example, suppose a later worker applies a CSV procedure checked on Linux to quoted input on macOS. If the candidates include a Linux result and an earlier failure to handle quoting, the worker reads their conditions to decide what needs checking now. Only after actually trying the procedure does the worker report this input, environment and observed result. Planning alone produces no execution outcome. The next worker can read that report and its sources with the procedure, then check another condition.

chiten is a separately distributed, optional integration. Projects without it can record knowledge use and procedure outcomes through CLI, MCP or HTTP and use the same condition-based reuse. The comparison with shared memory concerns the workflow of selecting conditional experience and returning use and observations to their sources, alongside recalling earlier context. It does not claim measured long-term reductions in work.

## In 0.25.1, align skill and MCP guidance with these distinctions

0.25.1 is a patch that aligns the skill and MCP instructions with the existing behavior. It distinguishes human requests and decisions from agents' observations and checks as wiki sources. It explains that an earlier report must not be copied as the current execution, and reading or planning alone produces no execution outcome. MCP preparation uses one search, consult or reuse mode per call; field descriptions separate agreement and open choices from unverified execution results. Storage formats and runtime operations remain unchanged.

Update the binary and installed skills to load the revised guidance. Without hooks, rerun `shikii --init` in an existing initialized project after updating the binary; this refreshes skills while preserving project data and leaving hooks disabled. Then reopen agent sessions and restart the page and MCP. See the [update instructions](guide.md).

## In 0.26, review judgments during work and verify their saved results

Alongside carrying knowledge into the next task, 0.26 requires a brief starting judgment and a checked ending review for the current task. The request hook keeps the turn obligation, and the agent links its judgments to the request, article revisions and captured observations. It updates the judgment when the premise or next action changes.

For example, an earlier CSV procedure may require preserving ownership. The agent checks that condition at the start. If quoted input reveals a different problem, it revises what to check next. At the end it saves what was verified and which conditions remain unknown, with sources available to later work. Retaining the approach is valid; a new discovery or article is not required each time.

The ending hook verifies the required saves. Another successful save or a fresh preparation cannot resolve a failed correction or capture. Replaying an old finish cannot certify later evidence, and changed articles require a fresh revision check. Recovery retries continue as required saves decrease; a retry with no progress ends explicitly incomplete.

This adds review and save verification tied to the current conditions. It does not request a transcript of private deliberation or establish judgment quality or long-term improvement. Whether a later task avoids a missed condition remains a separate observation. Model weights stay unchanged, with no additional model or person scoring. See the [inputs and recovery workflow](../skills/shikii/references/recording.md#required-review-while-doing-the-work).

## In 0.27, test expectations and choose the next check

- Record the expected observation, conditions, method and decision before an authorized check. Link the actual reported result to the same immutable trial, then cite challenged results in an updated judgment.
- Select a small unresolved check with current evidence. Compare two saved executions of the same case, question, conditions, method and expected outcome using paired measurements.
- Adjust source-reading depth while keeping required reviews and exact-revision recovery. Unresolved judgments, pending saves and changed premises deepen the read.
- Check enabled guards before supported publication calls observed by Claude Code's Bash hook. Codex retains turn-end checks; OpenCode uses MCP and skills without automatic hooks.
- Keep judgments and failure explanations in concise bullets, preserving conditions and sources. Reports and paired values remain agent evidence, with no automatic human truth or model-weight change.

The aim is for an observation to change what the agent checks next, alongside carrying context forward. A saved plan does not run an experiment; matched values need interpretation. See the [workflow and limits](../skills/shikii/references/recording.md#expectations-execution-and-the-next-check-0270).

## In 0.28, learn how much knowledge to read

Alongside carrying earlier knowledge forward, shikii uses measured delivery and source-backed checks to choose **how much of that knowledge the current work needs**. It feeds experience from separate tasks into the reading depths introduced in 0.27.

1. **Measure actual delivery.** Record the UTF-8 JSON bytes of supplied knowledge, read counts and full-source recoveries. Include later source reads instead of comparing only the first excerpts.
2. **Compare the same task.** Compare the standard `focused` read with another depth, keeping article revisions, topic, host stage and quality check matched. Report whether the required conditions were recovered, with evidence. A deeper read that recovers conditions missed by focused can also become a candidate.
3. **Check another task.** One favorable comparison creates a candidate. Matching comparisons on two distinct tasks, with separate work and non-overlapping reported results, can change the next default under the same conditions. Renaming source IDs, changing provenance, reordering JSON keys or respelling the same JSON number does not add support.
4. **Return omissions.** A reported missing condition suspends that reading choice. A later success alone does not erase it; use a verified alternative or a deeper check. A changed article revision, topic or stage does not inherit the old preference.

For example, consider a long procedure for preserving data during an update. If a short excerpt omits a recovery condition and the agent then reads the full source, that recovery counts too. A smaller first delivery alone does not establish an improvement. When separate tasks support a reading choice that recovers the required conditions with less delivery than focused, the next worker can use it under matching conditions. A new omission suspends the choice and calls for rechecking.

Explicit depth, deeper reading for unresolved conditions or changed premises, mandatory reviews and exact-revision source recovery take priority. Quality feedback is optional. chiten 0.9.3 or later can return it with ordinary consultation reports; projects without chiten can use the same CLI, MCP or HTTP workflow.

What adapts is the project's knowledge-delivery method. Model weights and decision permissions stay unchanged. The measurements cover knowledge delivery and reading, not a general reduction in model tokens, calls or response time, or an improvement in answer quality. Two tasks are a fixed adoption condition, not proof of statistical independence. See [the inputs and limits](../skills/shikii/references/recording.md#learn-a-reading-preference-from-checked-tasks).

## The work before 0.14.1 matters too

| Version | What it added |
| --- | --- |
| 0.7.3 | Turn corrections into checks a person can enable, and record memory effects. |
| 0.8 | Check that preparation and reflection were saved. |
| 0.10 | Measure whether the AI can distinguish answers before delegating decisions. |
| 0.11 | Consult relevant knowledge during work and just before file operations. |
| 0.12 | Feed assessments of memory usefulness back into delivery. |
| 0.13 | Use assessments to prompt reviews of articles and their sources. |
| 0.14 | Read linked decision criteria and article bodies, beyond procedural summaries. |
| 0.14.1 | Recover missing required work and verify progress before judging completion. |
| 0.15 | Edit or delete articles on the Knowledge page, retaining history and sources. |
| 0.16 | Feed evidenced manual discoveries into retrieval, carry rationale and changed premises, and combine planning with reflection. |
| 0.17 | Connect precise article edits, recorded delivery reasons and comparison against past situations. |
| 0.18 (shipped with 0.19) | Consult project knowledge about a proposal and assemble sourced procedures for current conditions. |
| 0.19 | Combine reading requests, conversations and knowledge with saving judgments, observations and articles; retain sources and revisions and retry failed items. |
| 0.20 | Connect focused original passages, knowledge added during work and sourced procedure results to the same task and later reuse. |
| 0.21 | Separate current knowledge from earlier context, independently retrieve counterexample candidates, and review procedures from recorded failures and corrections. |
| 0.22 | Connect experiences, keep probes and observations, reuse project practices and preview knowledge splits or merges. |
| 0.23 | Share peer changes, contrast evidence under its conditions, revisit saved probes and reconcile concurrent article edits before saving. |
| 0.24 | Carry conditions, coverage, read revisions and implementation state across stages through an optional assignment API, retaining the link between reports and their work. |
| 0.25 | Select outcomes relevant to current conditions and return actual use and execution reports through compatible chiten reports, CLI or MCP. |
| 0.25.1 | Clarify sources, inspection versus use and execution, candidate interpretation, MCP fields and updates without hooks; preserve storage formats. |
| 0.26 | Require evidence-linked starting judgments and ending reviews, preserving unresolved saves across retries and preparations. |
| 0.27 | Link expectations and observations to the next check, compare matched executions, adjust reading depth and check supported publication calls. |
| 0.28 | Compare reading depths using measured delivery and quality reports; return support from two distinct tasks or an omission to the next matching default. |

See the [changelog](../CHANGELOG.md) for version details and the [guide](guide.md) for operations.

### Memory-policy upgrades

A memory-policy release schedules a review of existing project knowledge by the
installed agent. Upgrading memory… shows durable progress; batches preserve
original sources, explicit preferences and human history while correcting obsolete
procedures and consolidating duplicates. Reading is not a completed review. Missing
sources stay unresolved, and a completed policy epoch is not restarted by routine
patch updates. Ordinary work separately needs only prepare and one ending report;
recording itself does not establish learning or improvement.

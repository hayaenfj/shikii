**English** · [日本語](memory-and-learning.ja.md)

# How shikii relates to memory

**Remembering earlier work is common ground. shikii connects that memory to using it at work, reviewing whether it helped, correcting it when needed, and adjusting which decisions to delegate based on people's answers.** Its aim is to reduce repeated explanations and corrections as the project develops its own ways of working.

Suppose you say, “When updating the guide, update both the English and Japanese versions.” Remembering that instruction alone does not tell you whether both were updated next time. shikii keeps it as an article with sources and delivers it during relevant work. The agent reviews whether it helped. A check enabled by a person can verify the parts that can be checked mechanically.

## Where it overlaps with OpenAI memory

OpenAI describes memories as carrying context from earlier chats into future work, including preferences, recurring workflows and project conventions. ChatGPT memory and local Codex memory have separate stores and controls. Local Codex memory also includes summaries and supporting evidence from prior chats. [OpenAI's memory documentation](https://learn.chatgpt.com/docs/customization/memories), [personalization documentation](https://learn.chatgpt.com/docs/personalize)

So “OpenAI remembers only preferences” or “only shikii stores memory locally” would be inaccurate distinctions. This comparison concerns the published description of memory features checked on October 31, 2026, rather than the full instruction, tool and permission systems in OpenAI products.

| What you need | What memory provides | What shikii combines |
| --- | --- | --- |
| Carry earlier work forward | Recall past context | Project knowledge with conditions, reasons and sources |
| Use it when needed | Context for future chats and tasks | Relevant articles before requests and just before file operations |
| Maintain memory | Controls for managing memories | Review evidence and assessments, then correct a passage while preserving the rest |
| Investigate missing context | Refer to earlier context | Inspect recorded retrieval, suppression and capacity reasons from the actual attempt |
| Try a correction | Use context in later work | Freeze past requests and article revisions, then compare retrieval candidates before saving |
| Delegate decisions | Answers informed by context | People's answers and observable outcomes to measure where the AI can decide alone |
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

## What has actually been checked?

An agent's review is a provisional assessment, stored separately from human truths and measured improvements. Reading more memory does not by itself establish that work got better.

Checks have a defined scope too. A program can check whether both language versions changed, but that does not guarantee that either is easy to understand. In 0.14.1, missing required planning or reflection records produce concrete recovery steps. shikii checks whether saving progressed. No progress or a verification error ends automatic continuation with the work explicitly incomplete.

Neither knowledge nor an AI decision grants permission. The current request and the host agent's permission rules take priority.

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

See the [changelog](../CHANGELOG.md) for version details and the [guide](guide.md) for operations.

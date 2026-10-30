**English** · [日本語](memory-and-learning.ja.md)

# How shikii relates to memory

**Remembering earlier work is common ground. shikii connects that memory to using it at work, reviewing whether it helped, and adjusting which decisions to delegate based on people's answers.** Its aim is to reduce repeated explanations and corrections as the project develops its own ways of working.

Suppose you say, “When updating the guide, update both the English and Japanese versions.” Remembering that instruction alone does not tell you whether both were updated next time. shikii keeps it as an article with sources and delivers it during relevant work. The agent reviews whether it helped. A check enabled by a person can verify the parts that can be checked mechanically.

## Where it overlaps with OpenAI memory

OpenAI describes memories as carrying context from earlier chats into future work, including preferences, recurring workflows and project conventions. ChatGPT memory and local Codex memory have separate stores and controls. Local Codex memory also includes summaries and supporting evidence from prior chats. [OpenAI's memory documentation](https://learn.chatgpt.com/docs/customization/memories), [personalization documentation](https://learn.chatgpt.com/docs/personalize)

So “OpenAI remembers only preferences” or “only shikii stores memory locally” would be inaccurate distinctions. This comparison concerns the published description of memory features, rather than the full instruction, tool and permission systems in OpenAI products.

| What you need | What memory provides | What shikii combines |
| --- | --- | --- |
| Carry earlier work forward | Recall past context | Project knowledge with conditions, reasons and sources |
| Use it when needed | Context for future chats and tasks | Relevant articles before requests and just before file operations |
| Maintain memory | Controls for managing memories | Reviews of helpful, unused or harmful memories that inform delivery and article maintenance |
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

See the [changelog](../CHANGELOG.md) for version details and the [guide](guide.md) for operations.

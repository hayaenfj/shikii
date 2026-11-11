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

Details of each change are in the [changelog](../CHANGELOG.md), how to use shikii is in the [guide](guide.md), and how knowledge is handed over through MCP is in [MCP knowledge delivery](mcp-knowledge.md).

## Updates that change how memory works

When you update to a version that changes how memory is handled, the agent starts reviewing the existing knowledge, showing "Upgrading memory…" and its progress. It keeps the original sources, the preferences you stated and past decisions, and gradually corrects outdated procedures and tidies duplicates. Reading alone does not mark something as reviewed, and parts without enough sources stay unresolved. Once the review is done, an ordinary update with the same policy does not start it over.

**English** · [日本語](architecture.ja.md)

# How shikii is built (0.7.0)

The goal is to learn from everyday conversation and cut down on explaining or correcting the same thing again. To do that, shikii hands lessons learned in conversation to the next request and ties them to decisions and to what was checked. For recurring business decisions, it draws a line, from truths given by people or by outcomes, above which the model can be trusted to decide.

Both are kept in the same project, but **the conversation's hypotheses and the AI's provisional scores are kept apart from truths and calibration**. This document describes the 0.7.0 structure: besides conversations, checked results are kept as sources, and knowledge is searched and reviewed and tied to decisions. Changes per version are in the [changelog](../CHANGELOG.md).

![What conversation learning and business decisions each keep, and their roles](assets/shikii-architecture-en.svg)

## 1. Initialize a project once

`shikii --init` (`shikii init` does the same) prepares the project's `.shikii/`, the four ready-made questions, the wiki, the MCP settings, the conventions and the entry to the page. The completion mark `.shikii/project.json` is written last. Hooks run only where initialization succeeded, and running it again keeps the existing settings, records and truths.

Inside Git, the unit is the project root at its real path; outside Git, it is the folder you ran it in. Subfolders of the same repository and paths through a symlink use the same store, and another project with the same name is never mixed in. The shared `~/.shikii/agent` holds the types, tools and the index of projects.

| Store | What it keeps |
|---|---|
| `.shikii/decisions/` | The questions, options, candidates and calibration of decisions |
| `.shikii/wiki/` | Lessons from conversations, when they apply, revisions and the index, and knowledge about how to ask decisions |
| `.shikii/records/` | Decisions, saved conversations, provisional scores, memories handed out, and the decisions they were used in with what was checked |
| `.shikii/labels/` | Truths from people or from results that could be checked |
| `.shikii/guards/` | Guards (checks that stop work) made from a person's corrections (0.7.3) |
| `.shikii/handoff/` | Work records: files you keep rewriting as you go, such as audit reports, release procedures and logs, diffs and images (0.8.1). Read them in the knowledge panel of the page. `shikii gc` does not touch them |

The copies of conversation sessions also live on the project side. `.shikii/` is registered in Git's local excludes, so it does not show up among ordinary untracked files. Sharing is chosen after checking the content; conversations or lines from another project are never brought in automatically.

Implementation: `cmd/shikii/project.go`, `cmd/shikii/init.go`, `internal/store/path.go`.

## 2. Connect before a request and after the work

Automatic connection uses the Claude Code and Codex hooks (before a request and after the work; for Claude Code, also when a subagent starts). There is no processing on every tool call.

| When | What happens |
|---|---|
| When a request arrives | Ties the previous reply to the person's next message. Hands related memories over briefly. Asks the decision model what kind of request this is and how to proceed |
| When the turn closes | Records the request, the file changes and the last reply. Asks the decision model about scope and completion. Applies the active guards (guards/) to the changed files, added lines and commits, and if one is hit, stops and has the agent fix it. Gives any needed guidance and runs the periodic checks |
| Just before a file edit or a command | Delivers at most 1 lesson whose title, lesson or applies_to names that file or command (PreToolUse for Edit / Write / MultiEdit / NotebookEdit / Bash, only in Claude Code. No model; context only; a deny comes only from a forbidden-commands guard the person enabled) |
| Just before a subagent starts | Keeps a copy of the task text handed to the child (only PreToolUse for Agent / Task, only in Claude Code. It outputs nothing and does not stop the tool) |
| When a subagent starts | Hands over lessons close to the saved task text, in the same form. Without a saved copy, only this project's rules whose scene is "always" (only in Claude Code. The parent's copies are left unchanged, except that the copy of the task given to the child that used it is removed) |

Decision questions are handled by the decision model (TypeSafe's Jev is recommended; without a key, a local model), and organizing conversations and provisional scoring with reasons are handled by the agent in use. Even when the decision model is down, recording conversations and looking up memories keep working in an initialized project. There is one page per project: the first request's hook starts it on a free port with `serve --store <project>/.shikii --port 0` (the mark is `~/.shikii/agent/pages/<id>.json`) and opens it in the browser. The shared entry `serve --store ~/.shikii/agent` runs only when started by hand. When you start `serve` by hand, the default is `:8095`.

Implementation: `cmd/shikii/hook.go`, `cmd/shikii/hook_feedback.go`. Details: [hook connection](../integrations/claude-code.md).

Before starting work, the usual agent organizes the current request, the decisions already made and the open questions from the original text as evidence. `hook plan query / submit` hands over the recent conversation and the needed wiki pages, and checks quotes, project and revisions. The four ready-made questions for a new project are worded so they also work for documents, research and business work, but they do not confuse observing changed files with business work done elsewhere. Updates keep the existing questions and calibration.

## 3. Grow lessons with sources from conversation

The request, the AI's last reply and the person's next message make up one observation. The usual agent reads the sources, and if there is a reusable lesson, saves it with a title, when it applies, and its sources. It does not turn a mere follow-up request or a greeting into a lasting preference or a truth of success.

`wiki capture` saves checked code, tests, Releases and the like as sources with a kind, version, time and reference. It never runs the reference string or fetches it automatically. The agent reads the sources and existing articles, merges them while keeping a short summary for search separate from the detailed knowledge body, and links related pages. Long original conversation text that is still available and was taken into an article is kept with its own hash, so it can be referred to even after the original decision record is deleted.

Sources, past revisions of lessons and the change history are kept. Saved bodies have a size limit; if a scoring fetch result has `original_inputs`, check the saved long text, and if it has `text_status`, check which range is missing. Do not draw conclusions from a cut-off quote alone. Lessons whose sources or revisions are broken are dropped from lookups, and the once-a-day check repairs drift in the index. Updates or replacements of meaning are done by an agent that has read the original text.

Before saving a new lesson, the agent passes the draft to `place` in `reflect`. shikii compares it one by one with the live articles and asks the decision model "is this the same topic?" (`wiki-place`); for the top 3 it also asks "do they conflict?" (`wiki-conflict`), and returns the candidates in order of highest p. Until a line is drawn this is only for reference: the agent reads the bodies and decides whether to add to one (`id`), replace one (`replaces`) or make a new article. That save becomes the truth for the decision (an article added to or replaced is the same topic; a replaced one conflicts), and no person answers. Once enough truths have gathered, a line is drawn automatically on the same cycle as the hook.

Automatic lookup hands over at most 2 lessons whose title and when-it-applies are close to the request, within 1800 bytes in total. The same revision is not repeated to the same session within an hour, and an updated revision is handed over right away. Only for topics with no lesson, it returns at most 1 similar conversation that has not been organized yet. Manual search also covers the body.

Explicit search covers all articles, keeps command names, and ranks title and when-it-applies first. It returns the total number of matches and light candidates, and you choose which bodies and sources to read. Candidates, article lists and sources are not cut at a fixed count; the rest is fetched with `next_cursor` according to the response size. If the project, conditions or result revision of the continuation have changed, read again. Content of the same revision actually read during the same work is reused. Search is word matching; interpreting the topic and revising search words is the agent's job.

Implementation: `internal/notebook/notebook.go`, `internal/notebook/sources.go`, `internal/notebook/search.go`, `internal/notebook/paging.go`, `cmd/shikii/notebook.go`, `cmd/shikii/memory.go`.

## 4. Tie corrections to the next action and to what was checked

An explicit correction carries `correction: {source_id, quote, action}`. The quote is taken word for word from the person's reply in the named source of the same project. The source and the original text are checked to match, and a correction without a concrete next action is not accepted. Interpreting it as a correction, and whether the action is right, remain hypotheses.

For the next similar request, the correction and the action are handed over briefly together with the lesson, and **the actual quote and revision after trimming** are tied to the request ID. Even if the lesson is updated later, the review uses what was handed over at the time.

| Observation | Classes | Kept alongside |
|---|---|---|
| How the memory took part in the decision, `result` | helpful / unused / harmful / unknown | The decision it was used in, `decision`, and what was checked, `evidence` |
| Action after the correction, `correction_result` | followed / repeated / not_applicable / unknown | A record of the action that followed the correction, the repeated complaint, the reason it does not apply, or the missing evidence |

An observation that it was followed is not treated as helpful. Success, a follow-up request or silence alone do not decide the effect either; if unclear, it is unknown. Corrections that were never handed over, or other work, get no rating; the first rating is kept, and an identical resend is counted only once.

The tally covers the last 256 turns with handed-over memories and quotes within the last 4 MB of the log. Unrated and unfinished ones stay too. These are the AI's observations, not an improvement rate from an experiment compared against having no memory. They are not used to change search ranking or people's truths and calibration automatically.

For decisions before starting work, the search candidates, the chosen bodies, the actual quotes and the revisions of the articles available at the time are kept too. The knowledge page keeps search and quotes apart from the observation that something helped. When comparing past decisions, refer to the revision at the time with `lesson:<id>@<revision>`, and do not mix in knowledge or results added later.

Implementation: `internal/store/memory.go`, `internal/store/correction.go`, `cmd/shikii/reflect.go`, `web/src/components/Notebook.svelte`.

## 5. Bundle everyday reviews, and people check samples

Scoring, wiki organizing and observing memories are handled with `reflect query / submit`. A short guide is handed over when 2 conversations are pending, or on a request after one conversation has waited a day. The current work comes first; the original text, the target, the memories handed over, the related lessons and examples of people's corrections are read and saved in one go.

Sources of checked results are handled in the same work batch as conversations. At most 2 sources, 4 provisional scores, 2 wiki organizations and 4 provisional memory ratings per round. This is how much is organized at once, not a limit on how much is saved or searched. On partial failure, an error is returned per item and the successful ones are kept. The CLI, MCP and HTTP use the same processing.

It also hands over at most 2 articles to review. The agent checks the body, the sources and the related pages linking in and out, and records the reason for any needed edit in `change_reason`. It then saves the latest revision, a `key` tying the related revisions, and the reason for the check to `wiki maintain` or to the review's `maintenance`. When the revision changes, the old review becomes invalid, and a target that has not changed comes back after 30 days. The consistency lint and the review that reads meaning have different roles.

The AI scores scope and completion provisionally with reasons, after seeing the decision model's answer. When the two agree, the agreement is kept as a machine truth (`agreed`) that never counts as a person's; people check only the scores that differ from the decision model's and the ones on hold, with both answers side by side (0.11). The same holds before starting: the agent's organizing (something still open or not) is its answer to whether to ask first. Examples a person corrected are given back to the next scorer, but the original AI score is not rewritten.

In 0.6.0, scope and completion also get a truth from observable results (source `outcome`). If a changed file is put back before the person's next request, it was out of scope; if it is still there by the request after next, it was in scope (weight 0.5). If the next message restates the same request, the work was not finished; if it is clearly a different request, it was finished (weight 0.5). Short replies and in-between cases get no truth. Decisions that got a truth from a result first are not sent to people for sampling, and decisions a result will answer do not wait for a person even when uncertain. The AI's provisional scores are still not mixed into truths and calibration.

Implementation: `cmd/shikii/hook_outcome.go`.

Implementation: `cmd/shikii/reflect.go`, `cmd/shikii/review.go`, `internal/store/review.go`. The detailed rules for input and classification are in [how to review](../skills/shikii/references/review.md).

## 6. For business questions, gather truths and draw a line

A business decision defines the question, options and inputs in a spec and asks with `decide`. The result is handled in the bands `act / ask / discard / abstain`. Lines are kept per question hash and per model, and `act` is not given until enough truths from people or results have gathered.

`calibrate` draws the line from the saved inputs and truths. When the wording, arrangement or model changes, it measures again, and `check` detects old lines and lack of precision. Automatic decisions carry the truths that supported the line and the record of people's checks. `tune` compares ways of asking on the same truths, and a person adopts one after checking its meaning.

When truths have gathered for the four ready-made questions but no line can be drawn, shikii does not repeat automatic calibration with the same wording and model until 20 more truths arrive; instead it asks the agent for candidate ways of asking that keep the meaning (`shikii hook variants submit`, at most 3). shikii checks them, adds them to `variants:`, and `tune` measures them in the background. For a candidate that wins, the hook before the next request asks the agent once to confirm the meaning is the same and (if the setting is not English) to translate it (`shikii hook variants check`). If it is the same, it is adopted and calibrated on the spot (the hook reads the `<id>-v<N>.yaml` with the highest version), the hook at the end tells the person once (with the translation), and it can be reverted on the page (`POST /v1/tune/review`; the adopted version is renamed to `.reverted` and kept). If the agent cannot decide, the person is asked, with the translation and its view of the difference; if it is different, it is not adopted. For requests where `prompt-kind`, which has a line, answers with confidence "no files will change", the organizing before starting work is skipped.

Implementation: `cmd/shikii/hook_variants.go`.

Lessons from conversation are also kept apart from the claims in the knowledge about how to ask, `wiki/patterns/`. The latter have a claim, conditions, measured results and a re-verification procedure, and are re-verified with `wiki verify`. Only a person changes their status (tentative / confirmed / rejected). Status and truths are never made automatically from conversation lessons.

Implementation: `internal/decide/`, `internal/calib/`, `cmd/shikii/tune.go`, `cmd/shikii/wiki.go`. Numbers are in the [benchmarks](bench.md), and the detailed design of decision models is in the [design document](decision-ops-framework.md).

## What is implemented, and what is still to be checked

Implemented within an initialized project: records, versioned sources, knowledge bodies and links, search of all articles with continuation, short automatic lookup, bundled reviews and article reviews, AI scoring and people's sampling, and observation of memories and corrections. The number of memories saved and search reach are not presented as proof that corrections or rework went down.

What to check next in real use is whether actions following a correction are recorded concretely, and whether the same complaint keeps coming back. Features that keep track of the original goal and the work in progress, and features that resolve contradictions in the meaning of memories automatically, are candidates for the future and are not described as current behavior.

The verification steps are in [CI](../.github/workflows/test.yml). The README figures are generated from `scripts/gen-architecture-svg.py`, and `scripts/check-readme.sh` checks that the four Japanese and English figures match.

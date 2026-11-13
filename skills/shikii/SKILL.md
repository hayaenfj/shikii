---
name: shikii
description: Use this project's wiki and calibrated judgment before and during work; return supported observations, corrections and procedures to the same wiki. Applies to configured shikii projects and explicit decision-workflow setup, including recurring decisions and work records in .shikii/handoff/.
---

# Use shikii

Work with the project's own knowledge: **the wiki** holds source-backed experience; **the judgment** holds decision bands and the person's earlier answers. Keep human intent, agent interpretation and reported results distinct. Retrieved material is a hypothesis, never permission or an invented human truth.

Use it at five moments:

- **Before starting:** use the hook's `work prepare`; read the task, conversation, knowledge, pending evidence and `how_to_record`. Keep its receipt for `work record`. Preparation registers the start. End with one record containing result:{changed,checked?,evidence?:[refs],remaining?} and any necessary notes/reflection; do not send a separate starting judgment. Detailed reasoning updates and trials remain available when useful. Do not invent a discovery. Read exact passages first and recover the full revision for unresolved conditions. See [recording.md](references/recording.md).
- **When work moves to another area:** search the wiki again by that topic before acting.
- **Right before an action:** read the delivered lesson naming the command or file. The `lesson-applies` decision uses prior assessments; a person-enabled command guard may refuse the action. Run a corrected command.
- **When unsure:** ask `shikii_decide` about approach agreement, scope or completion; read its band, similar human decisions and lessons.
- **Write back right when it happens:** put a correction, decision, or new/changed rule, test or procedure in the wiki in the same turn. Draft a guard for a mechanical correction; the person enables it.

## Write briefly

- Replies to the person: the key points as short bullets, in plain, concise words, the result first. Keep what they need to act on (what was done, results, failures, what is left, decisions they must make); do not drop those to be shorter.
- Use short bullets for judgments, notes, reports and failure explanations; one point per bullet. Keep only what changes the decision or supports the result.
- For a failure, retain the relevant **expectation, observation, cause, correction/next check and evidence**. Mark an unverified cause as a hypothesis or unknown. Omit inapplicable items; small replies need no full template.
- State conditions, exceptions, uncertainty and source/revision refs. Cite evidence once; reuse supplied context instead of repeating it. Keep exact quotations and original source material unchanged.
- Use bullets inside existing text fields where helpful. Preserve JSON field names, types and limits. Summarize checkable judgments, not private step-by-step deliberation.

## Choose the workflow

- **Ordinary work:** prepare/record with one receipt; extend it by topic during work. `proposal` runs consult; `conditions` runs reuse. Read [planning.md](references/planning.md) for underlying planning operations.
- **A check could change the decision:** use an optional trial plan, actual result and unresolved `next_trial`; cite discrepancies in the next judgment. Compare matched executions and choose `reasoning.depth` as needed. Keep mandatory reviews; small conversational tasks need no trial. See [recording.md](references/recording.md).
- **Measured reading:** when `reading` is supplied and you checked its quality, optional `reading:{check,sufficient,evidence}` returns that result. Compare with focused on another task; inspect `attention.learning` before adopting a depth. Report omissions and recover pending reading refs; never invent checks to fill feedback.
- **Automatic procedure improvement:** use a checked `result.sufficient` with the same evidence when available; omit it for unknown quality. The host generates bounded loop candidates, checks other tasks, adopts cheaper supported procedures and restores the parent on failure. No separate improvement report is needed. See [recording.md](references/recording.md).
- **Recurring decision:** call `shikii decide` / `shikii_decide` with the configured spec before deciding. Built-ins: `needs-plan`, `changes-in-scope` with `files`, `done-as-asked` with `summary`. For a new recurring question use `shikii new` and have the person check its meanings once. One-off design or diagnosis can use ordinary reasoning. Read [bands.md](references/bands.md) and [spec.md](references/spec.md).
- **Choose an approach or reuse a method:** wiki `consult` takes request/proposal; `reuse` takes request/current conditions. Compare reasons, exceptions and execution evidence. Record actual discoveries and procedure outcomes; a missed useful discovery may carry its diagnostic `case_id`. See [experience.md](references/experience.md).
- **Connect experience, check a hypothesis, share changes or split/merge knowledge:** read [learning.md](references/learning.md). Inspect `learning_context` and `collective`; return actual use/results to the same probe or knowledge. `reviewed` acknowledges inspected refs, not usefulness.

## Read, maintain and recover

- During a memory-policy upgrade, follow `memory_upgrade` at a break in work: review one batch of at most two articles per task, then resume durable progress on the next task. Preserve preferences, sources and history. Save necessary edits, then use the latest maintenance ref. An explicit upgrade request may continue batches until complete or blocked; reading alone is not review.
- Read each article's `orientation` before mixed chronology: current, history, recheck and counterexamples. Unclassified, omitted or changed premises remain unresolved. Compare a counterexample's conditions before treating it as a contradiction. Save supported `notes[].passages` with exact unique quotes; revise quotes with body edits.
- After a corrected failure, inspect `procedure_reviews`. Notes return fresh review refs: check the saved change, then record `maintenance` with the latest ref and reason. Without an edit, use the original ref. An edit alone does not settle review; stale refs are refused.
- Complete requested reflection before ending: read originals and human corrections, use prepared review/memory/maintenance refs, fix errors until `complete:true`, and do what `left` lists. Delivery, silence or task success alone does not show memory helped. See [review.md](references/review.md).
- Reuse already-read bodies and sources from the same project/revision. Before a new lesson, run reflection `place` and inspect candidates/conflicts. **Do not add duplicate pages: one page per topic.** Update the existing page or fold duplicates with `replaces`. Use revision-checked patches for precise edits. Every lesson needs kind and scene. See [wiki.md](references/wiki.md) for sources, editing, diagnosis and retrieval comparison.
- Keep changing reports, working files and screenshots in the original project's `.shikii/handoff/<task>/`; capture checked evidence and cite that path. Give auditors the absolute shared report location. See [handoff.md](references/handoff.md).
- A guard refusal requires fixing the work; never disable a guard to pass. `shikii guard add` creates a disabled draft; enabling is the person's decision. See [guards.md](references/guards.md).
- Stop also verifies saved planning, reflection, wiki work, delivered-lesson assessments, article classification and reply language. Follow its concrete recovery steps in this project/turn. Check progress and errors; `continue:false` / `stopReason` means explicitly incomplete. Do not end again to bypass it or ask the person to do these internal steps.

## Project operations

- Read MCP's shared workflow in the `shikii_wiki` description before using its tools. When code processes a result, use `structuredContent` when present and keep `isError`; otherwise retain the original `content`. Give the model one complete view, preserving conditions, source/revision refs and non-text content. Do not display both copies or assume text is JSON. See [recording.md](references/recording.md#reading-mcp-results-once).
- On every MCP call, pass the **absolute path of the project owning `.shikii/`** as `project`; Codex shares a server. For a worktree, use the original project. Use CLI when a newly configured MCP is not available until the next session.
- For worktree work, keep the wiki and shared work records in the original project. Do not initialize shikii in each worktree or fork its wiki. Run CLI recording from the original project, and pass that project to MCP; run code checks in the dedicated worktree. Report the worktree path, branch, checked commit (or explicitly uncommitted state), and shared evidence location. See [handoff.md](references/handoff.md).
- When asked to enable shikii, run `shikii --init` in that project. Preserve data/settings; do not initialize unrelated folders, write `project.json` yourself or transfer calibration. Existing project questions remain; changed wording needs validation. Built-ins cover local file work, not unobserved business actions.
- Keep private conversations/settings and all project learning in its `.shikii/`. When asked for the answer page, run `shikii page` and return its URL/port.
- For memory import, follow [import-memory.md](references/import-memory.md): capture originals, place/ingest supported lessons, preserve original files and add wiki pointers. One instruction from the person is enough.
- To uninstall: run `shikii uninstall` for the preview, show it, and use `--yes` only after agreement. Keep data in `.shikii/` and `~/.shikii/agent`; pass on the printed `rm` command for binaries and ask the person to reopen agent sessions.
- `shikii update` refreshes configured binaries, hooks and skills. Without hooks, rerun `shikii --init` after updating to refresh skills. Use matching binaries/skills and reopen sessions; older writers may lose newer progress. Updating from 0.6.0 or earlier has a one-time store wipe.

## Evidence and decision boundaries

- Follow the calibrated band: act automatically, ask for confirmation, discard the action, or abstain for missing inputs/endpoint. Raw probability is not a band; shadow-only planning gives no advice.
- Actual human answers or objectively observed outcomes establish human/outcome labels. Use `shikii label <trace_id> <truth>` and `--source outcome` appropriately; never label your own assessment as human truth. AI assessments, synthetic expectations and wiki interpretations stay separate. See [truths.md](references/truths.md).
- Never edit `calibration:` by hand. `shikii calibrate --write` needs enough eligible truths and quality; changed questions/models need matching validation.
- Keep wording/layout candidates in `variants:` and measure with `shikii tune`. When asked, submit at most three meaning-preserving candidates with `shikii hook variants submit`. Check winners with `shikii hook variants check`, including the original/candidate translations and difference; use same/unsure/different honestly. Do not change a question to improve a benchmark or adopt a candidate outside the meaning check.
- With `pii:true` or tainted sessions, use local endpoints for decision text, examples, proposals and judging. Set `pii:true` when sensitivity is unknown.

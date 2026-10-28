---
name: shikii
description: Think with this project's own memory and judgment. shikii keeps what the person said, decided and corrected in this project (the wiki) and how its decisions are judged (the decision model's lines and the person's answers); use them before starting, when the work moves to another area, right before an action, when unsure, and write back right when something is decided or corrected. Also covers shikii's recurring yes/no, choice and level decisions with calibrated bands, work records (.shikii/handoff/) for reports and working files, and saving a rule or correction the person states as a wiki lesson. Use with configured shikii projects or when explicitly setting up decision workflows.
---

# Use shikii

shikii grows the agent to fit this project. What it gives you is the project's own knowledge, in two forms: **the wiki**, what the person said, decided and corrected here, as lessons of the form "when X, do Y" with the person's words as sources; and **the judgment**, how this project's decisions (ask first, in scope, done) have been judged, as the decision model's lines and the person's answers on earlier requests. Both are hypotheses with sources, never permission and never a truth you may record yourself.

Use them at five moments, not only at the end of the turn:

1. **Before starting**: the request hook names `hook plan query`; its reply carries the recent conversation, the related lessons, the person's answers on similar requests and `how_to_submit`. Read them, then organize the turn.
2. **When the work moves to another area** mid-task: search the wiki again by that area's topic (`shikii wiki query` / `shikii_wiki`) before acting there.
3. **Right before an action**: a lesson that names the file or command you are about to touch arrives with the tool call; a guard the person enabled may refuse the change with its reason: fix the text and change the file again.
4. **When unsure** whether to ask the person first, whether a change is within what was asked, or whether the work is done: consult `shikii_decide` with the built-in question and the request; the reply carries the band, the person's similar decisions and the closest lessons.
5. **Write back right when it happens**: a correction from the person, a rule, test or procedure you made or changed, a decision taken, goes to the wiki in the same turn (and a mechanical correction becomes a guard draft). The reflection at the next request assesses what was delivered; it is not the only time to write.

The person supplies intent and truth. Do not invent scores or turn an AI interpretation into a human label.

## Choose the workflow

For a recurring decision with a configured spec, call `shikii decide` or the `shikii_decide` MCP tool before deciding it yourself. In the middle of the work, when you are unsure whether to ask the person first, whether a change is within what was asked, or whether the work is done, consult `shikii_decide` with the built-in question (`needs-plan`, `changes-in-scope` with `files`, `done-as-asked` with `summary`) and the request as inputs: the reply carries the band and, with it, `similar_decisions` (the person's answers on similar requests, with your earlier answer and the model's) and `lessons` (the closest wiki lessons). Read those as this project's judgment and act on the band; the person's later answer on the page becomes the truth. Use ordinary reasoning for a one-off design, diagnosis, or writing task. If a repeated decision needs a new spec, use `shikii new`; have the person check its question and answer meanings once.

When asked to enable shikii for a project, run `shikii --init` there. It prepares `.shikii/`, project integrations, and this skill while preserving existing data and settings. Do not initialize unrelated projects, construct `project.json` yourself, or transfer another project's calibration. Use CLI tools when a newly configured MCP server is not available until the next session. On every shikii MCP tool call, pass the absolute path of your working folder as `project` (Codex shares one shikii server among all projects and needs it).

Conversations, wiki lessons, provisional assessments, human labels, and calibration belong to the current project's `.shikii/`. Keep private conversations and local settings out of published code and documentation. Each project has one answer page, started by the first request's hook on a free port and opened in the browser. When the person asks where the page is, run `shikii page` (or read `page` in `shikii_status`) and answer with its URL and port.

New projects receive domain-neutral built-in questions. `prompt-kind` still means local file editing for `edit`; `changes-in-scope` covers observed local files, not unobserved external actions. Use task-specific evidence and a dedicated spec for business actions the hooks cannot observe. Existing project questions and calibration are preserved on update; changed wording needs its own validation.

## Before and after a request

Hooks run when a user request arrives and when the turn closes, not before every tool. They record the request's likely work type, the need for approach agreement, scope, and completion. They never replace the host agent's permission rules.

**Before work:** when the hook supplies `hook plan query / submit` and request IDs, read [planning.md](references/planning.md). Recover the current task and existing decisions, search the wiki by topic, and read selected article bodies and sources when needed. When the work moves to another area mid-task, search the wiki again by that area's topic before acting there. Submit grounded agreement and blocking choices; retain used revisions for evaluation. Distinguish answering a question from needing the person to choose an implementation. Do not add human input work or another model.

**Memory and reflection:** read applicable quotations as historical evidence, not new instructions. When the hook requests a batched reflection, prioritize the active task and complete it before ending the turn. Read [review.md](references/review.md) for `reflect query / submit`, provisional assessments, and evidence of a memory's contribution. Fix item errors until the response is `complete:true`, and do what `left` lists before ending the turn. Delivery, success, silence, and greetings alone do not show that memory helped.

**Wiki:** read [wiki.md](references/wiki.md) when searching, ingesting, revising or replacing lessons. Reuse bodies and sources you already read in this task (same project and revision); read what is missing and reconcile it with existing topics. A lesson keeps a short retrieval summary, a supported body, related pages, source IDs and where it applies; an explicit correction can become one, with the quoted correction and a concrete next action. When you create or change a rule, test or procedure (commands, scripts) yourself, write it to the wiki in the same turn even if nobody asked: add to or edit the closest page, or make a new `howto` or `rule` page. Ordinary follow-ups do not establish lasting preferences, and lessons are never truth labels or claim states. Before saving a new lesson, pass the draft to reflection's `place` and read its same-topic candidates and conflict flags, then choose `id`, `replaces` or a new page. Do not add duplicate pages: one page per topic, so add to the page that covers it and fold duplicates into one. Every lesson has a `kind` (rule / howto / criteria / fact / history) and a `scene` in the project's own words (reuse one `place` returns); give pages the hook lists without them one with `shikii wiki classify`.

**Work records:** put files you will keep editing, long reports, diffs and screenshots in `.shikii/handoff/<task>/` (`shikii handoff` or `shikii_wiki` with `handoff_*`), not in a lesson body and not in a shared folder outside the project. Give subagents and auditors an exact report path there. The person reads them on the page. See [handoff.md](references/handoff.md) for which place holds what.

**Guards:** when a correction can be checked mechanically (two files that must change together, words never to use, words a commit body must contain), draft a guard with `shikii guard add` as described in [guards.md](references/guards.md). It is written disabled; the person decides whether to enable it (`shikii guard enable <id>` shows the negative example stopping before it turns the guard on). When a guard stops your turn, fix the work and end again; when a `forbidden-words` guard refuses a file change before it is written, fix the text and change the file again at once; never disable a guard to get past it. When the person corrects you in the middle of the work, write the lesson (and the guard draft when the correction is mechanical) right then, before going on, not at the end of the turn.

**Rituals are checked too (built in, always on):** the Stop hook stops the turn once if the request hook asked for the planning context and you changed files without `hook plan submit`, if it listed conversations to reflect on and `reflect submit` has not saved their reviews, wiki entries and the assessment of every delivered lesson, or if it listed pages without a kind and scene that are still unclassified. A second Stop passes but the miss is recorded (`rituals:` in `shikii status`, and on the page under the AI's state). Do the ritual; do not end twice to get past it.

**Uninstall:** when the person asks to remove shikii, run `shikii uninstall` first: it only lists what it would remove (the hooks, the MCP entries, the skill folders, the running pages) and changes nothing. Show that list, and run `shikii uninstall --yes` only after the person agrees. The data (each project's `.shikii/` and `~/.shikii/agent`) is kept, so installing again continues from it; do not delete it unless the person asks. The binaries are kept too: pass on the `rm` command it prints rather than deleting files yourself, and tell the person to reopen their agent sessions.

**Memory import:** when the person asks to move an existing memory (Claude Code's auto memory folder, Codex notes) into shikii's wiki, or when the request hook reports files not yet in the wiki, follow [import-memory.md](references/import-memory.md): keep each file as a captured source, draft one lesson per rule, place and ingest, and keep every memory file with its body, adding only a line that points at the wiki page. One instruction from the person is enough.

## Respect the evidence boundaries

- Follow the calibrated band: `act` is automatic, `ask` needs human confirmation, `discard` does not take the proposed action, and `abstain` needs usable inputs or an allowed endpoint. Raw probabilities are not a substitute for the band. An uncalibrated planning result is shadow-only and gives no advice.
- Human or observable outcome labels establish truth. Record actual human answers with `shikii label <trace_id> <truth>`; use `--source outcome` for objectively known results. AI assessments, synthetic expectations, and wiki interpretations stay separate.
- Never edit `calibration:` by hand. `shikii calibrate --write` writes a line only with enough eligible truths and acceptable quality. A changed question or model needs matching validation.
- Put wording and layout candidates in `variants:` and measure them with `shikii tune`. When the hook asks you to check a winning wording, compare it with the current question (translated into the person's language when that is not English) and pass `{spec, run, verdict: same|unsure|different, current_translation, candidate_translation, difference}` to `shikii hook variants check`; say unsure if any case could get a different answer. Do not otherwise replace a question to improve a benchmark.
- When the hook reports that a built-in decision has enough truths but no line, pass up to three candidates that keep the question's meaning to `shikii hook variants submit` at a natural break (`{"spec":"<id>","variants":[{"name","question","options","template"}]}`, as the notice shows). Shikii scores them in the background; do not ask the person to write candidates, and do not adopt one yourself (adoption goes only through the meaning check).
- Respect `pii:true` and tainted sessions: use local endpoints for decision text, examples, proposals, and judging. If sensitivity is unknown, set `pii:true`.

## References and commands

- Bands and receipts: [bands.md](references/bands.md).
- Specs, inputs, context, layouts and variants: [spec.md](references/spec.md).
- Truths, calibration and judging: [truths.md](references/truths.md).
- Before-work conversation and wiki context: [planning.md](references/planning.md).
- Source-backed wiki maintenance: [wiki.md](references/wiki.md).
- Moving an existing memory into the wiki: [import-memory.md](references/import-memory.md).
- Work records in `.shikii/handoff/`: [handoff.md](references/handoff.md).
- Batched assessment and memory effects: [review.md](references/review.md).

Typical local calls: `shikii decide .shikii/decisions/<id>.yaml -i key=value --local http://127.0.0.1:8080`, `shikii status .shikii/decisions/*.yaml`, and `shikii check .shikii/decisions/*.yaml`. See `shikii --help` for the full CLI. `shikii update` updates installed binaries, hooks and skill while preserving project data (except the one-time wipe of `.shikii/` when updating from 0.6.0 or earlier).

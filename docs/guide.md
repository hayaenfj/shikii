# shikii (閾): the long guide (English)

shikii is a tool that grows your agent to fit your project. It keeps what was said, decided and checked in a project in `.shikii/`, and hands it to the agent before the next piece of work. Corrections a machine can check become guards, the routine before and after work is checked, and the trail of the work is kept in work records. As truths accumulate, it draws a line above which decisions can be left to the model without asking a person. The name comes from the Japanese word for threshold, *shikii* (閾), and the command is `shikii` too.

This guide covers everything the short [README](../README.md) leaves out: every command, the HTTP API, how to connect models and how to build business decisions. The Japanese guide is [guide.ja.md](guide.ja.md), the design is in [decision-ops-framework.md](decision-ops-framework.md) and the structure in [architecture.md](architecture.md). The module is `github.com/hayaenfj/shikii`; the license is Apache-2.0 OR MIT (your choice; [`LICENSE`](../LICENSE)).

- [Install and update](#install-and-update)
- [Enable it per project](#enable-it-per-project)
- [Use it in ordinary conversations](#use-it-in-ordinary-conversations)
- [Guards](#guards)
- [The routine before and after work](#the-routine-before-and-after-work)
- [Work records](#work-records)
- [Keep what you checked as sources, and search and review the knowledge](#keep-what-you-checked-as-sources-and-search-and-review-the-knowledge)
- [The page and the inventory](#the-page-and-the-inventory)
- [Decision models](#decision-models)
- [Delegate business decisions](#delegate-business-decisions)
- [Every command](#every-command)
- [The serve HTTP API](#the-serve-http-api)
- [Code layout](#code-layout)

## Current knowledge, counterexamples and procedure reviews

Preparation preserves `task` and `task_context` even under a response budget;
if the current request cannot fit, it fails before replacing the receipt. When
reusing a previously supplied body or excerpts under the same receipt,
`metadata_from` can reference unchanged summaries and conditions at an earlier
`delivery` number. Unclassified text, rechecks, counterexamples and incomplete
status remain visible during reference substitution; changed conditions are
supplied normally. The overall response budget can still replace article text
or orientation with an incomplete marker and exact revision query. Treat omitted
conditions as unread and unresolved; use a sufficient budget or the individual
exact-revision query for complete recovery. No extra model
or setting is required. See the [reference contract and limits](../skills/shikii/references/recording.md#bounded-reading-and-memory-upgrades).

After reading the sources, the agent can attach `passages` to exact body quotations to distinguish current guidance, earlier context, material needing rechecking and counterexample candidates. Each quotation retains its conditions and reason at the same article revision. `work prepare` and `consult` return this interpretation as `orientation`, with unclassified text and changed premises still explicit. A later date alone never overrides earlier conditions. The Knowledge page can display and edit quotations and conditions together.

`consult` searches interpreted counterexamples and sourced failure outcomes independently of ordinary topic candidates. It ranks failures individually against the current approach, so newer unrelated failures cannot bury an earlier matching counterexample. These are candidates with original conditions and evidence; the agent decides what they mean for the proposal.

Recording an execution with both failure and correction returns `procedure_reviews` immediately and through ordinary maintenance: current text candidates, a proposed correction and linked evidence. The agent compares current conditions and saves a supported article update through notes. The response returns the current review and, when it fits, the saved article in `procedure_reviews[].article`. Omitted article text remains unread; `article_query` retrieves that exact revision before review. `review_complete:false` means review is pending even when the submitted batch is complete. After reading that article and checking its sources and conditions, the agent records `maintenance:[{ref,reason}]` with that latest ref. An edit alone does not complete review and the old ref is refused. Without an edit, the agent records the checked reason with the original ref. Reviewed unchanged evidence does not recur; a new result reopens review. No additional person scoring or automatic success labels are created.

When a saved source of a report (the report itself or its evidence) is missing or was changed outside shikii, the maintenance list still opens, and so do `wiki query` without text, `reflect query`, the reflection notice before a request and `work prepare`. That report is listed as unreadable: its `unknown` names the sources to recover (`<source id> (missing)`, `(changed)` or `(unreadable)`), and nothing of the report or its evidence is shown, because it could not be checked. The article cannot be marked as reviewed until the sources are back; while other work waits, such articles take at most one of the two places in the list. In the `collective` of a work preparation the two reports of a procedure are then listed with `conditions: "not_compared"` and no difference is claimed. `consult` and `reuse`, which ask for that very evidence, keep refusing.

An article accepts up to eight unique, nonoverlapping body quotations: each at most 600 UTF-8 bytes, with conditions and reason each at most 400. Omission preserves them and an empty array clears them. Body edits that invalidate a quotation must update it in the same revision-checked change. See the [recording workflow](../skills/shikii/references/recording.md). Before using this metadata, update and restart every process writing the same wiki. Earlier binaries cannot validate interpreted passages and refuse saves, while retaining sources and history.

## Install and update

To install, use the one-line curl command in the [README](../README.md#get-started); it downloads the latest release and does not need Go. To install from source, run `sh install.sh` in the folder where you cloned shikii; that needs Go 1.27 or newer and Git.

```sh
shikii update
```

This fetches shikii and readout for your OS and CPU from the latest stable GitHub Release. Go, curl and GitHub CLI are not needed. It checks SHA256SUMS, the attached hashes and the version the new binary reports, then updates the files in the same folder as the real file of the running shikii. It never installs into a different place on your PATH instead. A release that is the same as or older than the current version is not installed.

The licenses and third-party notices are kept in `shikii-licenses/` next to the binary. A missing file in the release or a hash that does not match leaves everything as it was, and a failure in the middle of writing restores shikii and readout. Two updates at the same time are refused. If you cannot write to the folder, the update says so.

If hooks that use this binary are already installed, the new binary updates the hooks and the agent's instructions. Hooks that were not installed are not added. If you use MCP or skills without hooks, rerun `shikii --init` in an existing initialized project after updating to refresh the installed skills; this preserves project data and does not enable hooks. If reapplying already installed hooks fails after the binary update, the message says that the binary itself was updated, and you can retry that hook reapplication with `shikii hook install --yes`. A running page or MCP server is not stopped, so restart it to switch to the new version. The updated skill and hook instructions take effect in a new agent session. Existing projects keep their questions and calibration; projects initialized from now on get the new version's built-in prompts.

**Updating to 0.16.0:** update every machine sharing the wiki and restart agent sessions, the page and MCP. Older versions treat articles with the new rationale or premise metadata as changed and leave them out of retrieval. They also refuse wiki saves; the stored articles remain intact.

**Updating from 0.8 to 0.9 (0.9.0 or later):** articles given a kind and scene by 0.9.0 look "changed" to a 0.8 binary, and 0.8 refuses every save to the wiki (the articles are not damaged). After updating, reopen the open agent sessions (and their MCP). Update every machine that uses the same wiki to 0.9.0 or later (to 0.9.2 or later once any machine runs 0.9.2: 0.9.1 and earlier read articles written by 0.9.2, whose fixed words are English, as changed). The update also prints this note at the end. From 0.9.2 shikii does not write into AGENTS.md; the update removes the shikii section an earlier version wrote in each registered project, and keeps the rest of the file (only when exactly one section is enclosed by marker-only lines; a link pointing outside the repo, or one marker or three or more, is left alone and the reason is printed; CRLF files stay CRLF).

**Updating from 0.6.0 or earlier erases the contents of `.shikii/` in every initialized project and initializes them again** (a project decision). Earlier conversations, lessons, provisional assessments, truths, calibration and work records are removed, and the built-in questions, the wiki templates and the project marker are created again. Back up anything you want to keep first. Updates from 0.7.0 onwards keep everything. When you update from 0.6.0, the wipe runs inside the old binary's last step and its messages are not shown (0.6.0 throws that output away). Run `shikii hook install` without `--yes` first to see what would be erased (it also shows how many work records there are).

## Enable it per project

After installing, run `shikii --init` (`shikii init` is the same) once in each project where you want to use shikii. For the first project on a machine with no shikii hooks, init also installs the hooks, as `shikii hook install --yes` does (without a TypeSafe key, and with `llama-server` installed, that starts the local model in the background; its first run downloads the model). On a machine that already has projects and no hooks, init leaves them off and shows the command. You can also ask your agent: "Use shikii in this project." This prepares the built-in questions (the 4 hook questions, the 2 wiki questions, the delivery question `lesson-applies` and the tool question `quote-supports`, which chiten asks about the quotes of its research reports), the wiki, the places for guards and work records, the MCP setup, the agent's instructions and the way to the page, and writes `.shikii/project.json` last. Hooks run only in a folder that has this marker or sits under one; in a project that was not initialized they create no storage and no Git exclusion. Subfolders use the same project root, and initializing again keeps the existing settings, conversations and truths. `shikii doctor` checks the setup.

What is inside `.shikii/`:

| Place | Contents | Commit |
|---|---|---|
| `decisions/` | Questions for the decision model | Yes |
| `wiki/` | Lessons, conditions, earlier revisions | Yes |
| `guards/` | Guards made from people's corrections | Yes |
| `labels/` | Truths given by people and by results (an asset that survives a change of model) | Yes |
| `records/` | Results of conversations, decisions, assessments, deliveries, observations and guards. Grows with every decision | Optional (`shikii gc` removes old records without truths; `shikii forget` removes a finished session's records and keeps them in `records/forgotten/`) |
| `sessions/` | Per-session notes (lessons delivered, notices given, files changed) | No |
| `handoff/` | Work records (audit reports, release steps, diffs, screenshots) | Only what you want to keep (`shikii gc` never touches it) |

Initialization keeps `.shikii/` out of Git's untracked files, because it contains private conversations. To commit something, remove it from the exclusion and add it yourself.

**Which folder is the project:** shikii takes the nearest folder at or above the current folder that was initialized on this machine (one whose `.shikii/project.json` `shikii --init` wrote here) as the project. git is not needed for this. An agent working in a worktree kept under the project (`.claude/worktrees/`, chiten's `.chiten/wt/` and the like) or in a subfolder uses that project's wiki, guards and records, and its changed files are compared in that worktree. `shikii guard try` also checks the changes of the worktree you are in with the project's guards. With no such folder above, shikii looks at the top of the git working tree as before (outside git, the current folder). On the way up, a `.shikii/` that was copied in, or that someone else placed in a folder above, is passed over; the one at the top of the git working tree you are in (outside git, in the current folder) is used as before. After you move a project's folder, send an agent one request in it, and the folders under it find it again. `shikii --init` prepares the folder it runs in (the top of its git working tree), also below a project. A worktree created outside the project is not reached: name the original project there as before.

**Moving or renaming the folder:** nothing to fix for a move on the same disk (0.9.7). The records inside `.shikii/` keep the name the store got when it was first opened, and shikii never moves or renames them, so the wiki, its sources and the records move with the folder; the next request's hook reconnects the project at its new path by itself and says so once. A copy is not taken over (records keep going to the original). A move to another disk, or a `.shikii` brought in from an archive or another machine, is not reconnected by itself, since shikii cannot tell it from a copy: the hook says shikii is not running there, and `shikii --init` makes it a project of its own, with its wiki and records. A `.shikii` at the very path it was initialized at is that project, wherever it came from. The update to 0.9.7 gives every project registered on this machine its store id, so a move after the update is reconnected by itself; a store moved before the update is not (the index 0.9.6 wrote cannot tell the move from a copy): the hook says so, and `shikii --init` there keeps its wiki and records; a wiki store left under another name beside the current one is reported by `shikii wiki lint` as `other-path-store`, and a waiting source captured under a path the store does not know as `source-from-other-path` (capture it again if you still need it). Update every machine and reopen agent sessions: when an older shikii registers the project again, the hook says so and asks to update every shikii. The agents' own records (Claude Code's `~/.claude/projects/`, Codex's `[projects]`) are keyed by the path and are not moved; see the habit guide's [references/wiki.md](../skills/shikii/references/wiki.md), "Moving or renaming the project folder".

To change the answer page itself (`web/`, Svelte + Tailwind CSS) you need Node 26 (`make web`). Just using shikii does not: the built page is committed and embedded in the binary.

## Use it in ordinary conversations

In Claude Code and Codex, hooks before the request, after the work and at the session's start (also right after a summary), when a subagent starts and ends, and right before a file edit or a command (6 in both) record the conversation, look up the memory and answer the built-in questions. Your usual agent reads the request, the reply and the next human message and merges them into knowledge on the same topic. It keeps a short summary for searching apart from a body that holds the background, the reasons and what is still unknown, and links related pages. `shikii wiki query lesson:<page id>` reads a whole page, and topic searches include the body. See the [editing steps for agents](../skills/shikii/references/wiki.md).

Up to 2 short quotes, within 1800 bytes, are delivered before similar requests in the same project, and the same revision is not repeated in the same session for an hour. The delivery learns from the reflection's assessments (0.12): a lesson revision found unused twice and never helpful at a door (the request, or the command or file it was delivered before) is not delivered through that door again; another door is tried again, and so is a revision that changes the page's title, lesson or applies_to (an edit that leaves them alone keeps the count, 0.30.7). A lesson delivered at a tool call is not delivered again while the same request runs. Each delivery records its door. A lesson delivered to a subagent is assessed by that subagent: it is asked to end its final report with one line per lesson (`shikii: <key> helpful - <what it did>` or `shikii: <key> unused`; `harmful - <what happened>` is read too), the SubagentStop hook saves those lines wherever they stand in the last 256 KiB of that report, for up to 16 lessons, and the parent's reflection does not list these deliveries, because the parent cannot see how the subagent worked (Claude Code and Codex, and OpenCode 2.x through its plugin). For the lesson about to go out (at the request and at a tool call) the decision model is asked the ready-made question lesson-applies, "right before this command or file in this request, does this lesson apply?" (0.12). Until a line is drawn the answer is only recorded; once drawn, a trusted no skips that lesson and the next candidate is looked at; a model that fails or is down changes nothing. The reflection's assessment is its outcome truth (helpful is yes, unused is no), so the person never answers it, as with the wiki's same-topic question. History (`history`) articles are not delivered automatically (they explain why something became so, and are read when searched for). When a subagent starts (`SubagentStart`), it gets the rules whose scene is "いつも" (always) and how to report, and no lesson chosen by the parent's request or by its own task: lessons chosen by the parent's request were unrelated to the subagent's task, and lessons chosen by the words of its task were mostly unused (a read-only auditor got the steps to commit). A lesson about a command or a file still reaches the subagent right before that command or file, and the subagent can search the wiki for what its task needs. The parent's state is left untouched, and the delivery is recorded under its own ID. The same holds in Claude Code, Codex and OpenCode 2.x; a Codex subagent starts with a copy of the parent's conversation, so what shikii gave the parent is already in it. Right before Claude Code changes a file (Edit, Write, MultiEdit, NotebookEdit) or runs a command (Bash) (`PreToolUse`), at most 1 lesson whose title, lesson, applies_to or a code span of its body (`...`, a fenced block; plain body text does not count) names that file or command is delivered: a file by the end of its path (`cmd/shikii/version.go`) or its name (`version.go`; `myversion.go` and another folder's `version.go` are other files), a command by its program and first word (`git commit`, `gh release`, `make test`; `shikii` and `npm` take their third word too, so `shikii hook plan` is not `shikii hook install`; options such as `git -c k=v` are skipped, and neither words inside quotes nor anything from the first `<<` on (a here-document's body and whatever follows it) is a command). The same once-an-hour record per session applies, nothing arrives before the session's first request, the only model call is lesson-applies about the lesson about to go out, and the hook only adds context, except that a guard the person enabled refuses a command (permissionDecision deny): a `forbidden-commands` guard, or the file and commit-body guards before a `git push` or a mutating `gh release`. These deliveries are assessed in the reflection like the others, and `shikii status` counts them on a line of their own. Codex gets this hook for its shell (reported as `Bash`) and for `apply_patch`, where the file is read from the patch's file headers. In both agents the call is already written when the hook runs, so the agent reads the lesson together with the call's result and acts on it in its next step; only a guard's refusal stops the call itself. No separate model is started to organize the conversation. Recording and looking up the conversation work even when the decision model is not available. This is neither a way to block tools nor a way to grant permission to run them (only the guards and the routine below stop anything: the routine at the end of the turn, and a guard at the end of the turn or right before a command: `forbidden-commands` before any command, the file and commit-body guards before a `git push` or a mutating `gh release`). See [the hooks in detail](../integrations/claude-code.md).

Your own answers to the built-in questions ("Should it have asked you first?", "Were the changed files all needed?", "Did it finish what was asked?") reach the agent too, without waiting for a line: when a new request is similar to an earlier one you answered on the page (or in a spot check, or by hand), the agent is told, for example, "In a similar request (above), you answered "It should have asked" to "Should it have asked you first?"", with the earlier request, with English scaffolding and the page's configured-language display labels retained as evidence. With it come the two answers you judged between: the agent's own answer with its reason (its organizing before starting, or its provisional score; only for decisions compared under 0.11's rule) and the decision model's answer, so the agent learns how its own reasoning is judged in this project. Truths that came from results or from the agent are never quoted. Similar is the same bar as for past conversations: at least half of the new request's words, and two or more, appear in the earlier request. These quotes share the same 2 seats and 1800 bytes: when one of your answers fits, it keeps one seat, lessons take the other, and a seat no lesson takes goes to a second answer. They go only to your own requests, not to subagents. When the quotes do not fit the 1800 bytes, the ones that fit are delivered. The agent's reflection can reject them (0.30.7): an answer assessed unused (or harmful) at least twice and never helpful is not quoted again, leaves the page's "Answered" list and is listed in the knowledge section under "Past answers the AI judged not useful"; it stays a truth for its decision. A decision asked again carries the same answer, so the assessments under both records count as one and the rejection holds for both. Once the quotes of past answers were assessed unused (or harmful) ten times in a project and none was ever helpful, past answers are no longer quoted there before requests. Without a quote there is no new assessment, so neither a rejection nor that stop ends by itself; the AI decides them and you can undo them on the page. "Undo" on an answer under "Past answers the AI judged not useful" quotes it again under every record that carries it and puts it back in the "Answered" list; the assessments before the undo no longer count, so it is rejected again only when two later ones find it unused. "Undo" on the line about the stop quotes past answers again; only the assessments after the undo count toward the next stop, and the rejections of single answers stay. The undos are kept in the project's own records (`records/answer-restores/`), and no truth changes. The answers are still returned when the agent itself asks the decision model about a request, and they stay truths.

Whether a delivered lesson changed what the agent did is observed in reflection (helpful / unused / harmful / unknown). When a correction was delivered, whether the agent followed it or repeated the mistake is also kept (`correction_result`: followed / repeated / not_applicable / unknown). For any other lesson, `followed` with `unknown` records that the work went the way the lesson says although its effect is not shown (it takes the decision and needs no evidence); the delivery learns from it that the lesson applied there. Success, a follow-up request or silence alone are not counted as an improvement. The agent can assess a lesson in the work record of the request it was delivered in, by the lesson's key (`memory:[{ref:<its key>,result,decision,evidence}]`); the reply of the record names the lessons of that request still not assessed (`lessons_to_assess`) and gives the record that assesses them as its `next_action`. An assessment that is refused says whether `decision` or `evidence` is missing, or which of them is longer than 1500 characters (characters, not bytes, so Japanese gets the same room as English). What is not assessed there is asked of the same session at its next request. The numbers appear on the `memory:` line of `shikii status` and on the page; your answers that were delivered are assessed the same way and counted on the line below it ("of them your past answers").

## Guards

Corrections a machine can check become guards (`.shikii/guards/<id>.yaml`). There are four kinds: `paired-files` (when one file changes, its partner must change too), `forbidden-words` (words that must not appear in added lines), `commit-body` (words that must appear in the body of the commits made in the turn) and `forbidden-commands` (regular expressions no command about to run may match; refused right before the command runs, at the `PreToolUse` of Claude Code's Bash and of Codex's shell, with an `example` command that `enable` shows stopping; commands are not observed at `Stop`, 0.11). The paths in `pairs` and `except` are globs: a pattern without `/` also matches the file name alone at any depth (`*.md` matches `docs/a/b.md`, while `docs/*.md` matches only the `.md` files directly in `docs`), `*` stays inside one folder, a `**` segment stands for any number of folders, none included (`docs/**/*.md` matches `docs/b.md` and `docs/a/c/b.md`; `**/*.md` matches `README.md` too), and a trailing `dir/**` is everything below `dir`. At `Stop`, the enabled guards are applied to the changed files, the added lines and the commits since the HEAD recorded at the request. Added lines are the lines that were not in the file at the request. A line the person wrote before the request and had not committed is not an added line. For a file that had uncommitted changes at the request, or was not tracked yet, shikii keeps no copy of the content: while a `forbidden-words` guard is enabled, it keeps only a keyed hash (HMAC-SHA256, with a random key made for that request) of each line and how many times it occurs, next to the listing (`sessions/<id>.lines`, `sessions/<id>.guard.lines` for a slash command's turn, readable by the person only, replaced by the next request and removed after 7 days with the listing). The added lines of such a file are the lines that are not in that table, and a line written more times than at the request is added for each extra time. The table follows the guards' limits (200 files, 1 MB per file, no binary files) and the hook's 3-second limit for git; a file past them, or one that changed between the listing and the table, is compared with the commit that was HEAD at the request, as before, so lines the person had not committed in it count as added once the turn changes it. A project with nothing uncommitted at the request keeps no table. A line a `forbidden-words` guard stopped is never counted as a line from before the request. The `Stop` that stops it (or records the failure again) keeps those very lines, per file, as keyed hashes with a random key of their own, with how many copies were stopped and how many copies of the line the commit that was HEAD at the start of that turn held, in one record per repository, not per session (`records/stopped-lines/<hash of the repository's path>.stopped`, no line itself, readable by the person only). Until such a line is removed from its file, or committed before a turn starts, every `Stop` (of a request's turn or of a slash command's turn) and the check before a publication count it as an added line whenever the turn changes its file, in any session and any turn, however the content at the request is read (also after `git add`), so it is stopped again; a commit the turn itself makes does not clear it for that turn. Once the stopped line is committed, it no longer counts in any comparison of a turn that starts after that commit (a commit the turn itself makes does not clear it within that turn). The record knows a line only by the keyed hash of its text, so when the person removes the stopped line (from the file, or by a later commit or a revert) and writes the same text again before a `Stop` has compared that file since the commit or the removal, the line written again cannot be told from the stopped one and is stopped, as in 0.30.7. The person's lines are only the lines that were in the file at the request and are not among the stopped ones: when the person also wrote the same text, the copies beyond those stopped are the person's. A `Stop` looks again only at the files its turn changed: there it drops the stopped lines that were removed or committed, and those of a file that is gone; the stopped lines of the other files are kept as they are. A stopped line the person sets aside with `git stash` therefore still stops after `git stash pop`, unless a `Stop` compared its file while it was stashed (a turn changed that file, or the stash happened during the turn): that `Stop` takes it as removed. A `Stop` reads the record once: its write uses the record its comparison read, and under the lock it reads only the record's first line, which holds a random value made anew at every write, and reads the whole record again only when another `Stop` wrote it in between. A `Stop` that writes the record writes it once, in the order it was read and without sorting it, so even a record near its limit adds a single reading and a single writing of it to a `Stop`. The record is read, changed and written under its own lock (a `Stop` that cannot take it writes nothing and goes on). The counts in the record take a fixed width, so a kept line takes the same room whatever its counts become (a count too large for the width is written as the width's largest value and read as at least that, which can only stop more). The record holds up to 4 MB: the lines already kept always stay, and past 4 MB only the new ones that do not fit are dropped, the files with the fewest new lines first in. The record of a repository is removed 7 days after the repository was last seen: a `Stop` that reads the record, and the cleanup of the sessions that finds the repository there, start the 7 days again, so a repository away for a moment (renamed, a disk not mounted) keeps its record. When one fails, it returns `decision: block` with the reason and the agent fixes the work. A turn started by a slash command (`/commit` and the like) is not kept as a request, but it gets a boundary for the guards alone: the working tree listing and HEAD at the slash command are kept, and that turn's `Stop` applies the guards (no questions, no lessons at the slash command and no routine checks are added to it; a lesson before a tool call still arrives, as in the request's turn). That turn is compared with its own start (the listing and HEAD at the slash command), so what the person did in between, such as a pull or a checkout, is not counted as its work. Two things still reach back to the request before it. When that request's turn never reached its end (the person interrupted it) and the slash command's turn changes a file or makes a commit, it is finishing the request's work and is checked from the request's start, so a line that turn added is still an added line when `/commit` commits it; a slash command whose turn changes nothing (`/model`) stops nothing. And a command about to run (a `git push`) is checked from the request's start, as before, so work that still fails a guard is not published through a slash command. Each guard stops a turn at most once: a `Stop` after a guard's own stop is not blocked by that guard again, and whether the work was fixed is recorded in `records/guards.jsonl`. A guard that has not stopped the turn yet still stops it once, also after the routine check or another guard stopped an earlier `Stop`, so the stops of one turn are capped by the number of guards; at a `Stop` where the routine check ends the turn incomplete, guards stop nothing. When the turn rewrites the commit that was HEAD at the request (`--amend`, a rebase) or moves to another line of history, the HEAD recorded at the request is no longer an ancestor of the current HEAD and `commit-body` cannot read the turn's commits: it stops nothing and records the check as unverified (`"unverified": true`), not as passed; `shikii guard list` shows `last: not checked`, and the stopped and fixed counts do not include it. A `forbidden-commands` guard runs right before Claude Code or Codex runs a command (`PreToolUse` of Bash, 0.11; Codex from 0.36.0): a command that matches one of its patterns is refused (`permissionDecision: deny`) with the reason, and the agent runs a corrected command at once instead of hearing about it at the end of the turn. Such a stop is recorded for the turn (`tool: true`), and the `Stop` that finds the guard passing records the fix. File changes are not judged before they happen: ordinary edits are checked at `Stop` (a check of the lines an edit adds was tried in 0.11 and removed after three audits found ways to miss a line or refuse an excepted file).

```sh
shikii guard add < guard.json       # a draft (disabled). JSON: id, kind, title, reason, source, pairs/words/except/allow/required
shikii guard enable <id>            # shows it stopping on the negative example (RED) and passing on the positive one (GREEN), then enables it
shikii guard disable <id>           # disables it again (the file stays)
shikii guard list                   # state and last result
shikii guard try                    # apply to the current working tree and show the result (commits are not checked). In another worktree under the project: what it changed since it left the project's branch, its commits included
```

The agent places the draft and a person enables it (the steps are in [the skill's guards.md](../skills/shikii/references/guards.md)). The `guards:` line of `shikii status` shows how many are enabled, how many turns were stopped and how many were fixed.

## The routine before and after work

In every turn, planning, reasoning review and reflection are checked by built-in rules. They are always on and cannot be turned off (`shikii guard list` shows them as `built in (always enabled)`). They work in projects without `CLAUDE.md` or `AGENTS.md` too.

- **Planning before work (plan-before-work)**: read the preparation delivered by the request hook; call `work prepare` with hook IDs and topic words only if none was supplied. It retains the task and registers its start without an extra submission, and the ending `work record` carries what was agreed and what is still open. Concrete choices still require actual user input; preparation is not agreement. The older `hook plan query` / `submit` stay for existing integrations; they are not run beside `work prepare`, because `hook plan query` replaces its `receipt`.
- **Reasoning review (reasoning-before-end)**: the request hook saves a mandatory turn state; `work prepare` supplies current conditions and earlier experience. Preparation registers the start and an ending result closes the ordinary review. Explicit start/update/finish judgments remain available for consequential changes. New saved evidence reopens the ending review; replaying an old finish cannot certify it. Failed or pending wiki edits must be resolved rather than erased by preparing again. Each failed note or capture retains its own original bindings; another successful save does not resolve it. Capacity recovery retires the receipt while retaining the required work for a new preparation. An ending agent source retains the start, latest judgment and result for later work. Retained approaches and evidence gaps are valid; no new discovery or article is required each time. Stop verifies saved results and ending article revisions. This does not guarantee the quality of thought or reconsideration before every tool call. No additional model, setting or person scoring is needed. See [input and limits](../skills/shikii/references/recording.md#required-review-while-doing-the-work).
- **Reflection before ending (reflect-before-end)**: prepare includes pending originals and delivered memory. Read them and combine needed reviews, notes and memory assessments with the ending work record. Use separate reflect query/submit only for deeper recovery, rather than repeating supplied material.

```sh
shikii reflect query
shikii reflect submit < reflection.json
```

`reflection.json` is JSON with `reviews`, `wiki`, `memory` and `maintenance` for the targets you fetched. Check `complete` and each item's `error`, fix only what failed and send it again. See [input examples and the steps](../skills/shikii/references/review.md); the shape of a plan is in [planning.md](../skills/shikii/references/planning.md).

Before judging completion, `Stop` checks saved planning and reflection results. Missing work returns concrete commands to the agent. The next stop rechecks the saved results: complete work permits judgment; a smaller remaining count permits another continuation with only the remaining work; no decrease ends automatic continuation with `continue:false` and an explicit incomplete `stopReason`. A failed check or recovery-state save also ends incomplete. Background notifications do not reset this progress check, and a later user request cannot cause missing work to be judged complete. Results stay in `records/guards.jsonl` and appear in `shikii status` under `rituals:` and on The AI page. The final reply should report the original work’s result.

**The final reply in your language (reply-language, 0.11)**: when Claude Code's `language` is one written in its own script (Japanese, Chinese, Korean, Russian, Ukrainian), `Stop` also checks the final reply. A fixed rule decides: fenced code blocks, inline code, URLs, and words that look like paths or identifiers (containing `/`, `_`, a digit, a dot inside the word, camelCase and the like) are left out; if fewer than 60 letters (of your script and Latin letters together) remain, the reply is short and not checked; otherwise it passes when at least 20% of those letters are in your script. A Japanese reply that keeps English terms stays well above that, and a reply written in English is near 0%. When it fails, `Stop` asks the agent to write the reply again in your language. The same saved-progress check applies; an unchanged failure ends explicitly incomplete. Languages written in Latin letters (and no `language` setting) are never checked, and there is no setting: it is on exactly when such a language is set. It counts on the `rituals:` line as `reply-language` (checked, passed, and rewritten after a stop) and on The AI page.

So that the agent learns these habits even in a project without working rules, the hook adds one line once per session, in a turn where it has something to say (reports and working files go to the work records; rules and corrections the person states go straight into wiki lessons). Subagents are told where to write their reports even when no lesson is delivered.

## Work records

Working files the agent keeps editing (audit reports, release steps and logs, diffs, screenshots) live in `.shikii/handoff/<task>/`. They do not clutter the top of the repository and they stay after the session ends. People read them on the page under Knowledge → Work records (text is shown as text and images as images; HTML is not rendered).

```sh
shikii handoff list                                    # list (newest first)
shikii handoff read audit-0.9/report.md                # read (text up to 4 MiB)
shikii handoff write audit-0.9/report.md < report.md   # replace (UTF-8, up to 1 MiB per write)
shikii handoff append audit-0.9/log.md < more.md       # append
```

In MCP, these are `handoff_list`, `handoff_read`, `handoff_write` and `handoff_append` of `shikii_wiki`. Paths are relative to `.shikii/handoff/` (a leading `.shikii/handoff/` is also accepted). `..`, absolute paths, hidden names, control characters, links to files or folders, and hard links are refused, so nothing goes outside the folder. A text with nothing to read (empty, only spaces and line breaks, or only a byte order mark) is refused by `write` and `append`, on the command line and in MCP, and the record stays as it was. A replacement always goes through a temporary file with a new name and is then renamed (temporary files left by a crash in the middle of a write are cleaned up, if older than a day, the next time something is written to that folder).

Keep them apart: lessons that help the next job go into the wiki, the key points of the evidence at the time you checked go into sources (`shikii wiki capture`; `reference` can hold a work record's path), and files that keep changing go into work records.

## Keep what you checked as sources, and search and review the knowledge

Besides conversations, the code, tests, releases and so on that an agent actually checked can become sources. The commands below are run by your usual agent; a person does not need to organize anything each time.

```sh
shikii wiki capture < observation.json
shikii wiki query "refund conditions"
shikii wiki query "refund conditions" continue:CURSOR
shikii wiki query "lesson:<page id>"
shikii wiki query "<source id>" "<another source id>"
shikii wiki query "lesson:<page id>@<revision>"
shikii wiki ingest < article.json
shikii wiki classify <page ID> <kind> <scene>
shikii wiki maintain < maintenance.json
shikii wiki lint
```

In `observation.json`, write `kind` (`human`, `agent`, `code`, `test` or `release`), `title`, `content`, `reference`, `version` and `observed_at` (RFC3339). A source has no size limit of its own: the whole JSON may be up to 4 MiB, the store's line limit. Keep the excerpt or result you checked, together with the version of what you checked. `reference` only describes where it came from: no URL is fetched and no command is run. Use the returned ID in an article's `sources`. The same observation is not stored twice; changed content or a changed version becomes a new source.

A search looks at the title, the conditions, the summary and the body, and returns the fields and words that matched and the candidates' revisions. Candidates alone cannot settle whether something is really related, so read the body and the sources you need. Replace `CURSOR` with the `next_cursor` of the previous response to get the rest with the same search words or source IDs. Reaching the end is not the same as finding the knowledge you need. A large response is split, but the search is never cut off by a number of articles or sources. Within the same project and the same revision, reuse what you actually read during the work.

In `article.json`, keep the short `lesson` apart from the detailed `body`, and merge the background, the evidence, the conditions and what is still unknown into the existing article. Connect related pages with `related`, and leave a `change_reason` for edits that change the meaning. Long conversation originals that are available are kept with their own hash when quoted, so they can still be read after the original decision record is removed. When a source or an original is missing, record that limit.

The older planning entry `hook plan query` also returns up to two live `criteria` pages directly linked through `related` from the search matches (0.14). Summaries and applicability conditions are kept whole, with kind and scene. Without `pages`, up to two complete bodies are included as `article:<id>:<revision>`, criteria first, within 8000 body bytes in total. Bodies that do not fit can still be read through explicit `pages` (up to two) or `wiki query`. The reply's `wiki_context` returns `available_count` instead of the full inventory; the decision record still preserves every revision in `knowledge_context.available`. Linked candidates, body reads and actual citations stay distinct. Links and past judgments never grant current permission.

### Prepare knowledge and record content (0.19.0)

In 0.30.1, read the complete shared workflow in the `shikii_wiki` description before using MCP tools. Repeated same-receipt searches can reuse a fixed notice from an earlier delivery; use `reasoning:{depth:focused}` with the same receipt/query when the earlier response is unavailable. Preserve conditions, source revisions and full recovery, and pass one complete result view to the model while retaining error state and any additional content. See [MCP knowledge delivery](mcp-knowledge.md).

The request hook performs preparation and supplies its result. Read it before working; do not repeat prepare from scratch. Without that result, use `shikii work prepare` with topic words in `query` and the current hook's `session_id` and `turn_id`. Without a hook, pass `query` and an agent-supplied `request`; it cannot establish a human agreement. Read the returned conversation, knowledge, pending evidence and `how_to_record`. Then use `shikii work record` with the unchanged hook preparation's `session_id`/`turn_id` (or its `receipt`), your interpretation, checked observations and source-backed notes. After an explicit replacement prepare, use its new receipt; IDs alone cannot bind older local references to the new preparation. Shikii retains source identifiers, exact article revisions and capture metadata. Placement candidates requiring interpretation come back to the agent. Check `complete`, `review_complete`, `next_action`, `errors`, `pending` and reflection's remaining work; retry corrected content with the same receipt.

When the person sends a message while the agent is still working, the hook starts and prepares a new request, and the earlier receipt stops working. The delivered preparation's `carried` names the pages and saved sources of the interrupted work that stay valid with the new receipt (a page that work edited is read again before another edit). If automatic preparation fails, follow the hook's recovery notice. A finished request hands nothing over. When the agent explicitly prepares the same request again because it no longer holds usable preparation, the new receipt keeps the earlier references in the same way; subsequent recording requires that new receipt.

On the CLI the JSON goes on standard input, and `shikii work --help` (also `reflect`, `hook plan`, `wiki` and `handoff`) prints forms that run as written. Without `max_bytes` the CLI keeps a prepare reply within 28672 bytes, a size an agent's shell shows whole, and `output` names what was left out and how to get it. A request longer than that is not cut: the reply grows to hold it, still starting with `receipt` (the hook keeps 64 KiB of a request; `task_context` says when it kept less). Every reply an agent reads starts with `receipt` (or `complete` and `errors`), then `output` when the reply was shortened, the task, `how_to_record` and the lessons, and continues from small to large. A refused call says what was wrong and what to send next: an unknown field comes back with the object that held it and the fields that object takes.

MCP uses `shikii_wiki` prepare/record with a `work` object; HTTP uses the same body at `POST /v1/wiki/notebook`. The [recording reference](../skills/shikii/references/recording.md) covers content shapes, limits, stale contexts and partial retries. Individual planning, reflection and wiki commands remain available. Restart agent sessions, the page and MCP after updating, and keep processes writing the same wiki or active session on 0.27.0 or later. Condition-based outcome selection requires 0.25.0 or later.

### Connect external assignments

A host that creates short-lived agents can add an optional `assignment` to `work prepare` and `work record`. It identifies the execution, shared group, role, stage, purpose, question, conditions and implementation revision. Preparation adds `teamwork`: changes since peers actually inspected knowledge, prior and active investigations, unresolved questions with checking methods, and failure evidence for procedure review. Reports keep their conditions, sources and provisional, integrated or reverted code state. The host still controls scheduling, cancellation and integration.

The recording reference documents the [assignment contract and limits](../skills/shikii/references/recording.md#external-assignments). Record observed work with `event`, and use ordinary revision-checked notes to return a supported procedure correction or probe result. Reports never establish human truth, permission or verified success. `evidence_scope` limits automatic delivery of original reports; `shareable` allows a host-approved summary across those scopes within the same project. All participants with access to that project's store can still query its evidence; these fields do not provide access control.

This extension is optional. Without `assignment`, existing CLI, MCP and hook workflows follow their existing path and create no assignment records. It adds no chiten dependency, service, model or configuration option to shikii. New embedded agent instructions use English; original requests and evidence keep their language, and report prose follows the request.

Keep an event on its original receipt, including after partial failure. For an unfinished event from an older binary, resend its unchanged original recording once before correcting failed notes. Update every assignment writer to 0.24.0 or later before resuming work; the recording reference explains the retry and migration boundary.

A feedback-capable chiten report can distinguish inspected articles, knowledge that informed a decision and procedure execution results, forwarding them through the existing discoveries and outcomes operations. Actual use and each result have separate source snapshots, so note corrections and identical retries do not duplicate an execution. This optional workflow requires that chiten build installed separately; ordinary shikii use does not require chiten.

### Connect knowledge and results during work (0.20.0)

Preparation supplies exact, focused passages with byte offsets and revisions when full bodies do not fit. Read the whole revision only for unresolved conditions or exceptions. Pass the same receipt with query, proposal or conditions to add search, consult or reuse while retaining earlier references, saves and retries; cursor continues a search. Added article fields and exact passages can be quoted through the same knowledge reference, preserving earlier quotations; unseen bodies cannot be quoted and agreement still needs human words. Consult/reuse text fields, including the retained task, are limited to 16000 bytes. For a longer task, run `shikii wiki consult` or `shikii wiki reuse` in the project folder (MCP leaves both out) with an explicitly scoped request.

Record discoveries by article ref with context, decision and evidence. Identical work retries recover a published discovery after edits, deletion or replacement; definite refusals consume no recording capacity. Record procedure outcomes with a howto ref, conditions, action, failure, correction, result and observed source refs. Reuse returns prior conditions, failures, corrections, reported results and evidence, flags changed procedure revisions, and asks the agent to compare current conditions and save source-backed howto updates. Reports do not create success labels or human truths.

From 0.25.0, reuse keeps its two-outcome limit while prioritizing shared content terms in current conditions, then the task and reported actions/failures, with recent records breaking ties. A failure under equally matching conditions can stay alongside a later result. Empty context keeps recent-first order. These are candidates: compare original conditions, sources and changed procedure revisions before using them.

See the [recording workflow](../skills/shikii/references/recording.md) for examples, fixed limits and retry/version boundaries. These operations are available from 0.20.0. Update all processes sharing the records before using them; older writers do not preserve the new context or outcome index.

### Use project experience while working (0.18.0)

Before settling an approach, send the task and proposed approach as JSON to `shikii wiki consult`:

```json
{"request":"Route invoices for a new tenant","proposal":"Add a form before every routing decision"}
```

It returns applicable conditions, reasons, bodies, sources and linked project guidance. The host agent reads them and explains support, counterexamples, exceptions and unknowns. A search match or link is not semantic agreement. When configured, the existing relevance model contributes an applicability check. Historical choices never authorize the current action.

For recurring work, send the request and current conditions to `shikii wiki reuse`:

```json
{"request":"Prepare the next release","conditions":"Linux and macOS; the installer changed since the earlier release"}
```

The host agent assembles a procedure from howtos, rationale, recorded validation and explicitly referenced work records, retaining each step's sources, changed premises and missing success evidence. Current work records are unverified supplementary context. Commands are never executed by this operation.

On MCP, both run as a second `shikii_wiki` prepare with the receipt and `proposal` or `conditions` inside `work`; HTTP uses `POST /v1/wiki/notebook`. Text fields are at most 16000 UTF-8 bytes each; CLI JSON accepts 1 MiB including escaping. If the full context exceeds the relevance helper limits (1500 bytes for the request, 400 bytes for the combined proposal/conditions target), `model_notice` explicitly skips that supplemental check; the host still receives the full context. At most three topic candidates and six articles in total are returned through two link hops, with a shared 16000-byte body budget; long bodies prioritize interpreted quotations and add up to three unclassified exact excerpts within the same 2400-byte budget per article, naming full revisions for unresolved conditions. Each procedure has at most three source excerpts of 1200 bytes each and current work-record excerpts of 600 bytes. Only explicit `.shikii/handoff/` references are read through existing work-record restrictions. URLs and other paths are not fetched.

When an evidenced manual discovery was missed at the time, the agent adds the matching diagnostic `case_id` to the usual reflection `discoveries`. Complete same-project diagnostics must contain the exact live revision. The frozen non-delivery reason returns as `missed-delivery` maintenance, leading to diagnosis, comparison and a source-backed patch, or a reasoned no-change review. Actual delivery and session suppression do not establish a miss; missing history is not filled with current knowledge. Reviewed evidence does not recur, while new evidence reopens review. Better retrieval candidates and usefulness in later actual work are checked separately. No extra human input or truth labels are created.


### Grow connected experience into checks and practices (0.22.0)

An article's `learning` retains a synthesis of distinct experiences, a small check with reported results, or a reusable project practice. `connections` pin other article revisions with exact quotations, conditions, reasons and supporting/counterexample roles, inheriting original source citations. `probe` keeps the question, method, reconsideration condition and sourced results; `practice` keeps a howto's ordered conditions, actions, things to avoid and checks. These remain agent-authored reports, never human truths or success labels.

Save and revise them through CLI/MCP/HTTP `ingest`/`patch` or `work record` notes. Read `learning_context` from prepare/consult/reuse and recheck conditions when connected revisions or premises changed. Inline learning has a separate 2000-byte budget per article; larger content has `omitted` and an exact revision query. An explicit pinned `lesson:<id>@<revision>` query on the same work receipt returns the full learning context within its 12 KiB storage limit, without a duplicate in the article. When it fits, reuse also returns the practice at that article revision. The knowledge page displays questions, reported results and steps. Update all writers of a shared wiki before using these fields.

`shikii wiki organize` takes `organization:{reason,sources,targets,moves}` and previews a split or merge against pinned source revisions and exact passages. It saves nothing. Publish the returned destination drafts first, retaining source citations and interpreted quotations; compare the saved revisions before applying source patches with `requires`. A changed source or destination is refused. On a partial failure, inspect the saved results and replan. Original history and navigable references to the destinations remain. These are ordered individual saves, not a single transaction. The fixed limits are four sources, four new targets, eight moves and 1 MiB of CLI JSON for organize, ingest and patch. See the [agent procedure](../skills/shikii/references/learning.md) for examples and detailed limits.

Before saving learning in 0.22.0, run `shikii update` on every machine writing the same wiki and restart agent sessions, the page and MCP. Older binaries cannot read articles carrying learning. Updating from 0.21.0 preserves existing project data. Hooks using the updated binary also refresh the installed skills; without hooks, rerun `shikii --init` as described above. Running processes are not restarted. Keep a copy of `.shikii/` before updating if you may need to restore the earlier binary and data together.

### Connect an expectation to the next decision (0.27.0)

A consequential uncertainty can be recorded as a `work record` trial: a fixed
case, question, expected observation, method, conditions, the decision that would
change, reported effort and evidence. After the actual check, link its observed
result and sources to the same trial. Challenged expectations must return to a
reasoning update or ending review. Original plans and results survive renewed
preparation and interrupted captures without being replaced by another result.

The response suggests a small unresolved check with current evidence. The agent
can select another valid `reasoning.next_trial`. A `comparison` reads two saved
executions of the same case and returns their paired measurements, separate from
`wiki compare`'s retrieval comparison. The host performs the authorized checks;
recording a plan does not execute them or prove that a reported effect is true.

Use `reasoning.depth` (brief/focused/deep) to concentrate reading on the current
decision. Brief broad searches supply exact excerpts, pinned revisions remain
fully readable, and unresolved judgments or changed premises deepen the review.
Required saves and reflection remain visible. See the [trial inputs and
recovery contract](../skills/shikii/references/recording.md#expectations-execution-and-the-next-check-0270).

Enabled file/body guards are also checked before supported `git push` and mutating
`gh release` calls observed by the `PreToolUse` hook of Claude Code's Bash and of Codex's shell, including
ordinary RTK wrappers. Fix actual files or commit bodies before retrying. Run
publication in a separate tool call after other commands; compound publication
is refused so the checks inspect the final state. Dry-run/help and standalone
read-only commands are excluded. Until 0.35 Codex had only the turn-end checks; from
0.36.0 it has this command hook too. In OpenCode, shikii's plugin adds no hook for
commands, so the guards run only at the end of the turn. The bounded parser
does not cover opaque shell expansion, heredoc tails or other publication routes.

### Kinds and scenes of articles

From 0.9.0, an article with a lesson needs a `kind` and a `scene`. A save without them is refused, and the refusal lists the values you can use.

| kind | Name on the page | What it holds |
| --- | --- | --- |
| `rule` | Rules decided in this project | What to always do |
| `howto` | How to | Steps and ways of working |
| `criteria` | How to decide | How to choose between options |
| `fact` | Facts | What is true now |
| `history` | Background | Why things became this way |

`scene` is one line in the words of the work, "when …" (up to 48 characters): "when committing", "when sending an invoice", "when explaining to a patient", not limited to software words. Reflection's `place` returns the scenes in use and how many articles each has (`scenes`), so reuse the same wording when a close scene exists (similar wordings do not scatter the scenes). A rule that applies to every request uses "always". The agent sets them; a person never picks them.

Articles made before 0.9.0 have no kind or scene. The request hook lists five of them at a time and has the agent set them (`shikii wiki classify <page ID> <kind> <scene>`, or `operation: "classify"` on MCP's `shikii_wiki`); if the agent tries to end without setting them, Stop requests recovery (the routine check `wiki-kinds`); saved progress permits another request for the remaining pages, while no progress or a verification or recovery-state save error ends automatic continuation explicitly incomplete. The lesson text is not changed, and the result is kept as a new revision. If another agent edits the same article while the kind and scene are being set, classify refuses; read it again and set them again (an edit is never undone by the old lesson). While the wiki has an unreadable or hand-edited article, every save is refused, so the hook asks for no articles and only says, in one line, to check with `shikii wiki lint`. The reply of `wiki query` starts with how many remain and the next five, and the replies of `wiki ingest`, `wiki classify` and `reflect submit` also carry what remains. Agents without hooks (Codex before it trusts the hooks, OpenCode 1.x) are told the same by the MCP instructions and by the skill, which is put both in `~/.claude/skills/shikii` and in `~/.agents/skills/shikii` (read by Codex). shikii does not write into AGENTS.md.

`reflect query` keeps its reply within what an agent's tool shows (64 KiB for the whole MCP reply, 28672 bytes on the CLI; the HTTP endpoint is not bounded): when it does not fit, `output` names what was left out, starting with the bodies of the related lessons. Each conversation listed keeps its targets and delivered memories; when even their short forms do not fit, the delivered memories keep their keys only (the source, the key, the page and whether it is a correction), and then the newest conversations wait, counted in `more_pending`. When the first conversation alone is over the bound, it comes as a short form too, and `output` says so.

Listing with `wiki query` and `reflect query` include up to 2 articles to review under `maintenance`. Read the body, the sources and the related pages, save the edits that are needed, then pass the latest `page_id`, `revision` and `key` with your `reason` in `maintenance.json`. You may also put them together in the `maintenance` array of a reflection. When an article or a related revision changes, the old review is refused; an unchanged article comes back for review after 30 days. A missing body or link is a reason to look, not an instruction to add text or links that are not needed. Saved assessments also prioritize a review when the same entry point of an article has at least two unused deliveries and no helpful ones, counted over all of its revisions (rewording a lesson does not lose the evidence), or when a delivery of the current revision was harmful. A recorded review settles that evidence, so a later edit does not ask about it again. The task carries up to three decisions with their evidence and the total count. During normal reflection the agent checks these against the sources and edits applicability or content only when supported. Unused does not mean incorrect; a no-change review is valid. The same evidence is not shown on every request, while new qualifying evidence reopens the review. Unknown and pending assessments do not count, and neither does a harmful one on an earlier revision. No extra human writing or scoring is needed.

The older `hook plan query` can pick the bodies you need with `pages` before work. The decision keeps the candidates, the articles read, the revisions quoted, the revisions available at the time and the amount fetched. Past comparisons use only the recorded revisions, without mixing in later wiki updates or later outcomes. The numbers of saves, searches and quotes are kept apart from observations that something helped a decision. The full contract is in [the wiki steps](../skills/shikii/references/wiki.md).

An explicit search covers all articles and returns `search.total` (the number of valid matching articles) and `search.candidates`. A large response is split by the size of the JSON data; read the rest with the same search words or source IDs and `continue:CURSOR` on the CLI, or `cursor` in MCP and HTTP. A cursor whose project, conditions or results have changed is refused. A single large conversation source is returned on its own; when its long text (`originals`) does not fit in one reply, it comes in parts with `original_part` (`id`, `from`, `to`, `bytes`, `json`), and the `json` pieces joined in order are the JSON of that conversation's originals. A captured source whose body does not fit in one reply comes in parts with `observation_part` (`id`, `from`, `to`, `bytes`), and the parts join back into the body. The search matches words, so when the results are off topic, change the search words.

### Tidy overstatement and repetition in the wiki

Ask the agent to "tidy shikii". The skill's [tidy.md](../skills/shikii/references/tidy.md)
guides it through checking articles and retrieval summaries against original
sources, repairing overstatement and combining repetition with the same meaning
and conditions. Sources, exceptions, uncertainty and history remain; length is
not a reduction target. Without a specified scope, it starts in small batches
from prepared articles or maintenance candidates and reports what it inspected
and what remains. Claims the sources cannot settle remain unresolved.

### Move an existing memory into the wiki

If the agent already keeps a memory (Claude Code's auto memory folder with `MEMORY.md` and one file per fact, or Codex notes), one request moves it: "move the memory into shikii's wiki". The skill's [import-memory.md](../skills/shikii/references/import-memory.md) gives the steps: keep every file as a source (`shikii wiki capture`), draft one lesson per rule with its conditions, kind and scene (a rule you were told is a "rule", steps are a "howto", facts are a "fact", and so on; prefer the scenes in use that `place` returns), merge them into existing articles through reflection's `place` step so nothing is duplicated, and keep every memory file with its body as it is, adding only a line near the top that points at the wiki article (`MEMORY.md` keeps its lines and gets the article ids). Rules that apply to every request (the language of visible text, words not to use, when to push) also keep one line each in the index, because the wiki delivers lessons by how close they are to the request. Nothing is deleted, emptied or rewritten (old files are deleted only by the person). The steps are part of the skill that `shikii --init` places, so they work in every initialized project. When files without a pointer to `.shikii/wiki <article id>` remain in that folder (or in Codex's shared `~/.codex/memories`, among the files that name this project), the request hook notices once per session and tells the agent to move them after the current request. For files shared by all projects too, the body is kept and only a line pointing at the article is added.

## The page and the inventory

There is one answer page per project. If the project's page is not running, the first request's hook starts `shikii serve --store <project>/.shikii --port 0` (it takes a free port) and opens it in the browser. The port and pid are kept in `~/.shikii/agent/pages/<project id>.json`, so several agents in the same project share one page. `shikii page` tells you where the page is (`page: http://127.0.0.1:<port>/ (port …, pid …, up)`; if it is not running, it starts it and waits). Open the page with the names `127.0.0.1`, `localhost` or `::1` (other Host names are refused with 403).

The page has three parts, Answer, Knowledge and The AI, picked with the switch at the top. On first open it starts on Answer when a decision waits for a person, otherwise on Knowledge, and the part you picked opens next time.

Answer merges what waits for a person across the questions a person answers (the kind of request, whether to ask before starting, whether the changes were in scope, whether the request was done, and whether a quoted passage supports a report's claim, which chiten asks) into one queue and shows one card at a time. Where the agent answers the same question too (its organizing before starting, for whether to ask first; its provisional assessment in reflection, for scope and for done), a decision waits for you only when the agent's answer differs from the decision model's, or the agent could not decide. The card then shows both answers side by side, the decision model's with its confidence and the agent's with its reason, and you pick one with the answer buttons. When the two agree nobody is asked, even when the decision model was unsure: the agreement is kept as a machine truth (source `agreed`) that never counts as your answer, so a line still needs your truths. There is no cap on how many disagreements are shown. The question appears at the top of the card and above the buttons; inside are the request, the last reply, the changed files and the person's next message; the answer buttons stick to the bottom of the screen. Keys work too (1-9 to pick, Enter for the same as the AI, → to skip). The count waiting is shown beside Answer. Questions only the AI answers (wiki conflicts and placement) and records whose truth comes from results are not shown. A wording appears here only when the agent switched to a new one (keep it or undo it, with translations) or could not decide whether it means the same (with translations and its reading of the difference). Arriving through the hook's "answer later" URL puts that decision first.

The AI is where things you do not need to watch live. One line per question shows how far it has learned; pressing it opens that question (progress, the last calibration, lanes for unanswered, settled-by-results and answered items, changing an answer, wording candidates). The lessons delivered and whether they helped, the routine before and after work, the guards and their last result, the decision model in use and how often decisions wait for a person are here too.

Open an article in Knowledge and choose **Edit** to change its title, summary, conditions, background, kind or scene. **Delete** asks for confirmation, then removes the article from normal lists, searches and automatic delivery. Sources, related references and past revisions remain; this is not permanent data erasure. Explicit `shikii wiki query lesson:<id>` and `lesson:<id>@<revision>` references still read the records. A deleted article cannot be revived by ordinary ingestion, including a new draft with the same title. Keep running agents on 0.15.0 or later after deleting articles. If someone updates the article while the editor is open, saving returns a conflict and keeps your input; copy what you need and reopen the latest article. Changes keep the existing sources and add a source snapshot of the submitted edit, which is not a calibration truth.

Knowledge has two tabs, "Lessons" and "Work records". At the top of the Lessons tab, "Rules decided in this project" lists every rule, set by people or by the AI, whatever scene is picked, so none gets buried: "always" rules come first, then the newest, and rules added or changed in the last 7 days are marked "new". Below it, pick a scene on the left; the middle lists titles by kind, and pressing a title shows on the right the lesson, when it applies, the background, related articles and sources. With no scene picked, each kind shows its four most helpful lessons, and "show N more" opens the rest. Articles without a kind and scene are grouped under "Unclassified", which the agent reduces with every request. "For you to look at" at the top shows only the exceptions a machine cannot decide: a lesson that did harm, a repeated correction, and a guard not yet enabled. It is not built for a person to check articles one by one. The counts of searches, reviews and reflections, the tree of earlier articles and earlier claims are inside the folded "Detailed records of the articles".

### Inventory: `shikii status`

```sh
bin/shikii status --store .shikii .shikii/decisions/prompt-kind.yaml
# prompt-kind: records=1 labels=1
#   by source: outcome=1   by truth: !edit=1
#   jev-1.14.0 @ 68ed82631e63f2a2   1  collecting (min_n 50)
```

For each spec it prints the numbers of records and labels, where the labels came from (ask / outcome / file) and how their contents lean, and for each (model, spec_hash) the count of truths that calibration uses (the latest per input, from people and results; the same number as the page) and the state
(`calibrated: vector` / `collecting (min_n …)` / `only one answer so far` (no line until both answers appear) / `tried: the answers could not be separated` (tried again as truths grow) / `stopped: the probabilities do not tell the answers apart (AUC …)` (one of the hook's questions without any line; asked on 1 request in 20 only, and a new wording, or another model answering one of those requests, tries again) / `enough truths: the line is drawn automatically` / `stale hash`). This is the first thing to look at when running shikii. For a question whose truths come from outcomes, `outcome truths checked by a person: …` says, for each answer the outcome gave, how many of those truths the person checked on the page (an answer with 50 truths or fewer is shown whole, a more common one 1 in 10, while its agreement is unsettled), how many agreed, and whether they are used to draw lines.

The `person's work` line shows how many decisions a person answers under the current rules (unsure decisions that results do not answer and the agent gave no answer to, samples of automatic decisions above a line, and decisions where the agent's answer differs from the decision model's or is undecided), in total and per 1000 decisions over the last 7 days, and counts separately whether the truth was decided by a person or by a result. `left to a person` under `rungs` is the number of unsure decisions; unsure decisions that a result answers do not wait for a person. Compare this line before and after a change to see whether it reduced people's work.

There are also the `memory:` (lessons delivered and how they did), `guards:` (how many are enabled, turns stopped, how many were fixed), `rituals:` (the routine before and after work) and `page:` (the page's URL) lines.

When a line is newly drawn, or when the truths reached `eval.min_n` but no line can be drawn (only one answer so far, the answers could not be separated when tried, or the question was stopped), the Stop hook tells the person once at the end of the turn (Claude Code shows it in the terminal). It is said once per question, again only when the state, the wording or the model changes, and the page shows the same reason under the AI's state and in the question's panel.

## Decision models

### The recommended model (TypeSafe Jev)

```sh
export TYPESAFE_API_KEY=...       # the key is read from the environment only; shikii does not read a keychain
shikii doctor                     # when hosted shows "key present", the hooks, the built-in questions and pii: false decisions go to Jev
```

The recommended model is TypeSafe's Jev. With the key, the hooks and the built-in questions go to Jev and no local model is started (`shikii up` refuses too, nothing to download). To use a local model while keeping the key set, use `SHIKII_MODEL=local`; to force Jev, use `SHIKII_MODEL=hosted` (the same for the CLI, MCP, `serve` and the hooks). One project can choose for itself on its page (Settings in the menu: prefer the setting given in environment variables, Jev or the local model) or with `shikii init --model local` / `shikii init --model hosted`: the choice is kept in `.shikii/project.json`, comes before `SHIKII_MODEL` and the API key for that project's decisions, and the hooks (and `shikii up` run in that project's folder) start the local model for a project that chose local. `pii: true` decisions are only ever sent to a local model, key or no key, so to answer them with a local model while the key is set, start it with `SHIKII_MODEL=local shikii up`.

### The local model when you cannot set a key (llama.cpp)

```sh
brew install llama.cpp            # the official Homebrew formula
llama-server --version
shikii up                         # starts llama-server on :8086 and shikii-readout on :8080 (downloads about 7.6 GB the first time)
```

The default model is `Ternary-Bonsai-27B-Q2_g64.gguf` from `prism-ml/Ternary-Bonsai-27B-gguf` (an ordinary chat model; `shikii-readout` reads the probabilities of the option letters, so it answers at the speed of a decision model). To install llama.cpp, see the [Homebrew formula](https://formulae.brew.sh/formula/llama.cpp); for other systems, see the [official instructions](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md). `shikii up` and the built-in hooks also use this model when there is no key. It uses about 10 GB of memory while running, which can be heavy for an ordinary PC. For a trained decision model such as OpenJev, use `shikii up --hf <repo:quant>` (readout passes its answers through unchanged). Kev-4B is not supported.

The model name that is the calibration key is the `model` in the response, not the name you sent with `--model`.

### Endpoints

Business `decide`, MCP and `serve` use `POST /v1/systemone` in the System One format. This is not the same thing as an OpenAI-compatible chat API. If the URL you give the CLI has a path, that path is used; if it has none, `/v1/systemone` is added. The response must contain `answers` with probabilities for each question. An answer without usable probabilities is an error, like a failed model call, and is not recorded as a decision: a yes/no answer with no `noul` value (the field is missing or null) or a value outside 0–1, and a choice or score answer with a value outside 0–1, a probability for an option the question does not have, or no option above 0. A choice broken down into yes/no steps follows the yes/no rule for each step. A `noul` of exactly 0 is an answer ("no") and is decided as usual. `shikii-readout` is a converter that uses llama.cpp's `/apply-template`, `/tokenize` and `/completion`; it is not a converter for any chat API.

| Endpoint | Conditions | Example URL |
|---|---|---|
| Jev (recommended) | `TYPESAFE_API_KEY`, and a spec that allows sending outside (`pii: false`) | Not needed (`--hosted https://api.typesafe.ai` is the default) |
| llama.cpp | When you cannot set a key. An ordinary GGUF (the local standard is Ternary-Bonsai-27B) through `shikii-readout`. Decision models such as OpenJev are passed through | `--local http://127.0.0.1:8080` (the readout port that `shikii up` starts) |
| Ollama | v0.35.0 or newer, a decision model that supports System One. Inference on the real runtime not tested in this release | `--local http://127.0.0.1:11434 --model nimble` |
| LM Studio | A Jev-compatible System One API and a loaded decision model. Inference on the real runtime not tested in this release | `--local http://127.0.0.1:1234 --model <loaded model id>` |

References: [Ollama's System One](https://docs.ollama.com/api/systemone), [LM Studio's System One](https://lmstudio.ai/docs/developer/jev-compat/systemone), [TypeSafe's API](https://docs.typesafe.ai/introduction/quickstart). This table matches the official specifications against the client; it does not promise that inference works on each runtime. Local clients send no Authorization header, so a setup that requires authentication needs to be checked separately.

The built-in hooks use Jev when the key is set and otherwise a fixed `127.0.0.1:8080`; they do not switch to Ollama or LM Studio by themselves. Organizing and looking up conversations work even when the decision model is down. Ollama can also call cloud models through a local URL, so for decisions that must stay on your machine, choose a local model. See [Ollama's explanation of cloud models](https://docs.ollama.com/capabilities/decision).

### Examples of `--hosted` and `--local`

`--hosted` uses the URL as given when it has a path. The key comes from the environment variable `DECISIONS_API_KEY` (or `TYPESAFE_API_KEY`).

| Target | Example |
|---|---|
| TypeSafe Jev | `--hosted https://api.typesafe.ai --model jev-latest` |
| OpenRouter Decisions API | `--hosted https://openrouter.ai/api/alpha/decisions --model typesafe/jev-1.13` |
| llama.cpp (local) | `--local http://127.0.0.1:8080` |
| **`shikii-readout`** (an ordinary GGUF; any llama-server behind it) | `--local http://127.0.0.1:8090` ([`integrations/readout.md`](../integrations/readout.md)) |
| BonJev (an outside implementation of the same idea; Bonsai only) | `--local http://127.0.0.1:8083` ([`integrations/bonjev.md`](../integrations/bonjev.md)) |
| Fake model | `--local http://127.0.0.1:8099` (`make fake`) |

### Turn an ordinary GGUF into a decision model with stock llama.cpp alone: `shikii-readout`

**No fork, no Rust and no training needed.** Load any GGUF into the llama-server from `brew install llama.cpp`, put one adapter in front of it,
and to `shikii` it looks like the same kind of decision model as OpenJev or Jev.

Stock llama.cpp's `/v1/systemone` accepts only trained decision models (OpenJev / Clef …). To use an ordinary chat GGUF,
assign A, B, … to the options, run a single token and read the probabilities of those letters (the same idea as had-scorer). `shikii-readout`
does this and serves the result in the System One format. It does no inference of its own; it only asks the llama-server behind it (stock or the PrismML fork) over HTTP.

```sh
llama-server -m <any>.gguf --port 8086 --alias my-model      # stock or fork
bin/shikii-readout --upstream http://127.0.0.1:8086 --port 8090
bin/shikii decide .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8090 -i message='fix the typo in README'
```

- At start it checks with `/tokenize` that every label is a single token, and refuses to start if not
- It uses the model's chat template through `/apply-template`, and turns off Qwen3-style `<think>` with `enable_thinking: false` (and sends again without it to an upstream that does not accept it)
- The probabilities it returns are a **plain softmax**. It does not calibrate. The `model` in the response is set by llama-server's `--alias`, and that is the calibration key
- Options that did not make it into the top `--n-probs` get a floor probability (1e-6) (0 would break the log in calibration)
- The state goes at the end of the prompt, and the prefix for each spec (question and options) is run once first to make a checkpoint. For the Qwen3.5/3.8 family with recurrent layers
  (the base of Bonsai), `cache_prompt` does not work without this

Details in [`integrations/readout.md`](../integrations/readout.md).

## Delegate business decisions

Decisions in your own work ("is this inquiry the same matter as the last one?") can be handed over little by little while truths accumulate. Write the question, the options, the inputs and the thresholds in a decision file (spec) and keep supplying answers, and a line can be drawn above which the model is right often enough at that confidence. Until the line is drawn, `act` never comes out.

```sh
go build -o bin/shikii ./cmd/shikii

# copy the questions used in the examples below into this project
bin/shikii new prompt-intent --from examples/prompt-intent.yaml
bin/shikii new same-issue --from decisions/same-issue.yaml

# the spec's hash (the calibration key)
bin/shikii hash .shikii/decisions/same-issue.yaml

# decide. The hosted model is used when TYPESAFE_API_KEY is set; --tainted never sends to hosted
TYPESAFE_API_KEY=... bin/shikii decide .shikii/decisions/prompt-intent.yaml -i message='fix the typo in README'
bin/shikii decide .shikii/decisions/same-issue.yaml --local http://127.0.0.1:8080 --tainted \
  -i report1='Entering a wrong password 3 times on the login page gives a 500 error' -i report2='Server error (500) right after hitting the limit on wrong password attempts'

# decisions are kept with their text in ./.shikii/records/. Turn the answer chosen at ask into a label
bin/shikii label <trace_id> change_request --source ask
bin/shikii label --last --spec-id prompt-intent question --source ask   # label the latest record (no trace needed)

# calibrate. noul compares identity with Platt, choice/score compare identity with vector (a bias per option + temperature) under 5-fold CV and write the winner.
# labels from the file and labels in the store (those whose (spec_hash, model) match) are used together
bin/shikii calibrate .shikii/decisions/same-issue.yaml --model 'ternary-bonsai-27b-q2g64+state-last'   # the model in the response (Jev's name when a key is set)
```

### Writing a decision file (spec)

`shikii new <id>` makes one by asking you questions (or with the flags `--type --question --option id=description --input name --pii true|false --send-to local`). Every field and how to write it is in [skills/shikii/references/spec.md](../skills/shikii/references/spec.md) (`shikii init` also places it at `~/.claude/skills/shikii/references/spec.md`).

While the truths are fewer than `eval.min_n` (required in the spec; `shikii new` writes 50), neither `calibrate --write` nor `tune` writes anything. To try it while you have only a few, lower `eval.min_n` in the spec to the number you have. A line written with `--force` is flagged by `check` as "a line calibrate would not have written", so do not use it for trials.

### The whole loop in two minutes (no key, no GPU)

```sh
scripts/demo.sh                  # fake model (cmd/fake-systemone): decide → label → calibrate → check → drift → status
scripts/demo.sh --overconfident  # a fake model whose probabilities are pushed to the extremes; see calibration at work
```

### Contract checks and drift monitoring

```sh
# run in CI. Does the calibration's spec_hash match the current wording, and is precision at act at least act on the labels? exit 1 on a violation
bin/shikii check .shikii/decisions/*.yaml

# on the latest N labels, warn and exit 1 when the Brier score is 0.05 or more worse than at calibration, or precision falls below act
bin/shikii drift .shikii/decisions/prompt-intent.yaml --model jev-1.14.0 --window 50
```

There are three calibration methods. All of them are generated by `calibrate --write`, and the key is (spec_hash, model).

| method | For | What it is |
|---|---|---|
| `platt` | noul | σ((Δ−bias)/T) |
| `vector` | choice / score | softmax((log p_k + b_k)/T). Fixes each option's habits (such as falling back to other on a short state) |
| `identity` | any | No constants. A mark that the raw distribution was checked to give enough precision at act (for already calibrated models such as Jev) |

`calibrate --write` replaces only the `calibration` key and keeps the spec's comments and wording as they are. When you run it again on the same labelled file, it reuses the labels for the same inputs left in the store and does not call the model, so calibration, check and drift work even with the model stopped. Whether calibration is needed is decided by cross-validation for each (question, model), not by what the model claims. Numbers per model are in [`docs/model-comparison.md`](model-comparison.md), and the measured numbers in [`docs/bench.md`](bench.md).

### Bands for score: `thresholds.level`

On an ordered scale the probability splits between neighbouring levels, so deciding the band by the top probability makes the model look weaker than it is. For example, when the probability splits half and half between "directly related" and "strongly related", the top is around 0.5 and stops at ask, while the probability of "directly related or more" is high.
With `thresholds.level: k`, the band is decided by **P(level ≥ k)** (top and the expected score are still shown).
It is not part of the calibration key (like the thresholds). The reported precision is also measured as "P(level ≥ k) against true level ≥ k".

### Order bias in choice: `shikii bias`

```sh
bin/shikii bias .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8080 --rows 20 --permutations 5   # the states are the same savings as calibrate (the texts of the truths in --store, or --labeled)
```

It shuffles the order of the options, asks the same state again, and reports how often the top answer changes (flip rate) and how far the probability moves at each position
(the difference from asking in the spec's order). It warns and exits 1 when the flip rate is above 0.10 or the range of the position shift is above 0.10.
The model does not generate text, so running permutations is cheap. The options are sent in the order written in the spec (`systemone.OrderedCriteria`; a Go map would sort the keys).

### Memory nap: `shikii nap`

At most once a day, shikii sets aside a moment to go through what has piled up. When a person's session starts (never a subagent's), and 24 hours or more have passed since the last notice with 5 or more requests finished since then (saved work results), the hook tells the agent to run `shikii nap` in a background subagent. The numbers are fixed; there is no setting.

- The notice writes the time to `.shikii/nap` (this notice on the first line, the previous one on the second), so another session opened at the same time is not told again.
- A completed memory upgrade starts again. Only the articles changed since and the new ones come back for review. A new article also joins an upgrade that has not completed, for example because a page is blocked.
- `shikii nap` prints, as JSON, the conversations it has not shown yet (the request, the agent's summary and the person's reply): those written after the place in the record where the last `shikii nap` stopped, or all of them the first time. The agent adds the corrections and rules in the person's replies that the wiki does not hold yet, through the usual steps (`shikii wiki capture` → `shikii reflect place` → `shikii reflect submit`).
- The conversations come in the order they were written, oldest first. One run prints at most about 24,000 bytes in all, so that the agent's command tool shows it whole. The whole output counts, `next_action` and the articles whose premise changed (below) included: those articles come first, as many as fit, then the conversations that fit in the room left, at most 64. When the articles use the room up, that run prints no conversation (`newer_left` counts all that are waiting, and the place does not move) until saving the articles again makes the list shorter; when not even one article or one conversation fits, it prints the first article, or the first conversation when there is no article. When more conversations are waiting, `newer_left` counts the ones after the last conversation printed, and the next `shikii nap` starts with them. The first `shikii nap` starts from the oldest conversation ever saved, however many there are: it reads the whole record, not only the newest ones.
- Once it has printed its conversations, `shikii nap` writes where it stopped to `.shikii/nap-window-end`, a file of its own, in two lines: the byte position in the record just after the last conversation it printed, and the SHA-256 hash of that line (when it printed none, the file is left as it was). The record only grows at its end, so every conversation written after that position is shown by the next run, whatever time it carries; no time is compared. A notice the agent did not act on loses nothing: the next `shikii nap` starts where the last one stopped, and shows the conversations in between too. `shikii nap` goes on after the first line in the record that has that hash, so it still finds its place when lines before it were removed (`shikii forget` rewrites the record), and starts from the beginning when no line has it (the record was replaced). When the same line is in the record twice, it goes on after the first one: a conversation may then be shown again, none is lost.
- `.shikii/nap-window-end` is read only when it is an ordinary file with one name that holds those two lines; otherwise the run starts from the beginning. When the file cannot be written (a second name, a hard link, for one), the conversations are still printed, and the error says to remove `.shikii/nap-window-end`; until it is removed, every run prints the first conversations again.
- Each conversation carries `lessons` when articles already cite it: the ids of the articles that have it as a source, or that hold the person's correction from it. The agent only checks that those articles hold the correction right and does not add the same lesson again, and reads the conversations without `lessons` most closely. Every conversation is still listed. Withdrawn, deleted and replaced articles are left out, and the key is left out when no article cites the conversation.
- `premise_changed` lists the articles whose watched project files have changed or can no longer be read (`id`, `revision`, `title`), up to 32 in id order and as many as fit in the output limit above, and `premise_changed_more` counts the rest. They are listed even after they were reviewed, because a review keeps the files the article was saved against; saving the article again with its watch takes the files as they are now. The agent goes through them first: it reads the files again and either fixes the article or, when it needs no change, saves it again unchanged with its watch. A watched file that no longer exists cannot be watched again, so the article is saved with a watch that leaves that file out. A maintenance review that only records the reason leaves the article listed, and the articles past the 32nd, or past what fits, are listed once the ones before them are saved again. An article that watches no file is never listed.
- The same output carries the progress of the memory upgrade, and the agent carries it to the end after those articles.
- shikii never edits an article itself. When the agent does not run `shikii nap`, what is left of the review stays in the memory upgrade's checkpoint, the conversations wait in the records, and the next `shikii nap` shows them.

### Cleanup: `shikii gc`

Records (`records/`) grow with every decision. Labels are calibration material and are never thrown away, but old records that never got a label are not needed.

```sh
bin/shikii gc --store .shikii                 # show only (default: records older than 30d without a label are candidates)
# prompt-kind: would remove 120 unlabeled record(s) older than 30d; keep 48 labeled, 15 recent, 0 unparsable
# dry run: nothing was changed. Re-run with --apply to remove them.
bin/shikii gc --store .shikii --apply         # actually remove them
bin/shikii gc --older-than 1w .shikii/decisions/prompt-intent-v2.yaml       # choose the spec and the age (d / w / h; 0 means any age)
```

Only "records older than `--older-than` that have no label at all" are removed. Labelled records, labels and lines that cannot be read as JSON are kept whatever their age. The original text kept for a removed record (`records/text/`) is removed with the last record of its decision (0.30.7); in a run where a records file holds a line that names no decision (a line that is not JSON, or has no `trace_id`), no original text is removed. `shikii gc` over every question (no spec given) also sweeps the original texts left without any decision, older than the age given and than an hour, so such a text goes at a later run once that line is fixed; the result gives the count, or says that such a line blocks the sweep (find it by reading the records files for a line that is not JSON or has no `trace_id`).
Files are written in the same directory and then replaced by rename, so the original file is not broken if this stops halfway.

`make status` / `make drift` / `make check` / `make gc` / `make install` / `make server` are also available (`Makefile`).

### Removing what a finished session left: `shikii forget`

A trial run of an agent, or a session started by mistake, leaves records in `.shikii/`: its work state, the deliveries of lessons it was given, the sources it captured, the decisions it caused. `shikii forget` finds them by what each record names (a field that holds the session id, or an id the session's own records carry: its turns, its deliveries, its decisions, its sources) and never by searching the id as text. A person runs it in the project's folder. An agent can call `shikii_wiki` with `operation: "forget"` and `sessions`, but it removes nothing by itself: shikii asks the person in the client (see below).

```sh
shikii forget ses_a1b2c3                  # list: what would be removed, what stays and why, what only might belong (nothing changes)
shikii forget ses_a1b2c3 agent_9f --apply # remove them: a session id, and the subagent ids that ran under it
```

Without `--apply` nothing changes, not even a lock file. With it, everything that goes is first moved into `.shikii/records/forgotten/<UTC time>-<short id>/` (a file under its own path in the store; a removed line appended to the file of its path; a removed element of the search or delivery journal kept as it was), and `plan.json` there says what was removed, why it was sure to belong to the session, and what was left and why. Nothing is deleted from that folder by shikii. There is no command to put things back: a file that was moved whole can be moved back; for a file from which only lines or elements were removed (the `.jsonl` files, and the JSON files `told.json`, `.captured.json`, `.searches.json` and `.deliveries.json`) the copy holds only those, so add them to the end of the current file, or into the current JSON file (replacing the file with the copy would lose other sessions' lines and elements); what sits under `home/` in the copy goes back to `~/.shikii/` in the same way.

What stays, listed with its reason: a source an article or one of its revisions cites, or that an assignment, another session's work state or reasoning history, or the search records that remain still name; a decision (with all its labels) that a person labeled, whereas a machine's labels (`outcome`, `replay`, `context`, `judge`, `agreed`) go with their decision; the sessions of other ids, even when one starts with the id you gave. Records that only a word or a time connects to the session (an observation or an article that says the id in its text, an observation made while the session ran) are listed as candidates and never removed; `hook.log` and the assignments, which hold nothing that says whose they are, are not touched. The open rows of the coverage ledger that the removed results had closed are open again.

It refuses, and changes nothing, when the id is the session the command runs in (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `OPENCODE_SESSION_ID`), when the agent process that runs the session is still alive and the session is at work (the hook notes the process in the session's record at each event: the first ancestor of the hook that is named `claude`, `codex` or `opencode`, whatever stands between them, or that was started by the path of one of Claude Code's version files, a path that ends with `claude/versions/<version number>`, which names the process by the version (the path is read from `/proc/<pid>/exe` on Linux, and on macOS from the name ps gives when it is a path, else from the first word of the arguments, asked only for a name that is a version number), and none when there is no such ancestor; it counts as the same process only with the same pid, start time and machine, whatever the time since the records were written. The session is at work when the agent is in the middle of a turn. Any event of the session opens the turn, first thing in the hook, before its slow work: a message of the person (a request, a completion notice, a slash command that is not a request, a message with no text), a tool call that edits a file or runs a command, a subagent beginning or ending (a subagent's own events too), and, in OpenCode, the end of a subagent that wakes the session. A Stop that lets the turn end closes it, but not when an event of the session reached the hook after that Stop began (the Stop reads and judges for seconds, and the turn went on meanwhile), and, in OpenCode, not when the Stop names the message of an earlier request (the plugin sends the id of the message each request began with, and the Stop of that message). A turn the person stopped partway may leave no Stop, and `/clear` may call the hook and leave none, so such a session is refused until the agent is closed; so is one whose state holds an event newer than the Stop that closed its turn. A turn that goes on after a Stop without any such event (another Stop hook asked it to continue, and the continued turn only reads files and thinks) is not seen until its first one. The session is also at work when a subagent that began under it after the hook first saw the agent run the session has not ended: a subagent in the background thinks inside the agent's own process, so no program says so, and the hook keeps the id and start time of each subagent that began (SubagentStart, or the plugin's subagent start) until its end (SubagentStop, or the plugin's end of the subagent) and leaves them through the person's next requests; one that began before the hook saw this agent process belongs to an earlier process (the session was resumed) and is not counted, and a subagent whose end never reached the hook keeps the session refused until the agent is closed. A subagent the agent continued after it ended (Claude Code's `SendMessage`) may reach the hook with no start, so an event of the session that carries a subagent's id (a tool call of that subagent, say) also keeps that subagent open until its end; one that only thinks before its first such event is not seen, and a late event of a subagent that has ended opens it again, so the session stays refused until the agent is closed. In OpenCode the plugin tells the start and the end of every turn of a subagent's session, not only the first: a subagent the parent continues is open again from the start of its new turn, and a message that joins a turn that is running is not a new turn. The time the agent is noted at is the time the hook that first saw it began, not the time that hook finished, so a subagent that same hook started is counted. When the request's hook is stopped for its time in OpenCode, the Stop of that request's message still closes the turn, because the hook writes the message's id first. The session is also at work when a process below the agent that started after the hook first saw the agent run the session is still running. The list of processes is read once for each pass; forget itself, what it started to read the list and everything above it are not counted, nor are processes that started before that time, such as the servers the agent starts with itself, nor processes detached from the agent. A tool server the agent starts later, while the session runs (a language server, `caffeinate`), is counted until it ends, so a session can be refused for a while for it. The refusal says which it is and names the running process or the open subagent. A session whose last turn has ended and that has nothing running below its agent is taken while the agent stays open. When the state of the last turn (a state written before 0.41.0 does not say whether a turn is open, and shikii removes the state of a session 7 days after its last event), the time the agent was noted at, or the list of processes cannot be read, shikii cannot tell and refuses), when no process can be compared (none is noted, as in a record from before 0.41.0 or when the hook found none of the three agents above it, it was noted on another machine, or this machine's process table cannot be read or does not show the process that asks, as in a `/proc` that is empty or of another pid namespace) and any record of the session (its work state, guards, the lessons it or its subagents were given, its conversation, its reasoning history, its latest request) was written in the last 24 hours (the refusal says which), when its lock is held (it may still be running), when two ids share one file name, and when an id is a subagent that ran under several sessions (give the session id too). The locks of the wiki and of the lesson assessments are taken only by the steps that change them, so other agents' hooks are not kept waiting for the whole run. A step whose lock another agent holds, or whose records changed after the list was made, stops there; the steps before it are already done. The same happens when a session began something since the plan was made (a request, a subagent, a tool call of the agent): before each step, and in the last step under the lock that holds the state of the turn, forget reads the state of the sessions again and stops when it is not what the plan went by. Nothing is removed then, or what the steps before moved is kept, and the same command removes the rest once the session is idle. A run that stops in the middle (another agent holding a lock, records that changed, a full disk) is finished by running the same command again: what a step goes by is removed after that step, so the second run finds the rest.

Over MCP the same plan is shown to the person by the client, as a question (MCP elicitation: `elicitation/create`, a form with one yes or no that starts as no), and shikii removes only when the answer is accept with the box checked. The answer comes through the client, never through the arguments of the tool, so an agent cannot give it through the tool. Decline, dismiss, an unchecked box, an answer that is not that form, an answer that does not come within 10 minutes, or records that changed while the person decided (the plan is read again under the locks and must be the plan they saw) change nothing, and the reply says which (`status`: `removed`, `partial`, `declined`, `dismissed`, `not_confirmed`, `unanswered`, `changed`, `refused`, `needs_person`, `nothing_to_remove`, `failed`; `partial` has `changed` true: a step stopped as described above after the person agreed (also when the session began something meanwhile), what was already moved is kept and listed, `steps_left` names the rest, and the same call again, which asks the person again, removes it (when the session began something, once it is idle); `failed` is for a failure before anything was moved, also when a step stopped before it moved anything: `changed` is false and the folder it made is removed again). A run that stopped is run again with the same judgment of the session, because the record that names the agent process and the state of its last turn are removed in the last step, which takes its lock first. The same conditions apply, except that the session that asks is the one the call names (`work.session_id`), because the environment of an MCP server is the one of whoever started it, not of the session that calls now; a call that does not name its session is refused before anything is asked. While a question is open the server answers a ping and tells any other request that it is busy. A client that does not say it can ask the person (no `elicitation` capability, or only the `url` mode) gets the list and the same refusal as before: a person runs `shikii forget`. `shikii serve` (HTTP) has no forget.

## Every command

In the same order as `shikii --help`. For all the flags, run `shikii <command> --help`.

| Command | What it does |
|---|---|
| `shikii init` (`shikii --init`) | Enables this project (built-in questions, wiki, places for guards and work records, MCP, the agent's instructions, the page; for the first project on a machine with no hooks, the hooks too). Safe to run twice |
| `shikii new <id>` | Creates `.shikii/decisions/<id>.yaml` by asking a few questions (`--from` copies a sample) |
| `shikii up` | Starts the local model (llama-server, plus `shikii-readout` for an ordinary GGUF). Starts nothing when a key is set |
| `shikii serve` | The HTTP API and the answer page |
| `shikii mcp` | The same as an MCP server over stdio (`shikii_specs`, `shikii_decide`, `shikii_label`, `shikii_status`, `shikii_tune`, `shikii_wiki`, `shikii_review`, `shikii_reflect`) |
| `shikii doctor` | Checks the key, the local model and the store, and whether the shikii binary the hooks and Codex's MCP registration run still exists and can run (with how to fix it when it does not). It checks only hooks in exactly the form shikii writes (a path in single quotes, or a bare path of only the characters a path has, then ` hook <event>`, a binary named `shikii`). A command that runs shikii written another way (`~`, `$HOME`, quotes, chained after another command, more words after the event) is noted as written by hand and not checked, and `hook install` and `uninstall` leave the whole command as it is |
| `shikii update` | Updates shikii and readout to the latest stable release |
| `shikii hook install` / `uninstall` | Adds or removes the Claude Code and Codex hooks (without `--yes` it only shows what it would do) |
| `shikii uninstall` | Removes what shikii put into the agents (hooks, MCP entries, the agent's instructions) and stops the pages. The data (`.shikii/`, `~/.shikii/agent`) and the binaries stay; it prints the `rm` command for them (without `--yes` it only shows what it would do) |
| `shikii work prepare` / `record` | Prepares a task with the project's knowledge and records its result; planning before work and the review at the end go through these two (used by the agent) |
| `shikii hook plan query` / `submit` | The older entry to planning before work, kept for existing integrations (the agent uses `work prepare` and `work record`) |
| `shikii hook variants submit` | Hands over candidate wordings (used by the agent) |
| `shikii hook variants check` | Hands over whether a winning wording means the same, with translations of both into the person's language when it is not English (used by the agent; adopted when the same, a person can undo it) |
| `shikii decide <spec>` | Decides and returns a band |
| `shikii label <trace_id> <truth>` | Records a truth (`--last` for the latest record) |
| `shikii calibrate <spec>` | Draws the line from truths (`--write` writes it into the spec) |
| `shikii status` | Records and truths, the state of each line, and the lines for lessons, guards, the routine and the page |
| `shikii page` | The URL of this project's page (starts it if it is not running) |
| `shikii tune <spec>` | Scores candidate wordings on the stored truths (adopted only once the meaning is confirmed: the agent's `hook variants check`, the page, or `--write`) |
| `shikii import <past.csv> --spec <spec>` | Turns a table of past cases into stored truths (JSON) |
| `shikii judge <spec> --local <URL>` | Uses another, larger model as a judge; when it agrees with people's truths often enough, it adds weighted truths to records that have none |
| `shikii wiki query` / `consult` / `reuse` / `organize` / `reconcile` / `patch` / `diagnose` / `compare` / `capture` / `ingest` / `classify` / `maintain` / `lint` / `verify` | Searching, checking approaches, reusing procedures, sources, saving, setting kind and scene, reviewing and checking the wiki, and re-checking claims |
| `shikii wiki withdraw` | For a tool that saves articles from its own work (chiten, for a task that reached its goal): when the person deletes that work, the tool takes the article it made out of delivery. The article is not deleted: it stays as a withdrawn article with its text, history and sources, and is not delivered or found by a search. The Knowledge page lists the withdrawn articles, and the person restores each (it is delivered again) or deletes it (after a check that shows what goes; a deleted article cannot be restored). It takes `{id,revision,sources,reason}` on standard input and works only when every source the article cites is named in `sources` and the article holds no person's correction. Command line only, not MCP or HTTP; no command or MCP operation restores or deletes an article, and an agent does not call it on its own judgment. `shikii status` and the page's status say how many withdrawn articles are waiting. A shikii older than 0.41.0 cannot save any article into a wiki that holds a withdrawn article; after you update, reopen the running page (`shikii serve`), the MCP server and the agent sessions too (an old page does not list the withdrawn articles) |
| `shikii handoff list` / `read` / `write` / `append` | Reading and writing work records |
| `shikii guard list` / `add` / `enable` / `disable` / `try` | Guards |
| `shikii review query` / `submit` | Provisional assessments from conversations and results (usually done together in reflection) |
| `shikii reflect query` / `place` / `submit` | Reflection before ending (`place` shows where a draft lesson could go; `query session_id=<id>`, as a hook's text writes it, lists only what that session is asked) |
| `shikii check <spec>...` | For CI: fails on lines whose wording changed, hand-written lines, lines with too little precision and lines that lift an option no truth confirmed |
| `shikii drift <spec> --model M` | Checks whether a line has worsened on recent truths |
| `shikii bias <spec>` | Measures whether the order of options changes the answer |
| `shikii gc` | Cleans up old records without truths (`--apply` removes them) |
| `shikii forget <session id>...` | Lists, and with `--apply` removes, what finished sessions left in `.shikii/`; what goes is kept in `records/forgotten/` first. Over MCP (`shikii_wiki` `forget`) shikii asks the person in the client before it removes anything |
| `shikii nap` | The memory nap: lists, as JSON with `next_action`, the conversations not shown yet, the articles whose watched files changed and how far the review of the articles has gone, and records where it stopped in `.shikii/nap-window-end`. Edits no article. See [Memory nap](#memory-nap-shikii-nap) |
| `shikii hash <spec>` | The hash of the spec that keys calibration |
| `shikii version` | Prints the version |

## The serve HTTP API

The body is JSON. Without `-H 'Content-Type: application/json'` the request is refused with 415. `spec` is the id of `.shikii/decisions/<id>.yaml`.

| Endpoint | Body | Returns |
|---|---|---|
| `POST /v1/decide` | `{"spec","inputs":{…},"tainted":false}` | With a `request` input, also `similar_decisions` (the person's answers on similar requests, with the agent's and the model's) and `lessons` (the closest wiki lessons), when there are any (0.11). The decision (band, top, p, trace_id, receipt) |
| `POST /v1/label` | `{"spec","trace_id","truth","weight":1,"source":"ui"}` | The truth stored (a decision that went through the context rung gets it on both rungs) |
| `GET /v1/queue?spec=<id>` | | Unanswered (records waiting for a person and records waiting for a result; each item has `waits_for_person`; records that were redone are not listed) |
| `GET /v1/answered?spec=<id>&limit=30` | | Answered (people's truths only) |
| `POST /v1/redo` | `{"spec","trace_id"}` | Decides the same text again with today's wording and line and returns the old and new side by side (a new record; a person's answer is carried over) |
| `POST /v1/answers/restore` | `{"trace_id"}` or `{"stop":true}` | A person undoes the AI's rejection of one past answer (every record that carries it), or the stop of quoting past answers in this project. Only what is in place can be undone (otherwise 409; an unknown answer is 404). Not in MCP: it is a person's operation |
| `GET /v1/status` | | The state of each spec (whether it has a line, the number of truths, the question in people's words) |
| `GET /v1/tune?spec=<id>` · `POST /v1/tune/run` · `POST /v1/tune/variant` · `POST /v1/tune/adopt` | `{"spec","propose"}` · `{"spec","name","question","options"}` · `{"spec","run","meaning_same":true}` | The latest wording improvement and the current candidates · a run in the background · add a candidate written by a person (after validation; it is never adopted by itself) · adopt or reject |
| `POST /v1/tune/review` | `{"spec","run","keep":true}` | A person keeps (true) or undoes (false; the adopted file is renamed to `.reverted`, not deleted) a wording the agent adopted after checking its meaning |
| `POST /v1/tune/sync` | `{"spec","propose"}` | Runs the wording improvement in place and returns the result (used by the MCP tool `shikii_tune`; adopts nothing) |
| `GET /v1/wiki` · `POST /v1/wiki/claim` | | The list of the wiki's claims · change a claim's status (promote / delete; only a person changes it) |
| `POST /v1/wiki/notebook` | `operation: prepare / record / query / consult / reuse / organize / reconcile / ingest / patch / diagnose / compare / capture / classify / maintain / lint / handoff_list / handoff_read / handoff_write / handoff_append` | The project's wiki: search, store, review and lint sources and articles. Lessons are hypotheses. handoff_* read and write the work records (the same operations as MCP; the page itself only reads) |
| `POST /v1/wiki/article` | `action: edit / delete / restore`, `id`, `revision`; edit: `title`, `lesson`, `applies_to`, `body`, `kind`, `scene` | Edit or delete a current article in this project; restore or delete an article a work withdrew (`GET /v1/wiki` lists them under `withdrawn`). Stale revisions return 409, so a withdrawn article is restored or deleted only by the revision the page showed. Deletion stops search and delivery; sources and revisions remain. |
| `GET /v1/handoff/file?path=` | `path` (relative to .shikii/handoff/) | One work record. Images as images, text and HTML as text/plain, anything else as a download. Outside the folder is 400 |
| `POST /v1/review` | `operation: query / submit` | Provisional assessments from conversations and results. Fetches the originals, the targets and examples of corrections, and saves with evidence. People check samples and held cases |
| `POST /v1/reflect` | `operation: query / place / submit`, `reviews` / `wiki` / `memory` / `maintenance` | Fetch and save together: assessments, wiki work, observations of delivered memory and corrections, and article reviews (query lists the pending conversations oldest first: the oldest two with their raw text and up to 40 more as heads; the ones that did not fit are counted in more_pending and listed by the next query; it shows the oldest sources and review targets two at a time; submit takes any number of conversations, items and reviews and records a review of a page the same submit updates; what is left comes back as items; when extending an existing page, its sources are kept). Check `complete` and each `error` |
| `GET /v1/specs` | | The list of loaded specs |

Both decision lists return `next_cursor` when more records remain. Repeat the same endpoint and spec with `cursor=<next_cursor>` (and the desired `limit`, at most 1000). An invalid cursor returns 400; if the history or answers changed, the cursor returns 409 and the complete list must be read again from the start. The page reads these continuations automatically and shows records with the existing show-more controls.

```sh
curl -s localhost:8095/v1/decide -H 'Content-Type: application/json' -d '{"spec":"same-issue","inputs":{"report1":"…","report2":"…"}}'
curl -s localhost:8095/v1/label  -H 'Content-Type: application/json' -d '{"spec":"same-issue","trace_id":"<trace_id from the decide response>","truth":"yes"}'
```

## Code layout

| Place | Role |
|---|---|
| `internal/spec` | Reads and writes Decision Specs. The calibration key `Hash()` (from the question, template and options; thresholds and policy are not included) |
| `internal/systemone` | A thin client for the System One format (`POST /v1/systemone`). Jev and llama.cpp differ only in the base URL |
| `internal/decide` | Spec + inputs → band. A missing required input gives abstain, tainted removes hosted, **a decision without calibration never gives act** |
| `internal/calib` | Platt calibration (`calib.go`), vector scaling (`vector.go`), Brier / ECE / reliability diagram, AUC with its 95% interval (how well the answers are told apart) |
| `cmd/shikii` | The CLI |
| `cmd/shikii-readout` | **The adapter that turns an ordinary GGUF into System One**, `shikii-readout`. Reads the option letters' probabilities through the llama-server's `/apply-template` and `/completion` behind it. Has no inference and no calibration of its own |
| `cmd/fake-systemone` | A fake model to run the loop without a key or a GPU |
| `decisions/*.yaml` | Sample specs (in this repo; in your own project, copy them into `.shikii/decisions/`) |
| `testdata/` | Synthetic labelled data (62 requests, and 32 pairs each of bug reports and inquiries) and the calibration evidence for CI (`store/labels/`) |

## Not there yet

- Isotonic regression
- Reading keys from a store (pass them in environment variables; this process never touches a keychain)

## Knowledge found during work

The agent can now carry a useful manual discovery into later searches. A wiki query returns a lookup receipt for the articles actually supplied. When an article changes a decision, the agent records the context, decision and evidence with its normal reflection. Merely reading an article does not count as useful. The evidence helps retrieve that exact revision through the existing delivery gate; edits, replacements and deletion do not inherit its endorsement. Lookups and discoveries are bounded to the latest 64 and 128 records per project.

Articles can keep a short rationale: the conditions, choice, alternatives rejected and why, and observed result. The agent writes it from sources and leaves unsupported reasons unknown. It accompanies the lesson in planning and automatic delivery, and the Knowledge page lets you read and edit it.

For facts or procedures that depend on project files, the agent can attach up to four files. Shikii records hashes and brings the article back for review when a file changes, disappears or cannot be read. The article and the next delivery show that its premise needs checking. A change does not automatically invalidate or delete a rule. The agent checks the evidence before updating it.

Planning can also return pending reflection in the same response and save both together. Acknowledgements without judgment targets or delivered memories do not load unrelated judgment examples or articles. Failed saves still return explicit errors and must be retried.

API additions: `wiki query` returns `lookup:{id,query,pages:[{id,revision}]}`. `reflect submit` accepts `discoveries:[{lookup_id,page_id,revision,context,decision,evidence}]` (each text up to 1000 bytes). Wiki drafts accept `rationale` (up to 1600 bytes) and `watch` (up to four relative regular files of at most 1 MiB each; hidden paths, links and outside paths are rejected). Omission preserves these fields; empty values clear them. `maintenance.changed_files` and article `recheck` expose changed premises; resending `watch` after checking establishes a new baseline. These records are agent observations, not human labels or authorization. The older `hook plan submit` accepts `reflection` with the ordinary reflection submit shape, within 1 MiB in total; check its nested receipt.

## Correct knowledge and check when it will be found

An agent can now change a specific passage without resending the entire article. It first reads the article and its revision, then supplies the passage to replace, its replacement, supporting sources and a reason. If another edit arrived meanwhile, or the passage is ambiguous, the change is refused. Other sections, evidence, reasoning and watched files remain intact.

The agent can also inspect why an article did not arrive: it did not match the request, was below the candidate limit, had already arrived recently, was suppressed by earlier evaluations, or was ruled out by the decision model. The explanation uses the record from that moment. Earlier activity without a record remains unknown.

Before saving, the agent can try the proposed edit against up to eight recorded situations. The comparison keeps the old requests, article revisions and search context fixed, and shows which retrieval candidates change, including other articles displaced by the edit. It writes nothing. Becoming a candidate does not establish actual delivery or usefulness; session suppression, feedback and the decision model are not rerun. You do not need to prepare examples or score these comparisons.

The CLI operations read JSON from stdin. `wiki patch` accepts `{id,revision,sources,change_reason,edits:[{field,before,after}]}`; `wiki diagnose` accepts `{id,case_id}` (both optional); `wiki compare` accepts `{patch,cases}` (cases optional, at most eight distinct recorded ids); `wiki withdraw` accepts `{id,revision,sources,reason}` (reason up to 900 bytes; see the table of commands). MCP has patch and compare under the same names (diagnose runs only in the CLI and HTTP): patch is nested under `patch`, and compare keeps its fields. Changes are exact text replacements, at most 16 per patch. See the [agent procedure](../skills/shikii/references/wiki.md#precise-edits-delivery-diagnosis-and-comparison) for limits and results. Diagnostic history is local, bounded to 32 attempts / 4 MiB; incomplete or missing history is explicitly unavailable for comparison. These features are available in 0.17.0.

A partial edit has one article publication point. Validation or revision conflicts leave it unchanged. If a later index, log or source-review update fails, the reply includes the saved article and recovery warnings; reread that revision and follow the warnings instead of retrying the old patch. Discovery evidence belongs to an exact revision, so the comparison does not transfer its endorsement to a new draft.

### Share changes and compare concurrent work (0.23.0)

Work preparation now adds `collective`: changes to articles already supplied to
this work, explicit contrasts of conditions and evidence, and a related wiki
probe that can be checked during authorized work. Changes keep both revisions;
confirm inspection with `reviewed:["changeN"]`, and record actual usefulness
separately. Initial `knowledgeN` inspection can carry a baseline to the next
request in the same session, limited to that request's selected articles.
Shared sources are explicit; different source IDs do not prove independence.
Probe results return to the same question with their conditions and sources.

The added context shares 4 KiB, with at most two changes, one contrast and one
investigation. Omitted content retains exact queries; existing learning is
referenced rather than repeated. CLI, MCP and HTTP use the same operations, except that MCP leaves
reconcile, organize, diagnose, consult and reuse to the CLI and HTTP (over MCP, consult and reuse are
`proposal` and `conditions` on a prepared receipt).
`wiki reconcile` takes `{"patch":{...}}`, compares base/current/proposed without
saving, and returns a guarded patch only for a ready proposal. Conflicting text
or structured metadata remains explicit. Inspect and save through ordinary patch;
a newer revision refuses it. The Knowledge editor offers comparison after a
stale save and preserves the unsent input. See the [agent procedure](../skills/shikii/references/learning.md#work-together-on-the-same-knowledge).
Updating from 0.22.0 preserves project data. Update every shared writer to
0.24.0 or later and restart agent sessions, the page and MCP before using the new work
context; older writers do not preserve it. Keep a copy of `.shikii/` if you may
need to restore the earlier binary and data together.

### Less recording work

With a request hook, preparation and its evidence are delivered automatically;
the agent can finish ordinary work in one record call. Without a supplied
preparation, call prepare first. Record takes
result:{changed,checked?,evidence?:[refs],remaining?} with any needed notes/reflection.
Reference checked evidence once. No new knowledge means no wiki draft. An original
report is saved before placement; saved-pending identifies remaining organization
and failed refs can be retried without resending it. Internal scope/completion
grades do not ask people to score agents. A concrete task choice may still need
the person's answer.

Optional max_bytes bounds a prepare response (28672 bytes by default on the CLI; a longer request is not cut, and the reply grows to hold it; the hook keeps 64 KiB of a request, and the work it prepares fits, with the rest of its context, within the 10,000 characters Claude Code shows whole, the same for Codex and OpenCode: a request too long for that comes in `task` as its head, with `task_context.text_status` `delivered_head`, or `stored_request_truncated` when it is longer than the 64 KiB the hook keeps, while the work is matched on the request the hook keeps); omitted bodies retain exact recovery
queries. Sentence-boundary excerpts and same-receipt body or identical-excerpt reuse
reduce rereading; reused excerpts never establish a full-body read.
Identical host reading notices can be shared verbatim in `reading_notices` within
the same response; each `reading_notice` points there. Article text, quotations,
incomplete status and exact recovery queries are retained. Sharing is used only
when the complete transport response becomes smaller.
Memory unknown/unused assessments need no invented explanation. A shipped memory
policy upgrade shows Upgrading memory… and lets the installed agent review existing
knowledge in resumable batches, preserving sources, preferences and history. Ordinary
tasks review at most one batch of 10 articles, then resume on the next task. An explicit
request to finish the upgrade can continue the remaining batches. When the 10 articles do
not fit max_bytes (the CLI's default bound often cannot hold 10 full articles), prepare
leaves out first the passages of each article, then its body (each article keeps its
references, its task and the exact query that reads it whole), then its lesson, and last the
final articles of the batch. The articles left out were not reviewed, so the next prepare
returns them first, and `output.omitted` says what was left out.
Related-page version lineage alone does not reopen a reviewed article when the
related page's entire stored content, conditions, sources and edit reasons are exactly unchanged.
The edited article itself still needs review. New evidence, changed premises and
30-day checks remain active. Older checkpoints keep their previous behavior until
a real review supplies comparison evidence. Submissions still require current
revision and key values, including the current related revisions.
See [the recording contract](../skills/shikii/references/recording.md#prepare-once-record-the-result).

## Passing on what was left and what the wiki could not answer (0.38.0)

### Examined areas and what was left

The `result` of `work record` takes `coverage`. A row is `{area, outcome, evidence?:ref, reason?}`, at most 16 rows. `area` is a short name of what was examined (one line, at most 160 bytes), and `outcome` is one of four:

| `outcome` | Meaning | Written with it |
| --- | --- | --- |
| `reported` | a problem was found and reported | nothing |
| `clear` | examined, no problem found | `evidence` (one of `result.evidence`; `current` is not accepted) |
| `excluded` | left out on purpose | `reason` (at most 300 bytes) |
| `open` | not examined | nothing (it cannot be sent with `sufficient:true`) |

An `open` row stays in the project's records (`.shikii/records/coverage/`) and reaches the `coverage` of later `work prepare` replies. `rows` names the five newest, and `open` is the number of open areas. When a later result records the same `area` as `reported`, `clear` or `excluded`, that area alone is closed. When the size limit of a preparation is tight, `rows` first names fewer areas (the newest, at least one) so that no article loses its text for them; `open` still counts every open area, the areas that were not named stay open and reach a later preparation, and only the named ones, and the ones the same session wrote as `remaining`, are handed over. When even one row does not fit, the reply is shortened as usual, and `coverage` can be left out as a whole and named in the reply's `output`. The ledger is read from its newest 4 MB. Each time a result writes to the ledger, the open rows that are about to leave that part are written again at its end, so an open area stays listed until a result closes it (as long as the open rows together fit in the part that is read). When the open rows together come near 3 MB, the ledger grows fast with every record. A row is the agent's report of what it looked at. It is neither a guarantee that the area has no problem nor a permission.

The `remaining` of a result whose `coverage` has no `open` row becomes one `open` area (runs of white space and line breaks become one space). When the result wrote a row of the same `area` itself, that row stands and no area is made from `remaining`. Fixed phrases such as `none`, an empty text and punctuation alone make none. A text over 160 bytes is cut and ends in `…`. The next `work prepare` hands the row over with `from:"remaining"`. When the task that was handed it (the row was in its preparation's reply) records a result, the row is closed. If that task records nothing, the next preparation lists it again. The next result of the same session closes it too. If it is still left, that task writes it again in its own `remaining`. The same text written again stays one open area. The record's reply says `coverage:{rows, open, handed_over?}`. The names are those of the preparation's reply, but the shape differs: `rows` is the number of rows sent, `open` lists the names of the areas this result opened, and `handed_over` lists the `from:"remaining"` areas this record took over and closed (an area closed by a row of the same `area` is not listed).

### What the wiki could not answer

When a search by words (a `wiki query` with words, or the `query` of a `work prepare` that carries a receipt) finds no candidate, the words are remembered: one entry per set of words, at most 64. Words that differ only in upper and lower case, in white space, or in how a character is composed (a letter followed by its combining mark, or the one composed character) are the same words. `lesson:<id>`, ids alone, a continuing cursor and words over 200 bytes are not remembered. When a `work record` with a result is complete, its reply lists up to three of them since the task started in `missed_searches:{queries, next_action}`. A search made through a receipt belongs to the session of that receipt: only the records of that session list its words. A `shikii wiki query` run as a command by Claude Code, Codex or OpenCode belongs to the session it was run in when the id the agent puts in the command's environment (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` or `OPENCODE_SESSION_ID`) is of a session the hook kept a record of in this project; if none names such a session, or two of them name different sessions, it belongs to none. An OpenCode subagent runs in a session of its own that the hook keeps no record of, so a search run inside a subagent belongs to none either. A `wiki query` over MCP or HTTP belongs to none (the server was started before the session that asks now), so the words that a query with no session missed are listed by a task that was open then. A `wiki query` without words (the maintenance list) lists up to eight rows of `{query, at, result?}` in `missed_searches`. The list searches again when it is shown and leaves out words that now find a candidate because an article was added. One call searches again for at most the 16 newest.

A search splits its words into small units and compares them with the articles (a word of letters and digits is one unit; kanji and kana are taken two neighbouring characters at a time). A search of two or more units returns an article only when at least two of the units are in it. Alternatives for one thing, put into one search, can therefore find nothing although one of them is in an article. When a search of several words finds no candidate and some of its words find an article when searched alone, the reply carries `found_alone:{words, next_action}` (since 0.39.0; up to three words in the order searched; the first eight words are looked at; a preparation leaves it out when its size limit is tight). Searching one of those words returns the article; the articles themselves are not in `found_alone`. Search alternatives one word at a time. Put two words into one search only when both are likely to be in the same article.

When a record with a result completes, the words that found nothing since the task started are tied to the id of that result when they find its text. The candidates are the 16 newest of those words, including the ones the reply did not name in `missed_searches`. The text is what the agent wrote in the result (the contents of `changed`, `checked` and `remaining`, and of `area` and `reason` in its `coverage` rows), and the rule is that of a word search of the wiki. A result that says nothing of the words is not tied (since 0.39.0). Words that 0.38.0 tied keep pointing at the earlier record until the same words find an article or a record that speaks of them ties them again. When the same words find nothing again, the reply carries `earlier_search:{query, at, result, next_action}` (a preparation leaves it out when its size limit is tight). For words searched through a receipt, or by a `wiki query` that belongs to a session (above), the result tied is that of a task of the session that searched. For words searched by a `wiki query` that belongs to none, it is that of a task that was open when the words found nothing and later recorded a result that speaks of them; it need not be the task that searched. Passing the `result` id to a search as it is (`shikii wiki query <id>`, or the `query` of `operation:"query"` over MCP) returns the result the earlier task recorded. It is that task's report, not a verified article: confirm it before relying on it.

A `wiki query` made only of ids (one or more 16-digit hexadecimal ids separated by white space) reads saved sources over the CLI, MCP and HTTP alike. The `query` of a `work prepare` that carries a receipt reads the source when it is one id. To read an article, write `lesson:<id>`.

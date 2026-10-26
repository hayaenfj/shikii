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

## Install and update

To install, use the one-line curl command in the [README](../README.md#get-started). To install from source, run `sh install.sh` in the folder where you cloned shikii. Both need Go 1.27 or newer and Git.

```sh
shikii update
```

This fetches shikii and readout for your OS and CPU from the latest stable GitHub Release. Go, curl and GitHub CLI are not needed. It checks SHA256SUMS, the attached hashes and the version the new binary reports, then updates the files in the same folder as the real file of the running shikii. It never installs into a different place on your PATH instead. A release that is the same as or older than the current version is not installed.

The licenses and third-party notices are kept in `shikii-licenses/` next to the binary. A missing file in the release or a hash that does not match leaves everything as it was, and a failure in the middle of writing restores shikii and readout. Two updates at the same time are refused. If you cannot write to the folder, the update says so.

If hooks that use this binary are already installed, the new binary updates the hooks and the agent's instructions. Hooks that were not installed are not added. If only this last step fails, the message says that the binary itself was updated, and you can retry with `shikii hook install --yes`. A running page or MCP server is not stopped, so restart it to switch to the new version. The updated skill and hook instructions take effect in a new agent session. Existing projects keep their questions and calibration; projects initialized from now on get the new version's built-in prompts.

**Updating from 0.8 to 0.9 (0.9.0 or later):** articles given a kind and scene by 0.9.0 look "changed" to a 0.8 binary, and 0.8 refuses every save to the wiki (the articles are not damaged). After updating, reopen the open agent sessions (and their MCP). Update every machine that uses the same wiki to 0.9.0 or later (to 0.9.2 or later once any machine runs 0.9.2: 0.9.1 and earlier read articles written by 0.9.2, whose fixed words are English, as changed). The update also prints this note at the end. From 0.9.2 shikii does not write into AGENTS.md; the update removes the shikii section an earlier version wrote in each registered project, and keeps the rest of the file (only when exactly one section is enclosed by marker-only lines; a link pointing outside the repo, or one marker or three or more, is left alone and the reason is printed; CRLF files stay CRLF).

**Updating from 0.6.0 or earlier erases the contents of `.shikii/` in every initialized project and initializes them again** (a project decision). Earlier conversations, lessons, provisional assessments, truths, calibration and work records are removed, and the built-in questions, the wiki templates and the project marker are created again. Back up anything you want to keep first. Updates from 0.7.0 onwards keep everything. When you update from 0.6.0, the wipe runs inside the old binary's last step and its messages are not shown (0.6.0 throws that output away). Run `shikii hook install` without `--yes` first to see what would be erased (it also shows how many work records there are).

## Enable it per project

After installing, run `shikii --init` (`shikii init` is the same) once in each project where you want to use shikii. You can also ask your agent: "Use shikii in this project." This prepares the built-in questions (the 4 hook questions and the 2 wiki questions), the wiki, the places for guards and work records, the MCP setup, the agent's instructions and the way to the page, and writes `.shikii/project.json` last. Hooks run only where this marker exists; in a project that was not initialized they create no storage and no Git exclusion. Subfolders use the same project root, and initializing again keeps the existing settings, conversations and truths. `shikii doctor` checks the setup.

What is inside `.shikii/`:

| Place | Contents | Commit |
|---|---|---|
| `decisions/` | Questions for the decision model | Yes |
| `wiki/` | Lessons, conditions, earlier revisions | Yes |
| `guards/` | Guards made from people's corrections | Yes |
| `labels/` | Truths given by people and by results (an asset that survives a change of model) | Yes |
| `records/` | Results of conversations, decisions, assessments, deliveries, observations and guards. Grows with every decision | Optional (`shikii gc` removes old records without truths) |
| `sessions/` | Per-session notes (lessons delivered, notices given, files changed) | No |
| `handoff/` | Work records (audit reports, release steps, diffs, screenshots) | Only what you want to keep (`shikii gc` never touches it) |

Initialization keeps `.shikii/` out of Git's untracked files, because it contains private conversations. To commit something, remove it from the exclusion and add it yourself.

**Moving or renaming the folder:** nothing to fix (0.9.7). Inside `.shikii/` everything has fixed names, so the wiki, its sources and the records move with the folder. The next request's hook reconnects the project at its new path by itself and says so once (`shikii --init` there does the same). A copy whose original is still in place is not taken over (records keep going to the original); `shikii --init` in the copy makes it a project of its own. A `.shikii` that this machine did not initialize at the old path, such as one from an archive or another machine, stays inactive until `shikii --init`. Stores written before 0.9.7 move to the fixed names the first time they are opened. The agents' own records (Claude Code's `~/.claude/projects/`, Codex's `[projects]`) are keyed by the path and are not moved; see the habit guide's [references/wiki.md](../skills/shikii/references/wiki.md), "Moving or renaming the project folder".

To change the answer page itself (`web/`, Svelte + Tailwind CSS) you need Node 24 (`make web`). Just using shikii does not: the built page is committed and embedded in the binary.

## Use it in ordinary conversations

In Claude Code and Codex, hooks before the request and after the work (in Claude Code also just before a subagent is launched and when it starts: 4 in all; Codex has 2) record the conversation, look up the memory and answer the built-in questions. Your usual agent reads the request, the reply and the next human message and merges them into knowledge on the same topic. It keeps a short summary for searching apart from a body that holds the background, the reasons and what is still unknown, and links related pages. `shikii wiki query lesson:<page id>` reads a whole page, and topic searches include the body. See the [editing steps for agents](../skills/shikii/references/wiki.md).

Up to 2 short quotes, within 1800 bytes, are delivered before similar requests in the same project, and the same revision is not repeated in the same session for an hour. History (`history`) articles are not delivered automatically (they explain why something became so, and are read when searched for). Claude Code subagents get lessons in the same way: the task the parent hands to the Agent tool is kept just before the tool runs (`PreToolUse`, Agent / Task only), and when the subagent starts (`SubagentStart`) lessons are chosen by that task. Without a kept task, lessons are not chosen by the parent's request; only rules whose scene is "いつも" (always) are delivered (choosing by the parent's request delivered lessons unrelated to the subagent's task). The parent's state is left untouched except for removing the used task, and the delivery is recorded under its own ID. Codex has neither event. No separate model is started to organize the conversation. Recording and looking up the conversation work even when the decision model is not available. This is neither a way to block tools nor a way to grant permission to run them (only the guards and the routine below stop anything, and both only make the agent fix the work at the end of the turn). See [the hooks in detail](../integrations/claude-code.md).

Whether a delivered lesson changed what the agent did is observed in reflection (helpful / unused / harmful / unknown). When a correction was delivered, whether the agent followed it or repeated the mistake is also kept (`correction_result`: followed / repeated / not_applicable / unknown). Success, a follow-up request or silence alone are not counted as an improvement. The numbers appear on the `memory:` line of `shikii status` and on the page.

## Guards

Corrections a machine can check become guards (`.shikii/guards/<id>.yaml`). There are three kinds: `paired-files` (when one file changes, its partner must change too), `forbidden-words` (words that must not appear in added lines) and `commit-body` (words that must appear in the body of the commits made in the turn). At `Stop`, the enabled guards are applied to the changed files, the added lines and the commits since the HEAD recorded at the request. When one fails, it returns `decision: block` with the reason and the agent fixes the work. The `Stop` after a stop is never blocked again; whether the work was fixed is recorded in `records/guards.jsonl`.

```sh
shikii guard add < guard.json       # a draft (disabled). JSON: id, kind, title, reason, source, pairs/words/except/allow/required
shikii guard enable <id>            # shows it stopping on the negative example (RED) and passing on the positive one (GREEN), then enables it
shikii guard disable <id>           # disables it again (the file stays)
shikii guard list                   # state and last result
shikii guard try                    # apply to the current working tree and show the result (commits are not checked)
```

The agent places the draft and a person enables it (the steps are in [the skill's guards.md](../skills/shikii/references/guards.md)). The `guards:` line of `shikii status` shows how many are enabled, how many turns were stopped and how many were fixed.

## The routine before and after work

In every turn, two steps are checked by built-in rules. They are always on and cannot be turned off (`shikii guard list` shows them as `built in (always enabled)`). They work in projects without `CLAUDE.md` or `AGENTS.md` too.

- **Planning before work (plan-before-work)**: in a turn where the request hook asked for planning, before changing files the agent reads this and the recent conversations with `shikii hook plan query`, searches the wiki with topic words it picks, and saves what was agreed and what is still open with `shikii hook plan submit`. A query without search words answers that the wiki has not been searched yet. The plan is saved even when the decision model is down.
- **Reflection before ending (reflect-before-end)**: in a turn where the request hook named conversations to reflect on, before ending the agent reads the originals, the delivered memory, the lessons and people's corrections with `shikii reflect query`, and saves the assessments (reviews), the wiki work (wiki) and how the delivered lessons did (memory) with `shikii reflect submit`.

```sh
shikii reflect query
shikii reflect submit < reflection.json
```

`reflection.json` is JSON with `reviews`, `wiki`, `memory` and `maintenance` for the targets you fetched. Check `complete` and each item's `error`, fix only what failed and send it again. See [input examples and the steps](../skills/shikii/references/review.md); the shape of a plan is in [planning.md](../skills/shikii/references/planning.md).

When the agent tries to end with a step missing, `Stop` stops the turn once and tells it the command to run. The second time it does not stop (also after a background task's completion notice), but the miss is recorded in `records/guards.jsonl` as `plan-before-work` / `reflect-before-end`. It shows on the `rituals:` line of `shikii status` (how many of the requested steps were done, and how many were done after a stop) and on The AI page. The message on a stop asks the agent to report, in its last reply, the result of the work the person asked for rather than the routine itself.

So that the agent learns these habits even in a project without working rules, the hook adds one line once per session, in a turn where it has something to say (reports and working files go to the work records; rules and corrections the person states go straight into wiki lessons). Subagents are told where to write their reports even when no lesson is delivered.

## Work records

Working files the agent keeps editing (audit reports, release steps and logs, diffs, screenshots) live in `.shikii/handoff/<task>/`. They do not clutter the top of the repository and they stay after the session ends. People read them on the page under Knowledge → Work records (text is shown as text and images as images; HTML is not rendered).

```sh
shikii handoff list                                    # list (newest first)
shikii handoff read audit-0.9/report.md                # read (text up to 4 MiB)
shikii handoff write audit-0.9/report.md < report.md   # replace (UTF-8, up to 1 MiB per write)
shikii handoff append audit-0.9/log.md < more.md       # append
```

In MCP, these are `handoff_list`, `handoff_read`, `handoff_write` and `handoff_append` of `shikii_wiki`. Paths are relative to `.shikii/handoff/` (a leading `.shikii/handoff/` is also accepted). `..`, absolute paths, hidden names, control characters, links to files or folders, and hard links are refused, so nothing goes outside the folder. A replacement always goes through a temporary file with a new name and is then renamed (temporary files left by a crash in the middle of a write are cleaned up, if older than a day, the next time something is written to that folder).

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

Articles made before 0.9.0 have no kind or scene. The request hook lists five of them at a time and has the agent set them (`shikii wiki classify <page ID> <kind> <scene>`, or `operation: "classify"` on MCP's `shikii_wiki`); if the agent tries to end without setting them, Stop blocks once (the routine check `wiki-kinds`). The lesson text is not changed, and the result is kept as a new revision. If another agent edits the same article while the kind and scene are being set, classify refuses; read it again and set them again (an edit is never undone by the old lesson). While the wiki has an unreadable or hand-edited article, every save is refused, so the hook asks for no articles and only says, in one line, to check with `shikii wiki lint`. The reply of `wiki query` starts with how many remain and the next five, and the replies of `wiki ingest`, `wiki classify` and `reflect submit` also carry what remains. Agents without hooks (Codex before it trusts the hooks, OpenCode) are told the same by the MCP instructions and by the skill, which is put both in `~/.claude/skills/shikii` and in `~/.agents/skills/shikii` (read by Codex). shikii does not write into AGENTS.md.

Listing with `wiki query` and `reflect query` include up to 2 articles to review under `maintenance`. Read the body, the sources and the related pages, save the edits that are needed, then pass the latest `page_id`, `revision` and `key` with your `reason` in `maintenance.json`. You may also put them together in the `maintenance` array of a reflection. When an article or a related revision changes, the old review is refused; an unchanged article comes back for review after 30 days. A missing body or link is a reason to look, not an instruction to add text or links that are not needed.

`hook plan query` before work can pick the bodies you need with `pages`. The decision keeps the candidates, the articles read, the revisions quoted, the revisions available at the time and the amount fetched. Past comparisons use only the recorded revisions, without mixing in later wiki updates or later outcomes. The numbers of saves, searches and quotes are kept apart from observations that something helped a decision. The full contract is in [the wiki steps](../skills/shikii/references/wiki.md).

An explicit search covers all articles and returns `search.total` (the number of valid matching articles) and `search.candidates`. A large response is split by the size of the JSON data; read the rest with the same search words or source IDs and `continue:CURSOR` on the CLI, or `cursor` in MCP and HTTP. A cursor whose project, conditions or results have changed is refused. A single large conversation source is returned on its own; when its long text (`originals`) does not fit in one reply, it comes in parts with `original_part` (`id`, `from`, `to`, `bytes`, `json`), and the `json` pieces joined in order are the JSON of that conversation's originals. A captured source whose body does not fit in one reply comes in parts with `observation_part` (`id`, `from`, `to`, `bytes`), and the parts join back into the body. The search matches words, so when the results are off topic, change the search words.

### Move an existing memory into the wiki

If the agent already keeps a memory (Claude Code's auto memory folder with `MEMORY.md` and one file per fact, or Codex notes), one request moves it: "move the memory into shikii's wiki". The skill's [import-memory.md](../skills/shikii/references/import-memory.md) gives the steps: keep every file as a source (`shikii wiki capture`), draft one lesson per rule with its conditions, kind and scene (a rule you were told is a "rule", steps are a "howto", facts are a "fact", and so on; prefer the scenes in use that `place` returns), merge them into existing articles through reflection's `place` step so nothing is duplicated, and keep every memory file with its body as it is, adding only a line near the top that points at the wiki article (`MEMORY.md` keeps its lines and gets the article ids). Rules that apply to every request (the language of visible text, words not to use, when to push) also keep one line each in the index, because the wiki delivers lessons by how close they are to the request. Nothing is deleted, emptied or rewritten (old files are deleted only by the person). The steps are part of the skill that `shikii --init` places, so they work in every initialized project. When files without a pointer to `.shikii/wiki <article id>` remain in that folder (or in Codex's shared `~/.codex/memories`, among the files that name this project), the request hook notices once per session and tells the agent to move them after the current request. For files shared by all projects too, the body is kept and only a line pointing at the article is added.

## The page and the inventory

There is one answer page per project. If the project's page is not running, the first request's hook starts `shikii serve --store <project>/.shikii --port 0` (it takes a free port) and opens it in the browser. The port and pid are kept in `~/.shikii/agent/pages/<project id>.json`, so several agents in the same project share one page. `shikii page` tells you where the page is (`page: http://127.0.0.1:<port>/ (port …, pid …, up)`; if it is not running, it starts it and waits). Open the page with the names `127.0.0.1`, `localhost` or `::1` (other Host names are refused with 403).

The page has three parts, Answer, Knowledge and The AI, picked with the switch at the top. On first open it starts on Answer when a decision waits for a person, otherwise on Knowledge, and the part you picked opens next time.

Answer merges what waits for a person across the questions a person answers (the kind of request, whether to ask before starting, whether the changes were in scope, whether the request was done) into one queue and shows one card at a time. The question appears at the top of the card and above the buttons; inside are the request, the last reply, the changed files and the person's next message; the answer buttons stick to the bottom of the screen. Keys work too (1-9 to pick, Enter for the same as the AI, → to skip). The count waiting is shown beside Answer. Questions only the AI answers (wiki conflicts and placement) and records whose truth comes from results are not shown. A wording appears here only when the agent switched to a new one (keep it or undo it, with translations) or could not decide whether it means the same (with translations and its reading of the difference). Arriving through the hook's "answer later" URL puts that decision first.

The AI is where things you do not need to watch live. One line per question shows how far it has learned; pressing it opens that question (progress, the last calibration, lanes for unanswered, settled-by-results and answered items, changing an answer, wording candidates). The lessons delivered and whether they helped, the routine before and after work, the guards and their last result, the decision model in use and how often decisions wait for a person are here too.

Knowledge has two tabs, "Lessons" and "Work records". At the top of the Lessons tab, "Rules decided in this project" lists every rule, set by people or by the AI, whatever scene is picked, so none gets buried: "always" rules come first, then the newest, and rules added or changed in the last 7 days are marked "new". Below it, pick a scene on the left; the middle lists titles by kind, and pressing a title shows on the right the lesson, when it applies, the background, related articles and sources. With no scene picked, each kind shows its four most helpful lessons, and "show N more" opens the rest. Articles without a kind and scene are grouped under "Unclassified", which the agent reduces with every request. "For you to look at" at the top shows only the exceptions a machine cannot decide: a lesson that did harm, a repeated correction, and a guard not yet enabled. It is not built for a person to check articles one by one. The counts of searches, reviews and reflections, the tree of earlier articles and earlier claims are inside the folded "Detailed records of the articles".

### Inventory: `shikii status`

```sh
bin/shikii status --store .shikii .shikii/decisions/prompt-kind.yaml
# prompt-kind: records=1 labels=1
#   by source: outcome=1   by truth: !edit=1
#   jev-1.14.0 @ 68ed82631e63f2a2   1  collecting (min_n 50)
```

For each spec it prints the numbers of records and labels, where the labels came from (ask / outcome / file) and how their contents lean, and for each (model, spec_hash) the count of truths that calibration uses (the latest per input, from people and results; the same number as the page) and the state
(`calibrated: vector` / `collecting (min_n …)` / `only one answer so far` (no line until both answers appear) / `tried: the answers could not be separated` (tried again as truths grow) / `enough truths: the line is drawn automatically` / `stale hash`). This is the first thing to look at when running shikii.

The `person's work` line shows how many decisions a person answers under the current rules (unsure decisions that results do not answer, samples of automatic decisions above a line, samples and held cases of the AI's provisional assessments), in total and per 1000 decisions over the last 7 days, and counts separately whether the truth was decided by a person or by a result. `left to a person` under `rungs` is the number of unsure decisions; unsure decisions that a result answers do not wait for a person. Compare this line before and after a change to see whether it reduced people's work.

There are also the `memory:` (lessons delivered and how they did), `guards:` (how many are enabled, turns stopped, how many were fixed), `rituals:` (the routine before and after work) and `page:` (the page's URL) lines.

When a line is newly drawn, or when the truths reached `eval.min_n` but no line can be drawn (only one answer so far, or the answers could not be separated when tried), the Stop hook tells the person once at the end of the turn (Claude Code shows it in the terminal). It is said once per question, again only when the state, the wording or the model changes, and the page shows the same reason under the AI's state and in the question's panel.

## Decision models

### The recommended model (TypeSafe Jev)

```sh
export TYPESAFE_API_KEY=...       # the key is read from the environment only; shikii does not read a keychain
shikii doctor                     # when hosted shows "key present", the hooks, the built-in questions and pii: false decisions go to Jev
```

The recommended model is TypeSafe's Jev. With the key, the hooks and the built-in questions go to Jev and no local model is started (`shikii up` refuses too, nothing to download). To use a local model while keeping the key set, use `SHIKII_MODEL=local`; to force Jev, use `SHIKII_MODEL=hosted` (the same for the CLI, MCP, `serve` and the hooks). `pii: true` decisions are only ever sent to a local model, key or no key, so to answer them with a local model while the key is set, start it with `SHIKII_MODEL=local shikii up`.

### The local model when you cannot set a key (llama.cpp)

```sh
brew install llama.cpp            # the official Homebrew formula
llama-server --version
shikii up                         # starts llama-server on :8086 and shikii-readout on :8080 (downloads about 7.6 GB the first time)
```

The default model is `Ternary-Bonsai-27B-Q2_g64.gguf` from `prism-ml/Ternary-Bonsai-27B-gguf` (an ordinary chat model; `shikii-readout` reads the probabilities of the option letters, so it answers at the speed of a decision model). To install llama.cpp, see the [Homebrew formula](https://formulae.brew.sh/formula/llama.cpp); for other systems, see the [official instructions](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md). `shikii up` and the built-in hooks also use this model when there is no key. It uses about 10 GB of memory while running, which can be heavy for an ordinary PC. For a trained decision model such as OpenJev, use `shikii up --hf <repo:quant>` (readout passes its answers through unchanged). Kev-4B is not supported.

The model name that is the calibration key is the `model` in the response, not the name you sent with `--model`.

### Endpoints

Business `decide`, MCP and `serve` use `POST /v1/systemone` in the System One format. This is not the same thing as an OpenAI-compatible chat API. If the URL you give the CLI has a path, that path is used; if it has none, `/v1/systemone` is added. The response must contain `answers` with probabilities for each question. `shikii-readout` is a converter that uses llama.cpp's `/apply-template`, `/tokenize` and `/completion`; it is not a converter for any chat API.

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

### Cleanup: `shikii gc`

Records (`records/`) grow with every decision. Labels are calibration material and are never thrown away, but old records that never got a label are not needed.

```sh
bin/shikii gc --store .shikii                 # show only (default: records older than 30d without a label are candidates)
# prompt-kind: would remove 120 unlabeled record(s) older than 30d; keep 48 labeled, 15 recent, 0 unparsable
# dry run: nothing was changed. Re-run with --apply to remove them.
bin/shikii gc --store .shikii --apply         # actually remove them
bin/shikii gc --older-than 1w .shikii/decisions/prompt-intent-v2.yaml       # choose the spec and the age (d / w / h; 0 means any age)
```

Only "records older than `--older-than` that have no label at all" are removed. Labelled records, labels and lines that cannot be read as JSON are kept whatever their age.
Files are written in the same directory and then replaced by rename, so the original file is not broken if this stops halfway.

`make status` / `make drift` / `make check` / `make gc` / `make install` / `make server` are also available (`Makefile`).

## Every command

In the same order as `shikii --help`. For all the flags, run `shikii <command> --help`.

| Command | What it does |
|---|---|
| `shikii init` (`shikii --init`) | Enables this project (built-in questions, wiki, places for guards and work records, MCP, the agent's instructions, the page). Safe to run twice |
| `shikii new <id>` | Creates `.shikii/decisions/<id>.yaml` by asking a few questions (`--from` copies a sample) |
| `shikii up` | Starts the local model (llama-server, plus `shikii-readout` for an ordinary GGUF). Starts nothing when a key is set |
| `shikii serve` | The HTTP API and the answer page |
| `shikii mcp` | The same as an MCP server over stdio (`shikii_specs`, `shikii_decide`, `shikii_label`, `shikii_status`, `shikii_tune`, `shikii_wiki`, `shikii_review`, `shikii_reflect`) |
| `shikii doctor` | Checks the key, the local model and the store |
| `shikii update` | Updates shikii and readout to the latest stable release |
| `shikii hook install` / `uninstall` | Adds or removes the Claude Code and Codex hooks (without `--yes` it only shows what it would do) |
| `shikii uninstall` | Removes what shikii put into the agents (hooks, MCP entries, the agent's instructions) and stops the pages. The data (`.shikii/`, `~/.shikii/agent`) and the binaries stay; it prints the `rm` command for them (without `--yes` it only shows what it would do) |
| `shikii hook plan query` / `submit` | Planning before work (used by the agent) |
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
| `shikii wiki query` / `capture` / `ingest` / `classify` / `maintain` / `lint` / `verify` | Searching, sources, saving, setting kind and scene, reviewing and checking the wiki, and re-checking claims |
| `shikii handoff list` / `read` / `write` / `append` | Reading and writing work records |
| `shikii guard list` / `add` / `enable` / `disable` / `try` | Guards |
| `shikii review query` / `submit` | Provisional assessments from conversations and results (usually done together in reflection) |
| `shikii reflect query` / `submit` | Reflection before ending |
| `shikii check <spec>...` | For CI: fails on lines whose wording changed, hand-written lines and lines with too little precision |
| `shikii drift <spec> --model M` | Checks whether a line has worsened on recent truths |
| `shikii bias <spec>` | Measures whether the order of options changes the answer |
| `shikii gc` | Cleans up old records without truths (`--apply` removes them) |
| `shikii hash <spec>` | The hash of the spec that keys calibration |
| `shikii version` | Prints the version |

## The serve HTTP API

The body is JSON. Without `-H 'Content-Type: application/json'` the request is refused with 415. `spec` is the id of `.shikii/decisions/<id>.yaml`.

| Endpoint | Body | Returns |
|---|---|---|
| `POST /v1/decide` | `{"spec","inputs":{…},"tainted":false}` | The decision (band, top, p, trace_id, receipt) |
| `POST /v1/label` | `{"spec","trace_id","truth","weight":1,"source":"ui"}` | The truth stored (a decision that went through the context rung gets it on both rungs) |
| `GET /v1/queue?spec=<id>` | | Unanswered (records waiting for a person and records waiting for a result; each item has `waits_for_person`; records that were redone are not listed) |
| `GET /v1/answered?spec=<id>&limit=30` | | Answered (people's truths only) |
| `POST /v1/redo` | `{"spec","trace_id"}` | Decides the same text again with today's wording and line and returns the old and new side by side (a new record; a person's answer is carried over) |
| `GET /v1/status` | | The state of each spec (whether it has a line, the number of truths, the question in people's words) |
| `GET /v1/tune?spec=<id>` · `POST /v1/tune/run` · `POST /v1/tune/variant` · `POST /v1/tune/adopt` | `{"spec","propose"}` · `{"spec","name","question","options"}` · `{"spec","run","meaning_same":true}` | The latest wording improvement and the current candidates · a run in the background · add a candidate written by a person (after validation; it is never adopted by itself) · adopt or reject |
| `POST /v1/tune/review` | `{"spec","run","keep":true}` | A person keeps (true) or undoes (false; the adopted file is renamed to `.reverted`, not deleted) a wording the agent adopted after checking its meaning |
| `POST /v1/tune/sync` | `{"spec","propose"}` | Runs the wording improvement in place and returns the result (used by the MCP tool `shikii_tune`; adopts nothing) |
| `GET /v1/wiki` · `POST /v1/wiki/claim` | | The list of the wiki's claims · change a claim's status (promote / delete; only a person changes it) |
| `POST /v1/wiki/notebook` | `operation: query / ingest / capture / maintain / lint / handoff_list / handoff_read / handoff_write / handoff_append` | The project's wiki: search, store, review and lint sources and articles. Lessons are hypotheses. handoff_* read and write the work records (the same operations as MCP; the page itself only reads) |
| `GET /v1/handoff/file?path=` | `path` (relative to .shikii/handoff/) | One work record. Images as images, text and HTML as text/plain, anything else as a download. Outside the folder is 400 |
| `POST /v1/review` | `operation: query / submit` | Provisional assessments from conversations and results. Fetches the originals, the targets and examples of corrections, and saves with evidence. People check samples and held cases |
| `POST /v1/reflect` | `operation: query / submit`, `reviews` / `wiki` / `memory` / `maintenance` | Fetch and save together: assessments, wiki work, observations of delivered memory and corrections, and article reviews (query shows the oldest sources and review targets two at a time; submit takes any number of conversations, items and reviews; when extending an existing page, its sources are kept). Check `complete` and each `error` |
| `GET /v1/specs` | | The list of loaded specs |

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
| `internal/calib` | Platt calibration (`calib.go`), vector scaling (`vector.go`), Brier / ECE / reliability diagram, detection of flipped positives and negatives |
| `cmd/shikii` | The CLI |
| `cmd/shikii-readout` | **The adapter that turns an ordinary GGUF into System One**, `shikii-readout`. Reads the option letters' probabilities through the llama-server's `/apply-template` and `/completion` behind it. Has no inference and no calibration of its own |
| `cmd/fake-systemone` | A fake model to run the loop without a key or a GPU |
| `decisions/*.yaml` | Sample specs (in this repo; in your own project, copy them into `.shikii/decisions/`) |
| `testdata/` | Synthetic labelled data (62 requests, and 32 pairs each of bug reports and inquiries) and the calibration evidence for CI (`store/labels/`) |

## Not there yet

- Isotonic regression
- Reading keys from a store (pass them in environment variables; this process never touches a keychain)

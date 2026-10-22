# shikii: the long guide (English)

Everything the [README](../README.md) leaves out: every command, the full quick start, how the lifecycle works inside, what you can connect, the measured results and the FAQ. The Japanese counterpart is [guide.ja.md](guide.ja.md).

[What shikii is](#what-shikii-is) · [The goal](#the-goal) · [Quick start](#quick-start) · [How shikii works](#how-shikii-works) · [What you can connect](#what-you-can-connect) · [Results](#results) · [FAQ](#faq) · [Commands](#commands) · [More documentation](#more-documentation)

## Use it in ordinary conversations

For automatic recording in Claude Code or Codex, install once and initialize each project explicitly with `shikii --init`. Uninitialized projects stay untouched. Use the one-line curl installer in [Get started](../README.md#get-started), or clone the source and run `sh install.sh` in that checkout with Go 1.27 or newer.

Your usual agent reads requests, replies and the next human message and integrates them into existing topics. Each page can keep a concise retrieval summary, a knowledge body with its reasons and unknowns, and related page IDs, with sources and conditions. `shikii wiki query lesson:<page_id>` reads a full page; explicit topic searches include its body. See [wiki editing](../skills/shikii/references/wiki.md). Before similar work it receives up to two short quotes from the same project. Assessment, wiki work and memory observations are handled together using `shikii reflect query / submit`, MCP `shikii_reflect`, or `POST /v1/reflect`.

This also applies to writing, research and business work. New projects receive domain-neutral prompts; existing project questions and calibration remain unchanged. The built-in `edit` outcome and scope checks concern observed local files. External actions and business eligibility need their own spec and evidence. Without a usable Git snapshot, hooks do not infer a file-change truth from an empty list.

Explicit keyword search covers all stored articles and returns `search.total` and `search.candidates` with title, applicability, revision and matching fields/terms. Responses are divided by JSON data size instead of a candidate or source count. If `next_cursor` is present, repeat the same query or source IDs with `continue:CURSOR` on the CLI, or `cursor` in MCP/HTTP. Continue until the needed evidence is found or no cursor remains. Invalid, cross-project, changed-request and stale-result cursors are rejected. The knowledge view follows list continuations automatically. A single large source is returned intact on its own rather than truncated. Refine topic terms when results are unrelated; matching is not semantic search. Reuse actual text already read at the same project/revision and fetch missing sources together. A new task, changed revision or lost context needs a fresh lookup; citation checks remain active.

Reflection preserves the delivered quote and revision and records a decision and observed result: helpful, unused, harmful or unknown. The 0.4.1 correction workflow also links a verbatim user statement to an action, and observes followed, repeated, not applicable or unknown separately. Success or silence alone establishes neither. See [the input format and evidence rules](../skills/shikii/references/review.md) (Japanese).

Conversations, lessons, provisional assessments, truths and calibration belong to the project's `.shikii/`. The common `~/.shikii/agent` holds templates, tools and the project index. The Knowledge panel shows memory observations and their evidence. People check random samples and undecided cases; AI assessments do not become truth labels or calibration automatically.

The sections below cover the decision-model workflow and its CLI in detail.

## Update shikii

Use one command to update to the latest stable release:

```sh
shikii update
```

It downloads the latest stable GitHub Release for macOS or Linux on arm64 or amd64. Go, curl and GitHub CLI are unnecessary. SHA256SUMS and available asset digests are checked, then the staged binary must report the expected version before installation. The current executable's real directory receives shikii and readout; a different PATH entry is not updated instead. Equal or older releases are skipped.

Both binaries are prepared before replacement. A write failure restores replaced files. Licenses and third-party notices are retained in `shikii-licenses/` beside the binary. Concurrent updates are rejected. Conversations, lessons, truths and calibration are preserved.

Hooks already using this executable have their configuration and agent instructions refreshed by the new binary. No hooks are added for a standalone installation. If this final step fails, the error states that the binaries were updated and suggests `shikii hook install --yes` to retry. Running UI and MCP processes are left running; restart them to load the new binary. Updated skills and hook instructions are available in a new agent session. Existing project questions and calibration are preserved; new projects use the current built-in prompts.

## What shikii is

Decision models (TypeSafe Jev, llama.cpp OpenJev, OpenRouter Decisions, …) answer typed questions, `choice`, `score` and `noul` (yes/no), with probabilities and no generated text. Every vendor ships the same disclaimer: *the probabilities are not calibrated for your data, and thresholds do not carry across models or questions.*

shikii owns the part nobody ships: the **lifecycle** of a decision. A spec file holds the question, the options, the state template and the thresholds. `decide` asks a model and returns a band (`act` / `ask` / `discard` / `abstain`), never a raw number. `label` records what was true. `calibrate` fits and cross-validates, then writes the result back into the spec under a key made of the question's hash and the model's name. `check` fails CI when the wording or the model drifted. `gc` keeps the store small.

It also comes with `shikii-readout`, a small adapter (about 600 lines) that turns **any chat GGUF on a stock llama.cpp** into a decision model by reading the logits of the option letters. In our measurements an ordinary 27B ternary model read this way matched trained decision models on accuracy, at 0.6 seconds a question on a Mac.

shikii never runs inference itself (`shikii up` launches llama-server for you, and `shikii-readout` sits in front of a plain GGUF) and never reads a keychain. It talks to a decision-model endpoint you already have, keeps the records, and refuses to **act** until the probabilities for *this question on this model* have been verified against labels. Calibration blocks are generated, never written by hand.

> The name is Japanese: *shikii* (閾) means threshold, the line that turns a probability into an action. The command is `shikii` too.

## The goal

**People give only two things, their intent and the truths. The machine grows everything else: how to ask, how far to trust, when to escalate. What it learns is shared between people.**

- *How to ask*: wording, option descriptions and context layout are measured on the stored truths and improved without new labels (`tune`). Lessons about which phrasing works for which kind of question and which model family accumulate in a shared, human-editable wiki.
- *How far to trust*: calibration per (question, model), bands instead of raw numbers, no `act` without evidence, invalidation on change, replay after change, drift checks, and audit sampling: 1 in 20 decisions the machine acted on is shown to a person, so precision at `act` is also measured on truths people actually checked. `check` fails when that human-checked precision falls below `act` once at least `min_n` human-checked `act` decisions exist, however good the machine-generated truths look; every `act` decision carries a receipt (which line, fitted when on how many truths, how many a person checked and at what precision); and `calibrate` writes a line only when at least `min_n` of its truths were decided by people, never from the machine's own answers alone.
- *When to escalate* (the ladder exists): one model, several levels of asking: a minimal question first, the same question with more context when unsure (`state.context`), a person last. A choice can also be asked as a chain of yes/no `steps` whose branch probabilities multiply into the original options; `tune` scores that like any rewording. Each level is its own hash with its own calibration. Answers from a higher level become truths for the lower one: a confident answer with context is recorded as a weighted truth for the minimal question, and a person's answer is recorded for every level the decision went through. So the cheapest way of asking keeps getting better. The measure of progress is human decisions per thousand, holding precision; `status` prints it.

A hypothesis we intend to test, not a claim: for typed decisions, a measured question plus calibration may reach useful accuracy with far fewer examples than fine-tuning (LoRA) and with better-calibrated answers than retrieval (RAG), on the same base model. The benchmark is simple and lives in this repository: same task, same truths, same model, three methods, accuracy and Brier side by side. The first measurement did not support it: on one task (62 truths) with Qwen3-4B as the shared base, LoRA on about 50 examples reached 0.952 top-1 where the measured wording reached 0.855. The same weights moved 13 points with the prompt layout alone, so we made layout a `tune` candidate; `tune` then picked it on its own, the measured wording rose to 0.903, and retrieved examples in that layout tied LoRA at 0.952 (Brier 0.048 vs 0.038). Wording alone did not catch fine-tuning; wording plus retrieved examples did, on accuracy. On a yes/no task with 32 truths the picture flipped: LoRA on about 26 examples fell below the untouched base model (0.938 vs 0.969), while retrieved examples still helped. Whether fine-tuning wins depends on the question type and the number of examples. All three arms run from `scripts/bench/`; the numbers and caveats are in [bench.md](bench.md).

## Quick start

Go 1.27 or newer. The fake-model demo needs no key or GPU.

### Run the whole loop without a key or a GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # fake model: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # a fake model whose probabilities are too sharp; compare the raw and calibrated confusion matrices
```

### Prepare the default local model runtime

On macOS with Homebrew: `brew install go llama.cpp`, then check `go version` (1.27+) and `llama-server --version`. The [formula](https://formulae.brew.sh/formula/llama.cpp) installs the server. Other systems: [official installation instructions](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md). The default for `shikii up` and the hooks is Kev-4B Q4_K_M, `ggml-org/Kev-4B-GGUF:Q4_K_M`.

### Point it at a real model

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) local model on llama.cpp (downloads Ternary-Bonsai-27B Q2_g64, about 7.6 GB, on first run)
bin/shikii up                                     # finds llama-server, starts it on :8086 and shikii-readout on :8080, waits until healthy
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) hosted TypeSafe Jev. The key comes from the environment only
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

The answer is a band, a trace id and the probabilities. Until the spec carries a calibration for this model, the band is at most `ask`.

### Close the loop

```sh
bin/shikii label <trace_id> change_request          # or:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 when the wording changed or precision@act fell below act
```

`calibrate --write` refuses while a spec has fewer human truths than its `eval.min_n` (50 by default). For a trial, lower `eval.min_n` in the spec to the truths you have and raise it again before relying on the line; `--force` writes anyway, but `check` flags the line.

### Or run one process for programs and people

```sh
bin/shikii serve --local http://127.0.0.1:8080        # http://127.0.0.1:8095/  (specs from .shikii/decisions, knowledge from .shikii/wiki)
curl -s localhost:8095/v1/decide -H 'Content-Type: application/json' -d '{"spec":"prompt-intent","inputs":{"message":"fix the typo"}}'
curl -s localhost:8095/v1/label  -H 'Content-Type: application/json' -d '{"spec":"prompt-intent","trace_id":"<from the decide response>","truth":"change_request"}'
```

Programs call `POST /v1/decide` and `POST /v1/label` from any language; the body is JSON and the `Content-Type: application/json` header is required. People open the page: the decisions still waiting for an answer are listed (newest 30 per spec), `ask` first, with one button per option. Press the button, the truth is stored, and when a spec reaches `eval.min_n` the server calibrates and writes the spec by itself. Nobody types a trace id. The page is a single HTML file with no external loads, so it works on a laptop with no internet. It has a language switch; add a `display:` block to a spec (per language: title, question, option or level names) and the page shows the decision in that language too. `display` is never sent to the model and is not part of the calibration key.

| Method and path | What it does |
|---|---|
| `POST /v1/decide` | `{"spec","inputs",["context"]}` → band, trace id, probabilities, receipt |
| `POST /v1/label` | `{"spec","trace_id","truth"}` → stores a human truth; a wrong option is rejected with the list of valid ones |
| `GET /v1/queue?spec=<id>` | decisions waiting for an answer, `ask` first, audit samples marked `audit: true` |
| `GET /v1/answered?spec=<id>` | the latest answers, to fix an earlier one |
| `POST /v1/redo` | `{"spec","trace_id"}` → re-decides a past text with today's wording and line, appending, never deleting |
| `GET /v1/status` | the same inventory as `shikii status` |
| `GET /v1/tune?spec=<id>`, `POST /v1/tune/run`, `POST /v1/tune/variant`, `POST /v1/tune/adopt` | the latest tune result and the current candidates, a new run in the background, a rewording written by a person (`{"spec","name","question","options"}`; validated, never adopted by itself), adopt or reject a wording |
| `GET /v1/wiki`, `POST /v1/wiki/claim`, `GET /v1/specs` | the knowledge panel and the spec list |
| `POST /v1/wiki/notebook` | project wiki: query, ingest, capture observed sources, maintain article reviews and lint; lessons remain hypotheses |
| `POST /v1/review` | query and submit evidence-backed provisional assessments; humans check random samples and undecided cases |
| `POST /v1/reflect` | query or submit `reviews`, `wiki`, `memory` and `maintenance` together, including correction observations; check `complete` and each item's `error` |

The page also has a *wording* section per spec: the latest `tune` result, the winner's wording next to the current one, and two buttons, **same meaning → adopt** and **meaning changed → reject**. Numbers are the machine's job; whether a rewording still asks what you meant is a person's. Start `serve` with `--proposer http://127.0.0.1:8087` and the page can also ask a chat model for new candidates and score them in the background.

The same thing as MCP tools for an agent: `shikii mcp` speaks the Model Context Protocol over stdio and exposes `shikii_specs`, `shikii_decide`, `shikii_label`, `shikii_status`, `shikii_tune` `shikii_wiki`, `shikii_review` and `shikii_reflect`. There is deliberately no wording-adoption tool: an agent can propose and measure wordings, only a person can adopt one. The conversation wiki uses the usual agent to write hypotheses with sources, independently of truth labels and calibration. Claude Code picks it up from the repo's `.mcp.json`. See [`integrations/mcp.md`](../integrations/mcp.md).

### Use any GGUF as a decision model

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # stock llama-server with flash attention and 8-bit KV, :8086
make readout                                               # shikii-readout in front of it, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Improve the question, not just the answer

```sh
# in the spec:
# variants:
#   - name: other-tight
#     options:
#       other: Not a request at all - greetings, thanks, small talk. Anything that asks for work is never other.
bin/shikii tune .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8080          # scores every candidate on the stored truths
bin/shikii tune .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8080 --write  # adopts the winner as v+1 and calibrates it
```

The layout is fixed: run shikii at the project root, and everything shikii makes lives under `.shikii/`: `decisions/` (specs), `wiki/` (how-to-ask knowledge), `records/` and `labels/` (records and truths). Nothing else is added to the project root. `--store` moves the whole folder; there is no separate flag for where the specs or the wiki live.

Wording moves accuracy far more than calibration does (0.68 → 0.90 in our data; a layout change moved a 27B model by 5 points). The truths people gave do not depend on the wording, so every candidate wording can be scored on all of them with no new labels: top-1, Brier, precision and recall at `act`, plus a k-fold holdout of the selection itself. A candidate is adopted only when that holdout beats keeping the current wording; a win by one example out of 62 does not pass (we saw exactly that: 0.984 on the full bank, 0.952 on holdout, so the current 0.968 stayed). Ties keep the current wording. The winner becomes a new version file; the old one stays, and the calibration is fitted immediately by replay.

A candidate may also change the *layout* of the prompt, not just its words: `layout: plain` asks `shikii-readout` to drop the system message, list options as `- id: description` and read the option id itself instead of a letter. Layout is part of the hash and of the model name, so its calibration is its own. We added it after the same 4B weights moved 13 points between two layouts ([bench.md](bench.md)); a layout is now something `tune` measures, not something a person picks by hand.

A candidate may also change the *shape* of the question. For a choice, `steps:` asks a chain of yes/no questions in one call and multiplies the branch probabilities into the original options:

```yaml
variants:
  - name: two-step
    steps:
      - ask: Is this message a request, as opposed to a greeting, thanks or small talk?
        no: other
      - ask: Does it ask to change something in the repository?
        yes: change_request
        no: question
```

Every option appears exactly once as a leaf; the truths, the calibration and the bands are unchanged, so a decomposition is scored and adopted like any rewording. It helps models that answer yes/no better than a five-way choice; on a 27B model that already handles the choice well it lost (0.790 vs 0.968), which `tune` measured on the same truths.

### Adding context only when unsure

The ladder at run time is `state.template` → `state.context` → a person. `decide` asks the minimal question first. If the answer is unsure (`ask`; for a choice also `discard`, meaning no option reached `ask`) and the caller supplied the extra inputs that `state.context` names (the previous turn, the files touched, whatever the spec asks for), it asks the same question again with that context and returns the second answer; the first stays on the record as `minimal`. The context layout is its own hash, so it gets its own calibration line and cannot `act` until it has one. Nothing happens when the caller passes no context, or when the minimal answer was already confident, so adding `state.context` to a spec costs nothing on the decisions that were already fine, and does not touch the minimal question's hash or calibration.

On the 27B model through `shikii-readout`, uncalibrated, with the previous assistant turn "Shall I remove the deprecated `--legacy` flag?" as context:

| message | minimal question | with context |
|---|---|---|
| do it | operation 0.58 (no option reaches `ask`) | change_request 0.73 |
| yes please | question 0.34 | change_request 0.87 |
| go ahead | other 0.32 | change_request 0.77 |
| the tests | question 0.58 | change_request 0.45 (still unsure → a person) |

What context can settle gets settled; what is genuinely ambiguous stays with a person.

A judge can carry part of the labelling: `shikii judge` asks a second, larger model the same question, measures its agreement with the human truths first, and only if that reaches `act` lets it label the unanswered records with its agreement as the weight (source `judge`, never counted as a human truth). Truths flow down the ladder. When the context rung reaches `act`, its answer is recorded as a truth for the minimal rung with source `context` and the calibrated probability as weight; when a person answers a decision that went through both rungs, their answer is recorded for both. `calibrate` fits one line per rung, `check` verifies both, and `status` shows where decisions settle: by the minimal question, by added context, or left to a person (`ask`, and for a choice also `discard`, the same rule that decides when to climb), as a count per thousand decisions. Replay never re-asks a machine truth, so a machine answer cannot turn into a human-looking one. `tune` scores wording candidates on the minimal rung only, so a candidate is never credited for context it did not have.

You do not have to write the candidates yourself:

```sh
bin/shikii tune .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8090 --propose 3 --proposer http://127.0.0.1:8087
```

`--propose` hands a chat model the current wording, the confusion matrix, the examples it gets wrong and the relevant pages of the [wiki](../wiki/index.md) (how to ask this type, this model family's habits, confirmed patterns, this decision's own rules and the wordings people rejected), and asks for candidates. Texts from `pii: true` specs and texts recorded under `--tainted` go only to a proposer on this machine; the hosted key is never sent to a proposer (set `SHIKII_PROPOSER_KEY` if yours needs one). A wording proposed in this run is never adopted by the same `--write`: a person reads it first, then adopts on the page or with a plain `tune --write`. They are stored in the spec's `variants:` so people can read them, scored like any other candidate, and the run is written back to `wiki/runs/` with the numbers, the decision and a one-line lesson. The wiki is where the knowledge of *how to ask* accumulates, is edited by people, and is read by the next proposal. A pattern page is not prose but a verifiable claim: front matter with the claim, where it applies, the evidence runs, and a retest recipe (which spec, which candidate should beat which). `shikii wiki verify` reruns the recipe on the current model, records the verdict as new evidence, and says when two independent models support a claim; a person changes the status. Anyone can rerun a claim on their own model and add to its evidence. Every `tune` run drafts a claim for a candidate that beat the current wording, and the answer page has a knowledge panel where a person turns a draft into a general claim, confirms or rejects it; the machine never changes a status.

### Measure the agent's own thinking (Claude Code and Codex hooks)

```sh
shikii hook install          # shows what it would do (install.sh runs the --yes form for you)
shikii hook install --yes    # ~/.shikii/agent/ (shared templates and project index; data lives in each project’s .shikii/), the agent's skill, 2 hooks in ~/.claude/settings.json and ~/.codex/hooks.json, and the local model if llama-server is present; open a new terminal (Codex: run /hooks once to trust them)
shikii hook uninstall --yes  # removes the 2 hooks; records and truths stay
```

Conversations, lessons, provisional assessments, truth labels and calibration stay in each project's `.shikii/`. Hooks create it automatically at the repository root (the current folder outside Git) and add `/.shikii/` to Git's local `info/exclude`. The page on :8096 lists project entrances without combining their histories or scores. CLI, MCP and the selected project's page read and write the same store. Projects with the same folder name are distinguished by their real paths. Existing global histories are not imported.

The hooks start what is missing by themselves: the local model (`shikii up`, when :8080 does not answer) and the answer page (`shikii serve --store ~/.shikii/agent --port 8096`, when a decision waits and :8096 is down), at most once per 10 minutes, logging to `~/.shikii/agent/up.log` and `serve.log`. Nobody has to type a start command.

The hooks run at two moments only: when a request arrives (before the agent thinks) and when the turn ends (after it has finished). There is no per-tool hook: classifying every shell command is noise, not thinking, and permissions are already Claude Code's job. Four specs ship with it. Before the turn: `prompt-kind` (what the agent will end up doing; its truth comes for free from whether the working tree changed by the next request) and `needs-plan` (whether the current request still has a choice for the user to resolve). For the new needs-plan format, the agent prepares the agreed approach and open questions with quotes from the current exchange before the decision model runs; unknown context stays unknown. After the turn: `changes-in-scope` (the files that changed during the turn, compared against the request) and `done-as-asked` (request, changed files and closing message: did it finish what was asked). Three or four decisions per request, whatever the number of tool calls. Everything is local-only (`send_to: [local]`, `pii: true`). Each project's `.shikii/` holds its conversations, assessments, truths and calibration; the shared page opens each project separately. If a spec has candidate wordings (`variants:`), the Stop hook also runs `tune` in the background every 50 new human truths (never `--write`: adopting a wording stays a person's call; the run lands in `wiki/runs/`).

Model-based advice waits for a calibration line. Conversation recording, memory recall and batched reflection work before a line exists, including when the decision model is unavailable. Once it does: a confident needs-a-plan request becomes context for the agent, a turn that confidently looks out of scope or unfinished becomes one line to the person (never to the agent: feeding context at Stop would keep the conversation going by itself), and an unsure needs-plan decision prints one line with its URL. Scope and completion decisions wait for routine AI assessment after the next human reply; people check random samples and undecided assessments. Provisional assessments never become truth labels or calibration data. See [the assessment workflow](../skills/shikii/references/review.md) (Japanese). A hook never stops a tool or returns a permission decision. At the end of a turn one line says how many of this session's decisions wait for an answer (only when new ones were added). If Claude Code's `language` setting is Japanese, every message is shown in Japanese next to the English; for other languages the message stays English. Either way the record keeps `display` (language, the question and answer in that language when the spec has them, the message shown) so you can trace what was said later. Failures (no model, slow model) never block: the hook exits 0 and logs to `~/.shikii/agent/hook.log`. Details and the band → behaviour table: [`integrations/claude-code.md`](../integrations/claude-code.md).

### Install and update

```sh
sh install.sh                # Go 1.27+; run inside a source checkout
cd <project root> && shikii init   # .shikii/ (decisions, wiki with the shared knowledge, records, labels); MCP entries for Claude Code, Cursor, OpenCode, Codex; AGENTS.md section; the agent skill
make install          # copies bin/shikii and bin/shikii-readout to ~/.local/bin
git pull && make install
shikii doctor         # what is running, what is missing, and the command that fixes it
shikii new my-decision --type choice   # a new spec by answering a few questions; try it with the printed command
```

`shikii init` points the four MCP entries at a local model on :8080; hosted only? Remove `--local` from all four entries by hand. Unused tool files can be deleted or gitignored. Projects with only a `CLAUDE.md` get no `AGENTS.md`. The skill folder (`~/.claude/skills/shikii`) is replaced on every init, so do not edit it by hand; your own entries in the MCP files are never rewritten.

What to commit: `.shikii/decisions/` and `.shikii/wiki/` always. `.shikii/labels/` holds the truths, the asset that survives a model change, so commit it. `.shikii/records/` grows with every decision; committing it is optional (`shikii gc` drops old unlabelled records).

To change the answer page itself (`web/`, Svelte) you need Node 24 (`make web`); just using shikii does not: the built page is committed and embedded in the binary.

## How shikii works

Every decision is one YAML file. The question, the options, the state template, where the state may be sent, the thresholds, and, generated by `calibrate --write`, the calibration blocks. The full format is in [spec.md](../skills/shikii/references/spec.md).

```yaml
id: prompt-intent
type: choice
question: What is the user asking the assistant to do?
options:
  - id: change_request
    description: Change something in the repository - edit, fix, add, remove, rename...
  - id: question
    description: Explain, investigate, compare or give an opinion, without changing anything.
state:
  template: "{{ message }}"
  required: [message]            # missing input → abstain; the model is never called
  context: |                     # optional: asked only when the minimal question is unsure and the caller passed `previous`
    Previous assistant turn:
    {{ previous }}

    User's message:
    {{ message }}
policy:
  send_to: [hosted, local]       # a tainted session skips hosted automatically
  pii: true                      # the text may contain personal information → never sent to a hosted endpoint
thresholds:
  act: 0.75                      # ≥ act → act automatically, only if calibrated
  ask: 0.60                      # ≥ ask → ask a human; below → discard
```

<p align="center"><img src="assets/shikii-architecture-en.svg" alt="shikii architecture: three things people do, the assets (questions, truths, receipts), the loop the machine grows, the verifiable wiki, disposable models" width="100%"></p>

<p align="center"><img src="assets/shikii-lifecycle.svg" alt="one lifecycle for every decision model" width="100%"></p>

**The calibration key is `(hash of question + template + options, model id as returned by the endpoint)`.** Edit one word of the question, or let `jev-latest` roll to a new version, and the calibration is simply gone: the band drops to `ask` instead of silently acting on stale constants. Getting it back is machine time, not human time: truths are kept per input together with the text, independent of wording and model, and `calibrate` replays them against the new key. Prompt layout is part of the key too: `shikii-readout` reports its layout in the model name (`…+state-last`), because moving the state inside the prompt changed a 27B model's accuracy by 5 points with the same weights.

`calibrate` compares two methods per type under 5-fold cross-validation (`identity` against `platt` for yes/no, `identity` against `vector` for choice and score), keeps the ones that meet precision@act and writes the better of those. `identity` means the raw probabilities already meet precision@act and the model is simply marked as verified. `platt` (σ((Δ−bias)/T)) is for yes/no questions. `vector` (softmax((log p + b)/T)) is for choice and score, and fixes per-option habits such as "falls back to *other*". Partial labels (`!option`, "it was not *that*") count toward precision but not toward fitting. Labels obtained from a file are persisted, so `check` and `drift` run offline afterwards.

Safety-relevant branches (taint routing, no act without calibration, stale-hash invalidation, label validation, inversion detection, CV-based method choice, dry-run `gc`, the adoption gates, …) have recorded mutations that made a test fail before the fix was accepted. There are 313 Go tests across 10 packages; the demo runs in CI.

## What you can connect

shikii speaks the System One request shape (`POST /v1/systemone`), with an `answers` object and probabilities. A chat-completions API alone is insufficient. `shikii-readout` specifically requires llama.cpp's `/apply-template`, `/tokenize` and `/completion` APIs.

| Target | What shikii gets | Requirements and guide |
|---|---|---|
| **TypeSafe Jev** (hosted) | `choice` / `score` / `noul` with probabilities, model id such as `jev-1.13.0` | `TYPESAFE_API_KEY` in the environment. Default `--hosted` |
| **llama.cpp decision models** (OpenJev, Clef, …) | Same shape, locally, model id from `/v1/models` | `shikii up --hf <repo:quant>` (shikii-readout passes a decision model through) or `llama-server -hf <repo:quant>`. llama.cpp 0.6.0 or newer. Kev-4B is not supported |
| **Any chat GGUF on stock llama.cpp** (the default: Ternary-Bonsai-27B Q2_g64) | `shikii-readout` reads the option letters' probabilities and serves the same shape | `shikii up` (llama-server on :8086, readout on :8080). Details in [integrations/readout.md](../integrations/readout.md) |
| **Ollama** | System One probabilities from a decision model | v0.35.0+, `--local http://127.0.0.1:11434 --model nimble`. [Official API](https://docs.ollama.com/api/systemone); matched against the client, inference not tested in this update |
| **LM Studio** | Jev-compatible System One with a loaded decision model | `--local http://127.0.0.1:1234 --model <loaded-model-id>`. [Official API](https://lmstudio.ai/docs/developer/jev-compat/systemone); inference not tested in this update |
| **OpenRouter Decisions API** | Same shape through OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Any chat GGUF** via `shikii-readout` | Letter-logit readout in System One shape, no calibration of its own | Stock `llama-server` 0.7.0 behind it. [`integrations/readout.md`](../integrations/readout.md) |
| **PrismML Bonsai** via [BonJev](https://github.com/ChezRD/bonjev) | Same, with BonJev's own prompt and built-in calibration | Rust build and the Prism fork. [`integrations/bonjev.md`](../integrations/bonjev.md) |
| **Fake model** (`bin/fake-systemone`) | Deterministic probabilities for demos and tests | Nothing. `make fake` |
| **Claude Code and Codex hooks** (`shikii hook install`) | The agent's own thinking measured before and after each turn: will this request change files (labelled from the working tree), does it need a plan, were the changes in scope, was the request completed | [`integrations/claude-code.md`](../integrations/claude-code.md) (Japanese) |
| **Docker / Podman** | Containerized business API and page; `.shikii/` stays on the host. Host-agent hooks are separate | Docker Compose, or Podman with an external Compose provider. Podman execution not tested in this update. [`integrations/docker.md`](../integrations/docker.md) |

The built-in hooks remain fixed to localhost:8080; installing another server does not replace them. The endpoint alternatives above are for the business CLI, MCP and `serve`. Local clients send no Authorization header. For local-only decisions, choose a locally executed model: Ollama can forward to cloud models through a local URL ([official explanation](https://docs.ollama.com/capabilities/decision)). Specifications were checked on 2026-10-21; this is distinct from verifying inference on the actual runtime.

Taint is a first-class input: pass `--tainted` (or let your hook do it) in a session that touched production data and the request never leaves the machine. If no local endpoint is configured, the decision abstains.

## Results

Same specs, same labels, three synthetic tasks, five model routes (the table shows four; the fifth, Bonsai 2 through a forked readout, is in the full table): prompt intent (62 requests, English and Japanese), duplicate bug reports (32 Japanese pairs) and same-matter customer inquiries (32 Japanese pairs). Details and the full table are in [model-comparison.md](model-comparison.md).

| Model | Licence | top-1 (all / ja) | yes/no Brier, raw → chosen (bug reports / inquiries) | Seconds per question |
|---|---|---|---|---|
| Kev-4B (trained decision model) | Apache 2.0 | 0.919 / 0.913 | 0.038 → 0.035 platt / 0.030 → 0.020 platt | 0.25 |
| OpenJev 27B (trained) | CC BY-NC 4.0 | **0.984 / 1.000** | **0.007** identity / 0.037 identity | 3.8 |
| Bonsai 2 27B via BonJev | Apache 2.0 | 0.968 / 1.000 | 0.049 → **0.019** platt / 0.084 → 0.028 platt | 2.5 |
| **Ternary-Bonsai-27B via `shikii-readout`, stock llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.048 → 0.024 platt / 0.033 → 0.020 platt | **0.58** |

**Wording beats calibration.** On prompt intent, 19 of 21 errors landed on *other*; rewording the options took top-1 from 0.68 to 0.90 before any calibration. Calibration is for "right order, not enough confidence". The confusion matrix `calibrate` prints tells you which case you have.

**Whether a model needs calibration is decided by numbers, not by its label.** On the two yes/no sets, OpenJev's raw probabilities were good enough twice and `identity` was written; Kev-4B gained a little from Platt; the ordinary LLMs gained 1.5 to 4 times. All ten (model, set) pairs separated positives from negatives completely, so the difference is only how honest the probabilities are. `calibrate` runs the comparison under cross-validation for each *(question, model)* and writes what won.

**An ordinary GGUF lands next to a trained decision model.** A 27B ternary chat model, mainline llama.cpp 0.7.0, one adapter, no fork, no training, no Rust: same top-1 as the best Apache-licensed route (Bonsai 2 via BonJev), above the trained 4B, Japanese 1.000, 0.58 s a question with flash attention and a warmed prefix. Moving the state from before the options to right before the answer took it from 0.919 to 0.968 with the same weights, which is why the layout is part of the calibration key.

**A first user's walkthrough.** Someone who had never seen this repository routed 22 Japanese inquiry mails into four desks with nothing but the README: [examples/inquiry-route.md](examples/inquiry-route.md) (Japanese; the spec and the data are in English-friendly YAML and CSV).

## FAQ

**Does shikii run models?** Not the inference. It sends System One requests to an endpoint you run or rent, and keeps everything that happens after the answer (`shikii up` is a convenience that launches llama-server and waits for it). `shikii-readout` is the one exception in spirit: it converts a chat model's logits into the same shape, but the inference is still llama.cpp's.

**Why does my decision never reach `act`?** Because the spec has no calibration block for `(this question hash, this model id)`. Collect labels (`label`, or `--labeled file`), run `calibrate --write`, and `status` shows `calibrated: identity|platt|vector`. A decision without verified precision at the act threshold is not allowed to act. That is the point.

**I have fewer than 50 truths. How do I start?** Import a table of past decisions (`shikii import past.csv --spec .shikii/decisions/<id>.yaml`), or lower `eval.min_n` in the spec to what you have for a trial and raise it again before relying on the line. `calibrate` and `tune` say exactly this when the truths fall short.

**I changed one word in the question and the calibration disappeared.** Correct, and it costs you compute, not labels. The hash changed, so constants fitted to the old wording are not trusted. But the truths people gave (this request *was* a change request, these two reports *were* the same bug) do not depend on the wording or the model. Run `calibrate --write` again: it replays every stored truth against the new wording and the current model, refits, and writes. No human in the loop. The text of every recorded decision is kept in the local store for exactly this reason (`--no-record` and `"record": false` are the only exceptions); `calibrate` tells you if any older truths have no text and cannot be replayed.

**Which model should I use?** It depends on licence, speed and language, and shikii does not decide for you. The table above and [model-comparison.md](model-comparison.md) are the argument: with the same spec and the same labels you can choose by numbers, including whether a model needs calibration at all. `scripts/eval-model.sh <url> <name>` reproduces the table for a new endpoint.

**Does it work in Japanese?** Yes, and it is where the differences show. Kev-4B gets 0.913 on Japanese prompts; the 27B models get 1.000. On the two Japanese yes/no sets (duplicate bug reports, same-matter inquiries) every model we tried separated positives from negatives completely; calibration only sharpened the probabilities.

**Where does my data go?** Only the rendered state goes to the endpoint named in `send_to`, and only you decide where that may be: shikii does not guess what counts as personal information. Every spec must say `pii: true` (the text may contain personal information, so it is never sent to a hosted endpoint, whatever `send_to` says) or `pii: false`. There is no default. The text of every decision is kept in the local store on your machine, so people can answer on the page and truths can be replayed after a wording or model change. `--tainted` keeps everything local regardless. Keys are read from `DECISIONS_API_KEY` or `TYPESAFE_API_KEY` and nowhere else.

**The store keeps growing.** `shikii gc` removes records older than 30 days that never got a label. Labelled records and labels stay forever, as they are the calibration material. It is a dry run unless you pass `--apply`.

**Does it bias toward the first option?** Measure before you correct: `shikii bias <spec> --permutations 100` shuffles the options and reports flips and the position shift. Kev-4B: 0 flips in 100, shift ≤ 0.024. Ordinary LLMs read through logits may differ; the command exists for them. `bias` reads its states from the same store as `calibrate` (or from `--labeled`).

## Commands

Flags may come before or after the positional arguments.

```
shikii tune      <spec> [--labeled file] [--folds 5] [--propose N --proposer URL] [--write]   # score candidate wordings; adopt only past holdout + sign test + a person
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii import    <csv> --spec <spec> [--out truths.json]  # a table of past decisions → truths
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, hand-written blocks, precision@act below act → exit 1
shikii drift     <spec> --model M [--window N]            # recent labels vs the Brier and precision recorded at calibration
shikii bias      <spec> [--rows K] [--permutations N] [--store DIR] [--labeled file] [--local URL]   # does the answer depend on option order? states come from the store, like calibrate
shikii judge     <spec> --local <judge URL> [--model M] [--store DIR]   # a larger model labels unanswered records, only after its agreement with people reaches act
shikii status    [spec...] [--store DIR]                  # inventory: records, labels, sources, per-(model, hash) state
shikii gc        [spec...] [--older-than 30d] [--apply]   # drop old unlabelled records; dry run by default
shikii hash      <spec>
shikii wiki verify [pattern.md...] [--local URL] [--labeled file] [--store DIR]   # rerun the retest recipes of the wiki's claims on the current model; only a person changes a status
shikii wiki query [query-or-source-ids]                     # search or read sources; append continue:CURSOR with the same arguments
shikii wiki ingest                                         # lesson and source IDs as JSON on stdin
shikii wiki capture                                        # observed source with kind, version, reference and time
shikii wiki maintain                                       # reviewed page, revision, neighborhood key and reason
shikii wiki lint                                           # check sources, revisions and links; repair the generated index
shikii version                                             # show the CLI version
shikii serve     [--port 8095] [--specs DIR] [--store DIR] [--local URL] [--hosted URL] [--proposer URL]   # HTTP API + review and wording page; auto-calibrates
shikii mcp       [--specs DIR] [--store DIR] [--local URL] [--hosted URL] [--proposer URL]   # the same as MCP tools over stdio (no adopt tool)
shikii new       <id> [--type ...] [--question ...] [--option id=desc ...] [--input name ...] [--pii true|false] [--from spec.yaml]   # make a spec by answering questions
shikii init                                               # folders, wiki seed, MCP entries, AGENTS.md section, agent skill
shikii hook      install [--yes] | uninstall [--yes]      # Claude Code and Codex hooks that measure the agent's own thinking before and after each turn (project .shikii/, local model, project chooser on :8096)
                 user-prompt-submit | stop                 # run by the agent with its JSON on stdin; not for typing by hand
shikii up        [--hf repo:quant | --gguf file --alias name] [--port 8080]   # start a local model; puts shikii-readout in front of a plain GGUF
shikii doctor    [--specs DIR] [--store DIR] [--local URL] [--hosted URL]   # what is running, what is missing, how to fix it
```

## More documentation

- [decision-ops-framework.md](decision-ops-framework.md): design notes, what the framework owns and what it refuses to own, the verification plan and what was confirmed.
- [architecture.md](architecture.md) (Japanese): the structure behind the diagram.
- [model-comparison.md](model-comparison.md) and [bench.md](bench.md): the comparison table, the LoRA / RAG / wording benchmark, and how to reproduce both.
- [guide.ja.md](guide.ja.md): the long-form Japanese guide with every command and a full real-world loop.
- [spec.md](../skills/shikii/references/spec.md), [bands.md](../skills/shikii/references/bands.md): the spec format and what each band and receipt means.
- [integrations/readout.md](../integrations/readout.md), [integrations/bonjev.md](../integrations/bonjev.md), [integrations/claude-code.md](../integrations/claude-code.md), [integrations/mcp.md](../integrations/mcp.md), [integrations/docker.md](../integrations/docker.md): the connections we run.
- [examples/](../examples/) and [testdata/](../testdata/README.md): a spec with no calibration to start from, and the synthetic labelled sets used in tests.

## Knowledge sources and article maintenance

Capture inspected evidence with `wiki capture` (JSON on stdin): `kind` is human, agent, code, test or release; include title, content, reference, version and observed_at (RFC3339). Text is bounded to 32 KiB and the encoded object to 64 KiB. Reference strings are not executed or fetched. The returned ID is an immutable, project-scoped source for ingestion. Captured observations share the two-source reflection work batch; the pending queue and stored articles have no fixed count ceiling. Available long conversation originals are preserved separately with a citation hash during ingestion.

Query results explain matching fields and terms. Titles and applicability outrank body-only matches; automatic recall uses titles and applicability only. `hook plan query` can select up to two article IDs with `pages`; cite returned article keys during submit. A decision records ranked candidates, selected bodies, exact citations, available revisions, lookup milliseconds and context bytes. Use `lesson:<id>@<revision>` for historical articles and constrain retrospective comparisons to the recorded inventory. Retrieval, quotation, agent assessment and observable outcomes remain distinct.

Listing the wiki and querying reflection returns up to two maintenance tasks. The active agent reads articles, connected knowledge and sources, edits through ingestion with change_reason, then records the current page_id, revision, key and reason through wiki maintain or reflection. Page and connected revisions invalidate earlier reviews; unchanged neighborhoods return after 30 days. Missing bodies or links are inspection prompts, not semantic errors. See [the source and maintenance contract](../skills/shikii/references/wiki.md).

## Conversation recall and reflection limits

Before a request, the hook quotes up to two lessons whose titles and conditions match, within 1800 bytes after JSON escaping. The same revision is suppressed for one hour in that session; updated revisions arrive immediately. If no lesson applies, at most one similar unprocessed conversation is quoted. Archived source material is not repeated. Manual wiki queries still search lesson bodies.

Assessment and wiki ingestion are grouped into two conversations, or one pending conversation after a day at the next request. `shikii reflect query` returns original material, targets, relevant existing lessons and human corrections together. `shikii reflect submit` accepts `reviews`, `wiki` and `memory` in one JSON object. The MCP equivalent is `shikii_reflect` and the HTTP endpoint is `POST /v1/reflect`. Check `complete` and each item's `error`; successful items persist and can be retried with identical input. This does not create truth labels or calibration data. See [the JSON workflow](../skills/shikii/references/review.md).

Initialize each project explicitly with `shikii --init` (`shikii init` is equivalent), or ask the agent to use shikii in that project. This prepares the four built-in decisions, wiki, MCP, rules and page link. The completion marker is written last; uninitialized projects remain untouched by hooks. Reinitialization preserves existing settings and data. Subdirectories resolve to the same project root.

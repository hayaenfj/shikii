**English** · [日本語](docs/readme/README_ja.md) · [简体中文](docs/readme/README_zh.md) · [繁體中文](docs/readme/README_zh-TW.md) · [한국어](docs/readme/README_ko.md) · [Deutsch](docs/readme/README_de.md) · [Español](docs/readme/README_es.md) · [Français](docs/readme/README_fr.md) · [Italiano](docs/readme/README_it.md) · [Português (Brasil)](docs/readme/README_pt-BR.md) · [Русский](docs/readme/README_ru.md) · [Українська](docs/readme/README_uk.md) · [Polski](docs/readme/README_pl.md) · [Türkçe](docs/readme/README_tr.md) · [Tiếng Việt](docs/readme/README_vi.md) · [Bahasa Indonesia](docs/readme/README_id.md)

<p align="center"><img src="docs/assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: turn the AI's "probably" into a "safe to use" you can run a business on

### A tool that grows lessons from everyday conversations and checks AI decisions against outcomes and corrections. Your usual agent maintains the wiki while you work.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](go.mod)
[![Tests](https://img.shields.io/badge/tests-221%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](docs/operations.md)
[![Version](https://img.shields.io/badge/version-0.3.0-00ADD8?style=flat-square)](CHANGELOG.md)

[The problem](#the-problem) · [Use 1: measure the AI's own thinking](#use-1-measure-the-ais-own-thinking) · [Use 2: hand it a business decision](#use-2-hand-it-a-business-decision) · [What you do on the page](#what-you-do-on-the-page) · [Install](#install) · [Why you can swap the AI](#why-you-can-swap-the-ai) · [Read more](#read-more)

<p align="center"><img src="docs/assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## The problem

Ask an AI "is this inquiry the same case as the last one?" and it says "probably, 0.9". For work, that is not enough. Is that 0.9 really right nine times out of ten? Did it quietly become a lie the day you switched to a newer AI? Are the decisions it makes while nobody is watching still correct? If a person has to check every time, there was little point in using an AI.

shikii draws a **line** from your corrections: above this, the AI may act on its own. Until a line exists, nothing is trusted. Once it exists, every automatic decision carries its grounds: how many corrections the line rests on and how often it was right. The line lives with the question and the corrections, not inside the AI, so swapping the AI costs no human work.

There are two ways to use it: measuring the thinking of the AI that writes your code (the lightest), and handing it a decision from your business.

## Use 1: measure the AI's own thinking

For people who use Claude Code or Codex. The coding agent makes judgements when it receives a request and when it finishes; shikii measures them silently.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

That is all. It installs the binary, adds two hooks to Claude Code and Codex, puts the agent's rules (a skill) in place, and starts a small local AI (Kev-4B) in the background (if llama.cpp is missing it prints the one line to install it). Open a new terminal and keep working with the agent as usual (in Codex, run `/hooks` once to trust shikii's hooks). Four things are measured.

| When | Question | Where the truth comes from |
|---|---|---|
| A request arrives | Will this request end up changing files? | Automatic: did the working tree change before the next request |
| A request arrives | Should the agent agree on an approach with you before touching anything? | You, on the page |
| The turn ends | Were the changes within what you asked? | You, on the page |
| The turn ends | Did it finish what you asked? | You, on the page |

At the end of a turn one line appears, `shikii: 3 of this session's decisions wait for your answer: http://127.0.0.1:8096/`, and you open it when you have a minute (the hook starts the page in the background too); each answer takes seconds. After 50 answers a line is drawn and the agent's behaviour changes: a request that needs a plan becomes a note to the agent, and a turn that looks unfinished or out of scope becomes one line to you, each with grounds such as "line on 61 truths, human-checked precision 0.96". Until then the hooks only record. No hook ever stops a tool.

Each project's `.shikii/` holds its conversations, lessons, assessments, truths and calibration. The hooks prepare it automatically. The common page on :8096 opens each project separately; `~/.shikii/agent` holds shared templates, tools and the project index. Details: [the hook](integrations/claude-code.md) (Japanese).

**0.3: your ordinary conversations grow a wiki.** The original request, the agent's reply and your next message become source material. Your usual agent turns useful observations into lessons with sources and conditions. Before a similar request, shikii quotes related lessons from the same project. It keeps earlier revisions, updates the index and checks sources and links once a day. No wiki-specific page actions or additional model are needed. The Knowledge panel shows lessons and their original sources.

**AI handles routine assessment; people spot-check it.** After your next reply, your usual agent assesses whether the previous work stayed in scope and finished the request, recording evidence or leaving it undecided. People check random samples and undecided cases. Human corrections return as examples for future assessment. Provisional assessments stay separate from truth labels and calibration. [How assessment works](skills/shikii/references/review.md) (Japanese).

This follows [Andrej Karpathy's LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f): retain sources, let the agent maintain the wiki, and separate ingestion, queries and periodic maintenance. Lessons remain hypotheses with sources; interpreting a conversation never makes it a truth label or an accuracy claim. [Changes in 0.3](CHANGELOG.md) (Japanese).

## Use 2: hand it a business decision

For decisions you repeat at work, such as "is this inquiry the same case?" or "is this message a change request?": let the AI decide and grow it from your corrections. Here too there is no command to type. You tell the agent.

> I want shikii to decide whether an inquiry is the same case as an earlier one, and to grow it from my corrections

The agent follows its rules (the skill): it creates `.shikii/` in the project (`shikii init`), settles the question, the options and the inputs and writes the decision file (`shikii new`; it shows you the question and the options once, because that is your intent), calls the decision (`shikii decide`), starts the answer page (http://127.0.0.1:8095) and tells you. Until a line exists no decision is `act` (go ahead). As you answer on the page, the line is drawn automatically once the decision's own count is reached (20 in this example), and from then on:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (safe to act. The line rests on 61 truths, 48 of them answered by people; of the human-answered ones that this line would let act, 25, 96% were right)
```

There are only four `band` values: `act` = go ahead, `ask` = check with a person, `discard` = do not act, `abstain` = not enough input. Raw probabilities are never what you act on.

- **By hand**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. All of it is in [the guide](docs/guide.md)
- **From an agent**: `shikii init` writes the MCP setup, so Claude Code, Cursor, OpenCode and Codex can call the tools `shikii_decide` and `shikii_label` directly ([MCP](integrations/mcp.md))
- **From your own program**: start `shikii serve` and call `POST /v1/decide` (decide) and `POST /v1/label` (correct). The body is JSON and needs `-H 'Content-Type: application/json'` ([HTTP API](docs/guide.md#or-run-one-process-for-programs-and-people))
- **Ready-made decisions**: six in this repo's `decisions/` (request type, duplicate bug report, same inquiry, …). Copy one with `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` and reword it ([list](decisions/README.md))
- **You already have a table of past decisions**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` turns it into corrections, so you need not answer the first 50 by hand
- **Docker**: no Go and no model yet? `docker compose up` starts a local AI and shikii, with the page at http://127.0.0.1:8095 ([details](integrations/docker.md))
- **Someone else's example**: [routing inquiry mails](docs/examples/inquiry-route.md) (Japanese). A first-time user went through the whole loop with nothing but the README, decision file and 22 inputs included

## What you do on the page

On the answer page (`shikii serve`) a person does three things. Everything else is the machine's job.

1. **Answer.** The page shows the decisions the AI was unsure about, plus 1 in 20 of the ones it acted on, and you press the button for what was actually true. Keys 1 to 9 and Enter (same as the AI's pick) move fast. "Answered" lets you fix an earlier answer (the newest one counts)
2. **Write a rewording.** If you think "asking it this way would work better", type it in the "Wording" panel in your own words. The machine scores it against the current wording on the same corrections (for the agent's own decisions, automatically every 50 new answers) and only says "may be adopted" when it is genuinely better. You can also let an AI propose rewordings. You never open a file
3. **Press adopt or reject.** You look at whether the meaning changed and press. The machine checks the numbers; you check the meaning

"Details" adds the state of the line per model, "ask the AI again" (re-decide the same text with today's wording and line and see before and after), and the knowledge panel (claims of the form "asking this way works", with their evidence). Records are appended, never deleted; a new decision takes the old one's place.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 or newer; otherwise Docker (above)
```

What install.sh does: installs the binaries (`shikii` and `shikii-readout`), adds two hooks to Claude Code and Codex, puts the agent's rules (the skill) in `~/.claude/skills/shikii`, and, if llama.cpp is present, starts the local AI (Kev-4B on :8080) in the background. To use hosted Jev or OpenRouter instead, put the key in an environment variable ([connections](docs/guide.md#what-you-can-connect)). `shikii doctor` tells you what is running and what is missing.

The per-project setup (`shikii init`) is the agent's job (you may also run it yourself). It creates one folder, `.shikii/` (inside: `decisions/` = decision files, `wiki/` = knowledge, `records/` and `labels/` = records and corrections). Nothing else appears at the project root. `decisions/`, `wiki/` and `labels/` survive a change of AI. Hooks exclude `.shikii/` from untracked Git files because it also contains private conversations. Check and select any material before sharing it. `shikii gc` drops old records. It also adds a shikii entry to your agents' setup (Claude Code's `.mcp.json`, Cursor's `.cursor/mcp.json`, OpenCode's `opencode.json`, Codex's `~/.codex/config.toml`) without touching entries you wrote. Running it again changes nothing that exists.

Command output and hook messages are in English (if Claude Code's language setting is Japanese, hook messages are shown in Japanese next to the English). The page has Japanese. The finer points, such as using only a hosted AI, `AGENTS.md` versus `CLAUDE.md`, and Node for changing the page itself, are in [the guide](docs/guide.md).

## Why you can swap the AI

<p align="center"><img src="docs/assets/shikii-architecture-en.svg" alt="shikii architecture: three things people do, the assets (questions, truths, receipts), the loop the machine grows, the verifiable wiki, the disposable model" width="100%"></p>

- **Knowledge accumulates with the question and the corrections, not inside the AI.** The corrections survive the AI, so a new one can use them as they are and the line is redrawn. In our measurements, letting the machine choose the wording and layout and add similar examples reached the same accuracy as fine-tuning the model's weights (LoRA) ([numbers](docs/bench.md))
- **Automatic decisions carry a receipt.** Not "95% confident" but "precision 0.96 on 48 human-checked cases"
- **The wiki is not read, it is measured.** "Asking this way works" accumulates as claims with numbers, and can be retested on a new AI. Only a person decides a claim is confirmed

## Read more

- [The guide](docs/guide.md): every command, the HTTP API, FAQ · [the spec reference](skills/shikii/references/spec.md)
- [The hook](integrations/claude-code.md) (Japanese): how the agent's own thinking is measured, the band → behaviour table, the internals
- [Architecture](docs/architecture.md) (Japanese): the whole picture behind the diagram above
- [Ready-made decisions](decisions/README.md) · [the wiki of how to ask](wiki/index.md) · [measured numbers](docs/bench.md) · [operations notes](docs/operations.md)
- [Connecting](integrations/mcp.md): MCP, the adapter for ordinary chat models, Docker

**For contributors**: break a test on purpose and watch it fail before fixing it. Every number comes from a script. Only a person confirms a wiki claim. Add no settings; fixed places and fixed rules instead.

## License

[Apache 2.0](LICENSE). Model licenses are separate (Kev-4B and the Bonsai family are Apache 2.0, OpenJev is CC BY-NC 4.0, hosted Jev is TypeSafe's service).

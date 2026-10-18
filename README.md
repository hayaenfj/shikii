**English** · [日本語](docs/readme/README_ja.md) · [简体中文](docs/readme/README_zh.md) · [繁體中文](docs/readme/README_zh-TW.md) · [한국어](docs/readme/README_ko.md) · [Deutsch](docs/readme/README_de.md) · [Español](docs/readme/README_es.md) · [Français](docs/readme/README_fr.md) · [Italiano](docs/readme/README_it.md) · [Português (Brasil)](docs/readme/README_pt-BR.md) · [Русский](docs/readme/README_ru.md) · [Українська](docs/readme/README_uk.md) · [Polski](docs/readme/README_pl.md) · [Türkçe](docs/readme/README_tr.md) · [Tiếng Việt](docs/readme/README_vi.md) · [Bahasa Indonesia](docs/readme/README_id.md)

<p align="center"><img src="docs/assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: turn an AI's "probably" into a "safe to act" you can run a business on

### A tool for letting an AI decide yes/no, which one, or how much, and keeping that decision trustworthy in daily work. You check some answers and, now and then, confirm "is this still what I meant?". The machine does the rest.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](go.mod)
[![Tests](https://img.shields.io/badge/tests-188%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](docs/operations.md)

[The problem](#the-problem) · [What shikii does](#what-shikii-does) · [What you do](#what-you-do) · [Install](#install) · [Shortest ways in](#shortest-ways-in) · [One full loop](#one-full-loop) · [What is new](#what-is-new) · [Read more](#read-more)

<p align="center"><img src="docs/assets/shikii-hero.svg" alt="shikii: a model gives you a probability, shikii tells you whether you may act on it" width="100%"></p>

## The problem

Ask an AI "is this inquiry the same matter as the last one?" and it says "probably, 0.9". For real work that is not enough.

- Is that 0.9 really right 9 times in 10?
- Since the day you swapped in a new model, has it quietly started lying?
- If you change the wording a little, how does the accuracy move?
- The decisions it took on its own, with nobody watching: are they still right?

If a person has to check every answer, there was little point in using the AI.

## What shikii does

1. **Collects your corrections and draws the line above which the AI may act on its own.** From the answers you gave ("this is what was actually true") it measures how the AI's confidence relates to how often it is right, and decides from where up it can be trusted. Until a line exists, nothing acts. Once it does, every automatic decision carries a receipt that says how many checked answers the line rests on and how often they were right
2. **Tries other ways of asking and keeps the better one.** On the same corrections it scores alternative questions, option descriptions and prompt layouts, and adopts one only when it is genuinely better. You only confirm the meaning did not change
3. **Lets you swap the model without starting over.** The corrections live apart from the model, so a new model reuses them and the line is redrawn by the machine. No human work
4. **Keeps what it learned in a wiki.** "Asking it this way works" accumulates as claims with the measured numbers, re-testable on another model. What other people measured on their models is added to the same page

## What you do

- **Write down what you want decided.** Answer a few questions and a decision file appears (`shikii new`, interactively or with `--type --question --option --input --pii`; the full format is in [the spec reference](skills/shikii/references/spec.md))
- **Check answers.** The page shows the decisions the AI was unsure about, plus 1 in 20 of the ones it acted on, and you press the button for what was actually true. Keys 1 to 9 and Enter (same as the AI's pick) move fast. "Answered" lets you fix an earlier answer (the newest one counts). "Details" adds "ask the AI again", which re-decides the same text with today's wording and line and shows before and after; records are appended, never deleted
- **Now and then confirm "is this still what I meant?".** When the machine finds a wording that scores better, you look at whether the meaning changed and press adopt or reject

Everything else is the machine's job.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # needs Go 1.27 or newer
cd <project root> && shikii init
```

`shikii init` sets up the folders (`decisions/` `wiki/` `.shikii/`) and the agent integrations in one go: the shikii MCP server for Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) and Codex (`~/.codex/config.toml`), a section in `AGENTS.md`, and the detailed rules (the skill) in `~/.claude/skills/shikii`. Your own entries are never touched (an existing shikii entry is left alone). Only the skill folder is replaced each time, so do not edit it by hand. `wiki/` receives the shared knowledge (how to ask each question type, wording patterns that worked, each model's habits); the wording improver (`tune --propose`) reads it, and pages you edited are never overwritten by a later `init`. No Go? Use Docker below.

The entries point at a local model (`shikii up`, port 8080). Hosted only? Remove `--local` by hand from each of the four files. Config files for tools you do not use are created too; delete them or add them to `.gitignore` (a second `shikii init` never touches what exists). Commit `.shikii/labels/` (your corrections, the asset that survives a model change). `.shikii/records/` (every decision) keeps growing, so committing it is optional (`shikii gc` drops old ones). If Codex already has `mcp_servers`, nothing is added and the lines to paste are printed. A project with only a `CLAUDE.md` gets no `AGENTS.md` (OpenCode would stop reading `CLAUDE.md`).

Changing the answer page itself (`web/`, Svelte) needs Node 24 (`make web`). Using shikii does not: the built page is committed and embedded in the binary.

## Shortest ways in

- **Docker**: no Go, no model? `docker compose up` starts a local model (Kev-4B) and shikii, with the answer page at http://127.0.0.1:8095 ([details](integrations/docker.md))
- **Ready-made decisions**: six in `decisions/` (request type, duplicate bug report, same inquiry, …). Use one as is, or copy and edit with `shikii new --from decisions/prompt-intent-v2.yaml` ([list](decisions/README.md))
- **You already have a table of past decisions**: `shikii import past.csv --spec decisions/<id>.yaml` turns it into corrections, so you do not answer the first 50 by hand
- **From your own program**: start `shikii serve` and call `POST /v1/decide` (decide) and `POST /v1/label` (correct). The body is JSON and `-H 'Content-Type: application/json'` is required (all endpoints: [the long guide](docs/guide.md#or-run-one-process-for-programs-and-people))
- **Someone else's example**: [routing inquiry mails](docs/examples/inquiry-route.md) (Japanese). A first-time user went through the whole loop with nothing but the README; the spec and the 22 inputs are included
- **Measure the AI's own judgement** (Claude Code only): `shikii hook install` adds hooks that run shikii on the agent's own decisions: what a command does, whether an edit is within what you asked, whether a request needs a plan first, and what the agent will end up doing (that last truth comes for free from whether the working tree changed). Nothing changes until a line exists. Then a risky command (delete, change outside the project, send outside) asks you to confirm, with how many truths the line rests on and the human-checked precision; safe commands pass silently ([details](integrations/claude-code.md), Japanese)
- **In CI**: `uses: hayaenfj/shikii@main` runs `shikii check` on every pull request, so a changed wording or a weakened line fails the build

## One full loop

```sh
shikii up                                                             # start the local model (downloaded on first run)
shikii decide decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (no line yet, so: ask a person)
shikii serve --specs decisions --store .shikii --local http://127.0.0.1:8080
#   → answer at http://127.0.0.1:8095. Once a decision has the number it asks for (eval.min_n, 20 for this one) the line is drawn by itself
#     To try with fewer answers, lower eval.min_n in the spec to what you have (a line written with --force is flagged by check)
shikii decide decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (safe to act, with the evidence: the line rests on 61 corrections, 48 of them from people. Of the human answers that this line puts on the act side, 25, 96% were right)
```

There are only four bands. `act` = go ahead, `ask` = check with a person, `discard` = do nothing, `abstain` = input missing. You never read a raw probability.

## What is new

<p align="center"><img src="docs/assets/shikii-architecture-en.svg" alt="shikii architecture: three things people do, the assets (questions, truths, receipts), the loop the machine grows, the verifiable wiki, disposable models" width="100%"></p>

- **Knowledge accumulates in the questions and the corrections, not in the model.** So the model is disposable, and swapping it needs no human work. Measured: with the machine choosing the wording and layout and adding similar examples, an ordinary base model matched fine-tuning (LoRA) on accuracy ([numbers](docs/bench.md))
- **Automatic decisions come with evidence.** Not "95% confident" but "precision 0.96 on 48 answers people checked"
- **The wiki is not read, it is measured.** "Asking it this way works" accumulates as claims with numbers and is re-tested on new models. Only a person decides a claim is confirmed

## Read more

- [The long guide](docs/guide.md): every command, the full quick start, the HTTP API, what you can connect, the measured results and the FAQ · [the spec reference](skills/shikii/references/spec.md)
- [Design notes](docs/decision-ops-framework.md): what the framework owns and refuses to own, and what was verified · [Architecture](docs/architecture.md) (Japanese)
- [Ready-made decisions](decisions/README.md) · [the wiki of how to ask](wiki/index.md) · [benchmark](docs/bench.md) · [model comparison](docs/model-comparison.md) · [operations log](docs/operations.md)
- [Connections](integrations/mcp.md): MCP, the adapter that turns a plain chat model into a decision model, Docker, Claude Code
- [Japanese guide](docs/guide.ja.md) with a full real-world loop

## Contributing

Issues and pull requests are welcome. A test that guards a behaviour is shown failing first: break the behaviour on purpose, run the test, record how it failed, then fix it and record that it passes. Every number in the docs comes from a script. Only people change a wiki claim's status (`confirmed` / `tentative` / `rejected`), with the run that is the evidence. No new options: the layout (`decisions/` `wiki/` `.shikii/` at the project root) and fixed rules do the work.

## License

[Apache 2.0](LICENSE). Model licences are separate: Kev-4B and the Bonsai family are Apache 2.0, OpenJev is CC BY-NC 4.0, hosted Jev is TypeSafe's service. shikii stores your probabilities and labels, nothing of the models.

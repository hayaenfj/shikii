**English** · [日本語](docs/readme/README_ja.md) · [简体中文](docs/readme/README_zh.md) · [繁體中文](docs/readme/README_zh-TW.md) · [한국어](docs/readme/README_ko.md) · [Deutsch](docs/readme/README_de.md) · [Español](docs/readme/README_es.md) · [Français](docs/readme/README_fr.md) · [Italiano](docs/readme/README_it.md) · [Português (Brasil)](docs/readme/README_pt-BR.md) · [Русский](docs/readme/README_ru.md) · [Українська](docs/readme/README_uk.md) · [Polski](docs/readme/README_pl.md) · [Türkçe](docs/readme/README_tr.md) · [Tiếng Việt](docs/readme/README_vi.md) · [Bahasa Indonesia](docs/readme/README_id.md)

<p align="center"><img src="docs/assets/gen-mark.png" alt="shikii" width="96"></p>

# shikii: turn an AI's “probably” into a “safe to act”

A model gives you a probability. shikii tells you whether you may act on it. It draws the line from the answers people and results supply, turns every decision into a band (`act` / `ask` / `discard`), keeps what it learned in a wiki that your agent uses while it works, and turns your corrections into checks that stop the agent next time.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](go.mod)
[![Tests](https://img.shields.io/badge/tests-386%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](docs/decision-ops-framework.md)
[![Version](https://img.shields.io/badge/version-0.8.1-00ADD8?style=flat-square)](CHANGELOG.md)

Version 0.7 goes back to the line and the bands. The recommended model is TypeSafe Jev: put the key in the environment, download nothing, and get an answer in about 0.2 s. Without a key, llama.cpp answers the same questions locally. New wiki lessons are placed next to the right page and checked for contradictions by the decision model. 0.7.3 turns corrections into guards that stop the turn, delivers lessons to subagents too, and counts in production whether delivered lessons changed anything. **Updating from 0.6.0 or earlier erases the contents of `.shikii/` in every initialized project and initializes them again**; back up first. See the [changelog](CHANGELOG.md).

[The problem](#the-problem) · [What shikii does](#what-shikii-does) · [What you do](#what-you-do) · [Requirements](#requirements) · [Get started](#get-started) · [One full loop](#one-full-loop) · [Learn from conversations](#learn-from-conversations) · [Memory belongs to a project](#memory-belongs-to-a-project) · [Read more](#read-more)

<p align="center"><img src="docs/assets/shikii-hero.svg" alt="shikii: a model gives you a probability; shikii tells you whether you may act on it" width="100%"></p>

## The problem

Ask an AI “is this inquiry the same matter as the last one?” and it says “probably, 0.9”. For real work that is not enough.

- Is that 0.9 really right 9 times in 10?
- Since the day you swapped in a new model, has it quietly started to drift?
- If you change the wording a little, how does the accuracy move?
- The decisions it took on its own, with nobody watching: are they still right?

Whoever hands work to an agent has another set of problems.

- You corrected it once and you are correcting the same thing in the next job.
- You wrote the lesson down, but you cannot tell whether the agent read it, or whether reading it changed anything.
- The subagents it spawns never see that lesson at all.

If a person has to check every answer and repeat every correction, there was little point in using the AI.

## What shikii does

1. **Collects your answers and draws the line above which the AI may act on its own.** From the truths people supply, and from results that can be observed automatically, it measures how the model's confidence relates to how often it is right. Until that line exists, no decision is `act`.
2. **Turns every decision into a band.** `act` proceeds automatically, `ask` goes to a person, `discard` does nothing, `abstain` means an input or an endpoint is missing. Automatic decisions carry a record of how many truths and human checks stand behind the line.
3. **Tries other ways of asking and keeps the better one.** On the same truths it scores alternative questions, option descriptions and prompt layouts. A person only confirms that the meaning is unchanged before a better one is adopted.
4. **Lets you swap the model without starting over.** Truths live apart from the model. The recommended model is TypeSafe Jev: on this project's pages and answers it separated lesson placement, contradictions and relevant lessons more clearly than the local model ([the numbers](docs/guide.md#jev-against-the-local-standard)). Without a key, the local standard is Ternary-Bonsai-27B, an ordinary chat model that `shikii-readout` turns into a decision model by reading the probability of each option letter, at about 0.6 s per question.
5. **Keeps what it learned in a wiki your agent uses.** Requests, replies and corrections become lessons with sources. Before a new lesson is saved, the decision model finds the page it belongs to and flags a contradiction; what the agent then saves becomes the truth for that judgement, so nobody labels it. Relevant lessons reach the agent before similar requests, and since 0.7.3 they reach a subagent when it starts, based on the parent's request.
6. **Turns your corrections into guards.** Corrections that a machine can check (“keep both changelogs in step”, “never use that word”, “write the original problem and the verification in the commit body”) become checks that run at the end of every turn. When one fails, the agent cannot end the turn until it fixes the work. Lessons that merely repeat what the agent can read in the repository changed nothing (4 of 5 tasks tied), while delivered lessons the agent could not know otherwise were followed 6 times in 6; a guard works even when the model knows no rule (6 of 6).
7. **Counts in production whether delivered lessons changed anything.** Reflection marks each delivered lesson helpful, unused, harmful or unknown, and `shikii status` shows the counts, plus how many turns guards stopped and how many were fixed before the turn ended. Measured in real work, not in synthetic experiments.

## What you do

- **Write down what you want decided.** A decision file holds the question, the options, the inputs and the thresholds (`shikii new`, or copy a [ready-made decision](decisions/README.md)).
- **Answer some questions.** The page shows the decisions the model was unsure about and 1 in 20 of the ones it acted on; you press the button for what was actually true. Results that can be observed (a change undone, a request repeated, a lesson merged) are answered by the machine.
- **Now and then confirm a wording.** When a better way of asking is found, you check that it asks the same thing and press adopt or reject.
- **Correct your agent as usual.** “There is still English left” in an ordinary conversation is enough; the agent files it as a lesson and shikii keeps it next to the right page. When the correction can be checked by a machine, the agent also drafts a guard, disabled.
- **Enable guards.** Run `shikii guard enable <id>` once. shikii first shows the guard stopping on a deliberately broken example and passing on a correct one, then enables it. Disabling is also your call.

Everything else is the machine's job.

## Requirements

| Use case | What you need |
|---|---|
| Learn from ordinary conversations and turn corrections into guards | Claude Code or Codex, shikii, and `shikii --init` in the projects you choose. Delivery to subagents is Claude Code only |
| Answer the built-in questions | Recommended: TypeSafe Jev with `TYPESAFE_API_KEY` in the environment, nothing to download. Without a key: llama.cpp (`llama-server`) and Ternary-Bonsai-27B through `shikii up`, about 7.6 GB to download and 10 GB of memory while running |
| Use another model for business decisions | Any System One endpoint from the table below |
| Install from source | Go 1.27 or newer, Git and sh. The page is bundled; Node.js is not needed |
| Run the business API and page in containers | Docker with Compose, or Podman with a Compose provider ([setup](integrations/docker.md)) |
| Try the whole loop without a key or GPU | Go 1.27 or newer and the included fake model (`scripts/demo.sh`) |

| Endpoint | What it needs |
|---|---|
| Jev (TypeSafe AI) | Recommended. `TYPESAFE_API_KEY`. The built-in questions and every business decision whose `send_to` allows `hosted` go here when the key is set. About 0.2 s per question. [Official API](https://docs.typesafe.ai/introduction/quickstart) |
| llama.cpp | Without a key. Any chat GGUF through the included `shikii-readout` (the local standard is Ternary-Bonsai-27B; `shikii up` sets both up). Trained decision models such as OpenJev are passed through. Kev-4B is not supported. [Examples](integrations/readout.md) |
| Ollama / LM Studio | Their `/v1/systemone` endpoints with a decision model, for business decisions. Not tested in this release |

## Get started

### 1. Put a TypeSafe key in the environment (recommended)

Get an API key from [TypeSafe AI](https://docs.typesafe.ai/introduction/quickstart) and put it in the environment of your shell and your agent (`export TYPESAFE_API_KEY=…`). Nothing is downloaded and an ordinary PC is enough. With the key, the hooks and the built-in questions go to Jev and no local model is started (`shikii up` does not start one either). The request text, the agent's reply and wiki lessons are sent to TypeSafe for the built-in questions; they also stay in the local records. `SHIKII_MODEL=local` keeps a local model even with a key; `SHIKII_MODEL=hosted` forces Jev.

**Without a key: llama.cpp.** shikii then runs a local model. It is a 27B model that needs about 10 GB of memory while running, which can be heavy for an ordinary PC. On macOS with Homebrew:

```sh
brew install go llama.cpp
go version            # 1.27 or newer
llama-server --version
```

For other systems, see [llama.cpp's installation guide](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md). The default local model is **Ternary-Bonsai-27B Q2_g64** (`prism-ml/Ternary-Bonsai-27B-gguf`, Apache 2.0, about 7.6 GB to download and 10 GB of memory while running). `shikii up` starts llama-server on :8086 and `shikii-readout` on :8080, so hooks, MCP and the page always talk to :8080. Any other GGUF works the same way (`shikii up --gguf <file>`).

### 2. Install shikii

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

The installer builds from source, so it needs Go 1.27 or newer and Git. From a source checkout, run `sh install.sh` inside its folder. The installer adds the binaries, the hooks (before a turn, after it, and when a subagent starts) and the agent's instructions, and says when the binary directory must be added to PATH. In Codex, use `/hooks` once to trust the hooks.

### 3. Initialize the projects you choose

In a project you want to enable, ask your agent:

> Use shikii in this project.

The agent runs `shikii --init` (you can run it yourself). Only initialized projects are enabled. The answer page is one per project: the first request's hook starts it on a free port and opens it in the browser (`shikii page` tells you the URL). Initialization prepares the built-in questions, the wiki, the place for guards, the folder for work records, the MCP setup (it adds `.mcp.json`, `.cursor/mcp.json`, `opencode.json` and a section in `AGENTS.md` (not when the project has only `CLAUDE.md`) to the project; only `.shikii/` is excluded from Git) and the agent's instructions. `shikii doctor` checks the setup.

### Updates

```sh
shikii update
```

This fetches the latest stable release, verifies its hashes and replaces shikii and readout. **Updating from 0.6.0 or earlier erases the contents of `.shikii/` in every initialized project and initializes them again.** Earlier conversations, lessons, truths and calibration are removed; back up anything you want to keep. Updates from 0.7.0 onwards keep them. Restart the page and MCP afterwards.

## One full loop

In a source checkout (the sample decision is not part of the installer):

```sh
shikii new same-issue --from decisions/same-issue.yaml              # copies the sample decision into .shikii/decisions/
SHIKII_MODEL=local shikii up                                     # this question is pii: true, so it needs the local model; with a key set, up only starts one under SHIKII_MODEL=local
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask   (no line yet: a person decides)
shikii serve --local http://127.0.0.1:8080                       # answer page: http://127.0.0.1:8095
#   Once a decision has the number of truths it asks for (eval.min_n), the line is drawn by itself.
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act   with a record (how many truths, how many checked by a person, the precision of the act side)
```

| Band | What to do |
|---|---|
| `act` | Proceed automatically under the calibrated conditions |
| `ask` | Check with a person |
| `discard` | Do not act |
| `abstain` | Supply the missing input or an available endpoint |

Calibration belongs to one question and one model; change either and it is measured again. `shikii tune` scores alternative wordings on the stored truths; `shikii drift` and `shikii check` catch a model that drifted or a line that was weakened. To try this without a key or GPU, run `scripts/demo.sh` in the checkout.

- [Ready-made decisions](decisions/README.md) · [A business example](docs/examples/inquiry-route.md) (Japanese) · [Every command and the HTTP API](docs/guide.md) · [MCP](integrations/mcp.md) · [Measured numbers](docs/bench.md)

## Learn from conversations

With the hooks installed, two moments of every turn are kept: the request before the agent starts, and the reply and the changed files after it ends. The next human message closes the pair. The agent writes lessons with sources from them; shikii delivers up to two relevant lessons before similar work, and delivers lessons close to the parent's request when a subagent starts. In reflection the agent reports whether a delivered lesson helped.

<p align="center"><img src="docs/assets/shikii-conversation.svg" alt="requests and corrections become lessons with sources and next actions; observed results flow back into reflection" width="100%"></p>

The decision model answers the built-in questions about the agent's own work (what kind of request this is, whether to agree on an approach first, whether the changes stayed in scope, whether the request was completed) and, for new lessons, where they belong and whether they contradict a page. The truths for these come from what happens next: a change undone during the next request, a request repeated, a lesson merged or replaced. Only questions that results cannot answer wait for a person, and the page shows how many.

**Guards** are fixed rules, not the decision model. At the end of a turn they are applied to the changed files, the added lines and the commits of that turn. There are three kinds.

| Kind | Stops when | Example |
|---|---|---|
| `paired-files` | one file changed and its partner did not | `CHANGELOG.md` and `CHANGELOG_ja.md` |
| `forbidden-words` | an added line contains a word you decided not to use | words with an agreed replacement; excepted paths are not checked |
| `commit-body` | a commit made in the turn lacks a required word in its body | “original problem”, “after the change”, “verification” |

The agent drafts a guard from a correction (`shikii guard add`, disabled) and a person enables it (`shikii guard enable <id>`). Before enabling, shikii shows the guard stopping on a deliberately broken example and passing on a correct one. A turn that trips a guard cannot end until the work is fixed; the continuation after a stop is never stopped again, only recorded as fixed or not.

| Work | Usually done by | Your part |
|---|---|---|
| Lessons from conversations | Your usual agent | Correct it in ordinary conversation |
| Lesson placement, scope, completion | The decision model; truths from results | Check the samples and the undecided ones |
| Guards | Fixed rules; drafts by the agent | Decide whether to enable |
| Business truths and the meaning of a wording | You | Label, and confirm the meaning before adoption |

`shikii status` shows whether it is working: how many lessons were delivered and marked helpful, unused, harmful or unknown; how many turns guards stopped and how many were fixed; how many decisions wait for a person.

The design follows [Andrej Karpathy's LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f): raw sources are kept, and the agent ingests, searches and revises. Details: [the hooks](integrations/claude-code.md) (Japanese), [wiki editing](skills/shikii/references/wiki.md), [guards](skills/shikii/references/guards.md).

## Memory belongs to a project

Conversations, lessons, provisional assessments, truths, calibration, guards and work records live in that project's `.shikii/`. Unrelated projects stay apart. The shared `~/.shikii/agent` holds the templates, the tools and the list of projects. Because `.shikii/` contains private conversations, initialization keeps it out of Git's untracked files.

<p align="center"><img src="docs/assets/shikii-architecture.svg" alt="an initialized project keeps conversation lessons and provisional observations apart from business questions and truths; the shared place holds templates, tools and links to projects" width="100%"></p>

```text
.shikii/
  decisions/  questions for the decision model
  wiki/       lessons, conditions, earlier revisions
  guards/     checks made from your corrections
  records/    conversations, decisions, assessments, deliveries, observations, guard results
  labels/     truths from people and from results
  handoff/    work records: audit reports, release steps, diffs, screenshots (read on the page)
```

## Read more

- [The guide](docs/guide.md): every command, the HTTP API, connecting models, measured numbers
- [Architecture](docs/architecture.md) (Japanese)
- [The agent's instructions](skills/shikii/SKILL.md)
- [Changelog](CHANGELOG.md)

**For contributors**: tests are broken on purpose before they are fixed. Observations and truths are kept apart, and only measured numbers are written down. Fixed places and rules are preferred to more settings.

## License

[Apache-2.0 OR MIT](LICENSE). Choose [Apache 2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT). Third-party dependencies, bundled code, models and external services keep their own licenses. Distributions must include the three license files and the third-party notices.

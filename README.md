**English** · [日本語](docs/readme/README_ja.md) · [简体中文](docs/readme/README_zh.md) · [繁體中文](docs/readme/README_zh-TW.md) · [한국어](docs/readme/README_ko.md) · [Deutsch](docs/readme/README_de.md) · [Español](docs/readme/README_es.md) · [Français](docs/readme/README_fr.md) · [Italiano](docs/readme/README_it.md) · [Português (Brasil)](docs/readme/README_pt-BR.md) · [Русский](docs/readme/README_ru.md) · [Українська](docs/readme/README_uk.md) · [Polski](docs/readme/README_pl.md) · [Türkçe](docs/readme/README_tr.md) · [Tiếng Việt](docs/readme/README_vi.md) · [Bahasa Indonesia](docs/readme/README_id.md)

<p align="center"><img src="docs/assets/gen-mark.png" alt="shikii" width="96"></p>

# shikii: turn an AI's “probably” into a “safe to act”

A model gives you a probability. shikii tells you whether you may act on it. It draws the line from the answers people and results supply, turns every decision into a band (`act` / `ask` / `discard`), measures better ways of asking, and keeps what it learned in a wiki that your agent uses while it works.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](go.mod)
[![Tests](https://img.shields.io/badge/tests-322%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](docs/decision-ops-framework.md)
[![Version](https://img.shields.io/badge/version-0.7.2-00ADD8?style=flat-square)](CHANGELOG.md)

Version 0.7.0 goes back to the line and the bands. The recommended model is TypeSafe Jev: put the key in the environment, download nothing, and get an answer in about 0.2 s. Without a key, llama.cpp with Ternary-Bonsai-27B answers the same questions locally (`shikii-readout` runs an ordinary chat GGUF at decision-model speed). New wiki lessons are placed next to the right page and checked for contradictions by the decision model. **Updating to 0.7.0 erases the contents of `.shikii/` in every initialized project and initializes them again**; back up first. See the [changelog](CHANGELOG.md).

[The problem](#the-problem) · [What shikii does](#what-shikii-does) · [What you do](#what-you-do) · [Requirements](#requirements) · [Get started](#get-started) · [One full loop](#one-full-loop) · [Learn from conversations](#learn-from-conversations) · [Memory belongs to a project](#memory-belongs-to-a-project) · [Read more](#read-more)

<p align="center"><img src="docs/assets/shikii-hero.svg" alt="shikii: a model gives you a probability; shikii tells you whether you may act on it" width="100%"></p>

## The problem

Ask an AI “is this inquiry the same matter as the last one?” and it says “probably, 0.9”. For real work that is not enough.

- Is that 0.9 really right 9 times in 10?
- Since the day you swapped in a new model, has it quietly started to drift?
- If you change the wording a little, how does the accuracy move?
- The decisions it took on its own, with nobody watching: are they still right?

If a person has to check every answer, there was little point in using the AI.

## What shikii does

1. **Collects your answers and draws the line above which the AI may act on its own.** From the truths people supply, and from results that can be observed automatically, it measures how the model's confidence relates to how often it is right. Until that line exists, no decision is `act`.
2. **Turns every decision into a band.** `act` proceeds automatically, `ask` goes to a person, `discard` does nothing, `abstain` means an input or an endpoint is missing. Automatic decisions carry a receipt stating how many truths and human checks stand behind the line.
3. **Tries other ways of asking and keeps the better one.** On the same truths it scores alternative questions, option descriptions and prompt layouts. A person only confirms that the meaning is unchanged before a better one is adopted.
4. **Lets you swap the model without starting over.** Truths live apart from the model. The recommended model is TypeSafe Jev: on this project's pages and answers it separated lesson placement, contradictions and relevant lessons more clearly than the local model ([the numbers](docs/guide.md#jev-against-the-local-standard)). Without a key, the local standard is Ternary-Bonsai-27B, an ordinary chat model that `shikii-readout` turns into a decision model by reading the probability of each option letter, at about 0.6 s per question.
5. **Keeps what it learned in a wiki your agent uses.** Requests, replies and corrections become lessons with sources. Before a new lesson is saved, the decision model finds the page it belongs to and flags a contradiction; what the agent then saves becomes the truth for that judgement, so nobody labels it.

## What you do

- **Write down what you want decided.** A decision file holds the question, the options, the inputs and the thresholds (`shikii new`, or copy a [ready-made decision](decisions/README.md)).
- **Answer some questions.** The page shows the decisions the model was unsure about and 1 in 20 of the ones it acted on; you press the button for what was actually true. Results that can be observed (a change undone, a request repeated, a lesson merged) are answered by the machine.
- **Now and then confirm a wording.** When a better way of asking is found, you check that it asks the same thing and press adopt or reject.
- **Correct your agent as usual.** “There is still English left” in an ordinary conversation is enough; the agent files it as a lesson and shikii keeps it next to the right page.

Everything else is the machine's job.

## Requirements

| Use case | What you need |
|---|---|
| Learn from ordinary conversations | Claude Code or Codex, shikii, and `shikii --init` in the projects you choose |
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

From a source checkout, run `sh install.sh` inside its folder. The installer adds the binaries, two hooks (before and after a turn) and the agent's instructions, and says when the binary directory must be added to PATH. In Codex, use `/hooks` once to trust the hooks.

### 3. Initialize the projects you choose

In a project you want to enable, ask your agent:

> Use shikii in this project.

The agent runs `shikii --init` (you can run it yourself). Only initialized projects are enabled. Initialization prepares the built-in questions, the wiki, the MCP setup, the agent's instructions and the project's page. `shikii doctor` checks the setup; http://127.0.0.1:8096 lists the project pages.

### Updates

```sh
shikii update
```

This fetches the latest stable release, verifies its hashes and replaces shikii and readout. **The update to 0.7.0 erases the contents of `.shikii/` in every initialized project and initializes them again.** Earlier conversations, lessons, truths and calibration are removed; back up anything you want to keep. Later updates keep them. Restart the page and MCP afterwards.

## One full loop

```sh
shikii up                                                        # this question is pii: true, so it stays on the local model. The built-in questions and pii: false decisions go to Jev when the key is set
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask   (no line yet: a person decides)
shikii serve --local http://127.0.0.1:8080                       # answer page: http://127.0.0.1:8095
#   Once a decision has the number of truths it asks for (eval.min_n), the line is drawn by itself.
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act   with a receipt (how many truths, how many checked by a person, the precision of the act side)
```

| Band | What to do |
|---|---|
| `act` | Proceed automatically under the calibrated conditions |
| `ask` | Check with a person |
| `discard` | Do not act |
| `abstain` | Supply the missing input or an available endpoint |

Calibration belongs to one question and one model; change either and it is measured again. `shikii tune` scores alternative wordings on the stored truths; `shikii drift` and `shikii check` catch a model that drifted or a line that was weakened. To try this without a key or GPU, run `scripts/demo.sh` in the checkout.

- [Ready-made decisions](decisions/README.md) · [a business example](docs/examples/inquiry-route.md) (Japanese) · [all commands and the HTTP API](docs/guide.md) · [MCP](integrations/mcp.md) · [measurements](docs/bench.md)

## Learn from conversations

With the hooks installed, two moments of every turn are kept: the request before the agent starts, and the reply with the changed files after it. The next human message closes the pair. From these the agent writes lessons with sources; shikii delivers up to two related lessons before similar work and the agent reports in its reflection whether they helped.

<p align="center"><img src="docs/assets/shikii-conversation.svg" alt="A request and correction become a sourced lesson and an action for similar work; observed results feed reflection" width="100%"></p>

The decision model answers the built-in questions about the agent's own work (what kind of request this is, whether an approach should be agreed first, whether the changes stayed in scope, whether the request was completed) and, in 0.7.0, where a new lesson belongs and whether it contradicts an existing one. Truths for these come from what happens next: a change undone during the next request, a request repeated, a lesson merged or replaced. Only questions that results cannot answer wait for a person, and the page shows how many.

| Work | Routine owner | A person's part |
|---|---|---|
| Lessons from conversations | The active agent | Correct it in ordinary conversation |
| Where a lesson belongs, scope, completion | The decision model, answered by results | Check samples and undecided cases |
| Business truths and the meaning of a wording | People | Supply truths, confirm meaning before adoption |

This follows [Andrej Karpathy's LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f): keep the sources, let the agent ingest, query and maintain. Details: [hooks](integrations/claude-code.md) (Japanese), [the wiki workflow](skills/shikii/references/wiki.md).

## Memory belongs to a project

Conversations, lessons, provisional assessments, truths and calibration live in that project's `.shikii/`. Unrelated projects stay separate. The shared `~/.shikii/agent` holds templates, tools and the project index. Initialization keeps `.shikii/` out of untracked Git files because it contains private conversations.

<p align="center"><img src="docs/assets/shikii-architecture-en.svg" alt="An initialized project keeps conversation lessons and provisional observations separate from business questions and truths; the shared area contains templates, tools and project links" width="100%"></p>

```text
.shikii/
  decisions/  Questions for the decision model
  wiki/       Lessons, conditions and earlier revisions
  records/    Conversations, decisions, assessments, deliveries and observations
  labels/     Truths supplied by people or outcomes
```

## Read more

- [The guide](docs/guide.md): every command, the HTTP API, connecting models
- [Architecture](docs/architecture.md) (Japanese)
- [Agent instructions](skills/shikii/SKILL.md)
- [Changelog](CHANGELOG.md)

**For contributors**: break a test and watch it fail before fixing it. Keep observations apart from truths, and ground numbers in measurements. Prefer fixed places and rules over settings.

## License

[Apache-2.0 OR MIT](LICENSE), at your option: [Apache 2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT). Third-party dependencies, bundled code, models and external services keep their own licenses. Distributions should include all three license files and the applicable third-party notices.

**English** · [日本語](docs/readme/README_ja.md) · [简体中文](docs/readme/README_zh.md) · [繁體中文](docs/readme/README_zh-TW.md) · [한국어](docs/readme/README_ko.md) · [Deutsch](docs/readme/README_de.md) · [Español](docs/readme/README_es.md) · [Français](docs/readme/README_fr.md) · [Italiano](docs/readme/README_it.md) · [Português (Brasil)](docs/readme/README_pt-BR.md) · [Русский](docs/readme/README_ru.md) · [Українська](docs/readme/README_uk.md) · [Polski](docs/readme/README_pl.md) · [Türkçe](docs/readme/README_tr.md) · [Tiếng Việt](docs/readme/README_vi.md) · [Bahasa Indonesia](docs/readme/README_id.md)

<p align="center"><img src="docs/assets/gen-mark.png" alt="shikii" width="96"></p>

# shikii: turn a correction into the next action

Your usual agent builds lessons from requests, replies and corrections, with links to the original conversation. Related lessons arrive before similar work. Reflection records how they affected the next decision. People keep asking for work as usual and check samples and undecided cases.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](go.mod)
[![Tests](https://img.shields.io/badge/tests-261%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](docs/decision-ops-framework.md)
[![Version](https://img.shields.io/badge/version-0.4.2-00ADD8?style=flat-square)](CHANGELOG.md)

Version 0.4.2 adds `shikii update` for future upgrades. It also includes the correction tracking, concise memory and batched reflection from earlier versions. See the [changelog](CHANGELOG.md).

[What changes](#what-changes) · [Requirements](#requirements) · [Get started](#get-started) · [Learn from ordinary conversations](#learn-from-ordinary-conversations) · [What people check](#what-people-check) · [Check whether memory helped](#check-whether-memory-helped) · [Memory belongs to a project](#memory-belongs-to-a-project) · [Use it for business decisions](#use-it-for-business-decisions) · [Read more](#read-more)

<p align="center"><img src="docs/assets/shikii-hero.svg" alt="shikii: a model gives you a probability; shikii tells you whether you may act on it" width="100%"></p>

## What changes

“I already explained that.” “It remembers what I said, but still does the same thing.” shikii aims to reduce this repeated explanation and correction.

For example, you ask for a Japanese version of some instructions, then point out: “There is still English left.”

| Moment | What shikii and the agent keep or do |
|---|---|
| A correction arrives | Preserve the request, the reply and the person's statement as source material |
| A lesson is written | Link “Check the whole text when translating instructions” to its conditions, the quoted correction and a concrete action |
| Similar work comes next | Deliver a short quote; the agent checks whether it applies and uses it in the work |
| Reflection | Link the decision to evidence of the full-text check or a recurrence of the same issue |

<p align="center"><img src="docs/assets/shikii-conversation.svg" alt="A request and correction become a sourced lesson and an action for similar work; observed results feed reflection" width="100%"></p>

The goal is fewer repeated corrections as you use it. What exists today is the mechanism for checking that in real work. Stored memory counts and an agent's own scores alone do not demonstrate improvement.

## Requirements

Choose the combination for your use case. The agent that organizes conversations and the model that answers decision questions have different jobs.

| Use case | What you need |
|---|---|
| Learn from ordinary conversations | Claude Code or Codex, shikii, and initialization of the chosen project |
| Automatically evaluate the four built-in questions | The standard setup uses llama.cpp (`llama-server`) and Kev-4B. Hooks use `127.0.0.1:8080` |
| Use another model for business decisions | Choose a System One endpoint and decision model from the table below |
| Install from source | Go 1.27 or newer, Git and sh. The UI is bundled; Node.js is unnecessary |
| Run the business API and page in containers | Docker with Compose, or Podman with a Compose provider. Build containers supply Go and Node.js |
| Try the workflow without a key or GPU | Go 1.27 or newer and the included fake model (`scripts/demo.sh`) |
| Develop the UI or documentation | Node.js 24 and npm for the UI; Python 3 for diagram generation and documentation checks |

The business CLI, MCP server and `serve` can use these model endpoints:

| Endpoint | Preparation and verification |
|---|---|
| llama.cpp | A decision model such as Kev-4B. Ordinary chat GGUFs need the included `shikii-readout` adapter. [Examples](integrations/readout.md) |
| Ollama | v0.35.0 or newer with System One and a decision model. Its documented `/v1/systemone` shape matches the client; inference on Ollama was not tested in this update. [Official API](https://docs.ollama.com/api/systemone) |
| LM Studio | The Jev-compatible `/v1/systemone` endpoint and a loaded decision model. Documentation matches the client; inference on LM Studio was not tested in this update. [Official API](https://lmstudio.ai/docs/developer/jev-compat/systemone) |
| Jev (TypeSafe AI) | Hosted API access and `TYPESAFE_API_KEY`. Use only for decisions permitted to leave the machine. [Official API](https://docs.typesafe.ai/introduction/quickstart) |

These alternatives do not automatically replace the built-in hooks' llama.cpp setup. Select an endpoint URL and decision model for business use. A chat-completions endpoint alone is insufficient. See [connection requirements](docs/guide.md#what-you-can-connect).

Podman `compose` invokes an external Compose provider. See [Podman's documentation](https://docs.podman.io/en/latest/markdown/podman-compose.1.html). A Dockerfile and Compose file are included; Podman execution was not tested in this update. Containers run the business API and page; host-agent hooks are configured separately. See [Docker and Podman setup](integrations/docker.md).

## Get started

Automatic conversation recording supports Claude Code and Codex. Install once, initialize the projects you choose, then work as usual.

### 1. Prepare Go and llama.cpp

On macOS with Homebrew, use the official [Go](https://formulae.brew.sh/formula/go) and [llama.cpp](https://formulae.brew.sh/formula/llama.cpp) formulae. If needed, set up Homebrew from [its official site](https://brew.sh/) first.

```sh
brew install go llama.cpp
go version
llama-server --version
```

Check that Go is 1.27 or newer and `llama-server` is available. For other systems, see [llama.cpp's installation guide](https://github.com/ggml-org/llama.cpp/blob/master/docs/install.md).

The default model is **Kev-4B, Q4_K_M** (`ggml-org/Kev-4B-GGUF:Q4_K_M`). It downloads about 2.5 GB on first start and serves the four built-in decision questions locally on :8080. Your usual agent remains responsible for organizing conversation lessons.

### 2. Install shikii

With Go 1.27 or newer, install with one command:

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

To install from a source checkout instead, clone shikii and run this inside its folder:

```sh
sh install.sh
```

The installer adds the binaries, two hooks (before and after a turn), and the agent's instructions. It uses llama.cpp for the local decision model and explains how to prepare it if missing. It also reports when the binary directory needs adding to PATH. In Codex, use `/hooks` once to trust the hooks.

### 3. Initialize the project you choose

Open the agent in a new terminal, in a project you want to enable, and ask:

> Use shikii in this project.

The agent runs `shikii --init`; you can run it yourself too. Only initialized projects are enabled. Initialization prepares four decisions, the wiki, MCP setup, agent instructions and a link to the project's page. Reinitialization preserves existing data. Check setup with `shikii doctor` and open http://127.0.0.1:8096 for the project pages.

### Future updates

```sh
shikii update
```

This checks the latest stable release, verifies its hashes, and updates shikii and readout in the current binary directory. Go is unnecessary for updates. Conversations, lessons and truths are preserved, and existing hooks and agent instructions are refreshed. Restart running UI and MCP processes to use the new binary. [Details](docs/guide.md#update-shikii).

## Learn from ordinary conversations

1. **Keep the conversation.** The next human message joins the previous request and reply as one source observation.
2. **Let the usual agent organize it.** The agent writes lessons with sources and conditions during its ordinary work. Explicit corrections can include the quoted statement and the action to change.
3. **Recall briefly when relevant.** Up to two related lessons arrive for a similar request in the same project. The same revision is suppressed for one hour in a session; updated revisions arrive immediately.
4. **Reflect in batches.** Assessment, wiki work and memory observations are grouped after two pending conversations, or at the next request after a single conversation has waited a day. People need not perform these steps each time.

Lessons remain hypotheses with sources. The current request and rules take priority. Original conversations and earlier revisions remain available. Periodic checks inspect sources, revisions and links, and repair the index. The active agent organizes the conversations without starting another model for this work.

This follows [Andrej Karpathy's LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f): retain source material and let the agent handle ingestion, queries and maintenance. See [the workflow](skills/shikii/references/wiki.md) (Japanese).

## What people check

| Work | Routine owner | A person's part |
|---|---|---|
| Organize lessons from conversations | The active agent | Correct it in ordinary conversation; no separate organizing steps |
| Assess scope and completion | The agent, using outcomes and the next reply | Check random samples and undecided cases |
| Decide whether to agree on an approach first | A decision model makes the initial assessment | Supply the needed truths on the page |
| Business truths and the meaning of proposed wording | People or observable outcomes | Supply truths and check meaning before adoption |

Provisional AI assessments are stored separately from human truths. Corrections made during spot-checking return as examples for later assessment. Scope and completion assessments are not automatically fed into calibration. See [assessment and sampling](skills/shikii/references/review.md) (Japanese).

The Knowledge panel shows lessons, sources and memory observations. The Wording panel measures alternatives and lets a person adopt one after checking its meaning. You do not need to open these panels for each conversation to be remembered.

## Check whether memory helped

The delivered quote and revision stay linked to the request. Routine reflection connects them to a decision and an observed result, using two separate observations.

| Question | Recorded outcomes |
|---|---|
| How did memory affect the decision? | Helpful, unused, harmful, unknown |
| Was a delivered correction reflected in the next action? | Followed, same issue repeated, not applicable, unknown |

The Knowledge panel includes delivery counts and pending assessments. Following a correction and finding a memory helpful are assessed separately. Success, another request or silence alone does not establish an effect. These are evidence-backed AI observations, not an improvement rate measured against a no-memory control.

## Memory belongs to a project

Conversations, lessons, provisional assessments, truths and calibration belong to that project's `.shikii/`. Unrelated projects, including ones with the same name, stay separate. Subdirectories of the same project use the same store.

<p align="center"><img src="docs/assets/shikii-architecture-en.svg" alt="An initialized project keeps conversation lessons and provisional observations separate from business questions and truths; the shared area contains templates, tools and project links" width="100%"></p>

The four main storage areas are:

```text
.shikii/
  decisions/  Questions for the decision model
  wiki/       Lessons, conditions and earlier revisions
  records/    Conversations, decisions, assessments, deliveries and observations
  labels/     Truths supplied by people or outcomes
```

The shared `~/.shikii/agent` holds templates, tools and the project index. Initialization excludes `.shikii/` from untracked Git files because it contains private conversations. Review and select material before sharing it. Memory is stored outside model weights and remains available when you change agents or decision models.

## Use it for business decisions

Use shikii for repeated decisions such as routing inquiries or detecting duplicate bug reports.

> Have shikii decide whether an inquiry is the same case as an earlier one, and improve it from checked answers.

The agent prepares the question, options and inputs, then starts the decision service and answer page. People check the intended question and supply truths. These support a line above which the model may act. Until that line exists, no decision is `act`.

| Band | What to do |
|---|---|
| `act` | Proceed automatically under the calibrated conditions |
| `ask` | Check with a person |
| `discard` | Do not act |
| `abstain` | Supply missing input or an available endpoint |

Calibration belongs to a particular question and model; changes require measurement again. Automatic decisions carry a receipt stating the truths and human checks behind the line. Conversation hypotheses and provisional assessments alone never create it.

- [Ready-made decisions](decisions/README.md), [a business example](docs/examples/inquiry-route.md) (Japanese), [all commands and HTTP API](docs/guide.md)
- [MCP](integrations/mcp.md), [Docker](integrations/docker.md), [measurements](docs/bench.md)

To try the business-decision lifecycle without a key or GPU, run this in the shikii checkout with Go 1.27 or newer. Fake-model numbers demonstrate the workflow only.

```sh
scripts/demo.sh
```

## Read more

- [The guide](docs/guide.md): conversation reflection, CLI and HTTP API
- [Hooks](integrations/claude-code.md) (Japanese): work done before and after a turn
- [Architecture](docs/architecture.md) (Japanese): conversation learning, business decisions, storage and ownership
- [Changelog](CHANGELOG.md)
- [Agent instructions](skills/shikii/SKILL.md) (Japanese)

**For contributors**: break a test and observe its failure before fixing it. Separate observations from truths, and ground numerical claims in measurements. Keep fixed places and rules rather than adding settings.

## License

[Apache-2.0 OR MIT](LICENSE), at your option: [Apache 2.0](LICENSE-APACHE) or [MIT](LICENSE-MIT). The licenses of third-party dependencies, bundled code, models and external services remain separate. Source or binary distributions should include all three license files and applicable third-party notices. Existing 0.4.0 release assets retain their original Apache-2.0 notice.

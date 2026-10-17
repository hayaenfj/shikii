**English** · [日本語](docs/readme/README_ja.md) · [简体中文](docs/readme/README_zh.md) · [繁體中文](docs/readme/README_zh-TW.md) · [한국어](docs/readme/README_ko.md) · [Deutsch](docs/readme/README_de.md) · [Español](docs/readme/README_es.md) · [Français](docs/readme/README_fr.md) · [Italiano](docs/readme/README_it.md) · [Português (Brasil)](docs/readme/README_pt-BR.md) · [Русский](docs/readme/README_ru.md) · [Українська](docs/readme/README_uk.md) · [Polski](docs/readme/README_pl.md) · [Türkçe](docs/readme/README_tr.md) · [Tiếng Việt](docs/readme/README_vi.md) · [Bahasa Indonesia](docs/readme/README_id.md)

<p align="center"><img src="docs/assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: Operate Decision Models

### One lifecycle for every decision model: define → decide → record → label → calibrate → check → drift.

**A model gives you a probability. shikii tells you whether you may act on it.**

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](go.mod)
[![Tests](https://img.shields.io/github/actions/workflow/status/hayaenfj/shikii/test.yml?style=flat-square&label=tests)](.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](docs/operations.md)

**[Design notes](docs/decision-ops-framework.md) · [Operations](docs/operations.md) · [Model comparison](docs/model-comparison.md)**

[Quick start](#quick-start) · [How shikii works](#how-shikii-works) · [What you can connect](#what-you-can-connect) · [Results](#results) · [FAQ](#faq) · [Documentation](#documentation)

<p align="center"><img src="docs/assets/shikii-hero.svg" alt="shikii: a model gives you a probability, shikii tells you whether you may act on it" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii never runs a model and never reads a keychain. It talks to a decision-model endpoint you already have, keeps the records, and refuses to **act** until the probabilities for *this question on this model* have been verified against labels. Calibration blocks are generated, never written by hand.

---

Decision models (TypeSafe Jev, llama.cpp Kev-4B and OpenJev, OpenRouter Decisions, …) answer typed questions, `choice`, `score` and `noul` (yes/no), with probabilities and no generated text. Every vendor ships the same disclaimer: *the probabilities are not calibrated for your data, and thresholds do not carry across models or questions.*

shikii owns the part nobody ships: the **lifecycle** of a decision. A spec file holds the question, the options, the state template and the thresholds. `decide` asks a model and returns a band (`act` / `ask` / `discard` / `abstain`), never a raw number. `label` records what was true. `calibrate` fits and cross-validates, then writes the result back into the spec under a key made of the question's hash and the model's name. `check` fails CI when the wording or the model drifted. `gc` keeps the store small.

It also comes with `shikii-readout`, a 500-line adapter that turns **any chat GGUF on a stock llama.cpp** into a decision model by reading the logits of the option letters. In our measurements an ordinary 27B ternary model read this way matched trained decision models on accuracy, at 0.6 seconds a question on a laptop.

> The name is Japanese: *shikii* (閾) means threshold, the line that turns a probability into an action. The command is `shikii` too.

## Quick start

Go 1.24 or newer. Nothing else is needed for the first run.

### Run the whole loop without a key or a GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # fake model: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # a fake model whose probabilities need calibration; watch calibrate pick platt
```

### Point it at a real model

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) local decision model on llama.cpp (downloads Kev-4B, about 2.5 GB, on first run)
make server                                       # llama-server on :8080
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

### Or run one process for programs and people

```sh
bin/shikii serve --specs decisions --local http://127.0.0.1:8080        # http://127.0.0.1:8095/
curl -s localhost:8095/v1/decide -d '{"spec":"prompt-intent","inputs":{"message":"fix the typo"}}'
```

Programs call `POST /v1/decide` and `POST /v1/label` from any language. People open the page: every decision that is still waiting for an answer is listed, `ask` first, with one button per option. Press the button, the truth is stored, and when a spec reaches `eval.min_n` the server calibrates and writes the spec by itself. Nobody types a trace id. The page is a single HTML file with no external loads, so it works on a laptop with no internet. It has a language switch; add a `display:` block to a spec (per language: title, question, option or level names) and the page shows the decision in that language too. `display` is never sent to the model and is not part of the calibration key.

The same thing as MCP tools for an agent: `shikii mcp` speaks the Model Context Protocol over stdio and exposes `shikii_specs`, `shikii_decide`, `shikii_label` and `shikii_status`. Claude Code picks it up from the repo's `.mcp.json`. See [`integrations/mcp.md`](integrations/mcp.md).

### Use any GGUF as a decision model

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # stock llama-server with flash attention and 8-bit KV, :8086
make readout                                               # shikii-readout in front of it, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Install and update

```sh
make install          # copies bin/shikii to ~/.local/bin/shikii
git pull && make install
```

## How shikii works

Every decision is one YAML file. The question, the options, the state template, where the state may be sent, the thresholds, and, generated by `calibrate --write`, the calibration blocks.

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
policy:
  send_to: [hosted, local]       # a tainted session skips hosted automatically
  pii: true                      # the text may contain personal information → never sent to a hosted endpoint
thresholds:
  act: 0.75                      # ≥ act → act automatically, only if calibrated
  ask: 0.60                      # ≥ ask → ask a human; below → discard
```

<p align="center"><img src="docs/assets/shikii-lifecycle.svg" alt="one lifecycle for every decision model" width="100%"></p>

**The calibration key is `(hash of question + template + options, model id as returned by the endpoint)`.** Edit one word of the question, or let `jev-latest` roll to a new version, and the calibration is simply gone: the band drops to `ask` instead of silently acting on stale constants. Getting it back is machine time, not human time: truths are kept per input together with the text, independent of wording and model, and `calibrate` replays them against the new key. Prompt layout is part of the key too: `shikii-readout` reports its layout in the model name (`…+state-last`), because moving the state inside the prompt changed a 27B model's accuracy by 5 points with the same weights.

`calibrate` compares three methods under 5-fold cross-validation and writes the winner. `identity` means the raw probabilities already meet precision@act and the model is simply marked as verified. `platt` (σ((Δ−bias)/T)) is for yes/no questions. `vector` (softmax((log p + b)/T)) is for choice and score, and fixes per-option habits such as "falls back to *other*". Partial labels (`!option`, "it was not *that*") count toward precision but not toward fitting. Labels obtained from a file are persisted, so `check` and `drift` run offline afterwards.

Every safety-relevant branch (taint routing, no act without calibration, stale-hash invalidation, label validation, inversion detection, CV-based method choice, dry-run `gc`, …) has a recorded mutation that made a test fail before the fix was accepted. There are 63 Go tests across 8 packages; the demo runs in CI.

## What you can connect

shikii speaks the System One request shape (`POST /v1/systemone`), so anything that serves it works. The adapter covers everything else.

| Target | What shikii gets | Requirements and guide |
|---|---|---|
| **TypeSafe Jev** (hosted) | `choice` / `score` / `noul` with probabilities, model id such as `jev-1.13.0` | `TYPESAFE_API_KEY` in the environment. Default `--hosted` |
| **llama.cpp decision models** (Kev-4B, OpenJev, Clef, …) | Same shape, locally, model id from `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 or newer |
| **OpenRouter Decisions API** | Same shape through OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **Any chat GGUF** via `shikii-readout` | Letter-logit readout in System One shape, no calibration of its own | Stock `llama-server` 0.7.0 behind it. [`integrations/readout.md`](integrations/readout.md) |
| **PrismML Bonsai** via [BonJev](https://github.com/ChezRD/bonjev) | Same, with BonJev's own prompt and built-in calibration | Rust build and the Prism fork. [`integrations/bonjev.md`](integrations/bonjev.md) |
| **Fake model** (`bin/fake-systemone`) | Deterministic probabilities for demos and tests | Nothing. `make fake` |
| **Claude Code hook** (claude-jev) | Each prompt classified, each turn labelled from the working tree | [`integrations/claude-code.md`](integrations/claude-code.md) |

Taint is a first-class input: pass `--tainted` (or let your hook do it) in a session that touched production data and the request never leaves the machine. If no local endpoint is configured, the decision abstains.

## Results

Same specs, same labels, five evaluation sets, all synthetic: prompt intent (62 requests, English and Japanese), duplicate bug reports (32 Japanese pairs) and same-matter customer inquiries (32 Japanese pairs). Details and the full table are in [`docs/model-comparison.md`](docs/model-comparison.md).

| Model | Licence | top-1 (all / ja) | yes/no Brier, raw → chosen (bug reports / inquiries) | Seconds per question |
|---|---|---|---|---|
| Kev-4B (trained decision model) | Apache 2.0 | 0.919 / 0.913 | 0.038 → 0.035 platt / 0.030 → 0.020 platt | 0.25 |
| OpenJev 27B (trained) | CC BY-NC 4.0 | **0.984 / 1.000** | **0.007** identity / 0.037 identity | 3.8 |
| Bonsai 2 27B via BonJev | Apache 2.0 | 0.968 / 1.000 | 0.049 → **0.019** platt / 0.084 → 0.028 platt | 2.5 |
| **Ternary-Bonsai-27B via `shikii-readout`, stock llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.048 → 0.024 platt / 0.033 → 0.020 platt | **0.58** |

**Wording beats calibration.** On prompt intent, 19 of 21 errors landed on *other*; rewording the options took top-1 from 0.68 to 0.90 before any calibration. Calibration is for "right order, not enough confidence". The confusion matrix `calibrate` prints tells you which case you have.

**Whether a model needs calibration is decided by numbers, not by its label.** On the two yes/no sets, OpenJev's raw probabilities were good enough twice and `identity` was written; Kev-4B gained a little from Platt; the ordinary LLMs gained two to four times. All ten (model, set) pairs separated positives from negatives completely, so the difference is only how honest the probabilities are. `calibrate` runs the comparison under cross-validation for each *(question, model)* and writes what won.

**An ordinary GGUF lands next to a trained decision model.** A 27B ternary chat model, mainline llama.cpp 0.7.0, one adapter, no fork, no training, no Rust: same top-1 as the best Apache-licensed trained model, Japanese 1.000, 0.58 s a question with flash attention and a warmed prefix. Moving the state from before the options to right before the answer took it from 0.919 to 0.968 with the same weights, which is why the layout is part of the calibration key.

## FAQ

**Does shikii run models?** No. It sends System One requests to an endpoint you run or rent, and keeps everything that happens after the answer. `shikii-readout` is the one exception in spirit: it converts a chat model's logits into the same shape, but the inference is still llama.cpp's.

**Why does my decision never reach `act`?** Because the spec has no calibration block for `(this question hash, this model id)`. Collect labels (`label`, or `--labeled file`), run `calibrate --write`, and `status` shows `calibrated: identity|platt|vector`. A decision without verified precision at the act threshold is not allowed to act. That is the point.

**I changed one word in the question and the calibration disappeared.** Correct, and it costs you minutes, not labels. The hash changed, so constants fitted to the old wording are not trusted. But the truths people gave (this request *was* a change request, these two reports *were* the same bug) do not depend on the wording or the model. Run `calibrate --write` again: it replays every stored truth against the new wording and the current model, refits, and writes. No human in the loop. The text of every decision is kept in the local store for exactly this reason; `calibrate` tells you if any older truths have no text and cannot be replayed.

**Which model should I use?** It depends on licence, speed and language, and shikii does not decide for you. The table above and [`docs/model-comparison.md`](docs/model-comparison.md) are the argument: with the same spec and the same labels you can choose by numbers, including whether a model needs calibration at all. `scripts/eval-model.sh <url> <name>` reproduces the table for a new endpoint.

**Does it work in Japanese?** Yes, and it is where the differences show. Kev-4B gets 0.913 on Japanese prompts; the 27B models get 1.000. On the two Japanese yes/no sets (duplicate bug reports, same-matter inquiries) every model we tried separated positives from negatives completely; calibration only sharpened the probabilities.

**Where does my data go?** Only the rendered state goes to the endpoint named in `send_to`, and only you decide where that may be: shikii does not guess what counts as personal information. Every spec must say `pii: true` (the text may contain personal information, so it is never sent to a hosted endpoint, whatever `send_to` says) or `pii: false`. There is no default. The text of every decision is kept in the local store on your machine, so people can answer on the page and truths can be replayed after a wording or model change. `--tainted` keeps everything local regardless. Keys are read from `DECISIONS_API_KEY` or `TYPESAFE_API_KEY` and nowhere else.

**The store keeps growing.** `shikii gc` removes records older than 30 days that never got a label. Labelled records and labels stay forever, as they are the calibration material. It is a dry run unless you pass `--apply`.

**Does it bias toward the first option?** Measure before you correct: `shikii bias <spec> --permutations 100` shuffles the options and reports flips and the position shift. Kev-4B: 0 flips in 100, shift ≤ 0.024. Ordinary LLMs read through logits may differ; the command exists for them.

## Documentation

- [`docs/decision-ops-framework.md`](docs/decision-ops-framework.md): design notes, what the framework owns and what it refuses to own, the verification plan and what was confirmed.
- [`docs/operations.md`](docs/operations.md): how to stop or roll back every moving part, how to read a failure, what is still missing.
- [`docs/model-comparison.md`](docs/model-comparison.md): the comparison table and how to reproduce it.
- [`docs/guide.ja.md`](docs/guide.ja.md): the long-form Japanese guide with every command and a full real-world loop.
- [`integrations/readout.md`](integrations/readout.md), [`integrations/bonjev.md`](integrations/bonjev.md), [`integrations/claude-code.md`](integrations/claude-code.md), [`integrations/mcp.md`](integrations/mcp.md): the connections we run.
- [`examples/`](examples/) and [`testdata/`](testdata/README.md): a spec with no calibration to start from, and the synthetic labelled sets used in tests.

The commands:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, hand-written blocks, precision@act below act → exit 1
shikii drift     <spec> --model M [--window N]            # recent labels vs the Brier and precision recorded at calibration
shikii bias      <spec> [--rows K] [--permutations N]     # does the answer depend on option order?
shikii status    [spec...] [--store DIR]                  # inventory: records, labels, sources, per-(model, hash) state
shikii gc        [spec...] [--older-than 30d] [--apply]   # drop old unlabelled records; dry run by default
shikii hash      <spec>
shikii serve     [--port 8095] [--specs DIR] [--store DIR] [--local URL] [--hosted URL]   # HTTP API + review page; auto-calibrates
shikii mcp       [--specs DIR] [--store DIR] [--local URL] [--hosted URL]   # the same as MCP tools over stdio
```

## Contributing

Issues and pull requests are welcome. One rule: a test that guards a behaviour must be shown failing first. Break the behaviour on purpose, run the test, record how it failed, then fix it and record that it passes. The docs list those mutations for every safety branch; keep the list honest.

## License

[Apache 2.0](LICENSE). Model licences are separate: Kev-4B and the Bonsai family are Apache 2.0, OpenJev is CC BY-NC 4.0, hosted Jev is TypeSafe's service. shikii stores your probabilities and labels, nothing of the models.

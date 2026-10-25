**English** · [日本語](readout.ja.md)

# Turning an ordinary GGUF into a decision model with upstream llama.cpp alone (`shikii-readout`)

A route for using a local GGUF in `shikii`'s decisions even without a trained decision model. No fork, no Rust and no training are needed.

```
shikii ──/v1/systemone──▶ shikii-readout ──/apply-template, /completion──▶ llama-server (upstream or a fork) + any GGUF
                                 assigns A, B, … to the options, runs a single token and reads the letters' probabilities
```

## Installing

```sh
# --alias is the source of the response's model (the adapter adds a layout name such as "+state-last") = the calibration key
# -fa on (flash attention), -ctk/-ctv q8_0 (8-bit KV) and -np 1 (do not split slots) are for speed. make server-gguf GGUF=… ALIAS=… does the same
llama-server -m <model>.gguf --port 8086 --host 127.0.0.1 -c 4096 -ngl 99 -fa on -ctk q8_0 -ctv q8_0 -np 1 --alias <name>
bin/shikii-readout --upstream http://127.0.0.1:8086 --port 8090                   # checks the labels with /tokenize at startup
bin/shikii decide .shikii/decisions/prompt-intent-v2.yaml --local http://127.0.0.1:8090 -i message='fix the typo in README'
scripts/eval-model.sh http://127.0.0.1:8090 <name>                                   # the same 5 sets as the other models
```

Stop it with `pkill -x shikii-readout` (llama-server separately).

## What is inside (`cmd/shikii-readout/main.go`, about 600 lines; depends only on the standard library and the order-keeping of `internal/systemone`)

| Stage | What it does | Checks against breaking |
|---|---|---|
| Reading the request | `questions` is an object of name → question. choice criteria are read **in their JSON order** (not put into a Go map). score is an array of levels; noul has no criteria or a true/false description | anything but 2 to 26 options, an unknown type or missing instructions gives 422 |
| Prompt | system + user. The question, a list of `A. id: description`, **at the end** `<state>…</state>`, and "Reply with one letter." Because the state comes last, every question of the same spec shares the same beginning and llama-server's `cache_prompt` works. `--state-first` gives the old layout. The layout name is added to the response's `model` as `+state-last` / `+state-first` (a different order changes the probabilities, so the calibration key changes too) | tests fix the presentation order, the layout and the suffix |
| Switching layouts (`layout`) | the request's `layout` (from the spec's `layout:`) changes the layout per request: `state-last` (default) / `state-first` / `plain`. plain has no system, the question, a list of `- id: description`, the state after `Input:`, and "Answer with the option id only.", and reads the probability of **each id's first token** (`with_pieces` of `/tokenize`) instead of letters. 422 if two ids share the same first token. For layouts other than the default, the response's `model` is `<base>+<layout>` (the calibration keys are separate). An unknown name gives 422 | tests fix plain's order, the reading of ids, the model name and 422 (mutation: always read letters → fails) |
| chat template | passes to upstream's `/apply-template` with `chat_template_kwargs: {enable_thinking: false}`. If rejected with 4xx, it removes it and resends, and from then on does not add it | a test checks whether it resends |
| Prefix warm-up | Bonsai's base, the Qwen3.5/3.8 family, has recurrent layers (Gated DeltaNet) and cannot rewind the KV part-way. Without a checkpoint exactly at the end of the prefix, `cache_prompt` does not work and it prefills everything every time (measured `cache_n=0`, 4 seconds per question). So only when the spec's prefix (up to just before `<state>`) changes, it runs it first with `n_predict: 0` to make a checkpoint. If the response's `cache_n` does not reach the prefix, it runs it again next time | tests for once with a new prefix, zero times with the same prefix, and again after a cache miss |
| Readout | runs `/completion` with `n_predict: 1`, temperature 1, no truncation and `n_probs`, picks the label letters' probabilities and normalizes them. Letters that did not make the top get a floor of 1e-6 | tests for the floor and a sum of 1 |
| Response | the same shape as llama.cpp's decision model (choice: probabilities/choice/confidence, score: "0".."n-1" + legend + expected value, noul: P(yes)) | a test per type |
| At startup | the model name from `/v1/models`, and with `/tokenize` whether all 26 letters are 1 token each | it does not start if a letter is 2 tokens |

It has no calibration. The probabilities it returns are the letters' softmax as is, and `shikii calibrate` fits them per (spec_hash, model).

The 9 tests were each seen going RED under 9 mutations (sorting criteria, noul as P(no), floor 0, no resend, changing the score legend,
reversing the layout, dropping the model suffix, not sending the warm-up, not warming up again on a cache miss) before going GREEN (2026-10-17).

### Speed (2026-10-17, the same 4 questions in a row. The first includes building shaders and the warm-up)

| Setup | 1st question | 2nd and later |
|---|---|---|
| state-first, no warm-up (the first version) | ≈ 5 seconds | ≈ 4 seconds (280-token prefill every time, `cache_n=0`) |
| state-last + warm-up, the fork's llama-server (Bonsai 2 PQ2_0), `-fa on -ctk/-ctv q8_0 -np 1` | 4.8 seconds | **0.83 seconds** |
| state-last + warm-up, **upstream** llama.cpp 0.7.0 (Ternary-Bonsai-27B Q2_g64), same flags | 4.0 seconds | **0.58 seconds** |

The probabilities are the same before and after the layout and warm-up (0.999 / 0.996 / 0.997 / 0.998). It only got faster; the answers did not change.
Upstream is faster because on this Mac upstream's Q2_0 prefills faster than the Prism fork's PQ2_0 Metal kernel (66 vs ≈ 130 tok/s).

## Measured (2026-10-17, same specs and same labels. The comparison table is `../docs/model-comparison.md`)

### Ternary-Bonsai-2-27B PQ2_0 (PrismML fork's llama-server, Metal)

| Set | Raw | After calibration, CV | Method chosen | BonJev (same model, other implementation) |
|---|---|---|---|---|
| prompt-intent-v2 / 62 | top-1 **0.968**, prec@act 0.983 / rec 0.983 | vector 0.967 / 0.967 | **identity** | 0.968, raw 1.000 / 0.900 → vector 0.982 / 0.949 |
| ja 23 | top-1 1.000, rec@act 0.957 | 1.000 / 0.955 | identity | 1.000 |
| en 39 | top-1 0.949 | 1.000 / 1.000 | identity | 0.949 |
| yes/no 32 pairs | the material of that time was replaced, so no numbers are given. After the replacement, see the table below | | | same |
| Speed | ≈ 5 seconds per question (156 questions in 13 minutes; the prefix cache only works up to system) | | | ≈ 2.5 seconds |

How to read it:

- **The ranking is the same as BonJev** (top-1 matches on all 3 prompt-intent sets). Even without prompt tricks or built-in calibration, the letter readout itself lands in the same place
- **The probabilities are sharper than BonJev's** (raw recall@act 0.983 vs 0.900). The letters' softmax leans to the ends, so it reaches the act threshold 0.75 raw.
  On the other hand, on yes/no the separation was so extreme that `calib.Fit`'s plain Newton method diverged (T=5e-7, in-sample Brier 0.107).
  CV caught it and fell back to identity, but this was an optimization bug, and it was fixed the same day with a line search (those 32 items are now a test)
- **Whether calibration is needed is decided not by the model but by "model + readout method".** The same Bonsai 2 has Platt winning through BonJev and identity through the adapter.
  This is where the design, whose key is (spec_hash, model) with model named differently per implementation, matters

### Ternary-Bonsai-27B Q2_g64 (**upstream** llama.cpp 0.7.0 build 11669, brew, Metal. No fork)

| Set | Raw | After calibration, CV | Method chosen |
|---|---|---|---|
| prompt-intent-v2 / 62 | top-1 0.919, prec@act 0.966 / rec 0.982 | vector 0.934 / 1.000 | vector (recall goes up) |
| ja 23 | top-1 0.870, prec@act 0.952 / rec 1.000 | 0.909 / 1.000 | identity |
| en 39 | top-1 0.949 | 1.000 / 0.947 | identity |
| yes/no 32 pairs | the material of that time was replaced, so no numbers are given. After the replacement, see the table below | | |
| Speed | ≈ 5 seconds per question (156 questions in 13.5 minutes, state-first layout, fa/KV defaults) | | |

How to read it:

- This table proves the route: **an ordinary GGUF became a decision model with upstream llama.cpp alone**. Accuracy is the same as Kev-4B (all 0.919),
  and Japanese at 0.870 is behind Bonsai 2's 1.000 (the difference between the bases Qwen3.6 and Qwen3.8)
- If a version of Bonsai 2 that upstream supports comes out, swapping to it should bring Japanese too

### After changing the layout to state-last + warm-up (2026-10-17, same sets, `-fa on -ctk/-ctv q8_0 -np 1`)

| Set | Bonsai 2 PQ2_0 (fork) | Ternary-Bonsai-27B Q2_g64 (**upstream 0.7.0**) |
|---|---|---|
| prompt-intent-v2 / 62 top-1 | 0.968 (same as state-first), raw prec@act **1.000 / rec 1.000** | **0.968** (state-first was 0.919), raw 1.000 / 0.983 |
| ja 23 | 1.000 | **1.000** (state-first was 0.870) |
| en 39 | 0.949 | 0.949 |
| same-issue / same-inquiry 32 pairs, Brier (raw → Platt CV) | 0.024 / 0.059 → **0.016 / 0.014** | 0.048 / 0.033 → **0.024 / 0.020**. Both separate positives and negatives completely |
| Method chosen | identity for all | identity for all |
| Total for 156 questions | **1 minute 53 seconds** (state-first took 13 minutes 0 seconds) | **2 minutes 13 seconds** (state-first took 13 minutes 35 seconds) |

How to read it:

- **Putting the state last raised the upstream 27B from Kev-4B's level (0.919) to Bonsai 2's level (0.968), and Japanese went from 0.870 to 1.000.**
  A layout change made for speed moved accuracy too. For an ordinary LLM, having the thing to read (the state) right before the answer works better
- With the same model and the same weights, the prompt layout moves top-1 by 5 points. That is why the layout name is added to the response's `model` to separate the calibration keys
- Better ranking is the kind of thing calibration cannot give (design §7.2). If you use an ordinary GGUF, measure the layout and wording before calibration

## Cautions

- Labels are a bare single letter (A to Z). On the premise that the chat template ends with a newline, the first token becomes the bare letter. Digits become 2 tokens in many tokenizers, so they are not used
- The chance that option order changes the probabilities (position bias) is higher than for a trained model. Measure with `shikii bias` before deciding on a correction
- When a label is not in the top `n_probs`, it gets the floor probability and this is logged. Raise `--n-probs` for specs with many options
- It is an ordinary LLM, so the ranking moves with how the question and option descriptions are written. Ranking errors are not fixed by calibration (design §7.2)

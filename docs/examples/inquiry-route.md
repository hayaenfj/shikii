**English** · [日本語](inquiry-route.ja.md)

# Someone else's example: routing inquiry emails (2026-10-18)

A record of someone using shikii for the first time (an agent that did not know this repo's contents) going one full round on a problem it chose itself, relying only on the [Japanese README](../readme/README_ja.md).
A test to see whether things work as documented in a field other than "classifying requests to an AI" such as prompt-intent. It never needed to read the source code.

## The problem

Route inquiry emails to 4 desks (billing / fault / cancel / other). The input is 22 items in Japanese (5 to 6 each), with no personal information.

- spec: [`inquiry-route/inquiry-route.yaml`](inquiry-route/inquiry-route.yaml) (made with `shikii new inquiry-route --type choice --question … --option billing=… ×4 --input body --pii true --send-to local`, with 3 wording candidates `variants:` added by hand. The calibration block was removed; the user draws the line with their own model)
- Past table: [`inquiry-route/past.csv`](inquiry-route/past.csv) (22 rows of `body,truth`. `shikii import past.csv --spec .shikii/decisions/inquiry-route.yaml --out truths.json` turns it into material for checking answers)

## Results of the round (Kev-4B Q4_K_M, local llama-server)

| Step | Result |
|---|---|
| `shikii init` → `shikii new` → `shikii decide` on 1 item | Passed. `band=ask top=billing p=0.915 calibrated=false` (no line yet, so check with a person) |
| `shikii serve` + `POST /v1/decide` on 22 items | top-1 was right on 20 of 22. Bands: 21 ask, 1 discard |
| `POST /v1/label` on 22 items | A wrong answer (`refund`) was rejected with the list of options |
| `shikii calibrate --write` | 22 items < `eval.min_n` 50, so nothing written → lowering the spec's `eval.min_n` to 20 let it write. The method chosen was identity (the raw probabilities can be used as is) |
| `decide` after calibration | `band=act`, with a receipt. serve used the new line without a restart |
| `shikii tune` (3 candidates) | None beat the current one: `winner: current` (a tie keeps the current one). A table and a one-line learning were left in `.shikii/wiki/runs/` |
| Answering the samples (1 in 20 of act) | Decisions with `audit: true` in `/v1/queue` could be answered with `/v1/label` |
| MCP (initialize / tools/list / tools/call to `shikii mcp` over stdio) | 5 tools; `shikii_decide` returned JSON. The 4 settings written by `init` were as documented |

Rating: 3 out of 5. "With only the README and help, I got through init → new → decide → label without getting lost. How to start with fewer than 50 items, the body of serve's API, and where to find how to write a spec were not in the README; I got past them by guessing from error messages and the skill."

## What this test fixed (same day)

- The README's `curl` examples lacked `-H 'Content-Type: application/json'`, so typing them as is got rejected → fixed the examples, and `scripts/check-readme.sh` checks that curl examples to /v1 have the header
- When truths were short of `eval.min_n`, the messages from `calibrate` and `tune` did not say "what to do" (`tune` rejected with the same message even when given `--labeled`) → they now say "collect truths, or for a trial, lower the spec's `eval.min_n`"
- `shikii init` made `wiki/` empty, so the links in `tune`'s run page (the type and model pages) were broken, and there was no ready-made knowledge for the proposer to read → `init` copies index.md, types/, patterns/ and models/ (existing pages are not touched). The run page links only pages that exist
- Only `shikii bias` lacked `--store` and required `--labeled` → it runs on the same savings as calibrate / tune (the bodies of truths in the store)
- `drift`'s ask agreement printed `0.000` even with 0 matching items → `n/a`
- `/v1/redo` without `spec` said `unknown spec ""` → it now says "spec is required: a file name in decisions/"
- `status`'s "human-checked so far" and the receipt's "human-checked act decisions" were different numbers with the same words → status says "audit answers so far". The meaning of the receipt's number is in [bands.md](../../skills/shikii/references/bands.md)
- Documents: where to find how to write a spec, the list of serve's HTTP API ([guide.md](../guide.md#the-serve-http-api)), what in `.shikii/` to commit, that `shikii new` works interactively or with flags, that command output is in English, and that ready-made knowledge is copied into `wiki/`

## To try it on your own problem

```sh
shikii init
cp <this repo>/docs/examples/inquiry-route/inquiry-route.yaml .shikii/decisions/   # or write your own problem with shikii new <id>
shikii import <this repo>/docs/examples/inquiry-route/past.csv --spec .shikii/decisions/inquiry-route.yaml --out truths.json
shikii calibrate .shikii/decisions/inquiry-route.yaml --labeled truths.json --local http://127.0.0.1:8080 --write
shikii decide .shikii/decisions/inquiry-route.yaml --local http://127.0.0.1:8080 -i body='先月の請求額が二重に引き落とされているようです'
```

22 items is a trial number. For business use, put `eval.min_n` back to 50 or more, keep checking answers on the `shikii serve` page, and draw the line again.

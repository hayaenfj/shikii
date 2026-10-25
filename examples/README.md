# examples

Starter spec files with no calibration block. They carry no numbers (nothing has been measured). The ones in `decisions/` are in real use, and some include calibration.

| File | Type | Use | What to look at |
|---|---|---|---|
| `prompt-intent.yaml` | choice | The kind of request to an AI (5 choices) | Used by `scripts/demo.sh`. What `decisions/prompt-intent-v2.yaml` looked like before it was measured |
| `inquiry-kind.yaml` | choice | The kind of inquiry (business) | `pii: true` and `send_to: [local]`. The text is never sent to an outside AI (it stays in the local records, and staff can answer on the page) |
| `clause-conflict.yaml` | noul | Conflicting contract clauses (legal) | act means "put it in the queue for human review", not "fix it automatically" |
| `drone-landing-abort.yaml` | noul | Deciding to abort a landing (robots and drones) | What act / ask / discard / abstain each mean, one comment line each |

Each runs with `shikii decide examples/<file>.yaml --local <url> -i key=value …`. With no calibration, the answer stops at `ask` however high it is.
Only after you collect truths and run `shikii calibrate --write` does `act` appear.

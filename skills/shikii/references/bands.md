# Bands and receipts

Read the answer of `shikii decide` by its band. Do not decide on the raw probability.

| band | meaning | what you do |
|---|---|---|
| `act` | there is a line, and the calibrated probability is at or above the act threshold | you may go ahead automatically. The JSON `receipt` carries the grounds (the line's method, the date it was fitted, the number of truths and how many came from people, and the number and precision of act decisions a person checked). Show it when asked to explain. The receipt's `human_checked_act` is "the number of human truths that this line would put in act", which is different from the number of sampled items answered (`status`'s audit answers) |
| `ask` | unsure (when there is no line, everything whose raw probability is at or above the ask threshold lands here; anything below is discard) | check with the person. Record the person's answer with `shikii label <trace_id> <truth>` (it becomes material for the line) |
| `discard` | for yes/no, "no". For choice and score, "reaches none of them" | do not act. A choice discard means it is unsure, so if there is context (`state.context`), shikii has already asked again by itself |
| `abstain` | inputs are missing, or there is nowhere it may send them | fill in the inputs. `reason` has the reason |

- `rung: context` marks "unsure on the minimal question, so it asked again with context added". `minimal` keeps the first answer
- `is_calibrated: false` means there is no line. act does not appear. Collect truths first
- Sampling: 1 in 20 decisions that went ahead automatically on act appears on the page. When a person answers it, act's precision is measured against human truths. When prompted, pass it to the person

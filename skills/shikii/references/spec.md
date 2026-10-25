# How to write a spec

1 decision = 1 YAML (`.shikii/decisions/<id>.yaml`). The usual way to make one is `shikii new <id> --type choice --question "…" --option id=description … --input name --pii true|false --send-to local` (`--send-to` is where it may send. Without it you are asked interactively, so an agent typing the command always passes it). Ready-made decisions are in `decisions/README.md`.

- Type: `noul` (yes/no), `choice` (several options, each with an id and a description), `score` (levels, with levels in order from low to high)
- `state.template`: how the inputs are laid out. `{{ name }}` is an input. Required inputs go in `required`
- `state.context` (optional): a layout added only when unsure. It uses all of required and adds one or more inputs not in template (e.g. the previous message `{{ previous }}`). With context it has a separate hash and a separate line
- `policy.send_to`: `local` / `hosted`. With `pii: true` the text never leaves the machine. Always write `pii` (there is no default)
- `thresholds.act` / `ask`: the band boundaries. For `score`, `level` lets the band be decided on the probability of "k or higher"
- `eval.labeled`: the file of seed truths. `eval.min_n`: the number of human truths needed to write a line (50 as a guide)
- `variants:`: candidate ways of asking. `question` / `options` (id → reworded description) / `template` / `layout` (state-last | state-first | plain) / `steps` (turn a choice into a series of yes/no). tune measures them. You may write candidates, but a person adopts them
- `display.<lang>`: translations for the page (title, question, options). Not sent to the model
- Do not write `calibration:`. calibrate writes it

The hash is determined by the question, options, template, decomposition and layout. Change one word and the line disappears and it goes back to `ask`. That is correct.

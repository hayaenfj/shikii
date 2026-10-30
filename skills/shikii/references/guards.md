# Turn a correction into a guard

A guard is a deterministic check that runs at the end of every turn (the `Stop` hook) and **stops the turn** when it fails: the agent receives the reason and must fix the work before ending. Lessons the agent could already read in the repository changed nothing in the 2026-10-23 experiment (4 of 5 tasks tied); delivered lessons it could not know were followed 6 of 6, and guards fixed 6 of 6 turns, whatever the model knows. Guards live in `.shikii/guards/<id>.yaml` of the project. There are four kinds and no other options:

| kind | checks | fields |
|---|---|---|
| `paired-files` | when a file matching one pattern changed in the turn, a file matching the other must have changed too | `pairs: [[CHANGELOG.md, CHANGELOG_ja.md]]` (globs allowed, `dir/**` for a subtree) |
| `forbidden-words` | lines added in the turn must not contain the words | `words: ["root", "fingerprint"]`, optional `except: ["docs/operations.md", "wiki/**"]` (paths not checked) and `allow: ["grassroots", "uproot"]` (ordinary words that contain a forbidden word; removed before checking. Not a forbidden word itself, nor a forbidden word with only kana or punctuation added) |
| `commit-body` | every commit made in the turn (from the HEAD at the request) must contain all words in its body | `required: ["Problem", "Change", "Verification"]` |
| `forbidden-commands` | a command about to run (Claude Code's `PreToolUse` for Bash) must match none of the patterns; it is refused with the reason before it runs. Commands are not observed at `Stop` | `patterns: ["\\|\\s*tail\\b[^\\n]*\\bgit\\s+commit\\b"]` (regular expressions) and `example: "make test \| tail -3; git commit -m x"` (a command the check must stop; `enable` shows it stopping). No pattern may match the harmless command `true` |

A `forbidden-commands` guard runs right before you run a command in Claude Code (`PreToolUse` of Bash): a command matching one of its patterns is refused with the reason; run a corrected command at once. The other three kinds run at `Stop` only. Unknown observations pass: without a git listing for the turn, or without a recorded HEAD, the corresponding kind does not stop anything. For these user-defined guards, after a stop the next `Stop` (with `stop_hook_active`) does not block again; shikii records whether the guard now passes. Built-in ritual checks separately require saved results and may continue on progress or end explicitly incomplete. `shikii status` shows how many turns were stopped and how many were fixed before the turn ended.

## When to draft one

When the person corrects you with a rule that can be checked mechanically ("keep both changelogs in step", "never use that word", "write the background and the verification in the commit body", "never run that"), draft a guard **in addition to** the lesson and the `correction` in reflection. Your own repeated mistake counts too (0.11): when you notice you made the same mechanical mistake twice in this project (a check piped into `tail` before `git commit`, a word you keep writing), draft a `forbidden-commands` or `forbidden-words` guard with `reason` saying it is your own mistake and when, tell the person, and let them enable it. Do not draft guards for judgements that need reading (scope, tone, design); those stay lessons.

```sh
cat > /tmp/guard.json <<'EOF'
{"id":"changelog-pair","kind":"paired-files","title":"Keep the English and Japanese changelogs in step","reason":"the owner's correction 2026-10-23","source":"<source id of the correction>","pairs":[["CHANGELOG.md","CHANGELOG_ja.md"]]}
EOF
shikii guard add < /tmp/guard.json      # writes .shikii/guards/changelog-pair.yaml, disabled
```

`add` always writes the guard **disabled**. Tell the person what you drafted and ask whether to enable it. Enabling is the person's decision (or yours only when the person tells you to):

```sh
shikii guard enable changelog-pair      # RED: the kind's negative example must stop; GREEN: the positive example must pass; then enabled: true
shikii guard disable changelog-pair     # keeps the file, stops checking
shikii guard list                       # state and the last result of each guard
shikii guard try                        # apply the enabled guards to the working tree now (diff against HEAD; commits are not checked)
```

`enable` refuses a guard whose negative example does not fail. Never edit `enabled:` by hand to skip that check, and never disable a guard to get past it; if a guard is wrong, say so and let the person decide.

## When a guard stops your turn

Read the reason, fix the work (add the missing file, remove the word, amend the commit body), and end the turn again. Do not argue with the guard in the reply and do not disable it. If you believe the guard is mistaken, finish what you can and explain in one sentence which guard and why.

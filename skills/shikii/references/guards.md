# Turn a correction into a guard

A guard is a deterministic check that runs at the end of every turn (the `Stop` hook) and **stops the turn** when it fails: the agent receives the reason and must fix the work before ending. Lessons the agent could already read in the repository changed nothing in the 2026-10-23 experiment (4 of 5 tasks tied); delivered lessons it could not know were followed 6 of 6, and guards fixed 6 of 6 turns, whatever the model knows. Guards live in `.shikii/guards/<id>.yaml` of the project. There are four kinds and no other options:

| kind | checks | fields |
|---|---|---|
| `paired-files` | when a file matching one pattern changed in the turn, a file matching the other must have changed too | `pairs: [[CHANGELOG.md, CHANGELOG_ja.md]]` (globs allowed: `*` stays inside one folder; a `**` segment stands for any number of folders, none included, so `docs/**/*.md` is every `.md` under `docs` at any depth and `**/*.md` every `.md` including the top level; `dir/**` is everything below `dir`; a pattern without `/` also matches the file name alone) |
| `forbidden-words` | lines added in the turn must not contain the words (a file that was uncommitted or untracked at the request is compared with the request's HEAD commit, so the person's own uncommitted lines in a file the turn changes count as added) | `words: ["root", "fingerprint"]`, optional `except: ["docs/operations.md", "wiki/**"]` (paths not checked; the same globs) and `allow: ["grassroots", "uproot"]` (ordinary words that contain a forbidden word; removed before checking. Not a forbidden word itself, nor a forbidden word with only kana or punctuation added) |
| `commit-body` | every commit made in the turn (from the HEAD at the request) must contain all words in its body | `required: ["Problem", "Change", "Verification"]` |
| `forbidden-commands` | a command about to run (Claude Code's `PreToolUse` for Bash) must match none of the patterns; it is refused with the reason before it runs. Commands are not observed at `Stop` | `patterns: ["\\|\\s*tail\\b[^\\n]*\\bgit\\s+commit\\b"]` (regular expressions) and `example: "make test \| tail -3; git commit -m x"` (a command the check must stop; `enable` shows it stopping). No pattern may match the harmless command `true` |

A `forbidden-commands` guard runs right before you run a command in Claude Code (`PreToolUse` of Bash): a command matching one of its patterns is refused with the reason; run a corrected command at once. The other three kinds run at `Stop`. From 0.27.0 they also run before supported `git push` and mutating `gh release` calls observed by that same Claude Code Bash hook, including ordinary RTK wrappers. Run publication as a separate tool call so the checks see the final files and commits; a compound publication is refused when these guards are enabled. Dry-run/help and standalone read-only commands are excluded from this additional check. Codex retains its turn-end checks without this command hook. OpenCode uses MCP and skills without automatic hooks. The bounded parser does not cover opaque shell expansion, heredoc tails or other publication routes. A turn started by a slash command (`/commit` and the like) is not kept as a request, but guards still apply to it: shikii keeps the working tree listing and HEAD at the slash command, and that turn's `Stop` applies the guards to what changed and what was committed since then. Nothing else of a request is added to such a turn (no questions, delivered lessons or ritual checks). Unknown observations pass: without a git listing for the turn, or without a recorded HEAD, the corresponding kind does not stop anything. One unknown is recorded as such: when the turn rewrites the commit that was HEAD at the request (`--amend`, a rebase) or moves to another line of history, `commit-body` cannot tell which commits are the turn's, so it stops nothing and records the check as unverified, not as passed (`shikii guard list` shows `last: not checked`; `shikii status` counts neither a stop nor a fix). Each of these user-defined guards stops a turn at most once: after a guard's own stop, a later `Stop` of the same turn does not block on that guard again; shikii records whether it now passes. A guard that has not stopped the turn yet still stops it once, also at a `Stop` with `stop_hook_active` after a built-in ritual check or another guard stopped the turn first, so the stops of one turn are capped by the number of guards. Built-in ritual checks separately require saved results and may continue on progress or end explicitly incomplete; at a `Stop` where they end the turn incomplete, guards stop nothing. `shikii status` shows how many turns were stopped and how many were fixed before the turn ended.

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

**English** · [日本語](mcp.ja.md)

# Connection example: MCP (used as a tool from agents such as Claude Code)

`shikii mcp` exposes the same content as `shikii serve` as a stdio MCP server. It has no inference of its own and asks the decision model behind it (`--local` / `--hosted`).
It only calls the HTTP handlers internally, so it behaves the same as `serve` (records, truths, automatic calibration and the store are shared too).

## Installing (Claude Code)

`shikii --init` (same as `shikii init`) adds the ready-made questions (the hooks' 4 and the wiki's 2), the wiki, the entry to the page, and the same entry to Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`mcp` in `opencode.json`, `type: local`) and Codex (`[mcp_servers.shikii]` in `~/.codex/config.toml`) (it leaves them alone if they already exist). Config files for tools you do not use are created too (delete them or add them to `.gitignore` if you do not need them; a second init leaves existing ones alone). People who use `opencode.jsonc` get a separate `opencode.json` (OpenCode reads both). If the Codex config has `mcp_servers` written in any form, it does not add one (appending at the end would be a duplicate TOML definition; it prints the lines to add by hand). In a project with only `CLAUDE.md`, it does not create `AGENTS.md` (OpenCode would stop reading `CLAUDE.md`). To write it by hand, use the project's `.mcp.json`:

```json
{
  "mcpServers": {
    "shikii": {
      "command": "shikii",
      "args": ["mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"]
    }
  }
}
```

`make install` puts `shikii` in `~/.local/bin/shikii` (use the absolute path if it is not on PATH). Logs go to stderr (stdout is the channel).

## Tools

| Tool | What it does | Arguments |
|---|---|---|
| `shikii_specs` | the list of specs. id, type, question, thresholds, accepted truths (option id / yes, no / level number). **Call it first** | none |
| `shikii_decide` | asks the decision model a spec's question. band (act / ask / discard / abstain), the top answer, the probability, trace_id | `spec`, `inputs` (object), `tainted` (optional) |
| `shikii_label` | puts a truth on a decision. `!x` (it was not x) is allowed too. When truths reach `eval.min_n` it calibrates automatically, and the result carries `calibrated: true` and a report | `trace_id`, `truth`, `spec` / `weight` / `source` (optional) |
| `shikii_status` | the state of records, truths and (model, hash) per spec | none |
| `shikii_tune` | scores a spec's candidate ways of asking (`variants:` + N made by the proposer) on the stored truths. Numbers per candidate, holdout, whether it can be adopted | `spec`, `propose` (optional. Only when `mcp` was started with `--proposer URL`) |
| `shikii_wiki` | grows sourced lessons from the conversations of the same work. Ingest, search, check | `operation` (`query` / `ingest` / `lint`), `sources` / `title` / `lesson` / `applies_to` / `id` / `replaces` / `correction` / `query` |
| `shikii_review` | provisional scoring with grounds from the conversation and outcome. Refers to the person's samples and correction examples | `operation` (`query` / `submit`), `source_id` / `trace_id` / `truth` (`yes` / `no` / `unknown`) / `reason` |
| `shikii_reflect` | checks and saves provisional scores, wiki summaries and observations of memories and corrections together (query gives 2 at a time from the oldest conversation; submit has no count limit) | `operation` (`query` / `submit`), `reviews` / `wiki` / `memory` |

To use `propose > 0`, start `shikii mcp` with `--proposer URL` (not in the `.mcp.json` example).

`shikii_wiki` was added in 0.3. The current agent writes the text; no other model is used. It is stored in each work's `.shikii/`, the same as the hooks, and only handles the project where the MCP server was started. `query` returns related lessons and unsorted conversations, and passing `sources: [source ID]` lets you read the original text. `ingest` saves the title, lesson, where it applies and the sources, and a conversation with no lesson can be marked sorted with `sources` only. Lessons are hypotheses and are not counted as truths or calibration. [Details of the rules](../skills/shikii/references/wiki.md).

`shikii_review` first checks the same work's original text, scoring targets and the person's correction examples with `query`, then saves the provisional scores with `submit`. It does not register human truths, and does not add checks with the person for every item. shikii chooses the samples, and the person's corrections from the page come back in the next `query`. [Details of scoring](../skills/shikii/references/review.md).

The 0.4 hooks point to `shikii_reflect`. `query` does not repeat the original text and returns the scoring targets, related lessons and the person's correction examples together. `submit` takes up to 4 provisional scores, 2 wiki summaries and 4 provisional memory evaluations. It links a memory's `result` with the decision and check results, and for quotes with a correction also saves `correction_result` (followed / repeated / not_applicable / unknown). The wiki's `correction` holds the original text of the person's reply and the next action to change. Correction tracking from 0.4.1 is available in 0.4.1. Check `complete` in the reply and each item's `error`, fix the failed ones and send them again. The ones that succeeded are kept, and sending the same content again does not duplicate. [JSON and steps](../skills/shikii/references/review.md).

**There is no tool for adopting.** Whether the meaning is the same is checked by the agent the hook asks, with `shikii hook variants check` (with translations when the setting is not English) (if the same, it is adopted and the person can revert it on the page). Otherwise, when a person presses "the meaning is the same" on the page (`shikii serve`), or with `tune --write` in the terminal.

Errors (an unknown spec, a truth not in the spec, a missing trace) are returned not as JSON-RPC errors but as `isError: true` and text in the tool result, so the agent can read them and fix things.

## How an agent uses it (example)

1. Learn the spec ids and the candidate truths with `shikii_specs`
2. At a decision point, call `shikii_decide` and look at `band`. On `act` go ahead, on `ask` ask the person, on `discard` do nothing
3. When the outcome is known (whether it actually changed something, whether the user chose), call `shikii_label` with the `trace_id`
4. The line is drawn automatically once enough accumulate. You can see `shikii_status`'s `keys[].state` go from `collecting` (with `n` / `min_n`) to `calibrated`

## Tests

The 3 tests in `cmd/shikii/mcp_test.go` (initialize and tools/list; one round of decide → label → automatic calibration → act; errors as isError, and unknown tools and methods as JSON-RPC errors).
Each was seen failing before implementation, then GREEN (2026-10-17).

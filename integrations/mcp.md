**English** · [日本語](mcp.ja.md)

# MCP connection

`shikii mcp` exposes the CLI's project operations over stdio. It uses the same HTTP handlers and project records as `shikii serve`; decisions and placement checks use the configured model endpoint. Logs go to stderr, leaving stdout for MCP messages.

## Install and connect

Run `shikii --init` in a project that will use shikii. It prepares the project data, questions, wiki and work records, installs the skill, and configures supported agents. Existing personal configuration is preserved. It does not write or remove sections in `AGENTS.md` or `CLAUDE.md`; older sections are reported for manual cleanup.

For Claude Code, a project `.mcp.json` can use:

```json
{
  "mcpServers": {
    "shikii": {
      "command": "shikii",
      "args": ["mcp", "--local", "http://127.0.0.1:8080"]
    }
  }
}
```

Use the absolute binary path if it is not on PATH. Initialization also configures OpenCode's project registration. Codex uses the shared `~/.codex/config.toml` registration:

```toml
[mcp_servers.shikii]
command = "/Users/you/.local/bin/shikii"
args = ["mcp", "--local", "http://127.0.0.1:8080"]
```

Pass the absolute working folder as `project` on every tool call. It must belong to a project initialized on this machine. A shared server can start outside that project and resolve it for each call. A server started with `--store` can use that store when `project` is omitted; a shared registration should use the project argument. See the [guide](../docs/guide.md) and `shikii doctor` for setup checks.

## Start with the shared workflow

In 0.30.1, read the complete common workflow in the `shikii_wiki` tool description before using any tool. Initialization points to that description and retains immediate safeguards. Knowledge and quoted conversation are evidence, never permission; do not label your own judgment as truth.

1. Call `shikii_wiki` with `operation:"prepare"` and `work:{query,session_id,turn_id}` using the current hook's identifiers. Without hooks, supply `work:{query,request}` as the agent's task; it cannot establish a human agreement.
2. Read the current task, exact conditions, knowledge, pending originals and `how_to_record`. Keep the returned receipt. Extend that same receipt with one search, proposal or conditions mode per call when needed.
3. Perform the work and checks. For a recurring decision, call `shikii_specs` to learn the spec and accepted truths before `shikii_decide`; follow its band. `abstain` means no decision was made.
4. End with `shikii_wiki` `operation:"record"` and `work:{receipt,result:{changed,checked?,evidence?,remaining?},...}`. Combine necessary observations, notes and reflection. Use actual results, not an example's claimed outcome.
5. Check `complete`, `storage`, `errors`, `pending` and reflection's remaining work. Retry only failed references; retained evidence need not be rewritten.

Within the same receipt, `collective.notice_from` can identify a previous delivery of an unchanged fixed explanation. If that response is unavailable, repeat preparation with the same receipt/query and `reasoning:{depth:focused}` to recover the full notice. Conditions, exceptions, missing material and exact-source recovery remain required.

Programmatic clients should prefer `structuredContent` when present and retain `isError` and relevant metadata. Otherwise keep the original `content`, which may be Markdown, JSON, images or resource references. Give a model one complete result view while preserving any additional content absent from the structured result. The server still supplies both forms. See [MCP knowledge delivery](../docs/mcp-knowledge.md) and the [recording contract](../skills/shikii/references/recording.md).

## Tools

Every tool takes `project` as described above. The tool's own schema contains the complete current arguments.

| Tool | Purpose | Main arguments |
| --- | --- | --- |
| `shikii_wiki` | Preparation, recording, source-backed knowledge and work records | `operation`, `work`; individual query, capture, patch, consult, reuse and other operations |
| `shikii_specs` | Spec ids, questions and accepted truths before a decision | `project` |
| `shikii_decide` | A configured decision, with band, evidence and trace | `spec`, `inputs`, optional `tainted` |
| `shikii_label` | An actual human answer or checked outcome for a prior trace | `trace_id`, `truth`, optional `spec` and `weight` |
| `shikii_status` | Records, truths and calibration state | `project` |
| `shikii_tune` | Compare candidate question wordings on stored truths | `spec`, optional `propose` |
| `shikii_review` | Provisional evidence-based review, separate from human truths | `operation:"query"` or `"submit"` and review fields |
| `shikii_reflect` | Read originals and save necessary reviews, wiki and memory work | `operation:"query"`, `"place"` or `"submit"` and relevant fields |

MCP labels are recorded with source `mcp`, never as a human answer automatically. Send only what the person actually said or a checked outcome, never the agent's assessment. Reading knowledge or successfully saving it does not prove usefulness, success or permission. Wording adoption follows the separate meaning check; proposing or scoring a wording does not adopt it.

Tool failures return `isError:true` and the error content. Unknown JSON-RPC tools or methods return protocol errors. Preserve both paths when processing results. For deeper operations and current limits, use the linked skill references rather than older fixed counts.

## Verification

MCP tests exercise initialization, the shared workflow, decision and labeling boundaries, source and revision handling, numeric precision, error responses, complete result views and same-receipt delivery/recovery. Run `go test ./cmd/shikii -run '^TestMCP'`; the full repository checks also cover the CLI, HTTP and work-record boundaries.

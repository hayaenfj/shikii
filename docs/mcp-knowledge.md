**English** · [日本語](mcp-knowledge.ja.md)

# MCP knowledge delivery

The Markdown text view is available from 0.28.1. Restart MCP after updating.

Successful `shikii_wiki` and `shikii_reflect` calls can return a Markdown text view alongside the complete JSON result in `structuredContent`. Programmatic clients should read `structuredContent`; `content[].text` may be Markdown or compact JSON.

Read the complete shared workflow in the `shikii_wiki` tool description before using any shikii tool. Initialization instructions retain the entry point and immediate safeguards. Clients that append server instructions to every tool can reuse this one complete workflow instead of receiving its full text on each tool.

When forwarding a tool result to a model, provide one complete view and retain `isError` and relevant metadata. Prefer `structuredContent` for programmatic processing. If it is absent, retain the original `content` without assuming its text is JSON. Preserve any additional images or resource references that the structured result does not contain. The server continues to provide both representations for compatible clients.

Repeated broad preparations under the same receipt may reuse an unchanged `collective.notice` from the earlier delivery named by `notice_from`. Only the fixed explanation is shared; updates, contrasts, investigations, missing-material warnings and recovery references remain. If the earlier response is unavailable, repeat preparation with the same receipt/query and `reasoning:{depth:focused}` for full guidance. Explicit depths, exact-revision recovery, changed notices and new receipts keep the full notice. Reuse requires an actual reduction in complete transport size.

The text view keeps the original values. It does not summarize lessons, shorten quotations, remove conditions or change source and revision references. Long strings that occur more than once share a verbatim text section. A compact JSON metadata block retains other values. Each text section lists its original field locations as JSON Pointers and its UTF-8 byte length. Null array positions listed by a text section are placeholders for that section's string. Original field meanings, uncertainty and trust boundaries still apply.

Code fences are longer than any matching run in the original text. This keeps embedded Markdown and source quotations inside their own section. Numeric literals retain their precision across all successful MCP structured results, including integers beyond JavaScript's exact range. Other tools keep their existing JSON text format.

The renderer selects Markdown only when it saves at least 512 UTF-8 bytes against compact JSON. Otherwise it returns compact JSON. This byte rule is a presentation heuristic, not a token or accuracy guarantee. Unique text needs no shared section. Responses larger than 1 MiB keep compact JSON; bounded extraction also leaves deep or excess values in metadata. Error responses, other MCP tools, HTTP and CLI output, and stored records retain their existing formats. No new configuration is required.

Regression tests use the actual MCP exchange and independently reconstruct the complete JSON from the text view. They cover source identity, conditions, exact text, code fences, JSON Pointer escaping, arrays, nulls, empty values, numeric precision and extraction limits. Representative tokenizer measurements and conversion benchmarks complement these checks. Preserving every value does not establish unchanged model judgment accuracy; compare that separately on the models and tasks used for deployment.

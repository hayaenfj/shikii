**English** · [日本語](mcp-knowledge.ja.md)

# MCP knowledge delivery

Available in 0.28.1. Restart MCP after updating.

Successful `shikii_wiki` and `shikii_reflect` calls can return a Markdown text view alongside the complete JSON result in `structuredContent`. Programmatic clients should read `structuredContent`; `content[].text` may be Markdown or compact JSON.

The text view keeps the original values. It does not summarize lessons, shorten quotations, remove conditions or change source and revision references. Long strings that occur more than once share a verbatim text section. A compact JSON metadata block retains other values. Each text section lists its original field locations as JSON Pointers and its UTF-8 byte length. Null array positions listed by a text section are placeholders for that section's string. Original field meanings, uncertainty and trust boundaries still apply.

Code fences are longer than any matching run in the original text. This keeps embedded Markdown and source quotations inside their own section. Numeric literals retain their precision, including integers beyond JavaScript's exact range.

The renderer selects Markdown only when it saves at least 512 UTF-8 bytes against compact JSON. Otherwise it returns compact JSON. This byte rule is a presentation heuristic, not a token or accuracy guarantee. Unique text needs no shared section. Responses larger than 1 MiB keep compact JSON; bounded extraction also leaves deep or excess values in metadata. Error responses, other MCP tools, HTTP and CLI output, and stored records retain their existing formats. No new configuration is required.

Regression tests use the actual MCP exchange and independently reconstruct the complete JSON from the text view. They cover source identity, conditions, exact text, code fences, JSON Pointer escaping, arrays, nulls, empty values, numeric precision and extraction limits. Representative tokenizer measurements and conversion benchmarks complement these checks. Preserving every value does not establish unchanged model judgment accuracy; compare that separately on the models and tasks used for deployment.

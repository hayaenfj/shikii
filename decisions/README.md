# Ready-made decisions

In your own project, copy one into `.shikii/decisions/` and use it as is, or copy it with `shikii new --from <this repo>/decisions/<id>.yaml` and edit it (specs live in `.shikii/decisions/`, which `shikii init` creates). For those with a seed set of truths (`testdata/`), `shikii calibrate <spec> --labeled <seed> --local <URL>` draws a line on the spot. The wiki's detailed rules (`wiki/specs/<id>.md`) are the guide that people read when setting truths, and that the judge reads too.

| spec | What it decides | Type | Inputs | Seed truths | Rules |
|---|---|---|---|---|---|
| `prompt-intent-v2.yaml` | The kind of request to an AI (change / question / review / operation / not a request) | choice | `message`, and `previous` when unsure | 62 (39 English, 23 Japanese) `testdata/prompt-intent-labeled.json` | [wiki](../wiki/specs/prompt-intent.md) |
| `prompt-intent.yaml` | The v1 wording of the same decision (for comparison; use v2) | choice | `message`, `previous` | same as above | same as above |
| `prompt-kind.yaml` | Whether a request changes files (the 5 choices used by the Claude Code hook; the truth is set automatically from changes in the working tree) | choice | `request`, and `previous` when unsure | Collected in real use (`~/.shikii/agent/.shikii`, after `shikii hook install`) | [guide](../integrations/claude-code.md) |
| `same-issue.yaml` | Whether two bug reports are the same bug (duplicate detection) | noul | `report1`, `report2` | 32 pairs (Japanese; 8 borderline pairs with weight 0.5) `testdata/same-issue-labeled.json` | [wiki](../wiki/specs/same-issue.md) |
| `same-inquiry.yaml` | Whether two inquiries are the same case | noul | `inquiry1`, `inquiry2` | 32 pairs (Japanese) `testdata/same-inquiry-labeled.json` | (not yet) |
| `relatedness-level.yaml` | How strongly two reports are related (levels) | score | `report1`, `report2` | (not yet) | (not yet) |

For those with candidate wordings (`variants:`), `shikii tune <spec> --labeled <seed> --local <URL>` measures the candidates. A winning candidate becomes a draft claim in the wiki.

# Work records (`.shikii/handoff/`)

Each project has a folder for working files that change as the work goes: audit reports, release steps and their log, diffs, screenshots, long investigation notes. `shikii --init` creates it next to `decisions/`, `wiki/`, `records/` and `labels/`. It is not committed, `shikii gc` never touches it, and `shikii update` keeps it.

## Which place for what

| What | Where | How |
|---|---|---|
| A rule or judgement that should reach the next similar request | wiki lesson | `reflect place` then `reflect submit` (or `wiki ingest`) |
| Evidence as it was when you checked it (a test result, a release digest, a quoted report) | wiki source | `wiki capture` (immutable, at most 32 KiB) |
| A file you will keep editing, or anything long, binary or produced by another agent | work record | `shikii handoff write/append`, `shikii_wiki` with `handoff_*`, or write the file directly under `.shikii/handoff/` |

A lesson can cite a work record: capture an observation whose `reference` is the path (`.shikii/handoff/audit-0.8.1/report-B.md`) and whose `content` quotes the part that supports the lesson. Do not paste a whole report into a lesson body.

## Using it

- CLI: `shikii handoff list`, `shikii handoff read <path>`, `shikii handoff write <path>` and `shikii handoff append <path>` (body on stdin). Paths are relative to `.shikii/handoff/`; `..`, absolute paths and hidden names are refused.
- MCP: `shikii_wiki` with `operation` `handoff_list`, `handoff_read` (`path`), `handoff_write` or `handoff_append` (`path`, `content`). Text only, UTF-8, at most 1 MiB per call; reading returns text up to 4 MiB. For images and other binaries, write the file directly into the folder.
- Group files by task in a first-level folder (`audit-<version>/`, `release-<version>/`, `investigation-<topic>/`). The page groups by that folder.
- The person reads them on the answer page under Knowledge → Work records (read only; text stays text, images are shown, HTML is never rendered).

## Subagents and auditors

Tell a subagent where to write: give it an exact path under `.shikii/handoff/<task>/` for its report. Keep the repository copy it works on elsewhere (a `git archive` copy in a scratch folder), and forbid `git checkout`, `git restore`, `git stash` and edits in the real repository. After it reports, read the file, fix what it found, and capture the parts that support a lesson.

## Boundaries

- Work records are not truth labels and not permission. A report written by an agent stays an agent's observation.
- Keep secrets out. The folder is local, but it is still read by every agent working on the project.
- Do not delete another task's records to make room. If a record is obsolete, say so at its top or move it under an `old/` folder.

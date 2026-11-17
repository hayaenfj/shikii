# Work records (`.shikii/handoff/`)

Each project has a folder for working files that change as the work goes: audit reports, release steps and their log, diffs, screenshots, long investigation notes. `shikii --init` creates it next to `decisions/`, `wiki/`, `records/` and `labels/`. Like the rest of `.shikii/` it stays out of Git (the folder is in Git's local exclude; add a file with `git add -f` only if you mean to share it). `shikii gc` never touches it and `shikii update` keeps it, except for the one-time wipe when updating from 0.6.0 or earlier.

## Which place for what

| What | Where | How |
|---|---|---|
| A rule or judgement that should reach the next similar request | wiki lesson | `reflect place` then `reflect submit` (or `wiki ingest`) |
| Evidence as it was when you checked it (a test result, a release digest, a quoted report) | wiki source | `wiki capture` (immutable; no size limit of its own, up to the store's 4 MiB line limit) |
| A file you will keep editing, or anything long, binary or produced by another agent | work record | `shikii handoff write/append`, `shikii_wiki` with `handoff_*`, or write the file directly under `.shikii/handoff/` |

A lesson can cite a work record: capture an observation whose `reference` is the path (`.shikii/handoff/audit-0.8.1/report-B.md`) and whose `content` quotes the part that supports the lesson, then use that source in the lesson. `shikii handoff read` accepts that path as written. Do not paste a whole report into a lesson body.

## Using it

- CLI: `shikii handoff list`, `shikii handoff read <path>`, `shikii handoff write <path>` and `shikii handoff append <path>` (body on stdin). Paths are relative to `.shikii/handoff/` (a leading `.shikii/handoff/` is accepted, so the path in a source's `reference` works as is); `..`, absolute paths, hidden names, control characters and links (to files or folders, inside or outside, and hard links) are refused.
- MCP: `shikii_wiki` with `operation` `handoff_list`, `handoff_read` (`path`), `handoff_write` or `handoff_append` (`path`, `content`). Text only, UTF-8, at most 1 MiB per call; reading returns text up to 4 MiB. A text with nothing to read (`content` left out or empty, an empty standard input, only spaces and line breaks, or only a byte order mark) is refused, and the record stays as it was. For images and other binaries, write the file directly into the folder.
- Group files by task in a first-level folder (`audit-<version>/`, `release-<version>/`, `investigation-<topic>/`). The page groups by that folder.
- The person reads them on the answer page under Knowledge → Work records (read only; text stays text, images are shown, HTML is never rendered).

## Worktrees

Keep one wiki and one shared work-record location in the original project. A dedicated worktree is a code checkout, not another shikii project: do not run `shikii --init` there or create a separate wiki. Use the original project's absolute path for MCP `project`, and run CLI `work prepare/record`, wiki and handoff commands from that project. Run builds and tests in the dedicated worktree; save reports, diffs, logs and screenshots under the original project's `.shikii/handoff/<task>/`. From 0.37.0 a worktree kept under the project folder (`.claude/worktrees/`, `.chiten/wt/` and the like) finds the project by itself: the hooks and the CLI work from inside it and use the project's `.shikii/`. A worktree created elsewhere is not reached, so run the CLI from the original project there.

Name both locations explicitly in assignments and reports. Include the dedicated worktree's absolute path, branch, checked commit and shared evidence path. If changes are still uncommitted, say so instead of presenting the base commit as the checked changes. Before committing or integrating, identify any pre-existing changes and preserve them.

## Subagents and auditors

Tell a subagent where to write: give it an exact path under `.shikii/handoff/<task>/` for its report. Keep the repository copy it works on elsewhere (a `git archive` copy in a scratch folder), and forbid `git checkout`, `git restore`, `git stash` and edits in the real repository. After it reports, read the file, fix what it found, and capture the parts that support a lesson.

An audit covers duplication as well as defects. Put it in the auditor's brief: look for code, tests, rules, wiki pages, documents and settings that repeat something the project already has (a second copy of an existing helper, the same rule written in two places, near-identical tests, a new page on a topic an existing page covers). Each duplicate is a finding even when the behavior is correct, because copies drift apart. The finding names both locations and says which one stays. Do not count a duplicate as fixed until one copy is gone or the two are made to share one source.

An audit goes by classes of problem as well as by the diff. Name the rule behind the change (where a cut may fall in a character, how quoted text is fenced, who reads a secret file, which names a child process must not inherit) and have the auditor list every place the rule applies with a scan of the code, a walk over every source file or its syntax tree, not the places the author remembers. Then test each place. A fix is not done with the places the finding names: after it, run the same scan again to count the places of the same class, and break each place by itself in a copy to count the places that no test guards. Guard a rule that must hold everywhere with one owner (one function or constant) and an allow-list check: the check names the places that may do it themselves, fails on any other place, fails when an allowed place stops needing it, and has a negative example. A check that lists the bad ways of writing it misses the next way. The audit of a fix looks at what the fix added, too: a second copy of a helper, a test helper that repeats an existing one. Put all of this in the auditor's brief, with the condition that every test added shows RED before GREEN.

## Boundaries

- Work records are not truth labels and not permission. A report written by an agent stays an agent's observation.
- Keep secrets out. The folder is local, but it is still read by every agent working on the project.
- Do not delete another task's records to make room. If a record is obsolete, say so at its top or move it under an `old/` folder.

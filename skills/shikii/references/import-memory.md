# Move an agent's memory into the wiki

Use this when the person asks to move, import or migrate an existing memory into shikii's wiki ("メモリを shikii の wiki へ移して", "import my memory into the wiki"). One instruction is enough; do not ask the person to sort the files first. Work on the current project only.

## 1. Find and read the memory

- Claude Code's auto memory: `~/.claude/projects/<project-slug>/memory/` (an index `MEMORY.md` with one line per fact, and one Markdown file per fact with frontmatter `name`, `description`, `metadata.type` = `user` | `feedback` | `project` | `reference`). The folder may be a link to a shared folder; follow it.
- Other memory the person names (Codex notes, a `memory/` folder in the home directory, a handoff folder). Do not go looking for memory in unrelated projects.
- Read `MEMORY.md` and every file completely before moving anything. Leave secrets and raw conversation transcripts where they are; a session id or a home path is fine.

## 2. Keep each file as a source

For every file, run `shikii wiki capture` (or `shikii_wiki` with `operation: "capture"`) with the file as the observation:

- `kind`: `human` when the file records the person's own words or decisions (`feedback`, `user`), `agent` otherwise (`project`, `reference`).
- `title`: the file's `name` or its first heading; `content`: the file body verbatim (at most 32 KiB; split a longer file by its `##` sections into several captures, in order); `reference`: the file path; `version`: the file's `modified` date or last-changed date; `observed_at`: now.

Record the returned source ids per file. A source is evidence, not a lesson: it never changes a claim state, never grants permission, and never becomes a truth label.

## 3. Draft lessons

- `feedback` and `user` files: one lesson per file. `title` = the rule in one line; `lesson` = the fact plus the "How to apply" in at most two sentences; `applies_to` = the situation in which it applies, taken from "How to apply"; `body` = the Why, the dates, examples and exceptions; `related` = the `[[links]]` resolved to page ids once those pages exist.
- `project` files: do not copy the chronology. Extract only the reusable rules (how releases are made, what was decided and why) as lessons; the rest stays reachable as the captured source (`shikii wiki query <source_id>`).
- `reference` files: one lesson whose `applies_to` says when the pointer is needed and whose body holds the URL, session id or path.
- Rules that must hold in every request regardless of topic (the language of all visible text, words never to use, when to push) still become lessons, and also stay in the index (step 5), because the wiki delivers lessons by similarity to the request and a request rarely mentions them.

## 4. Place, then ingest

Pass all drafts to `shikii reflect place` (`{"operation":"place","wiki":[...]}`) and read the candidates: when a same-topic candidate is likely, set `id` to that page (or `replaces`) so the memory merges into the existing article instead of duplicating it; otherwise make a new page. Then `shikii wiki ingest` each (or `reflect submit` with `wiki[]`). Where a feedback file names a check that can be done mechanically (two files that must change together, words never to use, words a commit body must contain), also draft a guard with `shikii guard add` as in [guards.md](guards.md), left disabled.

## 5. Turn the memory folder into a thin index

Do not delete the files; the person does that. Replace each moved file's body with one line that names the wiki page (`正本: .shikii/wiki <page_id>` / "canonical: .shikii/wiki <page_id>") and keep its frontmatter, and rewrite `MEMORY.md` so that each line points at the page id. Keep the always-on rules of step 3 as one-liners in `MEMORY.md`. Add one line at the top saying that the canonical memory is this project's `.shikii/wiki` and that new facts are written there with `shikii wiki ingest`.

## 6. Verify and report

`shikii wiki lint` reports no issues; `shikii wiki query <a term from each rule>` finds the page. Report the page ids per moved file, what was merged into an existing page, and what was left in place (secrets, transcripts) and why. Running the procedure again on the same folder merges through `place` and does not duplicate pages.

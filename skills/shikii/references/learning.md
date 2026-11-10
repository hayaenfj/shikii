# Grow experience into knowledge and practice

Use this when distinct experiences suggest a useful connection, a project
hypothesis can be checked during the authorized work, a method deserves reuse,
or an article has become difficult to maintain. The host agent supplies the
interpretation. Keep conditions, counterexamples and the original evidence;
another agent should be able to challenge and improve the same knowledge.

## Connect experiences and check a hypothesis

Read at least two distinct live articles and their sources. Explain what they
share, where their conditions differ, and why the connection matters for the
current work. Write that provisional insight in the article's lesson/body and
save `learning.connections` alongside it:

```json
{"connections":[
  {"id":"<article id>","revision":"<read revision>","quote":"Exact unique body quotation.","conditions":"When this experience applies","reason":"Why it informs the new insight","relation":"supports"},
  {"id":"<different article id>","revision":"<read revision>","quote":"Exact unique body quotation.","conditions":"Its different conditions","reason":"Why this limits the insight","relation":"counterexample"}
]}
```

The program validates the pinned quotations and carries over their original
source citations. Two articles do not establish independent corroboration:
they can share a source. Connections are agent interpretations, not established
facts. `learning_context.recheck` identifies connected articles whose revision,
availability or watched premises changed. Investigate their conditions before
using them. Inline contexts allow 2000 bytes of learning per article; when
`omitted:true`, follow `query` to the full exact revision. An explicit pinned
`lesson:<id>@<revision>` query added to the same work receipt returns the full
learning context within its 12 KiB storage limit, once rather than duplicated
inside the article.

For a useful unresolved hypothesis, add `learning.probe`:

```json
{"question":"Will the split retain the necessary exceptions?",
 "method":"Compare the same representative request before and after the split.",
 "reconsider_when":"A required exception is absent or loses its conditions.",
 "results":[{"conditions":"The actual environment and inputs checked",
   "conclusion":"What the observed evidence supports and leaves unresolved",
   "assessment":"inconclusive","sources":["<observation id>"]}]}
```

Choose a small observation within the current task, then capture what actually
happened. Results accept `supports`, `challenges` or `inconclusive`; none becomes
a truth or success label. Keep contrary evidence and limitations. Do not invent
an experiment result, start unrelated experiments or add a question ledger.

## Grow a reusable project practice

On a `howto`, `learning.practice.steps` stores an ordered method:

```json
{"steps":[{"condition":"Before a shared article edit",
 "action":"Read the current article revision",
 "avoid":"Overwriting another writer's work",
 "check":"The revision still matches immediately before saving",
 "sources":["<checked observation id>"]}]}
```

`reuse` returns the practice with its exact article revision when the learning
fits inline, as well as the usual source evidence and procedure outcomes. If
omitted, read the cited full article. Compare each condition with this task,
then use the applicable steps. This is task-specific skill content from the
project wiki; do not copy private project procedures into a global skill.
After execution, record an outcome with observed sources. Review failures and
corrections through the existing procedure review; patch the practice when
the evidence supports a change. A method written by an agent is not proof
that it succeeded.

## Save and revise learning

Direct `wiki ingest` and `wiki patch` accept `learning`; MCP and HTTP use the
same field. A patch requires the read article revision, sources and a reason.
Omission preserves learning; `{}` clears it. A supplied nonempty object replaces
the complete learning value: read it first and retain any connections, probe or
practice that you are not changing. `work record` notes accept it too:
on an existing target send `target`, `sources`, `change_reason`, and `learning`
with any necessary exact body edits. In work notes a connection's `id` may be
the prepared `knowledgeN` ref (its revision is supplied internally); nested
result/step `sources` use prepared observation refs or this call's `new1`.
Direct wiki operations use actual article revisions and source IDs.

Learning is at most 12 KiB: up to six connections citing at least two distinct
articles, one probe with up to four results, and one howto practice with one to
eight steps. Each result/step cites one to four sources included in the article.
Ordinary prose edits retain the metadata and its historical evidence. Update
all writers of a shared wiki before using these fields; older binaries do not
understand them. The knowledge page displays the structured learning beside
the article, while the wiki remains the source of truth for storage.

## Organize knowledge without losing its provenance

Use `wiki organize` (MCP `operation:"organize"`, HTTP `/v1/wiki/notebook`) with
`organization:{reason,sources,targets,moves}`. This previews a split or merge;
it writes no articles:

```json
{"organization":{
 "reason":"Separate earlier experience from the current procedure.",
 "sources":[{"id":"<source article>","revision":"<read revision>",
   "lesson":"Revised summary for what stays in the source."}],
 "targets":[{"title":"Earlier verification experience",
   "lesson":"Earlier verification under its original conditions.",
   "applies_to":"Comparing earlier verification","kind":"history",
   "scene":"Comparing earlier work","body":"Exact passage to retain."}],
 "moves":[{"from":"<source article>","quote":"Exact passage to retain.",
   "to":"Earlier verification experience"}]}}
```

Each move's quote occurs exactly once in its pinned source body and destination
body. Moves cannot overlap or cut an interpreted quotation. Original source
citations and moved roles/conditions/reasons follow the text. Unselected text
stays; original articles retain their history and point to destinations through
`related`, so inbound references remain navigable. Read the whole proposed
articles to check meaning and the revised retrieval summaries.

Publish `creates[].draft` with ingest, keeping `create_only:true`; compare each
saved revision with `creates[].page.revision`. Then apply `patches` in order,
retaining `requires`. The source patch checks the source and destination
revisions under the wiki lock. A changed or missing destination prevents text
removal. These are ordered individual saves, not one transaction: after a
failure inspect the saved results and replan without repeating completed
creates or overwriting a concurrent edit. No original is deleted first.

The fixed limits are four source articles, four new targets and eight moves.
Targets receive at least one move and sources contribute at least one. Targets
are new articles; replacement, correction and watched-file changes remain
separate operations. The CLI accepts at most 1 MiB of organization, ingest or patch JSON.

## Work together on the same knowledge

`prepare` (including receipt extensions), `consult` and `reuse` add a shared
`collective` context: at most two changes to supplied articles, one contrast and
one investigation, within 4 KiB of JSON. At most 32 candidate article identities
are checked; omitted counts and exact recovery queries stay explicit. When
`evidence` names existing learning in the reply, read that path rather than
fetching the same content again. Otherwise read the intact quotes and conditions,
or follow `query` when `omitted` is true.

Changes retain before/after revisions, changed fields, sources and a reason when
one was recorded. Inspect both revisions and the intermediate history when its
conditions matter. Keep an unacknowledged `changeN` until inspected, then record
`reviewed:["changeN"]` with the same receipt. `knowledgeN` confirms an inspected
initial article for the next request in that session. Preparing alone does not
acknowledge anything. A later peer edit still needs its own inspection. Actual
use belongs separately in `discoveries`; neither confirmation creates helpfulness
or a human truth. Confirmed references are scoped to the project/session and the
next request's selected articles; missing or expired baselines are first exposure.

Contrasts preserve explicit supporting/counterexample connections or differing
reported executions of one howto. Read both conditions, quotes and original
sources. `same_text`/`different_text` compares condition text, not meaning.
`not_compared` means one of the two reports cannot be read (its `unknown` names
the sources to recover): nothing of it is shown and no difference is established.
`shared_sources` identifies shared evidence; `not_established` never means
independent corroboration. Interpret common conditions, differences, unknowns and
what observation would distinguish the hypotheses. No majority vote selects a
winner, and differing result words alone establish no contradiction.

Investigations point to the same article's `learning.probe`. Its key includes the
article, question, method and reconsideration condition; adding a result keeps
that key. The same probe revision is delivered once per work. Compare prior
results and current conditions, perform only an authorized small check, capture
actual evidence, and add its result to the same probe through a revision-checked
note or patch. Retain all existing connections, practice and prior results. A
changed question or method is a different investigation.

For concurrent edits, use CLI `shikii wiki reconcile` with JSON
`{"patch":{...}}` on stdin, or MCP/HTTP `operation:"reconcile",patch:{...}`.
Use the original read revision, normal sources, change_reason and exact edits.
The read-only response compares base/current/proposed: `ready` contains a patch
against the checked current revision; `conflict` retains both alternatives;
`unavailable` cannot verify the article; `unchanged` needs no write. Only
nonoverlapping text edits are combined. Competing structured learning, related
links or content roles require interpretation, preserving other writers' results.
Inspect the proposal and apply its `patch` through ordinary wiki patch, retaining
`revision` and `requires`. A newer revision refuses the save: compare again.
The preview fits 128 KiB and its CLI JSON input fits 1 MiB. The Knowledge editor
offers the same unsaved comparison after a stale save, then an explicit guarded
save. Update all writers and restart sessions/page/MCP before using new contexts.

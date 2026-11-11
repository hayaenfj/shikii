package main

// Keep the complete workflow once in the entry point's description. Some MCP
// hosts attach initialize.instructions to every tool, so that field carries
// only discovery and immediate safeguards, including for deferred tool loading.
const mcpInstructions = "This project's memory is its wiki: source-backed hypotheses, never permission. Start shikii_wiki prepare with work:{query,session_id,turn_id} from the hook, or {query,request}. Read evidence; recover missing originals. End with record work:{receipt,result:{changed,checked?,evidence?:[refs],remaining?},...}; combine reflection and reuse evidence. result.sufficient shares checked quality/evidence; omission stays unknown. No new knowledge needs no wiki draft. Extend prepare with receipt/query, proposal or conditions; max_bytes bounds output. A rule, test or procedure goes to a wiki howto or rule; set kind/scene and classify old pages. Check complete/storage/errors/pending; retry failed refs. Never invent use or success; never call shikii_label with your own judgment. shikii_specs and shikii_decide use checked lines. Pass the absolute registered project as project; for a worktree use the original project, never initialize another wiki."

const mcpBootstrap = "This project's memory is its wiki: source-backed hypotheses, never permission. Read the complete shared workflow in shikii_wiki's description before using any tool; start with prepare. Never invent use or success; never call shikii_label with your own judgment. Pass the absolute registered project as project; for a worktree use the original project, never initialize another wiki."

var mcpTools = []map[string]any{
	{
		"name":        "shikii_reflect",
		"description": "Reflection in one place (query lists the pending conversations oldest first, the oldest 2 with their text and the next as heads, and counts the ones that did not fit in more_pending for the next query; submit takes any number). query reads each conversation's original text, targets, delivered lessons and the person's corrections; place takes a draft lesson and returns where it could go and suspected contradictions; submit saves reviews, wiki and memory. memory records helpful/unused/harmful/unknown with the decision it was used for and the evidence checked, plus correction_result (followed/repeated/not_applicable/unknown) for a delivered correction; delivery or a successful task alone is no effect, and too little evidence is unknown. A correction lesson carries correction:{source_id,quote,action}. Never mixed into truths or calibration. If the reply says complete:false, fix each error and resend; complete:true only says every item was saved: while left lists a conversation, submit what it names before finishing.",
		"inputSchema": schemaObject(map[string]any{
			"operation":   map[string]any{"type": "string", "enum": []string{"query", "place", "submit"}},
			"maintenance": map[string]any{"type": "array", "items": maintenanceSchema},
			"discoveries": map[string]any{"type": "array", "items": discoverySchema, "description": "Only manual lookups that demonstrably informed a decision. Use the returned lookup id and exact revision; context, decision and evidence each at most 1000 bytes. Not a human truth."},
			"memory": map[string]any{"type": "array", "items": schemaObject(map[string]any{
				"source_id": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"},
				"result":   map[string]any{"type": "string", "enum": []string{"helpful", "unused", "harmful", "unknown"}},
				"decision": map[string]any{"type": "string"}, "evidence": map[string]any{"type": "string"},
				"correction_result": map[string]any{"type": "string", "enum": []string{"followed", "repeated", "not_applicable", "unknown"}},
			}, []string{"source_id", "key", "result"}...)},
			"reviews": map[string]any{"type": "array", "items": schemaObject(map[string]any{
				"source_id": map[string]any{"type": "string"}, "trace_id": map[string]any{"type": "string"},
				"truth": map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}}, "reason": map[string]any{"type": "string"},
			}, []string{"source_id", "trace_id", "truth", "reason"}...)},
			"wiki": map[string]any{"type": "array", "items": schemaObject(map[string]any{
				"sources":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"correction":    correctionSchema,
				"change_reason": map[string]any{"type": "string"},
				"rationale":     rationaleSchema, "watch": watchSchema, "passages": passagesSchema, "learning": learningSchema,
				"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "lesson": map[string]any{"type": "string"}, "applies_to": map[string]any{"type": "string"},
				"replaces": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"body":     map[string]any{"type": "string", "description": "the knowledge text, combining background, evidence, conditions and open points from the sources. Up to 16KB. Omit to keep the existing text; an empty string clears it"},
				"related":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of related pages in the same project. Omit to keep; an empty array clears"},
				"kind":     kindSchema,
				"scene":    sceneSchema,
			}, []string{"sources"}...)},
		}, []string{"operation"}...),
	},
	{
		"name":        "shikii_review",
		"description": "Tentatively scores, with evidence, whether the work stayed within what was asked and was completed, from the conversation and the work's results. Use query to check unscored original text, targets and examples of the person's corrections for the same work; save source_id / trace_id / truth / reason with submit. If the evidence is not enough, use unknown. Do not count a follow-up request, a greeting or silence alone as success or failure. Never mix this into human truths or calibration. shikii picks the spot checks for the person.",
		"inputSchema": schemaObject(map[string]any{
			"operation": map[string]any{"type": "string", "enum": []string{"query", "submit"}},
			"source_id": map[string]any{"type": "string"}, "trace_id": map[string]any{"type": "string"},
			"truth":  map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}},
			"reason": map[string]any{"type": "string", "description": "the concrete reason for the score, from the original text and the result"},
		}, []string{"operation"}...),
	},
	{
		"name":        "shikii_specs",
		"description": "List the decision specs shikii knows: id, type, question, thresholds and the answers (truths) each accepts. Call this first to learn spec ids and valid truths.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name":        "shikii_decide",
		"description": "Ask a decision model one typed question defined by a spec. Returns the band (act = safe to act automatically, ask = a human should confirm, discard = do nothing, abstain = not decided), the top answer, probabilities and a trace_id. act only appears when the spec is calibrated for this model. Consult it in the middle of the work too, with the request as an input (needs-plan: should I ask first; changes-in-scope with files: is this change within what was asked; done-as-asked with summary: is it finished): the reply then also carries similar_decisions, the person's answers on similar requests with the agent's and the model's answers, and lessons, the closest wiki lessons, so you reason with this project's judgment and not only a probability.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":    map[string]any{"type": "string", "description": "spec id (see shikii_specs)"},
				"inputs":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "values for the spec's state template, e.g. {\"message\": \"...\"}. If the spec has state.context, also pass its extra inputs (e.g. \"previous\"): they are used only when the minimal question is unsure"},
				"tainted": map[string]any{"type": "boolean", "description": "true if this session touched production data: the request then never leaves the machine"},
			},
			"required": []string{"spec", "inputs"},
		},
	},
	{
		"name":        "shikii_label",
		"description": "Record what was actually true for an earlier decision (by trace_id). Use the option id, yes/no, or the level index as truth; '!x' means 'it was not x'. When enough truths exist the spec is calibrated automatically and the result says so.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id": map[string]any{"type": "string"},
				"truth":    map[string]any{"type": "string"},
				"spec":     map[string]any{"type": "string", "description": "optional spec id to narrow the lookup"},
				"weight":   map[string]any{"type": "number", "description": "1 for a firm truth, lower (e.g. 0.5) for a weak one"},
				"source":   map[string]any{"type": "string", "description": "ignored: a truth sent through MCP is always recorded with source mcp (sent by the agent). It still counts as a truth and takes the decision off the person's Answer page, so send only what the person said or a checked result, never your own judgment"},
			},
			"required": []string{"trace_id", "truth"},
		},
	},
	{
		"name":        "shikii_tune",
		"description": "Score the candidate wordings of a spec (its variants:, plus N proposed by the configured chat model when propose > 0) on the stored truths. Returns per-candidate top-1, Brier, precision/recall at act, the holdout, and whether a winner may be adopted. Adoption itself is not a tool: a person must confirm on the page that the new wording still asks the same thing.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":    map[string]any{"type": "string"},
				"propose": map[string]any{"type": "integer", "description": "how many candidates the proposer should add (0 = only the spec's own variants)"},
			},
			"required": []string{"spec"},
		},
	},
	{
		"name":        "shikii_status",
		"description": "Inventory per spec: records, truths, and for each (model, wording hash) whether it is collecting, ready, calibrated or stale.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name":        "shikii_wiki",
		"description": "Grows this project's wiki. Start with prepare and a work object (query, current hook session_id/turn_id if available, otherwise request). It returns project knowledge, pending work and a receipt. record takes work:{receipt,...} with reasoning start/update/finish, optional trial plan/result, matched execution comparison, observations and notes; depth selects reading effort and next_trial selects a current check; times, revisions, source ids, placement and partial retries are handled internally. Read orientation (interpreted current/history/recheck/counterexamples, unclassified gaps and changed premises) before mixed chronology; exact body_excerpts precede full rereads. Save exact, source-backed passages with notes or patch, and handle procedure_reviews with an evidenced note, then check its returned current review and record maintenance with that latest ref and reason; an edit alone does not complete review. With no edit, use the original ref and checked reason. Extend the same receipt in exactly one mode per prepare call: query (optional cursor) for search, proposal for consult, or conditions for reuse. Do not combine these modes. Earlier saves remain. Record discoveries by article ref and decision/evidence; outcomes record a supplied howto you actually executed, its observed execution conditions, action, failure, correction, result and sources. Do not copy an earlier run or invent an execution when only reading or planning. Outcomes are reports, never success labels or truths. Read how_to_record and check complete/errors/pending. consult takes request + proposal and returns linked conditions, rationale and source-backed material: explain support, counterexamples and exceptions. reuse takes request + current conditions and returns howtos, cited evidence, work records and up to two prior outcome candidates per howto. Compare their actual conditions, failures, sources, unknowns and revision warnings before assembling a procedure; retrieval order is not proof of applicability or success. Learning connections, probe results and versioned practices stay in the wiki; inspect learning_context and its rechecks. organize previews exact source-preserving moves with organization:{reason,sources,targets,moves}; publish creates before guarded patches. Neither executes commands. query reads lessons, unprocessed sources and up to 2 pages to maintain (page on with next_cursor); a query naming source ids returns their original text. capture saves a source you checked (code, a test, a Release ...) with kind, version, time and reference. ingest saves a lesson: a short lesson and a body (background, reasons, open points), applies_to, source ids, related pages, a kind and a scene in the work's own words; id updates a page (send lesson, title and applies_to again), replaces retires old ones, and sources alone marks them processed. For an explicit correction add correction:{source_id,quote,action} (the person's reply verbatim, the next action). collective adds bounded changes, explicit hypothesis contrasts and probe opportunities; reviewed knowledgeN/changeN confirms inspection, never usefulness. reconcile previews a pinned patch against base/current/proposed without saving; apply only its ready patch. patch saves precise text edits against a read revision, with sources and change_reason, preserving other fields. diagnose returns recorded delivery reasons for id (optional case_id); no record means unknown. compare previews patch against up to eight recorded cases without saving; it compares retrieval candidates, not actual delivery or usefulness. classify gives a kind and a scene to the unclassified pages (from before 0.9.0) that query lists; maintain records a review (latest page_id, revision, key, reason). lint checks sources and links and rebuilds the index. handoff_list / handoff_read / handoff_write / handoff_append handle work records in .shikii/handoff/ (files you rewrite as you go: audit reports, release steps and records, diffs), which the person reads on the page. Lessons go to ingest, evidence as checked to capture, working files to handoff. Everything is a hypothesis: a quote does not prove its meaning, and nothing here changes truths, calibration or the confirmed state. Do not ask the person to operate the page.\n\nShared workflow:\n" + mcpInstructions,
		"inputSchema": schemaObject(map[string]any{
			"operation":    map[string]any{"type": "string", "enum": []string{"prepare", "record", "query", "consult", "reuse", "organize", "reconcile", "ingest", "patch", "diagnose", "compare", "capture", "maintain", "lint", "classify", "handoff_list", "handoff_read", "handoff_write", "handoff_append"}},
			"work":         workSchema,
			"request":      map[string]any{"type": "string", "maxLength": 16000, "description": "Current task for consult or reuse; at most 16000 UTF-8 bytes."},
			"proposal":     map[string]any{"type": "string", "maxLength": 16000, "description": "Proposed approach (required for consult). Check support, counterexamples and exceptions against returned evidence."},
			"conditions":   map[string]any{"type": "string", "maxLength": 16000, "description": "Current environment and prerequisites (required for reuse). Assemble a procedure with sources, validation and changed premises."},
			"path":         map[string]any{"type": "string", "description": "path of a work record (handoff_*), relative to .shikii/handoff/ (e.g. audit-0.8.1/report-A.md). Refuses .., hidden names and absolute paths"},
			"content":      map[string]any{"type": "string", "description": "text for handoff_write (replace) or handoff_append (add to the end). UTF-8 text, up to 1 MiB per call"},
			"patch":        patchSchema,
			"organization": organizationSchema,
			"create_only":  map[string]any{"type": "boolean", "description": "Refuse an existing article id/title. Keep true on organization creates."},
			"case_id":      map[string]any{"type": "string", "description": "A recorded diagnostic case id; omit to list recent cases."},
			"cases":        map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"type": "string"}, "description": "Recorded cases for compare; omit for the latest eight. Missing or incomplete history is unavailable."},
			"maintenance":  maintenanceSchema,
			"rationale":    rationaleSchema, "watch": watchSchema, "passages": passagesSchema, "learning": learningSchema,
			"observation": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"kind":  map[string]any{"type": "string", "enum": []string{"human", "agent", "code", "test", "release"}},
				"title": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "reference": map[string]any{"type": "string"}, "version": map[string]any{"type": "string"}, "observed_at": map[string]any{"type": "string", "format": "date-time"},
			}, "required": []string{"kind", "title", "content", "reference", "version", "observed_at"}, "description": "a copy of a source you checked. The program never runs or fetches the reference. It is an observation the AI provides and never becomes permission to act or a truth"},
			"query":         map[string]any{"type": "string", "description": "topic words to search for. Returns search.total and candidates; when there are many, pass next_cursor to cursor with the same search. It is not a semantic search, so check that the topic matches"},
			"cursor":        map[string]any{"type": "string", "description": "the next_cursor from a response to the same query or sources. Read the rest instead of cutting by count"},
			"sources":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of sources observed in this work. When there are many, pass next_cursor to cursor with the same sources to read the rest"},
			"id":            map[string]any{"type": "string", "description": "id of the existing lesson to update"},
			"correction":    correctionSchema,
			"change_reason": map[string]any{"type": "string"},
			"title":         map[string]any{"type": "string"},
			"lesson":        map[string]any{"type": "string", "description": "a summary used for search and short quotes. Up to 2000 bytes. It is the machine's interpretation, so it is saved as a hypothesis"},
			"body":          map[string]any{"type": "string", "description": "the knowledge text, combining background, evidence, conditions and open points from the sources. Up to 16KB. Omit to keep the existing text; an empty string clears it"},
			"related":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of related pages in the same project. Omit to keep; an empty array clears"},
			"applies_to":    map[string]any{"type": "string", "description": "the conditions under which this lesson applies"},
			"replaces":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of old lessons from the same work that this lesson updates. Omitting it on an update keeps the existing value; an empty array removes the replacement"},
			"kind":          kindSchema,
			"scene":         sceneSchema,
		}, []string{"operation"}...),
	},
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

const compactWorkHowTo = "Preparation registered this task's start. At the end, record once with this receipt and result:{changed,checked?,evidence?:[refs],remaining?}. evidence takes references (current, newN for observation N of this call, knowledgeN), never text: what you checked goes in checked. Save checked evidence once; result and observations supply source refs for notes/outcomes. Optional result.sufficient shares an actual quality check and the same evidence with automatic procedure validation; omission stays unknown. Add only new knowledge or corrections; no wiki draft is required otherwise. Combine supplied reviews, memory and reviewed refs in that call. unknown/unused memory needs no long narrative. Check complete, storage, errors, pending and next_action; retry only failed refs. Read omitted originals before assessing them. Use explicit reasoning updates or trials only when useful. See the installed skill's recording.md for the full contract."

const continuedWorkHowTo = "Use this receipt and its initial recording contract. Report once; keep evidence refs. A pending procedure index recovers automatically on prepare."

// cliPrepareBytes bounds a prepare reply on the CLI when the request names no max_bytes. An agent reads the CLI's reply through its
// shell tool, which shows a bounded part of a command's output and cuts the middle of a longer one: Claude Code shows 30,000
// characters, Codex 10,000 tokens (about 40,000 bytes). The general default below is twice what the MCP server sends (its bound
// counts the text and the structured copy of one reply), and with it 14 to 28% of the prepare replies an agent read directly were
// cut by the tool (measured on 2026-11-11). What does not fit 28 KiB is left out by limitWorkPreparation, which names it in output
// and keeps the way back to it; max_bytes asks for another size.
const cliPrepareBytes = 28 << 10

func workOutputBudget(n int) (int, error) {
	if n == 0 {
		return 64 << 10, nil
	}
	if n < 8192 || n > workSourceBudget*3 {
		return 0, errors.New("max_bytes must be between 8192 and 393216 UTF-8 JSON bytes")
	}
	return n, nil
}

// Bound the complete JSON response, rather than each nested component alone.
// Omitted bodies keep exact recovery queries. Never register unseen text as a
// delivered quote or teach a reading preference from a shortened response.
func limitWorkPreparation(book notebook.Book, w *workState, reply map[string]any, quoted map[string]string, requested int, sizing ...func(any) int) ([]store.KnowledgeReference, error) {
	// Reserve the newline added by CLI and HTTP JSON encoders.
	rawSize := workPreparationSizer(sizing)
	before := suppliedPreparationBytes(reply)
	var reading *workReadingTrace
	if w.Reasoning != nil {
		reading = w.Reasoning.Reading
	}
	readingBytes, readingMixed := 0, false
	if reading != nil {
		readingBytes, readingMixed = reading.Bytes, reading.Mixed
	}
	limited := false
	// Sharing and shortening change the reading metadata too. Include those
	// changes in both the sharing decision and every pre-save budget check.
	actualSize := func(v any) int {
		if reading != nil {
			reading.Mixed = readingMixed || limited || reply["reading_notices"] != nil || reusedWorkMetadata(reply) || reusedWorkNotice(reply)
			reading.Bytes = max(0, readingBytes-max(0, before-suppliedPreparationBytes(reply)))
		}
		if limited && w.Reasoning != nil && w.Reasoning.Procedure != nil {
			if _, delivered := reply["attention"]; !delivered {
				w.Reasoning.Procedure.Mixed = true
			}
		}
		return rawSize(v)
	}
	size := func(v any) int {
		shareWorkExplanations(reply, actualSize)
		return actualSize(v)
	}
	budget, err := workOutputBudget(requested)
	if err != nil {
		return nil, err
	}
	responseBudget := budget
	budget--
	// Reserve bounded recovery metadata before committing any quote/read state.
	if pendingWorkProcedure(w) || w.Reasoning != nil && w.Reasoning.Procedure != nil && w.Reasoning.Procedure.CloseReport {
		reserve := 512
		if len(sizing) > 0 && sizing[0] != nil {
			reserve = 2048
		}
		budget -= reserve
	}
	entries, _ := reply["knowledge"].([]any)
	reuseWorkMetadata(w, reply, actualSize)
	if size(reply) <= budget {
		return suppliedPreparationReads(reply), nil
	}
	limited = true
	omitted := []string{}
	if reply["how_to_record"] != continuedWorkHowTo {
		reply["how_to_record"] = compactWorkHowTo
	}
	for _, raw := range entries {
		e := raw.(map[string]any)
		if e["reused"] == true {
			continue
		}
		p, ok := e["article"].(notebook.Page)
		if !ok {
			v, err := book.Query("lesson:" + e["id"].(string) + "@" + e["revision"].(string))
			if err != nil || len(v.Lessons) != 1 {
				return nil, errors.New("recover the pinned article before shortening its response")
			}
			p = v.Lessons[0]
		}
		orientation := notebook.Orient(p, w.Query, 300)
		ref := e["ref"].(string)
		if w.Loop != nil {
			delete(w.Loop.Bodies, p.ID+"@"+p.Revision)
			delete(w.Loop.Excerpts, p.ID+"@"+p.Revision)
		}
		short := map[string]any{"ref": ref, "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "orientation": orientation, "body_included": false, "attention_limited": true, "query": e["query"], "reading_notice": "Incomplete original text. Check conditions, history, counterexamples and changed premises through this exact revision query before relying on missing material."}
		for key := range e {
			delete(e, key)
		}
		for key, value := range short {
			e[key] = value
		}
		quoted[ref] = workArticleSource(p, true, orientation.Excerpts())
	}
	// Original conversations, evidence and reflection remain recoverable by
	// their source IDs. Do not return shortened text with a scoring reference.
	for _, field := range []string{"pending", "observations"} {
		list, _ := reply[field].([]any)
		for i, raw := range list {
			e, ok := raw.(map[string]any)
			if !ok || workContextSize(e) < 1500 {
				continue
			}
			// An initial partial source has no writable alias. Fit its first
			// part inside this complete response instead of relying on unused
			// space left by the notebook's independent paging algorithm.
			if fitPreparedSource(book, reply, e, budget, size) {
				continue
			}
			ref, _ := e["ref"].(string)
			id := w.Sources[ref]
			if id == "" {
				continue
			}
			list[i] = map[string]any{"incomplete": true, "source_query": map[string]any{"operation": "prepare", "work": map[string]any{"receipt": reply["receipt"], "query": id}}, "next_action": "Read the original source before assessing it; no full-text assessment ref is supplied here."}
		}
	}
	for _, field := range []string{"reflection_context", "collective", "knowledge_context", "context_notice", "memory_upgrade", "conversation", "conversation_context"} {
		if size(reply) <= budget {
			break
		}
		if field == "knowledge_context" {
			// the search candidates go in steps, the top of the ranking last; the whole list only when even that does not fit
			if taken, fits := trimKnowledgeCandidates(w, reply, budget-1000, size); fits {
				omitted = append(omitted, taken...)
				continue
			}
		}
		if field == "memory_upgrade" {
			// the pages of the batch go without their bodies before the batch goes whole
			if taken, fits := trimMemoryUpgrade(reply, quoted, budget-1000, size); fits {
				omitted = append(omitted, taken...)
				continue
			}
		}
		if reply[field] != nil {
			delete(reply, field)
			omitted = append(omitted, field)
		}
	}
	if size(reply) > budget {
		// A lesson first keeps its title beside its references (a bare id tells an agent nothing about whether to read it), and
		// loses the title too only when that still does not fit.
		for _, keep := range []string{"title", ""} {
			for _, raw := range entries {
				e := raw.(map[string]any)
				ref := e["ref"].(string)
				delete(quoted, ref)
				for key := range e {
					if key != "ref" && key != "id" && key != "revision" && key != "query" && key != keep {
						delete(e, key)
					}
				}
				e["body_included"], e["attention_limited"] = false, true
				e["reading_notice"] = "Original text omitted; read the exact revision query, including its conditions and exceptions."
			}
			if size(reply) <= budget {
				break
			}
		}
	}
	if size(reply) > budget-1000 {
		for _, field := range []string{"reasoning", "reading", "attention", "pending", "observations"} {
			if size(reply) <= budget-1000 {
				break
			}
			if _, exists := reply[field]; !exists {
				continue
			}
			delete(reply, field)
			omitted = append(omitted, field)
		}
	}
	reply["output"] = workPreparationOutput(responseBudget, omitted)
	if hinted := workPreparationHint(reply["output"].(map[string]any)); size(reply) > budget {
		hinted["next_action"] = workPreparationNext // the hint is the first thing a tight bound leaves out
	}
	if size(reply) > budget {
		return nil, fmt.Errorf("the task and reference inventory exceed max_bytes (%d); prepare again with a larger max_bytes (up to %d)", responseBudget, workSourceBudget*3)
	}
	for ref := range quoted {
		visible := false
		for _, raw := range entries {
			visible = visible || raw.(map[string]any)["ref"] == ref
		}
		if upgrade, ok := reply["memory_upgrade"].(map[string]any); ok {
			if pages, ok := upgrade["pages"].([]any); ok {
				for _, raw := range pages {
					visible = visible || raw.(map[string]any)["ref"] == ref
				}
			}
		}
		if !visible {
			delete(quoted, ref)
		}
	}
	return suppliedPreparationReads(reply), nil
}

// trimKnowledgeCandidates makes the search candidates of a reply fit, the least useful part first: the heads of other pending
// conversations (pending carries them), each candidate's applies_to (its title, id and exact query stay), then the candidates at
// the end of the ranking, with a cursor in knowledge_query that continues right after the last one shown. It returns what it took
// off, as output names it, and whether the reply now fits. When it does not, the reply is as it was and the caller leaves the list
// out whole. Candidates whose lessons are in knowledge are never cut (the next page would send those lessons again).
func trimKnowledgeCandidates(w *workState, reply map[string]any, budget int, size func(any) int) (taken []string, fits bool) {
	context, _ := reply["knowledge_context"].(map[string]any)
	search, _ := context["search"].(map[string]any)
	candidates, _ := search["candidates"].([]any)
	if len(candidates) == 0 {
		return nil, false
	}
	pending, hadPending := context["pending_sources"]
	applies := make([]any, len(candidates))
	for i, raw := range candidates {
		applies[i] = raw.(map[string]any)["applies_to"]
	}
	query, _ := reply["knowledge_query"].(map[string]any)
	holder := query // the object that holds the cursor: knowledge_query itself, or its work for a search added to a receipt
	if work, ok := query["work"].(map[string]any); ok {
		holder = work
	}
	cursor, hadCursor := holder["cursor"]
	restore := func() {
		if hadPending {
			context["pending_sources"] = pending
		}
		for i, raw := range candidates {
			if applies[i] != nil {
				raw.(map[string]any)["applies_to"] = applies[i]
			}
		}
		search["candidates"] = candidates
		if holder != nil {
			if hadCursor {
				holder["cursor"] = cursor
			} else {
				delete(holder, "cursor")
			}
		}
	}
	if hadPending {
		delete(context, "pending_sources")
		taken = append(taken, "knowledge_context.pending_sources")
		if size(reply) <= budget {
			return taken, true
		}
	}
	for _, raw := range candidates {
		delete(raw.(map[string]any), "applies_to")
	}
	taken = append(taken, "knowledge_context.search.candidates[].applies_to")
	if size(reply) <= budget {
		return taken, true
	}
	if w == nil || w.candidateCursor == nil || holder == nil {
		restore()
		return nil, false
	}
	delivered := map[string]bool{}
	if entries, ok := reply["knowledge"].([]any); ok {
		for _, raw := range entries {
			if e, ok := raw.(map[string]any); ok {
				if id, ok := e["id"].(string); ok {
					delivered[id] = true
				}
			}
		}
	}
	least := 1
	for i, raw := range candidates {
		if id, _ := raw.(map[string]any)["id"].(string); delivered[id] {
			least = i + 1
		}
	}
	show := func(keep int) bool {
		search["candidates"] = candidates[:keep]
		holder["cursor"] = w.candidateCursor(keep)
		return size(reply) <= budget
	}
	if least >= len(candidates) || !show(least) {
		restore()
		return nil, false
	}
	low, high := least, len(candidates)-1 // the most candidates that fit: low fits
	for low < high {
		middle := (low + high + 1) / 2
		if show(middle) {
			low = middle
		} else {
			high = middle - 1
		}
	}
	show(low)
	return append(taken, "knowledge_context.search.candidates after the first "+strconv.Itoa(low)+" (knowledge_query continues)"), true
}

func fitPreparedSource(book notebook.Book, reply, entry map[string]any, budget int, size func(any) int) bool {
	if entry["incomplete"] != true {
		return false
	}
	query, ok := entry["source_query"].(map[string]any)
	if !ok {
		return false
	}
	ids, ok := query["sources"].([]string)
	if !ok || len(ids) != 1 {
		return false
	}
	original, oldQuery := entry["source"], entry["source_query"]
	measure := func(value any) int {
		part := value.(map[string]any)
		next := maps.Clone(query)
		delete(next, "cursor")
		if cursor, ok := part["next_cursor"].(string); ok && cursor != "" {
			next["cursor"] = cursor
		}
		entry["source"], entry["source_query"] = part, next
		// Leave room for the omission envelope added after source fitting.
		return size(reply) + 1024
	}
	_, err := book.ReadSourcesPageMeasured(ids, "", budget, measure)
	if err != nil || size(reply)+1024 > budget {
		entry["source"], entry["source_query"] = original, oldQuery
		return false
	}
	return true
}

const workPreparationNext = "Read exact article/source queries only as needed. Use the original host conversation for omitted task/history. reflect query recovers supporting reflection; memory-upgrade returns the current maintenance batch. No omitted body or evaluation is complete."

func workPreparationOutput(budget int, omitted []string) map[string]any {
	return map[string]any{"limited": true, "max_bytes": budget, "omitted": omitted, "next_action": workPreparationNext}
}

// workPreparationHint adds, to the output of a shortened reply, how to get more in one call: the search that finds the candidates
// left out, and the bound itself. The caller takes it back when the reply would not fit with it.
func workPreparationHint(output map[string]any) map[string]any {
	output["next_action"] = workPreparationNext + " knowledge_query continues the search after the candidates shown, or repeats it when none is; a larger max_bytes returns more in one reply."
	return output
}

// trimMemoryUpgrade keeps the upgrade batch when its pages do not fit: first without the orientation of each page (passages of
// the body that comes with it), then without the bodies. A page without its body keeps its reference, its maintenance reference,
// its task, the head of the article and the exact query that reads it, and is not counted as read or quotable. Before, the batch
// went whole, also in answer to the query memory-upgrade, and the agent had to guess a larger max_bytes (2026-11-12). It returns
// what it took and whether the reply fits now.
func trimMemoryUpgrade(reply map[string]any, quoted map[string]string, budget int, size func(any) int) ([]string, bool) {
	upgrade, _ := reply["memory_upgrade"].(map[string]any)
	pages, _ := upgrade["pages"].([]any)
	if len(pages) == 0 {
		return nil, false
	}
	for _, raw := range pages {
		delete(raw.(map[string]any), "orientation")
	}
	taken := []string{"memory_upgrade.pages[].orientation"}
	if size(reply) <= budget {
		return taken, true
	}
	for _, raw := range pages {
		e := raw.(map[string]any)
		p, ok := e["article"].(notebook.Page)
		if !ok {
			return nil, false
		}
		e["article"] = map[string]any{"id": p.ID, "revision": p.Revision, "title": p.Title, "kind": p.Kind, "lesson": p.Lesson}
		e["body_included"] = false
		e["query"] = map[string]any{"operation": "query", "query": "lesson:" + p.ID + "@" + p.Revision}
		e["next_action"] = "Read the page with its query before reviewing it; its ref and maintenance_ref stay valid with this receipt."
		if ref, _ := e["ref"].(string); ref != "" {
			delete(quoted, ref)
		}
	}
	taken = append(taken, "memory_upgrade.pages[].article (each query reads its page whole)")
	return taken, size(reply) <= budget
}

func suppliedPreparationReads(reply map[string]any) []store.KnowledgeReference {
	entries, _ := reply["knowledge"].([]any)
	read := suppliedWorkReads(entries)
	if upgrade, ok := reply["memory_upgrade"].(map[string]any); ok {
		if pages, ok := upgrade["pages"].([]any); ok {
			for _, raw := range pages {
				e := raw.(map[string]any)
				if p, ok := e["article"].(notebook.Page); ok && e["body_included"] == true {
					read = append(read, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
				}
			}
		}
	}
	return read
}

func suppliedPreparationBytes(reply map[string]any) int {
	n := workContextSize(reply["knowledge"])
	if notices := reply["reading_notices"]; notices != nil {
		n += len(`,"reading_notices":`) + workContextSize(notices)
	}
	if u, ok := reply["memory_upgrade"].(map[string]any); ok && u["pages"] != nil {
		n += workContextSize(u)
	}
	return n
}

// Recovery may update status after the atomic context save. It cannot change
// delivered knowledge: only its newly produced judgment details can be reduced.
func limitRecoveredPreparation(reply map[string]any, requested int, sizing ...func(any) int) error {
	size := workPreparationSizer(sizing)
	budget, err := workOutputBudget(requested)
	if err != nil {
		return err
	}
	if size(reply)+1 <= budget {
		return nil
	}
	if r, ok := reply["reasoning"].(reasoningStatus); ok {
		reply["reasoning"] = map[string]any{"required": r.Required, "started": r.Started, "complete": r.Complete, "wiki_pending": r.WikiPending, "details_omitted": true, "next_action": "Prepare with a larger max_bytes to read saved judgment and trial details."}
	}
	if size(reply)+1 > budget {
		return fmt.Errorf("recovery metadata exceeds max_bytes (%d); prepare again with a larger max_bytes (up to %d)", budget, workSourceBudget*3)
	}
	return nil
}

func workPreparationSizer(sizing []func(any) int) func(any) int {
	if len(sizing) > 0 && sizing[0] != nil {
		// A generation is the complete encoded value, including mutable nested
		// maps, reading accounting and rejected metadata candidates. Comparing
		// bytes avoids mutation hooks and never trusts size/mtime or a hash.
		// Keep this cache local to one reduction pass; post-save recovery gets a
		// fresh host measurement through its separate preparation sizer.
		var previous []byte
		var generation uint64
		measuredGeneration, measured := uint64(0), 0
		return func(value any) int {
			encoded, err := json.Marshal(value)
			if err != nil {
				previous = nil
				generation++
				return sizing[0](value)
			}
			if generation == 0 || !bytes.Equal(previous, encoded) {
				generation++
				previous = encoded
			}
			if measuredGeneration != generation {
				measured = sizing[0](value)
				measuredGeneration = generation
			}
			return measured
		}
	}
	return workContextSize
}

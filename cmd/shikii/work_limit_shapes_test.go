package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// limitShapes are replies of three different shapes, as prepare builds them before limitWorkPreparation bounds them: candidates (a
// few articles with their text, orientation, search candidates and the status fields), pending (conversations and observations
// waiting to be reflected on) and coverage (open areas of the ledger beside an article). The reply is made from nothing but
// constants, so the same shape is the same bytes on every run: testdata/work_limit_golden.json holds what the code before the
// tail of the bound (work_budget_tail.go) made of each shape at each bound, and a reply that fits today must stay those bytes.
var limitShapeNames = []string{"candidates", "pending", "coverage", "everything"}

// limitShapeBounds are the max_bytes each shape is bounded with: the smallest a caller can name, the sizes in between, and the CLI's.
var limitShapeBounds = []int{8192, 9000, 10000, 12288, 16384, 28672}

func limitShapePage(i, body int) notebook.Page {
	return notebook.Page{ID: fmt.Sprintf("%016x", 0x1000+i), Revision: fmt.Sprintf("%016x", 0x2000+i), Title: fmt.Sprintf("Export rule %d", i), Lesson: fmt.Sprintf("Check export filter %d before the totals.", i), AppliesTo: fmt.Sprintf("export audit %d", i), Rationale: "The filters decide which rows the totals count.", Body: strings.Repeat(fmt.Sprintf("Filter %d stays on until the totals are checked.\n\n", i), body/50+1)}
}

func limitShapeEntry(i, body int) map[string]any {
	page := limitShapePage(i, body)
	entry := map[string]any{"ref": fmt.Sprintf("knowledge%d", i), "id": page.ID, "revision": page.Revision, "article": page, "body_included": true, "query": map[string]any{"operation": "query", "query": "lesson:" + page.ID + "@" + page.Revision}}
	bindOrientation(entry, notebook.Orient(page, "export audit", 2400))
	return entry
}

func limitShapeStatus(n int) map[string]any {
	return map[string]any{"detail": strings.Repeat("s", n), "complete": false}
}

// limitShapeMaintenance is a maintenance field of two tasks as prepare builds it (each task a notebook.MaintenanceTask), and
// limitShapeCarried is the references of an interrupted request: fields the bound did not touch before its last fields.
func limitShapeMaintenance() []any {
	items := []any{}
	for i := 1; i <= 2; i++ {
		task := notebook.MaintenanceTask{PageID: fmt.Sprintf("%016x", 0x1000+i), Revision: fmt.Sprintf("%016x", 0x2000+i), Key: fmt.Sprintf("%016x", 0x4000+i), Title: fmt.Sprintf("Export rule %d", i), Reasons: []string{"delivery-feedback", "unlinked"}, Dependencies: map[string]string{}, Feedback: []store.MemoryAssessment{{SourceID: fmt.Sprintf("%016x", i), Key: "lesson:x:y", Result: "harmful", Decision: strings.Repeat("d", 200), Evidence: strings.Repeat("e", 300)}}, FeedbackTotal: 1}
		items = append(items, map[string]any{"ref": fmt.Sprintf("maintenance%d", i), "task": task})
	}
	return items
}

func limitShapeCarried() map[string]any {
	return map[string]any{"articles": []any{"knowledge1", "knowledge2", "knowledge3"}, "sources": []any{"observed1", "observed2"}, "next_action": strings.Repeat("c", 540)}
}

// limitShape returns the reply, the work state and the quoted map of a shape.
func limitShape(name string) (map[string]any, *workState, map[string]string) {
	w := &workState{Sources: map[string]string{"source1": "aaaaaaaaaaaaaaaa", "source2": "bbbbbbbbbbbbbbbb", "evidence1": "cccccccccccccccc"}}
	quoted := map[string]string{}
	reply := map[string]any{"receipt": "r-" + strings.Repeat("c", 100), "task": "Audit the export.", "task_context": map[string]any{"role": "user"}, "how_to_record": compactWorkHowTo, "planning_required": true, "agent_supplied_task": false, "more_pending": 0, "maintenance": []any{}, "reasoning": limitShapeStatus(400), "reading": limitShapeStatus(300), "attention": limitShapeStatus(500)}
	switch name {
	case "candidates":
		entries := []any{}
		for i, body := range []int{2500, 2000, 1800} {
			e := limitShapeEntry(i+1, body)
			quoted[e["ref"].(string)] = "quote " + e["ref"].(string)
			entries = append(entries, e)
		}
		reply["knowledge"] = entries
		candidates := []any{}
		for i := 1; i <= 8; i++ {
			candidates = append(candidates, map[string]any{"id": fmt.Sprintf("%016x", 0x3000+i), "title": fmt.Sprintf("Candidate %d", i), "applies_to": strings.Repeat("a", 150)})
		}
		reply["knowledge_context"] = map[string]any{"search": map[string]any{"candidates": candidates}, "pending_sources": []any{"x1", "x2"}, "next_cursor": "c8"}
		reply["knowledge_query"] = map[string]any{"operation": "query", "query": "export audit", "cursor": "c8"}
		reply["collective"] = map[string]any{"notice": strings.Repeat("n", 600)}
		reply["maintenance"], reply["carried"] = limitShapeMaintenance(), limitShapeCarried()
		w.candidateCursor = func(keep int) string { return fmt.Sprintf("cursor-%d", keep) }
	case "pending":
		entries := []any{limitShapeEntry(1, 900)}
		quoted["knowledge1"] = "quote knowledge1"
		reply["knowledge"] = entries
		pending, observations := []any{}, []any{}
		for i, ref := range []string{"source1", "source2"} {
			pending = append(pending, map[string]any{"ref": ref, "request": fmt.Sprintf("Request %d", i), "source": map[string]any{"text": strings.Repeat("p", 1700+i*600)}})
		}
		observations = append(observations, map[string]any{"ref": "evidence1", "source": map[string]any{"text": strings.Repeat("o", 2200)}})
		reply["pending"], reply["observations"] = pending, observations
		reply["reflection_context"] = map[string]any{"lessons": strings.Repeat("l", 900)}
	case "everything":
		// the three shapes in one reply with bulky status fields: at the smaller bounds every field the bound can leave out goes
		reply["reasoning"], reply["reading"], reply["attention"] = limitShapeStatus(1200), limitShapeStatus(900), limitShapeStatus(1000)
		entries := []any{limitShapeEntry(1, 1400), limitShapeEntry(2, 1000)}
		quoted["knowledge1"], quoted["knowledge2"] = "quote knowledge1", "quote knowledge2"
		reply["knowledge"] = entries
		rows := []any{}
		for i := 1; i <= 5; i++ {
			rows = append(rows, map[string]any{"area": fmt.Sprintf("open area %d %s", i, strings.Repeat("x", 300)), "source": "00000000000000a1"})
		}
		reply["coverage"] = map[string]any{"open": 7, "rows": rows, "next_action": workCoverageNext}
		reply["earlier_search"] = map[string]any{"query": "export", "result": "0123456789abcdef", "next_action": strings.Repeat("e", 900)}
		reply["found_alone"] = map[string]any{"note": strings.Repeat("f", 900)}
		reply["pending"] = []any{map[string]any{"ref": "source1", "request": "Request", "source": map[string]any{"text": strings.Repeat("p", 1700)}}}
		reply["observations"] = []any{map[string]any{"ref": "evidence1", "source": map[string]any{"text": strings.Repeat("o", 1700)}}}
		reply["reflection_context"] = map[string]any{"lessons": strings.Repeat("l", 1100)}
		reply["collective"] = map[string]any{"notice": strings.Repeat("n", 900)}
		reply["maintenance"], reply["carried"] = limitShapeMaintenance(), limitShapeCarried()
	case "coverage":
		entries := []any{}
		for i, body := range []int{1600, 1200} {
			e := limitShapeEntry(i+1, body)
			quoted[e["ref"].(string)] = "quote " + e["ref"].(string)
			entries = append(entries, e)
		}
		reply["knowledge"] = entries
		rows := []any{}
		for i := 1; i <= 5; i++ {
			rows = append(rows, map[string]any{"area": fmt.Sprintf("open area %d %s", i, strings.Repeat("x", 120)), "source": "00000000000000a1"})
		}
		reply["coverage"] = map[string]any{"open": 7, "rows": rows, "next_action": workCoverageNext}
		reply["earlier_search"] = map[string]any{"query": "export", "result": "0123456789abcdef", "next_action": strings.Repeat("e", 700)}
		reply["found_alone"] = map[string]any{"note": strings.Repeat("f", 500)}
	}
	return reply, w, quoted
}

// limitShapeResult is what the golden file keeps of one bounded reply: the bytes of the reply, the error, the articles read, and the
// references still quoted.
type limitShapeResult struct {
	Err    string                     `json:"err,omitempty"`
	Reply  string                     `json:"reply,omitempty"`
	Read   []store.KnowledgeReference `json:"read,omitempty"`
	Quoted []string                   `json:"quoted,omitempty"`
}

func runLimitShape(name string, bound int) limitShapeResult {
	reply, w, quoted := limitShape(name)
	read, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, bound)
	if err != nil {
		return limitShapeResult{Err: err.Error()}
	}
	raw, _ := json.Marshal(reply)
	refs := []string{}
	for ref := range quoted {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return limitShapeResult{Reply: string(raw), Read: read, Quoted: refs}
}

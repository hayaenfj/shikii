package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// auditIView is a pending list whose first conversation has a long request and many deliveries of a corrected lesson, followed by
// conversations with their own deliveries, questions and changed files.
func auditIView(firstRequest, firstDeliveries, others int) reflectView {
	long := func(n int) string { return strings.Repeat("x", n) }
	mem := func(c, i int) store.MemoryObservation {
		q := store.MemoryQuote{Key: fmt.Sprintf("lesson:%08x%08x:%016x", c, i, i), ID: fmt.Sprintf("%08x%08x", c, i), Revision: fmt.Sprintf("%016x", i), Lesson: long(200), AppliesTo: long(200), Rationale: long(200)}
		if i%2 == 0 {
			q.Correction = &store.Correction{SourceID: fmt.Sprintf("c%015x", i), Quote: long(300), Action: long(300)}
		}
		return store.MemoryObservation{SourceID: fmt.Sprintf("d%07x%08x", c, i), Parent: "0000000000000000", Agent: fmt.Sprintf("agent-%d", i), Request: long(300), Quote: q}
	}
	view := reflectView{HowToSubmit: "submit with reviews and memory"}
	for c := 0; c <= others; c++ {
		request := fmt.Sprintf("request %d ", c) + long(800)
		n := 2
		if c == 0 {
			request, n = "first request "+strings.Repeat("Keep the filters of the CSV export. ", firstRequest/36), firstDeliveries
		}
		s := reflectSource{Source: store.Feedback{ID: fmt.Sprintf("%016x", c), Request: request, Summary: long(300), Reply: long(300)},
			Targets: []reviewTarget{{Spec: "done-as-asked", TraceID: fmt.Sprintf("1%015x", c), Question: "Was it done as asked?", Files: "a.go\nb.go", OriginalInputs: map[string]string{"request": request}}}}
		for i := 0; i < n; i++ {
			s.Memories = append(s.Memories, mem(c, i))
		}
		view.Sources = append(view.Sources, s)
	}
	return view
}

// INVARIANT 5 over every bound from the whole reply down, for several shapes of the first conversation: each conversation listed
// keeps spec, trace_id, the question, the changed files and the keys of its deliveries (with the mark of each correction);
// listed + more_pending is the number pending; how_to_submit stays; a reply over the bound says so; the first conversation is
// whole unless output says that it alone is over; and, but for the bound where the first becomes a head, a smaller bound never
// lists more conversations or brings back the raw text of one.
func TestAuditIReflectBoundsOverShapes(t *testing.T) {
	for _, shape := range []struct{ request, deliveries, others int }{{3000, 41, 4}, {9000, 30, 6}, {20000, 10, 3}, {500, 60, 8}, {12000, 0, 5}} {
		view := auditIView(shape.request, shape.deliveries, shape.others)
		total := len(view.Sources)
		byID := map[string]reflectSource{}
		for _, s := range view.Sources {
			byID[s.Source.ID] = s
		}
		whole := workContextSize(view)
		prevListed, prevRaw, prevCut, overs, cuts, steps := -1, -1, false, 0, 0, 0
		for bound := whole + 10; bound > 1500; bound -= 389 { // a step that is no divisor of the shapes; the boundary itself is TestAuditIReflectAtTheHeadBoundary's
			steps++
			raw, _ := json.Marshal(limitReflectQuery(view, bound, workContextSize))
			var reply map[string]any
			if err := json.Unmarshal(raw, &reply); err != nil {
				t.Fatal(err)
			}
			listed, _ := reply["sources"].([]any)
			more, _ := reply["more_pending"].(float64)
			omitted := ""
			if output, ok := reply["output"].(map[string]any); ok {
				omitted = fmt.Sprint(output["omitted"])
			}
			where := fmt.Sprintf("shape %v, bound %d (%d bytes)", shape, bound, len(raw))
			if len(raw) > bound {
				overs++
				if !strings.Contains(omitted, "still over max_bytes") {
					t.Fatalf("%s: over the bound and output does not say so: %s", where, omitted)
				}
			}
			if len(listed)+int(more) != total || reply["how_to_submit"] != view.HowToSubmit {
				t.Fatalf("%s: listed %d + more_pending %v != %d, or how_to_submit gone", where, len(listed), more, total)
			}
			rawCount := 0
			firstCut := false
			for n, x := range listed {
				src := x.(map[string]any)
				material := src["source_material"].(map[string]any)
				id := material["id"].(string)
				orig := byID[id]
				if material["request"] == orig.Source.Request {
					rawCount++
				} else if n == 0 {
					firstCut = true
				}
				targets, _ := src["targets"].([]any)
				if len(targets) != len(orig.Targets) {
					t.Fatalf("%s: %s lost targets", where, id)
				}
				for i, tg := range targets {
					m := tg.(map[string]any)
					if m["spec"] != orig.Targets[i].Spec || m["trace_id"] != orig.Targets[i].TraceID || m["question"] != orig.Targets[i].Question || m["files"] != orig.Targets[i].Files {
						t.Fatalf("%s: %s target %d lost spec, trace_id, question or files: %v", where, id, i, m)
					}
				}
				mems, _ := src["memories"].([]any)
				if len(mems) != len(orig.Memories) {
					t.Fatalf("%s: %s lists %d of its %d deliveries", where, id, len(mems), len(orig.Memories))
				}
				for i, mm := range mems {
					m := mm.(map[string]any)
					q, _ := m["quote"].(map[string]any)
					if m["source_id"] != orig.Memories[i].SourceID || q["key"] != orig.Memories[i].Quote.Key || (orig.Memories[i].Quote.Correction != nil) != (q["correction"] != nil) {
						t.Fatalf("%s: %s delivery %d lost its source_id, key or the mark of its correction: %v", where, id, i, m)
					}
				}
			}
			if firstCut {
				cuts++
				if !strings.Contains(omitted, "it alone is over max_bytes") {
					t.Errorf("%s: the first conversation is cut and output does not say it alone is over", where)
				}
				alone := view
				alone.Sources, alone.MorePending = []reflectSource{reflectBare(view.Sources[0])}, total-1
				if need := workContextSize(alone) + 1200; need <= bound {
					t.Errorf("%s: the first conversation is cut, yet alone with its deliveries bare it is about %d bytes", where, need)
				}
			}
			if prevListed >= 0 && firstCut == prevCut && (len(listed) > prevListed || rawCount > prevRaw) {
				t.Errorf("%s: a smaller bound lists %d conversations (%d whole), the bound before %d (%d whole)", where, len(listed), rawCount, prevListed, prevRaw)
			}
			prevListed, prevRaw, prevCut = len(listed), rawCount, firstCut
		}
		t.Logf("shape %v: %d bytes whole, %d bounds, %d with the first cut, %d over the bound (each says so)", shape, whole, steps, cuts, overs)
	}
}

// INVARIANT 5 at the bound where the first conversation becomes a head, byte by byte: a reply over the bound has its first
// conversation as a head already (whole, it says "still over max_bytes" where a head would have fitted). The check that decides
// the head measures the first conversation alone with the notes the final reply carries; leaving the note of the bare deliveries
// out of that measure shifts the decision by the length of the note, and no shipped test sees it.
func TestAuditIReflectAtTheHeadBoundary(t *testing.T) {
	for _, shape := range []struct{ request, deliveries, others int }{{9000, 30, 3}, {3000, 41, 4}, {20000, 10, 2}} {
		view := auditIView(shape.request, shape.deliveries, shape.others)
		cutAt := func(bound int) (bool, bool, int) {
			raw, _ := json.Marshal(limitReflectQuery(view, bound, workContextSize))
			var reply map[string]any
			_ = json.Unmarshal(raw, &reply)
			first := reply["sources"].([]any)[0].(map[string]any)["source_material"].(map[string]any)["request"]
			return first != view.Sources[0].Source.Request, len(raw) > bound, len(raw)
		}
		low, high := 1500, workContextSize(view)
		for low < high { // the largest bound where the first is cut
			mid := (low + high + 1) / 2
			if cut, _, _ := cutAt(mid); cut {
				low = mid
			} else {
				high = mid - 1
			}
		}
		overWhole := 0
		for bound := low - 50; bound <= low+400; bound++ {
			if cut, over, size := cutAt(bound); over && !cut {
				overWhole++
				if overWhole == 1 {
					t.Errorf("shape %v, bound %d: %d bytes with the first conversation whole, over the bound, where a head fits at %d", shape, bound, size, low)
				}
			}
		}
		t.Logf("shape %v: the first is cut up to bound %d; bounds over with the first whole: %d", shape, low, overWhole)
	}
}

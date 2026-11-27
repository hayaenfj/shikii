package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// auditOwnDeliveries gives the turn of session as many deliveries of its own as its subagents received: every quote delivered to
// a subagent of that turn is saved again as a delivery to the conversation's own agent right before a tool call. A lesson delivered
// to a subagent is assessed by the subagent, so only these are listed for the conversation's reflection.
func auditOwnDeliveries(t *testing.T, st store.Store, scope, session string) {
	t.Helper()
	turn := loadSession(st.Dir, session).TurnID
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range deliveries {
		if d.Agent == "" || d.Parent != turn {
			continue
		}
		for _, q := range d.Quotes {
			n++
			own := store.MemoryDelivery{ID: fmt.Sprintf("%016x", 0xa0000+n), Scope: scope, Request: d.Request, Quotes: []store.MemoryQuote{q}, Parent: turn, Tool: fmt.Sprintf("Bash step-%d", n), Phrase: "go test"}
			if err := st.SaveMemoryDelivery(own); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// auditGHeavyView builds a store whose first pending conversation has many deliveries of its own to assess (auditOwnDeliveries); each is an always rule that
// carries a correction (so each delivery must be answered with correction_result). It returns the view with that conversation
// first, the handler, and the number of its deliveries.
func auditGHeavyView(t *testing.T, subagents int) (reflectView, func(map[string]any) map[string]any, int) {
	t.Helper()
	st, scope, h, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Kind, always.Scene, always.AppliesTo = "", "Visible text in Japanese", "Write every visible sentence, reports included, in Japanese.", "rule", "\u3044\u3064\u3082", "every request"
	always.Correction = &store.Correction{SourceID: draft.Sources[0], Quote: "\u307e\u3060\u82f1\u8a9e\u304c\u6b8b\u3063\u3066\u3044\u307e\u3059", Action: strings.Repeat("Write the report in Japanese. ", 8)}
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always); err != nil {
		t.Fatal(err)
	}
	corrected := draft // the lesson the subagents' tasks reach, now with a correction
	corrected.Correction = always.Correction
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(corrected); err != nil {
		t.Fatal(err)
	}
	const prompt = "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": prompt})
	for i := 0; i < subagents; i++ {
		startSubagent(t, "s1", scope, fmt.Sprintf("agent-%d", i), "general-purpose", prompt)
	}
	auditOwnDeliveries(t, st, scope, "s1")
	memoryEffectComplete(t, st, scope, "s1")
	for i := 0; i < 4; i++ {
		session := fmt.Sprintf("p%d", i)
		memoryEffectPrompt(t, scope, session)
		memoryEffectComplete(t, st, scope, session)
	}
	view, err := reflectQueryWith(st.Dir, scope, reflectHeadsMax)
	if err != nil {
		t.Fatal(err)
	}
	heavy := -1
	for i, s := range view.Sources {
		if len(s.Memories) >= subagents {
			heavy = i
		}
	}
	if heavy < 0 {
		t.Fatalf("precondition: the conversation with the deliveries of %d subagents saved as its own is pending", subagents)
	}
	first := view
	first.Sources = append([]reflectSource{}, view.Sources[heavy:]...)
	call := func(body map[string]any) map[string]any { return reflectCall(t, h, body) }
	return first, call, len(view.Sources[heavy].Memories)
}

// What submit takes from a reply whose deliveries are bare: one memory item per delivery listed, with correction_result where the
// delivery is a correction. The submit is complete and nothing is left of the conversations listed.
func TestAuditGSubmitFromBareDeliveries(t *testing.T) {
	view, call, deliveries := auditGHeavyView(t, 60)
	raw, _ := json.Marshal(limitReflectQuery(view, cliPrepareBytes, func(v any) int { return workContextSize(v) + 1 }))
	var reply map[string]any
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}
	output, _ := reply["output"].(map[string]any)
	listed, _ := reply["sources"].([]any)
	t.Logf("CLI bound: %d bytes, %d listed, more_pending=%v, omitted=%v", len(raw)+1, len(listed), reply["more_pending"], output["omitted"])
	if !strings.Contains(fmt.Sprint(output["omitted"]), "texts of the delivered lessons") {
		t.Fatal("precondition: the deliveries come bare at the CLI's bound")
	}
	memory := []map[string]string{}
	reviews := []map[string]string{}
	corrections := 0
	for _, x := range listed {
		src := x.(map[string]any)
		for _, tg := range src["targets"].([]any) {
			m := tg.(map[string]any)
			reviews = append(reviews, map[string]string{"source_id": src["source_material"].(map[string]any)["id"].(string), "trace_id": m["trace_id"].(string), "truth": "unknown", "reason": "Not checked in this audit."})
		}
		for _, mm := range src["memories"].([]any) {
			m := mm.(map[string]any)
			q := m["quote"].(map[string]any)
			item := map[string]string{"source_id": m["source_id"].(string), "key": q["key"].(string), "result": "unknown", "decision": "No decision depended on it.", "evidence": "No check result names it."}
			if q["correction"] != nil {
				item["correction_result"] = "unknown"
				corrections++
			}
			memory = append(memory, item)
		}
	}
	got := call(map[string]any{"operation": "submit", "reviews": reviews, "memory": memory})
	t.Logf("submit of %d deliveries (%d corrections, the first conversation had %d): complete=%v errors=%.300s left=%.300s", len(memory), corrections, deliveries, got["complete"], auditJSON(got["errors"]), auditJSON(got["left"]))
	if got["complete"] != true {
		t.Errorf("a submit built from bare deliveries is not complete: %.600s", auditJSON(got))
	}
	if corrections == 0 {
		t.Errorf("precondition: the deliveries carry their corrections")
	}
}

// auditGStripCorrections returns the size of a reply whose delivered corrections keep only their mark (correction: {}).
func auditGStripCorrections(reply map[string]any) int {
	raw, _ := json.Marshal(reply)
	var copyOf map[string]any
	_ = json.Unmarshal(raw, &copyOf)
	for _, x := range copyOf["sources"].([]any) {
		for _, mm := range x.(map[string]any)["memories"].([]any) {
			q := mm.(map[string]any)["quote"].(map[string]any)
			if q["correction"] != nil {
				q["correction"] = map[string]any{}
			}
		}
	}
	return workContextSize(copyOf) + 1
}

// RED (INVARIANT 5: the CLI's reflect query within 28672 bytes; bare deliveries keep "the mark of a correction"). The finding of
// audit F (a turn that started 60 subagents) with a lesson that carries a person's correction, as the rules shikii makes from
// corrections do: the bare form keeps the correction's quote and action (200 bytes each) in every delivery, and the reply is over
// the CLI's bound. With the mark alone it fits.
func TestAuditGReflectCLIWithCorrectedSubagentDeliveries(t *testing.T) {
	view, _, deliveries := auditGHeavyView(t, 60)
	raw, _ := json.Marshal(limitReflectQuery(view, cliPrepareBytes, func(v any) int { return workContextSize(v) + 1 }))
	var reply map[string]any
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}
	listed, _ := reply["sources"].([]any)
	output, _ := reply["output"].(map[string]any)
	marks := auditGStripCorrections(reply)
	t.Logf("the CLI's reflect query: %d bytes of %d, %d listed, the first with %d deliveries that carry a correction; with the mark of each correction alone: %d bytes\n   omitted=%v", len(raw)+1, cliPrepareBytes, len(listed), deliveries, marks, output["omitted"])
	if len(raw)+1 > cliPrepareBytes && marks <= cliPrepareBytes {
		t.Errorf("INVARIANT 5: the reply of reflect query on the CLI is %d bytes, over %d; the bare deliveries keep the texts of their corrections, and with the mark alone the reply is %d bytes", len(raw)+1, cliPrepareBytes, marks)
	}
}

// RED (the reason limitReflectQuery gives for cutting the first conversation: "its raw text cannot come in any reply of this
// size"). The first conversation has a request of 3 KB and many delivered lessons. It is over the bound alone only with the texts
// of its deliveries, which the next step takes off; it is cut to a head all the same, and output says that it alone is over.
func TestAuditGFirstConversationCutForTheTextsOfItsDeliveries(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	request := "first request " + strings.Repeat("Keep the filters of the CSV export. ", 85)
	mem := func(i int) store.MemoryObservation {
		return store.MemoryObservation{SourceID: fmt.Sprintf("d%015x", i), Parent: "0000000000000000", Agent: fmt.Sprintf("agent-%d", i), Request: long(300),
			Quote: store.MemoryQuote{Key: fmt.Sprintf("lesson:%016x:%016x", i, i), ID: fmt.Sprintf("%016x", i), Revision: fmt.Sprintf("%016x", i), Lesson: long(200), AppliesTo: long(200), Rationale: long(200)}}
	}
	first := reflectSource{Source: store.Feedback{ID: "0000000000000000", Request: request, Summary: "Done.", Reply: "Thanks."},
		Targets: []reviewTarget{{Spec: "done-as-asked", TraceID: "1000000000000000", Question: "Was it done?", OriginalInputs: map[string]string{"request": request}}}}
	for i := 0; i < 41; i++ {
		first.Memories = append(first.Memories, mem(i))
	}
	second := reflectSource{Source: store.Feedback{ID: "0000000000000001", Request: "second " + long(400)}, Targets: []reviewTarget{{Spec: "done-as-asked", TraceID: "1000000000000001", Question: "Was it done?"}}, Memories: []store.MemoryObservation{}}
	view := reflectView{HowToSubmit: "submit", Sources: []reflectSource{first, second}}
	bound := 20000
	raw, _ := json.Marshal(limitReflectQuery(view, bound, workContextSize))
	var reply map[string]any
	_ = json.Unmarshal(raw, &reply)
	output, _ := reply["output"].(map[string]any)
	listed := reply["sources"].([]any)
	got := listed[0].(map[string]any)["source_material"].(map[string]any)["request"].(string)
	// the reply the steps would give with the first conversation whole: every delivery bare, the second a head
	whole := view
	whole.Sources = []reflectSource{reflectBare(first), reflectBare(reflectHead(second))}
	keep := workContextSize(whole) + 1000 // room for output
	t.Logf("bound %d: %d bytes, the first request %d of %d bytes, omitted=%v; with the first conversation whole and the deliveries bare about %d bytes", bound, len(raw), len(got), len(request), output["omitted"], keep)
	if len(got) < len(request) && keep <= bound {
		t.Errorf("the raw text of the first conversation is cut (%d of %d bytes) and output says it alone is over max_bytes, yet a reply with it whole and the deliveries bare is about %d bytes of %d", len(got), len(request), keep, bound)
	}
}

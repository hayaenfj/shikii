package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// audit E: the oldest pending conversation had a long request (a pasted log), six short ones follow. What reflect query lists on
// the CLI and through MCP, and whether what it lists can be submitted as it came.
func TestAuditEReflectQueryWithTheLongConversationFirst(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	// settle what the fixture left pending, so the long conversation is the oldest
	long := "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044\u3002\n" + strings.Repeat("2026-11-12 12:00:00 install step finished without a record\n", (30<<10)/60)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s-long", "cwd": scope, "prompt": long})
	memoryEffectComplete(t, st, scope, "s-long")
	for i := 0; i < 6; i++ {
		session := fmt.Sprintf("s%d", i)
		memoryEffectPrompt(t, scope, session)
		memoryEffectComplete(t, st, scope, session)
	}
	view, err := reflectQuery(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	at := -1
	for i, s := range view.Sources {
		t.Logf("pending %d: %6d bytes whole, %5d as a head, head=%v, request %d bytes, targets=%d memories=%d", i, workContextSize(s), workContextSize(reflectHead(s)), s.Head, len(s.Source.Request), len(s.Targets), len(s.Memories))
		if len(s.Source.Request) > 1000 || workContextSize(s) > 20000 {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("precondition: the long conversation is pending")
	}
	// put the long conversation first, as it is once the ones before it are settled
	first := view
	first.Sources = append([]reflectSource{view.Sources[at]}, append(append([]reflectSource{}, view.Sources[:at]...), view.Sources[at+1:]...)...)
	total := len(first.Sources) + first.MorePending
	for _, c := range []struct {
		name   string
		budget int
	}{{"CLI", cliPrepareBytes}, {"MCP", 64 << 10}} {
		raw, _ := json.Marshal(limitReflectQuery(first, c.budget, func(v any) int { return workContextSize(v) + 1 }))
		var reply map[string]any
		_ = json.Unmarshal(raw, &reply)
		listed, _ := reply["sources"].([]any)
		more, _ := reply["more_pending"].(float64)
		output, _ := reply["output"].(map[string]any)
		head := listed[0].(map[string]any)["head"] == true
		t.Logf("%s: %d bytes of %d, %d of %d listed, more_pending=%v, first is a head=%v, omitted=%v", c.name, len(raw)+1, c.budget, len(listed), total, more, head, output["omitted"])
		if len(raw)+1 > c.budget {
			t.Errorf("%s: over the bound", c.name)
		}
		if len(listed)+int(more) != total {
			t.Errorf("%s: listed %d + more_pending %v is not the %d pending", c.name, len(listed), more, total)
		}
		if head && len(listed) == 1 && len(raw)+1 < c.budget/2 {
			t.Errorf("%s: the first conversation came as a head, and every other conversation waits although the reply uses %d of %d bytes: the heads of the other %d (about %d bytes each) would fit", c.name, len(raw)+1, c.budget, total-1, workContextSize(reflectHead(first.Sources[1])))
		}
		// what is listed can be submitted: the target of the head keeps spec, trace_id and question
		for _, rawTarget := range listed[0].(map[string]any)["targets"].([]any) {
			target := rawTarget.(map[string]any)
			if target["trace_id"] == "" || target["spec"] == "" || target["question"] == "" {
				t.Errorf("%s: a target of the first conversation lost what submit needs: %v", c.name, target)
			}
		}
	}
	_ = h
}

// audit E: every budget over a synthetic view. The reply is within the bound or says that it is not; what is listed plus what
// waits is everything pending; a smaller bound never lists a conversation, a lesson or a raw text a larger bound left out.
func TestAuditEReflectBoundInvariantsOverEveryBudget(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	source := func(i int, request int) reflectSource {
		return reflectSource{Source: store.Feedback{ID: fmt.Sprintf("%016x", i), Request: fmt.Sprintf("request %d ", i) + long(request), Reply: long(300)},
			Targets:  []reviewTarget{{Spec: "done-as-asked", TraceID: fmt.Sprintf("%016x", 1000+i), Question: "Was the requested work finished?", Files: long(600), OriginalInputs: map[string]string{"request": long(request), "summary": long(400)}}},
			Memories: []store.MemoryObservation{}}
	}
	for _, firstSize := range []int{500, 6000, 40000} {
		view := reflectView{HowToSubmit: "submit reviews, wiki and memory", Corrections: []any{map[string]any{"ai_reason": long(900)}}, Sources: []reflectSource{source(0, firstSize), source(1, 2500)}}
		for i := 2; i < 14; i++ {
			view.Sources = append(view.Sources, reflectHead(source(i, 2500)))
		}
		view.MorePending = 3
		total := len(view.Sources) + view.MorePending
		whole := workContextSize(view)
		prevListed, prevRaw, prevFirstHead := 1<<30, 1<<30, false
		over, steps := 0, 0
		for bound := whole + 10; bound > 600; bound -= 97 {
			raw, _ := json.Marshal(limitReflectQuery(view, bound, workContextSize))
			var reply map[string]any
			if err := json.Unmarshal(raw, &reply); err != nil {
				t.Fatal(err)
			}
			steps++
			listed, _ := reply["sources"].([]any)
			more, _ := reply["more_pending"].(float64)
			omitted := ""
			if output, ok := reply["output"].(map[string]any); ok {
				omitted = fmt.Sprint(output["omitted"])
			}
			if len(raw) > bound {
				over++
				if !strings.Contains(omitted, "still over max_bytes") {
					t.Fatalf("first %d, bound %d: the reply is %d bytes and does not say that it is over", firstSize, bound, len(raw))
				}
				if len(listed) != 1 || listed[0].(map[string]any)["head"] != true {
					t.Fatalf("first %d, bound %d: over the bound while something could still go: %d listed", firstSize, bound, len(listed))
				}
			}
			if len(listed)+int(more) != total {
				t.Fatalf("first %d, bound %d: listed %d + more_pending %v is not the %d pending", firstSize, bound, len(listed), more, total)
			}
			rawTexts := 0
			for i, s := range listed {
				src := s.(map[string]any)
				if src["head"] != true {
					rawTexts++
				}
				targets := src["targets"].([]any)
				if len(targets) != 1 || targets[0].(map[string]any)["trace_id"] != fmt.Sprintf("%016x", 1000+i) || targets[0].(map[string]any)["spec"] != "done-as-asked" {
					t.Fatalf("first %d, bound %d: conversation %d lost its target: %v", firstSize, bound, i, targets)
				}
				if src["source_material"].(map[string]any)["id"] != fmt.Sprintf("%016x", i) {
					t.Fatalf("first %d, bound %d: the conversations are not the oldest first", firstSize, bound)
				}
			}
			// the one bound where more may be listed than at a larger one: the first conversation is over the bound even alone and
			// becomes a head from the start, so the ones that waited behind it are listed again
			firstHead := strings.Contains(omitted, "the raw text of the first conversation")
			if (len(listed) > prevListed || rawTexts > prevRaw) && !(firstHead && !prevFirstHead) {
				t.Fatalf("first %d, bound %d: a smaller bound lists more (%d conversations, %d raw) than a larger one (%d, %d)", firstSize, bound, len(listed), rawTexts, prevListed, prevRaw)
			}
			if prevFirstHead && !firstHead {
				t.Fatalf("first %d, bound %d: the first conversation is whole again at a smaller bound", firstSize, bound)
			}
			prevListed, prevRaw, prevFirstHead = len(listed), rawTexts, firstHead
			if reply["how_to_submit"] != view.HowToSubmit {
				t.Fatalf("first %d, bound %d: how_to_submit is gone", firstSize, bound)
			}
		}
		t.Logf("first conversation %5d bytes: %d bounds from %d down, %d replies over the bound (each says so)", firstSize, steps, whole+10, over)
	}
}

// audit E: a head carries its scoring targets with their texts cut to 200 bytes. files is a list of paths, one per line: what the
// agent that scores "Were the changes within the requested scope?" sees of it.
func TestAuditEHeadCutsTheFileListOfATarget(t *testing.T) {
	files := []string{}
	for i := 0; i < 30; i++ {
		files = append(files, fmt.Sprintf("internal/export/csv/writer_%02d.go", i))
	}
	s := reflectSource{Source: store.Feedback{ID: "0000000000000001", Request: "Rename the CSV writer."}, Targets: []reviewTarget{{Spec: "changes-in-scope", TraceID: "1111111111111111", Question: reviewQuestion("changes-in-scope"), Files: filesInput(files, true)}}}
	head := reflectHead(s)
	raw, _ := json.Marshal(head.Targets[0])
	t.Logf("the target of a head: %s", raw)
	got := head.Targets[0].Files
	if len(got) < len(s.Targets[0].Files) && !strings.Contains(string(raw), "text_status") && !strings.HasSuffix(got, "more") && !strings.Contains(got, "\u2026") {
		t.Errorf("the list of changed files of a head is cut from %d to %d bytes (%d of 30 paths, the last one in the middle: %q) with nothing in the target that says so", len(s.Targets[0].Files), len(got), strings.Count(got, "\n"), got[strings.LastIndex(got, "\n")+1:])
	}
}

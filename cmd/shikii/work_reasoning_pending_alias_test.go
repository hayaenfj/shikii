package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

type frozenP struct {
	repo, root, session string
	book                notebook.Book
	receipt             any
}

func TestReasoningPendingAliasesAreNotReusedAfterResolution(t *testing.T) {
	f := fpFixture(t)
	source, err := f.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "CSV baseline evidence", Content: "A synthetic policy.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.book.Ingest(notebook.Draft{Title: "CSV baseline", Lesson: "Keep the condition.", AppliesTo: "CSV", Kind: "howto", Scene: "synthetic checks", Sources: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	f.receipt = fpAgain(t, f)
	fpGood(t, f, map[string]any{"reasoning": fpReason("start", "", "current")})
	note := func(title string) map[string]any {
		return map[string]any{"title": title, "lesson": "Keep the condition.", "applies_to": "CSV", "kind": "howto", "scene": "synthetic checks", "sources": []string{"current"}}
	}
	first, err := fpCall(t, f, map[string]any{"notes": []any{note("CSV pending A"), note("CSV pending B")}})
	if err != nil {
		t.Fatal(err)
	}
	refs := first["pending"].([]any)
	a, b := loopObject(t, refs[0])["ref"], loopObject(t, refs[1])["ref"]
	fpGood(t, f, map[string]any{"notes": []any{map[string]any{"ref": a, "target": "new"}}})
	third, err := fpCall(t, f, map[string]any{"notes": []any{note("CSV pending C")}})
	if err != nil {
		t.Fatal(err)
	}
	c := loopObject(t, third["pending"].([]any)[0])["ref"]
	if c == b || c == a {
		t.Fatal("a returned placement ref was reused for a different note")
	}
	state := fpState(f)
	if state.Planning.Work.Pending[b.(string)].Title != "CSV pending B" {
		t.Fatal("a new pending note overwrote the original B")
	}
	fpGood(t, f, map[string]any{"notes": []any{map[string]any{"ref": c, "target": "new"}}})
	if state = fpState(f); len(state.Reasoning.RequiredNotes) != 1 {
		t.Fatal("saving C must leave exactly B required")
	}
	fpGood(t, f, map[string]any{"notes": []any{map[string]any{"ref": b, "target": "new"}}})
	final := fpGood(t, f, map[string]any{"reasoning": fpReason("finish", "A, B and C were saved independently.", "current")})
	if !fpComplete(t, final) {
		t.Fatal("the original pending notes did not finish")
	}
}

func fpFixture(t *testing.T) frozenP {
	t.Helper()
	repo, root, _ := planningFixture(t)
	session := "frozen-p"
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": planningTestRequest})
	f := frozenP{repo: repo, root: root, session: session, book: notebook.Book{Root: root, Scope: feedbackScope(repo)}}
	f.receipt = fpAgain(t, f)
	return f
}

func fpAgain(t *testing.T, f frozenP) any {
	t.Helper()
	v, e := workCall(t, f.book, "prepare", map[string]any{"session_id": f.session, "turn_id": loadSession(f.root, f.session).TurnID, "query": "CSV"})
	if e != nil {
		t.Fatal("\u518d\u6e96\u5099\u304c\u6210\u7acb\u3057\u306a\u3044", e)
	}
	return v["receipt"]
}

func fpReason(phase, result string, refs ...string) map[string]any {
	r := map[string]any{"phase": phase, "focus": "\u5408\u6210CSV\u306e\u6761\u4ef6", "assessment": "retain", "reason": "\u4fdd\u5b58\u6e08\u307f\u306e\u6839\u62e0\u3092\u7167\u5408\u3059\u308b", "evidence": refs, "next_action": "\u6b21\u306e\u5408\u6210\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b"}
	if phase == "finish" {
		r["result"] = result
	}
	return r
}

func fpCall(t *testing.T, f frozenP, fields map[string]any) (map[string]any, error) {
	t.Helper()
	fields["receipt"] = f.receipt
	return workCall(t, f.book, "record", fields)
}

func fpGood(t *testing.T, f frozenP, fields map[string]any) map[string]any {
	t.Helper()
	v, e := fpCall(t, f, fields)
	if e != nil || v["complete"] != true {
		t.Fatal("\u5408\u6210\u306e\u4fdd\u5b58\u304c\u6210\u7acb\u3057\u306a\u3044", e)
	}
	return v
}

func fpState(f frozenP) *hookSession { return loadSession(f.root, f.session) }

func fpComplete(t *testing.T, v map[string]any) bool {
	t.Helper()
	return loopObject(t, v["reasoning"])["complete"] == true
}

func TestReasoningPendingAliasesStayWithTheirPreparation(t *testing.T) {
	for attempt := 0; attempt < 16; attempt++ {
		f := fpFixture(t)
		src, e := f.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "CSV\u306e\u571f\u53f0", Content: "\u5408\u6210\u306e\u6761\u4ef6", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.book.Ingest(notebook.Draft{Title: "CSV\u306e\u65e2\u5b58\u624b\u9806", Lesson: "\u6761\u4ef6\u3092\u4fdd\u6301\u3059\u308b", AppliesTo: "CSV", Kind: "howto", Scene: "\u5408\u6210\u78ba\u8a8d", Sources: []string{src.ID}})
		if e != nil {
			t.Fatal(e)
		}
		f.receipt = fpAgain(t, f)
		fpGood(t, f, map[string]any{"reasoning": fpReason("start", "", "current")})
		note := func(title string) map[string]any {
			return map[string]any{"title": title, "lesson": "\u6761\u4ef6\u3092\u4fdd\u6301\u3059\u308b", "applies_to": "CSV", "kind": "howto", "scene": "\u5408\u6210\u78ba\u8a8d", "sources": []string{"current"}}
		}
		first, e := fpCall(t, f, map[string]any{"notes": []any{note("CSV\u306e\u5fc5\u8981\u306a\u8a02\u6b63A"), note("CSV\u306e\u5fc5\u8981\u306a\u8a02\u6b63B")}})
		if e != nil || first["complete"] != false {
			t.Fatal("\u4e8c\u3064\u306e\u914d\u7f6e\u5f85\u3061\u3092\u6e96\u5099\u3067\u304d\u306a\u3044", e)
		}
		pending, ok := first["pending"].([]any)
		if !ok || len(pending) != 2 {
			t.Fatal("\u914d\u7f6e\u5f85\u3061\u304c\u4e8c\u3064\u3067\u306a\u3044")
		}
		oldA := loopObject(t, pending[0])["ref"].(string)
		oldB := loopObject(t, pending[1])["ref"].(string)
		requiredA, requiredB := "", ""
		for ref, item := range fpState(f).Reasoning.RequiredNotes {
			if item.Pending == oldA {
				requiredA = ref
			}
			if item.Pending == oldB {
				requiredB = ref
			}
		}
		if requiredA == "" || requiredB == "" {
			t.Fatal("\u5143\u306e\u7fa9\u52d9\u306b\u5bfe\u5fdc\u4ed8\u3051\u3089\u308c\u306a\u3044")
		}
		f.receipt = fpAgain(t, f)
		second, e := fpCall(t, f, map[string]any{"notes": []any{map[string]any{"ref": requiredB, "target": "new"}}})
		if e != nil || second["complete"] != false {
			t.Fatal("B\u306e\u914d\u7f6e\u5f85\u3061\u518d\u4f5c\u6210\u306b\u5931\u6557", e)
		}
		current := loopObject(t, second["pending"].([]any)[0])["ref"].(string)
		if current != oldA {
			t.Fatal("\u6e96\u5099\u3057\u76f4\u3057\u305f\u5225\u540d\u304c\u5143\u306eA\u3068\u885d\u7a81\u3057\u306a\u3044")
		}
		fixed, e := fpCall(t, f, map[string]any{"notes": []any{map[string]any{"ref": current, "target": "new"}}})
		if e != nil || fixed["complete"] != true {
			t.Fatal("\u65b0\u3057\u3044\u914d\u7f6e\u5f85\u3061\u306e\u89e3\u6c7a\u306b\u5931\u6557", e)
		}
		state := fpState(f)
		_, aRemains := state.Reasoning.RequiredNotes[requiredA]
		_, bRemains := state.Reasoning.RequiredNotes[requiredB]
		pages, e := f.book.Live()
		if e != nil {
			t.Fatal(e)
		}
		aSaved, bSaved := false, false
		for _, p := range pages {
			aSaved = aSaved || p.Title == "CSV\u306e\u5fc5\u8981\u306a\u8a02\u6b63A"
			bSaved = bSaved || p.Title == "CSV\u306e\u5fc5\u8981\u306a\u8a02\u6b63B"
		}
		if !aRemains && bRemains && !aSaved && bSaved {
			t.Logf("\u53cd\u5fa9=%d B\u306e\u8a18\u4e8b\u4fdd\u5b58=true A\u306e\u8a18\u4e8b\u4fdd\u5b58=false A\u306e\u7fa9\u52d9\u6d88\u53bb=true B\u306e\u7fa9\u52d9\u6b8b\u7559=true \u5225\u540d\u885d\u7a81=true", attempt+1)
			var published notebook.Page
			for _, page := range pages {
				if page.Title == "CSV\u306e\u5fc5\u8981\u306a\u8a02\u6b63B" {
					published = page
				}
			}
			read, readErr := workCall(t, f.book, "prepare", map[string]any{"receipt": f.receipt, "query": "lesson:" + published.ID})
			if readErr != nil {
				t.Fatal("B\u306e\u518d\u7de8\u96c6\u306e\u6e96\u5099\u5931\u6557", readErr)
			}
			target := ""
			for _, raw := range read["knowledge"].([]any) {
				k := loopObject(t, raw)
				if k["id"] == published.ID {
					target = k["ref"].(string)
				}
			}
			if target == "" {
				t.Fatal("B\u306e\u53c2\u7167\u304c\u306a\u3044")
			}
			repaired, repairErr := fpCall(t, f, map[string]any{"notes": []any{map[string]any{"ref": requiredB, "target": target, "sources": []string{"current"}, "edits": []any{map[string]any{"field": "lesson", "before": published.Lesson, "after": published.Lesson + " \u518d\u78ba\u8a8d\u3059\u308b\u3002"}}}}})
			if repairErr != nil || repaired["complete"] != true {
				t.Fatal("\u6b8b\u3063\u305fB\u306e\u4fee\u6b63\u304c\u6210\u7acb\u3057\u306a\u3044", repairErr)
			}
			final := fpGood(t, f, map[string]any{"reasoning": fpReason("finish", "\u5225\u540d\u7af6\u5408\u5f8c\u306e\u5408\u6210\u7d42\u4e86", "current")})
			runUnreviewedHook(t, "stop", map[string]any{"session_id": f.session, "cwd": f.repo, "last_assistant_message": "\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u3048\u307e\u3057\u305f\u3002"})
			finalState := fpState(f)
			t.Logf("A\u3092\u4fdd\u5b58\u305b\u305a\u5168\u7fa9\u52d9\u89e3\u6c7a=%t reasoning_complete=%t StopJudged=%t", len(finalState.Reasoning.RequiredNotes) == 0, fpComplete(t, final), finalState.Judged)
			t.Fatal("B\u306e\u914d\u7f6e\u5f85\u3061\u3092\u4fdd\u5b58\u3057\u305f\u3060\u3051\u3067A\u306e\u672a\u4fdd\u5b58\u7fa9\u52d9\u3092\u6d88\u53bb\u3057\u305f")
		}
		if !aRemains || bRemains || aSaved || !bSaved {
			t.Fatal("the returned current placement must resolve only B")
		}
		blocked, err := fpCall(t, f, map[string]any{"reasoning": fpReason("finish", "Only B was saved.", "current")})
		if err != nil || blocked["complete"] != false || fpComplete(t, blocked) {
			t.Fatal("A must remain required before completion", blocked, err)
		}
		aResult, err := fpCall(t, f, map[string]any{"notes": []any{map[string]any{"ref": requiredA, "target": "new"}}})
		if err != nil {
			t.Fatal(err)
		}
		if aResult["complete"] == false {
			next := loopObject(t, aResult["pending"].([]any)[0])["ref"]
			aResult, err = fpCall(t, f, map[string]any{"notes": []any{map[string]any{"ref": next, "target": "new"}}})
		}
		if err != nil || aResult["complete"] != true {
			t.Fatal("the original A must remain recoverable", aResult, err)
		}
		final := fpGood(t, f, map[string]any{"reasoning": fpReason("finish", "Both original notes were saved.", "current")})
		if !fpComplete(t, final) || len(fpState(f).Reasoning.RequiredNotes) != 0 {
			t.Fatal("the completed original work must finish")
		}
	}
	t.Log("16 iterations retained original A, saved B, refused early completion and recovered A")
}

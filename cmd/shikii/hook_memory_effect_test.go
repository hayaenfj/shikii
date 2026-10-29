package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.12 item 1a: the delivery learns from the reflection's assessments, with a fixed rule and no model. A lesson revision found "unused"
// twice and never "helpful" at one door (a command phrase, a file name, or the request) is not delivered through that door again; it
// still comes through other doors, and a new revision of the page starts afresh. Among candidates with the same word score, the one
// found helpful more often comes first. Every delivery records the door it came through (phrase).

// lastToolDelivery is the newest delivery made right before a tool call.
func lastToolDelivery(t *testing.T, st store.Store, scope string) store.MemoryDelivery {
	t.Helper()
	ds, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(ds) - 1; i >= 0; i-- {
		if ds[i].Tool != "" {
			return ds[i]
		}
	}
	t.Fatal("no delivery at a tool call")
	return store.MemoryDelivery{}
}

func assess(t *testing.T, st store.Store, scope string, d store.MemoryDelivery, key, result string) {
	t.Helper()
	if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: d.ID, Key: key, Result: result, Decision: "test", Evidence: "test", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func TestAToolLessonUnusedTwiceAtACommandGoesSilentThereOnly(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	commit := map[string]any{"command": "git commit -m x"}
	for _, session := range []string{"a", "b"} {
		ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Bash", commit))
		if !strings.Contains(ctx, ids["commit"]) {
			t.Fatalf("precondition: delivered in session %s:\n%s", session, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		if d.Phrase != "git commit" {
			t.Fatalf("the delivery records the door it came through: %q", d.Phrase)
		}
		memoryEffectComplete(t, st, scope, session)
		assess(t, st, scope, d, ids["commit"], "unused")
	}
	if out := toolHook(t, scope, "c", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("unused twice and never helpful at git commit: silent there:\n%s", out)
	}
	ctx := feedbackContext(t, toolHook(t, scope, "d", toolPrompt, "Bash", map[string]any{"command": "gh release create v0.12.0 --draft"}))
	if !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("the same lesson still comes through another door (gh release):\n%s", ctx)
	}
	// a new revision of the page starts afresh
	book := notebook.Book{Root: st.Dir, Scope: scope}
	id := strings.Split(ids["commit"], ":")[1]
	page, err := book.Query("lesson:" + id)
	if err != nil || len(page.Lessons) != 1 {
		t.Fatal(err)
	}
	p := page.Lessons[0]
	r, err := book.Ingest(notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson + " Check the output is empty.", AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: p.Scene, Sources: []string{p.Sources[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	ctx = feedbackContext(t, toolHook(t, scope, "e", toolPrompt, "Bash", commit))
	if !strings.Contains(ctx, "lesson:"+r.Page.ID+":"+r.Page.Revision) {
		t.Fatalf("the revised page is tried again at git commit:\n%s", ctx)
	}
}

func TestAToolLessonFoundHelpfulOnceKeepsComing(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	for i, result := range []string{"unused", "helpful", "unused"} {
		ctx := feedbackContext(t, toolHook(t, scope, "s"+string(rune('a'+i)), toolPrompt, "Edit", edit))
		if !strings.Contains(ctx, ids["version"]) {
			t.Fatalf("delivered before edit %d:\n%s", i, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		if d.Phrase != "version.go" {
			t.Fatalf("a file edit records the file's name as the door: %q", d.Phrase)
		}
		memoryEffectComplete(t, st, scope, "s"+string(rune('a'+i)))
		assess(t, st, scope, d, ids["version"], result)
	}
	ctx := feedbackContext(t, toolHook(t, scope, "sd", toolPrompt, "Edit", edit))
	if !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("unused twice but helpful once: still delivered:\n%s", ctx)
	}
}

func TestAtEqualScoreTheLessonFoundHelpfulComesFirst(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	keys := map[string]string{}
	for _, name := range []string{"one", "two"} {
		r, err := book.Ingest(notebook.Draft{Sources: draft.Sources, Title: "Commit rule " + name, Lesson: "Rule " + name + " about committing.", AppliesTo: "when running git commit in shikii", Kind: "rule", Scene: "when committing"})
		if err != nil {
			t.Fatal(err)
		}
		keys[name] = "lesson:" + r.Page.ID + ":" + r.Page.Revision
	}
	commit := map[string]any{"command": "git commit -m x"}
	first := feedbackContext(t, toolHook(t, scope, "p1", toolPrompt, "Bash", commit))
	winner, loser := "one", "two"
	if strings.Contains(first, keys["two"]) {
		winner, loser = "two", "one"
	}
	// silence the winner at git commit, so the loser gets delivered and assessed helpful
	d := lastToolDelivery(t, st, scope)
	memoryEffectComplete(t, st, scope, "p1")
	assess(t, st, scope, d, keys[winner], "unused")
	toolHook(t, scope, "p2", toolPrompt, "Bash", commit)
	d = lastToolDelivery(t, st, scope)
	memoryEffectComplete(t, st, scope, "p2")
	assess(t, st, scope, d, keys[winner], "unused")
	ctx := feedbackContext(t, toolHook(t, scope, "p3", toolPrompt, "Bash", commit))
	if !strings.Contains(ctx, keys[loser]) {
		t.Fatalf("with the winner silent the other arrives:\n%s", ctx)
	}
	d = lastToolDelivery(t, st, scope)
	memoryEffectComplete(t, st, scope, "p3")
	assess(t, st, scope, d, keys[loser], "helpful")
	// a new revision of the winner starts it afresh (no assessments); the loser, found helpful, now comes first
	wid := strings.Split(keys[winner], ":")[1]
	v, err := book.Query("lesson:" + wid)
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(err)
	}
	p := v.Lessons[0]
	if _, err := book.Ingest(notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson + " Revised.", AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: p.Scene, Sources: []string{p.Sources[0].ID}}); err != nil {
		t.Fatal(err)
	}
	ctx = feedbackContext(t, toolHook(t, scope, "p4", toolPrompt, "Bash", commit))
	if !strings.Contains(ctx, keys[loser]) || lessonKeys(ctx) != 1 {
		t.Fatalf("at equal word score the lesson found helpful comes first:\n%s", ctx)
	}
}

func TestARequestLessonUnusedTwiceIsNotQuotedAgain(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	key := "lesson:" + draft.ID + ":" + revision
	for _, session := range []string{"r1", "r2"} {
		ctx := memoryEffectPrompt(t, scope, session)
		if !strings.Contains(ctx, key) {
			t.Fatalf("precondition: quoted before the request in %s:\n%s", session, ctx)
		}
		turn := loadSession(st.Dir, session).TurnID
		ds, err := st.MemoryDeliveries(scope)
		if err != nil {
			t.Fatal(err)
		}
		var d store.MemoryDelivery
		for _, x := range ds {
			if x.ID == turn {
				d = x
			}
		}
		if d.ID == "" || d.Phrase != "request" {
			t.Fatalf("a delivery chosen by the request's words records the door %q (want request): %+v", d.Phrase, d)
		}
		memoryEffectComplete(t, st, scope, session)
		assess(t, st, scope, d, key, "unused")
	}
	if ctx := memoryEffectPrompt(t, scope, "r3"); strings.Contains(ctx, key) {
		t.Fatalf("unused twice at the request door: not quoted again:\n%s", ctx)
	}
}

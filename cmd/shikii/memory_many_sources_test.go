package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.5 check: an update of a page keeps every earlier source, so a long-lived page passes 16 sources (the release how-to had 25).
// A delivery record allows at most 16 sources per quote, so saving it failed and that request got no lesson at all ("memory: a memory
// delivery needs a request, quotes, sources and a version" in hook.log). The quote now carries at most 16 source ids, and the lesson arrives.
func TestLessonWithManySourcesIsStillDelivered(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	sources := append([]string{}, draft.Sources...)
	for i := range 17 {
		o, err := book.Capture(notebook.Observation{Kind: "test", Title: fmt.Sprint("check ", i), Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(int64(i+1), 0)})
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, o.ID)
	}
	d := draft
	d.Sources = sources
	r, err := book.Ingest(d)
	if err != nil || len(r.Page.Sources) <= 16 {
		t.Fatalf("the page has more than 16 sources: %v %d", err, len(r.Page.Sources))
	}
	ctx, err := memoryContext(st.Dir, scope, "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044", &hookSession{TurnID: "dddddddddddddddd"})
	if err != nil || !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a lesson with many sources is still delivered: %v\n%q", err, ctx)
	}
	deliveries, err := (store.Store{Dir: st.Dir}).MemoryDeliveries(scope)
	if err != nil || len(deliveries) == 0 || len(deliveries[len(deliveries)-1].Quotes[0].Sources) != 16 {
		t.Fatalf("the delivery is recorded with 16 source ids: %v %+v", err, deliveries)
	}
}

// A correction's source stays among the 16 source ids of the quote even when it sorts after the 16th (the delivery record requires it).
func TestCorrectionSourceKeptAmongDeliveredSources(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	conv, _ := reviewSeed(t, st, scope, 0x7ffffffffffff000)
	sources := []string{conv}
	// source ids depend on the temporary folder, so capture until 17 of them sort before the conversation's id
	for i, before := 0, 0; before < 17 && i < 400; i++ {
		o, err := book.Capture(notebook.Observation{Kind: "test", Title: fmt.Sprint("check ", i), Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(int64(i+1), 0)})
		if err != nil {
			t.Fatal(err)
		}
		if o.ID < conv {
			sources, before = append(sources, o.ID), before+1
		}
	}
	d := draft
	d.ID, d.Title, d.Sources = "", "README \u306e\u5c0e\u5165\u78ba\u8a8d(\u8a02\u6b63)", sources
	d.Correction = &store.Correction{SourceID: conv, Quote: "\u307e\u3060\u82f1\u8a9e\u304c\u6b8b\u3063\u3066\u3044\u307e\u3059", Action: "\u82f1\u8a9e\u304c\u6b8b\u3063\u3066\u3044\u306a\u3044\u304b\u3092\u78ba\u304b\u3081\u3066\u304b\u3089\u7d42\u3048\u308b"}
	r, err := book.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	at := -1
	for i, s := range r.Page.Sources {
		if s.ID == conv {
			at = i
		}
	}
	if at < 16 {
		t.Fatalf("precondition: the correction's source sorts after the 16th source (at %d)", at)
	}
	ctx, err := memoryContext(st.Dir, scope, "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044", &hookSession{TurnID: "eeeeeeeeeeeeeeee"})
	if err != nil || !strings.Contains(ctx, "\u82f1\u8a9e\u304c\u6b8b\u3063\u3066\u3044\u306a\u3044\u304b") {
		t.Fatalf("the corrected lesson is delivered: %v\n%q", err, ctx)
	}
}

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
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", &hookSession{TurnID: "dddddddddddddddd"})
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
	for i := range 40 {
		o, err := book.Capture(notebook.Observation{Kind: "test", Title: fmt.Sprint("check ", i), Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(int64(i+1), 0)})
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, o.ID)
	}
	d := draft
	d.ID, d.Title, d.Sources = "", "README の導入確認(訂正)", sources
	d.Correction = &store.Correction{SourceID: conv, Quote: "まだ英語が残っています", Action: "英語が残っていないかを確かめてから終える"}
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
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", &hookSession{TurnID: "eeeeeeeeeeeeeeee"})
	if err != nil || !strings.Contains(ctx, "英語が残っていないか") {
		t.Fatalf("the corrected lesson is delivered: %v\n%q", err, ctx)
	}
}

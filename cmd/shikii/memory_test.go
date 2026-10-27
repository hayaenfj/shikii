package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// G3: excluding history applies after candidates are cut to 6. With 6 history pages close to the request, the rule decided in this project (rule) loses its seat
func TestAudit091HistoryFilterBeforeCut(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	for i := 0; i < notebook.RecallCandidates; i++ {
		h := draft
		h.ID, h.Kind, h.Scene = "", "history", "README の導入手順を更新する時"
		h.Title = fmt.Sprintf("README の導入手順を更新した経緯 %d", i)
		h.AppliesTo = "README の導入手順を更新する時 README 導入手順 更新"
		h.Lesson = fmt.Sprintf("README の導入手順の更新の経緯その %d。", i)
		if _, err := book.Ingest(h); err != nil {
			t.Fatal(err)
		}
	}
	recalled, _ := book.Recall("README の導入手順を更新してください")
	kinds := []string{}
	for _, p := range recalled {
		kinds = append(kinds, p.Kind)
	}
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", &hookSession{TurnID: "cccccccccccccccc"})
	if err != nil || !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("choose 2 after removing history pages, and deliver the rule decided in this project. candidate kinds=%v\ndelivered text=%q", kinds, ctx)
	}
}

// 0.9.1: history pages are not delivered automatically (they tell why things came to be; read them when searching). Pages from the old version without a kind are delivered as before
func TestHistoryLessonsAreNotDeliveredAutomatically(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	hist := draft
	hist.ID, hist.Title, hist.Lesson, hist.Kind, hist.Scene = "", "README の導入手順を変えた経緯", "README の導入手順は 2026-10 に書き直した経緯がある。", "history", "README を直す時"
	if _, err := book.Ingest(hist); err != nil {
		t.Fatal(err)
	}
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", &hookSession{TurnID: "bbbbbbbbbbbbbbbb"})
	if err != nil || !strings.Contains(ctx, draft.Lesson) || strings.Contains(ctx, hist.Lesson) {
		t.Fatalf("history pages are not delivered automatically, while rules decided in this project are: %v %q", err, ctx)
	}
	v, err := book.Query("README の導入手順")
	found := false
	for _, p := range v.Lessons {
		found = found || p.Lesson == hist.Lesson
	}
	if err != nil || !found {
		t.Fatalf("history pages can still be read by searching: %v %+v", err, v.Lessons)
	}
}

// Audit (2026-10-23): cut to 2 after removing already delivered versions. Cutting to 2 before removing lets delivered versions take the seats so the 3rd lesson never arrives.
func TestRecallCutsToTwoAfterRemovingAlreadyDeliveredRevisions(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	var keys []string
	for i := 0; i < 2; i++ {
		d := draft
		d.ID, d.Title = "", "README の導入手順を更新してください "+string(rune('A'+i)) // make the request itself the title so it ranks above the original lesson
		d.Lesson = "README の導入手順では確認の手順 " + string(rune('A'+i)) + " も書く。"
		d.AppliesTo = "README の導入手順を更新してください"
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, "lesson:"+r.Page.ID+":"+r.Page.Revision)
	}
	sess := &hookSession{TurnID: "aaaaaaaaaaaaaaaa"}
	for _, k := range keys {
		sess.Memories = append(sess.Memories, memoryDelivery{Key: k, At: time.Now().UTC()})
	}
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", sess)
	if err != nil || !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("after removing the 2 delivered ones, the 3rd arrives: %v %q", err, ctx)
	}
}

// Audit 0.9.1 (test gaps GM7, 8, 10, 11): the "いつも" rules are only rules whose scene is "いつも" (English always, case-insensitive),
// and replaced pages are not returned. A child without a kept task is not given the original text of unorganized conversations either
func TestAlwaysRulesAreOnlyLiveAlwaysRules(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	mk := func(title, kind, scene string, replaces ...string) *notebook.Page {
		d := draft
		d.ID, d.Title, d.Lesson, d.Kind, d.Scene, d.Replaces = "", title, title+"の教訓。", kind, scene, replaces
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		return r.Page
	}
	old := mk("古い決まり", "rule", "いつも")
	mk("新しい決まり", "rule", "いつも", old.ID)
	mk("英語の決まり", "rule", "ALWAYS")
	mk("いつものやり方", "howto", "いつも")
	// a page with broken sources (the source was removed) is not returned (re-audit GI)
	obs, err := book.Capture(notebook.Observation{Kind: "human", Title: "消える原資料", Content: "いつもの決まりの元", Reference: "r", Version: "v1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	broken := draft
	broken.ID, broken.Sources, broken.Title, broken.Lesson, broken.Kind, broken.Scene = "", []string{obs.ID}, "出典の壊れた決まり", "出典の壊れた決まりの教訓。", "rule", "いつも"
	if _, err := book.Ingest(broken); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", "*", obs.ID+"*"))
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", obs.ID+"*"))
	}
	if len(files) == 0 {
		t.Fatal("precondition of this check: the source file is found")
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	got, err := book.AlwaysRules()
	titles := []string{}
	for _, p := range got {
		titles = append(titles, p.Title)
	}
	if err != nil || len(got) != 2 || !strings.Contains(strings.Join(titles, ","), "新しい決まり") || !strings.Contains(strings.Join(titles, ","), "英語の決まり") {
		t.Fatalf("returns only the \"いつも\" rules, without replaced pages: %v %v", err, titles)
	}
	// without "いつも" rules, the original text of unorganized conversations is not returned either, and nothing is delivered
	st2, scope2, _ := reviewFixture(t) // a base without lessons
	reviewSeed(t, st2, scope2, 2)      // conversations not organized yet, close to the request
	if ctx, err := memoryContextFor(st2.Dir, scope2, "README の起動手順を日本語にする 0000000000000002", &hookSession{TurnID: "dddddddddddddddd"}, "", ""); err != nil || !strings.Contains(ctx, "0000000000000002") {
		t.Fatalf("precondition of this check: picking by closeness to the request returns the original text of unorganized conversations: %v %q", err, ctx)
	}
	if ctx, err := alwaysContextFor(st2.Dir, scope2, "README の起動手順を日本語にする 0000000000000002", &hookSession{TurnID: "cccccccccccccccc"}, "p", "a"); err != nil || ctx != "" {
		t.Fatalf("a child without a kept task is not given the original text of unorganized conversations: %v %q", err, ctx)
	}
}

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

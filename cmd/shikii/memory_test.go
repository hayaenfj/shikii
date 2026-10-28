package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
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

// seedPersonAnswer saves one decision of a hook question on request and its truth from source, as the page (ui), a spot check (audit),
// a hand label (manual), a result (outcome) or the machine (judge, context, replay, mcp) writes it. It returns the decision's trace.
func seedPersonAnswer(t *testing.T, st store.Store, specID, request, truth, source string, tainted bool) string {
	t.Helper()
	sp, err := spec.Load(hookSpecPath(st.Dir, specID))
	if err != nil {
		t.Fatal(err)
	}
	trace := store.NewTraceID()
	inputs := map[string]string{"request": request}
	at := time.Now().UTC()
	rec := store.Record{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", Inputs: inputs, InputsHash: store.HashInputs(inputs), Top: "yes", P: .6, Band: "ask", Tainted: tainted, At: at}
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", InputsHash: rec.InputsHash, Inputs: inputs, Truth: truth, Weight: 1, Source: source, Tainted: tainted, At: at}); err != nil {
		t.Fatal(err)
	}
	return trace
}

const (
	pastSetupRequest    = "translate the README setup steps into Japanese and check the install commands"
	similarSetupRequest = "translate the README setup steps into Japanese once more"
	jaShouldHaveAsked   = "\u78ba\u8a8d\u3057\u3066\u307b\u3057\u304b\u3063\u305f"                                     // the page's words for needs-plan "yes"
	jaAskFirstTitle     = "\u59cb\u3081\u308b\u524d\u306b\u78ba\u8a8d\u3057\u3066\u307b\u3057\u304b\u3063\u305f\u304b" // needs-plan's title on the page
)

// 0.11 end condition 6-1: a person's own answer to a hook question reaches the next similar request's hook text, in the page's words
// (English first, the configured language alongside), without any line drawn. The same session does not get it again within the hour.
func TestPersonAnswerReachesSimilarRequest(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	setHookLanguage(t, "Japanese")
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "answers", "cwd": scope, "prompt": similarSetupRequest}))
	for _, want := range []string{"decision:" + trace, "It should have asked", "Should it have asked you first?", jaShouldHaveAsked, jaAskFirstTitle, "README setup steps"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("the person's answer to a similar request reaches the hook text (missing %q):\n%s", want, ctx)
		}
	}
	again := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "answers", "cwd": scope, "prompt": similarSetupRequest + " now"}))
	if strings.Contains(again, "decision:"+trace) {
		t.Fatalf("the same answer is not quoted again in the same session within the hour:\n%s", again)
	}
}

// 0.11 end condition 6-2: a dissimilar request gets no answer, and only the person's own answers (page, spot check, hand label) are
// quoted: truths from results, the agent, re-asking, the judge or the added context never are, nor answers on a tainted decision.
// A truth a result wrote after the person's answer does not hide the person's answer.
func TestOnlyPersonAnswersToSimilarRequestsAreQuoted(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	person := seedPersonAnswer(t, st, "done-as-asked", pastSetupRequest, "no", "manual", false)
	ctx, err := memoryContext(st.Dir, scope, "tune the colour of the status page buttons", &hookSession{TurnID: "aaaaaaaaaaaaaaa1"})
	if err != nil || strings.Contains(ctx, "decision:") {
		t.Fatalf("a dissimilar request gets no answer: %v\n%s", err, ctx)
	}
	others := []string{}
	for _, source := range []string{"outcome", "agreed", "mcp", "replay", "judge", "context", "file", "ask"} {
		others = append(others, seedPersonAnswer(t, st, "changes-in-scope", pastSetupRequest, "no", source, false))
	}
	tainted := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", true)
	// a result's truth after the person's answer on the same decision: the person's answer is still the one quoted
	sp, err := spec.Load(hookSpecPath(st.Dir, "done-as-asked"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(store.Label{TraceID: person, SpecID: sp.ID, SpecHash: sp.Hash(), Model: "test-model", Type: "noul", Truth: "yes", Weight: 1, Source: "outcome", Inputs: map[string]string{"request": pastSetupRequest}, At: time.Now().UTC().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	ctx, err = memoryContext(st.Dir, scope, similarSetupRequest, &hookSession{TurnID: "aaaaaaaaaaaaaaa2"})
	if err != nil || !strings.Contains(ctx, "decision:"+person) || !strings.Contains(ctx, "Not finished") {
		t.Fatalf("the person's own answer to a similar request is quoted, as the person gave it: %v\n%s", err, ctx)
	}
	for _, trace := range append(others, tainted) {
		if strings.Contains(ctx, trace) {
			t.Fatalf("only the person's own answers are quoted (found %s):\n%s", trace, ctx)
		}
	}
	// the person changed the answer later on the page: the later answer is the one quoted
	if err := st.AppendLabel(store.Label{TraceID: person, SpecID: sp.ID, SpecHash: sp.Hash(), Model: "test-model", Type: "noul", Truth: "yes", Weight: 1, Source: "ui", Inputs: map[string]string{"request": pastSetupRequest}, At: time.Now().UTC().Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	ctx, err = memoryContext(st.Dir, scope, similarSetupRequest, &hookSession{TurnID: "aaaaaaaaaaaaaaa3"})
	if err != nil || !strings.Contains(ctx, `\"Finished\"`) || strings.Contains(ctx, "Not finished") {
		t.Fatalf("the person's later answer is the one quoted: %v\n%s", err, ctx)
	}
}

// Seats: the delivery keeps at most 2 quotes. When a person's answer fits, it keeps one of them even if lessons could fill both;
// with no lesson, answers take both seats. A subagent's delivery gets no answers.
func TestPersonAnswerKeepsOneOfTwoSeats(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	first := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	second := seedPersonAnswer(t, st, "done-as-asked", pastSetupRequest+" today", "no", "audit", false)
	page := func(id, lesson string) notebook.Page {
		return notebook.Page{ID: id, Revision: "00000000000000a1", Lesson: lesson, AppliesTo: "when translating the README", Sources: []notebook.Source{{ID: "00000000000000b1"}}}
	}
	pages := []notebook.Page{page("00000000000000c1", "first lesson"), page("00000000000000c2", "second lesson")}
	ctx, err := deliverMemory(st.Dir, scope, similarSetupRequest, pages, true, &hookSession{TurnID: "bbbbbbbbbbbbbbb1"}, "", "")
	if err != nil || !strings.Contains(ctx, "first lesson") || strings.Contains(ctx, "second lesson") || strings.Count(ctx, `"key":"decision:`) != 1 {
		t.Fatalf("lessons first, and one seat kept for the person's answer: %v\n%s", err, ctx)
	}
	ctx, err = deliverMemory(st.Dir, scope, similarSetupRequest, nil, true, &hookSession{TurnID: "bbbbbbbbbbbbbbb2"}, "", "")
	if err != nil || !strings.Contains(ctx, "decision:"+first) || !strings.Contains(ctx, "decision:"+second) {
		t.Fatalf("with no lesson, the person's answers take both seats: %v\n%s", err, ctx)
	}
	// the person answered about the person's own requests, not a child's task
	ctx, err = deliverMemory(st.Dir, scope, similarSetupRequest, pages[:1], true, &hookSession{}, "bbbbbbbbbbbbbbb3", "child")
	if err != nil || strings.Contains(ctx, "decision:") {
		t.Fatalf("a subagent gets no answers: %v\n%s", err, ctx)
	}
}

// 0.11 end condition 6-3: a delivered answer shows in the reflection's memories, can be assessed, and status counts it.
func TestDeliveredPersonAnswerIsAssessedAndCounted(t *testing.T) {
	st, scope, h := reviewFixture(t)
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "assess", "cwd": scope, "prompt": similarSetupRequest}))
	key := "decision:" + trace
	if !strings.Contains(ctx, key) {
		t.Fatalf("precondition: the answer is delivered:\n%s", ctx)
	}
	id := loadSession(st.Dir, "assess").TurnID
	runHook(t, "stop", map[string]any{"session_id": "assess", "cwd": scope, "last_assistant_message": "I asked which install commands to keep before translating."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "assess", "cwd": scope, "prompt": "now adjust the colours of the page"})
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	found := false
	sources, _ := q["sources"].([]any)
	for _, s := range sources {
		memories, _ := s.(map[string]any)["memories"].([]any)
		for _, m := range memories {
			quote, _ := m.(map[string]any)["quote"].(map[string]any)
			found = found || (quote["key"] == key && quote["request"] != nil && quote["said"] != nil)
		}
	}
	if !found {
		t.Fatalf("the delivered answer is in the reflection's memories, with the past request and what the person said: %v", q)
	}
	result := memoryEffectSubmit(t, h, id, key, "helpful", "asked which install commands to keep before starting", "the reply asked first, as the person had wanted")
	receipts, _ := result["memory"].([]any)
	if result["complete"] != true || len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil {
		t.Fatalf("the delivered answer can be assessed: %v", result)
	}
	var b strings.Builder
	printMemoryEffect(&b, st, scope, time.Now())
	if !strings.Contains(b.String(), "memory: 1 quotes delivered; helpful 1") || !strings.Contains(b.String(), "your past answers 1 (helpful 1)") {
		t.Fatalf("status counts the delivered answer:\n%s", b.String())
	}
}

// 0.11: a person's answer reaches the agent with the two answers the person judged between, so the agent learns how its own reasoning
// was judged in this project: the agent's answer with its reason (its organizing before starting, or its provisional score) and the
// decision model's answer. The bare "yes" or "no" delivered before told the agent nothing it could act on (the person, 2026-10-28).
func TestPersonAnswerCarriesTheAgentsReasonAndTheModelsAnswer(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	setHookLanguage(t, "Japanese")
	sp, err := spec.Load(hookSpecPath(st.Dir, "needs-plan"))
	if err != nil {
		t.Fatal(err)
	}
	const reason = "whether the install commands are checked on Windows too"
	trace := store.NewTraceID()
	inputs := map[string]string{"request": pastSetupRequest, "agreed": "translate the setup steps", "open": planningPreface + reason}
	at := time.Now().UTC()
	rec := store.Record{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", Inputs: inputs, InputsHash: store.HashInputs(inputs), Top: "no", P: .3, Band: "ask", AgentPaired: true, At: at}
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", InputsHash: rec.InputsHash, Inputs: inputs, Truth: "yes", Weight: 1, Source: "ui", At: at}); err != nil {
		t.Fatal(err)
	}
	ctx, err := memoryContext(st.Dir, scope, similarSetupRequest, &hookSession{TurnID: "aaaaaaaaaaaaaaa2"})
	if err != nil {
		t.Fatal(err)
	}
	no := strings.TrimSpace(sp.Display["en"].Options["no"])
	noJA := strings.TrimSpace(sp.Display["ja"].Options["no"])
	// the agent had answered yes (open filled) because of its reason; the decision model (p .3) said no; the person said yes
	// the quotes are delivered as JSON, so a quotation mark inside a text is escaped
	for _, want := range []string{"decision:" + trace, "It should have asked", "the agent had answered", `It should have asked\" because: ` + reason, `the decision model said \"` + no + `\"`, jaShouldHaveAsked, "\u7406\u7531: " + reason, noJA} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("the person's answer carries the agent's answer with its reason and the model's answer (missing %q):\n%s", want, ctx)
		}
	}
	// a decision the agent did not answer (not paired) carries only the person's and the model's answers
	bare := seedPersonAnswer(t, st, "done-as-asked", "rename the status page buttons to plain words", "no", "ui", false)
	ctx, err = memoryContext(st.Dir, scope, "rename the status page buttons to plain words again", &hookSession{TurnID: "aaaaaaaaaaaaaaa3"})
	if err != nil || !strings.Contains(ctx, "decision:"+bare) || strings.Contains(ctx, "the agent had answered") || !strings.Contains(ctx, "the decision model said") {
		t.Fatalf("an unpaired decision carries the person's and the model's answers only: %v\n%s", err, ctx)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// assessAnswer records that the quote of the answer trace was delivered at a request and what the reflection said about it.
func assessAnswer(t *testing.T, st store.Store, scope, trace, result string, n int) {
	t.Helper()
	d := store.MemoryDelivery{ID: fmt.Sprintf("%016x", 0x5000+n), Scope: scope, Request: similarSetupRequest,
		Quotes: []store.MemoryQuote{{Key: "decision:" + trace, ID: trace, Request: pastSetupRequest, Said: "In a similar request (above), you answered \"yes\""}}}
	if err := st.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	// the turn is over (the reply and the person's next message are kept), so the delivery can be assessed
	if err := st.AppendFeedback(scope, store.Feedback{ID: d.ID, Request: d.Request, Summary: "translated the steps", Reply: "thanks"}); err != nil {
		t.Fatal(err)
	}
	assess(t, st, scope, d, "decision:"+trace, result)
}

func pageJSON(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != 200 {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// rejectedAnswersOnPage reads the list the knowledge section shows.
func rejectedAnswersOnPage(t *testing.T, h http.Handler) rejectedAnswersView {
	t.Helper()
	var page struct {
		Rejected *rejectedAnswersView `json:"rejected_answers"`
	}
	if err := json.Unmarshal([]byte(pageJSON(t, h, "/v1/wiki")), &page); err != nil || page.Rejected == nil {
		t.Fatalf("the knowledge section carries rejected_answers: %v", err)
	}
	return *page.Rejected
}

// An answer the reflection found unused twice and never helpful is rejected: it is not quoted again (not even when the agent asks
// for similar requests), it leaves the page's list of answered decisions, and the knowledge section lists it with the request and
// the answer in the page's words. Its truth stays in the labels.
func TestAnAnswerTheReflectionRejectedIsNeverQuotedAgain(t *testing.T) {
	st, scope, h := reviewFixture(t)
	rejected := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	kept := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest+" today", "no", "ui", false)
	quoted := func(turn string) string {
		t.Helper()
		ctx, err := deliverMemory(st.Dir, scope, similarSetupRequest, nil, true, &hookSession{TurnID: turn}, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
	if ctx := quoted("cccccccccccccc01"); !strings.Contains(ctx, "decision:"+rejected) || !strings.Contains(ctx, "decision:"+kept) {
		t.Fatalf("precondition: both answers are quoted for a similar request:\n%s", ctx)
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); !strings.Contains(answered, rejected) || !strings.Contains(answered, kept) {
		t.Fatalf("precondition: both answers are in the list of answered decisions:\n%s", answered)
	}
	assessAnswer(t, st, scope, rejected, "unused", 1)
	if ctx := quoted("cccccccccccccc02"); !strings.Contains(ctx, "decision:"+rejected) {
		t.Fatalf("unused once is not a rejection:\n%s", ctx)
	}
	assessAnswer(t, st, scope, rejected, "unused", 2)

	if ctx := quoted("cccccccccccccc03"); strings.Contains(ctx, "decision:"+rejected) || !strings.Contains(ctx, "decision:"+kept) {
		t.Fatalf("the rejected answer is not quoted again, the other one still is:\n%s", ctx)
	}
	similar, _, err := decideKnowledge(st.Dir, scope, similarSetupRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range similar {
		if q.ID == rejected {
			t.Fatalf("the rejected answer does not come back when the agent asks for similar requests: %+v", similar)
		}
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); strings.Contains(answered, rejected) || !strings.Contains(answered, kept) {
		t.Fatalf("the rejected answer leaves the list of answered decisions, the other one stays:\n%s", answered)
	}
	view := rejectedAnswersOnPage(t, h)
	if view.Stopped || len(view.Items) != 1 {
		t.Fatalf("the knowledge section lists the one rejected answer and nothing else: %+v", view)
	}
	item := view.Items[0]
	if item.TraceID != rejected || item.Unused != 2 || !strings.Contains(item.Request, "README setup steps") ||
		item.Answer["en"] != "It should have asked" || item.Answer["ja"] != jaShouldHaveAsked || item.Question["ja"] != jaAskFirstTitle {
		t.Fatalf("the rejected answer is listed with its request and the question and answer in the page's words: %+v", item)
	}
	labeled, err := st.LabeledTraces("needs-plan")
	if err != nil || labeled[rejected].Truth != "yes" {
		t.Fatalf("the rejected answer stays a truth for its decision: %+v %v", labeled[rejected], err)
	}
}

// One helpful assessment keeps an answer, however often it was unused.
func TestAnAnswerThatHelpedOnceIsNotRejected(t *testing.T) {
	st, scope, h := reviewFixture(t)
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	for n, result := range []string{"unused", "helpful", "unused", "unused"} {
		assessAnswer(t, st, scope, trace, result, n)
	}
	ctx, err := deliverMemory(st.Dir, scope, similarSetupRequest, nil, true, &hookSession{TurnID: "dddddddddddddd01"}, "", "")
	if err != nil || !strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("an answer that helped once is still quoted: %v\n%s", err, ctx)
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); !strings.Contains(answered, trace) {
		t.Fatalf("and stays in the list of answered decisions:\n%s", answered)
	}
}

// Quoting past answers stops in a project once ten of them were assessed unused and none ever helped: the seat goes back to the
// lessons. One below the bar they are still quoted, and one helpful assessment keeps them, whatever the count.
func TestQuotingPastAnswersStopsWhenNoneEverHelped(t *testing.T) {
	st, scope, h := reviewFixture(t)
	answer := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	page := func(id, lesson string) notebook.Page {
		return notebook.Page{ID: id, Revision: "00000000000000a1", Lesson: lesson, AppliesTo: "when translating the README", Sources: []notebook.Source{{ID: "00000000000000b1"}}}
	}
	pages := []notebook.Page{page("00000000000000c1", "first lesson"), page("00000000000000c2", "second lesson")}
	deliver := func(turn string) string {
		t.Helper()
		ctx, err := deliverMemory(st.Dir, scope, similarSetupRequest, pages, true, &hookSession{TurnID: turn}, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
	for n := 0; n < answerKindRejectUnused-1; n++ { // other answers, each unused once: none is rejected on its own
		assessAnswer(t, st, scope, fmt.Sprintf("%016x", 0x9000+n), "unused", n)
	}
	if ctx := deliver("eeeeeeeeeeeeee01"); !strings.Contains(ctx, "decision:"+answer) || strings.Contains(ctx, "second lesson") {
		t.Fatalf("one below the bar an answer still keeps a seat:\n%s", ctx)
	}
	assessAnswer(t, st, scope, fmt.Sprintf("%016x", 0x9000+answerKindRejectUnused), "unused", answerKindRejectUnused)
	if ctx := deliver("eeeeeeeeeeeeee02"); strings.Contains(ctx, `"key":"decision:`) || !strings.Contains(ctx, "first lesson") || !strings.Contains(ctx, "second lesson") {
		t.Fatalf("ten unused and none helpful: no answer is quoted and both seats go to lessons:\n%s", ctx)
	}
	if view := rejectedAnswersOnPage(t, h); !view.Stopped || view.Helpful != 0 || view.Unused != answerKindRejectUnused {
		t.Fatalf("the knowledge section says quoting past answers is stopped, with the counts: %+v", view)
	}
	assessAnswer(t, st, scope, fmt.Sprintf("%016x", 0x9100), "helpful", 100)
	if ctx := deliver("eeeeeeeeeeeeee03"); !strings.Contains(ctx, "decision:"+answer) {
		t.Fatalf("one helpful assessment keeps the answers:\n%s", ctx)
	}
}

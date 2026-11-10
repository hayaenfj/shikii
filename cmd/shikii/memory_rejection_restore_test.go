package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// restoreAnswers sends what the page's undo sends: the person undoes the AI's rejection of one answer or its stop of quoting.
func restoreAnswers(t *testing.T, h http.Handler, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/answers/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func mustRestore(t *testing.T, h http.Handler, body string) {
	t.Helper()
	if code, out := restoreAnswers(t, h, body); code != 200 {
		t.Fatalf("POST /v1/answers/restore %s: %d %s", body, code, out)
	}
}

func listedTraces(view rejectedAnswersView) []string {
	out := []string{}
	for _, item := range view.Items {
		out = append(out, item.TraceID)
	}
	return out
}

// The person undoes the AI's rejection of one answer: it is quoted again, it is back in the list of answered decisions and it
// leaves the list of rejected answers. The assessments before the undo no longer count, so one later unused assessment does not
// reject it and a second one does. Another rejected answer stays rejected, and no truth changes.
func TestUndoingARejectionQuotesTheAnswerAgainAndCountsOnlyLaterAssessments(t *testing.T) {
	st, scope, h := reviewFixture(t)
	restored := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	other := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest+" today", "no", "ui", false)
	for n, trace := range []string{restored, restored, other, other} {
		assessAnswer(t, st, scope, trace, "unused", n+1)
	}
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff01"); strings.Contains(ctx, "decision:") {
		t.Fatalf("precondition: both answers are rejected and not quoted:\n%s", cut(ctx, 400))
	}
	if view := rejectedAnswersOnPage(t, h); len(view.Items) != 2 {
		t.Fatalf("precondition: the page lists both rejected answers: %+v", view)
	}
	labels, err := st.LabeledTraces("needs-plan")
	if err != nil {
		t.Fatal(err)
	}

	mustRestore(t, h, fmt.Sprintf(`{"trace_id":%q}`, restored))

	if ctx := quotedBefore(t, st, scope, "ffffffffffffff02"); !strings.Contains(ctx, "decision:"+restored) || strings.Contains(ctx, "decision:"+other) {
		t.Fatalf("the restored answer is quoted again, the other rejected one is not:\n%s", cut(ctx, 600))
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); !strings.Contains(answered, restored) || strings.Contains(answered, other) {
		t.Fatalf("the restored answer is back in the list of answered decisions, the other one is not:\n%s", cut(answered, 400))
	}
	if listed := listedTraces(rejectedAnswersOnPage(t, h)); !reflect.DeepEqual(listed, []string{other}) {
		t.Fatalf("the restored answer leaves the list of rejected answers, the other one stays: %v", listed)
	}
	if after, err := st.LabeledTraces("needs-plan"); err != nil || !reflect.DeepEqual(after, labels) {
		t.Fatalf("an undo changes no truth: before %+v after %+v (%v)", labels, after, err)
	}

	assessAnswer(t, st, scope, restored, "unused", 5)
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff03"); !strings.Contains(ctx, "decision:"+restored) {
		t.Fatalf("one unused assessment after the undo is not a rejection (the two before it no longer count):\n%s", cut(ctx, 600))
	}
	assessAnswer(t, st, scope, restored, "unused", 6)
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff04"); strings.Contains(ctx, "decision:"+restored) {
		t.Fatalf("two unused assessments after the undo reject the answer again:\n%s", cut(ctx, 600))
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); strings.Contains(answered, restored) {
		t.Fatalf("rejected again, the answer leaves the list of answered decisions again:\n%s", cut(answered, 400))
	}
	view := rejectedAnswersOnPage(t, h)
	for _, item := range view.Items {
		if item.TraceID == restored && item.Unused != 2 {
			t.Fatalf("rejected again, the page counts only the assessments after the undo: %+v", item)
		}
	}
	if len(view.Items) != 2 {
		t.Fatalf("rejected again, the page lists both answers: %+v", view.Items)
	}
}

// An answer asked again sits under two decisions. Undoing it under either one restores it under both, and the assessments after
// the undo under both records count together again.
func TestUndoingARejectionOfAnAnswerAskedAgainRestoresEveryCopy(t *testing.T) {
	st, scope, h := reviewFixture(t)
	original := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st, scope, original, "unused", 1)
	assessAnswer(t, st, scope, original, "unused", 2)
	again := askAgain(t, st, "needs-plan", original)
	if listed := listedTraces(rejectedAnswersOnPage(t, h)); !reflect.DeepEqual(listed, []string{again}) {
		t.Fatalf("precondition: the page lists the answer under its newest record %s: %v", again, listed)
	}

	mustRestore(t, h, fmt.Sprintf(`{"trace_id":%q}`, original)) // the original, not the record the page lists

	if ctx := quotedBefore(t, st, scope, "ffffffffffffff11"); !strings.Contains(ctx, "decision:"+again) {
		t.Fatalf("the answer is quoted again under its newest record %s:\n%s", again, cut(ctx, 600))
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); !strings.Contains(answered, again) {
		t.Fatalf("the answer is back in the list of answered decisions under %s:\n%s", again, cut(answered, 400))
	}
	if view := rejectedAnswersOnPage(t, h); len(view.Items) != 0 {
		t.Fatalf("no copy of the restored answer stays in the list of rejected answers: %+v", view.Items)
	}
	assessAnswer(t, st, scope, again, "unused", 3)
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff12"); !strings.Contains(ctx, "decision:"+again) {
		t.Fatalf("one unused assessment after the undo is not a rejection:\n%s", cut(ctx, 600))
	}
	assessAnswer(t, st, scope, original, "unused", 4)
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff13"); strings.Contains(ctx, "decision:") {
		t.Fatalf("unused once under the copy and once under the original after the undo: rejected again:\n%s", cut(ctx, 600))
	}
}

// The person undoes the stop of quoting past answers: quoting resumes, while an answer rejected on its own stays rejected. Only
// the assessments after the undo count toward the next stop: nine unused ones do not stop it, the tenth does.
func TestUndoingTheStopQuotesPastAnswersAgainUntilTenMoreGoUnused(t *testing.T) {
	st, scope, h := reviewFixture(t)
	answer := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	rejected := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest+" today", "no", "ui", false)
	assessAnswer(t, st, scope, rejected, "unused", 0)
	assessAnswer(t, st, scope, rejected, "unused", 1)
	for n := 2; n < answerKindRejectUnused; n++ {
		assessAnswer(t, st, scope, fmt.Sprintf("%016x", 0x9000+n), "unused", n)
	}
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff21"); strings.Contains(ctx, "decision:") {
		t.Fatalf("precondition: ten unused and none helpful stop quoting:\n%s", cut(ctx, 400))
	}
	if view := rejectedAnswersOnPage(t, h); !view.Stopped {
		t.Fatalf("precondition: the page says quoting is stopped: %+v", view)
	}

	mustRestore(t, h, `{"stop":true}`)

	if ctx := quotedBefore(t, st, scope, "ffffffffffffff22"); !strings.Contains(ctx, "decision:"+answer) || strings.Contains(ctx, "decision:"+rejected) {
		t.Fatalf("quoting resumes, and the answer rejected on its own stays rejected:\n%s", cut(ctx, 600))
	}
	view := rejectedAnswersOnPage(t, h)
	if view.Stopped || view.Unused != 0 || !reflect.DeepEqual(listedTraces(view), []string{rejected}) {
		t.Fatalf("the page no longer says stopped, counts from the undo and still lists the rejected answer: %+v", view)
	}
	for n := 0; n < answerKindRejectUnused-1; n++ {
		assessAnswer(t, st, scope, fmt.Sprintf("%016x", 0x9100+n), "unused", 100+n)
	}
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff23"); !strings.Contains(ctx, "decision:"+answer) {
		t.Fatalf("nine unused assessments after the undo do not stop quoting:\n%s", cut(ctx, 600))
	}
	assessAnswer(t, st, scope, fmt.Sprintf("%016x", 0x9100+answerKindRejectUnused), "unused", 200)
	if ctx := quotedBefore(t, st, scope, "ffffffffffffff24"); strings.Contains(ctx, "decision:") {
		t.Fatalf("ten unused assessments after the undo stop quoting again:\n%s", cut(ctx, 600))
	}
	if view := rejectedAnswersOnPage(t, h); !view.Stopped || view.Unused != answerKindRejectUnused {
		t.Fatalf("the page says stopped again with the count after the undo: %+v", view)
	}
}

// The undo takes a JSON POST from the page like the other writes, and refuses what it cannot undo with the same error shape:
// another method, another content type, another origin, broken JSON, no target or two, an answer it does not know, an answer that
// is not rejected and a stop that is not in place. A refusal writes nothing.
func TestRestoringAnswersRefusesWhatItCannotUndo(t *testing.T) {
	st, _, h := reviewFixture(t)
	notRejected := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	send := func(method, contentType, origin, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/v1/answers/restore", strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, c := range []struct {
		name, method, contentType, origin, body string
		code                                    int
	}{
		{"GET", http.MethodGet, "", "", "", 405},
		{"plain text", http.MethodPost, "text/plain", "", `{"stop":true}`, 415},
		{"another origin", http.MethodPost, "application/json", "https://example.com", `{"stop":true}`, 403},
		{"broken JSON", http.MethodPost, "application/json", "", `{"trace_id":`, 400},
		{"no target", http.MethodPost, "application/json", "", `{}`, 400},
		{"two targets", http.MethodPost, "application/json", "", fmt.Sprintf(`{"trace_id":%q,"stop":true}`, notRejected), 400},
		{"unknown answer", http.MethodPost, "application/json", "", `{"trace_id":"0123456789abcdef"}`, 404},
		{"not rejected", http.MethodPost, "application/json", "", fmt.Sprintf(`{"trace_id":%q}`, notRejected), 409},
		{"not stopped", http.MethodPost, "application/json", "", `{"stop":true}`, 409},
	} {
		code, body := send(c.method, c.contentType, c.origin, c.body)
		if code != c.code || !strings.Contains(body, `"error"`) {
			t.Errorf("%s: want %d with an error, got %d %s", c.name, c.code, code, body)
		}
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "records", "answer-restores")); !os.IsNotExist(err) {
		t.Errorf("a refused undo writes nothing: %v", err)
	}
}

// An undo is kept in its own project: the same answer rejected in another project stays rejected there, and an answer of the
// other project cannot be undone from this one.
func TestUndoInOneProjectDoesNotRestoreAnother(t *testing.T) {
	st, scope, h := reviewFixture(t)
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st, scope, trace, "unused", 1)
	assessAnswer(t, st, scope, trace, "unused", 2)
	st2, scope2, h2 := reviewFixture(t)
	trace2 := seedPersonAnswer(t, st2, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st2, scope2, trace2, "unused", 1)
	assessAnswer(t, st2, scope2, trace2, "unused", 2)

	mustRestore(t, h, fmt.Sprintf(`{"trace_id":%q}`, trace))
	if code, body := restoreAnswers(t, h, fmt.Sprintf(`{"trace_id":%q}`, trace2)); code != 404 {
		t.Fatalf("an answer of another project is not known here: %d %s", code, body)
	}

	if ctx := quotedBefore(t, st, scope, "ffffffffffffff31"); !strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("the undo works in its own project:\n%s", cut(ctx, 600))
	}
	if ctx := quotedBefore(t, st2, scope2, "ffffffffffffff32"); strings.Contains(ctx, "decision:") {
		t.Fatalf("the other project's rejection stays:\n%s", cut(ctx, 600))
	}
	if listed := listedTraces(rejectedAnswersOnPage(t, h2)); !reflect.DeepEqual(listed, []string{trace2}) {
		t.Fatalf("the other project still lists its rejected answer: %v", listed)
	}
}

// deliverQuoteOf keeps a delivery that quotes the answer (with its turn's feedback), not assessed yet.
func deliverQuoteOf(t *testing.T, st store.Store, scope, trace string, n int) store.MemoryDelivery {
	t.Helper()
	d := store.MemoryDelivery{ID: fmt.Sprintf("%016x", 0x7000+n), Scope: scope, Request: similarSetupRequest,
		Quotes: []store.MemoryQuote{{Key: "decision:" + trace, ID: trace, Request: pastSetupRequest, Said: "In a similar request (above), you answered \"yes\""}}}
	if err := st.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendFeedback(scope, store.Feedback{ID: d.ID, Request: d.Request, Summary: "x", Reply: "thanks"}); err != nil {
		t.Fatal(err)
	}
	return d
}

// A quote delivered before the undo and assessed after it is a use from before the undo: it does not count towards the next
// rejection. (Counting by the time of the assessment, one quote after the undo was enough to reject the answer again.)
func TestAQuoteDeliveredBeforeTheUndoDoesNotCountAfterIt(t *testing.T) {
	st, scope, h := reviewFixture(t)
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	d1 := deliverQuoteOf(t, st, scope, trace, 1)
	d2 := deliverQuoteOf(t, st, scope, trace, 2)
	d3 := deliverQuoteOf(t, st, scope, trace, 3) // delivered before the rejection, its turn not assessed yet
	assess(t, st, scope, d1, "decision:"+trace, "unused")
	assess(t, st, scope, d2, "decision:"+trace, "unused")
	if ctx := quotedBefore(t, st, scope, "ffffffffffffaa01"); strings.Contains(ctx, "decision:"+trace) {
		t.Fatal("the answer is rejected")
	}
	mustRestore(t, h, fmt.Sprintf(`{"trace_id":%q}`, trace))
	assess(t, st, scope, d3, "decision:"+trace, "unused") // the reflection of a turn from before the undo
	d4 := deliverQuoteOf(t, st, scope, trace, 4)          // the first quote after the undo
	assess(t, st, scope, d4, "decision:"+trace, "unused")
	if ctx := quotedBefore(t, st, scope, "ffffffffffffaa02"); !strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("one unused quote after the undo is not a rejection: the quote delivered before the undo counted")
	}
	d5 := deliverQuoteOf(t, st, scope, trace, 5)
	assess(t, st, scope, d5, "decision:"+trace, "unused")
	if ctx := quotedBefore(t, st, scope, "ffffffffffffaa03"); strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("two unused quotes delivered after the undo reject the answer again")
	}
}

// A line of the undo records longer than a scanner's buffer does not hide the undos after it.
func TestALongLineInTheUndoRecordsHidesNoUndo(t *testing.T) {
	st, scope, h := reviewFixture(t)
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st, scope, trace, "unused", 1)
	assessAnswer(t, st, scope, trace, "unused", 2)
	dir := filepath.Join(st.Dir, "records", "answer-restores")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	junk := `{"note":"` + strings.Repeat("x", 70<<10) + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, store.StoreName(st.Dir, scope)+".jsonl"), []byte(junk), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRestore(t, h, fmt.Sprintf(`{"trace_id":%q}`, trace))
	if ctx := quotedBefore(t, st, scope, "ffffffffffffbb01"); !strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("the undo after a long line is read: the answer is quoted again")
	}
}

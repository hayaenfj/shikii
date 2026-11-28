package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A real Claude Code (4 sessions, 8 requests) sent a second record for two reasons that left nothing to correct:
//   - it added result.sufficient with no evidence, and the whole record was refused before anything was saved;
//   - it assessed again, in the next request, a lesson that the record of the request before had already assessed, and that one
//     entry made the record incomplete, so the result was saved without its ending review and the Stop asked again.
//
// Both now save what can be saved and say, in saved[].warnings, what was not saved and why, with complete true. What is refused is
// still refused: a lesson that is not the agent's own, and one of an earlier request that is not assessed yet.

const (
	noResendLong = "Rename the export button to Export and refresh the screenshots in the documentation."
)

// noResendText has what a text of a result can hold that a copy could lose: markup characters that JSON escapes, quotes, a
// backslash, a line break and a character of more than one byte.
var noResendText = "Changed <b>\"Export\"</b> & the path C:\\export\nsecond line " + string([]rune{0x65e5, 0x672c, 0x8a9e}) + " end."

// noResendRequest is one request of the session "ending": the context the hook gave, its turn and the keys of the quotes delivered
// before it.
type noResendRequest struct {
	context, turn string
	keys          []string
}

func (f *endingFixture) noResendStart(t *testing.T, prompt string) noResendRequest {
	t.Helper()
	context := f.request(t, prompt)
	return noResendRequest{context: context, turn: loadSession(f.root, "ending").TurnID, keys: quoteKeys(t, context)}
}

// noResendWork is the work of a record of the request: its ids and a result.
func noResendWork(t *testing.T, r noResendRequest, result map[string]any) map[string]any {
	t.Helper()
	var work map[string]any
	if err := json.Unmarshal([]byte(noticeIDs(t, r.context)), &work); err != nil {
		t.Fatal(err)
	}
	work["result"] = result
	return work
}

// noResendEnd sends the Stop of the session's request and says whether it asked for more.
func (f *endingFixture) noResendEnd(t *testing.T) (reason string, blocked bool) {
	t.Helper()
	return stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "ending", "cwd": f.repo, "last_assistant_message": "Done."}))
}

// noResendFirstRequest is a request whose words deliver the two lessons of the fixture, and the record that assesses the keys it is
// given (the first helpful, the others unused), as the notice of the request describes it. It ends the request.
func (f *endingFixture) noResendFirstRequest(t *testing.T, send sendWork, assess int) noResendRequest {
	t.Helper()
	first := f.noResendStart(t, endingLong)
	if len(first.keys) < 2 {
		t.Fatalf("precondition: the words of the first request deliver the two lessons: %v\n%.600s", first.keys, first.context)
	}
	work, _ := recordTheNoticeDescribes(t, first.context)
	if form, _ := work["memory"].([]map[string]any); assess < len(form) {
		work["memory"] = form[:assess]
	}
	if reply := send(t, f, "record", work); reply["complete"] != true || reply["errors"] != nil {
		t.Fatalf("precondition: the first request's record is saved: %v", reply)
	}
	if reason, blocked := f.noResendEnd(t); blocked {
		t.Fatalf("precondition: the first request ends: %s", reason)
	}
	return first
}

// noResendSecond starts the next request, whose words deliver no lesson, and checks that the conversation of the first is recorded
// for this session (it is what says which session a delivery went to).
func (f *endingFixture) noResendSecond(t *testing.T, first noResendRequest) noResendRequest {
	t.Helper()
	second := f.noResendStart(t, noResendLong)
	if second.turn == first.turn || len(second.keys) != 0 {
		t.Fatalf("precondition: the second request is another turn and delivers no lesson: %v %v", second.turn == first.turn, second.keys)
	}
	feedback, err := (store.Store{Dir: f.root}).RecentFeedback(feedbackScope(f.repo))
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, c := range feedback {
		linked = linked || c.ID == first.turn && c.Session == "ending"
	}
	if !linked {
		t.Fatalf("precondition: the first request's conversation is recorded for the session: %+v", feedback)
	}
	return second
}

// noResendAssessments are the saved assessments of the scope by delivery and key.
func noResendAssessments(t *testing.T, st store.Store, scope string) map[string]store.MemoryAssessment {
	t.Helper()
	report, err := st.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.MemoryAssessment{}
	for _, o := range report.All {
		if o.Assessment != nil {
			out[o.SourceID+" "+o.Quote.Key] = *o.Assessment
		}
	}
	return out
}

// noResendSavedItem is the first item of the reply's saved list of this kind (and ref, when one is given).
func noResendSavedItem(reply map[string]any, kind, ref string) map[string]any {
	for _, raw := range asList(reply["saved"]) {
		item, _ := raw.(map[string]any)
		if item["kind"] == kind && (ref == "" || item["ref"] == ref) {
			return item
		}
	}
	return nil
}

func noResendWarnings(item map[string]any) string {
	parts := []string{}
	for _, w := range asList(item["warnings"]) {
		parts = append(parts, fmt.Sprint(w))
	}
	return strings.Join(parts, " | ")
}

// noResendSavedResult reads the saved result source of a reply and returns what it holds.
func (f *endingFixture) noResendSavedResult(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	item := noResendSavedItem(reply, "result", "result")
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("the reply says no saved result: %v", reply["saved"])
	}
	material, err := f.book.ReadSources([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	observations, _ := material["observations"].([]notebook.Observation)
	if len(observations) != 1 {
		t.Fatalf("the saved result is one source: %v", material)
	}
	var saved map[string]any
	if err := json.Unmarshal([]byte(observations[0].Content), &saved); err != nil {
		t.Fatalf("the saved result is not JSON: %v\n%s", err, observations[0].Content)
	}
	return saved
}

// A result.sufficient with no captured evidence is not saved, and the rest of the result is: every field as it was sent, with the
// reply saying what was left out, why, and how the grade is given. The record is complete, asks for no other, and the Stop that
// follows does not ask again. Before, the whole record was refused and the agent sent it again without sufficient.
func TestASufficientWithoutEvidenceIsLeftOutAndTheRestIsSaved(t *testing.T) {
	for _, grade := range []bool{true, false} {
		for entryName, send := range endingEntries {
			t.Run(fmt.Sprintf("sufficient %v through %s", grade, entryName), func(t *testing.T) {
				f := endingSetup(t, endingFlows[0])
				f.noResendFirstRequest(t, send, 2)
				second := f.noResendStart(t, noResendLong)
				sent := map[string]any{"changed": noResendText, "checked": "Read <the> export & checked the \"total\" row.", "sufficient": grade}
				reply := send(t, f, "record", noResendWork(t, second, sent))
				if reply["complete"] != true || reply["errors"] != nil {
					t.Fatalf("the record is saved, whole: %v", reply)
				}
				saved := f.noResendSavedResult(t, reply)
				if saved["changed"] != noResendText || saved["checked"] != sent["checked"] {
					t.Errorf("the result's texts are saved as sent: %v", saved)
				}
				if _, has := saved["sufficient"]; has {
					t.Errorf("a grade with no evidence is not saved, the quality stays unknown: %v", saved)
				}
				warnings := noResendWarnings(noResendSavedItem(reply, "result", "result"))
				for _, word := range []string{"result.sufficient", "was not saved", "captured check evidence", "stays unknown", "nothing needs to be sent again", "evidence:[refs"} {
					if !strings.Contains(strings.ToLower(warnings), strings.ToLower(word)) {
						t.Errorf("the reply says %q about the grade: %q", word, warnings)
					}
				}
				if next, has := reply["next_action"]; has {
					t.Errorf("the reply asks for no other record: %v", next)
				}
				if _, left := reply["lessons_to_assess"]; left {
					t.Errorf("no lesson of the request is left: %v", reply["lessons_to_assess"])
				}
				if reason, blocked := f.noResendEnd(t); blocked {
					t.Errorf("the Stop after that record lets the turn end: %s", reason)
				}
				// the same record sent again (an agent may repeat it after reading the reply) is the same saved result, said the same way
				again := send(t, f, "record", noResendWork(t, second, sent))
				if again["complete"] != true || again["errors"] != nil || noResendSavedItem(again, "result", "result")["id"] != noResendSavedItem(reply, "result", "result")["id"] {
					t.Errorf("the record sent again is the same result: %v", again)
				}
				if warnings := noResendWarnings(noResendSavedItem(again, "result", "result")); !strings.Contains(warnings, "was not saved") {
					t.Errorf("the reply to the record sent again says the same: %q", warnings)
				}
			})
		}
	}
}

// A grade with its evidence is saved as it always was, with no warning: the change is only for the grade that has none.
func TestASufficientWithEvidenceIsSavedAsBefore(t *testing.T) {
	for entryName, send := range endingEntries {
		t.Run(entryName, func(t *testing.T) {
			f := endingSetup(t, endingFlows[0])
			f.noResendFirstRequest(t, send, 2)
			second := f.noResendStart(t, noResendLong)
			work := noResendWork(t, second, map[string]any{"changed": noResendText, "checked": "Ran the export.", "evidence": []string{"new1"}, "sufficient": true})
			work["observations"] = []any{map[string]any{"kind": "test", "title": "Synthetic export run", "content": "The export wrote the total row last."}}
			reply := send(t, f, "record", work)
			if reply["complete"] != true || reply["errors"] != nil {
				t.Fatalf("the graded record is saved: %v", reply)
			}
			saved := f.noResendSavedResult(t, reply)
			if saved["sufficient"] != true || saved["changed"] != noResendText {
				t.Errorf("the grade with its evidence is saved with the result: %v", saved)
			}
			if item := noResendSavedItem(reply, "result", "result"); item["warnings"] != nil {
				t.Errorf("a grade with evidence has nothing to warn about: %v", item["warnings"])
			}
		})
	}
}

// A lesson delivered in an earlier request of the session and already assessed is not assessed again: the entry is left out and the
// reply says so, the rest of the record is saved and it is complete. The saved assessment stays as it was (a different result sent
// now neither replaces it nor counts twice). Before, the entry was refused and the result was left without its ending review.
func TestAnEarlierRequestsAssessedLessonIsLeftOutNotRefused(t *testing.T) {
	for entryName, send := range endingEntries {
		t.Run(entryName, func(t *testing.T) {
			f := endingSetup(t, endingFlows[0])
			first := f.noResendFirstRequest(t, send, 2)
			second := f.noResendSecond(t, first)
			st, scope := store.Store{Dir: f.root}, feedbackScope(f.repo)
			atTool := "lesson:0123456789abcdef:fedcba9876543210"
			if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "aaaaaaaaaaaaaaaa", Scope: scope, Request: noResendLong, Parent: second.turn, Tool: "Bash make test", Phrase: "make test",
				Quotes: []store.MemoryQuote{{Key: atTool, ID: "0123456789abcdef", Revision: "fedcba9876543210", Lesson: "Run make test once before a commit.", AppliesTo: "make test", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			before := noResendAssessments(t, st, scope)
			if before[first.turn+" "+first.keys[0]].Result != "helpful" {
				t.Fatalf("precondition: the first request assessed its lesson: %v", before)
			}
			work := noResendWork(t, second, map[string]any{"changed": noResendText, "checked": "Read the export."})
			work["memory"] = []any{
				map[string]any{"ref": first.keys[0], "result": "harmful", "decision": "Changed my mind about it.", "evidence": "Nothing new."},
				map[string]any{"ref": atTool, "result": "unused"},
			}
			reply := send(t, f, "record", work)
			if reply["complete"] != true || reply["errors"] != nil {
				t.Fatalf("the record is saved, whole: %v", reply)
			}
			item := noResendSavedItem(reply, "memory", first.keys[0])
			if warnings := noResendWarnings(item); !strings.Contains(warnings, "not saved") || !strings.Contains(warnings, "already assessed") || item["id"] != nil {
				t.Errorf("the reply says the entry was left out because it was assessed: %v", item)
			}
			after := noResendAssessments(t, st, scope)
			for k, a := range before {
				if !reflect.DeepEqual(after[k], a) {
					t.Errorf("an assessment already saved is not changed: %s was %+v, now %+v", k, a, after[k])
				}
			}
			if len(after) != len(before)+1 || after["aaaaaaaaaaaaaaaa "+atTool].Result != "unused" {
				t.Errorf("only the lesson of this request is assessed now: %v", after)
			}
			if saved := f.noResendSavedResult(t, reply); saved["changed"] != noResendText {
				t.Errorf("the result is saved with the record: %v", saved)
			}
			if next, has := reply["next_action"]; has {
				t.Errorf("the reply asks for no other record: %v", next)
			}
			if _, left := reply["lessons_to_assess"]; left {
				t.Errorf("no lesson of the request is left: %v", reply["lessons_to_assess"])
			}
			if reason, blocked := f.noResendEnd(t); blocked {
				t.Errorf("the Stop after that record lets the turn end: %s", reason)
			}
		})
	}
}

// What is not the agent's own, or not assessed yet, is refused as before, with the same sentence: a lesson of an earlier request that
// is still to assess (its prepared ref is the way), an invented key, a lesson another session was given and assessed, and a lesson a
// subagent was given and assessed. A refused entry makes the record incomplete, and the rest of the record is still saved.
func TestWhatIsNotTheAgentsOwnAssessedLessonIsStillRefused(t *testing.T) {
	for entryName, send := range endingEntries {
		t.Run(entryName, func(t *testing.T) {
			f := endingSetup(t, endingFlows[0])
			first := f.noResendFirstRequest(t, send, 1) // the second lesson of the first request is left to assess
			st, scope := store.Store{Dir: f.root}, feedbackScope(f.repo)
			// a lesson of another session, delivered and assessed in a request of its own, and the conversation recorded for that session
			otherKey := "lesson:1111111111111111:2222222222222222"
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": f.repo, "prompt": "Adjust the size of the screen text in the settings page."})
			otherTurn := loadSession(f.root, "other").TurnID
			if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: otherTurn, Scope: scope, Request: "Adjust the size of the screen text in the settings page.",
				Quotes: []store.MemoryQuote{{Key: otherKey, ID: "1111111111111111", Revision: "2222222222222222", Lesson: "Keep screen text readable.", AppliesTo: "screen text", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: otherTurn, Key: otherKey, Result: "helpful", Decision: "Raised the size.", Evidence: "The page shows it.", InRequest: true}); err != nil {
				t.Fatal(err)
			}
			runUnreviewedHook(t, "stop", map[string]any{"session_id": "other", "cwd": f.repo, "last_assistant_message": "Done."})
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": f.repo, "prompt": "Thanks, now document the new size in the settings guide."})
			// a lesson a subagent of the session was given in the first request, and assessed by that subagent
			childKey := "lesson:3333333333333333:4444444444444444"
			if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "bbbbbbbbbbbbbbbb", Scope: scope, Request: endingLong, Parent: first.turn, Agent: "agent-1", Session: "ending", Tool: "Bash go vet", Phrase: "go vet",
				Quotes: []store.MemoryQuote{{Key: childKey, ID: "3333333333333333", Revision: "4444444444444444", Lesson: "Run go vet.", AppliesTo: "go vet", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: "bbbbbbbbbbbbbbbb", Key: childKey, Result: "helpful", Decision: "Ran it.", Evidence: "It passed.", Agent: "agent-1"}); err != nil {
				t.Fatal(err)
			}
			second := f.noResendSecond(t, first)
			before := noResendAssessments(t, st, scope)
			cases := map[string]string{
				"an earlier request's lesson not assessed yet": first.keys[1],
				"an invented key":                   "lesson:ffffffffffffffff:eeeeeeeeeeeeeeee",
				"a lesson another session assessed": otherKey,
				"a lesson a subagent assessed":      childKey,
			}
			memory := []any{}
			for _, key := range cases {
				memory = append(memory, map[string]any{"ref": key, "result": "helpful", "decision": "d", "evidence": "e"})
			}
			work := noResendWork(t, second, map[string]any{"changed": noResendText, "checked": "Read the export."})
			work["memory"] = memory
			reply := send(t, f, "record", work)
			if reply["complete"] != false {
				t.Errorf("a refused entry leaves the record incomplete: %v", reply["complete"])
			}
			errs := recordErrors(reply)
			for name, key := range cases {
				text := errs[key]
				for _, word := range []string{fmt.Sprintf("unknown memory reference %q: not a lesson delivered to you during this request (none was)", key), "A lesson delivered to a subagent is assessed by that subagent"} {
					if !strings.Contains(text, word) {
						t.Errorf("%s: the refusal lacks %q: %q", name, word, text)
					}
				}
				if item := noResendSavedItem(reply, "memory", key); item != nil {
					t.Errorf("%s: no memory item is said to be saved: %v", name, item)
				}
			}
			if !reflect.DeepEqual(noResendAssessments(t, st, scope), before) {
				t.Errorf("a refused entry saves no assessment: %v", noResendAssessments(t, st, scope))
			}
			if f.noResendSavedResult(t, reply)["changed"] != noResendText {
				t.Errorf("the result of the record is still saved")
			}
		})
	}
}

// A lesson delivered in this request is assessed by its key as before, also when the same lesson was given to the session in an
// earlier request and assessed there: the delivery of this request is the one assessed, and the earlier one is not touched.
func TestALessonOfThisRequestIsStillAssessedAfterAnEarlierOneOfTheSameKey(t *testing.T) {
	for entryName, send := range endingEntries {
		t.Run(entryName, func(t *testing.T) {
			f := endingSetup(t, endingFlows[0])
			first := f.noResendFirstRequest(t, send, 2)
			second := f.noResendSecond(t, first)
			st, scope := store.Store{Dir: f.root}, feedbackScope(f.repo)
			key := first.keys[0]
			id, revision := strings.Split(key, ":")[1], strings.Split(key, ":")[2]
			if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "cccccccccccccccc", Scope: scope, Request: noResendLong, Parent: second.turn, Tool: "Bash make test", Phrase: "make test",
				Quotes: []store.MemoryQuote{{Key: key, ID: id, Revision: revision, Lesson: "Use tabs, not spaces, to indent the export code.", AppliesTo: "make test", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			before := noResendAssessments(t, st, scope)
			work := noResendWork(t, second, map[string]any{"changed": noResendText, "checked": "Read the export."})
			work["memory"] = []any{map[string]any{"ref": key, "result": "unused"}}
			reply := send(t, f, "record", work)
			if reply["complete"] != true || reply["errors"] != nil {
				t.Fatalf("the record is saved, whole: %v", reply)
			}
			if item := noResendSavedItem(reply, "memory", key); item != nil {
				t.Errorf("a lesson of this request is assessed, not left out: %v", item)
			}
			after := noResendAssessments(t, st, scope)
			if after["cccccccccccccccc "+key].Result != "unused" || len(after) != len(before)+1 {
				t.Errorf("the delivery of this request is assessed: %v", after)
			}
			for k, a := range before {
				if !reflect.DeepEqual(after[k], a) {
					t.Errorf("the earlier assessment is not touched: %s was %+v, now %+v", k, a, after[k])
				}
			}
		})
	}
}

// Another path that meets the same result (a recording of an assignment) is checked before anything is saved, and still refuses a
// grade with no evidence as it always did: only the record of an agent leaves it out, and says so (leaveOutUnevidencedGrade).
func TestAGradeWithoutEvidenceIsStillRefusedWhereTheResultIsChecked(t *testing.T) {
	for _, grade := range []bool{true, false} {
		err := validateWorkRecording(workRequest{Result: &workCompletion{Changed: "Reviewed the export.", Sufficient: &grade}})
		if err == nil || !strings.Contains(err.Error(), "result.sufficient needs captured check evidence") {
			t.Errorf("a result with sufficient %v and no evidence is refused where it is checked: %v", grade, err)
		}
	}
}

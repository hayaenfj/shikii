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

// The first record is where an agent that has checked the quality says so, and a grade with no evidence beside it is not saved
// (above). The words that tell the agent how to record, in every form the hook and the replies carry them, say that the grade needs
// evidence, beside the words that say it reports an actual check and that leaving it out leaves the quality unknown. They said only
// that it reports an actual check; with the grade no longer refused, 4 sessions of a real Claude Code saved neither a grade nor its
// evidence, where 5 of 6 had saved both when the refusal taught the agent to send the evidence again (real-agent/report2.md).
func TestHowToRecordSaysSufficientNeedsEvidence(t *testing.T) {
	const sentence = "Optional result.sufficient (needs evidence) reports an actual quality check; omission stays unknown."
	for name, text := range map[string]string{
		"how_to_record":                   compactWorkHowTo,
		"how_to_record of the hook":       compactHowToRecord(&workState{}),
		"how_to_record of a standalone":   compactHowToRecord(&workState{Standalone: true}),
		"how_to_record with the hook ids": strings.Replace(compactWorkHowTo, "with this receipt", "with the hook's session_id/turn_id (or this receipt)", 1),
	} {
		if !strings.Contains(text, sentence) {
			t.Errorf("%s does not say that result.sufficient needs evidence: %q", name, text)
		}
		// the evidence it names is the one the sentence before it defines: references, never text
		if at := strings.Index(text, sentence); at >= 0 && strings.Index(text, "evidence takes references") > at {
			t.Errorf("%s names the evidence of the grade before it says what evidence is", name)
		}
	}
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
				for _, word := range []string{"result.sufficient", "was not saved", "captured check evidence", "stays unknown", "nothing needs to be sent again", "give sufficient together with evidence:[refs"} {
					if !strings.Contains(strings.ToLower(warnings), strings.ToLower(word)) {
						t.Errorf("the reply says %q about the grade: %q", word, warnings)
					}
				}
				// a true grade left out is the quality staying unknown: nothing is asked. A false one is what the agent alone knows, and
				// its next_action says how to keep it (TestAnUnevidencedFalseGradeSaysInNextActionHowToKeepIt)
				next, has := reply["next_action"]
				if has == grade || (has && next != fellShortGradeNext) {
					t.Errorf("next_action for a grade of %v with no evidence: %v", grade, next)
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

// A sufficient:false with no evidence is the agent's own word that the work fell short, and nothing else records it. The warnings are
// not what an agent is told to read (complete, errors and next_action are), so next_action says it was not saved and gives the two
// ways to keep it, each in the form the record takes: the result sent again with evidence and sufficient:false, or with remaining.
// The record stays complete: the rest of it is saved, the Stop lets the turn end, and sending nothing withdraws the claim.
func TestAnUnevidencedFalseGradeSaysInNextActionHowToKeepIt(t *testing.T) {
	for _, way := range []string{"evidence", "remaining"} {
		for entryName, send := range endingEntries {
			t.Run(way+" through "+entryName, func(t *testing.T) {
				f := endingSetup(t, endingFlows[0])
				f.noResendFirstRequest(t, send, 2)
				second := f.noResendStart(t, noResendLong)
				checked := "Read <the> export & checked the \"total\" row."
				reply := send(t, f, "record", noResendWork(t, second, map[string]any{"changed": noResendText, "checked": checked, "sufficient": false}))
				if reply["complete"] != true || reply["errors"] != nil {
					t.Fatalf("the record is saved, whole: %v", reply)
				}
				if saved := f.noResendSavedResult(t, reply); saved["changed"] != noResendText || saved["checked"] != checked || saved["sufficient"] != nil {
					t.Errorf("the rest of the result is saved as sent, and the grade is not: %v", saved)
				}
				next, _ := reply["next_action"].(string)
				for _, word := range []string{"result.sufficient:false was not saved", "nothing records that this work fell short", "sufficient:false and evidence:[refs of the observations that show it]", "remaining:\"what is missing\"", "to withdraw it, send nothing"} {
					if !strings.Contains(next, word) {
						t.Errorf("next_action lacks %q: %q", word, next)
					}
				}
				if reason, blocked := f.noResendEnd(t); blocked {
					t.Errorf("the Stop after that record lets the turn end, so the claim can be withdrawn by sending nothing: %s", reason)
				}
				// each way next_action names, sent as it says, is saved
				var again map[string]any
				if way == "evidence" {
					work := noResendWork(t, second, map[string]any{"changed": noResendText, "checked": checked, "evidence": []string{"new1"}, "sufficient": false})
					work["observations"] = []any{map[string]any{"kind": "test", "title": "Synthetic export run", "content": "The export wrote the total row first."}}
					again = send(t, f, "record", work)
				} else {
					again = send(t, f, "record", noResendWork(t, second, map[string]any{"changed": noResendText, "checked": checked, "remaining": "The total row is not checked."}))
				}
				if again["complete"] != true || again["errors"] != nil {
					t.Fatalf("the record sent as next_action says is saved: %v", again)
				}
				saved := f.noResendSavedResult(t, again)
				if way == "evidence" && (saved["sufficient"] != false || len(asList(saved["evidence"])) != 1) {
					t.Errorf("the false grade is saved with its evidence: %v", saved)
				}
				if way == "remaining" && (saved["remaining"] != "The total row is not checked." || saved["sufficient"] != nil) {
					t.Errorf("what is missing is saved in remaining: %v", saved)
				}
				if item := noResendSavedItem(again, "result", "result"); item["warnings"] != nil {
					t.Errorf("nothing is left out of the record sent again: %v", item["warnings"])
				}
				if next, has := again["next_action"].(string); has && strings.Contains(next, "was not saved") {
					t.Errorf("the record sent again does not ask for another: %q", next)
				}
				if reason, blocked := f.noResendEnd(t); blocked {
					t.Errorf("the Stop after the record sent again lets the turn end: %s", reason)
				}
			})
		}
	}
}

// The sentence of next_action belongs to a record that is whole: one that is retried by its failed refs is not sent again whole, so
// next_action keeps saying what it always said, and the warnings still say what was left out.
func TestTheFalseGradeSentenceIsLeftToAWholeRecord(t *testing.T) {
	for entryName, send := range endingEntries {
		t.Run(entryName, func(t *testing.T) {
			f := endingSetup(t, endingFlows[0])
			f.noResendFirstRequest(t, send, 2)
			second := f.noResendStart(t, noResendLong)
			work := noResendWork(t, second, map[string]any{"changed": noResendText, "checked": "Read the export.", "sufficient": false})
			work["memory"] = []any{map[string]any{"ref": "lesson:ffffffffffffffff:eeeeeeeeeeeeeeee", "result": "unused"}}
			reply := send(t, f, "record", work)
			if reply["complete"] != false || reply["errors"] == nil {
				t.Fatalf("precondition: the record is not whole: %v", reply)
			}
			if next, _ := reply["next_action"].(string); strings.Contains(next, "fell short") || !strings.Contains(next, "Retry only the failed or pending refs") {
				t.Errorf("next_action is the one of a record retried by its failed refs: %q", next)
			}
			if warnings := noResendWarnings(noResendSavedItem(reply, "result", "result")); !strings.Contains(warnings, "was not saved") {
				t.Errorf("the warnings still say what was left out: %q", warnings)
			}
		})
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
			// the lesson the first request assessed was given a second time in that request, right before a command, and that delivery
			// is not assessed: the key still has a delivery to assess, so it is not "assessed"
			twice := first.keys[0]
			parts := strings.Split(twice, ":")
			if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "dddddddddddddddd", Scope: scope, Request: endingLong, Parent: first.turn, Tool: "Bash make test", Phrase: "make test",
				Quotes: []store.MemoryQuote{{Key: twice, ID: parts[1], Revision: parts[2], Lesson: "Use tabs, not spaces, to indent the export code.", AppliesTo: "make test", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			// a subagent's start the host sent no id for is delivered under the first turn with no tool and no agent: it is not the
			// agent's own lesson, assessed or not
			noID := "lesson:5555555555555555:6666666666666666"
			if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "eeeeeeeeeeeeeeee", Scope: scope, Request: endingLong, Parent: first.turn,
				Quotes: []store.MemoryQuote{{Key: noID, ID: "5555555555555555", Revision: "6666666666666666", Lesson: "Run go vet.", AppliesTo: "go vet", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: "eeeeeeeeeeeeeeee", Key: noID, Result: "helpful", Decision: "Ran it.", Evidence: "It passed.", InRequest: true}); err != nil {
				t.Fatal(err)
			}
			second := f.noResendSecond(t, first)
			before := noResendAssessments(t, st, scope)
			cases := map[string]string{
				"an earlier request's lesson given twice, one delivery not assessed": twice,
				"a lesson of a subagent start with no id, assessed":                  noID,
				"an earlier request's lesson not assessed yet":                       first.keys[1],
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

// The entry of an assignment checks its record before the work is recorded (work_assignment_record.go), and refuses a grade with no
// evidence as it always did: only the record of an agent leaves such a grade out. The record is refused whole, and what is saved
// when it is sent without the grade is the result as sent.
func TestAnAssignmentRecordStillRefusesAGradeWithoutEvidence(t *testing.T) {
	for _, grade := range []bool{true, false} {
		t.Run(fmt.Sprintf("sufficient %v", grade), func(t *testing.T) {
			b, _ := loopBook(t)
			a := assignmentInput("graded-job", "case-1", "reviewer", "review")
			v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
			if err != nil {
				t.Fatal(err)
			}
			result := map[string]any{"changed": noResendText, "checked": "Read the export.", "sufficient": grade}
			record := map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "result", "state": "completed", "summary": "A synthetic review."}, "result": result}
			if _, err = workCall(t, b, "record", record); err == nil || !strings.Contains(err.Error(), "result.sufficient needs captured check evidence") {
				t.Fatalf("the assignment's record with a grade and no evidence is refused: %v", err)
			}
			delete(result, "sufficient")
			saved, err := workCall(t, b, "record", record)
			if err != nil || saved["complete"] != true {
				t.Fatalf("the same record without the grade is the first that is saved: %v %v", saved, err)
			}
			item := noResendSavedItem(saved, "result", "result")
			id, _ := item["id"].(string)
			material, err := b.ReadSources([]string{id})
			if err != nil || id == "" {
				t.Fatalf("the saved result is read: %v %v", err, saved["saved"])
			}
			observations, _ := material["observations"].([]notebook.Observation)
			if len(observations) != 1 || !strings.Contains(observations[0].Content, "second line") || strings.Contains(observations[0].Content, "sufficient") {
				t.Errorf("the saved result holds the text as sent and no grade: %v", observations)
			}
			if item["warnings"] != nil {
				t.Errorf("nothing was left out of the record that is saved: %v", item["warnings"])
			}
		})
	}
}

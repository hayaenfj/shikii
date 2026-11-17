package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A lesson delivered during a request is assessed in that request's own work record, by its key. The agent that just did the work
// knows best whether it used the lesson, and a session that sends no further request is never asked again: before this, every
// lesson delivered in the last request of a session stayed unassessed.

// turnDelivery delivers the fixture's lesson to the request of the session "owner" and, right before a command of the same
// request, a second lesson. It returns the store, the scope, the request's turn and the two delivered keys.
func turnDelivery(t *testing.T) (st store.Store, scope, turn, atRequest, atTool string) {
	t.Helper()
	st, scope, _, draft, revision := memoryEffectFixture(t)
	atRequest = "lesson:" + draft.ID + ":" + revision
	// the request of memoryEffectPrompt, through the hook that also prepares the work (the record goes by the hook's ids)
	ctx := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "owner", "cwd": scope, "prompt": "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"}))
	if !strings.Contains(ctx, atRequest) {
		t.Fatalf("precondition: the lesson is quoted before the owner's request:\n%s", ctx)
	}
	turn = loadSession(st.Dir, "owner").TurnID
	atTool = "lesson:0123456789abcdef:fedcba9876543210"
	tool := store.MemoryDelivery{ID: "aaaaaaaaaaaaaaaa", Scope: scope, Request: "Update the install steps of the README", Parent: turn, Tool: "Bash make test", Phrase: "make test",
		Quotes: []store.MemoryQuote{{Key: atTool, ID: "0123456789abcdef", Revision: "fedcba9876543210", Lesson: "Run make test once before a commit.", AppliesTo: "make test", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}
	if err := st.SaveMemoryDelivery(tool); err != nil {
		t.Fatal(err)
	}
	return st, scope, turn, atRequest, atTool
}

// turnEnds ends the owner's request and sends its next one, through the hooks (the texts are those of memoryEffectComplete).
func turnEnds(t *testing.T, scope string) {
	t.Helper()
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "owner", "cwd": scope, "last_assistant_message": "\u672a\u521d\u671f\u5316\u306e\u5834\u6240\u3067\u306f\u8a18\u9332\u3055\u308c\u306a\u3044\u3053\u3068\u3092\u5408\u6210\u30c6\u30b9\u30c8\u3067\u78ba\u8a8d\u3057\u307e\u3057\u305f\u3002"})
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "owner", "cwd": scope, "prompt": "\u7d9a\u3044\u3066\u753b\u9762\u306e\u8272\u3092\u8abf\u6574\u3057\u3066\u304f\u3060\u3055\u3044\u3002"})
}

// assessed maps the key of every delivery of the scope to its saved result ("" while it waits).
func assessed(t *testing.T, st store.Store, scope string) map[string]string {
	t.Helper()
	report, err := st.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, o := range report.All {
		out[o.SourceID+" "+o.Quote.Key] = ""
		if o.Assessment != nil {
			out[o.SourceID+" "+o.Quote.Key] = o.Assessment.Result
		}
	}
	return out
}

func recordErrors(r map[string]any) map[string]string {
	out := map[string]string{}
	for _, raw := range asList(r["errors"]) {
		item, _ := raw.(map[string]any)
		ref, _ := item["ref"].(string)
		text, _ := item["error"].(string)
		out[ref] = text
	}
	return out
}

func TestAWorkRecordAssessesTheLessonsDeliveredInItsOwnRequest(t *testing.T) {
	st, scope, turn, atRequest, atTool := turnDelivery(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	r, err := workCall(t, b, "record", map[string]any{"session_id": "owner", "turn_id": turn, "result": map[string]any{"changed": "Updated the install steps."},
		"memory": []any{
			map[string]any{"ref": atRequest, "result": "helpful", "decision": "Checked a folder that was not initialized.", "evidence": "The synthetic check passed."},
			map[string]any{"ref": atTool, "result": "unused"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := recordErrors(r); len(errs) != 0 {
		t.Fatalf("the record assesses the lessons delivered in its own request: %v", errs)
	}
	got := assessed(t, st, scope)
	if got[turn+" "+atRequest] != "helpful" || got["aaaaaaaaaaaaaaaa "+atTool] != "unused" {
		t.Fatalf("the assessments are saved on the deliveries of this request: %v", got)
	}
	if _, left := r["lessons_to_assess"]; left {
		t.Errorf("nothing is left to assess once every delivery of the request is assessed: %v", r["lessons_to_assess"])
	}
	// the record says how the assessment was made: in the request, before the person's next message
	report, err := st.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range report.All {
		if o.Assessment == nil || !o.Assessment.InRequest || o.Assessment.Agent != "" {
			t.Errorf("the saved assessment of %s is recorded as made in its request: %+v", o.Quote.Key, o.Assessment)
		}
	}
	// the conversation ends and the session sends its next request: the lessons already assessed are not asked about again
	turnEnds(t, scope)
	view, err := reflectQueryAs(st.Dir, scope, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, memories := listedMemory(view, turn); memories != 0 {
		t.Errorf("a lesson assessed in its own request is not asked about at the next request: %d listed", memories)
	}
}

func TestAWorkRecordSaysWhichLessonsOfItsRequestAreNotAssessed(t *testing.T) {
	st, scope, turn, atRequest, atTool := turnDelivery(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	r, err := workCall(t, b, "record", map[string]any{"session_id": "owner", "turn_id": turn, "result": map[string]any{"changed": "Updated the install steps."},
		"memory": []any{map[string]any{"ref": atTool, "result": "unused"}}})
	if err != nil {
		t.Fatal(err)
	}
	left, _ := r["lessons_to_assess"].(map[string]any)
	refs := asList(left["refs"])
	if len(refs) != 1 || refs[0] != atRequest {
		t.Fatalf("the reply names the lesson of this request that is not assessed yet: %v", r["lessons_to_assess"])
	}
	if how, _ := left["how"].(string); !strings.Contains(how, "memory:[{ref,result,decision,evidence}]") || !strings.Contains(how, "unknown") {
		t.Errorf("the reply says how to assess them: %q", how)
	}
	// real agents (OpenCode 2.3.0 twice, Codex 0.169.0 once) sent evidence as a list of refs, as result.evidence is, and were refused once
	if how, _ := left["how"].(string); !strings.Contains(how, "decision and evidence are text, not lists") {
		t.Errorf("the reply says the two are text: %q", how)
	}
	// agents are told to check next_action, and real ones (Codex 0.169.0, Claude Code) ended the request on complete:true with
	// lessons_to_assess in the reply: the reply's next step is the record that assesses them
	if next, _ := r["next_action"].(string); !strings.Contains(next, "another work record of this request") || !strings.Contains(next, "lessons_to_assess") {
		t.Errorf("next_action sends the agent to the lessons left: %q", next)
	}
	// the rest goes in another record of the same request, with nothing else in it
	r, err = workCall(t, b, "record", map[string]any{"session_id": "owner", "turn_id": turn,
		"memory": []any{map[string]any{"ref": atRequest, "result": "unknown", "decision": "The steps were updated as the lesson says.", "evidence": "No check shows the lesson was the reason.", "correction_result": "followed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := recordErrors(r); len(errs) != 0 {
		t.Fatalf("the second record assesses the rest: %v", errs)
	}
	if _, still := r["lessons_to_assess"]; still {
		t.Errorf("nothing is left after the second record: %v", r["lessons_to_assess"])
	}
	if next, _ := r["next_action"].(string); strings.Contains(next, "lessons_to_assess") {
		t.Errorf("with nothing left there is no such next step: %q", next)
	}
	if got := assessed(t, st, scope); got[turn+" "+atRequest] != "unknown" {
		t.Errorf("the second record's assessment is saved: %v", got)
	}
}

// Only the lessons of the record's own request are taken by their key: one delivered in an earlier request of the session (it has
// its prepared ref by then), to another session, or to a subagent (which assesses it itself) is refused, with the keys it can take.
func TestAWorkRecordTakesByKeyOnlyTheLessonsOfItsOwnRequest(t *testing.T) {
	st, scope, turn, atRequest, _ := turnDelivery(t)
	child := "lesson:1111111111111111:2222222222222222"
	if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "bbbbbbbbbbbbbbbb", Scope: scope, Request: "Update the install steps of the README", Parent: turn, Agent: "agent-1", Session: "owner", Tool: "Bash go vet", Phrase: "go vet",
		Quotes: []store.MemoryQuote{{Key: child, ID: "1111111111111111", Revision: "2222222222222222", Lesson: "Run go vet.", AppliesTo: "go vet", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	b := notebook.Book{Root: st.Dir, Scope: scope}
	// the owner's own record does not take the subagent's lesson
	r, err := workCall(t, b, "record", map[string]any{"session_id": "owner", "turn_id": turn, "result": map[string]any{"changed": "Updated the install steps."},
		"memory": []any{map[string]any{"ref": child, "result": "unused"}}})
	if err != nil {
		t.Fatal(err)
	}
	refusal := recordErrors(r)[child]
	for _, w := range []string{"not a lesson delivered to you during this request", atRequest} {
		if !strings.Contains(refusal, w) {
			t.Errorf("the refusal of a subagent's lesson lacks %q: %q", w, refusal)
		}
	}
	// another session's record does not take the owner's lesson
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "Adjust the size of the screen text."})
	r, err = workCall(t, b, "record", map[string]any{"session_id": "other", "turn_id": loadSession(st.Dir, "other").TurnID, "result": map[string]any{"changed": "Adjusted the size."},
		"memory": []any{map[string]any{"ref": atRequest, "result": "helpful", "decision": "d", "evidence": "e"}}})
	if err != nil {
		t.Fatal(err)
	}
	if refusal := recordErrors(r)[atRequest]; !strings.Contains(refusal, "not a lesson delivered to you during this request") {
		t.Errorf("another session's record does not assess the owner's lesson: %q", refusal)
	}
	// the owner's next request does not take the earlier request's lesson by its key: it is prepared there under its own ref
	turnEnds(t, scope)
	next := loadSession(st.Dir, "owner").TurnID
	if next == turn {
		t.Fatal("precondition: the owner's next request is another turn")
	}
	r, err = workCall(t, b, "record", map[string]any{"session_id": "owner", "turn_id": next, "result": map[string]any{"changed": "Adjusted the colours."},
		"memory": []any{map[string]any{"ref": atRequest, "result": "helpful", "decision": "d", "evidence": "e"}}})
	if err != nil {
		t.Fatal(err)
	}
	if refusal := recordErrors(r)[atRequest]; !strings.Contains(refusal, "not a lesson delivered to you during this request") {
		t.Errorf("a later request does not assess an earlier request's lesson by its key: %q", refusal)
	}
	for k, result := range assessed(t, st, scope) {
		if result != "" {
			t.Errorf("a refused assessment is not saved: %s %s", k, result)
		}
	}
}

// The agent is told once, where it is told how to record: the lessons of this request are assessed in the record, by their key.
func TestHowToRecordSaysTheLessonsOfTheRequestAreAssessedByTheirKey(t *testing.T) {
	for _, w := range []string{"a lesson delivered during this request", "{ref:<its key>,result,decision,evidence}"} {
		if !strings.Contains(compactWorkHowTo, w) {
			t.Errorf("how_to_record lacks %q", w)
		}
	}
}

// Real agents (Codex 0.169.0, OpenCode 2.3.0) write the record from step 2 of `## Prepare once; record the result` in the skill's
// recording.md, the section how_to_record sends them to, and not from the list of fields far below it: a Codex that got its lesson
// right before apply_patch sent a record without memory. Step 2 itself says it.
func TestTheStepAgentsReadSaysTheLessonsOfTheRequestGoInItsRecord(t *testing.T) {
	text := string(mustRead(t, "../../skills/shikii/references/recording.md"))
	_, rest, found := strings.Cut(text, "\n## Prepare once; record the result\n")
	_, step, has := strings.Cut(rest, "\n2. At the end, record the result")
	if !found || !has {
		t.Fatal("recording.md has no step 2 under `## Prepare once; record the result`")
	}
	step, _, _ = strings.Cut(step, "\n\n")
	step = strings.Join(strings.Fields(step), " ")
	for _, w := range []string{"delivered during this request", "`memory:[{ref:\"lesson:<id>:<revision>\",result,decision,evidence}]`", "`lessons_to_assess`"} {
		if !strings.Contains(step, w) {
			t.Errorf("step 2 lacks %s: %q", w, step)
		}
	}
}

// A real Codex (0.169.0) assessed its lesson with result unknown, correction_result followed and what it did, and no evidence. The
// refusal said only that grounds were needed, so its next record dropped everything but the result. The refusal names what is
// missing, and the assessment is not saved without it. A first wording ended with which assessments may leave both out, and the
// same Codex took that way out again: the refusal only says what to add to the entry it was sent.
func TestARefusedAssessmentOfTheRequestSaysWhatItLacks(t *testing.T) {
	for _, c := range []struct {
		lacks string
		entry map[string]any
	}{
		{"needs evidence", map[string]any{"result": "unknown", "decision": "Ran the steps before editing.", "correction_result": "followed"}},
		{"needs decision", map[string]any{"result": "helpful", "evidence": "The steps ran."}},
	} {
		st, scope, turn, atRequest, _ := turnDelivery(t)
		c.entry["ref"] = atRequest
		r, err := workCall(t, notebook.Book{Root: st.Dir, Scope: scope}, "record", map[string]any{"session_id": "owner", "turn_id": turn, "memory": []any{c.entry}})
		if err != nil {
			t.Fatal(err)
		}
		reply, _ := json.Marshal(r)
		if !strings.Contains(string(reply), c.lacks) || !strings.Contains(string(reply), "to the same entry and send it again") {
			t.Errorf("the refusal says the assessment %s and to add it to the same entry: %s", c.lacks, reply)
		}
		if strings.Contains(string(reply), "may leave") {
			t.Errorf("the refusal does not offer to drop what the entry has: %s", reply)
		}
		if got := assessed(t, st, scope); got[turn+" "+atRequest] != "" {
			t.Errorf("an assessment that %s is not saved: %v", c.lacks, got)
		}
	}
}

// Outside the request's own work record nothing changes: an assessment sent through the reflection still waits for the
// conversation to be recorded, and the store does not take an in-request assessment for a lesson delivered to a subagent.
func TestOnlyTheRequestsOwnRecordAssessesBeforeTheConversationIsRecorded(t *testing.T) {
	st, scope, turn, atRequest, _ := turnDelivery(t)
	_, err := runReflect(st.Dir, scope, reflectRequest{Operation: "submit", Memory: []memoryAssessmentRequest{{SourceID: turn, Key: atRequest, Result: "helpful", Decision: "d", Evidence: "e"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := assessed(t, st, scope); got[turn+" "+atRequest] != "" {
		t.Fatalf("a reflection sent before the conversation is recorded is not saved: %v", got)
	}
	var input reflectRequest
	if err := json.Unmarshal([]byte(`{"operation":"submit","memory":[{"source_id":"`+turn+`","key":"`+atRequest+`","result":"helpful","decision":"d","evidence":"e","in_request":true,"inRequest":true}]}`), &input); err != nil {
		t.Fatal(err)
	}
	if _, err := runReflect(st.Dir, scope, input); err != nil {
		t.Fatal(err)
	}
	if got := assessed(t, st, scope); got[turn+" "+atRequest] != "" {
		t.Fatalf("the input of a reflection cannot claim to be the request's own record: %v", got)
	}
	child := "lesson:1111111111111111:2222222222222222"
	if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: "bbbbbbbbbbbbbbbb", Scope: scope, Request: "Update the install steps of the README", Parent: turn, Agent: "agent-1", Session: "owner", Tool: "Bash go vet", Phrase: "go vet",
		Quotes: []store.MemoryQuote{{Key: child, ID: "1111111111111111", Revision: "2222222222222222", Lesson: "Run go vet.", AppliesTo: "go vet", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	err = st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: "bbbbbbbbbbbbbbbb", Key: child, Result: "unused", Decision: "d", Evidence: "e", InRequest: true})
	if err == nil || !strings.Contains(err.Error(), "assessed by that subagent") {
		t.Fatalf("the store does not take an in-request assessment for a subagent's lesson: %v", err)
	}
}

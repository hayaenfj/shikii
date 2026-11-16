package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A lesson delivered in one session is assessed by that session. Another session never saw how the lesson was used: asked about
// it, it could only answer unknown, and an assessment is final. So the reflection of another session does not list the delivery,
// and a conversation with nothing else pending waits for its own session. A caller that names no session (a plain reflect
// query) and a conversation recorded before sessions were kept are not narrowed.

// hookPrepared is the work the hook itself prepares for this request of the session, as the agent receives it.
func hookPrepared(t *testing.T, scope, session, prompt string) map[string]any {
	t.Helper()
	context := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": prompt}))
	_, encoded, found := strings.Cut(context, "shikii prepared work:\n")
	if !found {
		t.Fatalf("the hook prepares the work of session %s:\n%s", session, context)
	}
	var prepared map[string]any
	if err := json.Unmarshal([]byte(encoded), &prepared); err != nil {
		t.Fatalf("the preparation of session %s: %v\n%s", session, err, encoded)
	}
	return prepared
}

// preparedPending is what a preparation lists for the reflection: the keys of the deliveries to assess and the refs of the
// decisions to score.
func preparedPending(prepared map[string]any) (memory, reviews []string) {
	pending, _ := prepared["pending"].([]any)
	for _, item := range pending {
		conversation, _ := item.(map[string]any)
		for _, m := range asList(conversation["memory"]) {
			quote, _ := m.(map[string]any)["quote"].(map[string]any)
			key, _ := quote["key"].(string)
			memory = append(memory, key)
		}
		for _, r := range asList(conversation["reviews"]) {
			ref, _ := r.(map[string]any)["ref"].(string)
			reviews = append(reviews, ref)
		}
	}
	return memory, reviews
}

// sessionDelivery delivers the fixture's lesson to the session "owner" before its request and records the conversation (the
// reply and the person's next message). It returns the store, the scope, the conversation and the delivered quote's key.
func sessionDelivery(t *testing.T) (st store.Store, scope, turn, key string) {
	t.Helper()
	st, scope, _, draft, revision := memoryEffectFixture(t)
	key = "lesson:" + draft.ID + ":" + revision
	if ctx := memoryEffectPrompt(t, scope, "owner"); !strings.Contains(ctx, key) {
		t.Fatalf("precondition: the lesson is quoted before the owner's request:\n%s", ctx)
	}
	turn = memoryEffectComplete(t, st, scope, "owner")
	return st, scope, turn, key
}

// listedMemory reports whether the view lists the conversation, and how many of its deliveries wait for an assessment there.
func listedMemory(v reflectView, turn string) (listed bool, memories int) {
	for _, s := range v.Sources {
		if s.Source.ID == turn {
			return true, len(s.Memories)
		}
	}
	return false, 0
}

// Through the hooks: the preparation of another session's request does not hand it the owner's delivery; the owner's does.
func TestALessonDeliveredToOneSessionWaitsForThatSession(t *testing.T) {
	_, scope, _, key := sessionDelivery(t)
	other, _ := preparedPending(hookPrepared(t, scope, "other", "Adjust the size of the screen text."))
	for _, k := range other {
		if k == key {
			t.Fatalf("a lesson delivered to the session owner is not handed to another session for assessment: %s", k)
		}
	}
	found := false
	owner, _ := preparedPending(hookPrepared(t, scope, "owner", "Adjust the size of the screen text."))
	for _, k := range owner {
		found = found || k == key
	}
	if !found {
		t.Fatal("the session the lesson was delivered to is asked to assess it")
	}
}

func TestTheReflectionListsADeliveryForItsOwnSessionOnly(t *testing.T) {
	st, scope, turn, _ := sessionDelivery(t)
	fs, err := st.RecentFeedback(scope)
	if err != nil {
		t.Fatal(err)
	}
	var conversation store.Feedback
	for _, f := range fs {
		if f.ID == turn {
			conversation = f
		}
	}
	if conversation.Session != "owner" {
		t.Fatalf("the conversation's record names the session it took place in: %q", conversation.Session)
	}
	view := func(viewer string) reflectView {
		t.Helper()
		v, err := reflectQueryAs(st.Dir, scope, viewer)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	left := func(viewer string) int {
		t.Helper()
		items, err := reflectLeft(st.Dir, scope, []string{turn}, viewer)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, item := range items {
			n += item.memories
		}
		return n
	}
	// the conversation still waits to be sorted into the wiki, so it is listed for every session: without its deliveries for another
	if listed, n := listedMemory(view("other"), turn); !listed || n != 0 {
		t.Fatalf("another session sorts the conversation into the wiki but is not asked about its deliveries: listed %v, deliveries %d", listed, n)
	}
	if left("other") != 0 {
		t.Fatal("another session's Stop does not wait for the owner's deliveries")
	}
	for _, viewer := range []string{"owner", ""} {
		if listed, n := listedMemory(view(viewer), turn); !listed || n != 1 {
			t.Fatalf("viewer %q (the owner, or a caller that names no session) is asked about the delivery: listed %v, deliveries %d", viewer, listed, n)
		}
		if left(viewer) != 1 {
			t.Fatalf("viewer %q: the delivery is what is left of the conversation", viewer)
		}
	}
	// once the conversation is sorted into the wiki and its decision is scored, nothing of it is left for another session: it is
	// not listed at all
	settle := reflectRequest{Operation: "submit", Wiki: []notebook.Draft{{Sources: []string{turn}}}}
	for _, src := range view("other").Sources {
		if src.Source.ID == turn {
			for _, target := range src.Targets {
				settle.Reviews = append(settle.Reviews, reviewRequest{SourceID: turn, TraceID: target.TraceID, Truth: "unknown", Reason: "The synthetic conversation does not show the answer."})
			}
		}
	}
	if r, err := runReflect(st.Dir, scope, settle); err != nil || !r.(reflectResult).Complete {
		t.Fatalf("sorting the conversation into the wiki and scoring its decision: %v %v", r, err)
	}
	if listed, _ := listedMemory(view("other"), turn); listed {
		t.Fatal("a conversation whose only pending part is another session's deliveries is not listed")
	}
	if listed, n := listedMemory(view("owner"), turn); !listed || n != 1 {
		t.Fatalf("the owner still has its delivery to assess: listed %v, deliveries %d", listed, n)
	}
	// before a request, the hook asks each session about what waits for it
	ask := func(session string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "Adjust the size of the screen text."}))
	}
	if ctx := ask("other"); strings.Contains(ctx, turn) {
		t.Fatalf("the hook does not ask another session about the owner's conversation:\n%s", ctx)
	}
	if ctx := ask("owner"); !strings.Contains(ctx, turn) {
		t.Fatalf("the hook asks the owner about its conversation:\n%s", ctx)
	}
	// a record from before sessions were kept (the newest line of an id is the one read) stays listed for every session
	conversation.Session = ""
	if err := st.AppendFeedback(scope, conversation); err != nil {
		t.Fatal(err)
	}
	if listed, n := listedMemory(view("other"), turn); !listed || n != 1 {
		t.Fatalf("a conversation recorded without a session is assessed by whoever reflects, as before: listed %v, deliveries %d", listed, n)
	}
}

// At Stop, a session that was asked to reflect on another session's conversation is done once it sorted the conversation into the
// wiki and scored its decision: the lessons delivered there wait for their own session, not for this turn.
func TestStopDoesNotWaitForAnotherSessionsDeliveries(t *testing.T) {
	st, scope, turn, _ := sessionDelivery(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "Adjust the size of the screen text."})
	asked := false
	for _, id := range loadSession(st.Dir, "other").ReflectAsked {
		asked = asked || id == turn
	}
	if !asked {
		t.Fatalf("precondition: the other session is asked to sort the owner's conversation into the wiki: %v", loadSession(st.Dir, "other").ReflectAsked)
	}
	v, err := reflectQueryAs(st.Dir, scope, "other")
	if err != nil {
		t.Fatal(err)
	}
	settle := reflectRequest{Operation: "submit"}
	for _, src := range v.Sources {
		if src.WikiPending {
			settle.Wiki = append(settle.Wiki, notebook.Draft{Sources: []string{src.Source.ID}})
		}
		for _, target := range src.Targets {
			settle.Reviews = append(settle.Reviews, reviewRequest{SourceID: src.Source.ID, TraceID: target.TraceID, Truth: "unknown", Reason: "The synthetic conversation does not show the answer."})
		}
	}
	if r, err := runReflect(st.Dir, scope, settle); err != nil || !r.(reflectResult).Complete {
		t.Fatalf("the other session sorts and scores what is listed for it: %v %v", r, err)
	}
	if stop := runHook(t, "stop", map[string]any{"session_id": "other", "cwd": scope, "last_assistant_message": "Adjusted the size of the screen text."}); strings.Contains(stop, turn) {
		t.Fatalf("the other session's Stop does not wait for the owner's deliveries:\n%s", stop)
	}
}

// A work record of another session scores the owner's conversation; what it reports as left does not name the owner's delivery.
func TestAWorkRecordLeavesAnotherSessionsDeliveriesOutOfWhatIsLeft(t *testing.T) {
	st, scope, _, _ := sessionDelivery(t)
	_, refs := preparedPending(hookPrepared(t, scope, "other", "Adjust the size of the screen text."))
	if len(refs) == 0 {
		t.Fatal("precondition: the other session's preparation lists decisions to score")
	}
	reviews := []any{}
	for _, ref := range refs {
		reviews = append(reviews, map[string]any{"ref": ref, "answer": "unknown", "reason": "The synthetic conversation does not show the answer."})
	}
	b := notebook.Book{Root: st.Dir, Scope: scope}
	r, err := workCall(t, b, "record", map[string]any{"session_id": "other", "turn_id": loadSession(st.Dir, "other").TurnID, "result": map[string]any{"changed": "Adjusted the size of the screen text."}, "reviews": reviews})
	if err != nil {
		t.Fatal(err)
	}
	reflection, ok := r["reflection"].(map[string]any)
	if !ok {
		t.Fatalf("the record saves the scores as a reflection: %v", r)
	}
	if left := asList(reflection["left_memory"]); len(left) != 0 {
		t.Fatalf("the owner's delivery is not left for the other session: %v", left)
	}
}

// The older planning query of a session lists the reflection as that session sees it.
func TestThePlanningQueryListsDeliveriesForItsOwnSession(t *testing.T) {
	st, scope, turn, _ := sessionDelivery(t)
	deliveries := func(session string) int {
		t.Helper()
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "Adjust the size of the screen text."})
		v, err := planningCall(t, "query", map[string]any{"session_id": session, "turn_id": loadSession(st.Dir, session).TurnID, "query": "screen text"})
		if err != nil {
			t.Fatal(err)
		}
		reflection, _ := v["reflection"].(map[string]any)
		for _, src := range asList(reflection["sources"]) {
			source := src.(map[string]any)
			if source["source_material"].(map[string]any)["id"] == turn {
				return len(asList(source["memories"]))
			}
		}
		t.Fatalf("the planning query of session %s lists the conversation: %v", session, v["reflection"])
		return 0
	}
	if n := deliveries("other"); n != 0 {
		t.Fatalf("another session's planning query does not list the owner's delivery: %d", n)
	}
	if n := deliveries("owner"); n != 1 {
		t.Fatalf("the owner's planning query lists its delivery: %d", n)
	}
}

// A preparation made without a hook names no session a conversation took place in (its session id is made up for the receipt),
// so nothing is narrowed for it: it lists the delivery, as a plain reflect query does.
func TestAPreparationWithoutAHookIsNotNarrowed(t *testing.T) {
	st, scope, _, key := sessionDelivery(t)
	prepared, err := workCall(t, notebook.Book{Root: st.Dir, Scope: scope}, "prepare", map[string]any{"query": "README", "request": "Check the README instructions."})
	if err != nil {
		t.Fatal(err)
	}
	memory, _ := preparedPending(prepared)
	for _, k := range memory {
		if k == key {
			return
		}
	}
	t.Fatalf("a preparation without a hook lists the deliveries that wait, whatever their session: %v", memory)
}

// The older planning submit of another session sorts the owner's conversation; what it reports as left does not name the owner's
// delivery.
func TestThePlanningSubmitLeavesAnotherSessionsDeliveriesOutOfWhatIsLeft(t *testing.T) {
	st, scope, turn, _ := sessionDelivery(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "Adjust the size of the screen text."})
	ids := map[string]any{"session_id": "other", "turn_id": loadSession(st.Dir, "other").TurnID, "query": "screen text"}
	if _, err := planningCall(t, "query", ids); err != nil {
		t.Fatal(err)
	}
	ids["agreed"], ids["open"] = map[string]bool{"unknown": true}, map[string]bool{"unknown": true}
	ids["reflection"] = map[string]any{"wiki": []any{map[string]any{"sources": []string{turn}}}}
	v, err := planningCall(t, "submit", ids)
	if err != nil {
		t.Fatal(err)
	}
	reflection, ok := v["reflection"].(map[string]any)
	if !ok || reflection["complete"] != true {
		t.Fatalf("the submit sorts the conversation into the wiki: %v", v)
	}
	if left := asList(reflection["left_memory"]); len(left) != 0 {
		t.Fatalf("the owner's delivery is not left for the other session: %v", left)
	}
}

// stopTargets is the request the Stop's reason tells the agent to send: the JSON after "targets " in the reason, read as the reflection
// reads a request.
func stopTargets(t *testing.T, stop string) reflectRequest {
	t.Helper()
	reason, blocked := stopReason(t, stop)
	i := strings.Index(reason, "targets {")
	if !blocked || i < 0 {
		t.Fatalf("the Stop does not ask for the reflection with targets:\n%s", stop)
	}
	var req reflectRequest
	if err := json.NewDecoder(strings.NewReader(reason[i+len("targets "):])).Decode(&req); err != nil {
		t.Fatalf("the targets of the Stop are not a request: %v\n%s", err, reason)
	}
	return req
}

// A session that owes only the wiki part of another session's conversation is stopped and sent to reflect query. The targets it is
// given name the session, so the query and the submit that follow the Stop leave the owner's deliveries to the owner: the other
// session saw nothing of how the lesson was used and could only answer unknown, for good.
func TestTheStopsTargetsLeaveAnotherSessionsDeliveriesToTheirOwner(t *testing.T) {
	st, scope, turn, key := sessionDelivery(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "Adjust the size of the screen text."})
	targets := stopTargets(t, runHook(t, "stop", map[string]any{"session_id": "other", "cwd": scope, "last_assistant_message": "Adjusted the size of the screen text."}))
	if targets.SessionID != "other" || !slices.Contains(targets.Sources, turn) {
		t.Fatalf("the Stop names the conversation and the session that is asked, got %+v", targets)
	}
	query := targets
	query.Operation = "query"
	view, err := runReflect(st.Dir, scope, query)
	if err != nil {
		t.Fatal(err)
	}
	if listed, n := listedMemory(view.(reflectView), turn); !listed || n != 0 {
		t.Fatalf("the query the Stop sends the other session to lists the conversation without the owner's delivery %s, got listed %v with %d deliveries", key, listed, n)
	}
	submit := targets
	submit.Operation, submit.Wiki = "submit", []notebook.Draft{{Sources: []string{turn}}}
	r, err := runReflect(st.Dir, scope, submit)
	if err != nil {
		t.Fatal(err)
	}
	if res := r.(reflectResult); len(res.LeftMemory) != 0 || strings.Contains(strings.Join(res.Left, " "), "delivered lessons") {
		t.Fatalf("the submit that follows the Stop leaves the owner's delivery to the owner, got left %v and left_memory %v", res.Left, res.LeftMemory)
	}
	// the owner is still asked, and a request that names no session is not narrowed
	own, err := runReflect(st.Dir, scope, reflectRequest{Operation: "query", SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, n := listedMemory(own.(reflectView), turn); n != 1 {
		t.Fatalf("the owner's query lists its delivery, got %d", n)
	}
	plain, err := runReflect(st.Dir, scope, reflectRequest{Operation: "query"})
	if err != nil {
		t.Fatal(err)
	}
	if _, n := listedMemory(plain.(reflectView), turn); n != 1 {
		t.Fatalf("a query that names no session is not narrowed, got %d", n)
	}
}

// The notice before a request names the session with the conversations too, so the query it points to is narrowed in the same way.
func TestTheNoticeBeforeARequestNamesTheSessionWithItsTargets(t *testing.T) {
	ask := reflectAsk("shikii reflect", []string{"0123456789abcdef"}, "other")
	if !strings.Contains(ask, `{"session_id":"other","sources":["0123456789abcdef"]}`) {
		t.Fatalf("the notice gives the session with the targets:\n%s", ask)
	}
	if plain := reflectAsk("shikii reflect", []string{"0123456789abcdef"}, ""); strings.Contains(plain, "session_id") {
		t.Fatalf("a notice for no session names none:\n%s", plain)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The window of `shikii nap` moves only over the conversations the agent has read, and the only sign of reading is a save that names
// the conversation's id among the sources of a wiki entry after the output was printed. These tests are the reported failure: an agent
// that ran `shikii nap | python ...` and dropped the conversations from what it showed had its window moved over all of them, and they
// never came again. They send the sources through the same operation the agent uses (reflect submit, a wiki entry with sources only).

// napSendSources is what the agent does once it has read conversations: reflect submit with one wiki entry that has only sources. It
// checks that every id is accepted, those that were reviewed long before included.
func napSendSources(t *testing.T, st store.Store, scope string, ids ...string) {
	t.Helper()
	out, err := runReflect(st.Dir, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{{Sources: ids}}})
	if err != nil {
		t.Fatalf("sending the sources %v: %v", ids, err)
	}
	res, ok := out.(reflectResult)
	if !ok || !res.Complete || len(res.Wiki) != 1 || res.Wiki[0].Error != "" {
		t.Fatalf("sources already reviewed are accepted again, and the save is complete: %+v", out)
	}
}

// napShownAgainOf is the shown_again of the printed JSON (0 when the key is not there).
func napShownAgainOf(t *testing.T, out string) int {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	n, _ := v["shown_again"].(float64)
	return int(n)
}

// (a) A run whose output nobody read: nothing was sent, so the next run prints the same conversations again, says how many of them it
// gave before, and once the agent sends all of the ids nothing is printed any more.
func TestNapPrintsTheSameConversationsAgainWhenTheAgentSentNoSources(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-2*time.Hour), "second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-time.Hour), "third")
	first, out := decodeNapOutput(t)
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 3) || napShownAgainOf(t, out) != 0 {
		t.Fatalf("premise: the first run prints all three and none of them again: %v\n%s", got, out)
	}
	// the agent dropped the conversations without reading them: it sends nothing
	second, out := decodeNapOutput(t)
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(1, 3) {
		t.Fatalf("conversations nobody read are printed again: %v", got)
	}
	if n := napShownAgainOf(t, out); n != 3 {
		t.Fatalf("shown_again counts the conversations the last run gave and nobody read: %d\n%s", n, out)
	}
	napSendSources(t, st, scope, "0000000000000001", "0000000000000002", "0000000000000003")
	third, out := decodeNapOutput(t)
	if got := conversationIDs(third); len(got) != 0 || napShownAgainOf(t, out) != 0 {
		t.Fatalf("once the agent has sent all three ids, nothing is left: %v\n%s", got, out)
	}
}

// (b) Of three conversations printed, the agent sends the first two: the next run starts at the third. The third one's id had been sent
// before the first run printed (an earlier reflection named it), and that must not count as reading it: it is printed again.
func TestNapStartsAtTheFirstConversationNotSentWhenTheLeadingOnesWere(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-2*time.Hour), "second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-time.Hour), "third")
	napSendSources(t, st, scope, "0000000000000003") // before any run printed it
	first := decodeNap(t)
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 3) {
		t.Fatalf("premise: the first run prints all three: %v", got)
	}
	napSendSources(t, st, scope, "0000000000000001", "0000000000000002")
	second, out := decodeNapOutput(t)
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(3, 3) {
		t.Fatalf("the next run starts at the third conversation, the one not sent after the output: %v", got)
	}
	if n := napShownAgainOf(t, out); n != 1 {
		t.Fatalf("the third was given by the last run and not read: shown_again %d\n%s", n, out)
	}
	napSendSources(t, st, scope, "0000000000000003")
	if got := conversationIDs(decodeNap(t)); len(got) != 0 {
		t.Fatalf("after the third is sent, nothing is left: %v", got)
	}
}

// (c) Only the second conversation is sent: the first was not read, so the window does not move at all, and all three are printed again.
// The window stops at the first conversation that was not read, not at the last one that was.
func TestNapMovesNowhereWhenOnlyALaterConversationWasSent(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-2*time.Hour), "second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-time.Hour), "third")
	decodeNap(t)
	napSendSources(t, st, scope, "0000000000000002")
	second := decodeNap(t)
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(1, 3) {
		t.Fatalf("the first conversation was not sent, so all three come again: %v", got)
	}
}

// A conversation older than the newest 256 that no save has archived is not among the sources a save accepts, so the agent could not
// say it had read such a conversation `shikii nap` printed, and the nap printed the same ones again for ever. A conversation that
// .shikii/nap-shown lists, and that is a line of the record, is accepted: the agent sends the ids the nap gave it, and the next run goes on.
func TestNapAcceptsAsSourcesTheOldUnsavedConversationsItPrintedAndGoesOn(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	const total = 400 // the oldest 144 are older than the newest 256; one run prints at most 64
	for i := 2; i <= total; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(total+10-i)*time.Minute), "reply")
	}
	first := decodeNap(t)
	n := len(first.Conversations)
	if n < 44 || fmt.Sprint(conversationIDs(first)) != hexIDs(1, n) {
		t.Fatalf("premise: the first run prints the oldest conversations, among them ones older than the newest 256 that no save archived: %d", n)
	}
	napSendSources(t, st, scope, conversationIDs(first)...)
	second := decodeNap(t)
	if len(second.Conversations) == 0 || second.Conversations[0].ID != fmt.Sprintf("%016x", n+1) {
		t.Fatalf("the next run goes on after the conversations the agent sent: %v", conversationIDs(second))
	}
	// An old conversation that no run printed stays refused.
	out, err := runReflect(st.Dir, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{{Sources: []string{fmt.Sprintf("%016x", 140)}}}})
	if res, ok := out.(reflectResult); err != nil || !ok || res.Complete || len(res.Wiki) != 1 || res.Wiki[0].Error == "" {
		t.Fatalf("an id the nap did not print is refused as before (its conversation is in the record, but not in the newest 256): %+v %v", out, err)
	}
}

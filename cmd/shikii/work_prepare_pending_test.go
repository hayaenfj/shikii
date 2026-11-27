package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// These tests stand for the one test of 0.43.2 that was removed (TestAuditACLIPrepareWithManyPendingConversations, 42 conversations
// made by the real hook). It failed only when two mistakes were made together: prepare gave a conversation that comes as a head its
// whole text, and the last resort of limitWorkPreparation no longer left pending and observations out. Neither mistake alone was
// caught by any test. Each is caught here alone, with the conversations written straight into the project's feedback (the same
// records the hook writes at Stop) so that 42 of them cost a few milliseconds.

// pendingPrepareFixture makes a project with n conversations waiting to be sorted into the wiki, oldest first. Every request is
// longer than reflectHeadBytes, so what a head cuts is visible.
func pendingPrepareFixture(t *testing.T, n int) (root, scope string) {
	t.Helper()
	repo := gitRepo(t)
	root = hookEnv(t, nil, repo)
	t.Chdir(repo)
	scope = feedbackScope(repo)
	st := store.Store{Dir: root}
	start := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < n; i++ {
		request := fmt.Sprintf("Conversation %d. ", i) + strings.Repeat("Update the README setup steps for a machine with no project yet. ", 10)
		feedback := store.Feedback{ID: fmt.Sprintf("%016x", i+1), Request: request, Summary: "The setup steps are updated.", Reply: "Thanks, that is what I asked for.", At: start.Add(time.Duration(i) * time.Second)}
		if err := st.AppendFeedback(scope, feedback); err != nil {
			t.Fatal(err)
		}
	}
	return root, scope
}

// pendingPrepare is the CLI's work prepare with the given JSON on standard input: the reply as the agent's shell shows it, and
// decoded. A refusal comes back as err, with the reply empty.
func pendingPrepare(t *testing.T, input map[string]any) (string, map[string]any, error) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var out string
	withStdin(t, string(data), func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
	if err != nil {
		return out, nil, err
	}
	var reply map[string]any
	if e := json.Unmarshal([]byte(out), &reply); e != nil {
		t.Fatalf("%v: %.200s", e, out)
	}
	return out, reply, nil
}

func pendingList(t *testing.T, reply map[string]any) []map[string]any {
	t.Helper()
	list, _ := reply["pending"].([]any)
	entries := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		e, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("a pending entry is an object: %v", raw)
		}
		entries = append(entries, e)
	}
	return entries
}

// The oldest reflectRawSources pending conversations come whole, with an alias (source1, source2) an agent can name in a record.
// The ones after them come as heads: the text is cut to reflectHeadBytes and they carry no alias and no source, because a
// conversation seen only in part is never judged ("do not judge a shortened source"). Before, nothing checked a head in the reply
// of prepare: with the conversation of the third place given whole, with its alias, no test failed, and reviewed:["source3"] marked
// a conversation as sorted that the agent had read only 200 bytes of.
func TestPrepareGivesAPendingConversationAfterTheOldestTwoAsAHeadOnly(t *testing.T) {
	pendingPrepareFixture(t, reflectRawSources+reflectHeadsMax)
	// the largest max_bytes, so that no bound changes what pending holds: the shape of each entry is what is checked here
	prepare := map[string]any{"query": "README", "request": "Update the README", "max_bytes": workSourceBudget * 3}
	_, reply, err := pendingPrepare(t, prepare)
	if err != nil {
		t.Fatal(err)
	}
	pending := pendingList(t, reply)
	if len(pending) != reflectRawSources+reflectHeadsMax {
		t.Fatalf("precondition: every waiting conversation is listed (%d of %d), oldest first", len(pending), reflectRawSources+reflectHeadsMax)
	}
	var notHeads []int
	for i, e := range pending {
		wantRequest := fmt.Sprintf("Conversation %d. ", i)
		if request, _ := e["request"].(string); !strings.HasPrefix(request, wantRequest) {
			t.Fatalf("pending[%d] is not the conversation %d (oldest first): %.80s", i, i, request)
		}
		if i < reflectRawSources {
			if e["ref"] != "source"+strconv.Itoa(i+1) || e["source"] == nil {
				t.Errorf("pending[%d], one of the oldest %d, comes whole with its alias and its source: %.200v", i, reflectRawSources, e)
			}
			continue
		}
		request, _ := e["request"].(string)
		if e["head"] != true || e["ref"] != nil || e["source"] != nil || e["answer"] != nil || e["reply"] != nil || len(request) > reflectHeadBytes || len(request) == 0 {
			notHeads = append(notHeads, i)
		}
	}
	if len(notHeads) > 0 {
		t.Errorf("pending entries %v come as heads (head:true, no ref, no source, no answer or reply, a request of at most %d bytes); the first is %.300v", notHeads, reflectHeadBytes, pending[notHeads[0]])
	}
	// the head has no alias to record against: the alias the whole conversation would have had is refused, and the conversation
	// stays waiting
	first := reflectRawSources
	alias := "source" + strconv.Itoa(first+1)
	var saved string
	withStdin(t, fmt.Sprintf(`{"receipt":%q,"reviewed":[%q]}`, reply["receipt"], alias), func() {
		saved, err = captureErr(func() error { return cmdWork([]string{"record"}) })
	})
	var recorded map[string]any
	if e := json.Unmarshal([]byte(saved), &recorded); e == nil && recorded["complete"] == true {
		t.Errorf("reviewed:[%q] is accepted for a conversation that was given as a head: %.300s", alias, saved)
	}
	_, again, err := pendingPrepare(t, prepare)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(pendingList(t, again)); n != reflectRawSources+reflectHeadsMax {
		t.Errorf("the conversation of a head is still waiting after the refused record: %d listed, wanted %d", n, reflectRawSources+reflectHeadsMax)
	}
}

// When a reply does not fit its bound even after the lessons, the contexts and the reasoning are left out, what is left is the
// pending conversations and the observations: the last resort leaves them out too, and output.omitted names them (a conversation
// stays recoverable by reflect query). Before, nothing checked this step alone: without it the reply was refused with "exceed
// max_bytes" at a named bound, and on the CLI's default the call was repeated at 64 KiB, so an agent's shell, which shows 30,000
// characters, cut a reply of 39,000 bytes.
func TestPrepareLeavesPendingConversationsOutWhenTheyAloneMakeTheReplyExceedItsBound(t *testing.T) {
	pendingPrepareFixture(t, reflectRawSources+reflectHeadsMax)
	longRequest := strings.Repeat("Update the README. ", 800) // 15,200 bytes: a task is never cut, and it fits what the CLI keeps (16,000 at most)
	for _, c := range []struct {
		name     string
		request  string
		maxBytes int // 0: the CLI's own bound
		bound    int
	}{
		{"the CLI's own bound with a long request", longRequest, 0, cliPrepareBytes},
		{"the smallest max_bytes", "Update the README", workMinBudget, workMinBudget},
	} {
		t.Run(c.name, func(t *testing.T) {
			input := map[string]any{"query": "README", "request": c.request}
			if c.maxBytes > 0 {
				input["max_bytes"] = c.maxBytes
			}
			// precondition: the pending conversations are what makes the reply too large, not the task
			whole, wholeReply, err := pendingPrepare(t, map[string]any{"query": "README", "request": c.request, "max_bytes": workSourceBudget * 3})
			if err != nil || len(whole) <= c.bound || len(pendingList(t, wholeReply)) != reflectRawSources+reflectHeadsMax {
				t.Fatalf("precondition: with the largest max_bytes the reply holds all %d conversations and is over %d bytes: %d bytes, %d listed, err=%v", reflectRawSources+reflectHeadsMax, c.bound, len(whole), len(pendingList(t, wholeReply)), err)
			}
			out, reply, err := pendingPrepare(t, input)
			if err != nil {
				t.Fatalf("the reply is made to fit by leaving the pending conversations out, and is not refused: %v", err)
			}
			if len(out) > c.bound {
				t.Errorf("the reply is %d bytes, over its bound of %d", len(out), c.bound)
			}
			output, _ := reply["output"].(map[string]any)
			if reply["pending"] != nil || output == nil || output["limited"] != true || !slices.Contains(toStrings(output["omitted"]), "pending") {
				t.Errorf("the reply leaves pending out and output.omitted names it: pending=%v output=%v", reply["pending"], output)
			}
			for _, key := range []string{"receipt", "task", "how_to_record"} {
				if reply[key] == nil {
					t.Errorf("the reply keeps %s", key)
				}
			}
		})
	}
}

// The observations are the other half of the last resort. Two of them are listed at most, each below the size from which prepare
// replaces an entry by a pointer to its source, so what is left to leave out is the list. The request is chosen so that the reply
// fits at the smallest bound without them and not with them.
func TestPrepareLeavesPendingObservationsOutWhenTheyAloneMakeTheReplyExceedItsBound(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	for i := 0; i < reflectRawSources; i++ {
		content := fmt.Sprintf("Observation %d. ", i) + strings.Repeat("The export keeps its filters. ", 30)
		if _, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: fmt.Sprintf("Synthetic check %d", i), Content: content, Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	request := strings.Repeat("Update the README. ", 250) // 4,750 bytes
	whole, wholeReply, err := pendingPrepare(t, map[string]any{"query": "README", "request": request, "max_bytes": workSourceBudget * 3})
	observations, _ := wholeReply["observations"].([]any)
	if err != nil || len(observations) != reflectRawSources || len(whole) <= workMinBudget {
		t.Fatalf("precondition: with the largest max_bytes the reply lists %d observations and is over %d bytes: %d bytes, %d listed, err=%v", reflectRawSources, workMinBudget, len(whole), len(observations), err)
	}
	for i, raw := range observations {
		if e, _ := raw.(map[string]any); e["ref"] == nil || e["source"] == nil {
			t.Fatalf("precondition: observations[%d] comes whole, with its ref: %.200v", i, raw)
		}
	}
	out, reply, err := pendingPrepare(t, map[string]any{"query": "README", "request": request, "max_bytes": workMinBudget})
	if err != nil {
		t.Fatalf("the reply is made to fit by leaving the observations out, and is not refused: %v", err)
	}
	output, _ := reply["output"].(map[string]any)
	if len(out) > workMinBudget || reply["observations"] != nil || output == nil || !slices.Contains(toStrings(output["omitted"]), "observations") {
		t.Errorf("the reply is within %d bytes, leaves observations out and output.omitted names it: %d bytes, output=%v", workMinBudget, len(out), output)
	}
}

func toStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, raw := range list {
		if s, ok := raw.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The notice of a request tells the agent to end with one work record. Two things the hook knows when it writes the notice used to be
// asked for afterwards, each in a second record of the same request: continues_previous (the Stop asks for it when the message is
// short and not an acknowledgement) and the assessment of the quotes delivered before the request (the reply of the first record
// lists them in lessons_to_assess). The notice now carries both, in the form the record takes (hook_ending.go). These tests send
// the record the notice describes through the command line, MCP and HTTP, and check that nothing is left to ask for.

const (
	endingFirst = "Rename the export button to Export."
	endingShort = "Use tabs there."                                                                // under 40 characters, not an acknowledgement
	endingLong  = "Use tabs for the indentation of the export code and keep the existing filters." // 40 characters or more
)

// endingFlow is one way the hook delivers a request: the planning notice or the reasoning notice, with the work prepared by the hook
// or left for the agent to prepare (the notice the hook writes when the preparation fails).
type endingFlow struct {
	name     string
	planning bool // false: a request that needs no planning judgment
	prepared bool // false: the hook does not prepare the work
}

var endingFlows = []endingFlow{
	{"planning notice, work prepared by the hook", true, true},
	{"planning notice, work left to the agent", true, false},
	{"reasoning notice, work prepared by the hook", false, true},
	{"reasoning notice, work left to the agent", false, false},
}

type endingFixture struct {
	repo, root string
	flow       endingFlow
	h          http.Handler
	book       notebook.Book
}

// endingSetup makes a project in which the words "use tabs" find two lessons, and sends the first request of a session and ends it, so
// that the next request has a request before it.
func endingSetup(t *testing.T, flow endingFlow) *endingFixture {
	t.Helper()
	var repo, root string
	if flow.planning {
		repo, root, _ = planningFixture(t)
	} else {
		repo = gitRepo(t)
		root = hookEnv(t, fakeAgentModel(t, func(string) float64 { return .9 }), repo)
	}
	t.Chdir(repo)
	f := &endingFixture{repo: repo, root: root, flow: flow, h: newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)}), book: notebook.Book{Root: root, Scope: feedbackScope(repo)}}
	for _, l := range [][3]string{
		{"Indent with tabs", "Use tabs, not spaces, to indent the export code.", "use tabs when indenting the export code"},
		{"Tabs in generated files", "Use tabs there too: generated export files keep their tabs.", "use tabs in generated export files"},
		// two lessons on other topics, so that a word the two tab lessons share is rare among the pages (a word in more than half of them
		// does not deliver a lesson by itself)
		{"Name release branches", "Name a release branch after its version.", "naming a release branch"},
		{"Sign commits", "Sign every commit that is pushed.", "signing a commit before a push"},
	} {
		source, err := f.book.CaptureObserved(notebook.Observation{Kind: "test", Title: l[0] + " evidence", Content: "A synthetic check.", Reference: "synthetic", Version: "ending", ObservedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.book.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: l[0], Lesson: l[1], Kind: "rule", Scene: "when editing the export", AppliesTo: l[2]}); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// request sends a request of the session through the hook (the way the flow delivers it) and returns the context the agent is given.
func (f *endingFixture) request(t *testing.T, prompt string) string {
	t.Helper()
	in := map[string]any{"session_id": "ending", "cwd": f.repo, "prompt": prompt}
	var context string
	if f.flow.prepared {
		context = feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", in))
	} else {
		context = feedbackContext(t, runManualHook(t, "user-prompt-submit", in))
	}
	// the flow is the one the test names: the notice that replaces the preparation is in the context only when the hook prepared the work
	if got := strings.Contains(context, "shikii prepared work:\n"); got != f.flow.prepared {
		t.Fatalf("precondition: the hook prepared the work: %v, the flow says %v\n%.600s", got, f.flow.prepared, context)
	}
	return context
}

// first sends the first request of the session, whose words find no lesson, and ends it.
func (f *endingFixture) first(t *testing.T) string {
	t.Helper()
	context := f.request(t, endingFirst)
	runHook(t, "stop", map[string]any{"session_id": "ending", "cwd": f.repo, "last_assistant_message": "Done."})
	return context
}

// stop sends the Stop of the session's request with no record of the work and returns the reason it gives for not letting the turn end.
func (f *endingFixture) stop(t *testing.T) string {
	t.Helper()
	reason, _ := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "ending", "cwd": f.repo, "last_assistant_message": "Done."}))
	return reason
}

// quoteKeys are the keys of the quotes the context shows the agent, in the order shown.
func quoteKeys(t *testing.T, context string) []string {
	t.Helper()
	at := strings.Index(context, memoryPrefix)
	if at < 0 {
		return nil
	}
	line, _, _ := strings.Cut(context[at+len(memoryPrefix):], "\n")
	var quotes []struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(line), &quotes); err != nil {
		t.Fatalf("the quotes the context shows are not JSON: %v\n%s", err, line)
	}
	keys := []string{}
	for _, q := range quotes {
		keys = append(keys, q.Key)
	}
	return keys
}

// memoryHint is the sentence of the notice that gives the memory form, and only that sentence (it ends at "as text).", where
// memoryHintEnd is the last of what endingHints.text writes for it), "" when the notice has none.
func memoryHint(context string) string {
	_, rest, found := strings.Cut(context, "With that record send memory:[")
	if !found {
		return ""
	}
	sentence, _, found := strings.Cut(rest, memoryHintEnd)
	if !found {
		return ""
	}
	return "With that record send memory:[" + sentence + memoryHintEnd
}

// memoryHintEnd is how the memory sentence of the hints ends (hook_ending.go).
const memoryHintEnd = "as text)."

// The quotes of a request are delivered right after its notice is written; the notice then names them. Every notice the hook writes does:
// the planning one and the reasoning one, with the work prepared or not. A request no quote reaches gets no memory sentence.
func TestTheNoticeNamesTheQuotesDeliveredBeforeTheRequest(t *testing.T) {
	for _, flow := range endingFlows {
		t.Run(flow.name, func(t *testing.T) {
			f := endingSetup(t, flow)
			noQuotes := f.first(t)
			if keys := quoteKeys(t, noQuotes); len(keys) != 0 {
				t.Fatalf("precondition: the first request's words find no lesson: %v", keys)
			}
			if memoryHint(noQuotes) != "" || strings.Contains(noQuotes, "memory:[") || strings.Contains(noQuotes, "With that record") {
				t.Errorf("a request that is delivered no quote gets no memory sentence:\n%s", noQuotes)
			}
			context := f.request(t, endingShort)
			keys := quoteKeys(t, context)
			if len(keys) != 2 {
				t.Fatalf("precondition: the words of the request find both lessons: %v\n%s", keys, context)
			}
			// the keys of the delivery the store saved for this request are the ones the notice names, in the order they are shown
			turn := loadSession(f.root, "ending").TurnID
			deliveries, err := (store.Store{Dir: f.root}).MemoryDeliveries(feedbackScope(f.repo))
			if err != nil {
				t.Fatal(err)
			}
			saved := []string{}
			for _, d := range deliveries {
				if d.ID == turn {
					for _, q := range d.Quotes {
						saved = append(saved, q.Key)
					}
				}
			}
			if strings.Join(saved, ",") != strings.Join(keys, ",") {
				t.Fatalf("precondition: the context shows the keys of the delivery the store saved: shown %v, saved %v", keys, saved)
			}
			want := fmt.Sprintf(`memory:[{"ref":%q,"result":"..."},{"ref":%q,"result":"..."}] for the quotes delivered to you before this request, replacing each "..." with helpful, unused, harmful or unknown`, keys[0], keys[1])
			if got := memoryHint(context); !strings.Contains(got, want) {
				t.Errorf("the notice gives the memory form with both keys, as the quotes show them:\n%s\nwant %s", got, want)
			}
			// the sentence stands in the notice's own paragraph, before the line with the ids, once
			if strings.Count(context, "With that record send memory:[") != 1 {
				t.Errorf("the memory sentence is in the context once:\n%s", context)
			}
		})
	}
}

// The notice asks for continues_previous exactly when the Stop would: the message is short and not a bare acknowledgement, there is a
// request before it, and the request is one the hook asks the agent to organize (a request that needs no planning judgment has no
// grouping to settle). The Stop of the same turn is the check: it asks for continues_previous in these cases, and in no other.
func TestTheNoticeAsksForContinuesPreviousExactlyWhenTheStopWould(t *testing.T) {
	const groupingSentence = `The person's message is short and not a bare acknowledgement, so it is not known whether it continues the previous request: add "agreed":{"unknown":true},"open":{"unknown":true} and "continues_previous":true or false to that record (true when it only continues or answers the previous request and asks for nothing new).`
	for _, flow := range endingFlows {
		for _, c := range []struct {
			name, prompt string
			previous     bool
			hinted       bool
		}{
			{"a short message after a request", endingShort, true, flow.planning},
			{"a bare acknowledgement", "ok", true, false},
			{"a long message", endingLong, true, false},
			{"a short message with no request before it", endingShort, false, false},
		} {
			t.Run(flow.name+": "+c.name, func(t *testing.T) {
				f := endingSetup(t, flow)
				if c.previous {
					f.first(t)
				}
				context := f.request(t, c.prompt)
				if got := strings.Contains(context, groupingSentence); got != c.hinted {
					t.Errorf("the notice has the continues_previous sentence: %v, want %v\n%s", got, c.hinted, context)
				}
				if !c.hinted && strings.Contains(context, "continues_previous") {
					t.Errorf("a notice without the sentence does not name continues_previous at all:\n%s", context)
				}
				// the Stop of this turn asks for continues_previous in the same cases
				reason := f.stop(t)
				if got := strings.Contains(reason, "continues_previous"); got != c.hinted {
					t.Errorf("the Stop asks for continues_previous: %v, but the notice's sentence is %v\nthe Stop's reason: %s", got, c.hinted, reason)
				}
			})
		}
	}
}

// The notice asks for continues_previous by the Stop's own test (groupingAsk), and the Stop adds to it what the notice cannot see:
// the plan block is for a turn that is not reasoning only, and a turn the agent has prepared itself needs no question at its end. The
// same turn, in the same states, is put to both: the notice's test on the state the notice is written for, and the Stop on the state at
// the end. They agree wherever the state is the same; the one difference is in time (an agent that prepares after the notice was
// written), and there the notice's sentence is still what lets the first record carry the answer.
func TestTheNoticeAndTheStopAskForContinuesPreviousInTheSameStates(t *testing.T) {
	for _, flow := range []endingFlow{{"planning notice, work prepared by the hook", true, true}, {"planning notice, work left to the agent", true, false}} {
		for _, c := range []struct {
			name   string
			change func(*hookSession)
			notice bool // the notice's test on the state
			stop   bool // the Stop on the state
		}{
			{"as the hook left it after a short message", func(*hookSession) {}, true, true},
			{"the decision says no planning is needed (reasoning only, with the grouping still open)", func(s *hookSession) { s.Planning.ReasoningOnly = true }, false, false},
			{"the grouping was settled", func(s *hookSession) { s.Planning.Grouping.Settled = true }, false, false},
			{"the work was recorded", func(s *hookSession) { s.Planning.Result = &planningReceipt{} }, false, false},
			// the difference in time: the agent prepares after the notice was written, and the Stop no longer asks; the notice did, for
			// the record that ends the work, and the answer reaches the same place
			{"the agent prepared the work itself afterwards", func(s *hookSession) {
				s.Planning.Work = nil
				s.Reasoning.PreparedAt = time.Now()
			}, true, false},
		} {
			t.Run(flow.name+": "+c.name, func(t *testing.T) {
				f := endingSetup(t, flow)
				f.first(t)
				f.request(t, endingShort)
				if err := updateSession(f.root, "ending", c.change); err != nil {
					t.Fatal(err)
				}
				if got := groupingAsk(loadSession(f.root, "ending")); got != c.notice {
					t.Errorf("the notice's test on this state: %v, want %v", got, c.notice)
				}
				if got := strings.Contains(f.stop(t), "continues_previous"); got != c.stop {
					t.Errorf("the Stop asks for continues_previous: %v, want %v", got, c.stop)
				}
			})
		}
	}
}

// sendWork sends a work operation the way one entry does and returns the reply. The three entries an agent saves through take the same
// work object: the command line reads it from standard input, MCP and HTTP take it under work beside the operation.
type sendWork func(t *testing.T, f *endingFixture, operation string, work map[string]any) map[string]any

var endingEntries = map[string]sendWork{
	"the command line": func(t *testing.T, f *endingFixture, operation string, work map[string]any) map[string]any {
		data, err := json.Marshal(work)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := agentCLI(t, string(data), "work", operation)
		if err != nil {
			t.Fatalf("work %s: %v\n%s", operation, err, data)
		}
		return reply
	},
	"MCP": func(t *testing.T, f *endingFixture, operation string, work map[string]any) map[string]any {
		data, err := json.Marshal(map[string]any{"operation": operation, "work": work})
		if err != nil {
			t.Fatal(err)
		}
		result, _ := mcpToolCall(t, f.h, "shikii_wiki", string(data))
		reply, _ := result["structuredContent"].(map[string]any)
		if reply == nil {
			t.Fatalf("the MCP reply has no structured content: %v", result)
		}
		return reply
	},
	"HTTP": func(t *testing.T, f *endingFixture, operation string, work map[string]any) map[string]any {
		data, err := json.Marshal(map[string]any{"operation": operation, "work": work})
		if err != nil {
			t.Fatal(err)
		}
		code, body, err := callTool(f.h, "shikii_wiki", data)
		if err != nil || code >= 400 {
			t.Fatalf("HTTP work %s: %d %v %s", operation, code, err, body)
		}
		var reply map[string]any
		if err := json.Unmarshal(body, &reply); err != nil {
			t.Fatalf("the HTTP reply is not JSON: %v\n%s", err, body)
		}
		return reply
	},
}

// recordTheNoticeDescribes builds, from the notice alone, the work object of the one record it asks the agent to end with: the ids
// the notice carries, a result, the memory form with each "..." replaced by a result (the first helpful, with the decision and the
// evidence that result takes, the others unused) and, when the notice has the sentence, the fields it names with continues_previous
// true. It fails when the notice gives a form that cannot be read as JSON.
func recordTheNoticeDescribes(t *testing.T, context string) (work map[string]any, withGrouping bool) {
	t.Helper()
	if err := json.Unmarshal([]byte(noticeIDs(t, context)), &work); err != nil {
		t.Fatal(err)
	}
	work["result"] = map[string]any{"changed": "Nothing.", "checked": "Read the export."}
	_, rest, found := strings.Cut(context, "With that record send memory:")
	if !found {
		t.Fatalf("the notice gives no memory form:\n%s", context)
	}
	var form []map[string]any
	if err := json.NewDecoder(strings.NewReader(rest)).Decode(&form); err != nil {
		t.Fatalf("the memory form the notice gives is not JSON: %v\n%.300s", err, rest)
	}
	for i, entry := range form {
		if entry["result"] != "..." || len(entry) != 2 {
			t.Fatalf("each entry of the form is {ref, result:\"...\"}: %v", entry)
		}
		entry["result"] = "unused"
		if i == 0 {
			entry["result"], entry["decision"], entry["evidence"] = "helpful", "Used tabs to indent the export code.", "The synthetic check showed tabs in the indentation."
		}
	}
	work["memory"] = form
	if at := strings.Index(context, `"agreed":{"unknown":true}`); at >= 0 {
		fields, _, found := strings.Cut(context[at:], ` and "continues_previous"`)
		if !found {
			t.Fatalf("the sentence names continues_previous after the fields it adds:\n%s", context)
		}
		var named map[string]any
		if err := json.Unmarshal([]byte("{"+fields+"}"), &named); err != nil {
			t.Fatalf("the fields the sentence names are not JSON: %v: %s", err, fields)
		}
		for k, v := range named {
			work[k] = v
		}
		work["continues_previous"] = true
		withGrouping = true
	}
	return work, withGrouping
}

// The one record the notice describes is saved through every entry, and nothing is left to ask for: the reply lists no lesson to
// assess, the assessments are saved on the delivery of the request, and the Stop that follows does not ask for continues_previous
// or anything else. Before, the same request sent a second record for each of them.
func TestTheRecordTheNoticeDescribesLeavesNothingToAskFor(t *testing.T) {
	for _, flow := range endingFlows {
		for entryName, send := range endingEntries {
			t.Run(flow.name+" through "+entryName, func(t *testing.T) {
				f := endingSetup(t, flow)
				f.first(t)
				context := f.request(t, endingShort)
				work, withGrouping := recordTheNoticeDescribes(t, context)
				if withGrouping != flow.planning {
					t.Fatalf("precondition: the notice asks for continues_previous in a planning request only: %v", withGrouping)
				}
				keys := quoteKeys(t, context)
				turn := loadSession(f.root, "ending").TurnID
				if !flow.prepared {
					// the agent prepares by the ids, and records with the receipt it gets
					prepared := send(t, f, "prepare", map[string]any{"session_id": work["session_id"], "turn_id": work["turn_id"], "query": "use tabs"})
					delete(work, "session_id")
					delete(work, "turn_id")
					work["receipt"] = prepared["receipt"]
				}
				reply := send(t, f, "record", work)
				if reply["complete"] != true || reply["errors"] != nil {
					t.Fatalf("the record the notice describes is saved whole: %v", reply)
				}
				if left, listed := reply["lessons_to_assess"]; listed {
					t.Errorf("no lesson is left to assess after that record: %v", left)
				}
				if next, _ := reply["next_action"].(string); strings.Contains(next, "lessons_to_assess") {
					t.Errorf("the reply does not send the agent to a second record: %q", next)
				}
				got := assessed(t, store.Store{Dir: f.root}, feedbackScope(f.repo))
				if got[turn+" "+keys[0]] != "helpful" || got[turn+" "+keys[1]] != "unused" {
					t.Errorf("both quotes are assessed on the delivery of the request, with the results sent: %v", got)
				}
				s := loadSession(f.root, "ending")
				if flow.planning && (s.Planning.Result == nil || !s.Planning.Grouping.Settled || !s.Planning.Grouping.Joined) {
					t.Errorf("the record settles the grouping with continues_previous true")
				}
				if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "ending", "cwd": f.repo, "last_assistant_message": "Done."})); blocked || strings.Contains(reason, "continues_previous") {
					t.Errorf("the Stop after that record lets the turn end (blocked %v): %s", blocked, reason)
				}
			})
		}
	}
}

// What the notice cannot name is still asked for afterwards. A record that leaves the memory form out gets both quotes in
// lessons_to_assess (so the check above does not pass because nothing is ever listed), and a lesson that arrives right before a
// command, which the hook does not know when it writes the notice, is listed after a record that assessed the quotes of the request.
func TestTheReplyStillListsTheLessonsTheNoticeDidNotName(t *testing.T) {
	flow := endingFlows[0]
	listed := func(reply map[string]any) []string {
		left, _ := reply["lessons_to_assess"].(map[string]any)
		refs := []string{}
		for _, ref := range asList(left["refs"]) {
			refs = append(refs, fmt.Sprint(ref))
		}
		return refs
	}
	t.Run("a record that leaves the memory form out", func(t *testing.T) {
		f := endingSetup(t, flow)
		f.first(t)
		context := f.request(t, endingShort)
		work, _ := recordTheNoticeDescribes(t, context)
		delete(work, "memory")
		reply := endingEntries["MCP"](t, f, "record", work)
		got, want := listed(reply), quoteKeys(t, context)
		sort.Strings(got) // the reply lists the two quotes of one delivery in the order of its own
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("the reply lists both quotes of the request: %v, want %v", got, want)
		}
		if next, _ := reply["next_action"].(string); !strings.Contains(next, lessonsToAssessNext) {
			t.Errorf("the reply sends the agent to the record that assesses them: %q", next)
		}
	})
	t.Run("a lesson right before a command", func(t *testing.T) {
		f := endingSetup(t, flow)
		f.first(t)
		context := f.request(t, endingShort)
		turn := loadSession(f.root, "ending").TurnID
		atTool := "lesson:0123456789abcdef:fedcba9876543210"
		if err := (store.Store{Dir: f.root}).SaveMemoryDelivery(store.MemoryDelivery{ID: "aaaaaaaaaaaaaaaa", Scope: feedbackScope(f.repo), Request: endingShort, Parent: turn, Tool: "Bash make test", Phrase: "make test",
			Quotes: []store.MemoryQuote{{Key: atTool, ID: "0123456789abcdef", Revision: "fedcba9876543210", Lesson: "Run make test once before a commit.", AppliesTo: "make test", Sources: []string{"0000000000000001"}}}, At: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		work, _ := recordTheNoticeDescribes(t, context)
		reply := endingEntries["MCP"](t, f, "record", work)
		if got := listed(reply); len(got) != 1 || got[0] != atTool {
			t.Errorf("the reply lists the lesson that came right before a command, and not the quotes the record assessed: %v", got)
		}
	})
}

// The notice with both hints, a short request and a preparation too large to fit, stays within what the agent is shown: the notice is
// part of the context the preparation counts, so what does not fit is left out of the preparation.
func TestTheNoticeWithItsHintsStaysWithinTheContextLimit(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runManualHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": endingFirst})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	for i := 0; i < 6; i++ {
		interimAddArticle(t, b, fmt.Sprintf("Use tabs there %d", i), strings.Repeat("Use tabs there to indent the export code and the generated files. ", 40))
	}
	context, prepared := preparedHookWork(t, repo, endingShort) // fails when the context is over hookContextMax
	if prepared == nil {
		t.Fatalf("the hook delivered no prepared work: %.300s", context)
	}
	if output, _ := prepared["output"].(map[string]any); output == nil || output["limited"] != true {
		t.Fatal("precondition: the preparation does not all fit, so the bound decides where it stops")
	}
	if len(quoteKeys(t, context)) != 2 || memoryHint(context) == "" || !strings.Contains(context, `"continues_previous":true or false`) {
		t.Fatalf("precondition: the notice carries both hints:\n%.1500s", context)
	}
	// the whole context is the notice with its hints and then the preparation, filled up to the bound
	if n := hookContextLength(context); n > hookContextMax || n < hookContextMax-1500 {
		t.Errorf("the context is %d characters: within %d, and the preparation fills what the notice leaves", n, hookContextMax)
	}
}

// The sentences the hints add stay short, measured at the most the notice can carry: two quotes (a delivery has at most two, and a
// lesson's key is the longest key a quote has) and the grouping sentence. The widest measured is 342 bytes for the memory sentence and
// 320 for the grouping sentence; the limits leave a few words of room. What a notice grows by is counted without the command's path,
// which is the install's and not the sentence's.
func TestTheSentencesTheHintsAddStayShort(t *testing.T) {
	const maxMemory, maxGrouping, maxAdded = 350, 330, 680
	widest := endingHints{quotes: []string{"lesson:0123456789abcdef:fedcba9876543210", "lesson:fedcba9876543210:0123456789abcdef"}, grouping: true}
	memoryOnly, groupingOnly := endingHints{quotes: widest.quotes}.text(), endingHints{grouping: true}.text()
	if n := len(memoryOnly); n > maxMemory {
		t.Errorf("the memory sentence is %d bytes, over %d: %s", n, maxMemory, memoryOnly)
	}
	// memoryHint cuts the sentence out of a notice at its last words: the measure in TestMemoryDeliversRelevantLessonOncePerRevision
	// leaves out the memory sentence and nothing after it
	if !strings.HasSuffix(memoryOnly, memoryHintEnd) || strings.Count(memoryOnly, memoryHintEnd) != 1 {
		t.Errorf("the memory sentence ends with %q, which memoryHint cuts at: %s", memoryHintEnd, memoryOnly)
	}
	if n := len(groupingOnly); n > maxGrouping {
		t.Errorf("the grouping sentence is %d bytes, over %d: %s", n, maxGrouping, groupingOnly)
	}
	none := endingHints{}
	for name, grown := range map[string]int{
		"planning":  len(planningNoticeWith("session", "0123456789abcdef", widest)) - len(planningNoticeWith("session", "0123456789abcdef", none)),
		"reasoning": len(reasoningNoticeWith("session", "0123456789abcdef", widest)) - len(reasoningNoticeWith("session", "0123456789abcdef", none)),
		"prepared":  len(hookPreparedNotice("session", "0123456789abcdef", widest)) - len(hookPreparedNotice("session", "0123456789abcdef", none)),
	} {
		if grown > maxAdded {
			t.Errorf("the %s notice grows by %d bytes with both hints, over %d", name, grown, maxAdded)
		}
	}
	// negative: the limits can be exceeded. A third quote would put the memory sentence over its limit, and a delivery of three quotes
	// is not one the store takes, so there is no wider notice than the one measured
	if wider := (endingHints{quotes: append(append([]string{}, widest.quotes...), "lesson:0123456789abcdef:0123456789abcdef")}).text(); len(wider) <= maxMemory {
		t.Errorf("a third quote puts the memory sentence over its limit: %d bytes, limit %d", len(wider), maxMemory)
	}
	three := store.MemoryDelivery{ID: "0123456789abcdef", Scope: "/x", Request: "r", Quotes: []store.MemoryQuote{
		{Key: "lesson:0000000000000001:0000000000000001", ID: "0000000000000001", Revision: "0000000000000001", Lesson: "l", AppliesTo: "a", Sources: []string{"0000000000000001"}},
		{Key: "lesson:0000000000000002:0000000000000002", ID: "0000000000000002", Revision: "0000000000000002", Lesson: "l", AppliesTo: "a", Sources: []string{"0000000000000001"}},
		{Key: "lesson:0000000000000003:0000000000000003", ID: "0000000000000003", Revision: "0000000000000003", Lesson: "l", AppliesTo: "a", Sources: []string{"0000000000000001"}}}}
	if err := (store.Store{Dir: t.TempDir()}).SaveMemoryDelivery(three); err == nil || !strings.Contains(err.Error(), "needs a request, quotes, sources and a version") {
		t.Errorf("a delivery of three quotes is refused, so a notice never names more than two: %v", err)
	}
	if !regexp.MustCompile(`^lesson:[0-9a-f]{16}:[0-9a-f]{16}$`).MatchString(widest.quotes[0]) || len(widest.quotes[0]) != 40 {
		t.Errorf("the widest key is a lesson's, 40 bytes: %q", widest.quotes[0])
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// mcpToolCall sends one tools/call the way an MCP client does and returns the tool result with the size of the whole reply.
func mcpToolCall(t *testing.T, h http.Handler, name, arguments string) (map[string]any, int) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(arguments)})
	reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := reply["result"].(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("%s %s: %.400s", name, arguments, raw)
	}
	return result, len(raw)
}

// The reply of reflect query had no bound. In this project it reached 86 KB (the lessons close to the oldest conversations came
// with their whole bodies, 44 KB of it), and the agent's MCP client refused it: "result (53,830 characters) exceeds maximum allowed
// tokens" (2026-11-12). Through the CLI the agent's shell cut it in the middle. The reply is now made to fit what the agent's tool
// shows: the bound of work prepare (64 KiB for the whole MCP reply, 28 KiB on the CLI), the least useful part first: the bodies of
// the close lessons (their titles, lessons and exact queries stay), the details of the maintenance tasks, the examples of earlier
// corrections, the lessons at the end of the list, and last the raw text of the second conversation, which becomes a head like the
// ones after it. What the agent needs to submit stays: every conversation with its targets and the keys of its delivered lessons,
// and how_to_submit. output names what was left out. A reply that fits is returned as it was, and the HTTP endpoint is not bounded.
func TestTheReflectionQueryFitsWhatAnAgentsToolShows(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}

	// one pending conversation with a delivered lesson and one short lesson close to it: the reply fits and is not touched
	memoryEffectPrompt(t, scope, "s1")
	memoryEffectComplete(t, st, scope, "s1")
	small, _ := mcpToolCall(t, h, "shikii_reflect", `{"operation":"query"}`)
	fits := small["structuredContent"].(map[string]any)
	if fits["output"] != nil || len(fits["sources"].([]any)) == 0 {
		t.Fatalf("a reply that fits is returned as it is: %v", fits["output"])
	}
	for _, raw := range fits["lessons"].([]any) {
		if lesson := raw.(map[string]any); lesson["query"] != nil || lesson["body_included"] != nil {
			t.Errorf("a reply that fits carries its lessons whole: %v", lesson)
		}
	}

	// long lessons close to the conversation (a query carries the two closest lessons whole, each up to 16 KB of body)
	for i := 0; i < 4; i++ {
		body := fmt.Sprintf("README step %d. ", i) + strings.Repeat("Check the setup steps of the README on a machine that has no project yet. ", 200)
		if _, err := b.Ingest(notebook.Draft{Sources: draft.Sources, Title: fmt.Sprintf("README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3059\u308b %d", i), Lesson: "Check the README setup steps on a fresh machine.", AppliesTo: "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044", Kind: "rule", Scene: "\u30c6\u30b9\u30c8\u306e\u6642", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	whole := reflectCall(t, h, map[string]any{"operation": "query"}) // the HTTP endpoint: not bounded
	wholeText, _ := json.Marshal(whole)
	if unbounded := workContextSize(toolResult(200, wholeText, "shikii_reflect")); unbounded <= 64<<10 || len(wholeText) <= 28<<10 || whole["output"] != nil {
		t.Fatalf("precondition: the unbounded reply is over both bounds and the HTTP endpoint returns it whole: %d bytes as an MCP result, %d bytes of JSON", unbounded, len(wholeText))
	}
	keys := func(reply map[string]any) []string {
		var out []string
		for _, raw := range reply["sources"].([]any) {
			source := raw.(map[string]any)
			out = append(out, "source "+source["source_material"].(map[string]any)["id"].(string))
			for _, memory := range source["memories"].([]any) {
				m := memory.(map[string]any)
				out = append(out, "memory "+m["source_id"].(string)+" "+m["quote"].(map[string]any)["key"].(string))
			}
			for _, target := range source["targets"].([]any) {
				out = append(out, "target "+target.(map[string]any)["trace_id"].(string))
			}
		}
		return out
	}
	check := func(name string, reply map[string]any, size, bound int) {
		t.Helper()
		if size > bound {
			t.Errorf("%s: the reply is %d bytes, over %d", name, size, bound)
		}
		output, _ := reply["output"].(map[string]any)
		if output == nil || output["limited"] != true || fmt.Sprint(output["max_bytes"]) != fmt.Sprint(bound) || !strings.Contains(fmt.Sprint(output["omitted"]), "lessons[].body") || !strings.Contains(fmt.Sprint(output["next_action"]), "query") {
			t.Errorf("%s: output names the bound, what was left out and the way back: %v", name, output)
		}
		if reply["how_to_submit"] != whole["how_to_submit"] || fmt.Sprint(keys(reply)) != fmt.Sprint(keys(whole)) || len(keys(reply)) < 2 {
			t.Errorf("%s: what the agent needs to submit stays (every conversation, target and delivered lesson, and how_to_submit): %v", name, keys(reply))
		}
		lessons, _ := reply["lessons"].([]any)
		if len(lessons) == 0 {
			t.Errorf("%s: the close lessons stay by their titles", name)
			return
		}
		for _, raw := range lessons {
			lesson := raw.(map[string]any)
			if lesson["body"] != nil || lesson["title"] == nil || lesson["lesson"] == nil || lesson["id"] == nil || lesson["revision"] == nil || lesson["body_included"] != false {
				t.Fatalf("%s: a close lesson keeps its title, lesson and revision, without its body: %.300v", name, lesson)
			}
		}
		// the exact query of a lesson reads the body that was left out
		read := false
		for _, raw := range lessons {
			lesson := raw.(map[string]any)
			if lesson["lesson"] != "Check the README setup steps on a fresh machine." {
				continue
			}
			query, _ := json.Marshal(lesson["query"])
			code, body, err := callTool(h, "shikii_wiki", query)
			if err != nil || code != 200 || !strings.Contains(string(body), "Check the setup steps of the README on a machine that has no project yet.") {
				t.Errorf("%s: the query of a lesson reads its body: %d %.200s %v", name, code, body, err)
			}
			read = true
		}
		if !read {
			t.Errorf("%s: a long lesson close to the conversation is among the lessons: %.300v", name, lessons)
		}
	}
	result, size := mcpToolCall(t, h, "shikii_reflect", `{"operation":"query"}`)
	check("MCP", result["structuredContent"].(map[string]any), size, 64<<10)
	if text := result["content"].([]map[string]any)[0]["text"].(string); !strings.Contains(text, `"limited":true`) {
		t.Errorf("the MCP text carries output too: %.200s", text)
	}
	var cli map[string]any
	out := capture(t, func() error { return cmdReflect([]string{"query"}) })
	if err := json.Unmarshal([]byte(out), &cli); err != nil {
		t.Fatalf("%v: %.200s", err, out)
	}
	check("the CLI", cli, len(out), 28<<10)
}

// Every step of the bound, on a synthetic reply whose parts are each long: as the bound shrinks, the parts go in the order of
// their use to the agent (lesson bodies, maintenance details, earlier corrections, the lessons at the end of the list, the raw text
// of the second conversation), nothing taken off comes back while a less useful part is still there, and what submit needs is in
// every reply: both conversations, their targets, the keys of their deliveries and how_to_submit. The first conversation is never
// cut. When nothing more can go, the reply says that it is over the bound.
func TestTheReflectionBoundTakesTheLeastUsefulPartsFirst(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	view := reflectView{HowToSubmit: "submit reviews, wiki and memory", Corrections: []any{map[string]any{"ai_reason": long(1500)}},
		Sources: []reflectSource{
			{Source: store.Feedback{ID: "aaaaaaaaaaaaaaaa", Request: "first " + long(700), Summary: long(700), Reply: "ok"}, Targets: []reviewTarget{{Spec: "done-as-asked", TraceID: "1111111111111111"}},
				Memories: []store.MemoryObservation{{SourceID: "aaaaaaaaaaaaaaaa", Quote: store.MemoryQuote{Key: "lesson:1:1", ID: "1", Lesson: long(400)}}}},
			{Source: store.Feedback{ID: "bbbbbbbbbbbbbbbb", Request: "second " + long(700), Summary: long(2500), Reply: "ok"}, Targets: []reviewTarget{{Spec: "done-as-asked", TraceID: "2222222222222222"}},
				Memories: []store.MemoryObservation{{SourceID: "cccccccccccccccc", Quote: store.MemoryQuote{Key: "lesson:2:2", ID: "2", Lesson: long(400)}}}},
		},
		Maintenance: []notebook.MaintenanceTask{{PageID: "0123456789abcdef", Revision: "fedcba9876543210", Key: "k", Title: "A page to review", Reasons: []string{long(1500)}, NextAction: "Review it."}},
	}
	for i := 0; i < 6; i++ {
		view.Lessons = append(view.Lessons, notebook.Page{ID: fmt.Sprintf("%016d", i), Revision: fmt.Sprintf("%016d", i+100), Title: fmt.Sprintf("Lesson %d", i), Lesson: long(150), AppliesTo: long(100), Kind: "rule", Scene: "always", Body: long(3000)})
	}
	whole := workContextSize(view)
	if got, ok := limitReflectQuery(view, whole, workContextSize).(reflectView); !ok || len(got.Lessons) != 6 {
		t.Fatalf("a reply that fits is returned as it is")
	}
	steps := []string{"lessons[].body", "maintenance[] details", "human_corrections", "lessons after the first", "the raw text of the last 1 of the 2 conversations", "still over max_bytes"}
	reached := 0 // how far down the steps the replies have gone
	for bound := whole - 1; bound > 800; bound -= 150 {
		raw, _ := json.Marshal(limitReflectQuery(view, bound, workContextSize))
		var reply map[string]any
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Fatal(err)
		}
		omitted := fmt.Sprint(reply["output"].(map[string]any)["omitted"])
		deepest := -1
		for i, step := range steps {
			if strings.Contains(omitted, step) {
				deepest = i
			}
		}
		if deepest < 0 || deepest < reached {
			t.Fatalf("at %d bytes a part taken off at a larger bound is back: %s", bound, omitted)
		}
		for i, step := range steps[:deepest] { // every less useful part is gone before a more useful one goes
			if i != 3 && !strings.Contains(omitted, step) { // the list of lessons may fit whole again once a conversation is a head
				t.Fatalf("at %d bytes %q went before %q: %s", bound, steps[deepest], step, omitted)
			}
		}
		reached = deepest
		if over := strings.Contains(omitted, "still over max_bytes"); over != (len(raw) > bound) {
			t.Fatalf("at %d bytes the reply is %d bytes and says: %s", bound, len(raw), omitted)
		}
		sources := reply["sources"].([]any)
		first, second := sources[0].(map[string]any), sources[1].(map[string]any)
		if len(sources) != 2 || first["head"] == true || !strings.HasPrefix(first["source_material"].(map[string]any)["request"].(string), "first "+long(700)) ||
			first["targets"].([]any)[0].(map[string]any)["trace_id"] != "1111111111111111" || second["targets"].([]any)[0].(map[string]any)["trace_id"] != "2222222222222222" ||
			first["memories"].([]any)[0].(map[string]any)["quote"].(map[string]any)["key"] != "lesson:1:1" || second["memories"].([]any)[0].(map[string]any)["quote"].(map[string]any)["key"] != "lesson:2:2" ||
			second["memories"].([]any)[0].(map[string]any)["source_id"] != "cccccccccccccccc" || reply["how_to_submit"] != view.HowToSubmit {
			t.Fatalf("at %d bytes what submit needs is not all there, or the first conversation is cut: %.400s", bound, raw)
		}
		if head := second["head"] == true; head != strings.Contains(omitted, steps[4]) {
			t.Fatalf("at %d bytes the second conversation is a head exactly when output says so: %v %s", bound, head, omitted)
		}
	}
	if reached != len(steps)-1 {
		t.Errorf("the bounds went down to step %d of %d: every step must be reached by this test", reached+1, len(steps))
	}
}

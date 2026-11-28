package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// When a result is recorded without a review of the agent's own, shikii saves an ending review itself, with four fixed sentences. The
// reply of that record carried the four sentences every time (about 410 bytes of the same text in each reply, and 16% of the bytes of the
// record replies of one project). The reply now says "default":true in their place when the latest review is exactly the one shikii
// wrote and the reply still says that recording alone is no proof. The review stays whole in the history, in its immutable source and in
// the session; a review of the agent's own (even one character away from the fixed one) is returned whole; work prepare is not touched.

// defaultReviewEntryNames are the three entries that carry a record: shikii work record on the command line, shikii_wiki record over MCP
// and the HTTP endpoint behind both.
var defaultReviewEntryNames = []string{"shikii work record on the command line", "shikii_wiki record over MCP", "the HTTP endpoint of the wiki"}

// recordCall sends one work record through one entry and returns the reply and its compact size in bytes.
type recordCall func(t *testing.T, work map[string]any) (map[string]any, int)

// defaultReviewEntries sends a record the way each entry takes it.
func defaultReviewEntries(t *testing.T, repo string, b notebook.Book) map[string]recordCall {
	t.Helper()
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
	compactSize := func(t *testing.T, raw []byte) int {
		t.Helper()
		var out bytes.Buffer
		if err := json.Compact(&out, bytes.TrimSpace(raw)); err != nil {
			t.Fatal(err, string(raw))
		}
		return out.Len()
	}
	return map[string]recordCall{
		defaultReviewEntryNames[0]: func(t *testing.T, work map[string]any) (map[string]any, int) {
			t.Helper()
			in, _ := json.Marshal(work)
			var out string
			var err error
			withStdin(t, string(in), func() { out, err = captureErr(func() error { return cmdWork([]string{"record"}) }) })
			if err != nil {
				t.Fatal(err)
			}
			var reply map[string]any
			if err := json.Unmarshal([]byte(out), &reply); err != nil {
				t.Fatal(err, out)
			}
			return reply, compactSize(t, []byte(out))
		},
		defaultReviewEntryNames[1]: func(t *testing.T, work map[string]any) (map[string]any, int) {
			t.Helper()
			in, _ := json.Marshal(map[string]any{"operation": "record", "work": work})
			got, _ := mcpToolCall(t, h, "shikii_wiki", string(in))
			structured, ok := got["structuredContent"].(map[string]any)
			if !ok {
				t.Fatal("an MCP record carries its reply as structuredContent", got)
			}
			raw, _ := json.Marshal(structured)
			var reply map[string]any // as the agent reads it: numbers are decoded alike in every entry
			if err := json.Unmarshal(raw, &reply); err != nil {
				t.Fatal(err)
			}
			return reply, len(raw)
		},
		defaultReviewEntryNames[2]: func(t *testing.T, work map[string]any) (map[string]any, int) {
			t.Helper()
			in, _ := json.Marshal(map[string]any{"operation": "record", "work": work})
			code, body, err := callTool(h, "shikii_wiki", in)
			if err != nil || code >= 400 {
				t.Fatal(code, string(body), err)
			}
			var reply map[string]any
			if err := json.Unmarshal(body, &reply); err != nil {
				t.Fatal(err, string(body))
			}
			return reply, compactSize(t, body)
		},
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func savedIDOf(t *testing.T, reply map[string]any, kind string) string {
	t.Helper()
	for _, raw := range reply["saved"].([]any) {
		if item := raw.(map[string]any); item["kind"] == kind {
			return item["id"].(string)
		}
	}
	t.Fatalf("nothing of kind %s was saved: %v", kind, reply["saved"])
	return ""
}

// withFixedSentences is the latest review of a reply with the four fixed sentences put back, as the reply was before.
func withFixedSentences(latest map[string]any) map[string]any {
	whole := map[string]any{}
	for k, v := range latest {
		if k != "default" {
			whole[k] = v
		}
	}
	whole["focus"], whole["reason"], whole["next_action"], whole["result"] = defaultReviewFocus, defaultReviewReason, defaultReviewNextAction, defaultReviewResult
	return whole
}

// The review shikii wrote, in the reply, is "default":true and the rest of it as it was; saved, it is whole.
func TestARecordReplyDoesNotRepeatTheFixedReviewShikiiWroteItself(t *testing.T) {
	// what reasoning said in the reply before (a fixed value, not read from the code), and the notice that recording is no proof
	const savedNext = "The review is saved. New observations, actual uses, outcomes or wiki edits require an updated finish; recording alone is not proof of improvement."
	idPattern := regexp.MustCompile(`^[0-9a-f]{16}$`)
	keyPattern := regexp.MustCompile(`^reasoning:[0-9a-f]{64}$`)
	variants := []struct {
		name       string
		result     map[string]any
		assessment string
	}{
		{"a result that left nothing open", map[string]any{"changed": "Routed the synthetic invoice to the owner queue.", "checked": "The synthetic invoice reached the owner queue."}, "retain"},
		{"a result that left a condition open", map[string]any{"changed": "Routed the synthetic invoice to the owner queue.", "remaining": "A second input format is untested."}, "unresolved"},
	}
	for _, name := range defaultReviewEntryNames {
		for _, variant := range variants {
			t.Run(name+" with "+variant.name, func(t *testing.T) {
				repo, root, _ := planningFixture(t)
				b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
				entry := defaultReviewEntries(t, repo, b)[name]
				prepared, err := workCall(t, b, "prepare", map[string]any{"request": "Inspect invoice routing", "query": "invoice routing"})
				if err != nil {
					t.Fatal(err)
				}
				reply, size := entry(t, map[string]any{"receipt": prepared["receipt"], "result": variant.result})
				if reply["complete"] != true {
					t.Fatal("the record is saved", reply)
				}
				reasoning := reply["reasoning"].(map[string]any)
				latest := reasoning["latest"].(map[string]any)

				// the four sentences are gone and the flag stands in their place
				for _, field := range []string{"focus", "reason", "next_action", "result"} {
					if _, present := latest[field]; present {
						t.Errorf("the fixed %s is repeated in the reply: %v", field, latest)
					}
				}
				if latest["default"] != true {
					t.Errorf("latest does not say that shikii wrote the review: %v", latest)
				}
				// every other field of the review is what it was
				if got, want := keysOf(latest), []string{"assessment", "at", "default", "evidence", "generation", "key", "phase", "source"}; !reflect.DeepEqual(got, want) {
					t.Errorf("fields of latest: %v, want %v", got, want)
				}
				if latest["phase"] != "finish" || latest["assessment"] != variant.assessment {
					t.Errorf("phase and assessment are kept: %v", latest)
				}
				resultSource := reply["storage"].(map[string]any)["result_source"].(string)
				if !reflect.DeepEqual(latest["evidence"], []any{"source:" + resultSource}) {
					t.Errorf("the evidence is the saved result: %v", latest["evidence"])
				}
				reviewSource := savedIDOf(t, reply, "reasoning")
				if latest["source"] != reviewSource || !idPattern.MatchString(reviewSource) || !keyPattern.MatchString(latest["key"].(string)) {
					t.Errorf("source and key of the review: %v (saved %s)", latest, reviewSource)
				}
				if generation, _ := latest["generation"].(float64); generation < 1 {
					t.Errorf("generation: %v", latest["generation"])
				}
				if _, err := time.Parse(time.RFC3339Nano, latest["at"].(string)); err != nil {
					t.Errorf("at: %v", latest["at"])
				}
				// the rest of reasoning is what it was, and it still says that recording is no proof
				rest := map[string]any{}
				for k, v := range reasoning {
					if k != "latest" {
						rest[k] = v
					}
				}
				if want := map[string]any{"complete": true, "started": true, "required": false, "wiki_pending": false, "next": savedNext}; !reflect.DeepEqual(rest, want) {
					t.Errorf("reasoning besides latest: %v, want %v", rest, want)
				}
				whole, _ := json.Marshal(reply)
				if !strings.Contains(string(whole), "not proof") {
					t.Errorf("the reply no longer says that recording is no proof: %s", whole)
				}
				if variant.assessment == "retain" {
					if got, want := keysOf(reply), []string{"complete", "pending", "reasoning", "saved", "storage"}; !reflect.DeepEqual(got, want) {
						t.Errorf("fields of the reply: %v, want %v", got, want)
					}
					if reply["storage"].(map[string]any)["status"] != "saved" || reply["pending"] != nil {
						t.Errorf("storage and pending: %v %v", reply["storage"], reply["pending"])
					}
				}

				// what the reply left out is saved whole: in the immutable source of the review, in the history and in the session
				sources, err := b.ReadSources([]string{reviewSource})
				if err != nil {
					t.Fatal(err)
				}
				var snapshot struct {
					Finish workReasoning `json:"finish"`
				}
				if err := json.Unmarshal([]byte(sources["observations"].([]notebook.Observation)[0].Content), &snapshot); err != nil {
					t.Fatal(err)
				}
				if f := snapshot.Finish; f.Focus != defaultReviewFocus || f.Reason != defaultReviewReason || f.NextAction != defaultReviewNextAction || f.Result != defaultReviewResult || f.Assessment != variant.assessment {
					t.Errorf("the saved source of the review lost a sentence: %+v", f)
				}
				token, _ := workDecode(prepared["receipt"].(string))
				dir, err := openWorkSessions(b.Root)
				if err != nil {
					t.Fatal(err)
				}
				defer dir.Close()
				state, _, err := loadWorkState(dir, b.Scope, token)
				if err != nil {
					t.Fatal(err)
				}
				recorded, err := readReasoningHistory(b, state.Reasoning, latest["key"].(string))
				if err != nil || recorded == nil || !isDefaultReview(&recorded.Entry) {
					t.Errorf("the history of reasoning lost a sentence: %+v %v", recorded, err)
				}
				if !isDefaultReview(state.Reasoning.Latest) || !isDefaultReview(state.Reasoning.Finish) {
					t.Errorf("the session lost a sentence: %+v", state.Reasoning.Latest)
				}

				// the size: the same reply with the four sentences put back is what it was before
				before := map[string]any{}
				for k, v := range reply {
					before[k] = v
				}
				beforeReasoning := map[string]any{}
				for k, v := range reasoning {
					beforeReasoning[k] = v
				}
				beforeReasoning["latest"] = withFixedSentences(latest)
				before["reasoning"] = beforeReasoning
				beforeRaw, _ := json.Marshal(before)
				t.Logf("the reply is %d bytes, was %d bytes (%d fewer)", size, len(beforeRaw), len(beforeRaw)-size)
				if saved := len(beforeRaw) - size; saved < 300 {
					t.Errorf("the reply is only %d bytes shorter than with the four sentences", saved)
				}

				// work prepare keeps writing the review whole: this change is the record's alone
				again, err := workCall(t, b, "prepare", map[string]any{"receipt": prepared["receipt"], "query": "invoice routing"})
				if err != nil {
					t.Fatal(err)
				}
				prepareLatest, _ := again["reasoning"].(map[string]any)["latest"].(map[string]any)
				if prepareLatest["focus"] != defaultReviewFocus || prepareLatest["reason"] != defaultReviewReason || prepareLatest["default"] != nil {
					t.Errorf("work prepare changed its reasoning.latest: %v", prepareLatest)
				}
			})
		}
	}
}

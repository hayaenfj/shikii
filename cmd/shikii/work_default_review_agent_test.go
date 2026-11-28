package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// A review the agent wrote is returned whole, whatever it says, even when it is one character away from the fixed one. What decides is
// the text sent, compared with the fixed sentences as they are written out, and nothing else.
func TestAReviewTheAgentWroteIsReturnedWholeEvenOneCharacterAwayFromTheFixedOne(t *testing.T) {
	fixed := func(change func(m map[string]any)) map[string]any {
		m := map[string]any{"phase": "finish", "assessment": "retain", "evidence": []string{"current"}, "focus": defaultReviewFocus, "reason": defaultReviewReason, "next_action": defaultReviewNextAction, "result": defaultReviewResult}
		if change != nil {
			change(m)
		}
		return m
	}
	cases := []struct {
		name        string
		reasoning   map[string]any
		wantDefault bool
	}{
		{"a review of other words", reasoningInput("finish", "current"), false},
		{"a focus one character longer", fixed(func(m map[string]any) { m["focus"] = defaultReviewFocus + "." }), false},
		{"a reason one character shorter", fixed(func(m map[string]any) { m["reason"] = strings.TrimSuffix(defaultReviewReason, ".") }), false},
		{"a next action with one letter in another case", fixed(func(m map[string]any) { m["next_action"] = "use" + strings.TrimPrefix(defaultReviewNextAction, "Use") }), false},
		{"a result one character longer", fixed(func(m map[string]any) { m["result"] = defaultReviewResult + "!" }), false},
		// the comparison is of the text, not of who wrote it: word for word the fixed review is the fixed review
		{"the four sentences word for word", fixed(nil), true},
	}
	for _, name := range defaultReviewEntryNames {
		// one prepared request per entry: each review is the next ending review of it
		repo, _, b, prepared := reasoningFixture(t)
		entry := defaultReviewEntries(t, repo, b)[name]
		for _, c := range cases {
			t.Run(name+" with "+c.name, func(t *testing.T) {
				reply, _ := entry(t, map[string]any{"receipt": prepared["receipt"], "reasoning": c.reasoning})
				if reply["complete"] != true {
					t.Fatal("the record is saved", reply)
				}
				latest := reply["reasoning"].(map[string]any)["latest"].(map[string]any)
				if c.wantDefault {
					if latest["default"] != true || latest["focus"] != nil || latest["result"] != nil {
						t.Errorf("the fixed review is not written as the fixed one: %v", latest)
					}
					return
				}
				for _, field := range []string{"focus", "reason", "next_action", "result"} {
					if latest[field] != c.reasoning[field] {
						t.Errorf("%s of the agent's review was changed on the way: %v, sent %v", field, latest[field], c.reasoning[field])
					}
				}
				if _, flagged := latest["default"]; flagged {
					t.Errorf("a review the agent wrote is flagged as the one shikii wrote: %v", latest)
				}
			})
		}
	}
}

// The notice that recording is no proof is what lets the reply leave the sentences out. A reply whose reasoning.next says something else
// (here: work changed after the review was saved) keeps the four sentences of the review shikii wrote.
func TestTheFixedReviewIsKeptInAReplyThatDoesNotSayThatRecordingIsNoProof(t *testing.T) {
	for _, name := range defaultReviewEntryNames {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			entry := defaultReviewEntries(t, repo, b)[name]
			prepared, err := workCall(t, b, "prepare", map[string]any{"request": "Inspect invoice routing", "query": "invoice routing"})
			if err != nil {
				t.Fatal(err)
			}
			first, _ := entry(t, map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Routed the synthetic invoice to the owner queue."}})
			if first["reasoning"].(map[string]any)["latest"].(map[string]any)["default"] != true {
				t.Fatal("control: the first record writes the fixed review as the fixed one", first)
			}
			later, _ := entry(t, map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "A later check", "content": "A quoted field exposed another condition."}}})
			reasoning := later["reasoning"].(map[string]any)
			if reasoning["complete"] != false || strings.Contains(reasoning["next"].(string), "not proof") {
				t.Fatal("control: work after the review reopens it, and the next step is another one", reasoning)
			}
			latest := reasoning["latest"].(map[string]any)
			if latest["focus"] != defaultReviewFocus || latest["reason"] != defaultReviewReason || latest["next_action"] != defaultReviewNextAction || latest["result"] != defaultReviewResult || latest["default"] != nil {
				t.Errorf("the reply that does not say that recording is no proof dropped the sentences: %v", latest)
			}
		})
	}
}

// recordLatest decides by the text and by the reply's own next, one case at a time.
func TestRecordLatestLeavesOutTheFixedSentencesOnlyOfTheFixedReviewWithTheSavedNext(t *testing.T) {
	entry := func(change func(e *reasoningEntry)) *reasoningEntry {
		r := defaultEndingReview([]string{"source:aaaaaaaaaaaaaaaa"}, false)
		e := &reasoningEntry{workReasoning: r, Key: "reasoning:" + strings.Repeat("a", 64), Source: "bbbbbbbbbbbbbbbb", Generation: 2, At: time.Date(2026, 11, 28, 1, 2, 3, 0, time.UTC)}
		if change != nil {
			change(e)
		}
		return e
	}
	key := strings.Repeat("a", 64)
	cases := []struct {
		name    string
		v       reasoningStatus
		want    string // the reply as written when the sentences are left out
		isWhole bool   // the reply is the whole entry, as it was before
	}{
		{"no review", reasoningStatus{Next: reasoningSavedNext}, "", false},
		{"the fixed review and the saved next", reasoningStatus{Latest: entry(nil), Next: reasoningSavedNext}, `{"assessment":"retain","at":"2026-11-28T01:02:03Z","default":true,"evidence":["source:aaaaaaaaaaaaaaaa"],"generation":2,"key":"reasoning:` + key + `","phase":"finish","source":"bbbbbbbbbbbbbbbb"}`, false},
		{"the fixed review that left a condition open and the saved next", reasoningStatus{Latest: entry(func(e *reasoningEntry) { e.Assessment = "unresolved" }), Next: reasoningSavedNext}, `{"assessment":"unresolved","at":"2026-11-28T01:02:03Z","default":true,"evidence":["source:aaaaaaaaaaaaaaaa"],"generation":2,"key":"reasoning:` + key + `","phase":"finish","source":"bbbbbbbbbbbbbbbb"}`, false},
		{"the fixed review and another next", reasoningStatus{Latest: entry(nil), Next: "Record the checked result once with this receipt."}, "", true},
		{"a review of the agent's words", reasoningStatus{Latest: entry(func(e *reasoningEntry) { e.Focus = "Could the export lose a filter?" }), Next: reasoningSavedNext}, "", true},
		{"the fixed review with a depth the agent chose", reasoningStatus{Latest: entry(func(e *reasoningEntry) { e.Depth = "deep" }), Next: reasoningSavedNext}, `{"assessment":"retain","at":"2026-11-28T01:02:03Z","default":true,"depth":"deep","evidence":["source:aaaaaaaaaaaaaaaa"],"generation":2,"key":"reasoning:` + key + `","phase":"finish","source":"bbbbbbbbbbbbbbbb"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := recordLatest(c.v)
			if c.isWhole {
				whole, _ := json.Marshal(c.v.Latest)
				if string(got) != string(whole) {
					t.Errorf("the reply is not the whole review:\n%s\n%s", got, whole)
				}
				return
			}
			if string(got) != c.want {
				t.Errorf("latest is\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// The skill's references/recording.md says what "default":true leaves out: the four fixed sentences, in full, and what the flag means.
func TestTheSkillQuotesTheFixedSentencesOfTheReviewShikiiWrites(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", "shikii", "references", "recording.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for name, sentence := range map[string]string{"focus": defaultReviewFocus, "reason": defaultReviewReason, "next_action": defaultReviewNextAction, "result": defaultReviewResult} {
		if !strings.Contains(doc, sentence) {
			t.Errorf("references/recording.md does not quote the fixed %s in full: %q", name, sentence)
		}
	}
	for _, phrase := range []string{`"default":true`, "reasoning.latest", "does not establish success", "not proof"} {
		if !strings.Contains(doc, phrase) {
			t.Errorf("references/recording.md does not say %q about the review shikii wrote", phrase)
		}
	}
	if !strings.Contains(reasoningSavedNext, "not proof") {
		t.Error("the next that lets a reply leave the sentences out no longer says that recording is no proof")
	}
}

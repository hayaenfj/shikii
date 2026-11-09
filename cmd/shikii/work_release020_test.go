package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestWorkOutcomeCaptureCapacity(t *testing.T) {
	for _, count := range []int{31, 32} {
		t.Run(fmt.Sprintf("captures-%d", count), func(t *testing.T) {
			b, page, v, ref := regressionGrowthBook(t)
			for start := 0; start < count; start += 4 {
				obs := []any{}
				for i := start; i < min(start+4, count); i++ {
					obs = append(obs, map[string]any{"kind": "test", "title": fmt.Sprintf("Synthetic check %d", i), "content": "Synthetic rows preserved ownership."})
				}
				result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": obs})
				if err != nil || result["complete"] != true {
					t.Fatal("control: bounded observations must save", result, err)
				}
			}
			token, _ := workDecode(v["receipt"].(string))
			dir, err := openWorkSessions(b.Root)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			before, _, err := loadWorkState(dir, b.Scope, token)
			if err != nil || len(before.Captures) != count {
				t.Fatal("control: capture boundary not reached", err)
			}
			n := 3
			if count == 31 {
				n = 1
			}
			for i := 0; i < n; i++ {
				result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Check Quartz ownership", "result": fmt.Sprintf("Synthetic run %d remained bounded", i), "sources": []string{"observed1"}}}})
				if err != nil || result["complete"] != (count == 31) {
					t.Fatal("control: snapshot capacity not enforced", result, err)
				}
			}
			after, _, err := loadWorkState(dir, b.Scope, token)
			if err != nil {
				t.Fatal(err)
			}
			outcomes, err := b.ProcedureOutcomes(page, "Quartz")
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if count == 31 {
				want = 1
			}
			if len(outcomes) != want {
				t.Fatal("incorrect actual publication count", len(outcomes))
			}
			t.Logf("captures=%d reserved_before=%d reserved_after=%d published_outcomes=%d", len(after.Captures), len(before.Expected), len(after.Expected), len(outcomes))
			if len(after.Expected) != len(before.Expected) {
				t.Fatalf("definitely rejected outcomes retained %d recovery reservations", len(after.Expected)-len(before.Expected))
			}
		})
	}

	t.Run("revision-changed-before-publication", func(t *testing.T) {
		b, p, v, ref := regressionGrowthBook(t)
		observed, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Synthetic result", "content": "Ownership remained bounded."}}})
		if err != nil || observed["complete"] != true {
			t.Fatal(observed, err)
		}
		token, _ := workDecode(v["receipt"].(string))
		dir, err := openWorkSessions(b.Root)
		if err != nil {
			t.Fatal(err)
		}
		defer dir.Close()
		w, name, err := loadWorkState(dir, b.Scope, token)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		save := func() error {
			calls++
			err := saveWorkState(dir, name, b.Scope, token.Turn, w, false)
			if err == nil && calls == 1 {
				regressionChangeArticle(t, b, p, "updated")
			}
			return err
		}
		commit := workCommitter(func(action func() error) error { return commitWorkTurn(dir, name, b.Scope, token, action) })
		resolve := func([]string) ([]string, error) { return []string{w.Sources["observed1"]}, nil }
		_, err = recordWorkOutcome(b, w, workOutcome{Ref: ref, Conditions: "Linux", Action: "Check ownership", Result: "Synthetic rows stayed bounded", Sources: []string{"observed1"}}, resolve, save, commit)
		if err == nil {
			t.Fatal("control: stale outcome must be refused")
		}
		loaded, _, err := loadWorkState(dir, b.Scope, token)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Expected) != 0 {
			t.Fatal("definite publication rejection left a reservation", len(loaded.Expected))
		}
	})
}

func TestWorkInitialExcerptCitations(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	body := strings.Repeat("Background migration details.\n\n", 160) + "UnseenSaffronClause is outside the delivered passages.\n\n" + strings.Repeat("Background migration details.\n\n", 190) + "Quartz exports must preserve tenant ownership.\n\n"
	p := reasoningPage(t, b, "Quartz ownership", "howto", body)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz tenant ownership"})
	if err != nil {
		t.Fatal(err)
	}
	entry := v["knowledge"].([]any)[0].(map[string]any)
	if entry["body_included"] != false || entry["id"] != p.ID {
		t.Fatal("control requires the omitted article", entry["body_included"])
	}
	quote := "Quartz exports must preserve tenant ownership."
	delivered := false
	for _, raw := range entry["body_excerpts"].([]any) {
		delivered = delivered || strings.Contains(raw.(map[string]any)["text"].(string), quote)
	}
	if !delivered {
		t.Fatal("control: exact quote was not supplied")
	}
	payload := map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Preserve ownership for the requested export.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": entry["ref"], "text": quote}}}}
	for _, raw := range entry["body_excerpts"].([]any) {
		if strings.Contains(raw.(map[string]any)["text"].(string), "UnseenSaffronClause") {
			t.Fatal("control: hidden passage was unexpectedly returned")
		}
	}
	for _, quotes := range [][]any{
		{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": entry["ref"], "text": "UnseenSaffronClause is outside the delivered passages."}},
		{map[string]any{"ref": entry["ref"], "text": quote}},
	} {
		refused, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Preserve ownership.", "quotes": quotes}})
		if err != nil || refused["complete"] != false {
			t.Fatal("unseen text or article-only agreement was accepted", refused, err)
		}
	}
	first, err := workCall(t, b, "record", payload)
	if err != nil {
		t.Fatal(err)
	}
	// The same revision and quotation succeed after a separate full read.
	_, err = workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID + "@" + p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	control, err := workCall(t, b, "record", payload)
	if err != nil || control["complete"] != true {
		t.Fatal("control: full read did not make the identical quote available", control, err)
	}
	t.Logf("initial_excerpt_complete=%v after_full_read_complete=%v", first["complete"], control["complete"])
	if first["complete"] != true {
		t.Fatalf("a supplied initial excerpt was not quoteable: %v", first["errors"])
	}
}

func TestWorkPlanningQueryReplacesReceipt(t *testing.T) {
	for _, otherQuery := range []bool{false, true} {
		t.Run(fmt.Sprintf("other-query-%t", otherQuery), func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			p := reasoningPage(t, b, "Amber ownership", "howto", "Keep Amber ownership when exporting.")
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			turn := loadSession(root, "planning").TurnID
			v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "Quartz unmatched"})
			if err != nil {
				t.Fatal(err)
			}
			if otherQuery {
				_, err = planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": turn, "query": "Copper unmatched"})
				if err != nil {
					t.Fatal(err)
				}
			}
			extra, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID})
			if otherQuery {
				if err == nil {
					t.Fatal("a replaced planning context must reject its earlier receipt before supplying new knowledge")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ref := extra["knowledge"].([]any)[0].(map[string]any)["ref"]
			result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Preserve ownership for the requested export.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": ref, "text": p.Lesson}}}})
			if err != nil || result["complete"] != true {
				t.Fatalf("accepted current receipt lost its returned quotation: complete=%v errors=%v outer=%v", result["complete"], result["errors"], err)
			}
		})
	}

	t.Run("query-during-model", func(t *testing.T) {
		repo := gitRepo(t)
		var root string
		var armed, switched atomic.Bool
		model := fakeAgentModel(t, func(string) float64 {
			if armed.CompareAndSwap(true, false) {
				_, err := runPlanning(root, feedbackScope(repo), "query", planningRequest{SessionID: "planning", TurnID: loadSession(root, "planning").TurnID, Query: "Copper unmatched"})
				if err != nil {
					t.Errorf("replacement query failed: %v", err)
				} else {
					switched.Store(true)
				}
			}
			return .9
		})
		root = hookEnv(t, model, repo)
		usePlanningSpec(t, root)
		b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
		v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz unmatched"})
		if err != nil {
			t.Fatal(err)
		}
		armed.Store(true)
		result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Keep the requested CSV filters.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}}}})
		if err != nil || result["complete"] != false || !switched.Load() {
			t.Fatal("obsolete model result was accepted", result, err)
		}
		records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
		if err != nil || len(records) != 0 {
			t.Fatal("obsolete model result was published", len(records), err)
		}
	})
}

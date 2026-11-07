package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func regressionGrowthBook(t *testing.T) (notebook.Book, notebook.Page, map[string]any, string) {
	t.Helper()
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz workflow", "lesson": "Check Quartz ownership.", "applies_to": "Quartz", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz", "request": "Check Quartz ownership"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"].(string)
	return b, p, v, ref
}

func regressionChangeArticle(t *testing.T, b notebook.Book, p notebook.Page, change string) {
	t.Helper()
	switch change {
	case "updated":
		loopPage(t, b, p.Sources[0].ID, map[string]any{"id": p.ID, "title": p.Title, "lesson": "Check ownership and totals.", "applies_to": p.AppliesTo, "kind": p.Kind})
	case "deleted":
		if _, err := b.Manage(notebook.Change{ID: p.ID, Revision: p.Revision, Action: "delete"}); err != nil {
			t.Fatal(err)
		}
	case "replaced":
		_, err := b.Ingest(notebook.Draft{Title: "Replacement workflow", Lesson: "Use the replacement ownership check.", Kind: p.Kind, Scene: "checking", AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}, Replaces: []string{p.ID}})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkDiscoveryPublicationRecovery(t *testing.T) {
	for _, change := range []string{"updated", "deleted", "replaced"} {
		t.Run(change, func(t *testing.T) {
			b, p, v, ref := regressionGrowthBook(t)
			token, err := workDecode(v["receipt"].(string))
			if err != nil {
				t.Fatal(err)
			}
			dir, err := openWorkSessions(b.Root)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			w, name, err := loadWorkState(dir, b.Scope, token)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			save := func() error {
				n++
				if n == 2 {
					return errors.New("synthetic checkpoint failure after discovery publication")
				}
				return saveWorkState(dir, name, b.Scope, token.Turn, w, false)
			}
			commit := workCommitter(func(action func() error) error { return commitWorkTurn(dir, name, b.Scope, token, action) })
			d := workDiscovery{Ref: ref, Context: "Zephyr bounded check", Decision: "Kept the ownership check", Evidence: "Synthetic ownership remained bounded"}
			_, err = recordWorkDiscovery(b, w, token, d, save, commit)
			if err == nil || n != 2 {
				t.Fatal("the checkpoint failure was not reached")
			}
			recalled, err := b.Recall(d.Context)
			if err != nil || len(recalled) != 1 {
				t.Fatal("the discovery must already be published", err)
			}
			regressionChangeArticle(t, b, p, change)
			result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "discoveries": []any{map[string]any{"ref": ref, "context": d.Context, "decision": d.Decision, "evidence": d.Evidence}}})
			if err != nil || result["complete"] != true {
				t.Fatalf("published discovery cannot recover: complete=%v errors=%v outer=%v", result["complete"], result["errors"], err)
			}

			paths, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-searches", "*", ".searches.json"))
			if err != nil || len(paths) != 1 {
				t.Fatal(paths, err)
			}
			raw, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			var journal struct {
				Discoveries []notebook.Discovery `json:"discoveries"`
			}
			if err = json.Unmarshal(raw, &journal); err != nil || len(journal.Discoveries) != 1 {
				t.Fatal("recovery duplicated the published use", err)
			}
			// Recovery must not append a second discovery or permit changed evidence.
			changed := d
			changed.Evidence = "Changed evidence is a new claim."
			refused, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "discoveries": []workDiscovery{changed}})
			if err != nil || refused["complete"] != false {
				t.Fatal("changed evidence cannot recover an old use", refused, err)
			}
		})
	}
}

func TestWorkDiscoveryRejectionPreservesCapacity(t *testing.T) {
	b, p, v, ref := regressionGrowthBook(t)
	loopPage(t, b, p.Sources[0].ID, map[string]any{"id": p.ID, "title": p.Title, "lesson": "Check Quartz ownership and totals.", "applies_to": p.AppliesTo, "kind": "howto"})
	for _, context := range []string{"first rejected use", "second rejected use", "third rejected use"} {
		result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "discoveries": []any{map[string]any{"ref": ref, "context": context, "decision": "Keep ownership bounded", "evidence": "Synthetic rows remain bounded"}}})
		if err != nil || result["complete"] != false {
			t.Fatal("control: the stale revision must be refused", err)
		}
	}
	token, _ := workDecode(v["receipt"].(string))
	dir, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, _, err := loadWorkState(dir, b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Expected) != 0 {
		t.Fatalf("rejected stale inputs reserved %d durable slots", len(w.Expected))
	}

	for _, mode := range []string{"invalid-case", "changed-after-preflight", "uncertain-publication"} {
		t.Run(mode, func(t *testing.T) {
			b, p, v, ref := regressionGrowthBook(t)
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
			for i := 0; i < workSavedLimit-1; i++ {
				w.Saved[fmt.Sprintf("saved-%d", i)] = workItem{Kind: "test"}
			}
			if err = saveWorkState(dir, name, b.Scope, token.Turn, w, false); err != nil {
				t.Fatal(err)
			}
			d := workDiscovery{Ref: ref, Context: "Bounded capacity", Decision: "Keep ownership", Evidence: "Synthetic ownership checked"}
			if mode == "invalid-case" {
				d.CaseID = "ffffffffffffffff"
			}
			saves := 0
			save := func() error {
				saves++
				if saves == 1 && mode == "changed-after-preflight" {
					regressionChangeArticle(t, b, p, "updated")
				}
				return saveWorkState(dir, name, b.Scope, token.Turn, w, false)
			}
			calls := 0
			commit := workCommitter(func(action func() error) error {
				calls++
				err := commitWorkTurn(dir, name, b.Scope, token, action)
				if err == nil && calls == 2 && mode == "uncertain-publication" {
					return errors.New("synthetic error after publication")
				}
				return err
			})
			_, err = recordWorkDiscovery(b, w, token, d, save, commit)
			if err == nil {
				t.Fatal("injected refusal or uncertainty not reached")
			}
			loaded, _, err := loadWorkState(dir, b.Scope, token)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "uncertain-publication" {
				want = 1
			}
			if len(loaded.Expected) != want || len(loaded.Saved) != workSavedLimit-1 {
				t.Fatal("incorrect durable capacity", len(loaded.Expected), len(loaded.Saved))
			}
			if mode == "invalid-case" {
				d.CaseID = ""
				result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "discoveries": []workDiscovery{d}})
				if err != nil || result["complete"] != true {
					t.Fatal("last free slot was lost", result, err)
				}
			}
			if mode == "uncertain-publication" {
				regressionChangeArticle(t, b, p, "deleted")
				result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "discoveries": []workDiscovery{d}})
				if err != nil || result["complete"] != true {
					t.Fatal("reserved publication must recover at capacity", result, err)
				}
			}
		})
	}
}

func TestWorkAddedKnowledgeQuotes(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p := reasoningPage(t, b, "Amber ownership", "criteria", "Keep Amber ownership when exporting.")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "initial-control", "cwd": repo, "prompt": planningTestRequest})
	initial, err := workCall(t, b, "prepare", map[string]any{"session_id": "initial-control", "turn_id": loadSession(root, "initial-control").TurnID, "query": "Amber ownership"})
	if err != nil {
		t.Fatal(err)
	}
	initialRef := initial["knowledge"].([]any)[0].(map[string]any)["ref"]
	control, err := workCall(t, b, "record", map[string]any{"receipt": initial["receipt"], "agreed": map[string]any{"summary": "Implement the requested export while preserving ownership.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": initialRef, "text": p.Lesson}}}})
	if err != nil || control["complete"] != true {
		t.Fatal("control: initially prepared knowledge citation failed", err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz no matching knowledge"})
	if err != nil {
		t.Fatal(err)
	}
	extra, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	ref := extra["knowledge"].([]any)[0].(map[string]any)["ref"]
	result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Implement the requested export while preserving ownership.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": ref, "text": p.Lesson}}}})
	if err != nil || result["complete"] != true {
		t.Fatalf("read article cannot ground planning: complete=%v errors=%v outer=%v", result["complete"], result["errors"], err)
	}

	for _, operation := range []string{"query", "consult", "reuse"} {
		for _, mode := range []string{"valid", "wrong-quote", "wiki-only", "updated", "deleted", "replaced"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				repo, root, _ := planningFixture(t)
				b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
				p := reasoningPage(t, b, "Amber ownership", "howto", "Keep Amber ownership when exporting.")
				runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
				v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz unmatched"})
				if err != nil {
					t.Fatal(err)
				}
				req := map[string]any{"receipt": v["receipt"]}
				switch operation {
				case "query":
					req["query"] = "lesson:" + p.ID
				case "consult":
					req["proposal"] = "Check Amber ownership"
				case "reuse":
					req["conditions"] = "Check Amber ownership on Linux"
				}
				extra, err := workCall(t, b, "prepare", req)
				if err != nil {
					t.Fatal(err)
				}
				if len(extra["knowledge"].([]any)) == 0 {
					t.Fatal("the article was not returned", extra)
				}
				ref := extra["knowledge"].([]any)[0].(map[string]any)["ref"]
				regressionChangeArticle(t, b, p, mode)
				quote := p.Lesson
				if mode == "wrong-quote" {
					quote = "An unseen alternative quotation."
				}
				quotes := []any{map[string]any{"ref": ref, "text": quote}}
				if mode != "wiki-only" {
					quotes = append(quotes, map[string]any{"ref": "current", "text": planningTestRequest})
				}
				result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Preserve ownership for the requested export.", "quotes": quotes}})
				if err != nil || result["complete"] != (mode == "valid") {
					t.Fatal(mode, result, err)
				}
			})
		}
	}
	t.Run("partial-and-full-body-preserve-quotes", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		body := strings.Repeat("Background migration history. ", 180) + "HiddenCopperPassage must stay unseen. " + strings.Repeat("Background migration history. ", 180) + "Check Amber ownership before exporting."
		p := reasoningPage(t, b, "Amber ownership", "howto", body)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
		v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz unmatched"})
		if err != nil {
			t.Fatal(err)
		}
		extra, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "proposal": "Check Amber ownership"})
		if err != nil {
			t.Fatal(err)
		}
		entry := extra["knowledge"].([]any)[0].(map[string]any)
		ref := entry["ref"].(string)
		check := extra["experience"].(map[string]any)["checks"].([]any)[int(entry["experience_check"].(float64))].(map[string]any)
		if check["body_omitted"] != true {
			t.Fatal("control requires an omitted body")
		}
		excerpt := check["body_excerpts"].([]any)[0].(map[string]any)["text"].(string)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		token, _ := workDecode(v["receipt"].(string))
		dir, err := openWorkSessions(root)
		if err != nil {
			t.Fatal(err)
		}
		defer dir.Close()
		w, name, err := loadWorkState(dir, b.Scope, token)
		if err != nil {
			t.Fatal(err)
		}
		before, err := readPlanningSession(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		key := w.Keys[ref]
		previous := before.Planning.Sources[key]
		if !strings.Contains(previous, excerpt) || strings.Contains(previous, "HiddenCopperPassage") || len(before.Planning.Knowledge.Read) != 0 {
			t.Fatal("unseen body became quoteable or read")
		}
		rejected, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Keep ownership.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": ref, "text": "HiddenCopperPassage"}}}})
		if err != nil || rejected["complete"] != false {
			t.Fatal(rejected, err)
		}
		full, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID})
		if err != nil {
			t.Fatal(err)
		}
		if full["knowledge"].([]any)[0].(map[string]any)["ref"] != ref {
			t.Fatal("same revision replaced its short reference")
		}
		after, err := readPlanningSession(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(after.Planning.Sources[key], previous) || !strings.Contains(after.Planning.Sources[key], "HiddenCopperPassage") || len(after.Planning.Knowledge.Read) != 1 {
			t.Fatal("extension lost earlier quotes or full read")
		}
		// Repeating an identical read does not grow the retained source again.
		size := len(after.Planning.Sources[key])
		if _, err = workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID}); err != nil {
			t.Fatal(err)
		}
		after, _ = readPlanningSession(dir, name)
		if len(after.Planning.Sources[key]) != size {
			t.Fatal("repeated read grew the source")
		}
		result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Preserve ownership.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}, map[string]any{"ref": ref, "text": excerpt}}}})
		if err != nil || result["complete"] != true {
			t.Fatal(result, err)
		}
	})
	t.Run("session-capacity-is-atomic", func(t *testing.T) {
		b, p, v, _ := regressionGrowthBook(t)
		token, _ := workDecode(v["receipt"].(string))
		dir, err := openWorkSessions(b.Root)
		if err != nil {
			t.Fatal(err)
		}
		defer dir.Close()
		_, name, err := loadWorkState(dir, b.Scope, token)
		if err != nil {
			t.Fatal(err)
		}
		s, err := readPlanningSession(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		padding := ""
		s.Planning.Work.Pending["padding"] = notebook.Draft{Body: &padding}
		raw, _ := encodeHookSession(s)
		padding = strings.Repeat("x", (1<<20)-32-len(raw))
		if err = writeWorkSession(dir, name, s); err != nil {
			t.Fatal(err)
		}
		before, _ := dir.ReadFile(name)
		_, err = workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID})
		after, _ := dir.ReadFile(name)
		if err == nil || !bytes.Equal(before, after) {
			t.Fatal("capacity failure must leave the earlier receipt intact", err)
		}
		if _, _, err = loadWorkState(dir, b.Scope, token); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("turn-changes-during-consult", func(t *testing.T) {
		repo := gitRepo(t)
		var root string
		var enabled, switched atomic.Bool
		model := fakeAgentModel(t, func(state string) float64 {
			if enabled.Load() && switched.CompareAndSwap(false, true) {
				runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Start an independent task"})
			}
			return .9
		})
		root = hookEnv(t, model, repo)
		usePlanningSpec(t, root)
		b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		reasoningPage(t, b, "Amber ownership", "howto", "Check Amber ownership.")
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
		v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz unmatched"})
		if err != nil {
			t.Fatal(err)
		}
		enabled.Store(true)
		_, err = workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "proposal": "Check Amber ownership"})
		if err == nil || !switched.Load() {
			t.Fatal("old turn accepted model result", err)
		}
		current := loadSession(root, "planning")
		if current.Planning.Work != nil {
			t.Fatal("old receipt was attached to the new turn")
		}
		records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
		if err != nil || len(records) != 0 {
			t.Fatal("extension created a planning judgment", err)
		}
	})
}

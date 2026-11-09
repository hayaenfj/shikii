package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func auditR2Count(t *testing.T, b notebook.Book, title string) int {
	t.Helper()
	paths, e := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", "*.json"))
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, p := range paths {
		if len(filepath.Base(p)) != 21 || strings.HasPrefix(filepath.Base(p), ".") {
			continue
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		var o notebook.Observation
		if e = json.Unmarshal(raw, &o); e != nil {
			t.Fatal(e)
		}
		if o.Title == title {
			n++
		}
	}
	return n
}

func TestWorkRegressionR2ExactRetrySourceCount(t *testing.T) {
	b, _ := loopBook(t)
	p, e := workCall(t, b, "prepare", map[string]any{"query": "synthetic retry", "request": "Check a synthetic state."})
	if e != nil {
		t.Fatal(e)
	}
	fields := map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "SYNTHETIC-RETRY-RESULT"}}
	first, e := workCall(t, b, "record", fields)
	if e != nil || first["complete"] != true {
		t.Fatal("fixture first report failed", e)
	}
	before := auditR2Count(t, b, "Evidence-linked work judgment and ending review")
	again, e := workCall(t, b, "record", fields)
	if e != nil {
		t.Fatal(e)
	}
	after := auditR2Count(t, b, "Evidence-linked work judgment and ending review")
	t.Logf("same_retry_complete=%v ending_sources_before=%d after=%d", again["complete"], before, after)
	if before != after {
		t.Error("same report grew ending sources")
	}
}

func TestWorkRegressionR2UnboundResultAcrossTurn(t *testing.T) {
	for _, next := range []bool{false, true} {
		name := "same_turn_control"
		if next {
			name = "next_turn_counterexample"
		}
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			session := "synthetic-unbound-boundary"
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check synthetic first owner state."})
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "owner"})
			if e != nil {
				t.Fatal(e)
			}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "SYNTHETIC-UNBOUND-RESULT", "sufficient": true, "evidence": []string{"missing-evidence"}}})
			if e != nil || first["complete"] != false {
				t.Fatal("fixture must fail evidence binding", e)
			}
			original := loopObject(t, first["storage"])["result_source"].(string)
			if next {
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check synthetic second owner state."})
			}
			p2, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "owner"})
			if e != nil {
				t.Fatal(e)
			}
			finish := map[string]any{"phase": "finish", "focus": "Check current synthetic state", "assessment": "retain", "reason": "Current synthetic evidence is retained.", "evidence": []string{"current"}, "next_action": "Retain the checked condition.", "result": "Current check finished."}
			end, e := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "reasoning": finish})
			if e != nil {
				t.Fatal(e)
			}
			s := loadSession(root, session)
			oldStillPending := false
			obs, e := b.PendingObservations()
			if e != nil {
				t.Fatal(e)
			}
			for _, o := range obs {
				oldStillPending = oldStillPending || o.ID == original
			}
			stopCheck := verifyStopReasoning(root, s, s)
			t.Logf("ending_check_accepts=%v", stopCheck == nil)
			t.Logf("next_turn=%v result_state_retained=%v original_pending=%v record_complete=%v reasoning_complete=%v reasoning_left=%d", next, s.Reasoning.Result != nil, oldStillPending, end["complete"], loopObject(t, end["reasoning"])["complete"], reasoningLeft(s.Reasoning))
			if next && end["complete"] == true && s.Reasoning.Result == nil {
				t.Error("a new human turn loses the failed result binding obligation")
			}
			if !next && end["complete"] == true {
				t.Error("control unexpectedly finished an unbound report")
			}
		})
	}
}

func TestWorkRegressionR2PendingProcedureAcrossTurn(t *testing.T) {
	for _, next := range []bool{false, true} {
		name := "same_turn_control"
		if next {
			name = "next_turn_counterexample"
		}
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			session := "synthetic-procedure-boundary"
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			reasoningPage(t, b, "Synthetic owner state", "howto", strings.Repeat("Inspect the synthetic ownership state before continuing.\n\n", 60))
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check synthetic first owner state."})
			p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "Synthetic owner state"})
			if e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 3; i++ {
				_, e = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Synthetic owner state"})
				if e != nil {
					t.Fatal(e)
				}
			}
			searchDir := filepath.Join(root, "records", "wiki-searches", store.StoreName(root, b.Scope))
			if e = os.MkdirAll(searchDir, 0700); e != nil {
				t.Fatal(e)
			}
			checkpoint := filepath.Join(searchDir, ".procedure-policy-v1.json")
			if e = os.Mkdir(checkpoint, 0700); e != nil {
				t.Fatal(e)
			}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "SYNTHETIC-PENDING-PROCEDURE"}})
			if e != nil || first["complete"] != false {
				t.Fatal("fixture must interrupt procedure write", e)
			}
			if next {
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check synthetic second owner state."})
			}
			p2, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "Synthetic owner state"})
			if e != nil {
				t.Fatal(e)
			}
			end, e := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "result": map[string]any{"changed": "SYNTHETIC-NEXT-RESULT"}})
			if e != nil {
				t.Fatal(e)
			}
			s := loadSession(root, session)
			pending := pendingWorkProcedure(s.Planning.Work)
			t.Logf("next_turn=%v pending_procedure=%v recovery_pending=%v record_complete=%v reasoning_complete=%v reasoning_left=%d", next, pending, p2["procedure_recovery"] != nil, end["complete"], loopObject(t, end["reasoning"])["complete"], reasoningLeft(s.Reasoning))
			if next && pending && end["complete"] == true {
				t.Error("new report completion ignores pending procedure feedback retained from the prior turn")
			}
			if !next && end["complete"] == true {
				t.Error("control unexpectedly finished with blocked procedure feedback")
			}
			if e = os.Remove(checkpoint); e != nil {
				t.Fatal(e)
			}
			_, e = workCall(t, b, "prepare", map[string]any{"receipt": p2["receipt"], "query": "Synthetic owner state"})
			if e != nil {
				t.Fatal("recovery after removing isolated obstruction failed", e)
			}
			recovered := loadSession(root, session)
			if pendingWorkProcedure(recovered.Planning.Work) {
				t.Error("recovery control left procedure pending")
			}
			t.Log("obstruction_removed=true recovery_pending_procedure=false")
		})
	}
}

func TestWorkRegressionR2SameResultAfterReplacePreparation(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "same_receipt_control"
		if replace {
			name = "replace_preparation_counterexample"
		}
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			session := "synthetic-report-identity"
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check synthetic original report identity."})
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			prep := map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "identity"}
			p, e := workCall(t, b, "prepare", prep)
			if e != nil {
				t.Fatal(e)
			}
			result := map[string]any{"changed": "SYNTHETIC-IDENTICAL-REPORT"}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": result})
			if e != nil || first["complete"] != true {
				t.Fatal("fixture first report", e)
			}
			original := loopObject(t, first["storage"])["result_source"]
			if replace {
				p, e = workCall(t, b, "prepare", prep)
				if e != nil {
					t.Fatal(e)
				}
			}
			second, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": result})
			if e != nil {
				t.Fatal(e)
			}
			current := loopObject(t, second["storage"])["result_source"]
			n := auditR2Count(t, b, "Work result and remaining conditions")
			t.Logf("replace_preparation=%v same_result_source=%v report_sources=%d record_complete=%v", replace, original == current, n, second["complete"])
			if original != current {
				t.Error("replacing preparation changes immutable identity of an identical saved report")
			}
		})
	}
}

func TestWorkRegressionR2ReceiptBoundaries(t *testing.T) {
	b, _ := loopBook(t)
	p, e := workCall(t, b, "prepare", map[string]any{"query": "synthetic boundary", "request": "Check synthetic receipt boundaries."})
	if e != nil {
		t.Fatal(e)
	}
	receipt := p["receipt"].(string)
	token, e := workDecode(receipt)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"project", "session", "turn", "nonce", "encoding"} {
		t.Run(name, func(t *testing.T) {
			x := token
			target := b
			value := receipt
			switch name {
			case "project":
				target.Scope += "/synthetic-other"
			case "session":
				x.Session += "-other"
				value = workEncode(x)
			case "turn":
				x.Turn = strings.Repeat("f", 16)
				value = workEncode(x)
			case "nonce":
				x.Nonce = strings.Repeat("f", 32)
				value = workEncode(x)
			case "encoding":
				value += "x"
			}
			_, err := workCall(t, target, "record", map[string]any{"receipt": value, "result": map[string]any{"changed": "SYNTHETIC-REJECTED-BOUNDARY"}})
			if err == nil {
				t.Error("different receipt boundary was accepted")
			}
			t.Logf("boundary=%s rejected=%v", name, err != nil)
		})
	}
	dir, e := openWorkSessions(b.Root)
	if e != nil {
		t.Fatal(e)
	}
	_, name, e := loadWorkState(dir, b.Scope, token)
	if e != nil {
		t.Fatal(e)
	}
	unlock, e := workLock(dir, name)
	if e != nil {
		t.Fatal(e)
	}
	_, e = workCall(t, b, "record", map[string]any{"receipt": receipt, "result": map[string]any{"changed": "SYNTHETIC-CONCURRENT"}})
	unlock()
	dir.Close()
	if e == nil {
		t.Error("concurrent record was accepted")
	}
	good, e := workCall(t, b, "record", map[string]any{"receipt": receipt, "result": map[string]any{"changed": "SYNTHETIC-VALID-CONTROL"}})
	if e != nil || good["complete"] != true {
		t.Fatal("current unlocked receipt rejected", e)
	}
	s := loadSession(b.Root, token.Session)
	s.Planning.Work.At = time.Now().Add(-hookSessionTTL - time.Second)
	if e = saveSession(b.Root, token.Session, s); e != nil {
		t.Fatal(e)
	}
	_, e = workCall(t, b, "record", map[string]any{"receipt": receipt, "result": map[string]any{"changed": "SYNTHETIC-EXPIRED"}})
	if e == nil {
		t.Error("expired receipt was accepted")
	}
	t.Logf("concurrent_rejected=true current_control_complete=%v expired_rejected=%v rejected_reports_written=%d", good["complete"], e != nil, auditR2Count(t, b, "Work result and remaining conditions")-1)
}

func TestWorkRegressionR2CaptureCapacity(t *testing.T) {
	b, _ := loopBook(t)
	p, e := workCall(t, b, "prepare", map[string]any{"query": "synthetic capacity", "request": "Check fixed capture bounds."})
	if e != nil {
		t.Fatal(e)
	}
	observation := func(i int) map[string]any {
		return map[string]any{"kind": "test", "title": fmt.Sprintf("Synthetic bounded capture %d", i), "content": "SYNTHETIC-BOUNDED-CONTENT"}
	}
	for i := 0; i < 32; i++ {
		r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{observation(i)}})
		if e != nil || r["complete"] != true {
			t.Fatalf("fixture capture %d failed: %v", i, e)
		}
	}
	duplicate, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{observation(31)}})
	if e != nil || duplicate["complete"] != true {
		t.Fatal("identical retry failed at capacity", e)
	}
	extra, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{observation(32)}})
	if e != nil || extra["complete"] != false {
		t.Fatal("new capture must report incomplete at limit", e)
	}
	result, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "SYNTHETIC-NOT-CONFIRMED"}})
	if e != nil || result["complete"] != false || loopObject(t, result["storage"])["status"] != "unconfirmed" {
		t.Error("capacity must not confirm an uncaptured result")
	}
	t.Logf("duplicate_at_limit_complete=%v new_capture_complete=%v result_complete=%v result_status=%s", duplicate["complete"], extra["complete"], result["complete"], loopObject(t, result["storage"])["status"])
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
)

func variantsCall(t *testing.T, payload string) (string, error) {
	t.Helper()
	oldIn, oldOut := stdin, stdout
	var out bytes.Buffer
	stdin, stdout = strings.NewReader(payload), &out
	defer func() { stdin, stdout = oldIn, oldOut }()
	err := cmdHook([]string{"variants", "submit"})
	return out.String(), err
}

// For a decision that gets no line even after its truths reach min_n, ask the agent once for wording variants, check and add the variants received, and measure them in the background.
// If a variant wins, tell the person once at Stop (adoption is the person's call).
func TestHookAsksTheAgentForWordingsWhenNoLineCanBeDrawn(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.2 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	lowerMinN(t, agent, "prompt-kind", 2)
	oldCal, oldGap, oldTune := hookCalibrate, hookCalibrateGap, hookTune
	attempts := 0
	hookCalibrate = func(agent, path, storeDir, model string) { attempts++ } // no line could be drawn
	hookCalibrateGap = 0
	var tuned []string
	hookTune = func(agent, path, storeDir string) { tuned = append(tuned, filepath.Base(path)) }
	t.Cleanup(func() { hookCalibrate, hookCalibrateGap, hookTune = oldCal, oldGap, oldTune })
	turn := func(session string) string {
		out := runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "explain what the parser in " + session + " does"})
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Explained."})
		return out
	}
	asked := 0
	for i := 0; i < 6; i++ {
		before := attempts
		out := turn(fmt.Sprintf("s%d", i))
		if !strings.Contains(out, "hook variants submit") {
			continue
		}
		asked++
		if before == 0 || !strings.Contains(out, `the decision \"prompt-kind\"`) {
			t.Fatalf("wordings must be asked only after a failed attempt (attempts before this turn=%d): %s", before, out)
		}
	}
	if attempts != 1 || asked != 1 {
		t.Fatalf("one failed attempt must lead to exactly one request for wordings: attempts=%d asked=%d", attempts, asked)
	}

	// a shape error says which key should have which shape
	if _, err := variantsCall(t, `{"spec":"prompt-kind","variants":{"name":"a"}}`); err == nil || !strings.Contains(err.Error(), "variants") || !strings.Contains(err.Error(), "an array") {
		t.Fatalf("a wrong shape must name the field and the shape: %v", err)
	}
	if _, err := variantsCall(t, `{"spec":"prompt-kind","variants":[{"name":"x","options":{"nope":"z"}}]}`); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown option must be rejected: %v", err)
	}
	if _, err := variantsCall(t, `{"spec":"prompt-kind","variants":[{"name":"Bad Name","question":"q"}]}`); err == nil {
		t.Fatal("a bad name must be rejected")
	}
	if len(tuned) != 0 {
		t.Fatalf("rejected candidates must not start a tune: %v", tuned)
	}
	got, err := variantsCall(t, `{"spec":"prompt-kind","variants":[{"name":"ends-in-files","question":"Will the coding agent end up changing files for `+"`request`"+`?","rationale":"name the outcome"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]any
	if json.Unmarshal([]byte(got), &res) != nil || res["tune"] != "started" {
		t.Fatalf("submit result: %s", got)
	}
	sp := mustLoad(t, filepath.Join(agent, "decisions", "prompt-kind.yaml"))
	if len(sp.Variants) != 1 || sp.Variants[0].Name != "ends-in-files" || len(sp.Calibration) != 0 {
		t.Fatalf("the candidate must be added without touching the line: %+v", sp.Variants)
	}
	if len(tuned) != 1 || tuned[0] != "prompt-kind.yaml" {
		t.Fatalf("a tune must start for the new candidates: %v", tuned)
	}

	// if the measured variant wins (can be adopted), ask the agent once before the next request to check the meaning and translate it into Japanese (0.9.2).
	// until the agent has checked, nothing is said to the person
	runs := filepath.Join(agent, "wiki", "runs")
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(map[string]any{"spec": "prompt-kind", "spec_hash": sp.Hash(), "run": "prompt-kind-1", "at": "2026-10-23T01:00:00+09:00", "winner": "ends-in-files", "adoptable": true,
		"winner_wording": map[string]any{"question": "Will the coding agent end up changing files?"}, "current_wording": map[string]any{"question": sp.Question}})
	if err := os.WriteFile(filepath.Join(runs, "prompt-kind-1.json"), result, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s6", "cwd": repo, "prompt": "explain what the lexer does in detail"}))
	if !strings.Contains(ctx, "ends-in-files") || !strings.Contains(ctx, "hook variants check") {
		t.Fatalf("an adoptable candidate must be handed to the agent for a meaning check once: %s", ctx)
	}
	if stop := runHook(t, "stop", map[string]any{"session_id": "s6", "cwd": repo, "last_assistant_message": "Explained."}); strings.Contains(stop, "ends-in-files") {
		t.Fatalf("the person is not asked before the agent checks the meaning: %s", stop)
	}
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s6", "cwd": repo, "prompt": "and explain the token types as well please"})); strings.Contains(again, "hook variants check") {
		t.Fatalf("the same run must not be handed over twice: %s", again)
	}
}

// Errors in the pre-start organizing submission say which item to fix and how (fewer retries for the agent).
func TestPlanningSubmitErrorsSayWhatToFix(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	_, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": turn, "agreed": []any{map[string]any{"summary": "x"}}, "open": map[string]any{"unknown": true}})
	if err == nil || !strings.Contains(err.Error(), "agreed") || !strings.Contains(err.Error(), "an object") {
		t.Fatalf("an array for agreed must say it should be an object: %v", err)
	}
	payload := planningPayload(root)
	payload["agreed"] = map[string]any{"summary": "s", "evidence": []map[string]string{{"source": "previous_assistant", "quote": "x"}}}
	if _, err := planningCall(t, "submit", payload); err == nil || !strings.Contains(err.Error(), "previous_assistant") || !strings.Contains(err.Error(), "current_user") {
		t.Fatalf("a missing source must name it and the available ones: %v", err)
	}
	payload["agreed"] = map[string]any{"summary": "s", "evidence": []map[string]string{{"source": "current_user", "quote": "not in the text"}}}
	if _, err := planningCall(t, "submit", payload); err == nil || !strings.Contains(err.Error(), "evidence[0]") {
		t.Fatalf("a quote that is not in the source must name the evidence: %v", err)
	}
}

// Ask only when "tried and no line". Do not ask while calibration may still be running, nor after a line is drawn. Results that cannot be adopted are not told to the person.
func TestStuckWordingAndWordingSummaryEdges(t *testing.T) {
	agent := hookEnv(t, nil)
	path := filepath.Join(agent, "decisions", "prompt-kind.yaml")
	sp := mustLoad(t, path)
	guard := filepath.Join(agent, "calibrating-prompt-kind")
	if err := os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-23T00:00:00+09:00 kev-4b %d %s\n", sp.Eval.MinN+10, sp.Hash())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := stuckWording(agent, "prompt-kind"); ok {
		t.Fatal("a calibration that may still be running must not count as stuck")
	}
	old := time.Now().Add(-2 * hookCalibrateGap)
	if err := os.Chtimes(guard, old, old); err != nil {
		t.Fatal(err)
	}
	if n, ok := stuckWording(agent, "prompt-kind"); !ok || n != sp.Eval.MinN+10 {
		t.Fatalf("a finished attempt without a line must count as stuck: %d %v", n, ok)
	}
	sp.Calibration = []spec.Calibration{{Model: "kev-4b", SpecHash: sp.Hash(), Method: "identity", N: sp.Eval.MinN}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := stuckWording(agent, "prompt-kind"); ok {
		t.Fatal("a spec with a line is not stuck")
	}
	runs := filepath.Join(agent, "wiki", "runs")
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(map[string]any{"spec": "prompt-kind", "spec_hash": mustLoad(t, path).Hash(), "run": "r1", "at": "2026-10-23T01:00:00+09:00", "winner": "current", "adoptable": false})
	if err := os.WriteFile(filepath.Join(runs, "r1.json"), result, 0o644); err != nil {
		t.Fatal(err)
	}
	if line := wordingSummary(agent); line != "" {
		t.Fatalf("a run without an adoptable winner must not be told: %s", line)
	}
}

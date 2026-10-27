package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

// With the English setting, "unsure" tells the person only the variant's name; the Japanese notice also carries the agent's view of the difference and both wordings.
func TestAuditEnglishUnsureNoticeCarriesTheDifference(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	delete(cfg, "language")
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	diff := "the candidate no longer allows supporting files such as tests"
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "unsure", "difference": diff}); err != nil {
		t.Fatal(err)
	}
	s := wordingSummary(agent)
	if !strings.Contains(s, "undecided") && !strings.Contains(s, "could not decide") {
		t.Fatalf("no unsure notice: %q", s)
	}
	if !strings.Contains(s, diff) {
		t.Fatalf("the English notice does not tell the person what the difference is: %q", s)
	}
}

// English notice for a project copy from 0.9.1 (display.ja only): name it like the page does (the title in any language), not "this question"
func TestAuditProbeTitleFallsBackLikeThePage(t *testing.T) {
	sp := &spec.Spec{ID: "needs-plan", Display: map[string]spec.Display{"ja": {Title: "始める前に聞くべきだったか"}}}
	if got := questionTitle(sp, "en"); got != "始める前に聞くべきだったか" {
		t.Fatalf("the English notice names the question as the page does: %q", got)
	}
	if got := questionTitle(&spec.Spec{ID: "my-question"}, "ja"); got != "my-question" {
		t.Fatalf("with no display at all the page shows the id; the notice must name something the person can find: %q", got)
	}
}

// Found while doing 0.9.9 (2026-10-27): the hook adopted an agent-checked wording with only the local model's URL, so with a TypeSafe key
// and no local model the calibration's replay failed ("dial tcp 127.0.0.1:8080: connection refused") and a wording measured on Jev was
// never adopted. The adoption uses the same endpoints the hook decides with.
func TestPatch099WordingAdoptionUsesTheHooksEndpoints(t *testing.T) {
	_, _, res := wordingCheckFixture(t)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("SHIKII_MODEL", "")
	var got endpointFlags
	adopt := wordingAdopt
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		got = ef
		return adopt(r, ef, storeDir, labeled, reports...)
	}
	if out, err := submitWordingCheck(t, checkBody(res, "same")); err != nil || out["adopted"] != true {
		t.Fatalf("precondition: adopted: %v %v", out, err)
	}
	if got.hosted != hookHostedURL || got.local != hookLocalURL {
		t.Fatalf("the adoption calibrates with the hook's endpoints (hosted %q, local %q): %+v", hookHostedURL, hookLocalURL, got)
	}
}

func TestWordingCheckAskedOfAgentOncePerRun(t *testing.T) {
	agent, repo, res := wordingCheckFixture(t)
	if s := wordingSummary(agent); s != "" {
		t.Fatalf("do not ask the person before the AI checks: %q", s)
	}
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	for _, want := range []string{"hook variants check", res.Run, res.WinnerWording.Question[:40], res.Current.Question[:40], "current_translation", "translate both into 日本語"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("the text asking the agent to check meaning and translate lacks %q: %q", want, ctx)
		}
	}
	prompt["session_id"] = "s2"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "hook variants check") {
		t.Fatalf("ask about the same run only once: %q", again)
	}
}

func TestWordingCheckSameAdoptsAndTellsPersonWithTranslation(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	out, err := submitWordingCheck(t, checkBody(res, "same"))
	if err != nil || out["adopted"] != true {
		t.Fatalf("adopt when the same: %v %v", out, err)
	}
	got, err := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if err != nil || !got.Adopted || !got.AutoAdopted || len(got.MeaningChecks) != 1 || got.MeaningChecks[0].By != "agent" || got.MeaningChecks[0].Candidate == "" {
		t.Fatalf("keep the AI's check and translation, and mark it as adopted automatically: %+v %v", got, err)
	}
	if p := hookSpecPath(agent, "changes-in-scope"); p != got.AdoptedPath {
		t.Fatalf("the hook reads the adopted version: %s", p)
	}
	summary := wordingSummary(agent)
	for _, want := range []string{"依頼の範囲の中でファイルを変えたか", "頼まれていないファイルを変えたか", "元に戻せます"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("tell the person with the translation and say it can be reverted (missing %q): %q", want, summary)
		}
	}
	if again := wordingSummary(agent); again != "" {
		t.Fatalf("tell only once: %q", again)
	}
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err == nil {
		t.Fatal("do not check an adopted run again")
	}
}

func TestWordingCheckUnsureWaitsForPersonAndDifferentRejects(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"}); err == nil {
		t.Fatal("do not accept a check without a translation")
	}
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": "other-run", "verdict": "same", "current_translation": "a", "candidate_translation": "b"}); err == nil {
		t.Fatal("do not accept a run that is not the latest")
	}
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "unsure", "current_translation": "a", "candidate_translation": "b"}); err == nil {
		t.Fatal("when it cannot decide, an assessment of the difference is required")
	}
	out, err := submitWordingCheck(t, checkBody(res, "unsure"))
	if err != nil || out["adopted"] != false {
		t.Fatalf("do not adopt when it cannot decide: %v %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(agent, "decisions", fmt.Sprintf("changes-in-scope-v%d.yaml", res.Version+1))); err == nil {
		t.Fatal("do not write a new version when it cannot decide")
	}
	summary := wordingSummary(agent)
	for _, want := range []string{"決めきれません", "聞く順だけが違う", "頼まれていないファイルを変えたか"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("ask the person with the translation and the difference (missing %q): %q", want, summary)
		}
	}
	// Another run: different → do not adopt, and do not ask the person either
	agent2, _, res2 := wordingCheckFixture(t)
	if out, err := submitWordingCheck(t, checkBody(res2, "different")); err != nil || out["adopted"] != false {
		t.Fatalf("do not adopt when different: %v %v", out, err)
	}
	if s := wordingSummary(agent2); s != "" {
		t.Fatalf("do not ask the person when different: %q", s)
	}
}

// If the person's configured language is English, do not ask for a translation, and accept a check without one (the owner: no translation needed when the language setting is English).
func TestWordingCheckNeedsNoTranslationInEnglish(t *testing.T) {
	agent, repo, res := wordingCheckFixture(t)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	delete(cfg, "language")
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "Fix the typo in the README."}))
	if !strings.Contains(ctx, "hook variants check") || strings.Contains(ctx, "translation") || strings.Contains(ctx, "訳して") {
		t.Fatalf("with the English setting, do not ask for a translation: %q", ctx)
	}
	got, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"})
	if err != nil || got["adopted"] != true {
		t.Fatalf("with the English setting, accept a check without a translation: %v %v", got, err)
	}
	if s := wordingSummary(agent); !strings.Contains(s, "undo it on the page") {
		t.Fatalf("notify in English: %q", s)
	}
}

// With a setting other than English, ask for a translation into that language and add it to the notice (the owner: other languages are planned, so if a language other than English is set, make the translation in that language).
func TestWordingCheckTranslatesIntoTheSetLanguage(t *testing.T) {
	agent, repo, res := wordingCheckFixture(t)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	cfg["language"] = "German"
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "Bitte den Tippfehler beheben."}))
	if !strings.Contains(ctx, "translate both into Deutsch") {
		t.Fatalf("ask for a translation into the configured language (German): %q", ctx)
	}
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"}); err == nil {
		t.Fatal("a setting other than English needs a translation")
	}
	body := map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same", "current_translation": "Lag alles im Rahmen?", "candidate_translation": "Wurde etwas Ungefragtes geändert?"}
	if got, err := submitWordingCheck(t, body); err != nil || got["adopted"] != true {
		t.Fatalf("accept it when there is a translation: %v %v", got, err)
	}
	if s := wordingSummary(agent); !strings.Contains(s, "Lag alles im Rahmen?") || !strings.Contains(s, "Wurde etwas Ungefragtes geändert?") {
		t.Fatalf("add the translation in the configured language to the notice: %q", s)
	}
}

// Audit 0.9.2 (English), medium 5: the meaning check was asked only once. When the agent's call failed (it ran the command in the wrong folder),
// nobody was asked again and the person heard nothing. A request with no check yet is asked again on a later day.
func TestWordingCheckIsAskedAgainOnALaterDay(t *testing.T) {
	agent, repo, res := wordingCheckFixture(t)
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	if ctx := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); !strings.Contains(ctx, "hook variants check") {
		t.Fatalf("asked the first time: %q", ctx)
	}
	mark := filepath.Join(agent, "wording-check-asked-"+res.SpecID)
	earlier := time.Now().Add(-26 * time.Hour) // asked on an earlier day, and still no check
	if err := os.Chtimes(mark, earlier, earlier); err != nil {
		t.Fatal(err)
	}
	prompt["session_id"] = "s1b"
	if ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1c", "cwd": repo, "prompt": "Fix the typo in the guide."})); !strings.Contains(ctx, "hook variants check") {
		t.Fatalf("an unchecked candidate is asked again on a later day: %q", ctx)
	}
	// asked again today: not a third time on the same day
	prompt["session_id"], prompt["prompt"] = "s2", "CHANGELOG の誤字を直してください。"
	if ctx := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(ctx, "hook variants check") {
		t.Fatalf("asked at most once a day: %q", ctx)
	}
}

// Audit 0.9.2 (English), medium 5: when the agent said "same" but the new wording could not be adopted (calibration failed), the person is told once.
func TestWordingSameButNotAdoptedIsToldToThePerson(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		return "", fmt.Errorf("not adopted: the new wording could not be calibrated")
	}
	out, err := submitWordingCheck(t, checkBody(res, "same"))
	if err != nil || out["adopted"] != false {
		t.Fatalf("not adopted when calibration fails: %v %v", out, err)
	}
	if s := wordingSummary(agent); !strings.Contains(s, "could not be adopted") || !strings.Contains(s, "could not be calibrated") {
		t.Fatalf("the person is told that the agreed wording could not be adopted, and why: %q", s)
	}
	if again := wordingSummary(agent); again != "" {
		t.Fatalf("told once: %q", again)
	}
}

// A tune result keeps the spec's absolute path. After the project folder was moved or renamed, the agent's "same" could not adopt the wording
// ("open …/old-name/.shikii/decisions/changes-in-scope.yaml: no such file", 2026-10-26). A result read from this wiki uses this project's paths
// (the spec of the same file name, and this wiki's run page), so the adoption is read from and saved in this project (audit 0.9.2: a move changes
// spec_path and run_page together; adoptWording still checks that the spec's hash is the one that was measured).
func TestWordingCheckAdoptsAfterTheFolderMoved(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	var adoptedFrom string
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		if _, err := os.Stat(r.SpecPath); err != nil {
			return "", err
		}
		adoptedFrom = r.SpecPath
		r.Adopted, r.AdoptedPath = true, r.SpecPath+".v-next"
		return r.AdoptedPath, nil
	}
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) { // recorded before the move
		r.SpecPath = filepath.Join("/nonexistent/old-name/.shikii/decisions", filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join("/nonexistent/old-name/.shikii/wiki/runs", filepath.Base(r.RunPage))
	})
	out, err := submitWordingCheck(t, checkBody(res, "same"))
	if err != nil || out["adopted"] != true {
		t.Fatalf("the wording is adopted from this project's spec after the folder moved: %v %v", out, err)
	}
	if adoptedFrom != hookSpecPath(agent, res.SpecID) {
		t.Fatalf("adopted from this project's spec: %s", adoptedFrom)
	}
	if got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run); !got.Adopted || len(got.MeaningChecks) != 1 {
		t.Fatalf("the adoption is saved in this project's wiki: %+v", got)
	}
}

// Audit 0.9.2 (ui): the person reads the Japanese wording notices outside the page, so they use the page's plain words: the question's Japanese title instead of
// the spec id and the candidate's name, no inner word 判定 (glossary), and the count is of truths, not "your" answers (0.9.2 counts the agent's truths too).
func TestWordingNoticesUsePlainWordsAndTitle(t *testing.T) {
	notices := map[string]func(t *testing.T) (string, *tuneResult){
		"adopted": func(t *testing.T) (string, *tuneResult) {
			agent, _, res := wordingCheckFixture(t)
			if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
				t.Fatal(err)
			}
			return wordingSummary(agent), res
		},
		"unsure": func(t *testing.T) (string, *tuneResult) {
			agent, _, res := wordingCheckFixture(t)
			if _, err := submitWordingCheck(t, checkBody(res, "unsure")); err != nil {
				t.Fatal(err)
			}
			return wordingSummary(agent), res
		},
		"failed": func(t *testing.T) (string, *tuneResult) {
			agent, _, res := wordingCheckFixture(t)
			wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
				return "", fmt.Errorf("not adopted: the new wording could not be calibrated")
			}
			if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
				t.Fatal(err)
			}
			return wordingSummary(agent), res
		},
	}
	title := "AI が変えたファイルは、頼んだことに必要な物だけか" // display.ja.title of changes-in-scope
	for state, make := range notices {
		t.Run(state, func(t *testing.T) {
			summary, res := make(t)
			en, ja, ok := strings.Cut(summary, "\n")
			if !ok || ja == "" {
				t.Fatalf("a Japanese notice is shown: %q", summary)
			}
			for _, bad := range []string{"判定", res.SpecID, res.Winner, "あなたの"} {
				if strings.Contains(ja, bad) {
					t.Fatalf("the Japanese notice holds %q: %q", bad, ja)
				}
			}
			if !strings.Contains(ja, "「"+title+"」") {
				t.Fatalf("the Japanese notice names the question by its title: %q", ja)
			}
			if regexp.MustCompile(`(?i)\byour answers?\b`).MatchString(en) {
				t.Fatalf("the English notice calls the counted truths the person's answers: %q", en)
			}
			// 0.9.3: the English notice names the question by its English title and gives the candidate's question, not the spec id or the candidate's internal name
			for _, bad := range []string{res.SpecID, res.Winner} {
				if strings.Contains(en, bad) {
					t.Fatalf("the English notice holds %q: %q", bad, en)
				}
			}
			if !strings.Contains(en, `"Were the changed files all needed?"`) || !strings.Contains(en, "Did the agent change any file nobody asked for?") {
				t.Fatalf("the English notice names the question by its title and shows the candidate's question: %q", en)
			}
		})
	}
}

// Low 2, the other way: while the page adopts a run, the agent's check of the same run is refused (it does not adopt it a second time).
func TestAgentCheckRefusedWhilePageAdopts(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	unlock, err := lockTuneRun(filepath.Join(agent, "wiki"), res.Run)
	if err != nil {
		t.Fatal(err)
	}
	_, err = submitWordingCheck(t, checkBody(res, "same"))
	unlock()
	if err == nil || !strings.Contains(err.Error(), "being adopted") {
		t.Fatalf("refused while the run is being adopted: %v", err)
	}
	if got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run); got.Adopted || len(got.MeaningChecks) != 0 {
		t.Fatalf("nothing is recorded while refused: %+v", got)
	}
	if out, err := submitWordingCheck(t, checkBody(res, "same")); err != nil || out["adopted"] != true {
		t.Fatalf("after the lock is released, the check adopts: %v %v", out, err)
	}
}

// Medium 1: after the folder moved, the result's run_page still pointed at the old place, so the agent's adoption and check were not saved
// (the new version was in use, but the person was not told and could not undo it).
func TestAuditMovedFolderAdoptionIsSaved(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.SpecPath = filepath.Join(movedStore, "decisions", filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join(movedStore, "wiki", "runs", filepath.Base(r.RunPage))
	})
	out, err := submitWordingCheck(t, checkBody(res, "same"))
	if err != nil || out["adopted"] != true {
		t.Fatalf("adopt: %v %v", out, err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if !got.Adopted || !got.AutoAdopted || len(got.MeaningChecks) != 1 {
		t.Fatalf("the adoption and the check are kept in this wiki's result: adopted=%v auto=%v checks=%d (in use: %s)", got.Adopted, got.AutoAdopted, len(got.MeaningChecks), hookSpecPath(agent, res.SpecID))
	}
	if md, _ := os.ReadFile(res.RunPage); !strings.Contains(string(md), "**Adopted**") {
		t.Fatalf("the note goes to this wiki's run page: %q", md)
	}
	if s := wordingSummary(agent); s == "" {
		t.Fatal("the person is told about the adoption")
	}
}

// Medium 2: in a copied project the recorded paths still point at the original project, which still exists. Adoption wrote a new version
// into the original project's decisions/ and its wiki. It must read and write only this project.
func TestAuditCopiedProjectAdoptsInsideThisProject(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	other := filepath.Join(t.TempDir(), ".shikii")
	od, ow := filepath.Join(other, "decisions"), filepath.Join(other, "wiki", "runs")
	for _, d := range []string{od, ow} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(res.SpecPath)
	_ = os.WriteFile(filepath.Join(od, filepath.Base(res.SpecPath)), b, 0o600)
	_ = os.WriteFile(filepath.Join(ow, res.Run+".md"), []byte("# run\n"), 0o600)
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.SpecPath = filepath.Join(od, filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join(ow, res.Run+".md")
	})
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
		t.Fatal(err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if !got.Adopted || !strings.HasPrefix(got.AdoptedPath, agent+string(filepath.Separator)) {
		t.Fatalf("the new version is written inside this project: adopted=%v %s", got.Adopted, got.AdoptedPath)
	}
	if files, _ := filepath.Glob(filepath.Join(od, "*-v*.yaml")); len(files) > 0 {
		t.Fatalf("nothing is written into the original project's decisions: %v", files)
	}
	if _, err := os.Stat(filepath.Join(ow, res.Run+".json")); err == nil {
		t.Fatal("nothing is written into the original project's wiki")
	}
	if md, _ := os.ReadFile(filepath.Join(ow, res.Run+".md")); string(md) != "# run\n" {
		t.Fatalf("the original project's run page is untouched: %q", md)
	}
}

// Audit 0.9.2 (behavior), low 4: what the English notice about a changed wording shows.

// Low 4: with the English setting, the notice that the wording was changed shows the current and the new question (shortened), not only the candidate's name.
func TestWordingAdoptedNoticeInEnglishShowsBothQuestions(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	delete(cfg, "language")
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"}); err != nil || got["adopted"] != true {
		t.Fatalf("adopt: %v %v", got, err)
	}
	s := wordingSummary(agent)
	for _, want := range []string{"current: " + cut(res.Current.Question, 30), "new: " + cut(res.WinnerWording.Question, 30)} {
		if !strings.Contains(s, want) {
			t.Fatalf("the English notice shows %q: %q", want, s)
		}
	}
	if long := trim(res.Current.Question, wordingNoticeRunes); len([]rune(res.Current.Question)) > wordingNoticeRunes && !strings.Contains(s, long) {
		t.Fatalf("a long question is shortened: %q", s)
	}
}

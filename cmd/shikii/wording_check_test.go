package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
)

// 0.9.2: when a wording candidate beats the current question, an agent (LLM), not the person, checks whether the meaning is the same and adds a Japanese translation.
// Same → adopt automatically, tell the person with the translation, and let them revert. Cannot decide → ask the person with the translation and the difference. Different → do not adopt.

// wordingCheckFixture puts one adoptable tune result on the hook question changes-in-scope. Adoption (calibration) is swapped for a fake.
func wordingCheckFixture(t *testing.T) (agent, repo string, res *tuneResult) {
	t.Helper()
	repo = gitRepo(t)
	agent = hookEnv(t, nil, repo)
	t.Chdir(repo)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	cfg["language"] = "Japanese"
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	sp, err := spec.Load(hookSpecPath(agent, "changes-in-scope"))
	if err != nil {
		t.Fatal(err)
	}
	v := spec.Variant{Name: "unasked-first", Question: "Did the agent change any file nobody asked for? Answer no if it did."}
	res = &tuneResult{SpecID: sp.ID, SpecPath: sp.Path(), SpecHash: sp.Hash(), Version: sp.Version, Model: "m", Truths: 50, At: "2026-10-25T10:00:00Z",
		Winner: v.Name, Adoptable: true, WinnerVariant: &v, WinnerWording: wordingOf(sp.WithVariant(v)), Current: wordingOf(sp),
		Candidates: []candidateResult{{Name: "current", TopOne: 0.321}, {Name: v.Name, TopOne: 0.786}}}
	runs := filepath.Join(agent, "wiki", "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	res.RunPage = filepath.Join(runs, "changes-in-scope-20261025-100000.md")
	res.Run = strings.TrimSuffix(filepath.Base(res.RunPage), ".md")
	if err := os.WriteFile(res.RunPage, []byte("# run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveTuneResult(res)
	old := wordingAdopt
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		base, err := os.ReadFile(r.SpecPath)
		if err != nil {
			return "", err
		}
		next := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(base), fmt.Sprintf("version: %d", r.Version+1))
		path := filepath.Join(filepath.Dir(r.SpecPath), fmt.Sprintf("%s-v%d.yaml", r.SpecID, r.Version+1))
		if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
			return "", err
		}
		r.Adopted, r.AdoptedPath = true, path
		return path, nil
	}
	t.Cleanup(func() { wordingAdopt = old })
	return agent, repo, res
}

func submitWordingCheck(t *testing.T, body map[string]any) (map[string]any, error) {
	t.Helper()
	b, _ := json.Marshal(body)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	var out string
	var err error
	out = capture(t, func() error { err = cmdHookVariants([]string{"check"}); return nil })
	var v map[string]any
	_ = json.Unmarshal([]byte(out), &v)
	return v, err
}

func checkBody(res *tuneResult, verdict string) map[string]any {
	return map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": verdict,
		"current_translation": "依頼の範囲の中でファイルを変えたか", "candidate_translation": "頼まれていないファイルを変えたか", "difference": "聞く順だけが違う"}
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

// For wording adopted automatically, the person can choose "keep as is" or "revert" on the page. Reverting goes back to the previous version, and that run can no longer be adopted.
func TestWordingReviewKeepsOrReverts(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	adopted := got.AdoptedPath
	if code, _ := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run}); code != 400 {
		t.Fatalf("400 without keep (true/false): %d", code)
	}
	if code, r := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": false}); code != 200 || r["reverted"] != true {
		t.Fatalf("revert: %d %v", code, r)
	}
	if _, err := os.Stat(adopted); err == nil {
		t.Fatal("after reverting, rename the adopted version's file to a name that is not used")
	}
	if _, err := os.Stat(adopted + ".reverted"); err != nil {
		t.Fatal("keep the adopted version instead of deleting it")
	}
	if p := hookSpecPath(agent, "changes-in-scope"); p != res.SpecPath {
		t.Fatalf("after reverting, read the previous version: %s", p)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": true}); code != 409 {
		t.Fatalf("a reverted run can no longer be chosen: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/tune/adopt", map[string]any{"spec": res.SpecID, "run": res.Run, "meaning_same": true}); code != 409 {
		t.Fatalf("a reverted run cannot be adopted again: %d", code)
	}
	// Another run: keep as is → keep the person's confirmation and do not notify again
	agent2, _, res2 := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res2, "same")); err != nil {
		t.Fatal(err)
	}
	srv2 := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent2, "decisions"), storeDir: agent2}))
	t.Cleanup(srv2.Close)
	if code, r := postJSON(t, srv2.URL+"/v1/tune/review", map[string]any{"spec": res2.SpecID, "run": res2.Run, "keep": true}); code != 200 || r["reverted"] != false {
		t.Fatalf("keep as is: %d %v", code, r)
	}
	kept, _ := loadTuneResult(filepath.Join(agent2, "wiki"), res2.Run)
	if !kept.Adopted || len(kept.MeaningChecks) != 2 || kept.MeaningChecks[1].By != "page" || !kept.MeaningChecks[1].Same {
		t.Fatalf("keep the person's \"keep as is\": %+v", kept)
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

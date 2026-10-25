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

	"github.com/hayaenfj/shikii/internal/spec"
)

// 0.9.2: 聞き方の候補が今の質問を上回った時、意味が同じかは人ではなくエージェント(LLM)が確かめ、和訳を付ける。
// 同じ → 自動で採用し、人には和訳付きで知らせて元に戻せるようにする。決めきれない → 和訳と違いを付けて人に聞く。違う → 採用しない。

// wordingCheckFixture は hook の問い changes-in-scope に、採用できる tune の結果を 1 つ置く。採用(較正)は偽物に差し替える。
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
		t.Fatalf("AI が確かめる前は人に聞かない: %q", s)
	}
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	for _, want := range []string{"hook variants check", res.Run, res.WinnerWording.Question[:40], res.Current.Question[:40], "current_translation", "translate both into 日本語"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("エージェントに意味の確かめと和訳を頼む文に %q が無い: %q", want, ctx)
		}
	}
	prompt["session_id"] = "s2"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "hook variants check") {
		t.Fatalf("同じ run は一度だけ頼む: %q", again)
	}
}

func TestWordingCheckSameAdoptsAndTellsPersonWithTranslation(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	out, err := submitWordingCheck(t, checkBody(res, "same"))
	if err != nil || out["adopted"] != true {
		t.Fatalf("同じなら採用する: %v %v", out, err)
	}
	got, err := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if err != nil || !got.Adopted || !got.AutoAdopted || len(got.MeaningChecks) != 1 || got.MeaningChecks[0].By != "agent" || got.MeaningChecks[0].Candidate == "" {
		t.Fatalf("AI の確かめと和訳を残し、自動で採用した印を付ける: %+v %v", got, err)
	}
	if p := hookSpecPath(agent, "changes-in-scope"); p != got.AdoptedPath {
		t.Fatalf("hook は採用した版を読む: %s", p)
	}
	summary := wordingSummary(agent)
	for _, want := range []string{"依頼の範囲の中でファイルを変えたか", "頼まれていないファイルを変えたか", "元に戻せます"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("人へ和訳付きで知らせ、元に戻せると書く(%q が無い): %q", want, summary)
		}
	}
	if again := wordingSummary(agent); again != "" {
		t.Fatalf("知らせは一度だけ: %q", again)
	}
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err == nil {
		t.Fatal("採用済みの run は確かめ直さない")
	}
}

func TestWordingCheckUnsureWaitsForPersonAndDifferentRejects(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"}); err == nil {
		t.Fatal("和訳が無い確かめは受け取らない")
	}
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": "other-run", "verdict": "same", "current_translation": "a", "candidate_translation": "b"}); err == nil {
		t.Fatal("最新でない run は受け取らない")
	}
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "unsure", "current_translation": "a", "candidate_translation": "b"}); err == nil {
		t.Fatal("決めきれない時は違いの見立てが要る")
	}
	out, err := submitWordingCheck(t, checkBody(res, "unsure"))
	if err != nil || out["adopted"] != false {
		t.Fatalf("決めきれない時は採用しない: %v %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(agent, "decisions", fmt.Sprintf("changes-in-scope-v%d.yaml", res.Version+1))); err == nil {
		t.Fatal("決めきれない時は新しい版を書かない")
	}
	summary := wordingSummary(agent)
	for _, want := range []string{"決めきれません", "聞く順だけが違う", "頼まれていないファイルを変えたか"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("人に和訳と違いを付けて聞く(%q が無い): %q", want, summary)
		}
	}
	// 別の run: 違う → 採用せず、人にも聞かない
	agent2, _, res2 := wordingCheckFixture(t)
	if out, err := submitWordingCheck(t, checkBody(res2, "different")); err != nil || out["adopted"] != false {
		t.Fatalf("違う時は採用しない: %v %v", out, err)
	}
	if s := wordingSummary(agent2); s != "" {
		t.Fatalf("違う時は人に聞かない: %q", s)
	}
}

// 自動で採用した聞き方は、人が画面で「このままでよい」か「元に戻す」を選べる。元に戻すと前の版に戻り、その run はもう採用できない。
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
		t.Fatalf("keep(true/false)が無ければ 400: %d", code)
	}
	if code, r := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": false}); code != 200 || r["reverted"] != true {
		t.Fatalf("元に戻す: %d %v", code, r)
	}
	if _, err := os.Stat(adopted); err == nil {
		t.Fatal("元に戻したら、採用した版のファイルは使われない名にする")
	}
	if _, err := os.Stat(adopted + ".reverted"); err != nil {
		t.Fatal("採用した版は消さずに残す")
	}
	if p := hookSpecPath(agent, "changes-in-scope"); p != res.SpecPath {
		t.Fatalf("元に戻した後は前の版を読む: %s", p)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": true}); code != 409 {
		t.Fatalf("元に戻した run はもう選べない: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/tune/adopt", map[string]any{"spec": res.SpecID, "run": res.Run, "meaning_same": true}); code != 409 {
		t.Fatalf("元に戻した run は採用し直せない: %d", code)
	}
	// 別の run: このままでよい → 人の確かめを残し、もう知らせない
	agent2, _, res2 := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res2, "same")); err != nil {
		t.Fatal(err)
	}
	srv2 := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent2, "decisions"), storeDir: agent2}))
	t.Cleanup(srv2.Close)
	if code, r := postJSON(t, srv2.URL+"/v1/tune/review", map[string]any{"spec": res2.SpecID, "run": res2.Run, "keep": true}); code != 200 || r["reverted"] != false {
		t.Fatalf("このままでよい: %d %v", code, r)
	}
	kept, _ := loadTuneResult(filepath.Join(agent2, "wiki"), res2.Run)
	if !kept.Adopted || len(kept.MeaningChecks) != 2 || kept.MeaningChecks[1].By != "page" || !kept.MeaningChecks[1].Same {
		t.Fatalf("人の「このままでよい」を残す: %+v", kept)
	}
}

// 人の設定の言語が英語なら、訳は頼まず、無くても受け取る(本人「言語設定英語なら不用」)。
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
		t.Fatalf("英語の設定では訳を頼まない: %q", ctx)
	}
	got, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"})
	if err != nil || got["adopted"] != true {
		t.Fatalf("英語の設定では訳が無くても受け取る: %v %v", got, err)
	}
	if s := wordingSummary(agent); !strings.Contains(s, "undo it on the page") {
		t.Fatalf("英語で知らせる: %q", s)
	}
}

// 英語以外の設定では、その言語の訳を頼み、知らせにも添える(本人「他言語化するよていなので、英語以外が設定されていたらその言語で訳を作る」)。
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
		t.Fatalf("設定の言語(ドイツ語)に訳すよう頼む: %q", ctx)
	}
	if _, err := submitWordingCheck(t, map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same"}); err == nil {
		t.Fatal("英語以外の設定では訳が要る")
	}
	body := map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": "same", "current_translation": "Lag alles im Rahmen?", "candidate_translation": "Wurde etwas Ungefragtes geändert?"}
	if got, err := submitWordingCheck(t, body); err != nil || got["adopted"] != true {
		t.Fatalf("訳があれば受け取る: %v %v", got, err)
	}
	if s := wordingSummary(agent); !strings.Contains(s, "Lag alles im Rahmen?") || !strings.Contains(s, "Wurde etwas Ungefragtes geändert?") {
		t.Fatalf("知らせに設定の言語の訳を添える: %q", s)
	}
}

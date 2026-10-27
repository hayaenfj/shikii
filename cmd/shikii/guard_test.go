package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// Each of the 3 kinds of checks that stop the turn (0.7.3) stops on its negative example and passes its positive example. Unknown observations (the file list or commits cannot be read) pass.
func TestGuardKindsStopOnNegativesAndPassPositives(t *testing.T) {
	pair := guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}}
	words := guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "根・指紋を使わない", Words: []string{"根", "指紋"}, Except: []string{"docs/operations.md", "wiki/**"}}
	body := guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "コミット本文に背景と検証", Required: []string{"元の問題", "検証"}}
	for _, g := range []guard{pair, words, body} {
		if err := g.validate(); err != nil {
			t.Fatalf("%s: %v", g.ID, err)
		}
		neg, what := guardNegative(g)
		if r := evalGuard(g, neg); r.Pass || r.Reason == "" || what == "" {
			t.Fatalf("%s stops on the negative example (%s): %+v", g.ID, what, r)
		}
		if r := evalGuard(g, guardPositive(g)); !r.Pass {
			t.Fatalf("%s passes the positive example: %+v", g.ID, r)
		}
		if r := evalGuard(g, guardInput{}); !r.Pass {
			t.Fatalf("%s passes when nothing was observed: %+v", g.ID, r)
		}
	}
	// paired-files: passes when both change, stops when only one changes (either way round), passes when the file list cannot be read
	if r := evalGuard(pair, guardInput{Changed: []string{"CHANGELOG_ja.md"}, Known: true}); r.Pass || !strings.Contains(r.Reason, "CHANGELOG.md did not") {
		t.Fatalf("stops the other way round too: %+v", r)
	}
	if r := evalGuard(pair, guardInput{Changed: []string{"CHANGELOG.md"}, Known: false}); !r.Pass {
		t.Fatalf("passes when the file list cannot be read: %+v", r)
	}
	// forbidden-words: paths in except are not checked; the matching line and word are given as the reason
	in := guardInput{Known: true, Changed: []string{"docs/operations.md", "wiki/x.md", "README.md"}, Added: map[string][]string{"docs/operations.md": {"根"}, "wiki/x.md": {"指紋"}, "README.md": {"ここは問題ない"}}}
	if r := evalGuard(words, in); !r.Pass {
		t.Fatalf("paths in except are not checked: %+v", r)
	}
	in.Added["README.md"] = append(in.Added["README.md"], "プロジェクトの根に置く")
	if r := evalGuard(words, in); r.Pass || !strings.Contains(r.Reason, `README.md: "根"`) {
		t.Fatalf("stops on a word in an added line: %+v", r)
	}
	// commit-body: lists the words missing from the body. A subject-only commit stops too. Passes when the commits are unknown
	if r := evalGuard(body, guardInput{CommitsKnown: true, Commits: []string{"fix: x\n\n元の問題: y"}}); r.Pass || !strings.Contains(r.Reason, `"検証"`) || strings.Contains(r.Reason, `"元の問題"`) {
		t.Fatalf("lists only the missing words: %+v", r)
	}
	if r := evalGuard(body, guardInput{CommitsKnown: false, Commits: []string{"x"}}); !r.Pass {
		t.Fatalf("passes when the commits are unknown: %+v", r)
	}
	// validate: kind and fields
	for _, bad := range []guard{{ID: "A", Kind: guardKindPairedFiles, Title: "t", Pairs: [][]string{{"a", "b"}}}, {ID: "x", Kind: "other", Title: "t"}, {ID: "x", Kind: guardKindForbiddenWords, Title: "t"}, {ID: "x", Kind: guardKindPairedFiles, Title: "t", Pairs: [][]string{{"a", "a"}}}, {ID: "x", Kind: guardKindCommitBody, Title: "t", Required: []string{" "}}} {
		if err := bad.validate(); err == nil {
			t.Fatalf("an invalid guard is refused: %+v", bad)
		}
	}
	// added lines: multiset difference. Empty lines are not counted
	if got := addedLines([]byte("a\nb\n"), []byte("a\nb\nc\n\nb\n")); len(got) != 2 || got[0] != "c" || got[1] != "b" {
		t.Fatalf("added lines: %v", got)
	}
}

// add writes a draft (disabled); enable shows the negative example stop (RED) and the positive example pass (GREEN) before enabling. disable turns it back off.
func TestGuardEnableShowsRedThenGreen(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if _, err := os.Stat(filepath.Join(root, "guards")); err != nil {
		t.Fatal("init also prepares guards/: ", err)
	}
	old := stdin
	t.Cleanup(func() { stdin = old })
	stdin = strings.NewReader(`{"id":"changelog-pair","kind":"paired-files","title":"変更履歴は英語と日本語を揃える","reason":"本人の訂正","pairs":[["CHANGELOG.md","CHANGELOG_ja.md"]],"enabled":true}`)
	out := capture(t, func() error { return cmdGuard([]string{"add"}) })
	g, err := loadGuard(guardPath(root, "changelog-pair"))
	if err != nil || g.Enabled || g.Red != nil || !strings.Contains(out, "draft") {
		t.Fatalf("add ignores enabled and writes a disabled draft: %+v %v\n%s", g, err, out)
	}
	stdin = strings.NewReader(`{"id":"changelog-pair","kind":"paired-files","title":"x","pairs":[["a","b"]]}`)
	if err := cmdGuard([]string{"add"}); err == nil {
		t.Fatal("a draft with the same id is not overwritten")
	}
	out = capture(t, func() error { return cmdGuard([]string{"enable", "changelog-pair"}) })
	if !strings.Contains(out, "RED:") || !strings.Contains(out, "GREEN:") || !strings.Contains(out, ": enabled.") {
		t.Fatalf("enable shows RED → GREEN:\n%s", out)
	}
	if g, _ = loadGuard(guardPath(root, "changelog-pair")); !g.Enabled || g.Red == nil || g.Red.Negative == "" {
		t.Fatalf("enable keeps a record of the negative example and enables: %+v", g)
	}
	out = capture(t, func() error { return cmdGuard([]string{"list"}) })
	if !strings.Contains(out, "changelog-pair") || !strings.Contains(out, "enabled") {
		t.Fatalf("list shows the state:\n%s", out)
	}
	capture(t, func() error { return cmdGuard([]string{"disable", "changelog-pair"}) })
	if g, _ = loadGuard(guardPath(root, "changelog-pair")); g.Enabled {
		t.Fatal("disable turns it back off")
	}
	// try: adding only CHANGELOG.md to the working tree stops (while enabled)
	capture(t, func() error { return cmdGuard([]string{"enable", "changelog-pair"}) })
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdGuard([]string{"try"}) })
	if !strings.Contains(out, "STOP changelog-pair") {
		t.Fatalf("try runs against the working tree and shows the result:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG_ja.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = capture(t, func() error { return cmdGuard([]string{"try"}) }); !strings.Contains(out, "ok   changelog-pair") {
		t.Fatalf("passes when both are there:\n%s", out)
	}
}

// audit R2: commit-body ignores merge commits, and stops a commit with no subject and no body.
func TestCommitBodySkipsMergesAndStopsEmptyMessages(t *testing.T) {
	repo := gitRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	gitCommitAll(t, repo, "base\n\n元の問題: x\n変更後: y\n検証: z")
	run := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	head, since := gitHead(repo), time.Now()
	run("checkout", "-q", "-b", "feature")
	write("b.txt", "b")
	gitCommitAll(t, repo, "feature work\n\n元の問題: a\n変更後: b\n検証: c")
	run("checkout", "-q", "-")
	write("c.txt", "c")
	gitCommitAll(t, repo, "main work\n\n元の問題: a\n変更後: b\n検証: c")
	run("merge", "--no-ff", "-q", "-m", "Merge branch 'feature'", "feature")
	in := guardTurnInput(repo, "", nil, false, head, since)
	g := guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "x", Required: []string{"元の問題", "変更後", "検証"}}
	if r := evalGuard(g, in); !r.Pass {
		t.Fatalf("does not stop on the automatic text of a merge commit: %+v %+v", r, in.Commits)
	}
	run("commit", "-q", "--allow-empty", "--allow-empty-message", "-m", "")
	in = guardTurnInput(repo, "", nil, false, head, since)
	if r := evalGuard(g, in); r.Pass {
		t.Fatalf("stops a commit with no subject and no body: %+v", in.Commits)
	}
}

// When a forbidden word is part of an ordinary word (e.g. 「根」 in 「根拠」), the words in allow are removed before checking.
func TestForbiddenWordsHonourAllowedWords(t *testing.T) {
	g := guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}, Allow: []string{"根拠", "根本"}}
	if err := g.validate(); err != nil {
		t.Fatal(err)
	}
	in := guardInput{Known: true, Changed: []string{"a.md"}, Added: map[string][]string{"a.md": {"根拠にしない。根本から直す。"}}}
	if r := evalGuard(g, in); !r.Pass {
		t.Fatalf("a forbidden word inside an allowed word is not counted: %+v", r)
	}
	in.Added["a.md"] = append(in.Added["a.md"], "プロジェクトの根に置く(根拠はある)")
	if r := evalGuard(g, in); r.Pass {
		t.Fatalf("stops if one remains after removing the allowed words: %+v", r)
	}
	bad := g
	bad.Allow = []string{"拠点"} // a word that does not contain a forbidden word cannot be in allow (a typo)
	if err := bad.validate(); err == nil {
		t.Fatal("an allow word without a forbidden word is refused")
	}
}

// audit R3: 2. forbidden-words' allow cannot hold a forbidden word with only a particle, space or punctuation added, nor another forbidden word itself.
func TestForbiddenWordsAllowCannotBeALoophole(t *testing.T) {
	for _, a := range []string{"根を", "根の", "根 ", "「根」", "根。", "根拠"} {
		g := guard{ID: "x", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根", "根拠"}, Allow: []string{a}}
		if err := g.validate(); err == nil {
			t.Fatalf("allow %q would be a loophole, so it is refused", a)
		}
	}
	g := guard{ID: "x", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}, Allow: []string{"根拠", "根本"}}
	if err := g.validate(); err != nil {
		t.Fatalf("ordinary words are allowed: %v", err)
	}
}

// 6. enable works even when except is a broad glob (the negative example's path uses a name except does not match).
func TestGuardEnableWorksWhenExceptIsBroad(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if err := saveGuard(root, guard{ID: "no-todo", Kind: guardKindForbiddenWords, Title: "Leave no TODO", Words: []string{"TODO"}, Except: []string{"*.txt", "docs/**"}}); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdGuard([]string{"enable", "no-todo"}) })
	if !strings.Contains(out, "RED:") {
		t.Fatalf("blocks on the negative example even when except is broad: %s", out)
	}
}

// 7. Path matching also matches on the name alone (audit mutation B).
func TestMatchPathMatchesByBaseName(t *testing.T) {
	if !matchPath("CHANGELOG.md", "docs/CHANGELOG.md") || matchPath("CHANGELOG.md", "docs/CHANGELOG_ja.md") || !matchPath("docs/**", "docs/a/b.md") || matchPath("docs/**", "src/docs.md") {
		t.Fatal("matching on the name alone and under a folder")
	}
}

// audit 6 (0.9.3): a turn the person's guard stopped, whose Stop after the block never came, does not count as "fixed before the turn
// ended" because the tree at the person's next request passes (the person may have fixed it; the late judgment is not a Stop)
func TestAudit6LateJudgmentDoesNotCountAGuardAsFixed(t *testing.T) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	notes := filepath.Join(repo, "NOTES.md")
	if err := os.WriteFile(notes, []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote the notes."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the guard stops the turn: %s", out)
	}
	// Esc: no Stop after the block. The person fixes the file and asks the next thing
	if err := os.WriteFile(notes, []byte("a fine word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	guards, err := loadGuards(root)
	if err != nil {
		t.Fatal(err)
	}
	if s := countGuardStats(guards, readGuardRecords(root)); s.Stopped != 1 || s.Fixed != 0 {
		t.Fatalf("a guard that was not rechecked at a Stop must not count as fixed: %+v", s)
	}
}

// Low 1: a forbidden-words guard can be enabled even when a word is part of the English positive example ("his" is inside "this", "is", "line").
func TestAuditTranslationGuardEnableWithEnglishWords(t *testing.T) {
	for _, words := range [][]string{{"his"}, {"is"}, {"line"}, {"clean"}, {"t", "h", "i", "s", "l", "n", "e", "c", "a"}, {"ab", "aab"}} {
		g := guard{ID: "w", Kind: guardKindForbiddenWords, Title: "w", Words: words}
		if r := evalGuard(g, guardPositive(g)); !r.Pass {
			t.Errorf("words %v: the positive example must pass, but it stopped: %s", words, r.Reason)
		}
		neg, _ := guardNegative(g)
		if r := evalGuard(g, neg); r.Pass {
			t.Errorf("words %v: the negative example must stop", words)
		}
	}
	root := t.TempDir()
	if err := saveGuard(root, guard{ID: "no-his", Kind: guardKindForbiddenWords, Title: "his", Words: []string{"his"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := captureErr(func() error { return guardSetEnabled(root, "no-his", true) }); err != nil {
		t.Fatalf("guard enable refused a guard whose word is inside the positive example: %v", err)
	}
}

func captureErr(fn func() error) (string, error) {
	old := stdout
	var b strings.Builder
	stdout = &b
	defer func() { stdout = old }()
	err := fn()
	return b.String(), err
}

// 0.7.4: show the content of `shikii status` (the person's effort, the effect of delivered lessons, guards) on the page too. Adds summary to /v1/status.
func TestStatusEndpointCarriesTheSummaryForThePage(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	get := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("status JSON: %v %s", err, rec.Body.String())
		}
		s, _ := out["summary"].(map[string]any)
		if s == nil {
			t.Fatalf("status needs a summary: %s", rec.Body.String())
		}
		return s
	}
	s := get()
	mem, _ := s["memory"].(map[string]any)
	gu, _ := s["guards"].(map[string]any)
	if mem["delivered"] != 0.0 || gu["total"] != 0.0 || gu["enabled"] != 0.0 || gu["rituals"] == nil { // no person's guards; only the count of ritual checks (built in) (0.8.0)
		t.Fatalf("nothing at first (the count of ritual checks is separate): %v", s)
	}
	writeGuard(t, st.Dir, guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}})
	memoryEffectPrompt(t, scope, "s1")
	s = get()
	mem, _ = s["memory"].(map[string]any)
	gu, _ = s["guards"].(map[string]any)
	var items []any // only guards a person made (built-in ritual checks are listed with builtin:true)
	for _, it := range gu["items"].([]any) {
		if it.(map[string]any)["builtin"] != true {
			items = append(items, it)
		}
	}
	if mem["delivered"] != 1.0 || mem["pending"] != 1.0 || gu["total"] != 1.0 || gu["enabled"] != 1.0 || len(items) != 1 || items[0].(map[string]any)["id"] != "changelog-pair" {
		t.Fatalf("delivered lessons and guards appear in summary: %v", s)
	}
	person, _ := s["person"].(map[string]any)
	if person == nil || person["records"] == nil {
		t.Fatalf("the person's effort appears too: %v", s)
	}
}

// Audit R5-3: spot checks (1 in 20 of act) wait for a person; spot checks of AI-only questions wait for an outcome. status's rituals count and the model's name.
func TestStatusCountsSpotChecksRitualsAndModel(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	var samples []string
	for i := 0; i < 2000 && len(samples) < 2; i++ {
		if id := fmt.Sprintf("s%d", i); store.AuditSample(id) {
			samples = append(samples, id)
		}
	}
	if len(samples) < 2 {
		t.Fatal("no trace that hits a spot check was found")
	}
	sampled, sampledWiki := samples[0], samples[1]
	st.AppendRecord(store.Record{TraceID: sampled, SpecID: "needs-plan", SpecHash: "h", Type: "noul", Model: "m", Inputs: map[string]string{"request": "x", "agreed": "a", "open": "o"}, InputsHash: "a1", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: sampledWiki, SpecID: "wiki-conflict", SpecHash: "h", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "a", "new_lesson": "b"}, InputsHash: "a2", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	appendGuardRecords(root, []guardRecord{{Session: "s", Turn: "t1", Guard: ritualPlan, Pass: false}, {Session: "s", Turn: "t1", Guard: ritualPlan, Pass: true, Recheck: true}, {Session: "s", Turn: "t2", Guard: ritualReflect, Pass: true}})
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs   []map[string]any `json:"specs"`
		Model   map[string]any   `json:"model"`
		Summary struct {
			Guards struct {
				Rituals map[string]map[string]float64 `json:"rituals"`
			} `json:"guards"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, sp := range body.Specs {
		got[sp["id"].(string)] = sp
	}
	if np := got["needs-plan"]; np["waiting_for_person"] != float64(1) || np["settled_by_ai"] != float64(0) {
		t.Fatalf("an act that hit a spot check waits for a person: %v", np)
	}
	if wc := got["wiki-conflict"]; wc["waiting_for_person"] != float64(0) || wc["awaiting_results"] != float64(1) {
		t.Fatalf("AI-only questions do not wait for a person even when spot-checked: %v", wc)
	}
	if body.Model["name"] != "TypeSafe Jev" {
		t.Fatalf("with a key, the decision model is Jev: %v", body.Model)
	}
	r := body.Summary.Guards.Rituals
	if r["plan"]["asked"] != 1 || r["plan"]["done"] != 0 || r["plan"]["fixed"] != 1 || r["reflect"]["asked"] != 1 || r["reflect"]["done"] != 1 {
		t.Fatalf("number of rituals done: %v", r)
	}
}

// Audit R3: records/guards.jsonl (the blocking checks' record) is not counted as a decision "guards". Even without spec paths it reads .shikii/decisions/*.yaml,
// counting the same way as the page (/v1/status) (a choice's discard counts as unsure).
func TestStatusIgnoresGuardRecordsAndReadsTheStoresSpecsByDefault(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"xyz"}})
	if err := appendGuardRecords(root, []guardRecord{{Session: "s", Turn: "t", Guard: "no-ne", Pass: false, Reason: "r"}}); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "st", "cwd": repo, "prompt": "README を直して"})
	out := capture(t, func() error { return cmdStatus([]string{"--store", root}) })
	if strings.Contains(out, "guards: records=") {
		t.Fatalf("the blocking checks' record must not be counted as a decision:\n%s", out)
	}
	if !strings.Contains(out, "prompt-kind: records=1") || strings.Contains(out, "spec not given") {
		t.Fatalf("even without spec paths it must read the built-in questions in .shikii/decisions and show the calibration state:\n%s", out)
	}
}

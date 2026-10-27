package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.8.3: once truths reach eval.min_n, do not say "ready to calibrate" from the count alone; split it into only one answer so far, cannot separate,
// and line drawn, show it in status, and tell the person once at the end of a turn (once more when the state changes).
func TestLineStatesAreToldApartAndAnnouncedOnce(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := filepath.Join(agent, "decisions", "changes-in-scope.yaml")
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	add := func(truth string, n int) {
		var labels []store.Label
		for i := 0; i < n; i++ {
			labels = append(labels, store.Label{TraceID: fmt.Sprintf("%s%03d", truth, i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: fmt.Sprintf("%s%03d", truth, i), Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	status := func() string {
		return capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	}

	add("no", sp.Eval.MinN)
	if out := status(); strings.Contains(out, "ready to calibrate") || !strings.Contains(out, fmt.Sprintf("only one answer so far (no=%d)", sp.Eval.MinN)) {
		t.Fatalf("with only one answer, it says no line can be drawn even though the count is enough:\n%s", out)
	}
	first := lineSummary(agent)
	if !strings.Contains(first, `"Were the changed files all needed?"`) || !strings.Contains(first, "only one answer") {
		t.Fatalf("tells the person there is only one answer: %q", first)
	}
	if again := lineSummary(agent); again != "" {
		t.Fatalf("the same state is not told twice: %q", again)
	}

	add("yes", 5)
	if out := status(); !strings.Contains(out, "enough truths: the line is drawn automatically") {
		t.Fatalf("with both answers, it says the line is being drawn automatically:\n%s", out)
	}
	guard := filepath.Join(agent, "calibrating-changes-in-scope")
	if err := os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m1 %d %s\n", sp.Eval.MinN+5, sp.Hash())), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * hookCalibrateGap)
	if err := os.Chtimes(guard, old, old); err != nil {
		t.Fatal(err)
	}
	if out := status(); !strings.Contains(out, "tried: the answers could not be separated") {
		t.Fatalf("when tried and no line was drawn, it says the answers cannot be separated:\n%s", out)
	}
	if line := lineSummary(agent); !strings.Contains(line, "could not be separated") {
		t.Fatalf("tells the person the answers cannot be separated (once more, since the state changed): %q", line)
	}

	sp.Calibration = []spec.Calibration{{Model: "m1", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN + 5}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if out := status(); !strings.Contains(out, "calibrated: platt") {
		t.Fatalf("the line was drawn: \n%s", out)
	}
	drawn := lineSummary(agent)
	if !strings.Contains(drawn, "a line was drawn") || !strings.Contains(drawn, "goes ahead") {
		t.Fatalf("tells the person a line was drawn and that the AI goes ahead when confident from now on: %q", drawn)
	}
	if again := lineSummary(agent); again != "" {
		t.Fatalf("a drawn line is not told twice: %q", again)
	}
}

// The page (/v1/status) makes the same distinction and returns one_answer / not_separable in the state of each question key (not ready from the count alone).
func TestStatusAPIShowsWhyNoLineWasDrawn(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "wiki-place.yaml"))
	st := store.Store{Dir: root}
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN; i++ {
		labels = append(labels, store.Label{TraceID: fmt.Sprintf("p%03d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: fmt.Sprintf("p%03d", i), Truth: "no", Weight: 0.5, Source: "outcome", At: time.Now()})
	}
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	state := func() string {
		rec := httptest.NewRecorder()
		newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var body struct {
			Specs []struct {
				ID   string           `json:"id"`
				Keys []map[string]any `json:"keys"`
			} `json:"specs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, s := range body.Specs {
			if s.ID == "wiki-place" && len(s.Keys) == 1 {
				return s.Keys[0]["state"].(string)
			}
		}
		return ""
	}
	if got := state(); got != "one_answer" {
		t.Fatalf("one_answer when there is only one answer: %q", got)
	}
	st.AppendLabels([]store.Label{{TraceID: "y1", SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: "y1", Truth: "yes", Weight: 1, Source: "outcome", At: time.Now()}})
	guard := filepath.Join(root, "calibrating-wiki-place")
	os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m1 %d %s\n", sp.Eval.MinN+1, sp.Hash())), 0o600)
	old := time.Now().Add(-2 * hookCalibrateGap)
	os.Chtimes(guard, old, old)
	if got := state(); got != "not_separable" {
		t.Fatalf("not_separable when tried and no line was drawn: %q", got)
	}
}

// 0.9.3 (audit 0.9.2 ui): the line notices are read by the person outside the page, so they name each question by its title (Japanese: display.ja.title,
// English: display.en.title), never by the spec id, and the Japanese text uses the page's plain words (none of the inner words of the glossary, no act, no raw answer ids such as no=10).
func TestLineNoticesUseTitlesAndPlainWords(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"language":"japanese"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	load := func(id string) *spec.Spec { return mustLoad(t, filepath.Join(root, "decisions", id+".yaml")) }
	add := func(id, m, truth string, n int) {
		sp := load(id)
		var labels []store.Label
		for i := 0; i < n; i++ {
			tr := fmt.Sprintf("%s-%s-%s-%03d", id, m, truth, i)
			labels = append(labels, store.Label{TraceID: tr, SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: m, InputsHash: tr, Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{"changes-in-scope", "needs-plan", "wiki-place", "wiki-conflict"}
	inner := []string{"判定", "線", "抜き取り", "抜き打ち", "act", "="}
	check := func(stage, all string, want int) {
		t.Helper()
		var n int
		for _, l := range strings.Split(all, "\n") {
			if l == "" {
				continue
			}
			n++
			ja := auditJapanese.MatchString(l)
			for _, id := range append(append([]string{}, hookSpecs...), wikiSpecs...) {
				if strings.Contains(l, id) {
					t.Fatalf("%s: the notice holds the spec id %q: %q", stage, id, l)
				}
			}
			titled := false
			for _, id := range ids {
				d := load(id).Display
				if (ja && strings.Contains(l, "「"+d["ja"].Title+"」")) || (!ja && strings.Contains(l, `"`+d["en"].Title+`"`)) {
					titled = true
				}
			}
			if !titled {
				t.Fatalf("%s: the notice names its question by the title: %q", stage, l)
			}
			if ja {
				for _, w := range inner {
					if strings.Contains(l, w) {
						t.Fatalf("%s: the Japanese notice holds the inner word %q: %q", stage, w, l)
					}
				}
			} else {
				// the English notice too names answers by their names on the page and does not use the inner word act (0.9.3 audit 2, low 3)
				for _, w := range []string{"(act)", "="} {
					if strings.Contains(l, w) {
						t.Fatalf("%s: the English notice holds the inner %q: %q", stage, w, l)
					}
				}
			}
		}
		if n != want {
			t.Fatalf("%s: %d lines (English and Japanese for each question), want %d: %q", stage, n, want, all)
		}
	}
	for _, id := range ids[:3] {
		add(id, "m1", "no", load(id).Eval.MinN)
	}
	check("one answer", lineSummary(root), 6)
	for _, id := range ids[:3] {
		sp := load(id)
		sp.Calibration = []spec.Calibration{{Model: "m1", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN}}
		if err := sp.Save(); err != nil {
			t.Fatal(err)
		}
	}
	check("drawn", lineSummary(root), 6)
	cs := load("wiki-conflict")
	add("wiki-conflict", "m3", "yes", cs.Eval.MinN)
	add("wiki-conflict", "m3", "no", cs.Eval.MinN)
	guard := filepath.Join(root, "calibrating-wiki-conflict")
	os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m3 %d %s\n", 2*cs.Eval.MinN, cs.Hash())), 0o600)
	old := time.Now().Add(-2 * hookCalibrateGap)
	os.Chtimes(guard, old, old)
	check("not separable", lineSummary(root), 2)
}

// audit 0.8.3 M4, a test gap: the notice changes with the question (wiki questions are never asked of people), it is told again when the model changes,
// and the Stop hook really sends it to the person as a one-liner (systemMessage).
func TestLineNoticeWordingModelChangeAndStopWiring(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	add := func(specID, m, truth string, n int) {
		sp := mustLoad(t, filepath.Join(root, "decisions", specID+".yaml"))
		var labels []store.Label
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("%s-%s-%s-%03d", specID, m, truth, i)
			labels = append(labels, store.Label{TraceID: id, SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: m, InputsHash: id, Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	place := mustLoad(t, filepath.Join(root, "decisions", "wiki-place.yaml"))
	add("wiki-place", "m1", "no", place.Eval.MinN)
	// Stop hook wiring: it goes out as a one-liner to the person
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": repo, "prompt": "README を直して"})
	out := runHook(t, "stop", map[string]any{"session_id": "w", "cwd": repo, "last_assistant_message": "直した"})
	if !strings.Contains(out, "systemMessage") || !strings.Contains(out, "Where a wiki lesson goes") || !strings.Contains(out, "only one answer") {
		t.Fatalf("the Stop hook tells the person the line state: %s", out)
	}
	if strings.Contains(out, "keeps asking") || !strings.Contains(out, "nothing is asked of you") {
		t.Fatalf("does not say \"keeps asking\" for a question never asked of people: %s", out)
	}
	if again := lineSummary(root); strings.Contains(again, "Where a wiki lesson goes") {
		t.Fatalf("the same state is not told twice: %q", again)
	}
	// When the model in use changes to m2 (more truths than m1), tell again
	add("wiki-place", "m2", "no", place.Eval.MinN+5)
	if line := lineSummary(root); !strings.Contains(line, "Where a wiki lesson goes") {
		t.Fatalf("when the model changes, tell again: %q", line)
	}
	// A hook question whose truth comes from results (changes-in-scope) does not ask people even with one answer (re-audit: only needs-plan asks people)
	add("changes-in-scope", "m1", "yes", mustLoad(t, filepath.Join(root, "decisions", "changes-in-scope.yaml")).Eval.MinN)
	if line := lineSummary(root); !strings.Contains(line, "Were the changed files all needed?") || !strings.Contains(line, "waits for results") || strings.Contains(line, "keeps asking") {
		t.Fatalf("a question whose truth comes from results says it waits for results and does not say it keeps asking: %q", line)
	}
	// A question asked of people (needs-plan) says it keeps asking
	add("needs-plan", "m1", "no", mustLoad(t, filepath.Join(root, "decisions", "needs-plan.yaml")).Eval.MinN)
	if line := lineSummary(root); !strings.Contains(line, "Should it have asked you first?") || !strings.Contains(line, "keeps asking") {
		t.Fatalf("a question people answer says it keeps asking: %q", line)
	}
	// The text when a line is drawn also changes with the question: wiki questions do not ask people and are never replaced silently
	sp := mustLoad(t, filepath.Join(root, "decisions", "wiki-place.yaml"))
	sp.Calibration = []spec.Calibration{{Model: "m2", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if line := lineSummary(root); !strings.Contains(line, `a line was drawn for "Where a wiki lesson goes"`) || !strings.Contains(line, "never replaced silently") || strings.Contains(line, "the rest still ask") {
		t.Fatalf("when a line is drawn for a wiki question, it says people are not asked: %q", line)
	}
	// When the answers cannot be separated: hook questions ask for other wordings; wiki questions are only tried again as truths grow
	for _, id := range []string{"changes-in-scope", "wiki-conflict"} {
		cs := mustLoad(t, filepath.Join(root, "decisions", id+".yaml"))
		add(id, "m3", "yes", cs.Eval.MinN)
		add(id, "m3", "no", cs.Eval.MinN)
		guard := filepath.Join(root, "calibrating-"+id)
		os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m3 %d %s\n", 2*cs.Eval.MinN, cs.Hash())), 0o600)
		old := time.Now().Add(-2 * hookCalibrateGap)
		os.Chtimes(guard, old, old)
	}
	line := lineSummary(root)
	var scope, conflict string
	for _, l := range strings.Split(line, "\n") {
		if strings.Contains(l, "Were the changed files all needed?") {
			scope = l
		}
		if strings.Contains(l, "Wiki lessons that disagree") {
			conflict = l
		}
	}
	if !strings.Contains(scope, "other wordings") || strings.Contains(conflict, "other wordings") || !strings.Contains(conflict, "tried again when") {
		t.Fatalf("when the answers cannot be separated, only hook questions ask for other wordings: %q / %q", scope, conflict)
	}
}

// audit 0.8.3 L2: choice can draw a line from the raw-probability check even with one kind of truth, so it does not say "only one answer".
func TestChoiceWithOneAnswerIsNotOneAnswer(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := filepath.Join(agent, "decisions", "prompt-kind.yaml")
	sp := mustLoad(t, path)
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN; i++ {
		id := fmt.Sprintf("pk%03d", i)
		labels = append(labels, store.Label{TraceID: id, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Model: "m1", InputsHash: id, Truth: "edit", Weight: 1, Source: "outcome", At: time.Now()})
	}
	if err := (store.Store{Dir: agent}).AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	if strings.Contains(out, "only one answer") || !strings.Contains(out, "enough truths: the line is drawn automatically") {
		t.Fatalf("one kind of truth for choice says the line is being drawn automatically:\n%s", out)
	}
}

// re-audit, low: the line state counts the latest one per input (re-labels of the same input are not counted) and only truths from people and results (machine truths are not counted),
// both the same as automatic calibration. They are checked separately so the test fails if either one is dropped.
func TestLineStateCountsLatestPerInputAndHumanSourcesSeparately(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	sp := mustLoad(t, filepath.Join(agent, "decisions", "changes-in-scope.yaml"))
	mk := func(n int, src string, sameInput bool) []store.Label {
		var labels []store.Label
		for i := 0; i < n; i++ {
			in := fmt.Sprintf("in%03d", i)
			if sameInput {
				in = fmt.Sprintf("in%d", i%3)
			}
			labels = append(labels, store.Label{TraceID: fmt.Sprintf("%s%03d", src, i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: in, Truth: []string{"yes", "no"}[i%2], Weight: 1, Source: src, At: time.Now().Add(time.Duration(i) * time.Second)})
		}
		return labels
	}
	if s := lineStateOf(sp, sp.Eval.MinN, "m1", sp.Hash(), mk(sp.Eval.MinN, "outcome", true), false); s.Kind != lineCollecting || s.N != 3 {
		t.Fatalf("re-labels of the same input count as one: %+v", s)
	}
	if s := lineStateOf(sp, sp.Eval.MinN, "m1", sp.Hash(), mk(sp.Eval.MinN, "context", false), false); s.Kind != lineCollecting || s.N != 0 {
		t.Fatalf("machine truths (context) are not counted: %+v", s)
	}
}

// re-re-audit test gap: the text when a line is drawn for a question whose truth comes from results (changes-in-scope), and the Japanese text. With the Japanese setting, Japanese
// follows the English, and a question never asked of people does not mix in "聞きます", or "人に聞くことはありません" with the spot-check note.
func TestLineNoticeOutcomeCalibratedAndJapanese(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"language":"japanese"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	add := func(specID, truth string, n int) {
		sp := mustLoad(t, filepath.Join(root, "decisions", specID+".yaml"))
		var labels []store.Label
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("%s-%s-%03d", specID, truth, i)
			labels = append(labels, store.Label{TraceID: id, SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: "m1", InputsHash: id, Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	calibrate := func(id string) {
		sp := mustLoad(t, filepath.Join(root, "decisions", id+".yaml"))
		sp.Calibration = []spec.Calibration{{Model: "m1", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN}}
		if err := sp.Save(); err != nil {
			t.Fatal(err)
		}
	}
	pick := func(all, id string) string { // the English and the Japanese line of the question, found by its titles (the notices do not show the id)
		d := mustLoad(t, filepath.Join(root, "decisions", id+".yaml")).Display
		var out []string
		for _, l := range strings.Split(all, "\n") {
			if strings.Contains(l, `"`+d["en"].Title+`"`) || strings.Contains(l, "「"+d["ja"].Title+"」") {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n")
	}
	for _, id := range []string{"changes-in-scope", "wiki-place", "needs-plan"} {
		sp := mustLoad(t, filepath.Join(root, "decisions", id+".yaml"))
		add(id, "yes", sp.Eval.MinN)
	}
	one := lineSummary(root)
	if l := pick(one, "changes-in-scope"); !strings.Contains(l, "答えが片方だけ") || !strings.Contains(l, "結果から付くのを待ちます") || strings.Contains(l, "今までどおり聞きます") {
		t.Fatalf("a results question says it waits for results in Japanese too: %q", l)
	}
	if l := pick(one, "needs-plan"); !strings.Contains(l, "今までどおり聞きます") {
		t.Fatalf("a question people answer says it keeps asking in Japanese too: %q", l)
	}
	for _, id := range []string{"changes-in-scope", "wiki-place", "needs-plan"} {
		calibrate(id)
	}
	drawn := lineSummary(root)
	scope := pick(drawn, "changes-in-scope")
	if !strings.Contains(scope, `a line was drawn for "Were the changed files all needed?"`) || !strings.Contains(scope, "wait for results to supply the truth") || strings.Contains(scope, "the rest still ask") ||
		!strings.Contains(scope, "境目が決まりました") || !strings.Contains(scope, "迷っても人に聞かず、結果から正解が付くのを待ちます") || strings.Contains(scope, "今までどおり聞きます") {
		t.Fatalf("when a line is drawn for a results question, it says the rest wait for results too instead of asking people (English and Japanese): %q", scope)
	}
	if l := pick(drawn, "wiki-place"); !strings.Contains(l, "人に聞くことはなく") || strings.Contains(l, "確かめのため画面に出ます") {
		t.Fatalf("a wiki question does not ask people and does not mention spot checks: %q", l)
	}
	if l := pick(drawn, "needs-plan"); !strings.Contains(l, "それ以外は今までどおり聞きます") || !strings.Contains(l, "確かめのため画面に出ます") {
		t.Fatalf("a question people answer says the rest are asked and spot checks appear: %q", l)
	}
}

// jaCounts uses the page's answer names
func TestAuditProbeJaCounts(t *testing.T) {
	sp := &spec.Spec{Display: map[string]spec.Display{"ja": {Options: map[string]string{"edit": "変更"}}}}
	if got := jaCounts(sp, map[string]int{"edit": 2}); got != "変更 2 件" {
		t.Fatalf("display.ja.options name: %q", got)
	}
	if got := jaCounts(&spec.Spec{}, map[string]int{"no": 10, "yes": 1}); got != "いいえ 10 件・はい 1 件" {
		t.Fatalf("yes/no in plain Japanese: %q", got)
	}
}

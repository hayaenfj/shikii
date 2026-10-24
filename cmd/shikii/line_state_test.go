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

// 0.8.3: 正解が eval.min_n に届いた後の状態を、件数だけで「ready to calibrate」と言わず、片方の答えしか無い・分けられない・
// 線が引けた、に分けて status に出し、本人にもターンの終わりに 1 回だけ知らせる(状態が変わったらまた 1 回)。
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
		t.Fatalf("片方の答えしか無い時は、件数が足りても線を引けないと出す:\n%s", out)
	}
	first := lineSummary(agent)
	if !strings.Contains(first, "changes-in-scope") || !strings.Contains(first, "only one answer") {
		t.Fatalf("片方の答えしか無いことを本人に知らせる: %q", first)
	}
	if again := lineSummary(agent); again != "" {
		t.Fatalf("同じ状態は二度知らせない: %q", again)
	}

	add("yes", 5)
	if out := status(); !strings.Contains(out, "enough truths: the line is drawn automatically") {
		t.Fatalf("両方の答えがあれば、自動で線を引く途中と出す:\n%s", out)
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
		t.Fatalf("試して引けなかった時は、分けられないと出す:\n%s", out)
	}
	if line := lineSummary(agent); !strings.Contains(line, "could not be separated") {
		t.Fatalf("分けられないことを本人に知らせる(状態が変わったのでもう 1 回): %q", line)
	}

	sp.Calibration = []spec.Calibration{{Model: "m1", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN + 5}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if out := status(); !strings.Contains(out, "calibrated: platt") {
		t.Fatalf("線が引けた: \n%s", out)
	}
	drawn := lineSummary(agent)
	if !strings.Contains(drawn, "a line was drawn") || !strings.Contains(drawn, "act") {
		t.Fatalf("線が引けたことと、これから act で進むことを本人に知らせる: %q", drawn)
	}
	if again := lineSummary(agent); again != "" {
		t.Fatalf("引けた線は二度知らせない: %q", again)
	}
}

// 画面(/v1/status)も同じ区別で、問いの鍵ごとの state に one_answer / not_separable を返す(件数だけで ready にしない)。
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
		t.Fatalf("片方の答えしか無い時は one_answer: %q", got)
	}
	st.AppendLabels([]store.Label{{TraceID: "y1", SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: "y1", Truth: "yes", Weight: 1, Source: "outcome", At: time.Now()}})
	guard := filepath.Join(root, "calibrating-wiki-place")
	os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m1 %d %s\n", sp.Eval.MinN+1, sp.Hash())), 0o600)
	old := time.Now().Add(-2 * hookCalibrateGap)
	os.Chtimes(guard, old, old)
	if got := state(); got != "not_separable" {
		t.Fatalf("試して引けなかった時は not_separable: %q", got)
	}
}

// 監査 0.8.3 の M4・テストの穴: 知らせの文は問いで変わる(wiki の問いは人に聞かない)、モデルが変わればもう一度知らせる、
// Stop の hook から実際に本人への一言(systemMessage)として出る。
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
	// Stop の hook の配線: 本人への一言として出る
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": repo, "prompt": "README を直して"})
	out := runHook(t, "stop", map[string]any{"session_id": "w", "cwd": repo, "last_assistant_message": "直した"})
	if !strings.Contains(out, "systemMessage") || !strings.Contains(out, "wiki-place") || !strings.Contains(out, "only one answer") {
		t.Fatalf("Stop の hook が線の状態を本人に知らせる: %s", out)
	}
	if strings.Contains(out, "keeps asking") || !strings.Contains(out, "nothing is asked of you") {
		t.Fatalf("人に聞かない問いに「聞き続ける」と言わない: %s", out)
	}
	if again := lineSummary(root); strings.Contains(again, "wiki-place") {
		t.Fatalf("同じ状態は二度知らせない: %q", again)
	}
	// 今使うモデルが m2 に変わった(正解が m1 より多い)ら、もう一度知らせる
	add("wiki-place", "m2", "no", place.Eval.MinN+5)
	if line := lineSummary(root); !strings.Contains(line, "wiki-place") {
		t.Fatalf("モデルが変われば、もう一度知らせる: %q", line)
	}
	// 結果から正解が付く hook の問い(changes-in-scope)は、片方だけでも人に聞かない(再監査: 人に聞くのは needs-plan だけ)
	add("changes-in-scope", "m1", "yes", mustLoad(t, filepath.Join(root, "decisions", "changes-in-scope.yaml")).Eval.MinN)
	if line := lineSummary(root); !strings.Contains(line, "changes-in-scope") || !strings.Contains(line, "waits for results") || strings.Contains(line, "keeps asking") {
		t.Fatalf("結果から正解が付く問いは、結果を待つと言い、聞き続けるとは言わない: %q", line)
	}
	// 人に聞く問い(needs-plan)は、聞き続けると言う
	add("needs-plan", "m1", "no", mustLoad(t, filepath.Join(root, "decisions", "needs-plan.yaml")).Eval.MinN)
	if line := lineSummary(root); !strings.Contains(line, "needs-plan") || !strings.Contains(line, "keeps asking") {
		t.Fatalf("人が答える問いは、聞き続けると言う: %q", line)
	}
	// 線が引けた時の文も問いで変わる: wiki の問いは人に聞かず、黙って置き換えない
	sp := mustLoad(t, filepath.Join(root, "decisions", "wiki-place.yaml"))
	sp.Calibration = []spec.Calibration{{Model: "m2", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if line := lineSummary(root); !strings.Contains(line, "a line was drawn for wiki-place") || !strings.Contains(line, "never replaced silently") || strings.Contains(line, "the rest still ask") {
		t.Fatalf("wiki の問いで線が引けた時は、人に聞かないと言う: %q", line)
	}
	// 分けられない時: hook の問いは別の聞き方を頼む、wiki の問いは正解が増えたら試すだけ
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
		if strings.Contains(l, "changes-in-scope") {
			scope = l
		}
		if strings.Contains(l, "wiki-conflict") {
			conflict = l
		}
	}
	if !strings.Contains(scope, "other wordings") || strings.Contains(conflict, "other wordings") || !strings.Contains(conflict, "tried again when") {
		t.Fatalf("分けられない時、hook の問いだけ別の聞き方を頼む: %q / %q", scope, conflict)
	}
}

// 監査 0.8.3 の L2: choice は正解が 1 種類でも生の確率の確かめで線を引けるので、「答えが片方だけ」とは言わない。
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
		t.Fatalf("choice の 1 種類の正解は、自動で引く途中と出す:\n%s", out)
	}
}

// 再監査の低: 線の状態の数え方は、入力ごとに最新の 1 件(同じ入力の付け直しを数えない)と、人と結果の正解だけ(機械の正解を数えない)の
// どちらも自動の較正と同じ。片方だけ外しても落ちるよう、別々に確かめる。
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
		t.Fatalf("同じ入力の付け直しは 1 件に数える: %+v", s)
	}
	if s := lineStateOf(sp, sp.Eval.MinN, "m1", sp.Hash(), mk(sp.Eval.MinN, "context", false), false); s.Kind != lineCollecting || s.N != 0 {
		t.Fatalf("機械の正解(context)は数えない: %+v", s)
	}
}

// 再々監査のテストの穴: 結果から正解が付く問い(changes-in-scope)で線が引けた時の文と、日本語の文。日本語の設定では英語の後に
// 日本語が続き、人に聞かない問いで「聞きます」「人に聞くことはありません」と抜き取りの話を混ぜない。
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
	pick := func(all, id string) string {
		var out []string
		for _, l := range strings.Split(all, "\n") {
			if strings.Contains(l, id) {
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
		t.Fatalf("結果の問いは日本語でも結果を待つと言う: %q", l)
	}
	if l := pick(one, "needs-plan"); !strings.Contains(l, "今までどおり聞きます") {
		t.Fatalf("人が答える問いは日本語でも聞き続けると言う: %q", l)
	}
	for _, id := range []string{"changes-in-scope", "wiki-place", "needs-plan"} {
		calibrate(id)
	}
	drawn := lineSummary(root)
	scope := pick(drawn, "changes-in-scope")
	if !strings.Contains(scope, "a line was drawn for changes-in-scope") || !strings.Contains(scope, "wait for results to supply the truth") || strings.Contains(scope, "the rest still ask") ||
		!strings.Contains(scope, "線が引けました") || !strings.Contains(scope, "迷っても人に聞かず、結果から正解が付くのを待ちます") || strings.Contains(scope, "今までどおり聞きます") {
		t.Fatalf("結果の問いで線が引けた時は、残りも人に聞かず結果を待つと言う(英語と日本語): %q", scope)
	}
	if l := pick(drawn, "wiki-place"); !strings.Contains(l, "人に聞くことはなく") || strings.Contains(l, "抜き取り") {
		t.Fatalf("wiki の問いは人に聞かず、抜き取りの話をしない: %q", l)
	}
	if l := pick(drawn, "needs-plan"); !strings.Contains(l, "それ以外は今までどおり聞きます") || !strings.Contains(l, "抜き取り") {
		t.Fatalf("人が答える問いは、残りを聞き、抜き取りが出ると言う: %q", l)
	}
}

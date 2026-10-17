package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// 2 回目の監査(tune / propose / 採用)で見つかった穴。どれも直す前に落ちるのを確かめてある。

// 提案役に渡す「外した実例」に tainted な本文は、提案役が手元(loopback)でない限り入らない。
func TestProposerPromptDropsTaintedExamplesUnlessLocal(t *testing.T) {
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	bank := []bankRow{
		{inputs: map[string]string{"message": "why PUBLIC-ROW"}, truth: "question", weight: 1},
		{inputs: map[string]string{"message": "why SECRET-PROD-ROW"}, truth: "question", weight: 1, tainted: true},
	}
	cur := candScore{name: "current", correct: []bool{false, false}, weights: []float64{1, 1}, confusion: map[string]map[string]float64{"question": {"change_request": 2}}}
	_, remote := buildProposerPrompt(sp, cur, bank, "", 2, false)
	if strings.Contains(remote, "SECRET-PROD-ROW") || !strings.Contains(remote, "PUBLIC-ROW") {
		t.Fatalf("a remote proposer must see the public example but never the tainted one:\n%s", remote)
	}
	_, local := buildProposerPrompt(sp, cur, bank, "", 2, true)
	if !strings.Contains(local, "SECRET-PROD-ROW") {
		t.Fatalf("a local proposer may see tainted examples:\n%s", local)
	}
}

// hosted の鍵(TypeSafe / OpenRouter)は提案役には送らない。提案役の鍵は SHIKII_PROPOSER_KEY だけ。
func TestProposerNeverGetsTheHostedKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "sk-typesafe-SECRET")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_PROPOSER_KEY", "")
	if a := proposerAuth("https://api.example.invalid/v1"); a != "" {
		t.Fatalf("the hosted key must not be sent to a proposer, got %q", a)
	}
	t.Setenv("SHIKII_PROPOSER_KEY", "pk-proposer")
	if a := proposerAuth("https://api.example.invalid/v1"); a != "pk-proposer" {
		t.Fatalf("SHIKII_PROPOSER_KEY is the proposer's key, got %q", a)
	}
}

// 手元の形はどれも loopback。遠いものは違う。
func TestIsLoopbackForms(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8087", "http://localhost:8087", "http://localhost.:8087", "http://[::1]:8087", "http://127.0.0.2:1", "http://0.0.0.0:8087", "http://[::ffff:127.0.0.1]:1", "localhost:8087", "127.0.0.1:8087/v1"} {
		if !isLoopback(u) {
			t.Fatalf("%s must count as local", u)
		}
	}
	for _, u := range []string{"https://api.example.invalid/v1", "http://10.0.0.1:8087", "http://127.0.0.1.evil.example", "http://localhost.evil.example"} {
		if isLoopback(u) {
			t.Fatalf("%s must not count as local", u)
		}
	}
}

// 偽モデル: 質問に strict があれば半分当てる(自信 0.9 で外す所もある)。無ければ全部外す。較正が精度を満たせない状況を作る。
func confidentlyWrongModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		state := req.State
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			strict := strings.Contains(q.Instructions, "strict")
			why := strings.Contains(state, "why")
			st := strings.TrimSpace(state)
			odd := strings.HasSuffix(st, "b") || strings.HasSuffix(st, "d") || strings.HasSuffix(st, "f")
			right := map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
			if why {
				right = map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			}
			wrong := map[string]float64{"change_request": right["question"], "question": right["change_request"], "other": 0}
			probs := wrong // strict でなければ全部外す
			if strict && !odd {
				probs = right // strict なら a c e の 6 件を当て、b d f の 6 件は自信を持って外す(精度 0.5 で較正が拒む)
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 採用と較正は一体: 較正が精度不足で書けなければ、新しい版は残らず、run も「採用済み」にならず、やり直せる。
func TestAdoptionIsUndoneWhenCalibrationRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join(dir, "decisions")
	specWithVariants(t, dir)
	labeledPath := filepath.Join(dir, "rows.json")
	rows := bankRows()
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(specsDir, "intent.yaml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "eval:\n  min_n: 2\n", "eval:\n  min_n: 2\n  labeled: "+labeledPath+"\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	model := confidentlyWrongModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef})
	s := httptest.NewServer(h)
	defer s.Close()
	code, res := postJSON(t, s.URL+"/v1/tune/sync", map[string]any{"spec": "intent"})
	if code != 200 || res["adoptable"] != true || res["winner"] != "strict" {
		t.Fatalf("strict must win on top-1 and be adoptable: %d %v", code, res)
	}
	code, out := postJSON(t, s.URL+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": res["run"], "meaning_same": true})
	if code == 200 || out["adopted"] == true {
		t.Fatalf("adoption must fail when calibration refuses: %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(specsDir, "intent-v2.yaml")); err == nil {
		t.Fatal("an uncalibrated v2 must not be left behind")
	}
	var st map[string]any
	getJSON(t, s.URL+"/v1/tune?spec=intent", &st)
	if st["latest"].(map[string]any)["adopted"] == true {
		t.Fatalf("the run must not say adopted: %v", st)
	}
	// serve は v1 のまま
	sps, err := loadSpecs(specsDir)
	if err != nil || len(sps) != 1 || sps["intent"] == nil || sps["intent"].Version != 1 {
		t.Fatalf("serve must keep using v1: %v %+v", err, sps)
	}
}

// 帰無仮説: 全部の候補が同じ実力なら、候補をいくら足しても採用はまれ(片側の符号検定 p < 0.05)。
// 候補ごとに独立な乱数で 0.7 の確率で当てる偽モデル。20 候補 × 30 回で採用率を測る。
func TestManyEqualCandidatesAreRarelyAdopted(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var seed int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			h := fnvHash(fmt.Sprintf("%d|%s|%s", seed, q.Instructions, req.State))
			truthIsQ := strings.Contains(req.State, "why")
			right := h%10 < 7
			probs := map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
			if truthIsQ == right {
				probs = map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	if err := os.MkdirAll("decisions", 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	b, _ := os.ReadFile(specPath)
	variants := "variants:\n"
	for i := 0; i < 20; i++ {
		variants += fmt.Sprintf("  - name: c%d\n    question: What is asked? variant %d\n", i, i)
	}
	if err := os.WriteFile(specPath, []byte(string(b)+variants), 0o644); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for i := 0; i < 20; i++ {
		rows = append(rows, fmt.Sprintf(`{"inputs":{"message":"fix %d"},"truth":"change_request"}`, i), fmt.Sprintf(`{"inputs":{"message":"why %d"},"truth":"question"}`, i))
	}
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte("["+strings.Join(rows, ",")+"]"), 0o644); err != nil {
		t.Fatal(err)
	}
	adopted := 0
	const runs = 30
	for seed = 1; seed <= runs; seed++ {
		out := capture(t, func() error {
			return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
		})
		if strings.Contains(out, "dry run: pass --write") {
			adopted++
		}
	}
	if adopted > runs*15/100 {
		t.Fatalf("with 20 candidates of equal skill, %d/%d runs would adopt a wording: the holdout gate is not holding (want ≤ 5%% nominal)", adopted, runs)
	}
}

func fnvHash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// 基本構成: 実行する場所に decisions/ wiki/ .shikii/。wiki は常に ./wiki で、spec の置き場からは推測しない(親フォルダに書かない)。
func TestWikiLivesAtTheProjectRootNotNextToTheSpec(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "myproject")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, project) // myproject/decisions/intent.yaml
	flat := filepath.Join(project, "flat.yaml")
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(flat, b, 0o644); err != nil {
		t.Fatal(err)
	}
	rows := bankRows()
	labeledPath := filepath.Join(project, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error {
		return cmdTune([]string{flat, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(project, ".shikii")})
	})
	if _, err := os.Stat(filepath.Join(root, "wiki")); err == nil {
		t.Fatal("tune must never write a wiki above the project")
	}
	if runs, _ := filepath.Glob(filepath.Join(project, "wiki", "runs", "*.md")); len(runs) != 1 {
		t.Fatalf("the run must land in ./wiki of the project: %v", runs)
	}
}

// 端末の --propose N --write は、AI が作った文言を人が見ないまま採用しない。候補は variants に残り、見てから tune --write で採用できる。
func TestProposeWithWriteDoesNotAdoptUnseenWording(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	model := wordingSensitiveModel(t)
	proposer, _ := fakeProposer(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	rows := bankRows()
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir, "--propose", "2", "--proposer", proposer.URL, "--write"})
	})
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatalf("a proposed wording must not be adopted unseen:\n%s", out)
	}
	if !strings.Contains(out, "winner: proposed-strict") || !strings.Contains(out, "not adopted") {
		t.Fatalf("the person must be told the winner and that it waits for their check:\n%s", out)
	}
	// 候補は残っているので、見てから採用できる
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir, "--write"})
	})
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err != nil {
		t.Fatal("after a person has seen the variants, tune --write adopts the winner")
	}
	res, err := latestTuneResult("wiki", "intent")
	if err != nil || len(res.MeaningChecks) != 1 || res.MeaningChecks[0].By != "terminal --write" {
		t.Fatalf("the terminal adoption must be recorded as the person's meaning check: %v %+v", err, res)
	}
}

// 貯金の重複解消は calibrate と同じ「入力ごとに最新の正解」。後から付けた訂正がファイルの行に勝つ。
// 古い正解(今の spec に無い選択肢)と重み > 1 の行は数えて除く。
func TestTruthBankUsesLatestTruthAndSkipsUnknownAnswers(t *testing.T) {
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	labeledPath := filepath.Join(dir, "rows.json")
	rows := `[{"inputs":{"message":"fix a"},"truth":"change_request"},{"inputs":{"message":"old"},"truth":"removed_option"},{"inputs":{"message":"heavy"},"truth":"question","weight":5}]`
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: filepath.Join(dir, ".shikii")}
	later := store.Label{TraceID: "t1", SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: "m",
		InputsHash: store.HashInputs(map[string]string{"message": "fix a"}), Inputs: map[string]string{"message": "fix a"},
		Raw: map[string]float64{"question": 0.5, "change_request": 0.5}, Truth: "question", Weight: 1, Source: "ui", At: time.Now().Add(time.Hour)}
	if err := st.AppendLabel(later); err != nil {
		t.Fatal(err)
	}
	bank, skipped, err := truthBank(sp, st, labeledPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank) != 2 || skipped != 1 {
		t.Fatalf("want 2 rows (fix a, heavy) and 1 skipped (removed_option), got %d rows, %d skipped: %+v", len(bank), skipped, bank)
	}
	for _, r := range bank {
		if r.inputs["message"] == "fix a" && r.truth != "question" {
			t.Fatalf("the later correction must win: %+v", r)
		}
		if r.weight > 1 {
			t.Fatalf("file weights are capped at 1 like the store: %+v", r)
		}
	}
}

// 採点できない行(必要な入力が無い、送れる先が無い)は tune を止めず、数えて除く。
func TestTuneSkipsRowsItCannotScore(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	rows := bankRows(`{"inputs":{"other":"no message here"},"truth":"question"}`)
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
	})
	if !strings.Contains(out, "winner: strict") || !strings.Contains(out, "1 truth(s) could not be scored") {
		t.Fatalf("one unscorable row must be skipped and counted, not abort the run:\n%s", out)
	}
}

// Save は variants を触っていなければ再エンコードしない(人のコメントが消えない)。
func TestSaveKeepsCommentsInsideVariants(t *testing.T) {
	dir := t.TempDir()
	p := specWithVariants(t, dir)
	b, _ := os.ReadFile(p)
	src := strings.Replace(string(b), "variants:\n", "variants: # keep me\n  # and me\n", 1)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, p)
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), "keep me") || !strings.Contains(string(after), "and me") {
		t.Fatalf("comments inside variants must survive a Save that did not change them:\n%s", after)
	}
}

// 提案役の前置きに [ があっても JSON の配列を見つける。
func TestParseProposalsSkipsBracketsInProse(t *testing.T) {
	props, err := parseProposals("Here are [two] ideas:\n```json\n[{\"name\":\"a\",\"question\":\"Q?\"}]\n```")
	if err != nil || len(props) != 1 || props[0].Name != "a" {
		t.Fatalf("got %v %+v", err, props)
	}
}

// 人の「意味が違う」は spec のページにも残り、次の提案役が読む。
func TestRejectionReachesTheNextProposer(t *testing.T) {
	url, _, wiki, _ := newTuneFixture(t)
	code, res := postJSON(t, url+"/v1/tune/sync", map[string]any{"spec": "intent"})
	if code != 200 {
		t.Fatalf("%d %v", code, res)
	}
	if code, _ := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": res["run"], "meaning_same": false, "note": "it now asks about tone"}); code != 200 {
		t.Fatalf("reject: %d", code)
	}
	sp := mustLoad(t, filepath.Join(filepath.Dir(wiki), "decisions", "intent.yaml"))
	if ctx := wikiContext(wiki, sp, "m"); !strings.Contains(ctx, "it now asks about tone") || !strings.Contains(ctx, "strict") {
		t.Fatalf("the proposer must be told which wording a person rejected and why:\n%s", ctx)
	}
}

// 引数無し / hash --help は使い方を出して失敗しない。使い方には今ある道具と flag が載る。
func TestUsageMentionsCurrentToolsAndFlags(t *testing.T) {
	u := usageText()
	for _, must := range []string{"shikii_tune", "--proposer", "--propose", "shikii tune", "shikii up", "shikii doctor"} {
		if !strings.Contains(u, must) {
			t.Fatalf("usage must mention %q", must)
		}
	}
	if err := cmdHash([]string{"--help"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("hash --help must print usage, got %v", err)
	}
}

var _ = spec.Choice

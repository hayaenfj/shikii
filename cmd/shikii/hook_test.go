package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// hookEnv は仮の HOME に道具を入れ、指定した仕事の .shikii を準備してモデルと画面を差し替える。
func hookEnv(t *testing.T, model *httptest.Server, roots ...string) (agent string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent = filepath.Join(home, ".shikii", "agent")
	oldLocal, oldPage, oldCal, oldTune, oldSpawn := hookLocalURL, hookPageURL, hookCalibrate, hookTune, hookSpawn
	hookCalibrate = runCalibrateInProcess // テストは線を同プロセスで引く(本番は別プロセスを切り離す)
	hookTune = runTuneInProcess
	spawned = nil
	hookSpawn = recordSpawn // 本物の up / serve は起こさない(記録だけ)
	opened = nil
	openBrowser = func(url string) { opened = append(opened, url) }
	pageStartWait = 0                   // 起こした画面を待たない(記録だけなので来ない)
	hookLocalURL = "http://127.0.0.1:1" // 閉じている口(モデルが無い)
	if model != nil {
		hookLocalURL = model.URL
	}
	hookPageURL = "http://127.0.0.1:1"
	t.Cleanup(func() {
		hookLocalURL, hookPageURL, hookCalibrate, hookTune, hookSpawn = oldLocal, oldPage, oldCal, oldTune, oldSpawn
	})
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	spawned = nil // install が起こした分は数えない
	root := t.TempDir()
	if len(roots) > 0 {
		root = roots[0]
	}
	t.Chdir(root)
	agent, _ = initTestProject(t, root)
	return agent
}

func initTestProject(t *testing.T, root string) (string, string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	capture(t, func() error { return cmdInit(nil) })
	t.Chdir(previous)
	store, scope, err := projectStoreAt(root)
	if err != nil {
		t.Fatal(err)
	}
	// 既存プロジェクトは更新しても旧形式を保持する。その回帰検査用の状態を準備する。
	// 新形式の入力と経路はhook_planning_test.goで検証する。
	path := filepath.Join(store, "decisions", "needs-plan.yaml")
	sp := mustLoad(t, path)
	sp.Version = 1
	sp.Question = "A user sent `request` to a coding agent. Before changing anything, should the agent first agree on an approach with the user (because the change is large or ambiguous, several designs are reasonable, it changes existing behaviour, or the user is still thinking aloud), rather than just doing it?\n"
	sp.State = spec.State{Template: "request: {{ request }}", Required: []string{"request"}, Context: "previous assistant turn: {{ previous }}\nrequest: {{ request }}"}
	data, err := yaml.Marshal(sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return store, scope
}

// spawned は hookSpawn の記録(テスト)。opened はブラウザで開いた URL の記録。
var spawned, opened []string

// spawnedUps は起こした中の手元のモデル(up)だけ(画面の serve を除く)。
func spawnedUps() []string {
	var out []string
	for _, s := range spawned {
		if s == "up" || strings.HasPrefix(s, "up ") {
			out = append(out, s)
		}
	}
	return out
}

func recordSpawn(agent, logName string, args ...string) int {
	spawned = append(spawned, strings.Join(args, " "))
	return 0
}

// runHook は stdin の JSON を与えて event を走らせ、stdout を返す。hook は何があっても誤りを返さない。
func runHook(t *testing.T, event string, in map[string]any) string {
	b, _ := json.Marshal(in)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	return strings.TrimSpace(capture(t, func() error { return cmdHook([]string{event}) }))
}

// fakeAgentModel は hook の 4 spec に答える偽物: choice(prompt-kind)は edit 0.9、noul は state の文で決める関数 p(state)。
func fakeAgentModel(t *testing.T, p func(state string) float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State     string `json:"state"`
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		body, _ := readAll(r)
		_ = json.Unmarshal(body, &req)
		ans := map[string]any{}
		for k, q := range req.Questions {
			if q.Type == "noul" {
				ans[k] = map[string]any{"type": "noul", "noul": p(req.State)}
			} else {
				ans[k] = map[string]any{"type": "choice", "choice": "edit", "probabilities": map[string]float64{"edit": 0.9, "explain": 0.04, "investigate": 0.03, "run": 0.02, "decide": 0.01}, "confidence": 0.9}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "kev-4b", "answers": ans})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var b []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil {
			return b, nil
		}
	}
}

// gitRepo は仮の git リポジトリ(無ければ skip)。
func gitRepo(t *testing.T) string {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	return repo
}

// lowerMinN は install が写した spec の min_n を下げる(人が spec を直した、のつもり)。
func lowerMinN(t *testing.T, agent, specID string, n int) string {
	p := filepath.Join(agent, "decisions", specID+".yaml")
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "min_n: 50") {
		t.Fatalf("%s: min_n: 50 not found", p)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "min_n: 50", fmt.Sprintf("min_n: %d", n), 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// 帯 → 振る舞いの表。影(線が無い)では何も出さず、許可の判定(ask / deny)は決して出さない。
func TestHookOutcomeTable(t *testing.T) {
	d := func(band decide.Band, top string, p float64, calibrated bool) *decide.Decision {
		return &decide.Decision{TraceID: "0123456789abcdef", Band: band, Top: top, P: p, IsCalibrated: calibrated}
	}
	cases := []struct {
		name string
		spec string
		d    *decide.Decision
		want func(o hookOut) bool
	}{
		{"shadow: never speaks", "done-as-asked", d(decide.Ask, "yes", 0.6, false), func(o hookOut) bool { return o.empty() }},
		{"shadow: even a confident 'not done' is silent without a line", "done-as-asked", d(decide.Discard, "yes", 0.02, false), func(o hookOut) bool { return o.empty() }},
		{"shadow: a confident needs-plan is silent too", "needs-plan", d(decide.Act, "yes", 0.99, false), func(o hookOut) bool { return o.empty() }},
		{"needs plan: context for the agent", "needs-plan", d(decide.Act, "yes", 0.88, true), func(o hookOut) bool {
			return strings.Contains(o.Context, "EnterPlanMode") && strings.Contains(o.Context, "0.88")
		}},
		{"no plan needed: silent", "needs-plan", d(decide.Discard, "yes", 0.1, true), func(o hookOut) bool { return o.empty() }},
		{"needs plan unsure: one line with the URL", "needs-plan", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.Context == "" && strings.Contains(o.System, "spec=needs-plan&trace=0123456789abcdef")
		}},
		{"changes in scope: silent", "changes-in-scope", d(decide.Act, "yes", 0.9, true), func(o hookOut) bool { return o.empty() }},
		{"changes out of scope: a line for the person, no context", "changes-in-scope", d(decide.Discard, "yes", 0.1, true), func(o hookOut) bool {
			return o.Context == "" && strings.Contains(o.System, "outside what you asked") && strings.Contains(o.System, "0.10")
		}},
		{"changes unsure: 仮採点へ回す", "changes-in-scope", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.empty()
		}},
		{"done: silent", "done-as-asked", d(decide.Act, "yes", 0.9, true), func(o hookOut) bool { return o.empty() }},
		{"not done: a line for the person", "done-as-asked", d(decide.Discard, "yes", 0.2, true), func(o hookOut) bool {
			return o.Context == "" && strings.Contains(o.System, "unfinished") && strings.Contains(o.System, "0.20")
		}},
		{"done unsure: 仮採点へ回す", "done-as-asked", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.empty()
		}},
		{"prompt-kind: record only", "prompt-kind", d(decide.Act, "edit", 0.95, true), func(o hookOut) bool { return o.empty() }},
	}
	for _, c := range cases {
		o := hookOutcome(c.spec, c.d, hookLoc{})
		if !c.want(o) {
			t.Errorf("%s: got %+v", c.name, o)
		}
	}
	// hook の出力に許可の判定は無い(Stop では文脈も出さない)
	if b := (hookOut{Context: "x"}).json("Stop"); b != nil {
		t.Fatalf("Stop must not return context (it would continue the conversation): %s", b)
	}
	if b := string((hookOut{Context: "x", System: "y"}).json("UserPromptSubmit")); !strings.Contains(b, `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(b, `"additionalContext":"x"`) || !strings.Contains(b, `"systemMessage":"y"`) || strings.Contains(b, "permissionDecision") {
		t.Fatalf("UserPromptSubmit output: %s", b)
	}
}

// 影の期間: 依頼の直後とターンの終わりに 4 つを聞いて記録だけし、stdout には何も出さない。モデルが無ければ exit 0・無出力・hook.log に 1 行。
func TestHookShadowRecordsAndStaysSilent(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 }) // noul は確信して「いいえ」(discard: 答え待ちにならない)
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "fix the typo in a.go"}); out != "" {
		t.Fatalf("shadow mode must print nothing, got %q", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": repo, "last_assistant_message": "Fixed the typo."}); out != "" {
		t.Fatalf("shadow mode must print nothing at Stop, got %q", out)
	}
	st := store.Store{Dir: agent}
	for _, id := range hookSpecs {
		recs, err := st.Records(id, 0)
		if err != nil || len(recs) != 1 {
			t.Fatalf("%s: one record expected, got %v %v", id, recs, err)
		}
		if recs[0].Band == "act" {
			t.Fatalf("%s: no act without a line: %+v", id, recs[0])
		}
	}
	cis, _ := st.Records("changes-in-scope", 0)
	if cis[0].Inputs["request"] != "fix the typo in a.go" || cis[0].Inputs["files"] != "a.go\nb.md" || cis[0].Inputs["summary"] != "Fixed the typo." {
		t.Fatalf("changes-in-scope must see the request, the changed files and the closing message: %+v", cis[0].Inputs)
	}
	// 記録には仕事の印(repo のルートの名)が付く。入力ではないので inputs とハッシュには入らない
	for _, id := range hookSpecs {
		recs, _ := st.Records(id, 0)
		if recs[0].Project != filepath.Base(repo) || recs[0].Inputs["project"] != "" {
			t.Fatalf("%s: the record must carry the project mark %q outside the inputs: project=%q inputs=%v", id, filepath.Base(repo), recs[0].Project, recs[0].Inputs)
		}
	}
	// status は仕事ごとの数を出す
	if out := capture(t, func() error { return cmdStatus([]string{"--store", agent}) }); !strings.Contains(out, "by project: "+filepath.Base(repo)+"=1") {
		t.Fatalf("status must count records per project: %s", out)
	}
	daa, _ := st.Records("done-as-asked", 0)
	if daa[0].Inputs["summary"] != "Fixed the typo." || daa[0].Inputs["files"] != "a.go\nb.md" {
		t.Fatalf("done-as-asked inputs: %+v", daa[0].Inputs)
	}
	// 確信した「いいえ」は答え待ちにならない。prompt-kind の edit 0.9 は線が無いので ask だが、結果から正解が付くので人を待たない
	sess := loadSession(agent, "s1")
	if len(sess.Asked) != 0 {
		t.Fatalf("asked: %+v", sess.Asked)
	}

	// モデルが無い: 止めない(無出力)、理由は hook.log
	hookLocalURL = "http://127.0.0.1:1"
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "s2", "cwd": repo, "prompt": "and now the other file"}); out != "" {
		t.Fatalf("a missing model must not produce output, got %q", out)
	}
	logb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
	if !strings.Contains(string(logb), "127.0.0.1:1") {
		t.Fatalf("hook.log must say why: %q", logb)
	}
	// 依頼の控えが無いセッションの Stop は、ターンの終わりの問いを聞かない
	hookLocalURL = model.URL
	runHook(t, "stop", map[string]any{"session_id": "fresh", "cwd": repo, "last_assistant_message": "hello"})
	if recs, _ := st.Records("done-as-asked", 0); len(recs) != 1 {
		t.Fatalf("Stop without a request must not ask done-as-asked: %d records", len(recs))
	}
}

// ターンの終わりの問いは同じ依頼について 1 回だけ。何も変えていなければ changes-in-scope は聞かず done-as-asked だけ。git の外では変わったファイルは「分からない」。
// changedFiles は増えた・変わった・消えたを名前順に、filesInput は 30 件で切る。
func TestHookStopJudgesEachRequestOnce(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	st := store.Store{Dir: agent}
	count := func(id string) int { recs, _ := st.Records(id, 0); return len(recs) }

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "once", "cwd": repo, "prompt": "explain what the Makefile does, do not change anything"})
	runHook(t, "stop", map[string]any{"session_id": "once", "cwd": repo, "last_assistant_message": "It builds."})
	if count("changes-in-scope") != 0 || count("done-as-asked") != 1 {
		t.Fatalf("a turn that changed nothing asks only done-as-asked: changes=%d done=%d", count("changes-in-scope"), count("done-as-asked"))
	}
	recs, _ := st.Records("done-as-asked", 0)
	if recs[0].Inputs["files"] != "(none)" {
		t.Fatalf("files must read (none): %+v", recs[0].Inputs)
	}
	// 同じ依頼で Stop がもう一度来ても(/ 始まりの入力や再開)聞き直さない
	runHook(t, "stop", map[string]any{"session_id": "once", "cwd": repo, "last_assistant_message": "It builds, again."})
	if count("done-as-asked") != 1 {
		t.Fatalf("a second Stop on the same request must not ask again: %d", count("done-as-asked"))
	}
	// 新しい依頼で戻る。変えれば changes-in-scope も
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "once", "cwd": repo, "prompt": "now add a README with the same explanation"})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("builds\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "once", "cwd": repo, "last_assistant_message": "Added README.md."})
	if count("changes-in-scope") != 1 || count("done-as-asked") != 2 {
		t.Fatalf("after a new request both are asked again: changes=%d done=%d", count("changes-in-scope"), count("done-as-asked"))
	}
	recs, _ = st.Records("changes-in-scope", 0)
	if recs[0].Inputs["files"] != "README.md" {
		t.Fatalf("changed files: %+v", recs[0].Inputs)
	}
	// git の外: changes-in-scope は聞かず、done-as-asked の files は「分からない」
	plain := t.TempDir()
	initTestProject(t, plain)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "nogit", "cwd": plain, "prompt": "write notes.txt with three lines about the weather"})
	runHook(t, "stop", map[string]any{"session_id": "nogit", "cwd": plain, "last_assistant_message": "Wrote notes.txt."})
	plainStore := store.Store{Dir: filepath.Join(plain, ".shikii")}
	recs, _ = plainStore.Records("done-as-asked", 0)
	if count("changes-in-scope") != 1 || len(recs) != 1 || !strings.HasPrefix(recs[0].Inputs["files"], "(unknown") {
		t.Fatalf("outside git: changes=%d files=%q", count("changes-in-scope"), recs[0].Inputs["files"])
	}
	if fileExists(sessionFilesPath(plainStore.Dir, "nogit")) {
		t.Fatal("outside git no listing may be kept")
	}

	// changedFiles: 変わった・増えた・消えた(名前順)。同じ中身は入らない。filesInput は 30 件 + 残りの件数
	before := "100644 aaaa 0\tkeep.go\n100644 bbbb 0\tchanged.go\n100644 cccc 0\tgone.go\n"
	after := "100644 aaaa 0\tkeep.go\n100644 b2b2 0\tchanged.go\n100644 dddd 0\tnew/added.md\n"
	if got := strings.Join(changedFiles(before, after), ","); got != "changed.go,gone.go,new/added.md" {
		t.Fatalf("changedFiles: %s", got)
	}
	many := make([]string, 45)
	for i := range many {
		many[i] = fmt.Sprintf("f%02d", i)
	}
	if s := filesInput(many, true); strings.Count(s, "\n") != filesMax || !strings.HasSuffix(s, "... and 15 more") {
		t.Fatalf("filesInput must cap at %d: %q", filesMax, s)
	}
}

// UserPromptSubmit は prompt-kind と needs-plan を聞き、依頼とツリーのハッシュを控える。次の依頼(無ければ Stop)はツリーが変わっていれば edit、
// 変わっていなければ !edit(重み 0.7)を結果の正解として付け、Stop は直前のアシスタントの発言を控える。正解が min_n に届けば Stop が線を引く。
func TestHookPromptKindGetsOutcomeTruthAtStop(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.2 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	pkPath := lowerMinN(t, agent, "prompt-kind", 4)
	traceOf := map[string]string{} // session → prompt-kind の trace
	run := func(session string, change bool) {
		out := runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "fix the typo in README " + session})
		if out != "" {
			t.Fatalf("shadow: %q", out)
		}
		sess := loadSession(agent, session)
		if sess.Request != "fix the typo in README "+session || len(sess.Pending) != 1 || sess.Pending[0].Tree == "" || !fileExists(sessionFilesPath(agent, session)) {
			t.Fatalf("session after prompt: %+v", sess)
		}
		traceOf[session] = sess.Pending[0].Trace
		if change {
			if err := os.WriteFile(filepath.Join(repo, session+".txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done: " + session})
		sess = loadSession(agent, session)
		// 変えたなら edit が付いて片付く。変えていなければ !edit(部分)を付けて次の人の依頼まで持ち越す(裏の作業が後から変えたら edit に置き換えるため)
		wantPending := 0
		if !change {
			wantPending = 1
		}
		if sess.Previous != "Done: "+session || len(sess.Pending) != wantPending || !sess.Judged || (wantPending == 1 && !sess.Pending[0].Settled) {
			t.Fatalf("session after stop: %+v", sess)
		}
	}
	run("same", false)
	if b, _ := os.ReadFile(pkPath); strings.Contains(string(b), "calibration:") {
		t.Fatal("one truth must not draw a line")
	}
	for _, s := range []string{"changed", "changed2", "changed3", "changed4"} {
		run(s, true)
	}
	labels, err := (store.Store{Dir: agent}).LabeledTraces("prompt-kind")
	if err != nil || len(labels) != 5 {
		t.Fatalf("five outcome labels expected: %v %v", labels, err)
	}
	// 正解が min_n(4)に届いたので、Stop が線を引いて spec に書く(serve を通らない正解なので hook が引く)
	if b, _ := os.ReadFile(pkPath); !strings.Contains(string(b), "calibration:") || !strings.Contains(string(b), "model: kev-4b") {
		logb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
		t.Fatalf("Stop must draw the line once truths reach min_n. hook.log:\n%s", logb)
	}
	if logb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log")); !strings.Contains(string(logb), "auto-calibrate prompt-kind (kev-4b)") {
		t.Fatalf("hook.log must say the line was drawn: %s", logb)
	}
	for _, l := range labels {
		if l.Source != "outcome" {
			t.Fatalf("source must be outcome: %+v", l)
		}
	}
	// 変えたセッションは edit(重み 1)、変えなかったセッションは !edit(重み 0.7)
	if l := labels[traceOf["changed"]]; l.Truth != "edit" || l.Weight != 1 {
		t.Fatalf("the session that changed the tree must get edit w=1, got %+v", l)
	}
	if l := labels[traceOf["same"]]; l.Truth != "!edit" || l.Weight != 0.7 {
		t.Fatalf("the session that left the tree alone must get !edit w=0.7, got %+v", l)
	}
	// 2 回目の UserPromptSubmit は previous(文脈)を持って聞く: 落ちないこと。短い返事は依頼を置き換えず前の依頼に添える(ターンの終わりの問いが「ok」を依頼として読まない)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": "and the other one"})
	if strings.Contains(out, "systemMessage") || loadSession(agent, "same").Request != "fix the typo in README same / and the other one" || loadSession(agent, "same").Judged {
		t.Fatalf("second prompt: out=%q sess=%+v", out, loadSession(agent, "same"))
	}
	long := "please also rewrite the README section about installation so that it mentions Docker"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": long})
	if loadSession(agent, "same").Request != long {
		t.Fatalf("a full request replaces the previous one: %+v", loadSession(agent, "same"))
	}
	// 依頼の境目は次の依頼: 1 つ目の依頼で何も変えず、2 つ目の依頼の後で変えた時、1 つ目は !edit、2 つ目は edit(Stop まで待つと両方 edit になってしまう)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "two", "cwd": repo, "prompt": "what does this function do? just explain"})
	first := loadSession(agent, "two").Pending[0].Trace
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "two", "cwd": repo, "prompt": "now fix the bug in the parser please"})
	second := loadSession(agent, "two").Pending[0].Trace
	if err := os.WriteFile(filepath.Join(repo, "two.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "two", "cwd": repo})
	labels, _ = (store.Store{Dir: agent}).LabeledTraces("prompt-kind")
	if labels[first].Truth != "!edit" || labels[second].Truth != "edit" {
		t.Fatalf("first request must be settled at the second request: first=%+v second=%+v", labels[first], labels[second])
	}
	// commit しただけでは「変えた」にならない(ハッシュは作業ツリーの中身で、HEAD や index は入れない)
	before := treeSnapshot(repo)
	if out, err := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
	if treeSnapshot(repo) != before {
		t.Fatal("commit alone must not change the tree hash")
	}
	if err := os.WriteFile(filepath.Join(repo, "two.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if treeSnapshot(repo) == before {
		t.Fatal("changing a file's content must change the tree hash")
	}
	// 空と / 始まりは判定しない
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "x", "cwd": repo, "prompt": "/model"}); out != "" || loadSession(agent, "x").Request != "" {
		t.Fatal("slash commands must be skipped")
	}
	// 長い直前の発言は 1500 バイトに切る(多バイト文字を割らない)
	longJa := strings.Repeat("日本語の長い説明。", 300)
	runHook(t, "stop", map[string]any{"session_id": "same", "cwd": repo, "last_assistant_message": longJa})
	if prev := loadSession(agent, "same").Previous; len(prev) > requestMax || !utf8.ValidString(prev) || len(prev) < requestMax-3 {
		t.Fatalf("previous must be cut to %d bytes on a rune boundary, got %d bytes valid=%v", requestMax, len(prev), utf8.ValidString(prev))
	}
	// stop_hook_active なら何もしない
	if out := runHook(t, "stop", map[string]any{"session_id": "same", "cwd": repo, "stop_hook_active": true, "last_assistant_message": "loop"}); out != "" || loadSession(agent, "same").Previous == "loop" {
		t.Fatal("stop_hook_active must be a no-op")
	}
}

// 線が引けたら Stop が口を出す: 確信して「終えていない」なら端末に 1 行(hookSpecificOutput は無い)。設定の言語が日本語なら併記し、記録に display が付く。英語なら display は無い。
func TestHookCalibratedStopSpeaksInTheConfiguredLanguage(t *testing.T) {
	// 「Done」と言って終えたターンは終えた(0.9)、「half」は終えていない(0.1)、と答える偽物
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "closing message: Done") {
			return 0.9
		}
		return 0.1
	})
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	home, _ := os.UserHomeDir()
	specPath := lowerMinN(t, agent, "done-as-asked", 2)
	settings := filepath.Join(home, ".claude", "settings.json")
	setLang := func(lang string) {
		sb, _ := os.ReadFile(settings)
		var cfg map[string]any
		_ = json.Unmarshal(sb, &cfg)
		cfg["language"] = lang
		out, _ := json.Marshal(cfg)
		if err := os.WriteFile(settings, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	st := store.Store{Dir: agent}
	turn := func(session, prompt, last string) string {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": cwd, "prompt": prompt})
		return runHook(t, "stop", map[string]any{"session_id": session, "cwd": cwd, "last_assistant_message": last})
	}
	// 英語: 影の記録に display は無い
	setLang("English")
	turn("e1", "rename the config loader and update its three callers", "Done: renamed and updated the callers.")
	recs, _ := st.Records("done-as-asked", 0)
	if len(recs) != 1 || recs[0].Display != nil {
		t.Fatalf("English must leave no display: %+v", recs)
	}
	// 日本語: 影でも質問と答えの訳が記録に付く(出した文は無い)
	setLang("Japanese")
	turn("j1", "write the migration for the new users table", "half: I wrote the up migration; the down migration is still missing.")
	turn("j2", "add retries to the HTTP client with a test", "Done: retries added, test included.")
	bySummary := func(prefix string) store.Record {
		recs, _ := st.Records("done-as-asked", 0)
		for _, r := range recs {
			if strings.HasPrefix(r.Inputs["summary"], prefix) {
				return r
			}
		}
		t.Fatalf("no done-as-asked record whose summary starts with %q", prefix)
		return store.Record{}
	}
	if d := bySummary("half: I wrote").Display; d == nil || d.Lang != "ja" || d.Question == "" || d.Top != "終えていない" || d.Message != "" {
		t.Fatalf("shadow record in Japanese must carry question and answer, no message: %+v", d)
	}
	recs, _ = st.Records("done-as-asked", 0)
	for _, r := range recs {
		truth := "no"
		if strings.HasPrefix(r.Inputs["summary"], "Done") {
			truth = "yes"
		}
		capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", r.TraceID, truth}) })
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", agent, "--model", "kev-4b", "--write"})
	})

	// 線が引けた後: 「終えていない」は端末の 1 行(英語 / 日本語の併記)、hookSpecificOutput は無い、記録の display.message にも同じ文
	out := turn("j3", "delete the unused feature flags and their tests", "half: I removed two of the five flags so far.")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	if _, has := got["hookSpecificOutput"]; has {
		t.Fatalf("Stop must not return hookSpecificOutput: %s", out)
	}
	msg := fmt.Sprint(got["systemMessage"])
	if !strings.Contains(msg, "looks unfinished") || !strings.Contains(msg, "終えていないように見える") {
		t.Fatalf("message must carry English and Japanese: %s", out)
	}
	if d := bySummary("half: I removed").Display; d == nil || !strings.HasPrefix(msg, d.Message) {
		t.Fatalf("the record must keep the message that was shown: %+v", d)
	}
	// 終えたターンは黙る
	if out := turn("j4", "bump the Go version in go.mod and CI", "Done: bumped both."); strings.Contains(out, "unfinished") {
		t.Fatalf("a finished turn must not be flagged: %s", out)
	}
	// 英語に戻すと併記は消える
	setLang("English")
	if out := turn("e2", "move the helpers into their own package", "half: moved two helpers, the rest still import the old path."); strings.Contains(out, "見える") || !strings.Contains(out, "unfinished") {
		t.Fatalf("English setting must not add Japanese: %s", out)
	}
	// noul の答えは display が無くても はい / いいえ。Top は常に yes で P が p(yes) なので、p<0.5 は「いいえ」
	if l := localize(&spec.Spec{Type: spec.Noul}, &decide.Decision{Top: "yes", P: 0.8}, "ja"); l.Top != "はい" {
		t.Fatalf("noul p=0.8 must read はい in Japanese, got %+v", l)
	}
	if l := localize(&spec.Spec{Type: spec.Noul}, &decide.Decision{Top: "yes", P: 0.1}, "ja"); l.Top != "いいえ" {
		t.Fatalf("noul p=0.1 must read いいえ in Japanese, got %+v", l)
	}
}

// Stop の要約: 答え待ちがあれば 1 行(画面が立っていれば URL、無ければ起動コマンド)、無ければ無出力。答えが付けば数から外れる。同じ数を毎ターン繰り返さない。
func TestHookStopSummarizesWhatWaitsForAPerson(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.6 }) // noul は迷い(ask): 全部答え待ち
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd}); out != "" {
		t.Fatalf("nothing waits yet: %q", out)
	}
	// 人の確認は needs-plan の 1 件。prompt-kind は結果から正解が付くので迷っても人を待たない。Stop の完了判定は AI の仮採点と結果を待つ。
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": cwd, "prompt": "add a --json flag to the status command"})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd, "last_assistant_message": "Added."})
	if !strings.Contains(out, "1 of this session's decisions wait") || !strings.Contains(out, pageNotUp+"/") || strings.Contains(out, "Start the page") {
		t.Fatalf("the line must always say where to answer (the page is started in the background; here it never comes up): %s", out)
	}
	if len(spawned) != 1 || !strings.HasPrefix(spawned[0], "serve --store "+filepath.Join(cwd, ".shikii")+" --port 0 --local ") {
		t.Fatalf("the first request starts this project's page once on a free port; the Stop does not start a second one: %v", spawned)
	}
	if strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("Stop must not return hookSpecificOutput (it would continue the conversation): %s", out)
	}
	// 新しく答え待ちになった判定が無ければ、同じ数を毎ターン繰り返さない(同じ依頼の 2 回目の Stop は問いも立てない)
	if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd}); out != "" {
		t.Fatalf("nothing new: the line must not repeat: %s", out)
	}
	// 画面が立っている → 起こさない。1 件に答え、次の依頼で 1 件増えると残りは 1 件。
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	t.Cleanup(page.Close)
	common, _ := agentHome() // このプロジェクトの画面の印(0.7.6: ポートは空いているもの)
	writePageRecord(common, cwd, page.Listener.Addr().(*net.TCPAddr).Port, os.Getpid())
	spawned = nil
	recs, _ := (store.Store{Dir: agent}).Records("needs-plan", 0)
	capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", recs[0].TraceID, "no"}) })
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": cwd, "prompt": "and document the flag in the README too"})
	out = runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd, "last_assistant_message": "Documented."})
	if !strings.Contains(out, "1 of this session's decisions wait") || !strings.Contains(out, page.URL+"/") {
		t.Fatalf("with the page up the line must carry the URL and the remaining count: %s", out)
	}
	if len(spawned) != 0 {
		t.Fatalf("a page that is up must not be started again: %v", spawned)
	}
}

// モデルが無ければ hook が `shikii up` を裏で起こす(10 分に 1 回)。install は作法(skill)も置き、モデルを起こす。人がコマンドを打つ場面を無くす。
func TestHookStartsTheModelAndTheSkillByItself(t *testing.T) {
	oldLook := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil } // llama-server がある機械、のつもり(CI でも走る)
	t.Cleanup(func() { lookPath = oldLook })
	cwd := t.TempDir()
	hookEnv(t, nil, cwd) // モデルの口は閉じている
	home, _ := os.UserHomeDir()
	if !fileExists(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md")) {
		t.Fatal("hook install must put the agent's skill in place")
	}
	// install は guard を書いているので、まず guard を消して依頼の直後の起こし方を見る
	_ = os.Remove(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "starting-model"))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": cwd, "prompt": "add a flag to the status command"})
	if ups := spawnedUps(); len(ups) != 1 { // 画面(serve)の起動は別に数える(0.7.6)
		t.Fatalf("a missing model must start `shikii up` once: %v", spawned)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": cwd, "prompt": "and document it in the README as well"})
	runHook(t, "stop", map[string]any{"session_id": "m", "cwd": cwd})
	if len(spawnedUps()) != 1 {
		t.Fatalf("within %s the model must not be started again: %v", hookStartGap, spawned)
	}
	// 10 分過ぎれば(guard の時刻が古く、前の子も生きていなければ)もう一度
	old := time.Now().Add(-hookStartGap - time.Minute)
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "starting-model"), []byte("0 "+old.Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": cwd, "prompt": "now run the tests and tell me what fails"})
	if len(spawnedUps()) != 2 {
		t.Fatalf("after the gap the model may be started again: %v", spawned)
	}
	// install の表示に、人が打つコマンドは無い
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if strings.Contains(out, "shikii serve --store") || !strings.Contains(out, "started in the background") {
		t.Fatalf("install must not tell the person to start anything by hand: %s", out)
	}
}

// 監査で落ちなかった変異を捕まえる: needs-plan の迷いも 1 行、session_id の経路の無害化、壊れた settings.json は触らない、
// 線が引けた後の UserPromptSubmit は正しい event 名で文脈を返す。
func TestHookAuditGaps(t *testing.T) {
	if hookEventName("user-prompt-submit") != "UserPromptSubmit" || hookEventName("stop") != "Stop" {
		t.Fatal("event names must match Claude Code's")
	}
	// session_id は sessions/ の中に収まる
	if p := sessionPath("/a", "../../etc/passwd"); filepath.Dir(p) != filepath.Join("/a", "sessions") {
		t.Fatalf("session path must stay inside sessions/: %s", p)
	}
	if p := sessionFilesPath("/a", "s"); p != filepath.Join("/a", "sessions", "s.files") {
		t.Fatalf("files path: %s", p)
	}
	// cut は多バイト文字を割らない
	if s := cut("日本語の依頼", 7); s != "日本" {
		t.Fatalf("cut must not split a rune: %q", s)
	}
	// 古い版の pre-tool-use は黙って exit 0(settings に残っていても Claude Code に誤りが出ない)
	if out := capture(t, func() error { return cmdHook([]string{"pre-tool-use"}) }); out != "" {
		t.Fatalf("pre-tool-use must be a silent no-op: %q", out)
	}

	// 「redesign」を含む依頼だけ方針合わせが要る、と答える偽物
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "redesign") {
			return 0.9
		}
		return 0.1
	})
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	// 線が引けた needs-plan は UserPromptSubmit の文脈として返る(event 名が違うと Claude Code が捨てる)
	np := lowerMinN(t, agent, "needs-plan", 2)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": cwd, "prompt": "write the installation notes to docs/install.md"})
	for i := 0; i < 2; i++ {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": cwd, "prompt": fmt.Sprintf("redesign the whole authentication module, version %d", i)})
	}
	nrecs, _ := (store.Store{Dir: agent}).Records("needs-plan", 0)
	for _, r := range nrecs {
		truth := "no"
		if strings.Contains(r.Inputs["request"], "redesign") {
			truth = "yes"
		}
		capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", r.TraceID, truth}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{np, "--store", agent, "--model", "kev-4b", "--write"}) })
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": cwd, "prompt": "redesign the whole authentication module, version 9"})
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	hs, _ := got["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "UserPromptSubmit" || !strings.Contains(fmt.Sprint(hs["additionalContext"]), "EnterPlanMode") {
		t.Fatalf("calibrated needs-plan must return context under UserPromptSubmit: %s", out)
	}

	// 壊れた settings.json は触らず誤りを返す。空のファイルは {} と同じ
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(settings, []byte("{ broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdHook([]string{"install", "--yes"}); err == nil {
		t.Fatal("a broken settings.json must be an error")
	}
	if sb, _ := os.ReadFile(settings); string(sb) != "{ broken" {
		t.Fatal("a broken settings.json must not be rewritten")
	}
	if err := os.WriteFile(settings, []byte(`{"hooks":{"Stop":{"bad":"shape"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdHook([]string{"install", "--yes"}); err == nil || !strings.Contains(err.Error(), "hooks.Stop is not a list") {
		t.Fatalf("an event that is not a list must be refused, got %v", err)
	}
	if err := os.WriteFile(settings, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(settings); len(cmds) != 3 {
		t.Fatalf("an empty settings.json must be treated as {}: %v", cmds)
	}
}

// install は表示だけ → --yes で書く。2 回目は増えない。他の hook は残る。古い版の PreToolUse の shikii hook は外す。uninstall は自分の分だけ外す。spec は無いものだけ写す。
func TestHookInstallIsIdempotentAndKeepsOtherHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	other := `{"language":"Japanese","hooks":{"Stop":[{"hooks":[{"type":"command","command":"node other.mjs"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}],"SessionStart":[]}}`
	if err := os.WriteFile(settings, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, "shown only") {
		t.Fatalf("without --yes it must only show: %s", out)
	}
	if b, _ := os.ReadFile(settings); string(b) != other {
		t.Fatal("without --yes nothing may be written")
	}
	if fileExists(filepath.Join(home, ".shikii", "agent", "decisions")) {
		t.Fatal("without --yes no folder may be created")
	}

	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	cmds := hookCommands(settings)
	if len(cmds) != 3 || !strings.HasSuffix(cmds[0], " hook stop") || !strings.HasSuffix(cmds[1], " hook subagent-start") || !strings.HasSuffix(cmds[2], " hook user-prompt-submit") || !filepath.IsAbs(strings.Fields(cmds[0])[0]) {
		t.Fatalf("hooks after install must run the binary by its absolute path: %v", cmds)
	}
	// Codex には SubagentStart が無いので 2 つだけ(0.7.3)
	if codex := hookCommands(filepath.Join(home, ".codex", "hooks.json")); len(codex) != 2 || strings.Contains(strings.Join(codex, " "), "subagent-start") {
		t.Fatalf("Codex gets UserPromptSubmit and Stop only: %v", codex)
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), "node other.mjs") || !strings.Contains(string(b), "rtk hook claude") || !strings.Contains(string(b), `"language": "Japanese"`) || !strings.Contains(string(b), `"SessionStart": []`) {
		t.Fatalf("other hooks and keys must stay:\n%s", b)
	}
	if !fileExists(settings + ".bak-shikii") {
		t.Fatal("a backup must be written before changing settings.json")
	}
	for _, id := range hookSpecs {
		if !fileExists(filepath.Join(home, ".shikii", "agent", "decisions", id+".yaml")) {
			t.Fatalf("spec %s must be seeded", id)
		}
	}
	for _, id := range []string{"command-effect", "edit-in-scope"} {
		if fileExists(filepath.Join(home, ".shikii", "agent", "decisions", id+".yaml")) {
			t.Fatalf("the tool-hook spec %s must not be seeded any more", id)
		}
	}
	if !fileExists(filepath.Join(home, ".shikii", "agent", "wiki", "index.md")) {
		t.Fatal("wiki must be seeded")
	}

	// 2 回目: 増えない、人が直した spec は上書きしない
	mine := filepath.Join(home, ".shikii", "agent", "decisions", "needs-plan.yaml")
	if err := os.WriteFile(mine, []byte("id: needs-plan\n# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(settings); len(cmds) != 3 {
		t.Fatalf("second install must not duplicate: %v", cmds)
	}
	if !strings.Contains(out, "already there") {
		t.Fatalf("second install must say nothing changed: %s", out)
	}
	if b, _ := os.ReadFile(mine); string(b) != "id: needs-plan\n# mine\n" {
		t.Fatal("a spec the person edited must not be overwritten")
	}

	// 古い版が残した PreToolUse の shikii hook は install が外す(他の PreToolUse の hook は残る)
	var cfg map[string]any
	b, _ = os.ReadFile(settings)
	_ = json.Unmarshal(b, &cfg)
	hooks := cfg["hooks"].(map[string]any)
	hooks["PreToolUse"] = append(hooks["PreToolUse"].([]any), map[string]any{"matcher": "Bash|Edit", "hooks": []any{map[string]any{"type": "command", "command": "shikii hook pre-tool-use", "timeout": 10}}})
	b, _ = json.Marshal(cfg)
	if err := os.WriteFile(settings, b, 0o600); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(settings); len(cmds) != 3 || !strings.Contains(out, "hooks added") {
		t.Fatalf("install must remove the old pre-tool-use hook: %v\n%s", cmds, out)
	}
	if b, _ = os.ReadFile(settings); !strings.Contains(string(b), "rtk hook claude") || strings.Contains(string(b), "Bash|Edit") {
		t.Fatalf("the other PreToolUse hook must stay and the shikii group must go:\n%s", b)
	}

	// uninstall: 自分の分だけ消え、他の hook は残る
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if cmds := hookCommands(settings); len(cmds) != 0 {
		t.Fatalf("uninstall must remove shikii hooks: %v", cmds)
	}
	b, _ = os.ReadFile(settings)
	if !strings.Contains(string(b), "node other.mjs") || !strings.Contains(string(b), "rtk hook claude") || strings.Contains(string(b), "UserPromptSubmit") {
		t.Fatalf("other hooks must stay, empty events must go:\n%s", b)
	}
	if !fileExists(filepath.Join(home, ".shikii", "agent", "decisions", "prompt-kind.yaml")) {
		t.Fatal("uninstall must keep the agent's records and specs")
	}
	// 控えは shikii の hook が入っていない版: uninstall 後の settings.json(shikii 無し)でも bak は最初の 1 回のまま
	if bb, _ := os.ReadFile(settings + ".bak-shikii"); string(bb) != other {
		t.Fatalf("the backup must stay the first pre-shikii version:\n%s", bb)
	}
	// settings.json が元から無い時: install は控えを作らず、その後の uninstall も hook 入りの版を控えない
	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	s2 := filepath.Join(home2, ".claude", "settings.json")
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if fileExists(s2 + ".bak-shikii") {
		t.Fatal("a settings.json that shikii created must never be backed up with shikii's hooks in it")
	}
	// --help は stdout に usage、誤りではない
	if out := capture(t, func() error { return cmdHook([]string{"--help"}) }); !strings.Contains(out, "usage: shikii hook") || strings.Contains(out, "pre-tool-use") {
		t.Fatalf("--help must print the current usage: %s", out)
	}
}

// 2 回目の監査: ツリーの状態取りが repo の index と .git/objects を汚さない、並列の更新が消えない、抜き取りも答え待ちに入る、
// 同じ入力の正解 50 件では線を引こうとしない(calibrate と同じ数え方)。
func TestHookSecondAudit(t *testing.T) {
	// treeState: 未追跡ファイルの中身が .git/objects に残らず、本物の index も触らない。一覧には path が入る
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, listing, listed, project := treeState(repo)
	if hash == "" || !listed || project != filepath.Base(repo) || !strings.HasSuffix(listing, "\t.env") || strings.Contains(listing, "hunter2") {
		t.Fatalf("snapshot must work in a fresh repo and list paths, not contents: %q %q", hash, listing)
	}
	blob, _ := exec.Command("git", "-C", repo, "hash-object", ".env").Output()
	if err := exec.Command("git", "-C", repo, "cat-file", "-e", strings.TrimSpace(string(blob))).Run(); err == nil {
		t.Fatal("the content of an untracked file must not be left in the repo's .git/objects")
	}
	if out, _ := exec.Command("git", "-C", repo, "ls-files", "--cached").Output(); len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("the repo's own index must stay untouched, got %q", out)
	}
	if h, l, ok, _ := treeState(t.TempDir()); h != "" || l != "" || ok {
		t.Fatal("outside git the snapshot must be empty")
	}

	// 抜き取り(act の 20 件に 1 件)も人の答え待ちに入る
	audit := ""
	for i := 0; i < 2000 && audit == ""; i++ {
		tr := fmt.Sprintf("%016x", i)
		if store.AuditSample(tr) {
			audit = tr
		}
	}
	if audit == "" {
		t.Fatal("no audit sample trace found")
	}
	s := &hookSession{}
	noteAsked("done-as-asked", &decide.Decision{TraceID: audit, Band: decide.Act, IsCalibrated: true})(s)
	noteAsked("done-as-asked", &decide.Decision{TraceID: "ffffffffffffffff", Band: decide.Act, IsCalibrated: true})(s)
	if len(s.Asked) != 1 || s.Asked[0].Trace != audit {
		t.Fatalf("the audit sample must be asked and the plain act must not: %+v", s.Asked)
	}

	// 並列の更新が消えない(錠の中で 読む → 変える → 書く)
	model := fakeAgentModel(t, func(string) float64 { return 0.6 })
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = updateSession(agent, "par", func(s *hookSession) {
				s.Asked = append(s.Asked, askedItem{Spec: "done-as-asked", Trace: fmt.Sprintf("%016x", i)})
			})
		}(i)
	}
	wg.Wait()
	if got := len(loadSession(agent, "par").Asked); got != 30 {
		t.Fatalf("30 parallel updates must all survive, got %d", got)
	}
	if left, _ := filepath.Glob(filepath.Join(agent, "sessions", "*.tmp")); len(left) != 0 {
		t.Fatalf("no .tmp may be left behind: %v", left)
	}

	// 同じ入力(同じ依頼文)の正解が 3 件あっても、入力ごとに 1 件と数えるので線は引かない(毎 Stop に ERROR を書き続けない)
	lowerMinN(t, agent, "needs-plan", 3)
	same := "please run the whole test suite and tell me which packages fail"
	for i := 0; i < 3; i++ {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": fmt.Sprintf("dup%d", i), "cwd": cwd, "prompt": same})
	}
	recs, _ := (store.Store{Dir: agent}).Records("needs-plan", 0)
	if len(recs) != 3 {
		t.Fatalf("3 records expected: %d", len(recs))
	}
	for _, r := range recs {
		capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", r.TraceID, "no"}) })
	}
	runHook(t, "stop", map[string]any{"session_id": "dup0", "cwd": cwd})
	if logb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log")); strings.Contains(string(logb), "auto-calibrate needs-plan") {
		t.Fatalf("3 truths on one input are 1 truth; the hook must not try to draw a line:\n%s", logb)
	}
}

// Codex: ~/.codex/hooks.json にも同じ 2 つ(matcher は無い)。他の hook は残り、2 回目は増えず、古い版の PreToolUse は外れ、uninstall で自分の分だけ消える。
// stdin の鍵は Claude Code と同じなので、Codex の Stop(turn_id つき)でも同じ記録になる。
func TestHookCodex(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHooks := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(codexHooks), 0o755); err != nil {
		t.Fatal(err)
	}
	other := `{"description":"mine","hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":"~/.codex/scripts/deny-check.sh"}]},{"matcher":"Bash|apply_patch","hooks":[{"type":"command","command":"shikii hook pre-tool-use","timeout":10}]}]}}`
	if err := os.WriteFile(codexHooks, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	// hookEnv と同じ差し替え(HOME は上で決めたものを使う)
	agent := filepath.Join(home, ".shikii", "agent")
	oldLocal, oldPage, oldCal, oldSpawn := hookLocalURL, hookPageURL, hookCalibrate, hookSpawn
	hookLocalURL, hookPageURL, hookCalibrate, hookSpawn = model.URL, "http://127.0.0.1:1", runCalibrateInProcess, recordSpawn
	t.Cleanup(func() { hookLocalURL, hookPageURL, hookCalibrate, hookSpawn = oldLocal, oldPage, oldCal, oldSpawn })
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	cmds := hookCommands(codexHooks)
	if len(cmds) != 2 || !strings.HasSuffix(cmds[0], " hook stop") || !strings.HasSuffix(cmds[1], " hook user-prompt-submit") {
		t.Fatalf("Codex must get the 2 hooks and lose the old one: %v", cmds)
	}
	b, _ := os.ReadFile(codexHooks)
	if strings.Contains(string(b), "apply_patch") || !strings.Contains(string(b), "deny-check.sh") || !strings.Contains(string(b), `"description": "mine"`) {
		t.Fatalf("Codex hooks.json must drop the old PreToolUse group and keep what was there:\n%s", b)
	}
	if fileExists(codexHooks + ".bak-shikii") {
		t.Fatal("a file that already had shikii hooks in it must not be backed up")
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(codexHooks); len(cmds) != 2 {
		t.Fatalf("second install must not duplicate in Codex: %v", cmds)
	}

	// Codex の stdin(turn_id、model つき)でも同じように記録する
	repo := gitRepo(t)
	initTestProject(t, repo)
	agent = filepath.Join(repo, ".shikii")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "cx", "turn_id": "t1", "model": "gpt", "cwd": repo, "prompt": "add the installation notes and fix the parser"})
	if err := os.WriteFile(filepath.Join(repo, "install.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "cx", "turn_id": "t1", "model": "gpt", "cwd": repo, "last_assistant_message": "Added the notes; the parser fix is next."})
	recs, _ := (store.Store{Dir: agent}).Records("changes-in-scope", 0)
	if len(recs) != 1 || recs[0].Inputs["files"] != "install.md" {
		t.Fatalf("Codex Stop must be judged like Claude Code's: %+v", recs)
	}

	// uninstall は Codex 側も自分の分だけ外す
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	b, _ = os.ReadFile(codexHooks)
	if len(hookCommands(codexHooks)) != 0 || !strings.Contains(string(b), "deny-check.sh") {
		t.Fatalf("uninstall must remove only shikii's Codex hooks:\n%s", b)
	}
}

// 聞き方の候補(variants)がある spec は、人の正解が min_n に届き、前回から hookTuneEvery 件増えるたびに、Stop が tune を裏で回す(--write 無し。採用は人)。
// 候補の無い spec は回さない。増えていなければ回さない。
func TestHookAutoTunesWhenTruthsPileUp(t *testing.T) {
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "redesign") {
			return 0.9
		}
		return 0.1
	})
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	old := hookTuneEvery
	hookTuneEvery = 2
	t.Cleanup(func() { hookTuneEvery = old })
	np := lowerMinN(t, agent, "needs-plan", 2)
	b, _ := os.ReadFile(np)
	if err := os.WriteFile(np, append(b, []byte("variants:\n  - name: short\n    question: Should the agent agree on an approach with the user before changing anything for `request`?\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	lowerMinN(t, agent, "prompt-kind", 2) // 候補の無い spec: 正解が溜まっても回さない
	st := store.Store{Dir: agent}
	logHas := func(s string) int {
		lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
		return strings.Count(string(lb), s)
	}
	answer := func(i int) { // 1 依頼 → needs-plan と prompt-kind に人の正解を 1 件ずつ
		prompt := fmt.Sprintf("add a flag to the status command, round %d", i)
		if i%2 == 0 {
			prompt = fmt.Sprintf("redesign the storage layer, round %d", i)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": fmt.Sprintf("t%d", i), "cwd": cwd, "prompt": prompt})
		for _, id := range []string{"needs-plan", "prompt-kind"} {
			recs, _ := st.Records(id, 0)
			truth := "no"
			if id == "prompt-kind" {
				truth = "edit"
			} else if strings.Contains(recs[0].Inputs["request"], "redesign") {
				truth = "yes"
			}
			capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", recs[0].TraceID, truth}) })
		}
	}
	answer(1)
	runHook(t, "stop", map[string]any{"session_id": "t1", "cwd": cwd})
	if logHas("auto-tune needs-plan") != 0 {
		t.Fatal("below min_n nothing must be tuned")
	}
	answer(2)
	runHook(t, "stop", map[string]any{"session_id": "t2", "cwd": cwd})
	if logHas("auto-tune needs-plan") != 1 {
		lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
		t.Fatalf("at min_n the first tune must run once:\n%s", lb)
	}
	if logHas("ERROR") != 0 {
		lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
		t.Fatalf("auto-tune must succeed:\n%s", lb)
	}
	if runs, _ := filepath.Glob(filepath.Join(agent, "wiki", "runs", "*needs-plan*.md")); len(runs) != 1 {
		t.Fatalf("the run must be written to wiki/runs: %v", runs)
	}
	if g, _ := os.ReadFile(filepath.Join(agent, "tuned-needs-plan")); !strings.HasPrefix(string(g), "2 ") || len(strings.Fields(string(g))) != 3 {
		t.Fatalf("the guard must remember the count, the spec hash and the time of the last run: %q", g)
	}
	// 増えていなければ回さない(同じ Stop を繰り返しても 1 回のまま)
	runHook(t, "stop", map[string]any{"session_id": "t2", "cwd": cwd})
	answer(3)
	runHook(t, "stop", map[string]any{"session_id": "t3", "cwd": cwd})
	if logHas("auto-tune needs-plan") != 1 {
		t.Fatalf("one new truth is below hookTuneEvery: no second run")
	}
	answer(4)
	runHook(t, "stop", map[string]any{"session_id": "t4", "cwd": cwd})
	if logHas("auto-tune needs-plan") != 2 {
		t.Fatalf("after %d more truths the tune must run again", hookTuneEvery)
	}
	// 候補の無い spec は、正解が溜まっても回さない
	if logHas("auto-tune prompt-kind") != 0 {
		t.Fatal("a spec without variants has nothing to try: no auto-tune")
	}
	// 採用は人: spec の文言は変わっていない(variants は残る)
	if b, _ := os.ReadFile(np); !strings.Contains(string(b), "name: short") || strings.Contains(string(b), "version: 2") {
		t.Fatalf("auto-tune must not adopt: %s", b)
	}
}

// 監査の再現(2026-10-19): 依頼 A の Stop がモデル待ちの間に次の依頼 B が控えを書き終えていると、Stop A の変更は B の控えに当たってはいけない
// (B が「評価済み」になり、B の prompt-kind に結果の正解が付き、B の Stop が何も聞かなくなる)。
func TestHookStopDoesNotTouchTheNextRequest(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	st := store.Store{Dir: agent}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "r", "cwd": repo, "prompt": "fix the typo in a.go please"})
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stop A が始まる: 錠の外で控えを読み、モデルに聞く(まだ書かない)
	snapshot := loadSession(agent, "r")
	in := hookInput{SessionID: "r", CWD: repo, LastAssistantMessage: "Fixed the typo."}
	_, muts, err := hookStop(agent, &in, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// その間に依頼 B の UserPromptSubmit が終わる(控えを書く)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "r", "cwd": repo, "prompt": "now add a README that explains the module"})
	traceB := loadSession(agent, "r").Pending[0].Trace
	if err := updateSession(agent, "r", func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	sess := loadSession(agent, "r")
	labels, _ := st.LabeledTraces("prompt-kind")
	if sess.Judged {
		t.Fatal("request B must not be marked judged by request A's Stop")
	}
	if l, ok := labels[traceB]; ok {
		t.Fatalf("request B's prompt-kind must not get an outcome truth from A's Stop: %+v", l)
	}
	if sess.Previous == "Fixed the typo." {
		t.Fatal("A's closing message must not become B's previous")
	}
	// A の pending(A の trace)は決まっている
	if len(labels) != 1 {
		t.Fatalf("A's prompt-kind must be settled exactly once: %v", labels)
	}
	// B のターン: README を足して Stop → done-as-asked は 2 件目
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": "Added README.md."})
	recs, _ := st.Records("done-as-asked", 0)
	if len(recs) != 2 {
		t.Fatalf("done-as-asked for request B must still be asked: %d records", len(recs))
	}
	cis, _ := st.Records("changes-in-scope", 0)
	if len(cis) != 2 || cis[0].Inputs["files"] != "README.md" {
		t.Fatalf("B's changes must be compared against B's own listing: %+v", cis)
	}
}

// 監査の再現: サブディレクトリから起こしたセッションでも repo 全体を見る。chmod だけの変更も「変わった」。hook.log の 1 行は切る。
// 裏で起こす guard は flock で排他(並列 30 本で 1 回)で、前の子が生きていれば 10 分を過ぎても起こさない。tune の guard は spec の文言が変わると数え直す。
func TestHookSecondAuditRound(t *testing.T) {
	repo := gitRepo(t)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, before, listed, project := treeState(sub)
	if !listed || project != filepath.Base(repo) || !strings.Contains(before, "\tother.txt") {
		t.Fatalf("from a subdirectory the listing must cover the whole repo and the project must be the repo: listed=%v project=%q listing=%q", listed, project, before)
	}
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, after, _, _ := treeState(sub)
	if got := changedFiles(before, after); strings.Join(got, ",") != "other.txt" {
		t.Fatalf("a change outside the subdirectory must count: %v", got)
	}
	// mode だけの変更
	if err := os.Chmod(filepath.Join(repo, "other.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, modeChanged, _, _ := treeState(sub)
	if got := changedFiles(after, modeChanged); strings.Join(got, ",") != "other.txt" {
		t.Fatalf("a mode-only change must count: %v", got)
	}

	cwd := t.TempDir()
	agent := hookEnv(t, nil, cwd)
	// hook.log の 1 行は hookLogLineMax に切られ、改行は畳まれる(モデルの応答本文を丸ごと残さない)
	hookLog("line1\n" + strings.Repeat("x", 2000))
	lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
	last := lastLine(string(lb))
	if len(last) > hookLogLineMax+40 || strings.Contains(last, "line1\nx") {
		t.Fatalf("hook.log lines must be cut and single-line: %d bytes", len(last))
	}

	// startOnce: 並列 30 本で 1 回だけ、生きている子(自分の pid)がいれば gap を過ぎても起こさない
	var mu sync.Mutex
	calls := 0
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startOnce(agent, "thing", func() int { mu.Lock(); calls++; mu.Unlock(); return os.Getpid() })
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("30 parallel hooks must start the thing once, got %d", calls)
	}
	old := time.Now().Add(-hookStartGap - time.Minute)
	_ = os.Chtimes(filepath.Join(agent, "starting-thing"), old, old)
	if err := os.WriteFile(filepath.Join(agent, "starting-thing"), []byte(fmt.Sprintf("%d %s\n", os.Getpid(), old.Format(time.RFC3339))), 0o600); err != nil {
		t.Fatal(err)
	}
	if startOnce(agent, "thing", func() int { return 0 }) {
		t.Fatal("while the child is alive the thing must not be started again, even after the gap")
	}
	if err := os.WriteFile(filepath.Join(agent, "starting-thing"), []byte(fmt.Sprintf("%d %s\n", 999999, old.Format(time.RFC3339))), 0o600); err != nil {
		t.Fatal(err)
	}
	if !startOnce(agent, "thing", func() int { return 0 }) {
		t.Fatal("a dead child and an old guard must allow a new start")
	}

	// tune の guard: 文言(hash)が変わると前回の数を引き継がない
	oldEvery := hookTuneEvery
	hookTuneEvery = 50
	t.Cleanup(func() { hookTuneEvery = oldEvery })
	np := lowerMinN(t, agent, "needs-plan", 1)
	b, _ := os.ReadFile(np)
	if err := os.WriteFile(np, append(b, []byte("variants:\n  - name: short\n    question: Plan first for `request`?\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	sp, _ := spec.Load(np)
	if err := os.WriteFile(filepath.Join(agent, "tuned-needs-plan"), []byte("40 "+sp.Hash()+" 2026-10-19T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookLocalURL = fakeAgentModel(t, func(string) float64 { return 0.9 }).URL
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": cwd, "prompt": "redesign the storage layer"})
	recs, _ := (store.Store{Dir: agent}).Records("needs-plan", 0)
	capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", recs[0].TraceID, "yes"}) })
	runHook(t, "stop", map[string]any{"session_id": "g", "cwd": cwd})
	if lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log")); strings.Contains(string(lb), "auto-tune needs-plan") {
		t.Fatal("same wording, 1 truth since the last run of 40: no tune")
	}
	// 文言が変わった(hash が違う)→ 前回の 40 は引き継がず、1 件 ≥ min_n(1) で回る
	b, _ = os.ReadFile(np)
	if err := os.WriteFile(np, []byte(strings.Replace(string(b), "should the agent first agree", "must the agent first agree", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g2", "cwd": cwd, "prompt": "redesign the whole queue"})
	recs, _ = (store.Store{Dir: agent}).Records("needs-plan", 0)
	capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", recs[0].TraceID, "yes"}) })
	runHook(t, "stop", map[string]any{"session_id": "g2", "cwd": cwd})
	if lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log")); !strings.Contains(string(lb), "auto-tune needs-plan") {
		t.Fatalf("a changed wording must not inherit the old count:\n%s", lb)
	}
}

// 裏のエージェントの完了通知(Claude Code が「ユーザーの発言」として流す <task-notification>)は依頼ではない:
// 考える前の 2 問は聞かず、依頼・一覧・pending は置き換えない。通知の後に作業が続いたら、そのターンの終わりで
// 元の依頼に対してもう一度だけ判定し直す(本人の選択、2026-10-20)。実測では記録の半分近くが通知由来だった。
func TestHookIgnoresBackgroundNotificationsAsRequests(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	st := store.Store{Dir: agent}
	count := func(id string) int { recs, _ := st.Records(id, 0); return len(recs) }
	const human = "translate the README into German in the background and tell me when it is done"

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "bg", "cwd": repo, "prompt": human})
	runHook(t, "stop", map[string]any{"session_id": "bg", "cwd": repo, "last_assistant_message": "Started one agent; waiting."})
	if count("prompt-kind") != 1 || count("needs-plan") != 1 || count("done-as-asked") != 1 {
		t.Fatalf("the human request is judged once: kind=%d plan=%d done=%d", count("prompt-kind"), count("needs-plan"), count("done-as-asked"))
	}
	notices := []string{
		"<task-notification>\n<task-id>a1</task-id>\n<status>completed</status>\n<summary>Agent finished</summary>\n</task-notification>",
		"<system-reminder>\n[SYSTEM NOTIFICATION - NOT USER INPUT]\nThis is an automated background-task event.\n<task-notification>…</task-notification>\n</system-reminder>",
	}
	for _, n := range notices {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "bg", "cwd": repo, "prompt": n})
	}
	if count("prompt-kind") != 1 || count("needs-plan") != 1 {
		t.Fatalf("a notification is not a request and is not judged: kind=%d plan=%d", count("prompt-kind"), count("needs-plan"))
	}
	sess := loadSession(agent, "bg")
	if sess.Request != human || len(sess.Pending) != 1 {
		t.Fatalf("a notification must not replace the request or settle its pending: request=%q pending=%d", sess.Request, len(sess.Pending))
	}
	// 通知の後に作業が続いた: そのターンの終わりで、元の依頼に対して範囲と完了をもう一度判定する
	if err := os.WriteFile(filepath.Join(repo, "README_de.md"), []byte("Deutsch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "bg", "cwd": repo, "last_assistant_message": "German README written."})
	if count("changes-in-scope") != 1 || count("done-as-asked") != 2 {
		t.Fatalf("after a notification the Stop judges again: changes=%d done=%d", count("changes-in-scope"), count("done-as-asked"))
	}
	recs, _ := st.Records("changes-in-scope", 0)
	if recs[0].Inputs["request"] != human || recs[0].Inputs["files"] != "README_de.md" {
		t.Fatalf("the re-judgement is about the human request and the files changed since it: %+v", recs[0].Inputs)
	}
	// 通知が無ければ、同じ依頼の 3 回目の Stop は聞き直さない
	runHook(t, "stop", map[string]any{"session_id": "bg", "cwd": repo, "last_assistant_message": "German README written, again."})
	if count("done-as-asked") != 2 {
		t.Fatalf("without a new notification the same request is not judged again: %d", count("done-as-asked"))
	}
	// 結果の正解: 最初の Stop では「変わっていない」(!edit、部分)が付くが、通知の後の Stop でファイルが変わったので
	// 同じ trace に edit(重み 1)が付き直り、新しい方が勝つ。次の人の依頼では付け足さない
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "bg", "cwd": repo, "prompt": "thanks, now explain the Makefile"})
	latest, _ := st.LabeledTraces("prompt-kind")
	if len(latest) != 1 {
		t.Fatalf("one trace labeled: %+v", latest)
	}
	for _, l := range latest {
		if l.Truth != "edit" || l.Weight != 1 || l.Source != "outcome" {
			t.Fatalf("the newest outcome truth must be edit w=1: %+v", l)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "labels", "prompt-kind.jsonl")); strings.Count(string(b), `"truth":"!edit"`) != 1 || strings.Count(string(b), `"truth":"edit"`) != 1 {
		t.Fatalf("first !edit (partial) then edit on the same trace, nothing more:\n%s", b)
	}
	if sess := loadSession(agent, "bg"); len(sess.Pending) != 1 {
		t.Fatalf("only the new request's pending remains: %d", len(sess.Pending))
	}
}

// runTuneInProcess は hookTune の同プロセス版(テスト用)。
func runTuneInProcess(agent, path, storeDir string) {
	var buf bytes.Buffer
	_, err := func() (res *tuneResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("tune panicked: %v", r)
			}
		}()
		return runTune(tuneOpts{output: &buf, specPath: path, ef: endpointFlags{local: hookLocalURL}, storeDir: storeDir, labeled: "none"})
	}()
	line := lastLine(buf.String())
	if err != nil {
		line += " ERROR: " + err.Error()
	}
	hookLog(fmt.Sprintf("auto-tune %s: %s", strings.TrimSuffix(filepath.Base(path), ".yaml"), line))
}

// runCalibrateInProcess はテストで較正を同プロセス内で実行する。
func runCalibrateInProcess(agent, path, storeDir, model string) {
	var buf bytes.Buffer
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("calibrate panicked: %v", r)
			}
		}()
		return runCalibrate(path, calibrateOpts{output: &buf, ef: endpointFlags{local: hookLocalURL, model: model}, storeDir: storeDir, labeled: "none", bins: 10, write: true, rung: "minimal"})
	}()
	line := lastLine(buf.String())
	if err != nil {
		line += " ERROR: " + err.Error()
	}
	hookLog(fmt.Sprintf("auto-calibrate %s (%s): %s", strings.TrimSuffix(filepath.Base(path), ".yaml"), model, line))
}

// treeSnapshot は作業ツリーの中身のハッシュだけ(treeState の前半)。
func treeSnapshot(cwd string) string {
	h, _, _, _ := treeState(cwd)
	return h
}

// hookCommands はテストで settings.json の登録内容を確かめる。
func hookCommands(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	var out []string
	for _, groups := range cfg.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if isShikiiHook(h.Command) {
					out = append(out, h.Command)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// treeState はテストで scope 以外の作業ツリーの状態を読む。
func treeState(cwd string) (hash, listing string, listed bool, project string) {
	hash, listing, listed, project, _ = treeStateWithScope(cwd)
	return
}

// 言われなくても気付く: このプロジェクトのエージェントのメモリ(~/.claude/projects/<slug>/memory)に wiki へ移していないファイルがあれば、
// 依頼前の hook が作法の手順で移すよう伝える(セッションに 1 回)。移した印(.shikii/wiki <記事の id>)のあるファイルは数えない。途中まで移した置き場でも残りを出す。
// .md 以外は数えない。Codex の共通のメモリはこのプロジェクトを名指しする(経路・4 文字以上のフォルダ名を語として・origin の名)ファイルだけ数え、MEMORY.md は見ない。
func TestHookNoticesAgentMemoryNotYetInTheWiki(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	home, _ := os.UserHomeDir()
	// 置き場の経路: Claude Code の規則(英数字以外を - に)を固定の例で確かめる(本体と同じ式を写した鏡テストにしない。監査 R4)
	os.MkdirAll(filepath.Join(home, ".claude", "projects", "-tmp-x-y-z", "memory"), 0o755)
	if got := agentMemoryDir("/tmp/x.y_z"); got != filepath.Join(home, ".claude", "projects", "-tmp-x-y-z", "memory") {
		t.Fatalf("置き場の経路: %q", got)
	}
	repo := filepath.Join(t.TempDir(), "mywork1") // フォルダ名が 4 文字以上(名指しの語になる)
	os.MkdirAll(repo, 0o755)
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	initAt(t, repo)
	if dir := agentMemoryDir(repo); dir != "" {
		t.Fatalf("メモリの置き場が無ければ空: %q", dir)
	}
	exec.Command("git", "-C", repo, "remote", "add", "origin", "https://example.com/org/shikii-like.git").Run()
	dir := filepath.Join(home, ".claude", "projects", strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, repo), "memory")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# index\n- [a](feedback-a.md)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "feedback-a.md"), []byte("---\nname: feedback-a\n---\n\n日本語で書く。教訓は .shikii/wiki に入れる方針を決めた(移行はまだ)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "feedback-b.md"), []byte("---\nname: feedback-b\n---\n\n正本: .shikii/wiki 0123456789abcdef\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "feedback-old.md.bak"), []byte("古い\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.json"), []byte("{}\n"), 0o644)
	// Codex の共通のメモリ
	base := filepath.Base(repo)
	codex := filepath.Join(home, ".codex", "memories")
	os.MkdirAll(codex, 0o755)
	os.WriteFile(filepath.Join(codex, "MEMORY.md"), []byte("# 共通\n\n"+base+" の記憶は wiki が正本\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "this-project.md"), []byte("# "+base+" の決まり\n\nテストを先に書く\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "by-repo-name.md"), []byte("# shikii-like の決まり\n\nリポジトリの名で名指し\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "by-path.md"), []byte("# 経路で名指し\n\n"+repo+" は古い\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "other-project.md"), []byte("# 別のプロジェクトの決まり\n\n~/git/"+base+"-old/ は古い。関係ない\n"), 0o644)
	os.WriteFile(filepath.Join(codex, "perf-"+base+".json"), []byte("{}\n"), 0o644)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "m1", "cwd": repo, "prompt": "README を直して"})
	if !strings.Contains(out, "に 1 件(feedback-a.md)") || strings.Contains(out, "feedback-b.md") || strings.Contains(out, ".bak") || strings.Contains(out, "notes.json") || !strings.Contains(out, "import-memory.md") {
		t.Fatalf("未移行の .md だけを数え(wiki に触れただけの文は印にしない)、作法の手順を示す:\n%s", out)
	}
	if !strings.Contains(out, "Codex の") || !strings.Contains(out, "3 件(by-path.md, by-repo-name.md, this-project.md)") || strings.Contains(out, "other-project.md") || strings.Contains(out, "perf-") {
		t.Fatalf("Codex の共通のメモリは、経路・フォルダ名(語として)・origin の名で名指しする .md だけ数え、MEMORY.md と別のプロジェクトの物は数えない:\n%s", out)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "m1", "cwd": repo, "prompt": "次は画面を直して"})
	if strings.Contains(out, "移していない") {
		t.Fatalf("同じセッションでは二度言わない:\n%s", out)
	}
	os.WriteFile(filepath.Join(dir, "feedback-a.md"), []byte("---\nname: feedback-a\n---\n\n正本: .shikii/wiki fedcba9876543210\n"), 0o644)
	for _, name := range []string{"this-project.md", "by-repo-name.md", "by-path.md"} {
		b, _ := os.ReadFile(filepath.Join(codex, name))
		os.WriteFile(filepath.Join(codex, name), append(b, []byte("\n"+base+" では .shikii/wiki 0123456789abcdef に写した\n")...), 0o644)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "m2", "cwd": repo, "prompt": "次は画面を直して"})
	if strings.Contains(out, "移していない") {
		t.Fatalf("全部移したら出さない:\n%s", out)
	}
	// 名指しの語境界: 短いフォルダ名(app)は語としても数えず、経路だけ。「.」「/」は名指しの語にしない
	if names := projectNames(""); names != nil {
		t.Fatalf("空の scope は名指ししない: %v", names)
	}
	if names := projectNames("/work/app"); len(names) != 1 || names[0] != "/work/app" {
		t.Fatalf("短いフォルダ名は経路だけで名指しする: %v", names)
	}
	if namesProject("myapp の決まり", []string{"app"}) || !namesProject("app の決まり", []string{"app"}) || !namesProject("/work/app は古い", []string{"/work/app"}) {
		t.Fatal("名指しは語の境目で合わせ、経路は部分一致")
	}
}

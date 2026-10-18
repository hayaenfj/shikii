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
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// hookEnv は HOME を仮の場所にし、~/.shikii/agent を install して、モデルと画面の URL を差し替える。
func hookEnv(t *testing.T, model *httptest.Server) (agent string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent = filepath.Join(home, ".shikii", "agent")
	oldLocal, oldPage, oldCal := hookLocalURL, hookPageURL, hookCalibrate
	hookCalibrate = runCalibrateInProcess // テストは線を同プロセスで引く(本番は別プロセスを切り離す)
	hookLocalURL = "http://127.0.0.1:1"   // 閉じている口(モデルが無い)
	if model != nil {
		hookLocalURL = model.URL
	}
	hookPageURL = "http://127.0.0.1:1"
	t.Cleanup(func() { hookLocalURL, hookPageURL, hookCalibrate = oldLocal, oldPage, oldCal })
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	return agent
}

// runHook は stdin の JSON を与えて event を走らせ、stdout を返す。hook は何があっても誤りを返さない。
func runHook(t *testing.T, event string, in map[string]any) string {
	b, _ := json.Marshal(in)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	return strings.TrimSpace(capture(t, func() error { return cmdHook([]string{event}) }))
}

// 低い min_n の command-effect を agent の decisions に置く(install は無いものだけ写すので、先に置けばこちらが使われる)。
func writeAgentCommandEffect(t *testing.T, home string) {
	dir := filepath.Join(home, ".shikii", "agent", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `id: command-effect
version: 1
type: choice
question: What does the command do?
options:
  - id: read
    description: reads
  - id: change_project
    description: changes the project
  - id: change_outside
    description: changes outside
  - id: destroy
    description: destroys
  - id: send_outside
    description: sends outside
state:
  template: "{{ cwd }} {{ command }}"
  required: [command, cwd]
policy:
  send_to: [local]
  pii: true
thresholds:
  act: 0.75
  ask: 0.60
eval:
  min_n: 2
`
	if err := os.WriteFile(filepath.Join(dir, "command-effect.yaml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 帯 → 振る舞いの表。影(線が無い)では何も出さず、deny は決して出さない。
func TestHookOutcomeTable(t *testing.T) {
	d := func(band decide.Band, top string, p float64, calibrated bool) *decide.Decision {
		return &decide.Decision{TraceID: "0123456789abcdef", Band: band, Top: top, P: p, IsCalibrated: calibrated}
	}
	cases := []struct {
		name   string
		spec   string
		d      *decide.Decision
		bypass bool
		want   func(o hookOut) bool
	}{
		{"shadow: never speaks", "command-effect", d(decide.Ask, "destroy", 0.7, false), false, func(o hookOut) bool { return o.empty() }},
		{"shadow: even a confident destroy is silent without a line", "command-effect", d(decide.Act, "destroy", 0.99, false), false, func(o hookOut) bool { return o.empty() }},
		{"read acts silently", "command-effect", d(decide.Act, "read", 0.9, true), false, func(o hookOut) bool { return o.empty() }},
		{"change_project acts silently", "command-effect", d(decide.Act, "change_project", 0.9, true), false, func(o hookOut) bool { return o.empty() }},
		{"destroy asks", "command-effect", d(decide.Act, "destroy", 0.91, true), false, func(o hookOut) bool {
			return o.Permission == "ask" && strings.Contains(o.Reason, "destroy") && strings.Contains(o.Reason, "0.91")
		}},
		{"send_outside asks", "command-effect", d(decide.Act, "send_outside", 0.8, true), false, func(o hookOut) bool { return o.Permission == "ask" }},
		{"bypass: ask becomes a system line", "command-effect", d(decide.Act, "destroy", 0.91, true), true, func(o hookOut) bool {
			return o.Permission == "" && strings.Contains(o.System, "destroy")
		}},
		{"unsure with a line: system line with the answer URL", "command-effect", d(decide.Ask, "destroy", 0.62, true), false, func(o hookOut) bool {
			return o.Permission == "" && strings.Contains(o.System, "spec=command-effect&trace=0123456789abcdef")
		}},
		{"edit in scope: silent", "edit-in-scope", d(decide.Act, "yes", 0.9, true), false, func(o hookOut) bool { return o.empty() }},
		{"edit out of scope: context for the agent", "edit-in-scope", d(decide.Discard, "yes", 0.1, true), false, func(o hookOut) bool {
			return o.Permission == "" && strings.Contains(o.Context, "outside what the user asked")
		}},
		{"needs plan: context for the agent", "needs-plan", d(decide.Act, "yes", 0.88, true), false, func(o hookOut) bool {
			return o.Permission == "" && strings.Contains(o.Context, "EnterPlanMode")
		}},
		{"no plan needed: silent", "needs-plan", d(decide.Discard, "yes", 0.1, true), false, func(o hookOut) bool { return o.empty() }},
		{"prompt-kind: record only", "prompt-kind", d(decide.Act, "edit", 0.95, true), false, func(o hookOut) bool { return o.empty() }},
	}
	for _, c := range cases {
		o := hookOutcome(c.spec, c.d, c.bypass, hookLoc{})
		if o.Permission == "deny" {
			t.Fatalf("%s: a hook must never deny", c.name)
		}
		if !c.want(o) {
			t.Errorf("%s: got %+v", c.name, o)
		}
	}
}

// 影の期間: Bash の PreToolUse は記録だけして stdout に何も出さない。モデルが無ければ exit 0・無出力・hook.log に 1 行。
func TestHookShadowRecordsAndStaysSilent(t *testing.T) {
	model := fakeSystemOne(t, "kev-4b", map[string]float64{"read": 0.1, "change_project": 0.2, "change_outside": 0.05, "destroy": 0.6, "send_outside": 0.05})
	agent := hookEnv(t, model)
	out := runHook(t, "pre-tool-use", map[string]any{
		"session_id": "s1", "cwd": t.TempDir(), "permission_mode": "default", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "rm -rf build/"},
	})
	if out != "" {
		t.Fatalf("shadow mode must print nothing, got %q", out)
	}
	recs, err := (store.Store{Dir: agent}).Records("command-effect", 0)
	if err != nil || len(recs) != 1 || recs[0].Inputs["command"] != "rm -rf build/" || recs[0].Band != "ask" {
		t.Fatalf("one ask record expected, got %v %v", recs, err)
	}
	// 迷った判定はセッションの控えに「答え待ち」として残る
	sess := loadSession(agent, "s1")
	if len(sess.Asked) != 1 || sess.Asked[0].Spec != "command-effect" || sess.Asked[0].Trace != recs[0].TraceID {
		t.Fatalf("asked: %+v", sess.Asked)
	}

	// モデルが無い: 止めない(無出力)、理由は hook.log
	hookLocalURL = "http://127.0.0.1:1"
	out = runHook(t, "pre-tool-use", map[string]any{"session_id": "s1", "cwd": t.TempDir(), "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}})
	if out != "" {
		t.Fatalf("a missing model must not produce output, got %q", out)
	}
	logb, _ := os.ReadFile(filepath.Join(agent, "hook.log"))
	if !strings.Contains(string(logb), "127.0.0.1:1") {
		t.Fatalf("hook.log must say why: %q", logb)
	}
	// Bash 以外・Edit で依頼の控えが無い → 判定しない
	out = runHook(t, "pre-tool-use", map[string]any{"session_id": "s2", "cwd": t.TempDir(), "tool_name": "Edit", "tool_input": map[string]any{"file_path": "/x/y.go"}})
	if out != "" {
		t.Fatalf("Edit without a request must be silent, got %q", out)
	}
}

// 線が引けたら帯で振る舞う: destroy が act なら ask を出し、bypass なら端末の 1 行に落ちる。read が act なら黙る。
func TestHookCalibratedCommandEffectAsks(t *testing.T) {
	model := fakeSystemOne(t, "kev-4b", map[string]float64{"read": 0, "change_project": 0, "change_outside": 0, "destroy": 1, "send_outside": 0})
	agent := hookEnv(t, model)
	writeAgentCommandEffect(t, filepath.Dir(filepath.Dir(agent))) // 人が spec を直した(min_n を 2 に)、のつもり
	specPath := filepath.Join(agent, "decisions", "command-effect.yaml")
	storeDir := agent
	cwd := t.TempDir()
	var traces []string
	for i := 0; i < 3; i++ {
		out := runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": fmt.Sprintf("rm -rf %d", i)}})
		if out != "" {
			t.Fatalf("shadow: %q", out)
		}
	}
	recs, _ := (store.Store{Dir: storeDir}).Records("command-effect", 0)
	for _, r := range recs {
		traces = append(traces, r.TraceID)
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", tr, "destroy"}) })
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev-4b", "--write"})
	})

	out := runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "permission_mode": "default", "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf /tmp/x"}})
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	hs, _ := got["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "PreToolUse" || hs["permissionDecision"] != "ask" || !strings.Contains(hs["permissionDecisionReason"].(string), "destroy") {
		t.Fatalf("calibrated destroy must ask: %s", out)
	}
	out = runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "permission_mode": "bypassPermissions", "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf /tmp/y"}})
	got = nil
	_ = json.Unmarshal([]byte(out), &got)
	if _, has := got["hookSpecificOutput"]; has || !strings.Contains(fmt.Sprint(got["systemMessage"]), "destroy") {
		t.Fatalf("bypass: ask must become a system line only: %s", out)
	}
}

// UserPromptSubmit は prompt-kind と needs-plan を聞き、依頼とツリーのハッシュを控える。Stop はツリーが変わっていれば edit、
// 変わっていなければ !edit(重み 0.7)を結果の正解として付け、直前のアシスタントの発言を控える。
func TestHookPromptKindGetsOutcomeTruthAtStop(t *testing.T) {
	// どの質問にも同じ分布を返す偽物(prompt-kind は choice、needs-plan は noul)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		body, _ := readAll(r)
		_ = json.Unmarshal(body, &req)
		ans := map[string]any{}
		for k, q := range req.Questions {
			if q.Type == "noul" {
				ans[k] = map[string]any{"type": "noul", "noul": 0.2}
			} else {
				ans[k] = map[string]any{"type": "choice", "choice": "edit", "probabilities": map[string]float64{"edit": 0.9, "explain": 0.04, "investigate": 0.03, "run": 0.02, "decide": 0.01}, "confidence": 0.9}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "kev-4b", "answers": ans})
	}))
	t.Cleanup(model.Close)
	agent := hookEnv(t, model)
	// 人が prompt-kind の min_n を 4 に下げた、のつもり(install が写した spec を直す)
	pkPath := filepath.Join(agent, "decisions", "prompt-kind.yaml")
	pk, _ := os.ReadFile(pkPath)
	if err := os.WriteFile(pkPath, []byte(strings.Replace(string(pk), "min_n: 50", "min_n: 4", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	traceOf := map[string]string{} // session → prompt-kind の trace
	run := func(session string, change bool) {
		out := runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "fix the typo in README " + session})
		if out != "" {
			t.Fatalf("shadow: %q", out)
		}
		sess := loadSession(agent, session)
		if sess.Request != "fix the typo in README "+session || len(sess.Pending) != 1 || sess.Pending[0].Tree == "" {
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
		if sess.Previous != "Done: "+session || len(sess.Pending) != 0 {
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
		logb, _ := os.ReadFile(filepath.Join(agent, "hook.log"))
		t.Fatalf("Stop must draw the line once truths reach min_n. hook.log:\n%s", logb)
	}
	if logb, _ := os.ReadFile(filepath.Join(agent, "hook.log")); !strings.Contains(string(logb), "auto-calibrate prompt-kind (kev-4b)") {
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
	// 2 回目の UserPromptSubmit は previous(文脈)を持って聞く: 落ちないこと。短い返事は依頼を置き換えず前の依頼に添える(edit-in-scope が「ok」を依頼として読まない)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": "and the other one"})
	if out != "" || loadSession(agent, "same").Request != "fix the typo in README same / and the other one" {
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

// Stop の要約: 答え待ちがあれば 1 行(画面が立っていれば URL、無ければ起動コマンド)、無ければ無出力。答えが付けば数から外れる。
func TestHookStopSummarizesWhatWaitsForAPerson(t *testing.T) {
	model := fakeSystemOne(t, "kev-4b", map[string]float64{"read": 0.1, "change_project": 0.2, "change_outside": 0.05, "destroy": 0.6, "send_outside": 0.05})
	agent := hookEnv(t, model)
	cwd := t.TempDir()
	if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd}); out != "" {
		t.Fatalf("nothing waits yet: %q", out)
	}
	runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf a"}})
	runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf b"}})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd})
	if !strings.Contains(out, "2 of this session's decisions wait") || !strings.Contains(out, "--port 8096") {
		t.Fatalf("without a page the line must give the serve command: %s", out)
	}
	if strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("Stop must not return hookSpecificOutput (it would continue the conversation): %s", out)
	}
	// 新しく答え待ちになった判定が無ければ、同じ数を毎ターン繰り返さない
	if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd}); out != "" {
		t.Fatalf("nothing new: the line must not repeat: %s", out)
	}
	// 画面が立っている → URL。1 件に答え、1 件増えると、残りの数(2)で言い直す
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	t.Cleanup(page.Close)
	hookPageURL = page.URL
	recs, _ := (store.Store{Dir: agent}).Records("command-effect", 0)
	capture(t, func() error {
		return cmdLabel([]string{"--store", agent, "--source", "ui", recs[0].TraceID, "destroy"})
	})
	runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf c"}})
	out = runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd})
	if !strings.Contains(out, "2 of this session's decisions wait") || !strings.Contains(out, page.URL+"/") {
		t.Fatalf("with the page up the line must carry the URL and the remaining count: %s", out)
	}
}

// 監査で落ちなかった変異を捕まえる: change_outside も確認を出す、needs-plan の迷いも 1 行、Write も見る、session_id の経路の無害化、
// 壊れた settings.json は触らない、線が引けた後の UserPromptSubmit は正しい event 名で文脈を返す。
func TestHookAuditGaps(t *testing.T) {
	d := func(band decide.Band, top string, p float64) *decide.Decision {
		return &decide.Decision{TraceID: "0123456789abcdef", Band: band, Top: top, P: p, IsCalibrated: true}
	}
	if o := hookOutcome("command-effect", d(decide.Act, "change_outside", 0.9), false, hookLoc{}); o.Permission != "ask" || !strings.Contains(o.Reason, "change_outside") {
		t.Fatalf("change_outside must ask: %+v", o)
	}
	if o := hookOutcome("needs-plan", d(decide.Ask, "yes", 0.6), false, hookLoc{}); !strings.Contains(o.System, "spec=needs-plan&trace=") {
		t.Fatalf("an unsure needs-plan must give the answer URL: %+v", o)
	}
	if o := hookOutcome("edit-in-scope", d(decide.Ask, "yes", 0.6), false, hookLoc{}); !strings.Contains(o.System, "spec=edit-in-scope&trace=") {
		t.Fatalf("an unsure edit-in-scope must give the answer URL: %+v", o)
	}
	if hookEventName("user-prompt-submit") != "UserPromptSubmit" || hookEventName("pre-tool-use") != "PreToolUse" || hookEventName("stop") != "Stop" {
		t.Fatal("event names must match Claude Code's")
	}
	// session_id は sessions/ の中に収まる
	if p := sessionPath("/a", "../../etc/passwd"); filepath.Dir(p) != filepath.Join("/a", "sessions") {
		t.Fatalf("session path must stay inside sessions/: %s", p)
	}
	// cut は多バイト文字を割らない
	if s := cut("日本語の依頼", 7); s != "日本" {
		t.Fatalf("cut must not split a rune: %q", s)
	}

	// Write も edit-in-scope を聞く(依頼の控えがある時)。Edit だけではない
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				p := 0.1 // 「redesign」を含む依頼だけ方針合わせが要る、と答える偽物
				if strings.Contains(req.State, "redesign") {
					p = 0.9
				}
				ans[k] = map[string]any{"type": "noul", "noul": p}
			} else {
				ans[k] = map[string]any{"type": "choice", "choice": "edit", "probabilities": map[string]float64{"edit": 0.9, "explain": 0.04, "investigate": 0.03, "run": 0.02, "decide": 0.01}, "confidence": 0.9}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "kev-4b", "answers": ans})
	}))
	t.Cleanup(model.Close)
	agent := hookEnv(t, model)
	cwd := t.TempDir()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": cwd, "prompt": "write the installation notes to docs/install.md"})
	runHook(t, "pre-tool-use", map[string]any{"session_id": "w", "cwd": cwd, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(cwd, "docs", "install.md"), "content": "x"}})
	recs, _ := (store.Store{Dir: agent}).Records("edit-in-scope", 0)
	if len(recs) != 1 || recs[0].Inputs["file"] != filepath.Join("docs", "install.md") {
		t.Fatalf("Write must be asked as edit-in-scope with the path relative to cwd: %+v", recs)
	}

	// 線が引けた needs-plan は UserPromptSubmit の文脈として返る(event 名が違うと Claude Code が捨てる)
	np := filepath.Join(agent, "decisions", "needs-plan.yaml")
	b, _ := os.ReadFile(np)
	if err := os.WriteFile(np, []byte(strings.Replace(string(b), "min_n: 50", "min_n: 2", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	storeDir := agent
	for i := 0; i < 2; i++ {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": cwd, "prompt": fmt.Sprintf("redesign the whole authentication module, version %d", i)})
	}
	nrecs, _ := (store.Store{Dir: storeDir}).Records("needs-plan", 0)
	for _, r := range nrecs {
		truth := "no"
		if strings.Contains(r.Inputs["request"], "redesign") {
			truth = "yes"
		}
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", r.TraceID, truth}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{np, "--store", storeDir, "--model", "kev-4b", "--write"}) })
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": cwd, "prompt": "redesign the whole authentication module, version 9"})
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	hs, _ := got["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "UserPromptSubmit" || !strings.Contains(fmt.Sprint(hs["additionalContext"]), "EnterPlanMode") {
		t.Fatalf("calibrated needs-plan must return context under UserPromptSubmit: %s", out)
	}

	// 壊れた settings.json は触らず誤りを返す。空のファイルは {} と同じ
	home := filepath.Dir(filepath.Dir(agent))
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

// install は表示だけ → --yes で書く。2 回目は増えない。他の hook は残る。uninstall は自分の分だけ外す。spec は無いものだけ写す。
func TestHookInstallIsIdempotentAndKeepsOtherHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	other := `{"language":"Japanese","hooks":{"Stop":[{"hooks":[{"type":"command","command":"node other.mjs"}]}]}}`
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
	if len(cmds) != 3 || cmds[0] != "shikii hook pre-tool-use" || cmds[1] != "shikii hook stop" || cmds[2] != "shikii hook user-prompt-submit" {
		t.Fatalf("hooks after install: %v", cmds)
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), "node other.mjs") || !strings.Contains(string(b), `"language": "Japanese"`) || !strings.Contains(string(b), hookToolsRe) {
		t.Fatalf("other hooks and keys must stay, matcher must be set:\n%s", b)
	}
	if !fileExists(settings + ".bak-shikii") {
		t.Fatal("a backup must be written before changing settings.json")
	}
	for _, id := range []string{"command-effect", "edit-in-scope", "needs-plan", "prompt-kind"} {
		if !fileExists(filepath.Join(home, ".shikii", "agent", "decisions", id+".yaml")) {
			t.Fatalf("spec %s must be seeded", id)
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

	// uninstall: 自分の分だけ消え、他の hook は残る
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if cmds := hookCommands(settings); len(cmds) != 0 {
		t.Fatalf("uninstall must remove shikii hooks: %v", cmds)
	}
	b, _ = os.ReadFile(settings)
	if !strings.Contains(string(b), "node other.mjs") || strings.Contains(string(b), "PreToolUse") {
		t.Fatalf("other hooks must stay, empty events must go:\n%s", b)
	}
	if !fileExists(filepath.Join(home, ".shikii", "agent", "decisions", "command-effect.yaml")) {
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
	if out := capture(t, func() error { return cmdHook([]string{"--help"}) }); !strings.Contains(out, "usage: shikii hook") {
		t.Fatalf("--help must print usage: %s", out)
	}
}

// 設定の言語が英語以外なら、出す文は英語と併記し、記録に display(言語・質問・答え・出した文)が付く。英語なら英語だけで display は無い。
func TestHookWritesTheConfiguredLanguageAlongsideEnglish(t *testing.T) {
	model := fakeSystemOne(t, "kev-4b", map[string]float64{"read": 0, "change_project": 0, "change_outside": 0, "destroy": 1, "send_outside": 0})
	agent := hookEnv(t, model)
	home := filepath.Dir(filepath.Dir(agent))
	writeAgentCommandEffect(t, home)
	specPath := filepath.Join(agent, "decisions", "command-effect.yaml")
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(specPath, append(b, []byte("display:\n  ja:\n    question: このコマンドは何をするか？\n    options:\n      destroy: 消す・上書き・戻せない変更\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
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
	storeDir := agent
	cwd := t.TempDir()
	st := store.Store{Dir: storeDir}

	// 英語: 影の記録に display は無い
	setLang("English")
	runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf 0"}})
	recs, _ := st.Records("command-effect", 0)
	if len(recs) != 1 || recs[0].Display != nil {
		t.Fatalf("English must leave no display: %+v", recs)
	}
	// 日本語: 影でも質問と答えの訳が記録に付く(出した文は無い)
	setLang("Japanese")
	runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf 1"}})
	runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf 2"}})
	recs, _ = st.Records("command-effect", 0)
	if d := recs[0].Display; d == nil || d.Lang != "ja" || d.Question != "このコマンドは何をするか？" || d.Top != "消す・上書き・戻せない変更" || d.Message != "" {
		t.Fatalf("shadow record in Japanese must carry question and answer, no message: %+v", recs[0].Display)
	}
	for _, r := range recs {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", r.TraceID, "destroy"}) })
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev-4b", "--write"})
	})

	// 線が引けた後: 確認の理由は英語 / 日本語の併記、記録の display.message にも同じ文
	out := runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "permission_mode": "default", "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf 3"}})
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	hs, _ := got["hookSpecificOutput"].(map[string]any)
	reason := fmt.Sprint(hs["permissionDecisionReason"])
	if !strings.Contains(reason, "looks like destroy") || !strings.Contains(reason, "「消す・上書き・戻せない変更」に見える") {
		t.Fatalf("reason must carry English and Japanese: %s", out)
	}
	recs, _ = st.Records("command-effect", 0)
	if d := recs[0].Display; d == nil || d.Message != reason {
		t.Fatalf("the record must keep the message that was shown: %+v", recs[0].Display)
	}
	// 英語に戻すと併記は消える
	setLang("English")
	out = runHook(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": cwd, "permission_mode": "default", "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf 4"}})
	if strings.Contains(out, "見える") {
		t.Fatalf("English setting must not add Japanese: %s", out)
	}
	// noul の答えは display が無くても はい / いいえ
	if l := localize(&spec.Spec{}, &decide.Decision{Top: "yes"}, "ja"); l.Top != "はい" {
		t.Fatalf("noul yes must read はい in Japanese, got %+v", l)
	}
}

// 2 回目の監査: ツリーのハッシュ取りが repo の index と .git/objects を汚さない、重い 3 択の和で確認を出す、並列の更新が消えない、抜き取りも答え待ちに入る、
// 同じ入力の正解 50 件では線を引こうとしない(calibrate と同じ数え方)。
func TestHookSecondAudit(t *testing.T) {
	// treeSnapshot: 未追跡ファイルの中身が .git/objects に残らず、本物の index も触らない
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if treeSnapshot(repo) == "" {
		t.Fatal("snapshot must work in a fresh repo")
	}
	blob, _ := exec.Command("git", "-C", repo, "hash-object", ".env").Output()
	if err := exec.Command("git", "-C", repo, "cat-file", "-e", strings.TrimSpace(string(blob))).Run(); err == nil {
		t.Fatal("the content of an untracked file must not be left in the repo's .git/objects")
	}
	if out, _ := exec.Command("git", "-C", repo, "ls-files", "--cached").Output(); len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("the repo's own index must stay untouched, got %q", out)
	}
	if treeSnapshot(t.TempDir()) != "" {
		t.Fatal("outside git the snapshot must be empty")
	}

	// 重い 3 択の間で割れている(destroy 0.45 / send_outside 0.40)なら、迷いではなく確認
	split := &decide.Decision{TraceID: "0123456789abcdef", Band: decide.Ask, Top: "destroy", P: 0.45, IsCalibrated: true}
	o := hookOutcome("command-effect", split, false, hookLoc{Heavy: 0.85, Act: 0.75})
	if o.Permission != "ask" || !strings.Contains(o.Reason, "together p=0.85") {
		t.Fatalf("a split between heavy options must still ask: %+v", o)
	}
	if o := hookOutcome("command-effect", split, false, hookLoc{Heavy: 0.55, Act: 0.75}); o.Permission != "" || !strings.Contains(o.System, "unsure") {
		t.Fatalf("below act the split is just unsure: %+v", o)
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
	noteAsked("command-effect", &decide.Decision{TraceID: audit, Band: decide.Act, IsCalibrated: true})(s)
	noteAsked("command-effect", &decide.Decision{TraceID: "ffffffffffffffff", Band: decide.Act, IsCalibrated: true})(s)
	if len(s.Asked) != 1 || s.Asked[0].Trace != audit {
		t.Fatalf("the audit sample must be asked and the plain act must not: %+v", s.Asked)
	}

	// 並列の更新が消えない(錠の中で 読む → 変える → 書く)
	model := fakeSystemOne(t, "kev-4b", map[string]float64{"read": 0.1, "change_project": 0.2, "change_outside": 0.05, "destroy": 0.6, "send_outside": 0.05})
	agent := hookEnv(t, model)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = updateSession(agent, "par", func(s *hookSession) {
				s.Asked = append(s.Asked, askedItem{Spec: "command-effect", Trace: fmt.Sprintf("%016x", i)})
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

	// 同じ入力(ls)の正解が 50 件あっても、入力ごとに 1 件と数えるので線は引かない(毎 Stop に ERROR を書き続けない)
	cwd := t.TempDir()
	storeDir := agent
	specPath := filepath.Join(agent, "decisions", "command-effect.yaml")
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(specPath, []byte(strings.Replace(string(b), "min_n: 50", "min_n: 3", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		runHook(t, "pre-tool-use", map[string]any{"session_id": "dup", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}})
	}
	recs, _ := (store.Store{Dir: storeDir}).Records("command-effect", 0)
	for _, r := range recs {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", r.TraceID, "destroy"}) })
	}
	runHook(t, "stop", map[string]any{"session_id": "dup", "cwd": cwd})
	if logb, _ := os.ReadFile(filepath.Join(storeDir, "hook.log")); strings.Contains(string(logb), "auto-calibrate command-effect") {
		t.Fatalf("3 truths on one input are 1 truth; the hook must not try to draw a line:\n%s", logb)
	}
}

// Codex: ~/.codex/hooks.json にも同じ 3 つ(PreToolUse は Bash|apply_patch)。他の hook は残り、2 回目は増えず、uninstall で自分の分だけ消える。
// Codex は "ask" を受けないので確認は端末の 1 行に落ち、apply_patch は patch の文から触るファイルを取って edit-in-scope を聞く。
func TestHookCodex(t *testing.T) {
	files := patchFiles("*** Begin Patch\n*** Update File: cmd/a.go\n@@\n-x\n+y\n*** Add File: docs/b.md\n+hi\n*** Delete File: c.txt\n*** End Patch\n", 3)
	if strings.Join(files, ",") != "cmd/a.go,docs/b.md,c.txt" {
		t.Fatalf("patchFiles: %v", files)
	}
	if got := patchFiles("*** Update File: a\n*** Update File: b\n*** Update File: c\n*** Update File: d\n", 2); len(got) != 2 {
		t.Fatalf("patchFiles must stop at max: %v", got)
	}

	model := fakeSystemOne(t, "kev-4b", map[string]float64{"read": 0, "change_project": 0, "change_outside": 0, "destroy": 1, "send_outside": 0})
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHooks := filepath.Join(home, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(codexHooks), 0o755); err != nil {
		t.Fatal(err)
	}
	other := `{"description":"mine","hooks":{"PreToolUse":[{"matcher":"^Bash$","hooks":[{"type":"command","command":"~/.codex/scripts/deny-check.sh"}]}]}}`
	if err := os.WriteFile(codexHooks, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	// hookEnv と同じ差し替え(HOME は上で決めたものを使う)
	agent := filepath.Join(home, ".shikii", "agent")
	oldLocal, oldPage, oldCal := hookLocalURL, hookPageURL, hookCalibrate
	hookLocalURL, hookPageURL, hookCalibrate = model.URL, "http://127.0.0.1:1", runCalibrateInProcess
	t.Cleanup(func() { hookLocalURL, hookPageURL, hookCalibrate = oldLocal, oldPage, oldCal })
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	cmds := hookCommands(codexHooks)
	if len(cmds) != 3 {
		t.Fatalf("Codex must get the 3 hooks: %v", cmds)
	}
	b, _ := os.ReadFile(codexHooks)
	if !strings.Contains(string(b), `"matcher": "Bash|apply_patch"`) || !strings.Contains(string(b), "deny-check.sh") || !strings.Contains(string(b), `"description": "mine"`) {
		t.Fatalf("Codex hooks.json must carry the apply_patch matcher and keep what was there:\n%s", b)
	}
	if bb, _ := os.ReadFile(codexHooks + ".bak-shikii"); string(bb) != other {
		t.Fatal("the Codex file must be backed up once before shikii touches it")
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(codexHooks); len(cmds) != 3 {
		t.Fatalf("second install must not duplicate in Codex: %v", cmds)
	}

	// apply_patch → edit-in-scope(ファイル名は patch の見出しから)
	cwd := t.TempDir()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "cx", "turn_id": "t1", "cwd": cwd, "prompt": "add the installation notes and fix the parser"})
	runHook(t, "pre-tool-use", map[string]any{"session_id": "cx", "turn_id": "t1", "cwd": cwd, "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: docs/install.md\n+x\n*** End Patch\n"}})
	recs, _ := (store.Store{Dir: agent}).Records("edit-in-scope", 0)
	if len(recs) != 1 || recs[0].Inputs["file"] != "docs/install.md" {
		t.Fatalf("apply_patch must be asked as edit-in-scope with the patched file: %+v", recs)
	}

	// Codex(turn_id あり)では線が引けた後の destroy も "ask" を出さず、端末の 1 行に落ちる
	writeAgentCommandEffect(t, home)
	specPath := filepath.Join(agent, "decisions", "command-effect.yaml")
	storeDir := agent
	for i := 0; i < 3; i++ {
		runHook(t, "pre-tool-use", map[string]any{"session_id": "cx", "turn_id": "t2", "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": fmt.Sprintf("rm -rf %d", i)}})
	}
	crecs, _ := (store.Store{Dir: storeDir}).Records("command-effect", 0)
	for _, r := range crecs {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", r.TraceID, "destroy"}) })
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev-4b", "--write"})
	})
	out := runHook(t, "pre-tool-use", map[string]any{"session_id": "cx", "turn_id": "t3", "cwd": cwd, "permission_mode": "default", "tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf /tmp/x"}})
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if _, has := got["hookSpecificOutput"]; has || !strings.Contains(fmt.Sprint(got["systemMessage"]), "destroy") {
		t.Fatalf("Codex cannot take ask: it must become a system line only: %s", out)
	}

	// uninstall は Codex 側も自分の分だけ外す
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	b, _ = os.ReadFile(codexHooks)
	if len(hookCommands(codexHooks)) != 0 || !strings.Contains(string(b), "deny-check.sh") {
		t.Fatalf("uninstall must remove only shikii's Codex hooks:\n%s", b)
	}
}

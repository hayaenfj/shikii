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

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// hookEnv は HOME を仮の場所にし、~/.shikii/agent を install して、モデルと画面の URL を差し替える。
func hookEnv(t *testing.T, model *httptest.Server) (agent string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent = filepath.Join(home, ".shikii", "agent")
	oldLocal, oldPage := hookLocalURL, hookPageURL
	hookLocalURL = "http://127.0.0.1:1" // 閉じている口(モデルが無い)
	if model != nil {
		hookLocalURL = model.URL
	}
	hookPageURL = "http://127.0.0.1:1"
	t.Cleanup(func() { hookLocalURL, hookPageURL = oldLocal, oldPage })
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
		o := hookOutcome(c.spec, c.d, c.bypass)
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
	recs, err := (store.Store{Dir: filepath.Join(agent, ".shikii")}).Records("command-effect", 0)
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
	logb, _ := os.ReadFile(filepath.Join(agent, ".shikii", "hook.log"))
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
	storeDir := filepath.Join(agent, ".shikii")
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
				ans[k] = map[string]any{"type": "choice", "choice": "edit", "probabilities": map[string]float64{"edit": 0.7, "explain": 0.1, "investigate": 0.1, "run": 0.05, "decide": 0.05}, "confidence": 0.7}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "kev-4b", "answers": ans})
	}))
	t.Cleanup(model.Close)
	agent := hookEnv(t, model)
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	traceOf := map[string]string{} // session → prompt-kind の trace
	run := func(session string, change bool) {
		out := runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "fix the typo in README"})
		if out != "" {
			t.Fatalf("shadow: %q", out)
		}
		sess := loadSession(agent, session)
		if sess.Request != "fix the typo in README" || len(sess.Pending) != 1 || sess.Pending[0].Tree == "" {
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
	run("changed", true)
	run("same", false)
	labels, err := (store.Store{Dir: filepath.Join(agent, ".shikii")}).LabeledTraces("prompt-kind")
	if err != nil || len(labels) != 2 {
		t.Fatalf("two outcome labels expected: %v %v", labels, err)
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
	// 2 回目の UserPromptSubmit は previous(文脈)を持って聞く: 落ちないこと、依頼が更新されること
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": "and the other one"})
	if out != "" || loadSession(agent, "same").Request != "and the other one" {
		t.Fatalf("second prompt: out=%q sess=%+v", out, loadSession(agent, "same"))
	}
	// 空と / 始まりは判定しない
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "x", "cwd": repo, "prompt": "/model"}); out != "" || loadSession(agent, "x").Request != "" {
		t.Fatal("slash commands must be skipped")
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
	if !strings.Contains(out, "2 of this session's decisions wait") || !strings.Contains(out, "shikii serve --port 8096") {
		t.Fatalf("without a page the line must give the serve command: %s", out)
	}
	if strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("Stop must not return hookSpecificOutput (it would continue the conversation): %s", out)
	}
	// 画面が立っている → URL。1 件に答えると 1 件に減る
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	t.Cleanup(page.Close)
	hookPageURL = page.URL
	recs, _ := (store.Store{Dir: filepath.Join(agent, ".shikii")}).Records("command-effect", 0)
	capture(t, func() error {
		return cmdLabel([]string{"--store", filepath.Join(agent, ".shikii"), "--source", "ui", recs[0].TraceID, "destroy"})
	})
	out = runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd})
	if !strings.Contains(out, "1 of this session's decisions wait") || !strings.Contains(out, page.URL+"/") {
		t.Fatalf("with the page up the line must carry the URL and the remaining count: %s", out)
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
}

package main

// shikii hook: Claude Code の hook として、AI 自身の作業中の判断(このコマンドは何をするか、この編集は頼まれた範囲か、
// この依頼は着手前に方針を合わせるべきか、この依頼で結局ファイルを変えるか)を shikii に通す。
//
// 置き場は固定: ~/.shikii/agent/ に基本構成(decisions/ wiki/ .shikii/)。モデルは手元の :8080。答え合わせの画面は :8096。
// 線が無いうちは記録だけで何も出さない(影の期間)。線が引けたら帯で振る舞いを変える(hookOutcome)。deny は出さない。止めるのは人。
// 失敗(モデルが無い、遅い、書けない)は exit 0 で黙る: hook が作業を止めてはいけない。理由は .shikii/hook.log に 1 行。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// 固定の規則(オプションは無い)。テストだけが差し替える。
var (
	hookLocalURL      = "http://127.0.0.1:8080" // 手元のモデル(init の MCP 設定と同じ)
	hookPageURL       = "http://127.0.0.1:8096" // AI 自身の判断の答え合わせの画面(プロジェクトの serve は 8095)
	hookDecideTimeout = 5 * time.Second
	hookProbeTimeout  = 300 * time.Millisecond
	hookSessionTTL    = 7 * 24 * time.Hour
)

const (
	hookCmdPrefix = "shikii hook " // settings.json の中で自分の handler を見分ける印
	hookTimeout   = 10             // 秒。モデルの 5 秒 + 書き込み
	hookToolsRe   = "Bash|Edit|Write|MultiEdit|NotebookEdit"
	requestMax    = 1500 // request として控える文の長さ
)

func agentHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shikii", "agent"), nil
}

func cmdHook(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: shikii hook install [--yes] | uninstall [--yes] | pre-tool-use | user-prompt-submit | stop  (the last three read Claude Code's JSON on stdin)")
	}
	switch args[0] {
	case "install":
		return hookInstall(args[1:], true)
	case "uninstall":
		return hookInstall(args[1:], false)
	case "pre-tool-use", "user-prompt-submit", "stop":
		// hook は作業を止めない: 何があっても exit 0。理由は hook.log に
		if err := runHookEvent(args[0]); err != nil {
			hookLog(err.Error())
		}
		return nil
	}
	return fmt.Errorf("hook: unknown event %q (install | uninstall | pre-tool-use | user-prompt-submit | stop)", args[0])
}

// ---------------------------------------------------------------- Claude Code の JSON

// hookInput は Claude Code が stdin に渡す JSON のうち使う所。
type hookInput struct {
	SessionID            string          `json:"session_id"`
	CWD                  string          `json:"cwd"`
	PermissionMode       string          `json:"permission_mode"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	Prompt               string          `json:"prompt"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage string          `json:"last_assistant_message"`
}

// hookOut は hook が返すもの。空なら何も出さない(判断なし。承認ではない)。
type hookOut struct {
	Permission string // "" | "ask"(deny は出さない)
	Reason     string // ask の理由(確認の画面に出る)
	Context    string // エージェントに渡す文(additionalContext)
	System     string // 端末の人に見せる 1 行(systemMessage)
}

func (o hookOut) empty() bool { return o == hookOut{} }

func (o hookOut) json(event string) []byte {
	if o.empty() {
		return nil
	}
	specific := map[string]any{"hookEventName": event}
	if o.Permission != "" {
		specific["permissionDecision"] = o.Permission
		specific["permissionDecisionReason"] = o.Reason
	}
	if o.Context != "" {
		specific["additionalContext"] = o.Context
	}
	out := map[string]any{}
	if len(specific) > 1 { // Stop は文脈を返すと会話が続いてしまうので hookSpecificOutput を出さない(系の文だけ)
		out["hookSpecificOutput"] = specific
	}
	if o.System != "" {
		out["systemMessage"] = o.System
	}
	b, _ := json.Marshal(out)
	return b
}

// ---------------------------------------------------------------- セッションの状態

// hookSession はセッションごとの控え(~/.shikii/agent/.shikii/sessions/<id>.json)。transcript は読まない(形が文書化されていない)。
type hookSession struct {
	Request  string        `json:"request,omitempty"`  // 人が最後に送った依頼(UserPromptSubmit で控える)
	Previous string        `json:"previous,omitempty"` // 直前のアシスタントの発言(Stop で控える)
	Pending  []pendingItem `json:"pending,omitempty"`  // prompt-kind の結果の正解を待つ判定
	Asked    []askedItem   `json:"asked,omitempty"`    // このセッションで人の答えを待つことになった判定(ask と抜き取り)
	At       time.Time     `json:"at"`
}

type pendingItem struct {
	Trace string    `json:"trace"`
	Tree  string    `json:"tree"` // 作業ツリーのハッシュ(中身は入れない)
	At    time.Time `json:"at"`
}

type askedItem struct {
	Spec  string `json:"spec"`
	Trace string `json:"trace"`
}

func sessionPath(agent, id string) string {
	id = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
	if id == "" {
		id = "no-session"
	}
	return filepath.Join(agent, ".shikii", "sessions", id+".json")
}

func loadSession(agent, id string) *hookSession {
	s := &hookSession{}
	b, err := os.ReadFile(sessionPath(agent, id))
	if err == nil {
		_ = json.Unmarshal(b, s)
	}
	return s
}

func saveSession(agent, id string, s *hookSession) error {
	s.At = time.Now()
	p := sessionPath(agent, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// pruneSessions は古いセッションの控えを消す(7 日)。
func pruneSessions(agent string) {
	dir := filepath.Join(agent, ".shikii", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > hookSessionTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ---------------------------------------------------------------- 判定

// hookDecide は ~/.shikii/agent の spec で判定し、記録する。線が無ければ band は act にならない(= 影の期間)。
func hookDecide(agent, specID string, inputs map[string]string) (*spec.Spec, *decide.Decision, error) {
	sp, err := spec.Load(filepath.Join(agent, "decisions", specID+".yaml"))
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookDecideTimeout)
	defer cancel()
	eps := []decide.Endpoint{{Kind: "local", Client: systemone.New(hookLocalURL, "", "jev-latest")}}
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{}, eps)
	if err != nil {
		return nil, nil, err
	}
	st := store.Store{Dir: filepath.Join(agent, ".shikii")}
	attachReceipt(sp, st, d)
	if d.Band != decide.Abstain {
		if err := recordDecision(st, sp, d, inputs); err != nil {
			return nil, nil, err
		}
	}
	return sp, d, nil
}

// waitsForPerson は人の答えを待つ判定か: 迷った(ask)か、抜き取り(act の 20 件に 1 件)。
func waitsForPerson(d *decide.Decision) bool {
	return d.Band == decide.Ask || (d.Band == decide.Act && store.AuditSample(d.TraceID))
}

func answerURL(specID, trace string) string {
	return fmt.Sprintf("%s/?spec=%s&trace=%s", hookPageURL, specID, trace)
}

// hookOutcome は帯を振る舞いに写す純関数。線が無ければ(影)何も出さない。deny は出さない。
// bypass(--dangerously-skip-permissions)では ask を出さず、端末の 1 行に落とす: 本人が確認を飛ばす選択をしている。
func hookOutcome(specID string, d *decide.Decision, bypass bool) hookOut {
	if d == nil || !d.IsCalibrated || d.Band == decide.Abstain {
		return hookOut{}
	}
	unsure := func() hookOut {
		return hookOut{System: fmt.Sprintf("shikii: unsure (%s %s p=%.2f). Answer later: %s", specID, d.Top, d.P, answerURL(specID, d.TraceID))}
	}
	receipt := ""
	if r := d.Receipt; r != nil {
		receipt = fmt.Sprintf("; line on %d truths, human-checked precision %.2f", r.N, r.HumanPrecision)
	}
	switch specID {
	case "command-effect":
		switch d.Band {
		case decide.Act:
			switch d.Top {
			case "change_outside", "destroy", "send_outside":
				reason := fmt.Sprintf("shikii: this command looks like %s (p=%.2f%s)", d.Top, d.P, receipt)
				if bypass {
					return hookOut{System: reason}
				}
				return hookOut{Permission: "ask", Reason: reason}
			}
			return hookOut{}
		default: // ask / discard: 線はあるが迷った
			return unsure()
		}
	case "edit-in-scope":
		switch d.Band {
		case decide.Discard: // 確信して「範囲外」
			msg := fmt.Sprintf("shikii: this edit looks outside what the user asked for (edit-in-scope p(yes)=%.2f%s). If it is a needed step, say why; if not, do not make it", d.P, receipt)
			return hookOut{Context: msg, System: msg}
		case decide.Ask:
			return unsure()
		}
		return hookOut{}
	case "needs-plan":
		switch d.Band {
		case decide.Act:
			return hookOut{Context: fmt.Sprintf("shikii: this request looks like one to agree on an approach with the user before changing anything (needs-plan p=%.2f%s). Consider EnterPlanMode or a one-line confirmation first", d.P, receipt)}
		case decide.Ask:
			return unsure()
		}
		return hookOut{}
	}
	return hookOut{} // prompt-kind など: 記録だけ
}

// ---------------------------------------------------------------- event ごと

func runHookEvent(event string) error {
	agent, err := agentHome()
	if err != nil {
		return err
	}
	if !fileExists(filepath.Join(agent, "decisions")) {
		return fmt.Errorf("%s: not installed (shikii hook install --yes)", agent)
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 4<<20))
	if err != nil {
		return err
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("stdin is not Claude Code's hook JSON: %w", err)
	}
	bypass := in.PermissionMode == "bypassPermissions"
	sess := loadSession(agent, in.SessionID)
	var out hookOut
	switch event {
	case "pre-tool-use":
		out, err = hookPreToolUse(agent, &in, sess, bypass)
	case "user-prompt-submit":
		out, err = hookUserPrompt(agent, &in, sess)
	case "stop":
		out, err = hookStop(agent, &in, sess)
	}
	if serr := saveSession(agent, in.SessionID, sess); serr != nil && err == nil {
		err = serr
	}
	if b := out.json(hookEventName(event)); b != nil {
		fmt.Fprintln(stdout, string(b))
	}
	return err
}

func hookEventName(event string) string {
	switch event {
	case "pre-tool-use":
		return "PreToolUse"
	case "user-prompt-submit":
		return "UserPromptSubmit"
	}
	return "Stop"
}

func hookPreToolUse(agent string, in *hookInput, sess *hookSession, bypass bool) (hookOut, error) {
	var ti struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Notebook string `json:"notebook_path"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	switch in.ToolName {
	case "Bash":
		if strings.TrimSpace(ti.Command) == "" {
			return hookOut{}, nil
		}
		_, d, err := hookDecide(agent, "command-effect", map[string]string{"command": ti.Command, "cwd": in.CWD})
		if err != nil {
			return hookOut{}, err
		}
		noteAsked(sess, "command-effect", d)
		return hookOutcome("command-effect", d, bypass), nil
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		file := ti.FilePath
		if file == "" {
			file = ti.Notebook
		}
		if file == "" || sess.Request == "" {
			return hookOut{}, nil // 依頼の控えが無ければ「範囲か」は問えない
		}
		if rel, err := filepath.Rel(in.CWD, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = rel
		}
		_, d, err := hookDecide(agent, "edit-in-scope", map[string]string{"file": file, "request": sess.Request})
		if err != nil {
			return hookOut{}, err
		}
		noteAsked(sess, "edit-in-scope", d)
		return hookOutcome("edit-in-scope", d, bypass), nil
	}
	return hookOut{}, nil
}

func hookUserPrompt(agent string, in *hookInput, sess *hookSession) (hookOut, error) {
	req := strings.TrimSpace(in.Prompt)
	if req == "" || strings.HasPrefix(req, "/") {
		return hookOut{}, nil
	}
	if len(req) > requestMax {
		req = req[:requestMax]
	}
	sess.Request = req
	inputs := map[string]string{"request": req}
	if sess.Previous != "" {
		inputs["previous"] = sess.Previous // 文脈の段(迷った時だけ使われる)
	}
	var firstErr error
	// prompt-kind: 結果の正解のために作業ツリーのハッシュを控える
	if _, d, err := hookDecide(agent, "prompt-kind", inputs); err != nil {
		firstErr = err
	} else if d.Band != decide.Abstain {
		if tree := treeSnapshot(in.CWD); tree != "" {
			sess.Pending = append(sess.Pending, pendingItem{Trace: d.TraceID, Tree: tree, At: time.Now()})
		}
		noteAsked(sess, "prompt-kind", d)
	}
	_, d, err := hookDecide(agent, "needs-plan", inputs)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		return hookOut{}, firstErr
	}
	noteAsked(sess, "needs-plan", d)
	return hookOutcome("needs-plan", d, false), firstErr
}

func hookStop(agent string, in *hookInput, sess *hookSession) (hookOut, error) {
	if in.StopHookActive {
		return hookOut{}, nil
	}
	if m := strings.TrimSpace(in.LastAssistantMessage); m != "" {
		if len(m) > requestMax {
			m = m[:requestMax]
		}
		sess.Previous = m
	}
	st := store.Store{Dir: filepath.Join(agent, ".shikii")}
	// 結果の正解: 依頼の時のツリーと今のツリーが違えば edit、同じなら !edit(部分、重み 0.7)
	var firstErr error
	if len(sess.Pending) > 0 {
		now := treeSnapshot(in.CWD)
		for _, p := range sess.Pending {
			if now == "" {
				break
			}
			truth, weight := "!edit", 0.7
			if p.Tree != now {
				truth, weight = "edit", 1
			}
			if err := labelTrace(st, "prompt-kind", p.Trace, truth, weight, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		sess.Pending = nil
	}
	pruneSessions(agent)
	// 答え待ちの要約: このセッションで人の答えを待つことになった判定のうち、まだ答えの無いもの
	n := unansweredCount(st, sess.Asked)
	if n == 0 {
		return hookOut{}, firstErr
	}
	if serveUp() {
		return hookOut{System: fmt.Sprintf("shikii: %d of this session's decisions wait for your answer: %s/", n, hookPageURL)}, firstErr
	}
	return hookOut{System: fmt.Sprintf("shikii: %d of this session's decisions wait for your answer. Start the page: cd %s && shikii serve --port 8096 --local %s", n, agent, hookLocalURL)}, firstErr
}

func noteAsked(sess *hookSession, specID string, d *decide.Decision) {
	if waitsForPerson(d) {
		sess.Asked = append(sess.Asked, askedItem{Spec: specID, Trace: d.TraceID})
	}
}

func unansweredCount(st store.Store, asked []askedItem) int {
	labeled := map[string]map[string]store.Label{}
	n := 0
	for _, a := range asked {
		if _, ok := labeled[a.Spec]; !ok {
			m, err := st.LabeledTraces(a.Spec)
			if err != nil {
				m = map[string]store.Label{}
			}
			labeled[a.Spec] = m
		}
		if _, ok := labeled[a.Spec][a.Trace]; !ok {
			n++
		}
	}
	return n
}

func labelTrace(st store.Store, specID, trace, truth string, weight float64, source string) error {
	rec, err := st.FindRecord(specID, trace)
	if err != nil {
		return err
	}
	labels, err := store.LabelsFor(rec, truth, weight, source)
	if err != nil {
		return err
	}
	return st.AppendLabels(labels)
}

// serveUp は答え合わせの画面が立っているか(300ms)。
func serveUp() bool {
	c := &http.Client{Timeout: hookProbeTimeout}
	resp, err := c.Get(hookPageURL + "/v1/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// treeSnapshot は作業ツリーの状態のハッシュ: HEAD、status、diff --stat、diff --cached --stat の sha256 の先頭 16 桁。中身は入れない。
// git の外なら ""。
func treeSnapshot(cwd string) string {
	if cwd == "" {
		return ""
	}
	run := func(args ...string) (string, bool) {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", false
		}
		return out.String(), true
	}
	if _, ok := run("rev-parse", "--is-inside-work-tree"); !ok {
		return ""
	}
	head, _ := run("rev-parse", "HEAD") // commit が無ければ空
	status, ok := run("status", "--porcelain=v1", "--untracked-files=all")
	if !ok {
		return ""
	}
	diff, _ := run("diff", "--stat")
	cached, _ := run("diff", "--cached", "--stat")
	sum := sha256.Sum256([]byte(head + "\n" + status + "\n" + diff + "\n" + cached))
	return hex.EncodeToString(sum[:])[:16]
}

func hookLog(msg string) {
	agent, err := agentHome()
	if err != nil {
		return
	}
	p := filepath.Join(agent, ".shikii", "hook.log")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}

// ---------------------------------------------------------------- install / uninstall

// hookEvents は settings.json に足す 3 つ。command は PATH の shikii(MCP の設定と同じ流儀)。
var hookEvents = []struct {
	event, matcher, cmd string
	timeout             int
}{
	{"PreToolUse", hookToolsRe, hookCmdPrefix + "pre-tool-use", hookTimeout},
	{"UserPromptSubmit", "", hookCmdPrefix + "user-prompt-submit", hookTimeout},
	{"Stop", "", hookCmdPrefix + "stop", hookTimeout + 5},
}

func hookInstall(args []string, install bool) error {
	yes := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			yes = true
		} else {
			return fmt.Errorf("hook %s: unknown argument %q (only --yes)", map[bool]string{true: "install", false: "uninstall"}[install], a)
		}
	}
	agent, err := agentHome()
	if err != nil {
		return err
	}
	home := filepath.Dir(filepath.Dir(agent))
	settings := filepath.Join(home, ".claude", "settings.json")
	tilde := func(p string) string { return "~" + strings.TrimPrefix(p, home) }
	if install {
		fmt.Fprintf(stdout, "1. %s: decisions/ wiki/ .shikii/ for the agent's own decisions; copy the 4 specs that are missing (command-effect, edit-in-scope, needs-plan, prompt-kind) and the shared wiki pages. Existing files are never overwritten\n", tilde(agent))
		fmt.Fprintf(stdout, "2. %s: add 3 hooks (PreToolUse %s, UserPromptSubmit, Stop) running `shikii hook <event>`; a backup goes to settings.json.bak-shikii. Hooks that are already there are kept; a second install adds nothing\n", tilde(settings), hookToolsRe)
		fmt.Fprintf(stdout, "   Until a spec has a calibration line the hooks only record (shadow mode); they never deny a tool. The model is the local one on %s\n", hookLocalURL)
	} else {
		fmt.Fprintf(stdout, "1. %s: remove the handlers whose command starts with `%s` (other hooks stay). %s is kept: it holds the records and truths\n", tilde(settings), hookCmdPrefix, tilde(agent))
	}
	if !yes {
		fmt.Fprintln(stdout, "(shown only; pass --yes to do it)")
		return nil
	}
	if install {
		for _, d := range []string{"decisions", "wiki", filepath.Join("wiki", "specs"), filepath.Join("wiki", "runs"), ".shikii"} {
			if err := os.MkdirAll(filepath.Join(agent, d), 0o755); err != nil {
				return err
			}
		}
		n, err := seedAgentSpecs(filepath.Join(agent, "decisions"))
		if err != nil {
			return err
		}
		w, err := seedWiki(filepath.Join(agent, "wiki"))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %d specs and %d wiki pages added\n", tilde(agent), n, w)
	}
	changed, err := editHooks(settings, install)
	if err != nil {
		return err
	}
	switch {
	case changed && install:
		fmt.Fprintf(stdout, "%s: hooks added. Start a new terminal for them to take effect\n", tilde(settings))
	case changed:
		fmt.Fprintf(stdout, "%s: hooks removed. Start a new terminal for it to take effect\n", tilde(settings))
	case install:
		fmt.Fprintf(stdout, "%s: hooks were already there, nothing changed\n", tilde(settings))
	default:
		fmt.Fprintf(stdout, "%s: no shikii hooks found, nothing changed\n", tilde(settings))
	}
	if install {
		fmt.Fprintf(stdout, "answer page: cd %s && shikii serve --port 8096 --local %s   (then %s/)\n", tilde(agent), hookLocalURL, hookPageURL)
	}
	return nil
}

// seedAgentSpecs は埋め込みの spec を無いものだけ写す。写した数を返す。
func seedAgentSpecs(dst string) (int, error) {
	n := 0
	err := fs.WalkDir(shikii.AgentSpecsFS, "integrations/claude-code/specs", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		target := filepath.Join(dst, filepath.Base(p))
		if fileExists(target) {
			return nil
		}
		b, err := shikii.AgentSpecsFS.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

// editHooks は settings.json の hooks に shikii の 3 つを足す(install)か、`shikii hook ` で始まる handler を外す(uninstall)。
// 他の hook と他の鍵は残す。書く前に .bak-shikii に控える。変えたかを返す。
func editHooks(path string, install bool) (bool, error) {
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&cfg); err != nil {
			return false, fmt.Errorf("%s: %w (fix it by hand; shikii does not overwrite a broken file)", path, err)
		}
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	ours := func(h any) bool {
		m, _ := h.(map[string]any)
		c, _ := m["command"].(string)
		return strings.HasPrefix(c, hookCmdPrefix)
	}
	changed := false
	for _, ev := range hookEvents {
		groups, _ := hooks[ev.event].([]any)
		if install {
			present := false
			for _, g := range groups {
				gm, _ := g.(map[string]any)
				hs, _ := gm["hooks"].([]any)
				for _, h := range hs {
					if ours(h) && h.(map[string]any)["command"] == ev.cmd {
						present = true
					}
				}
			}
			if present {
				continue
			}
			group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": ev.cmd, "timeout": ev.timeout}}}
			if ev.matcher != "" {
				group["matcher"] = ev.matcher
			}
			hooks[ev.event] = append(groups, group)
			changed = true
			continue
		}
		var kept []any
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			var rest []any
			for _, h := range hs {
				if ours(h) {
					changed = true
				} else {
					rest = append(rest, h)
				}
			}
			if len(rest) > 0 {
				gm["hooks"] = rest
				kept = append(kept, gm)
			} else if len(hs) == 0 {
				kept = append(kept, gm)
			}
		}
		if len(kept) > 0 {
			hooks[ev.event] = kept
		} else {
			delete(hooks, ev.event)
		}
	}
	if !changed {
		return false, nil
	}
	if len(hooks) > 0 {
		cfg["hooks"] = hooks
	} else {
		delete(cfg, "hooks")
	}
	if len(b) > 0 {
		if err := os.WriteFile(path+".bak-shikii", b, 0o600); err != nil {
			return false, err
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path+".tmp", buf.Bytes(), 0o600); err != nil {
		return false, err
	}
	return true, os.Rename(path+".tmp", path)
}

// hookCommands は settings.json に入っている shikii の hook の command を返す(doctor / テスト用)。
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
				if strings.HasPrefix(h.Command, hookCmdPrefix) {
					out = append(out, h.Command)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

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
	"syscall"
	"time"
	"unicode/utf8"

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
	requestMax    = 1500 // request / command として控える文の長さ(バイト)
	shortReply    = 40   // これより短い依頼は返事とみなし、前の依頼に添える(文字数)
)

func agentHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shikii", "agent"), nil
}

func cmdHook(args []string) error {
	const usage = "usage: shikii hook install [--yes] | uninstall [--yes] | pre-tool-use | user-prompt-submit | stop\n  install/uninstall: Claude Code hooks that measure the agent's own decisions (shown first; --yes does it)\n  the other three are run by Claude Code and read its JSON on stdin; not for typing by hand\n"
	if len(args) == 0 {
		return errors.New(strings.TrimSpace(usage))
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return nil
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

// message は記録に残す「出した文」: 確認の理由、端末の 1 行、エージェントへの文脈のうち最初にあるもの。
func (o hookOut) message() string {
	for _, s := range []string{o.Reason, o.System, o.Context} {
		if s != "" {
			return s
		}
	}
	return ""
}

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
	Reported int           `json:"reported,omitempty"` // Stop で最後に答え待ちを伝えた時の Asked の数(新しく増えた時だけ言う)
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

// lockSession はセッションの控えの錠(flock)。Claude Code は hook を並列に走らせる(Bash と Edit の PreToolUse が同時、Stop と次の依頼が重なる)ので、
// 読む → 変える → 書く の間を 1 プロセスずつにする。錠が取れなければ待たずに進む(hook は止めない)。
func lockSession(agent, id string) (unlock func()) {
	p := sessionPath(agent, id) + ".lock"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

func loadSession(agent, id string) *hookSession {
	s := &hookSession{}
	b, err := os.ReadFile(sessionPath(agent, id))
	if err == nil {
		if uerr := json.Unmarshal(b, s); uerr != nil {
			hookLog(fmt.Sprintf("session %s: unreadable (%v); starting over", id, uerr))
			s = &hookSession{}
		}
	}
	return s
}

// saveSession は tmp に書いて rename する(書き途中を別のプロセスが読まない。editHooks と同じ流儀)。
func saveSession(agent, id string, s *hookSession) error {
	s.At = time.Now()
	p := sessionPath(agent, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := fmt.Sprintf("%s.%d.tmp", p, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// cut は s を最大 n バイトに切る(多バイト文字を割らない)。
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
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

// hookDecide は ~/.shikii/agent の spec で判定し、帯を振る舞いに写し、記録する。線が無ければ band は act にならない(= 影の期間)。
// 設定の言語が英語以外なら、出す文は英語と併記し、記録にも display(言語、質問、答え、出した文)を添える。
func hookDecide(agent, specID string, inputs map[string]string, bypass bool) (*decide.Decision, hookOut, error) {
	sp, err := spec.Load(filepath.Join(agent, "decisions", specID+".yaml"))
	if err != nil {
		return nil, hookOut{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookDecideTimeout)
	defer cancel()
	eps := []decide.Endpoint{{Kind: "local", Client: systemone.New(hookLocalURL, "", "jev-latest")}}
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{}, eps)
	if err != nil {
		return nil, hookOut{}, err
	}
	st := store.Store{Dir: filepath.Join(agent, ".shikii")}
	attachReceipt(sp, st, d)
	l := localize(sp, d, hookLang())
	out := hookOutcome(specID, d, bypass, l)
	if d.Band != decide.Abstain {
		rec := recordOf(sp, d, inputs)
		if l.Lang != "" {
			rec.Display = &store.RecordDisplay{Lang: l.Lang, Question: l.Question, Top: l.Top, Message: out.message()}
		}
		if err := recordDecisionRec(st, sp, d, rec); err != nil {
			return nil, hookOut{}, err
		}
	}
	return d, out, nil
}

// ---------------------------------------------------------------- 設定の言語(併記)

// langCodes は Claude Code の設定 `language` の値 → 言語コード(README の 15 言語)。英語と不明は ""(併記しない)。
var langCodes = map[string]string{
	"japanese": "ja", "日本語": "ja", "chinese": "zh", "simplified chinese": "zh", "简体中文": "zh", "traditional chinese": "zh-TW", "繁體中文": "zh-TW",
	"korean": "ko", "한국어": "ko", "german": "de", "deutsch": "de", "spanish": "es", "español": "es", "french": "fr", "français": "fr",
	"italian": "it", "italiano": "it", "portuguese": "pt-BR", "português": "pt-BR", "russian": "ru", "русский": "ru", "ukrainian": "uk", "українська": "uk",
	"polish": "pl", "polski": "pl", "turkish": "tr", "türkçe": "tr", "vietnamese": "vi", "tiếng việt": "vi", "indonesian": "id", "bahasa indonesia": "id",
}

// hookLang は ~/.claude/settings.json の language を言語コードにする。英語・無い・不明なら ""。
func hookLang() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Language string `json:"language"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return langCodes[strings.ToLower(strings.TrimSpace(cfg.Language))]
}

// hookLoc は設定の言語での言い方: 言語コード、spec の display.<lang> の質問と答え(無ければ空)。Lang が "" なら英語だけ。
type hookLoc struct {
	Lang, Question, Top string
}

func localize(sp *spec.Spec, d *decide.Decision, lang string) hookLoc {
	l := hookLoc{Lang: lang}
	if lang == "" || sp == nil || d == nil {
		return l
	}
	if disp, ok := sp.Display[lang]; ok {
		l.Question = strings.TrimSpace(disp.Question)
		l.Top = disp.Options[d.Top]
	}
	if l.Top == "" && lang == "ja" {
		l.Top = map[string]string{"yes": "はい", "no": "いいえ"}[d.Top]
	}
	return l
}

// topName は答えの人の言葉(無ければ id)。
func (l hookLoc) topName(id string) string {
	if l.Top != "" {
		return l.Top
	}
	return id
}

// bi は英語の文に、設定の言語の文を併記する(ja だけ文の型を持つ。他の言語は英語のまま)。sep は "\n"(端末・文脈)か " / "(確認の理由の 1 行)。
func bi(en, ja, sep string, l hookLoc) string {
	if l.Lang != "ja" || ja == "" {
		return en
	}
	return en + sep + ja
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
func hookOutcome(specID string, d *decide.Decision, bypass bool, l hookLoc) hookOut {
	if d == nil || !d.IsCalibrated || d.Band == decide.Abstain {
		return hookOut{}
	}
	url := answerURL(specID, d.TraceID)
	unsure := func() hookOut {
		return hookOut{System: bi(
			fmt.Sprintf("shikii: unsure (%s %s p=%.2f). Answer later: %s", specID, d.Top, d.P, url),
			fmt.Sprintf("shikii: 迷っている(%s「%s」p=%.2f)。あとで答える: %s", specID, l.topName(d.Top), d.P, url), "\n", l)}
	}
	receipt, receiptJA := "", ""
	if r := d.Receipt; r != nil {
		receipt = fmt.Sprintf("; line on %d truths, human-checked precision %.2f", r.N, r.HumanPrecision)
		receiptJA = fmt.Sprintf("。線は正解 %d 件、人が確かめた精度 %.2f", r.N, r.HumanPrecision)
	}
	switch specID {
	case "command-effect":
		switch d.Band {
		case decide.Act:
			switch d.Top {
			case "change_outside", "destroy", "send_outside":
				en := fmt.Sprintf("shikii: this command looks like %s (p=%.2f%s)", d.Top, d.P, receipt)
				ja := fmt.Sprintf("shikii: このコマンドは「%s」に見える(p=%.2f%s)", l.topName(d.Top), d.P, receiptJA)
				if bypass {
					return hookOut{System: bi(en, ja, "\n", l)}
				}
				return hookOut{Permission: "ask", Reason: bi(en, ja, " / ", l)}
			}
			return hookOut{}
		default: // ask / discard: 線はあるが迷った
			return unsure()
		}
	case "edit-in-scope":
		switch d.Band {
		case decide.Discard: // 確信して「範囲外」
			msg := bi(
				fmt.Sprintf("shikii: this edit looks outside what the user asked for (edit-in-scope p(yes)=%.2f%s). If it is a needed step, say why; if not, do not make it", d.P, receipt),
				fmt.Sprintf("shikii: この編集は頼まれた範囲の外に見える(edit-in-scope p(はい)=%.2f%s)。必要な手順なら理由を言い、違えば直さない", d.P, receiptJA), "\n", l)
			return hookOut{Context: msg, System: msg}
		case decide.Ask:
			return unsure()
		}
		return hookOut{}
	case "needs-plan":
		switch d.Band {
		case decide.Act:
			return hookOut{Context: bi(
				fmt.Sprintf("shikii: this request looks like one to agree on an approach with the user before changing anything (needs-plan p=%.2f%s). Consider EnterPlanMode or a one-line confirmation first", d.P, receipt),
				fmt.Sprintf("shikii: この依頼は、何かを変える前に進め方を人と合わせるべきものに見える(needs-plan p=%.2f%s)。EnterPlanMode か一言の確認を先に", d.P, receiptJA), "\n", l)}
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
	unlock := lockSession(agent, in.SessionID)
	defer unlock()
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
		d, out, err := hookDecide(agent, "command-effect", map[string]string{"command": cut(ti.Command, requestMax), "cwd": in.CWD}, bypass)
		if err != nil {
			return hookOut{}, err
		}
		noteAsked(sess, "command-effect", d)
		return out, nil
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
		d, out, err := hookDecide(agent, "edit-in-scope", map[string]string{"file": file, "request": sess.Request}, bypass)
		if err != nil {
			return hookOut{}, err
		}
		noteAsked(sess, "edit-in-scope", d)
		return out, nil
	}
	return hookOut{}, nil
}

func hookUserPrompt(agent string, in *hookInput, sess *hookSession) (hookOut, error) {
	req := cut(strings.TrimSpace(in.Prompt), requestMax)
	if req == "" || strings.HasPrefix(req, "/") {
		return hookOut{}, nil
	}
	// 短い返事(「ok」「yes go ahead」)は依頼を置き換えず、前の依頼に添える: edit-in-scope が「ok」を依頼として読まないように
	if utf8.RuneCountInString(req) < shortReply && sess.Request != "" {
		sess.Request = cut(sess.Request+" / "+req, requestMax)
	} else {
		sess.Request = req
	}
	inputs := map[string]string{"request": req}
	if sess.Previous != "" {
		inputs["previous"] = sess.Previous // 文脈の段(迷った時だけ使われる)
	}
	var firstErr error
	// 前の依頼の結果の正解は、次の依頼が来た時点のツリーで決める(依頼の境目 = 次の依頼。Stop まで待つと 2 つの依頼が混ざる)
	if err := settlePending(agent, sess, in.CWD); err != nil {
		firstErr = err
	}
	// prompt-kind: 結果の正解のために作業ツリーのハッシュを控える
	if d, _, err := hookDecide(agent, "prompt-kind", inputs, false); err != nil && firstErr == nil {
		firstErr = err
	} else if err == nil && d.Band != decide.Abstain {
		if tree := treeSnapshot(in.CWD); tree != "" {
			sess.Pending = append(sess.Pending, pendingItem{Trace: d.TraceID, Tree: tree, At: time.Now()})
		}
		noteAsked(sess, "prompt-kind", d)
	}
	d, out, err := hookDecide(agent, "needs-plan", inputs, false)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		return hookOut{}, firstErr
	}
	noteAsked(sess, "needs-plan", d)
	return out, firstErr
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
	firstErr := settlePending(agent, sess, in.CWD)
	// 線を引く: hook が書いた正解(prompt-kind の結果)は serve を通らないので、ここで min_n に届いたかを見る(他の spec も、画面を立てていない間に CLI で答えた分を拾う)
	for _, id := range []string{"prompt-kind", "command-effect", "edit-in-scope", "needs-plan"} {
		if err := hookMaybeCalibrate(agent, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	pruneSessions(agent)
	// 答え待ちの要約: このセッションで人の答えを待つことになった判定のうち、まだ答えの無いもの。増えた時だけ言う(毎ターン同じ数を繰り返さない)
	n := unansweredCount(st, sess.Asked)
	if n == 0 || len(sess.Asked) <= sess.Reported { // 新しく答え待ちになった判定が無ければ繰り返さない
		return hookOut{}, firstErr
	}
	sess.Reported = len(sess.Asked)
	l := hookLoc{Lang: hookLang()}
	if serveUp() {
		return hookOut{System: bi(
			fmt.Sprintf("shikii: %d of this session's decisions wait for your answer: %s/", n, hookPageURL),
			fmt.Sprintf("shikii: このセッションの判定 %d 件が答え待ち: %s/", n, hookPageURL), "\n", l)}, firstErr
	}
	start := fmt.Sprintf("cd %s && shikii serve --port 8096 --local %s", agent, hookLocalURL)
	return hookOut{System: bi(
		fmt.Sprintf("shikii: %d of this session's decisions wait for your answer. Start the page: %s", n, start),
		fmt.Sprintf("shikii: このセッションの判定 %d 件が答え待ち。画面を立てる: %s", n, start), "\n", l)}, firstErr
}

// hookMaybeCalibrate は serve の自動較正と同じ規則: 今の文言(hash)にまだ線が無いモデルの正解が eval.min_n に届いていれば線を引いて spec に書く。
// 線があるモデルは触らない(引き直しは drift が悪化を言った時)。結果は hook.log に 1 行。
func hookMaybeCalibrate(agent, specID string) error {
	path := filepath.Join(agent, "decisions", specID+".yaml")
	sp, err := spec.Load(path)
	if err != nil {
		return nil // spec が無い(人が消した)なら何もしない
	}
	st := store.Store{Dir: filepath.Join(agent, ".shikii")}
	recs, err := st.Records(specID, 0)
	if err != nil {
		return err
	}
	models := map[string]bool{}
	for _, r := range recs {
		if r.Model != "" && sp.LiveHash(r.SpecHash) {
			models[r.Model] = true
		}
	}
	for model := range models {
		if c := sp.CalibrationFor(model); c != nil && c.SpecHash == sp.Hash() {
			continue
		}
		labels, err := st.Labels(specID, sp.Hash(), model)
		if err != nil || len(labels) < sp.Eval.MinN {
			continue
		}
		var buf bytes.Buffer
		old := stdout
		stdout = &buf
		ef := endpointFlags{local: hookLocalURL, model: model}
		err = func() (err error) {
			defer func() {
				stdout = old
				if r := recover(); r != nil {
					err = fmt.Errorf("calibrate panicked: %v", r)
				}
			}()
			return runCalibrate(path, calibrateOpts{ef: ef, storeDir: st.Dir, labeled: "none", bins: 10, write: true, rung: "minimal"})
		}()
		line := lastLine(buf.String())
		if err != nil {
			line += " ERROR: " + err.Error()
		}
		hookLog(fmt.Sprintf("auto-calibrate %s (%s): %s", specID, model, line))
	}
	return nil
}

// settlePending は控えてある prompt-kind の判定に結果の正解を付ける: 依頼の時のツリーと今のツリーが違えば edit(重み 1)、同じなら !edit(部分、重み 0.7)。
// 依頼の直後(次の依頼が来た時)と Stop で呼ぶ。git の外なら付けずに捨てる(比べる物が無い)。
func settlePending(agent string, sess *hookSession, cwd string) error {
	if len(sess.Pending) == 0 {
		return nil
	}
	st := store.Store{Dir: filepath.Join(agent, ".shikii")}
	now := treeSnapshot(cwd)
	var firstErr error
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
	return firstErr
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

// treeSnapshot は作業ツリーの中身のハッシュ: 仮の index に `git add -A`(.gitignore は尊重、未追跡も入る)して `git write-tree` した tree の id の先頭 16 桁。
// HEAD や index の状態は入れないので、commit / stash / checkout だけでは変わらず、ファイルの中身が変わった時だけ変わる。中身そのものは残らない。git の外なら ""。
func treeSnapshot(cwd string) string {
	if cwd == "" {
		return ""
	}
	run := func(env []string, args ...string) (string, bool) {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), env...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", false
		}
		return out.String(), true
	}
	if _, ok := run(nil, "rev-parse", "--is-inside-work-tree"); !ok {
		return ""
	}
	tmp, err := os.CreateTemp("", "shikii-index-*")
	if err != nil {
		return ""
	}
	tmp.Close()
	os.Remove(tmp.Name()) // 空の index から始める(無いファイル名を渡すと git が作る)
	defer os.Remove(tmp.Name())
	env := []string{"GIT_INDEX_FILE=" + tmp.Name()}
	if _, ok := run(env, "add", "-A", "--", "."); !ok {
		return ""
	}
	tree, ok := run(env, "write-tree")
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(tree)))
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
		fmt.Fprintf(stdout, "2. %s: add 3 hooks (PreToolUse %s, UserPromptSubmit, Stop) running `shikii hook <event>`; a backup of the file as it was goes to settings.json.bak-shikii (once, only if it existed). Hooks that are already there are kept; a second install adds nothing\n", tilde(settings), hookToolsRe)
		fmt.Fprintf(stdout, "   Until a spec has a calibration line the hooks only record (shadow mode); they never deny a tool. Claude Code only (Cursor, OpenCode and Codex have no hook).\n")
		fmt.Fprintf(stdout, "   Needs: the local model on %s (start it with `shikii up`; without it the hooks stay silent and record nothing) and `shikii` on Claude Code's PATH (`which shikii`)\n", hookLocalURL)
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
	if err == nil && strings.TrimSpace(string(b)) != "" { // 空のファイルは {} と同じ
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
	for _, ev := range hookEvents {
		if v, has := hooks[ev.event]; has {
			if _, ok := v.([]any); !ok {
				return false, fmt.Errorf("%s: hooks.%s is not a list (fix it by hand; shikii does not replace what it cannot read)", path, ev.event)
			}
		}
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
	if len(b) > 0 && !fileExists(path+".bak-shikii") && !strings.Contains(string(b), hookCmdPrefix) { // 控えは最初の 1 回だけ、shikii の hook が入っていない版(install が作った settings.json は控えない)
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

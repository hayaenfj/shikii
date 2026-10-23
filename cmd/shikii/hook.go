package main

// shikii hook: Claude Code と Codex の hook として、AI 自身の「考え方」を 2 点で測る。
// 考える前(UserPromptSubmit): この依頼で結局ファイルを変えることになるか(prompt-kind)、着手前に進め方を人と合わせるべきか(needs-plan)。
// 考え終わった後(Stop): 変えたものは頼まれた範囲の中だったか(changes-in-scope)、頼まれたことを終えたか(done-as-asked)。
// ツール呼び出しごとの hook は持たない: コマンド 1 つ 1 つの分類は AI の考え方ではなく、許可の是非は Claude Code の permission が見ている。
//
// 置き場は各プロジェクトの .shikii/(decisions/ wiki/ records/ labels/ sessions/)。共通側は型・道具と画面の入口だけ。モデルは手元の :8080。答え合わせの画面は :8096。
// 判定からの助言は線が引けてから帯に従って出す(hookOutcome)。過去の会話は線が無くても引用できる。
// deny は出さず、ツールも止めない。止めるのは人。
// 失敗(モデルが無い、遅い、書けない)は exit 0 で黙る: hook が作業を止めてはいけない。理由は hook.log に 1 行。

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
	"sync"
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
	hookLocalURL      = "http://127.0.0.1:8080"   // 手元のモデル(init の MCP 設定と同じ)
	hookHostedURL     = "https://api.typesafe.ai" // 標準の TypeSafe Jev。鍵(TYPESAFE_API_KEY / DECISIONS_API_KEY)がある時だけ使う(2026-10-23 の決定)
	hookPageURL       = "http://127.0.0.1:8096"   // 文の中の画面の URL の置き字。出す時にそのプロジェクトの画面の URL(空いているポート)に置き換える(0.8.0)
	hookDecideTimeout = 5 * time.Second
	hookProbeTimeout  = 300 * time.Millisecond
	hookSessionTTL    = 7 * 24 * time.Hour
	hookLockWait      = 2 * time.Second  // 錠を待つ上限
	hookGitTimeout    = 3 * time.Second  // 作業ツリーの状態取り(git)の上限。超えたら無し(正解を付けない)
	hookCalibrateGap  = 10 * time.Minute // 同じ spec の自動較正を続けて起こさない間
	// hookCalibrate は線を引く本体。既定は別プロセス(shikii calibrate --write)を切り離して起こす: 聞き直し(replay)が長くても hook の timeout に掛からない。テストは同プロセスに差し替える
	hookCalibrate = spawnCalibrate
	// hookTune は聞き方の候補を採点する本体(shikii tune。--write は付けない: 採用は人)。同じく別プロセスを切り離す。テストは同プロセスに差し替える
	hookTune      = spawnTune
	hookTuneEvery = 50 // 前回の自動 tune から人の正解(入力ごとに 1 件)がこれだけ増えたら、また回す
	// hookRecalibrateEvery: 線が引けなかった spec は、正解がこれだけ増えるまで自動較正(聞き直しを含む)を繰り返さない
	hookRecalibrateEvery = 20
	// hookSpawn は shikii 自身を切り離した別プロセスで起こす(モデル `up`、画面 `serve`)。人にコマンドを打たせない。テストは記録するだけの物に差し替える
	hookSpawn    = spawnShikii      // 起こした子の pid を返す(分からなければ 0)
	hookStartGap = 10 * time.Minute // 同じ物を起こし続けない間(起こしてから応えるまでの時間、失敗した時の再試行の間)
)

const (
	hookTimeout    = 10      // 秒。モデルの 5 秒(2 つの問いは並行) + 書き込み
	requestMax     = 1500    // request / summary として控える文の長さ(バイト)
	shortReply     = 40      // これより短い依頼は返事とみなし、前の依頼に添える(文字数)
	filesMax       = 30      // Stop の問いに並べる「変わったファイル」の上限(残りは件数だけ)
	listingMax     = 4 << 20 // 控える作業ツリーの一覧の上限(バイト)。超えたら変わったファイルは分からない扱い
	hookLogLineMax = 400     // hook.log の 1 行の上限(バイト)。誤りの文にモデルの応答本文が付いてくることがある
)

// hookSpecs は hook が聞く 4 つ(埋め込みの spec の id)。前 2 つは依頼の直後、後 2 つはターンの終わり。
var hookSpecs = []string{"prompt-kind", "needs-plan", "changes-in-scope", "done-as-asked"}

func agentHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shikii", "agent"), nil
}

func cmdHook(args []string) error {
	const usage = "usage: shikii hook install [--yes] | uninstall [--yes] | user-prompt-submit | stop | subagent-start | plan query | plan submit | variants submit\n  install/uninstall: Claude Code and Codex hooks that measure the agent's own thinking before and after each turn (shown first; --yes does it)\n  the event hooks, plan query/submit and variants submit are run by the agent and read JSON on stdin; not for typing by hand\n"
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
	case "plan":
		return cmdHookPlan(args[1:])
	case "variants":
		return cmdHookVariants(args[1:])
	case "user-prompt-submit", "stop", "subagent-start":
		// hook は作業を止めない: 何があっても exit 0。理由は hook.log に
		if err := runHookEvent(args[0]); err != nil {
			hookLog(err.Error())
		}
		return nil
	case "pre-tool-use":
		// 古い版が settings に残した event。何もせず黙る(次の `shikii hook install --yes` が外す)
		return nil
	}
	return fmt.Errorf("hook: unknown event %q (install | uninstall | user-prompt-submit | stop | subagent-start)", args[0])
}

// ---------------------------------------------------------------- Claude Code / Codex の JSON

// hookInput は Claude Code と Codex が stdin に渡す JSON のうち使う所(鍵は両方同じ)。
type hookInput struct {
	SessionID            string `json:"session_id"`
	AgentID              string `json:"agent_id"`   // 付いていれば人の依頼ではない(サブエージェント等)。依頼の控えを変えない
	AgentType            string `json:"agent_type"` // SubagentStart: サブエージェントの種類(Explore など)
	CWD                  string `json:"cwd"`
	Prompt               string `json:"prompt"`
	StopHookActive       bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// hookOut は hook が返すもの。空なら何も出さない(判断なし)。許可の判定(allow / ask / deny)は出さない。
type hookOut struct {
	Context string // エージェントに渡す文(additionalContext。UserPromptSubmit と SubagentStart)
	System  string // 端末の人に見せる文(systemMessage)
	Block   bool   // Stop を止める(止める検査に引っかかった。0.7.3)。Claude Code は reason を渡して続行させる
	Reason  string // 止める理由(エージェントへ)
}

func (o hookOut) empty() bool { return o == hookOut{} }

// message は記録に残す「出した文」: 端末の文、止める理由、エージェントへの文脈のうち最初にあるもの。
func (o hookOut) message() string {
	for _, s := range []string{o.System, o.Reason, o.Context} {
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
	out := map[string]any{}
	if o.Context != "" && event != "Stop" { // Stop は文脈を返すと会話が続いてしまうので hookSpecificOutput を出さない(系の文だけ)
		out["hookSpecificOutput"] = map[string]any{"hookEventName": event, "additionalContext": o.Context}
	}
	if o.System != "" {
		out["systemMessage"] = o.System
	}
	if o.Block && event == "Stop" && o.Reason != "" { // 止める検査: Claude Code の Stop の decision(Codex は読めなければ無視する)
		out["decision"] = "block"
		out["reason"] = o.Reason
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// ---------------------------------------------------------------- セッションの状態

// hookSession はプロジェクトごとの控え(.shikii/sessions/<id>.json)。transcript は読まない(形が文書化されていない)。
// 依頼の時の作業ツリーの一覧は隣の <id>.files(大きいので JSON には入れない)。
type hookSession struct {
	Request       string           `json:"request,omitempty"`          // 人が最後に送った依頼(UserPromptSubmit で控える)
	RequestText   string           `json:"request_text,omitempty"`     // 画面用の原文。判定の入力とは分けて保持する
	RawPrompt     string           `json:"last_user_prompt,omitempty"` // 結合せず控えた最後の人の発言。引用の出典に使う
	TurnID        string           `json:"turn_id,omitempty"`          // 同じ文の依頼を繰り返しても別のターン
	Scope         string           `json:"scope,omitempty"`            // repo の実パス。会話も採点も線も同じ仕事に限る
	Completed     *completedTurn   `json:"completed,omitempty"`        // この依頼に返答した時の控え。次の人の発言と一組にする
	Memories      []memoryDelivery `json:"memories,omitempty"`         // このセッションに届けた記憶の版(直近16件)
	Previous      string           `json:"previous,omitempty"`         // 直前のアシスタントの発言(Stop で控える)
	Planning      *planningFrame   `json:"planning,omitempty"`         // 現在の依頼について、エージェントが原文付きで整理する文脈
	Pending       []pendingItem    `json:"pending,omitempty"`          // prompt-kind の結果の正解を待つ判定
	Watch         []scopeWatch     `json:"watch,omitempty"`            // changes-in-scope の結果の正解を待つ判定(変えたファイルが戻されるか)
	Asked         []askedItem      `json:"asked,omitempty"`            // このセッションで人の答えを待つことになった判定(ask と抜き取り)
	Reviews       []askedItem      `json:"reviews,omitempty"`          // 会話を受けた仮採点と、その後の抜き打ち確認を待つ判定
	Reported      []string         `json:"reported_traces,omitempty"`  // Stop で答え待ちとして伝えた trace(新顔がある時だけ言う)
	Judged        bool             `json:"judged,omitempty"`           // 今の依頼についてターンの終わりの自己評価(changes-in-scope / done-as-asked)を済ませた(次の依頼で戻る)
	Head          string           `json:"head,omitempty"`             // 依頼の時の HEAD(止める検査がこのターンのコミットを見るため。0.7.3)
	HeadAt        time.Time        `json:"head_at,omitempty"`          // 依頼の時刻。これより前にコミットされたもの(切り替え先の既存のコミット)は見ない
	MemoryNoticed bool             `json:"memory_noticed,omitempty"`   // まだ wiki に無いメモリがあると、このセッションで一度伝えた(0.8.0)
	ReflectAsked  []string         `json:"reflect_asked,omitempty"`    // 依頼前に振り返りを頼んだ会話(作法の検査が Stop で見る。0.8.0)
	At            time.Time        `json:"at"`
}

type pendingItem struct {
	Trace string    `json:"trace"`
	Tree  string    `json:"tree"` // 作業ツリーのハッシュ(中身は入れない)
	At    time.Time `json:"at"`
	// Settled: ターンの終わりに「変わっていない」(!edit、部分)を付けた後も、次の人の依頼まで持ち越す印。裏の作業が後から
	// ファイルを変えたら(通知の後の Stop)、同じ trace に edit(重み 1、新しい答えが勝つ)を付け直せる
	Settled bool `json:"settled,omitempty"`
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
	return filepath.Join(agent, "sessions", id+".json")
}

// sessionFilesPath は依頼の時の作業ツリーの一覧(`git ls-files --stage` の出力)の控え。
func sessionFilesPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".files"
}

// lockSession はセッションの控えの錠(flock)。Claude Code は hook を並列に走らせる(Stop と次の依頼が重なる)ので、
// 読む → 変える → 書く の間を 1 プロセスずつにする。錠は短く持つ(モデルの呼び出しは錠の外)。hookLockWait 待っても取れなければ錠なしで進む(hook は止めない)。
func lockSession(agent, id string) (unlock func()) {
	p := sessionPath(agent, id) + ".lock"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	deadline := time.Now().Add(hookLockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			hookLog(fmt.Sprintf("session %s: lock busy for %s; proceeding without it", id, hookLockWait))
			f.Close()
			return func() {}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// updateSession は錠の中で 控えを読む → fn で変える → 書く。並列の hook が互いの変更を消さないための唯一の書き口。
func updateSession(agent, id string, fn func(s *hookSession)) error {
	unlock := lockSession(agent, id)
	defer unlock()
	s := loadSession(agent, id)
	fn(s)
	return saveSession(agent, id, s)
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
	b, _ := json.MarshalIndent(s, "", "  ")
	return writeAtomic(sessionPath(agent, id), b)
}

func writeAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
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

// pruneSessions は古いセッションの控え(.json と .files)を消す(7 日)。
func pruneSessions(agent string) {
	dir := filepath.Join(agent, "sessions")
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

// hookEndpoints は hook が送る口。標準は TypeSafe の Jev(鍵がある時)、次に手元のモデル。spec の send_to の順で最初に送れる口が使われる。
// 鍵があっても SHIKII_MODEL=local なら手元だけ(modelChoice)。
func hookEndpoints() []decide.Endpoint {
	var eps []decide.Endpoint
	if key := hostedKey(); key != "" && modelChoice() != "local" {
		eps = append(eps, decide.Endpoint{Kind: "hosted", Client: systemone.New(hookHostedURL, key, "jev-latest")})
	}
	if modelChoice() != "hosted" {
		eps = append(eps, decide.Endpoint{Kind: "local", Client: systemone.New(hookLocalURL, "", "jev-latest")})
	}
	return eps
}

// usesHosted は hook が Jev を使うか(鍵があり、SHIKII_MODEL=local でない)。
func usesHosted() bool {
	return hostedKey() != "" && modelChoice() != "local"
}

// hookDecide はプロジェクトの spec で判定し、帯を振る舞いに写し、記録する。線が無ければ band は act にならない(= 影の期間)。
// 設定の言語が英語以外なら、出す文は英語と併記し、記録にも display(言語、質問、答え、出した文)を添える。
func hookDecide(agent, specID string, inputs map[string]string, project string, originals ...map[string]string) (*decide.Decision, hookOut, error) {
	sp, err := spec.Load(filepath.Join(agent, "decisions", specID+".yaml"))
	if err != nil {
		return nil, hookOut{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookDecideTimeout)
	defer cancel()
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{}, hookEndpoints())
	if err != nil {
		return nil, hookOut{}, err
	}
	st := store.Store{Dir: agent}
	attachReceipt(sp, st, d)
	l := localize(sp, d, hookLang())
	out := hookOutcome(specID, d, l)
	page := pageBase(filepath.Dir(agent))
	out.Context = strings.ReplaceAll(out.Context, hookPageURL+"/", page+"/")
	out.System = strings.ReplaceAll(out.System, hookPageURL+"/", page+"/")
	if d.Band != decide.Abstain {
		rec := recordOf(sp, d, inputs)
		rec.ReviewPending = store.ReviewSpec(specID)
		rec.Project = project                                                         // どの仕事の判定か(画面と status で分けて見る印。入力ではないのでハッシュは変わらない)
		if l.Lang != "" && (l.Question != "" || l.Top != "" || out.message() != "") { // 言語だけの display は付けない(読む物が無い)
			rec.Display = &store.RecordDisplay{Lang: l.Lang, Question: l.Question, Top: l.Top, Message: out.message()}
		}
		if err := recordDecisionRec(st, sp, d, rec); err != nil {
			return nil, hookOut{}, err
		}
		for _, text := range originals {
			if err := st.SaveRecordText(rec, text); err != nil {
				return d, out, err
			}
		}
	}
	return d, out, nil
}

// hookDecideAll は同じ入力で複数の spec を並行して聞く(1 つの hook の中で順に待つと timeout に近づく)。
// 返す順は specIDs の順。聞かなかった(skip)spec は nil。
func hookDecideAll(agent string, specIDs []string, inputs map[string]string, project string, skip func(id string) bool, originals ...map[string]string) ([]*decide.Decision, []hookOut, error) {
	ds := make([]*decide.Decision, len(specIDs))
	outs := make([]hookOut, len(specIDs))
	errs := make([]error, len(specIDs))
	var wg sync.WaitGroup
	for i, id := range specIDs {
		if skip != nil && skip(id) {
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			ds[i], outs[i], errs[i] = hookDecide(agent, id, inputs, project, originals...)
		}(i, id)
	}
	wg.Wait()
	var firstErr error
	for _, e := range errs {
		if e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return ds, outs, firstErr
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
	id := d.Top
	if sp.Type == spec.Noul && id == "yes" && d.P < 0.5 { // noul の判定は Top が常に yes で P が p(yes): 人の言葉では p<0.5 は「いいえ」側
		id = "no"
	}
	if disp, ok := sp.Display[lang]; ok {
		l.Question = strings.TrimSpace(disp.Question)
		l.Top = disp.Options[id]
	}
	if l.Top == "" && lang == "ja" {
		l.Top = map[string]string{"yes": "はい", "no": "いいえ"}[id]
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

// bi は英語の文に、設定の言語の文を併記する(ja だけ文の型を持つ。他の言語は英語のまま)。
func bi(en, ja string, l hookLoc) string {
	if l.Lang != "ja" || ja == "" {
		return en
	}
	return en + "\n" + ja
}

func answerURL(specID, trace string) string {
	return fmt.Sprintf("%s/?spec=%s&trace=%s", hookPageURL, specID, trace)
}

// hookOutcome は帯を振る舞いに写す純関数。線が無ければ(影)何も出さない。許可の判定は出さない(ツールを止めない)。
// 依頼の直後の問い(needs-plan)はエージェントへの文脈に、ターンの終わりの問い(changes-in-scope / done-as-asked)は端末の人への文になる
// (Stop で文脈を返すと会話が続いてしまう。続けるかどうかは人が決める)。
func hookOutcome(specID string, d *decide.Decision, l hookLoc) hookOut {
	if d == nil || !d.IsCalibrated || d.Band == decide.Abstain {
		return hookOut{}
	}
	url := answerURL(specID, d.TraceID)
	unsure := func() hookOut {
		return hookOut{System: bi(
			fmt.Sprintf("shikii: unsure (%s %s p=%.2f). Answer later: %s", specID, d.Top, d.P, url),
			fmt.Sprintf("shikii: 迷っている(%s「%s」p=%.2f)。あとで答える: %s", specID, l.topName(d.Top), d.P, url), l)}
	}
	receipt, receiptJA := "", ""
	if r := d.Receipt; r != nil {
		receipt = fmt.Sprintf("; line on %d truths, human-checked precision %.2f", r.N, r.HumanPrecision)
		receiptJA = fmt.Sprintf("。線は正解 %d 件、人が確かめた精度 %.2f", r.N, r.HumanPrecision)
	}
	switch specID {
	case "needs-plan":
		switch d.Band {
		case decide.Act:
			return hookOut{Context: bi(
				fmt.Sprintf("shikii: this request looks like one to agree on an approach with the user before changing anything (needs-plan p=%.2f%s). Consider EnterPlanMode or a one-line confirmation first", d.P, receipt),
				fmt.Sprintf("shikii: この依頼は、何かを変える前に進め方を人と合わせるべきものに見える(needs-plan p=%.2f%s)。EnterPlanMode か一言の確認を先に", d.P, receiptJA), l)}
		case decide.Ask:
			return unsure()
		}
	case "changes-in-scope":
		switch d.Band {
		case decide.Discard: // 確信して「範囲の外」
			return hookOut{System: bi(
				fmt.Sprintf("shikii: this turn's changes look outside what you asked for (changes-in-scope p(yes)=%.2f%s). Check the changed files before going on", d.P, receipt),
				fmt.Sprintf("shikii: このターンで変えたものは頼まれた範囲の外に見える(changes-in-scope p(はい)=%.2f%s)。続ける前に変わったファイルを確かめる", d.P, receiptJA), l)}
		case decide.Ask:
			return hookOut{} // 会話を受けた仮採点へ。人は抜き取りと保留を確認する
		}
	case "done-as-asked":
		switch d.Band {
		case decide.Discard: // 確信して「終えていない」
			return hookOut{System: bi(
				fmt.Sprintf("shikii: this turn looks unfinished against what you asked (done-as-asked p(yes)=%.2f%s). Something may be left to ask for again", d.P, receipt),
				fmt.Sprintf("shikii: このターンは頼んだことを終えていないように見える(done-as-asked p(はい)=%.2f%s)。言い直すことが残っているかもしれない", d.P, receiptJA), l)}
		case decide.Ask:
			return hookOut{}
		}
	}
	return hookOut{} // prompt-kind と、確信して「問題なし」側: 記録だけ
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
		return fmt.Errorf("stdin is not the agent's hook JSON: %w", err)
	}
	if event == "stop" && in.StopHookActive {
		// 止めた後の Stop: もう止めない(無限ループを避ける)。止める検査だけもう一度当てて、直ったかを記録する(0.7.3)
		if root, _, perr := prepareHookProject(in.CWD); perr == nil {
			guardRecheck(root, &in)
		}
		return nil
	}
	common := agent
	agent, _, err = prepareHookProject(in.CWD)
	if errors.Is(err, errProjectInactive) {
		return nil // 初期化していない場所では保存・Gitの変更・モデルの起動をしない。
	}
	if err != nil {
		return err
	}
	ensureModel(common) // 無ければ裏で起こす(この turn は記録が付かないだけ。待たない)
	// モデルの呼び出しは錠の外(控えの写しを読むだけ)。控えへの変更は muts にまとめ、錠の中で読み直した控えに当てる
	snapshot := loadSession(agent, in.SessionID)
	var out hookOut
	var muts []func(*hookSession)
	var summary string
	switch event {
	case "user-prompt-submit":
		out, muts, err = hookUserPrompt(agent, &in, snapshot)
	case "stop":
		out, muts, err = hookStop(agent, &in, snapshot)
		muts = append(muts, func(s *hookSession) { summary = stopSummary(agent, s) }) // 答え待ちの要約は、このターンの判定を控えに足した後の控えで数える
	case "subagent-start":
		out, err = hookSubagentStart(agent, &in, snapshot) // 親の控えは変えない(muts 無し)
	}
	if len(muts) > 0 {
		if serr := updateSession(agent, in.SessionID, func(sess *hookSession) {
			for _, m := range muts {
				m(sess)
			}
		}); serr != nil && err == nil {
			err = serr
		}
	}
	if event == "user-prompt-submit" && len(muts) > 0 {
		// 人の依頼が進んだ時だけ。通知は同じ TurnID なので wiki の仕事を増やさない。
		current := loadSession(agent, in.SessionID)
		if current.TurnID != snapshot.TurnID {
			// 同じセッションへ届ける記憶は、書き戻しと一緒に重複を除く。
			if merr := updateSession(agent, in.SessionID, func(s *hookSession) {
				if s.TurnID != current.TurnID || s.Scope != current.Scope {
					return
				}
				context, merr := memoryContext(agent, s.Scope, cut(in.Prompt, requestMax), s)
				if merr != nil {
					hookLog("memory: " + merr.Error())
				} else {
					out.Context = joinLines(out.Context, context)
				}
			}); merr != nil {
				hookLog("memory: " + merr.Error())
			}
			if context, ids, rerr := reflectContext(agent, current.Scope); rerr != nil {
				hookLog("reflect: " + rerr.Error())
			} else {
				out.Context = joinLines(out.Context, context)
				// 頼んだ会話を控えに書く(作法の検査が Stop で見る: 振り返りが済んだか)。頼まなかったターンは空にする
				if uerr := updateSession(agent, in.SessionID, func(s *hookSession) {
					if s.TurnID == current.TurnID {
						s.ReflectAsked = ids
					}
				}); uerr != nil {
					hookLog("reflect: " + uerr.Error())
				}
			}
			out.Context = joinLines(out.Context, variantsContext(agent))
		}
	}
	if event == "stop" {
		if line := wordingSummary(agent); line != "" {
			ensurePage(agent)
			summary = joinLines(summary, line)
		}
	}
	if summary != "" {
		out.System = joinLines(out.System, summary)
	}
	if b := out.json(hookEventName(event)); b != nil {
		fmt.Fprintln(stdout, string(b))
	}
	if event == "stop" {
		// 線を引くのと聞き方を試すのは錠の外で別プロセス(カーパシーの wiki の lint に当たる、ゆっくりした周期の仕事)
		for _, id := range hookSpecs {
			if cerr := hookMaybeCalibrate(agent, id); cerr != nil && err == nil {
				err = cerr
			}
			if terr := hookMaybeTune(agent, id); terr != nil && err == nil {
				err = terr
			}
		}
		for _, id := range wikiSpecs { // wiki の差し込み先・矛盾も、結果の正解が貯まれば同じ周期で線を引く
			if cerr := hookMaybeCalibrate(agent, id); cerr != nil && err == nil {
				err = cerr
			}
		}
		if werr := notebookMaintenance(agent, snapshot.Scope); werr != nil {
			hookLog("wiki lint: " + werr.Error())
		}
	}
	return err
}

func joinLines(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

func hookEventName(event string) string {
	switch event {
	case "user-prompt-submit":
		return "UserPromptSubmit"
	case "subagent-start":
		return "SubagentStart"
	}
	return "Stop"
}

// isNotification は、Claude Code が裏の作業(サブエージェント・背景のコマンド)の完了を「ユーザーの発言」として流す
// <task-notification> / [SYSTEM NOTIFICATION - NOT USER INPUT] の入力か(人の依頼ではない。実測で記録の半分近くがこれだった)。
func isNotification(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if strings.HasPrefix(p, "<task-notification>") || strings.HasPrefix(p, "<system-reminder>") {
		return true
	}
	head := p
	if len(head) > 300 {
		head = head[:300]
	}
	return strings.Contains(head, "[SYSTEM NOTIFICATION - NOT USER INPUT]")
}

// hookSubagentStart は、サブエージェント(Claude Code の Agent ツール)が始まる時に、親の依頼に近い教訓を同じ形で届ける。
// サブエージェントの依頼文は UserPromptSubmit に来ないので、0.7.2 まではここに何も届いていなかった(2026-10-23 の実験で判明)。
// 親のセッションの控え(依頼・届けた記憶の版)は変えず、配送は親とは別の ID(ターンとサブエージェントのハッシュ)で記録する。
// 親の依頼がまだ無ければ何も出さない。判定(2 問)はしない。
func hookSubagentStart(agent string, in *hookInput, snapshot *hookSession) (hookOut, error) {
	if snapshot.Request == "" || snapshot.Scope == "" || snapshot.TurnID == "" {
		return hookOut{}, nil
	}
	sub := hookSession{Scope: snapshot.Scope} // 届けた版の控えは持たない: 親が受け取っていてもサブエージェントには届ける
	context, err := memoryContextFor(agent, snapshot.Scope, snapshot.Request, &sub, snapshot.TurnID, in.AgentID)
	if err != nil || context == "" {
		return hookOut{}, err
	}
	kind := in.AgentType
	if kind == "" {
		kind = "subagent"
	}
	head := "shikii: このサブエージェント(" + kind + ")に、親の依頼「" + cut(snapshot.Request, 80) + "」に近い記憶を届けます。"
	return hookOut{Context: joinLines(head, context)}, nil
}

// hookUserPrompt は考える前の 2 つ(prompt-kind と needs-plan)を聞き、依頼と作業ツリーの状態を控える。前の依頼の結果の正解はここ(次の依頼が来た時)で決める。
func hookUserPrompt(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	req := cut(strings.TrimSpace(in.Prompt), requestMax)
	if req == "" || strings.HasPrefix(req, "/") || in.AgentID != "" {
		return hookOut{}, nil, nil
	}
	if isNotification(req) {
		// 裏の作業の完了通知は依頼ではない: 2 問は聞かず、依頼・一覧・pending も置き換えない(結果の正解は次の人の依頼で決める)。
		// 通知の後に作業が続いたら、そのターンの終わりで元の依頼に対してもう一度だけ判定し直す(Judged を戻す)
		if snapshot.Request == "" {
			return hookOut{}, nil, nil
		}
		return hookOut{}, []func(*hookSession){func(s *hookSession) {
			if s.Request == snapshot.Request {
				s.Judged = false
			}
		}}, nil
	}
	// 短い返事(「ok」「yes go ahead」)は依頼を置き換えず、前の依頼に添える: ターンの終わりの問いが「ok」を依頼として読まないように
	request := req
	if utf8.RuneCountInString(req) < shortReply && snapshot.Request != "" {
		request = cut(snapshot.Request+" / "+req, requestMax)
	}
	inputs := map[string]string{"request": req}
	if snapshot.Previous != "" {
		inputs["previous"] = snapshot.Previous // 文脈の段(迷った時だけ使われる)
	}
	tree, listing, listed, project, scope := treeStateWithScope(in.CWD) // 今のツリー: 前の依頼の結果を決める「今」でもあり、この依頼の控えでもある(1 回で両方)
	pageURL, pageStarted := ensureProjectPage(scope)                    // エージェントが動き出したら(最初の依頼)このプロジェクトの画面を 1 つ立て、ブラウザで開く(0.8.0)
	head := gitHead(in.CWD)                                             // このターンのコミットを止める検査が見るための起点
	requestText := strings.TrimSpace(in.Prompt)
	if utf8.RuneCountInString(req) < shortReply && snapshot.Request != "" && snapshot.Scope == scope {
		previous := snapshot.RequestText
		if previous == "" {
			previous = snapshot.Request
		}
		requestText = previous + " / " + requestText
	}
	if !strings.HasPrefix(requestText, request) {
		requestText = ""
	}
	requestText = cut(requestText, store.RecordTextLimit+utf8.UTFMax)
	turnID := store.NewTraceID()
	var planning *planningFrame
	if sp, err := spec.Load(filepath.Join(agent, "decisions", "needs-plan.yaml")); err == nil && needsPlanningContext(sp) {
		planning = newPlanningFrame(snapshot, in, scope, turnID)
	}
	var firstErr error
	pendingNow := snapshot.Pending // 前の依頼の pending(写し)。錠の中で settle するのはこの trace だけ(重なった Stop や次の依頼が足した pending を巻き込まない)
	muts := []func(*hookSession){
		func(s *hookSession) {
			if ferr := observeFeedback(agent, s, snapshot, scope, req); ferr != nil {
				hookLog("feedback: " + ferr.Error())
			}
			if derr := settleDone(agent, linkedCompletion(s, snapshot, scope), req); derr != nil {
				hookLog("outcome: " + derr.Error())
			}
			s.Request, s.Judged, s.TurnID, s.Scope, s.Completed = request, false, turnID, scope, nil
			s.Head, s.HeadAt = head, time.Now()
			s.RequestText = requestText
			s.RawPrompt = cut(strings.TrimSpace(in.Prompt), 4000)
			s.Planning = planning
		},
		// 前の依頼の結果の正解は、次の依頼が来た時点のツリーで決める(依頼の境目 = 次の依頼。Stop まで待つと 2 つの依頼が混ざる)
		func(s *hookSession) {
			if err := settlePending(agent, s, tree, pendingNow, true); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := settleScope(agent, s, listing, listed, true); err != nil && firstErr == nil {
				firstErr = err
			}
		},
		func(s *hookSession) {
			p := sessionFilesPath(agent, in.SessionID)
			if !listed {
				_ = os.Remove(p) // git の外(か間に合わない、か大きすぎる): 前の依頼の一覧を残して比べてしまわない
				return
			}
			// 先頭に「どの依頼の一覧か」の印: Stop は自分が見た依頼の一覧だけと比べる(次の依頼が書き直した一覧と比べない)
			if err := writeAtomic(p, []byte(listingHeader(request)+listing)); err != nil && firstErr == nil {
				firstErr = err
			}
		},
	}
	ds, outs, err := hookDecideAll(agent, hookSpecs[:2], inputs, project, func(id string) bool { return id == "needs-plan" && planning != nil }, map[string]string{"request": strings.TrimSpace(in.Prompt)})
	if err != nil && firstErr == nil {
		firstErr = err
	}
	if planning != nil && planningNotNeeded(ds[0]) {
		planning = nil // 線のある prompt-kind が確信して「ファイルを変えない」: 着手前の整理を頼まない(控えへは最初の mut が nil を書く)
	}
	var out hookOut
	if planning != nil {
		out.Context = planningNotice(in.SessionID, turnID)
	}
	for i, d := range ds {
		if d == nil || d.Band == decide.Abstain {
			continue
		}
		if hookSpecs[i] == "prompt-kind" && tree != "" { // 結果の正解のために作業ツリーのハッシュを控える
			muts = append(muts, func(s *hookSession) {
				s.Pending = append(s.Pending, pendingItem{Trace: d.TraceID, Tree: tree, At: time.Now()})
			})
		}
		muts = append(muts, noteAsked(hookSpecs[i], d))
		out.Context = joinLines(out.Context, outs[i].Context)
		out.System = joinLines(out.System, outs[i].System)
	}
	if pageStarted {
		out.Context = joinLines(out.Context, "shikii: このプロジェクトの画面を起こし、ブラウザで開きました: "+pageURL+"/")
	}
	// 言われなくても気付く: このプロジェクトのエージェントのメモリに wiki へ移していないファイルがあれば伝える(0.8.0)。セッションに 1 回
	if !snapshot.MemoryNoticed {
		if notice := memoryImportNotice(scope); notice != "" {
			out.Context = joinLines(out.Context, notice)
			muts = append(muts, func(s *hookSession) { s.MemoryNoticed = true })
		}
	}
	return out, muts, firstErr
}

// planningNotNeeded は、着手前の整理を省いてよいか: prompt-kind に線があり、確信して(act)説明・調査・実行(ファイルを変えない)と答えた時だけ。
// 提案への返事・選択(decide)は合意が要になるので省かない。線が無ければ省かない(省く根拠が測られていない)。
func planningNotNeeded(d *decide.Decision) bool {
	if d == nil || !d.IsCalibrated || d.Band != decide.Act {
		return false
	}
	switch d.Top {
	case "explain", "investigate", "run":
		return true
	}
	return false
}

// hookStop は考え終わった後の 2 つ(changes-in-scope と done-as-asked)を、依頼・変わったファイルの一覧・最後の返答を揃えて聞く。
// 同じ依頼について 1 回だけ(Judged)。依頼の控えが無ければ聞かない。何も変えていなければ changes-in-scope は聞かない。
// ほかに、直前のアシスタントの発言を控え、残った pending に結果の正解を付ける。答え待ちの要約は呼ぶ側が錠の中で足す(stopSummary)。
func hookStop(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	tree, listing, listed, project, scope := treeStateWithScope(in.CWD)
	last := cut(strings.TrimSpace(in.LastAssistantMessage), requestMax)
	req := snapshot.Request // この Stop が評価する依頼。錠の中で控えの依頼が変わっていたら(次の依頼が先に来た)、この Stop の印は当てない
	pendingNow := snapshot.Pending
	var firstErr error
	muts := []func(*hookSession){
		func(s *hookSession) {
			if last != "" && s.Request == req && s.TurnID == snapshot.TurnID {
				s.Previous = last
				if s.TurnID != "" && scope != "" && s.Scope == scope {
					var targets []store.ReviewTarget
					if s.Completed != nil && s.Completed.TurnID == s.TurnID {
						targets = s.Completed.Targets
					}
					s.Completed = &completedTurn{TurnID: s.TurnID, Request: req, Summary: last, At: time.Now(), Targets: targets}
				}
			}
		},
		func(s *hookSession) {
			if err := settlePending(agent, s, tree, pendingNow, false); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := settleScope(agent, s, listing, listed, false); err != nil && firstErr == nil {
				firstErr = err
			}
		},
	}
	pruneSessions(agent)
	if req == "" {
		return hookOut{}, muts, firstErr
	}
	var changed []string
	known := false
	var before []byte
	if listed {
		var err error
		if before, err = os.ReadFile(sessionFilesPath(agent, in.SessionID)); err == nil {
			if !strings.HasPrefix(string(before), listingHeader(req)) {
				return hookOut{}, muts, firstErr // 一覧は既に次の依頼のもの: この Stop は比べる物を失ったので評価しない(次の依頼の Stop が評価する)
			}
			changed, known = changedFiles(string(before), listing), true
		}
	}
	// 止める検査(0.7.3): 有効な guard を変わったファイル・足した行・このターンのコミットに当てる。引っかかれば Stop を止めて理由を返す
	out, gerr := guardStop(agent, in, snapshot, string(before), changed, known, false)
	if gerr != nil && firstErr == nil {
		firstErr = gerr
	}
	if snapshot.Judged || out.Block {
		return out, muts, firstErr // 止めたターンは直す前の変更と返答で判定しない(次の依頼で、直した後のツリーと返答を見る)
	}
	inputs := map[string]string{"request": snapshot.Request, "files": filesInput(changed, known), "summary": last}
	if inputs["summary"] == "" {
		inputs["summary"] = "(none)"
	}
	originals := map[string]string{"summary": strings.TrimSpace(in.LastAssistantMessage)}
	if snapshot.RequestText != "" {
		originals["request"] = snapshot.RequestText
	}
	ds, outs, err := hookDecideAll(agent, hookSpecs[2:], inputs, project, func(id string) bool {
		return id == "changes-in-scope" && (!known || len(changed) == 0) // 何も変えていない(か分からない)なら「範囲か」は問えない
	}, originals)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	targets := []store.ReviewTarget{}
	for i, d := range ds {
		if d == nil || d.Band == decide.Abstain {
			continue
		}
		target := askedItem{Spec: hookSpecs[2+i], Trace: d.TraceID}
		var watch *scopeWatch
		if target.Spec == "changes-in-scope" { // 変えたファイルが次の依頼の間に戻されるかを見張る(結果の正解)
			w := newScopeWatch(d.TraceID, string(before), changed)
			watch = &w
		}
		muts = append(muts, func(s *hookSession) {
			if s.Request == req && s.TurnID == snapshot.TurnID {
				s.Reviews = append(s.Reviews, target)
				if watch != nil {
					s.Watch = append(s.Watch, *watch)
				}
			}
		})
		targets = append(targets, store.ReviewTarget{Spec: hookSpecs[2+i], TraceID: d.TraceID})
		out.System = joinLines(out.System, outs[i].System)
	}
	muts = append(muts, func(s *hookSession) {
		if s.Request == req && s.TurnID == snapshot.TurnID {
			s.Judged = true
			if s.Completed != nil && s.Completed.TurnID == s.TurnID {
				s.Completed.Targets = targets
			}
		}
	})
	// 作法の検査(0.8.0): 判定を記録した上で、頼んだ計画・振り返りが済んでいなければ一度だけ止める
	if rout, rerr := ritualStop(agent, snapshot.Scope, in, snapshot, changed, known, false); rerr != nil && firstErr == nil {
		firstErr = rerr
	} else if rout.Block {
		out.Block, out.Reason = true, joinLines(out.Reason, rout.Reason)
	}
	return out, muts, firstErr
}

// listingHeader は作業ツリーの一覧の先頭行: どの依頼の時の一覧か(依頼のハッシュ)。changedFiles はタブの無い行を読まないので比較には入らない。
func listingHeader(request string) string {
	sum := sha256.Sum256([]byte(request))
	return "#request " + hex.EncodeToString(sum[:])[:16] + "\n"
}

// stopSummary は答え待ちの要約(錠の中、このターンの判定を足した後): このセッションで人の答えを待つことになった判定のうち、まだ答えの無いもの。
// 新顔がある時だけ 1 行(毎ターン同じ数を繰り返さない)。画面が立っていなければ裏で起こし、行は常に URL。
func stopSummary(agent string, sess *hookSession) string {
	st := store.Store{Dir: agent}
	waiting := unanswered(st, append(append([]askedItem{}, sess.Asked...), sess.Reviews...))
	n := len(waiting)
	if n == 0 || !hasNew(waiting, sess.Reported) {
		return ""
	}
	sess.Reported = waiting
	l := hookLoc{Lang: hookLang()}
	page := pageBase(filepath.Dir(agent)) // 立っていなければ裏で起こす。人に起動コマンドを打たせない
	return bi(
		fmt.Sprintf("shikii: %d of this session's decisions wait for your answer: %s/", n, page),
		fmt.Sprintf("shikii: このセッションの判定 %d 件が答え待ち: %s/", n, page), l)
}

// changedFiles は 2 つの一覧(`git ls-files --stage`: "mode sha stage\tpath")を比べ、中身か mode が変わった・増えた・消えたファイルの path を名前順で返す。
func changedFiles(before, after string) []string {
	a, b := parseListing(before), parseListing(after)
	seen := map[string]bool{}
	var out []string
	for p, sha := range a {
		if b[p] != sha {
			out = append(out, p)
			seen[p] = true
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok && !seen[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// filesInput は変わったファイルの一覧を問いの入力にする(最大 filesMax 件、残りは件数)。分からなければ "(unknown)"、無ければ "(none)"。
func filesInput(changed []string, known bool) string {
	switch {
	case !known:
		return "(unknown: no git listing for this turn)"
	case len(changed) == 0:
		return "(none)"
	}
	if len(changed) <= filesMax {
		return strings.Join(changed, "\n")
	}
	return strings.Join(changed[:filesMax], "\n") + fmt.Sprintf("\n... and %d more", len(changed)-filesMax)
}

// hookMaybeCalibrate は serve の自動較正と同じ規則: 今の文言(hash)にまだ線が無いモデルについて、人の正解(入力ごとに 1 件)が eval.min_n に届いていれば線を引く。
// 線があるモデルは触らない(引き直しは drift が悪化を言った時)。本体は hookCalibrate(既定は別プロセスを切り離す)。同じ spec は hookCalibrateGap の間は 1 回。
func hookMaybeCalibrate(agent, specID string) error {
	path := filepath.Join(agent, "decisions", specID+".yaml")
	sp, err := spec.Load(path)
	if err != nil {
		return nil // spec が無い(人が消した)なら何もしない
	}
	st := store.Store{Dir: agent}
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
		if err != nil {
			continue
		}
		var human []store.Label
		for _, l := range store.LatestPerInput(labels) { // calibrate と同じ数え方: 入力ごとに 1 件、人の正解だけ
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		if len(human) < sp.Eval.MinN {
			continue
		}
		guard := filepath.Join(st.Dir, "calibrating-"+specID)
		if info, err := os.Stat(guard); err == nil && time.Since(info.ModTime()) < hookCalibrateGap {
			continue // 前の較正がまだ走っている(か、直前に失敗した)。しばらく置く
		}
		if last, ok := lastCalibrateAttempt(guard, sp.Hash(), model); ok && len(human) < last+hookRecalibrateEvery {
			continue // 同じ文言とモデルで線が引けなかった: 正解が hookRecalibrateEvery 件増えるまで聞き直しをしない
		}
		_ = os.WriteFile(guard, []byte(fmt.Sprintf("%s %s %d %s\n", time.Now().Format(time.RFC3339), model, len(human), sp.Hash())), 0o600)
		hookCalibrate(agent, path, st.Dir, model)
	}
	return nil
}

// lastCalibrateAttempt は前回の自動較正の控え(時刻 モデル 正解の数 文言のハッシュ)から、同じ文言とモデルで試した時の正解の数を返す。
// 線が引けていればそもそも呼ばれないので、控えがあるのは「試したが線が引けなかった」か「まだ走っている」。古い形の控えは無いものとする。
func lastCalibrateAttempt(guard, hash, model string) (int, bool) {
	b, err := os.ReadFile(guard)
	if err != nil {
		return 0, false
	}
	var at, m, h string
	var n int
	if _, err := fmt.Sscanf(string(b), "%s %s %d %s", &at, &m, &n, &h); err != nil || m != model || h != hash {
		return 0, false
	}
	return n, true
}

// humanTruths は今の文言(hash)に付いた人の正解の数(入力ごとに 1 件、モデルをまたいで数える)。calibrate / tune と同じ数え方。
func humanTruthsFor(st store.Store, sp *spec.Spec) (int, error) {
	recs, err := st.Records(sp.ID, 0)
	if err != nil {
		return 0, err
	}
	models := map[string]bool{}
	for _, r := range recs {
		if r.Model != "" && sp.LiveHash(r.SpecHash) {
			models[r.Model] = true
		}
	}
	var all []store.Label
	for model := range models {
		labels, err := st.Labels(sp.ID, sp.Hash(), model)
		if err != nil {
			return 0, err
		}
		all = append(all, labels...)
	}
	n := 0
	for _, l := range store.LatestPerInput(all) {
		if store.HumanSource(l.Source) {
			n++
		}
	}
	return n, nil
}

// hookMaybeTune は聞き方の候補を機械に試させる(wiki の lint に当たるゆっくりした周期): spec に variants(人が書いた候補の言い方)があり、
// 人の正解が eval.min_n 以上で、前回の自動 tune から hookTuneEvery 件増えていれば、`shikii tune`(--write 無し)を切り離して起こす。
// 候補が無ければ試す物が無いので何もしない。採用は人が意味を確かめてから(画面か tune --write)。前回の数は tuned-<spec> に控える。
func hookMaybeTune(agent, specID string) error {
	path := filepath.Join(agent, "decisions", specID+".yaml")
	sp, err := spec.Load(path)
	if err != nil || len(sp.Variants) == 0 {
		return nil
	}
	st := store.Store{Dir: agent}
	n, err := humanTruthsFor(st, sp)
	if err != nil {
		return err
	}
	if n < sp.Eval.MinN {
		return nil
	}
	guard := filepath.Join(agent, "tuned-"+specID)
	last := -1
	if b, err := os.ReadFile(guard); err == nil {
		var cnt int
		var hash string
		if _, err := fmt.Sscanf(string(b), "%d %s", &cnt, &hash); err == nil && hash == sp.Hash() {
			last = cnt // 同じ文言の時だけ前回の数を引き継ぐ(文言を変えたら新しい spec として数え直す)
		}
	}
	if last >= 0 && n-last < hookTuneEvery {
		return nil
	}
	if err := writeTuneGuard(agent, sp, n); err != nil {
		return err
	}
	hookTune(agent, path, st.Dir)
	return nil
}

// spawnTune は `shikii tune <spec> --store … --local …` を切り離した別プロセスで起こす(hook は待たない。--write は付けない)。出力は hook.log に追記。
func spawnTune(agent, path, storeDir string) {
	exe, err := os.Executable()
	if err != nil {
		hookLog("auto-tune: cannot find shikii: " + err.Error())
		return
	}
	logf, err := os.OpenFile(filepath.Join(agent, "hook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s auto-tune %s: started in the background (adoption is a person's call: the page, or tune --write)\n", time.Now().Format(time.RFC3339), strings.TrimSuffix(filepath.Base(path), ".yaml"))
	cmd := exec.Command(exe, "tune", path, "--store", storeDir, "--local", hookLocalURL)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logf, "%s auto-tune: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}
	_ = cmd.Process.Release()
}

// spawnCalibrate は `shikii calibrate <spec> --store … --local … --model … --write` を切り離した別プロセスで起こす(hook は待たない)。
// 出力は hook.log に追記。聞き直しが長くても hook の timeout に掛からず、途中で殺されても spec は tmp + rename で壊れない。
func spawnCalibrate(agent, path, storeDir, model string) {
	exe, err := os.Executable()
	if err != nil {
		hookLog("auto-calibrate: cannot find shikii: " + err.Error())
		return
	}
	logf, err := os.OpenFile(filepath.Join(agent, "hook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s auto-calibrate %s (%s): started in the background\n", time.Now().Format(time.RFC3339), filepath.Base(path), model)
	cmd := exec.Command(exe, "calibrate", path, "--store", storeDir, "--local", hookLocalURL, "--model", model, "--write")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logf, "%s auto-calibrate: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}
	_ = cmd.Process.Release()
}

// settlePending は控えてある prompt-kind の判定に結果の正解を付ける: 依頼の時のツリーと今のツリー(now)が違えば edit(重み 1)、同じなら !edit(部分、重み 0.7)。
// 依頼の直後(次の依頼が来た時)と Stop で呼ぶ。now が ""(git の外、または git が間に合わない)なら付けずに捨てる(比べる物が無い)。
// only は呼ぶ側が錠の外で見た pending: 錠の中でもまだ残っているものだけ付ける(重なった hook が後から足した pending は、その hook の番で決まる)。
// final は「次の人の依頼が来た」(依頼の境目): ここで pending は必ず片付く。Stop(final=false)では、変わっていれば edit を付けて片付け、
// 変わっていなければ !edit(部分)を 1 回だけ付けて持ち越す(裏の作業の通知の後の Stop で変わっていれば edit に置き換わる)。
func settlePending(agent string, sess *hookSession, now string, only []pendingItem, final bool) error {
	if len(sess.Pending) == 0 || len(only) == 0 {
		return nil
	}
	mine := map[string]bool{}
	for _, p := range only {
		mine[p.Trace] = true
	}
	st := store.Store{Dir: agent}
	var firstErr error
	var rest []pendingItem
	for _, p := range sess.Pending {
		if !mine[p.Trace] {
			rest = append(rest, p)
			continue
		}
		if now == "" {
			continue // 比べる物が無い: 付けずに捨てる
		}
		if p.Tree != now {
			if err := labelTrace(st, "prompt-kind", p.Trace, "edit", 1, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !p.Settled {
			if err := labelTrace(st, "prompt-kind", p.Trace, "!edit", 0.7, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
			p.Settled = true
		}
		if !final {
			rest = append(rest, p)
		}
	}
	sess.Pending = rest
	return firstErr
}

// noteAsked は「人の答えを待つ判定」を控えに足す変更(錠の中で当てる)。
func noteAsked(specID string, d *decide.Decision) func(*hookSession) {
	return func(s *hookSession) {
		if waitsForPersonFor(specID, d) {
			s.Asked = append(s.Asked, askedItem{Spec: specID, Trace: d.TraceID})
		}
	}
}

// unanswered は asked のうち、まだ答えの無い trace。
func unanswered(st store.Store, asked []askedItem) []string {
	labeled := map[string]map[string]store.Label{}
	reviews := map[string]map[string]store.Review{}
	records := map[string]map[string]store.Record{}
	var out []string
	for _, a := range asked {
		if _, ok := labeled[a.Spec]; !ok {
			m, err := st.LabeledTraces(a.Spec)
			if err != nil {
				m = map[string]store.Label{}
			}
			labeled[a.Spec] = m
			reviews[a.Spec], _ = st.Reviews(a.Spec)
			records[a.Spec] = map[string]store.Record{}
			all, _ := st.Records(a.Spec, 0)
			for _, rec := range all {
				records[a.Spec][rec.TraceID] = rec
			}
		}
		rec, ok := records[a.Spec][a.Trace]
		if !ok {
			continue
		}
		if show, _, _ := reviewQueueState(rec, labeled[a.Spec], reviews[a.Spec]); show {
			out = append(out, a.Trace)
		}
	}
	return out
}

// hasNew は waiting に reported に無い trace があるか。
func hasNew(waiting, reported []string) bool {
	seen := map[string]bool{}
	for _, r := range reported {
		seen[r] = true
	}
	for _, w := range waiting {
		if !seen[w] {
			return true
		}
	}
	return false
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

// modelUp は手元のモデルが応えるか(/health、300ms。up.go の probe は 3 秒 × 3 要求で hook には重い)。
func modelUp() bool {
	c := &http.Client{Timeout: hookProbeTimeout}
	resp, err := c.Get(strings.TrimRight(hookLocalURL, "/") + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// lookPath は PATH の検索(テストが差し替える)。
var lookPath = exec.LookPath

// treeStateWithScope は作業ツリーの状態: 仮の index に `git add -A`(.gitignore は尊重、未追跡も入る)して `git write-tree` した tree の id の先頭 16 桁(hash)と、
// その index の `git ls-files --stage` の一覧(listing。Stop で依頼の時と比べて「変わったファイル」を出す。空の repo では空。listed はそれが取れたか)、
// それと実パスの scope、project(記録に付ける仕事の印: repo のルートのフォルダ名。git の外なら cwd のフォルダ名)。
// HEAD や index の状態は入れないので、commit / stash / checkout だけでは変わらず、ファイルの中身が変わった時だけ変わる。
// 新しい blob は仮の objects(GIT_OBJECT_DIRECTORY)に書いて消すので、未追跡ファイルの中身が repo の .git/objects に残らない(repo の objects は alternates として読むだけ)。
// 全体で hookGitTimeout を超えたら ""(.gitignore の無い巨大な node_modules など。正解を付けないだけで hook は止めない)。git の外なら ""。

// 一覧と同じ repo の実パスを返し、同名の別 repo の会話を混ぜず、
// サブディレクトリや symlink からの依頼は同じ仕事として参照する。
func treeStateWithScope(cwd string) (hash, listing string, listed bool, project, scope string) {
	if cwd == "" {
		return "", "", false, "", ""
	}
	scope = feedbackScope(cwd)
	project = filepath.Base(filepath.Clean(cwd))
	ctx, cancel := context.WithTimeout(context.Background(), hookGitTimeout)
	defer cancel()
	dir := cwd
	run := func(env []string, args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", false
		}
		return strings.TrimSpace(out.String()), true
	}
	top, ok := run(nil, "rev-parse", "--show-toplevel")
	if !ok {
		return "", "", false, project, scope // git の外: 印は cwd の名
	}
	project = filepath.Base(top)
	scope = feedbackScope(top)
	dir = top // 以降は repo のルートで(サブディレクトリから起こしたセッションでも repo 全体を見る)
	objects, ok := run(nil, "rev-parse", "--git-path", "objects")
	if !ok {
		return "", "", false, project, scope
	}
	if !filepath.IsAbs(objects) {
		objects = filepath.Join(cwd, objects)
	}
	scratch, err := os.MkdirTemp("", "shikii-git-*")
	if err != nil {
		return "", "", false, project, scope
	}
	defer os.RemoveAll(scratch)
	env := []string{
		"GIT_INDEX_FILE=" + filepath.Join(scratch, "index"),
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(scratch, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + objects,
	}
	if err := os.MkdirAll(filepath.Join(scratch, "objects"), 0o755); err != nil {
		return "", "", false, project, scope
	}
	if _, ok := run(env, "add", "-A", "--", "."); !ok {
		return "", "", false, project, scope
	}
	// 仮の index だけから内部記録を除く。ignored な path の否定指定は git add がエラーにするため、別の操作で除く。
	if _, ok := run(env, "rm", "--cached", "-r", "-f", "--ignore-unmatch", "--", ".shikii"); !ok {
		return "", "", false, project, scope
	}
	tree, ok := run(env, "write-tree")
	if !ok {
		return "", "", false, project, scope
	}
	sum := sha256.Sum256([]byte(tree))
	hash = hex.EncodeToString(sum[:])[:16]
	// 一覧は読みながら上限で止める(巨大な repo で全部をメモリに読んでから捨てない)
	ls := exec.CommandContext(ctx, "git", "ls-files", "--stage", "--full-name")
	ls.Dir = dir
	ls.Env = append(os.Environ(), env...)
	ls.Stderr = io.Discard
	pipe, err := ls.StdoutPipe()
	if err != nil || ls.Start() != nil {
		return hash, "", false, project, scope
	}
	lb, rerr := io.ReadAll(io.LimitReader(pipe, int64(listingMax)+1))
	_ = ls.Wait()
	if rerr != nil || len(lb) > listingMax {
		return hash, "", false, project, scope
	}
	return hash, strings.TrimSpace(string(lb)), true, project, scope
}

func hookLog(msg string) {
	agent, err := agentHome()
	if err != nil {
		return
	}
	p := filepath.Join(agent, "hook.log")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	msg = strings.ReplaceAll(cut(msg, hookLogLineMax), "\n", " ") // モデルの応答本文(依頼や返答を含み得る)を丸ごと残さない
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}

// ---------------------------------------------------------------- 裏で起こす(人にコマンドを打たせない)

// spawnShikii は `shikii <args>` を Setsid で切り離して起こし、出力を ~/.shikii/agent/<logName> に追記する。hook も install も待たない。子の pid を返す。
func spawnShikii(agent, logName string, args ...string) int {
	exe, err := os.Executable()
	if err != nil {
		hookLog("cannot find shikii: " + err.Error())
		return 0
	}
	logf, err := os.OpenFile(filepath.Join(agent, logName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s shikii %s: started in the background\n", time.Now().Format(time.RFC3339), strings.Join(args, " "))
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logf, "%s shikii %s: %v\n", time.Now().Format(time.RFC3339), strings.Join(args, " "), err)
		return 0
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid
}

// startOnce は起こす権利を guard(~/.shikii/agent/starting-<what>)の flock で 1 つの hook だけに渡し、前に起こした子がまだ生きている(pid)か
// hookStartGap の中なら起こさない。長いモデルの取得の間(10 分を超えても)、生きている子がいれば二重に起こさない。fn は起こした子の pid を返す。
func startOnce(agent, what string, fn func() int) bool {
	return startOnceWithin(agent, what, hookStartGap, fn)
}

// startOnceWithin は startOnce の間隔(gap)を指定する版。画面は 30 秒(止めてすぐ立て直せる)、モデルは 10 分(取得に時間がかかる)。
func startOnceWithin(agent, what string, gap time.Duration, fn func() int) bool {
	_ = os.MkdirAll(agent, 0o755)
	guard := filepath.Join(agent, "starting-"+what)
	f, err := os.OpenFile(guard, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false // 別の hook が今まさに起こしている
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	b, _ := io.ReadAll(f)
	var pid int
	var at string
	fmt.Sscanf(string(b), "%d %s", &pid, &at)
	if pid > 0 && processAlive(pid) {
		return false
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil && time.Since(t) < gap {
		return false
	}
	pid = fn()
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	fmt.Fprintf(f, "%d %s\n", pid, time.Now().Format(time.RFC3339))
	return true
}

// processAlive は pid のプロセスがまだいるか(signal 0)。
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// ensureModel は手元のモデル(hookLocalURL)が応えなければ `shikii up` を裏で起こす。llama-server が無ければ起こさず hook.log に入れ方を 1 行(10 分に 1 回)。
// 初回はモデルを取ってくるので、応えるまでの数ターンは記録が付かないだけ。hook はこれを待たない。
func ensureModel(agent string) {
	if usesHosted() || modelUp() {
		return // 鍵があれば Jev が標準(SHIKII_MODEL=local なら除く)。手元のモデルは起こさない
	}
	startOnce(agent, "model", func() int {
		if _, err := lookPath("llama-server"); err != nil {
			hookLog("no local model on " + hookLocalURL + " and no llama-server on PATH: install llama.cpp (brew install llama.cpp) and the hooks will start it; until then they record nothing")
			return 0
		}
		return hookSpawn(agent, "up.log", "up")
	})
}

// ensurePage は、この置き場(root)のプロジェクトの画面が立っていなければ裏で起こす(0.8.0: プロジェクトごとに 1 つ、ポートは空いているもの)。
func ensurePage(root string) {
	ensureProjectPage(filepath.Dir(root))
}

// ---------------------------------------------------------------- install / uninstall

// hookEvent は settings に足す 1 つ。command は PATH の shikii(MCP の設定と同じ流儀)。matcher は無い(どちらの event もツールを選ばない)。
type hookEvent struct {
	event, cmd string
	timeout    int
	claudeOnly bool // Codex に無い event(SubagentStart)は Claude Code の設定にだけ足す
}

// hookEvents は 3 つ: 考える前(UserPromptSubmit)、考え終わった後(Stop)、サブエージェントが始まる時(SubagentStart。Claude Code だけ)。
// command は今の実行ファイルの絶対パス + " hook <event>": エージェントの shell の PATH に `shikii` が無くても(go install 直後の ~/go/bin など)動く。
// 実行ファイルが動いたら `shikii hook install --yes` を流し直せば、古いパスの handler は外れて新しいものが入る。
func hookEvents() []hookEvent {
	exe := "shikii"
	if p, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		exe = p
	}
	return []hookEvent{
		{"UserPromptSubmit", exe + " hook user-prompt-submit", hookTimeout, false},
		{"Stop", exe + " hook stop", hookTimeout + 5, false},
		{"SubagentStart", exe + " hook subagent-start", hookTimeout, true}, // サブエージェントにも親の依頼に近い教訓を届ける(0.7.3)
	}
}

// hookEventsFor は設定ファイルごとに足す hook。Codex には SubagentStart が無いので外す。
func hookEventsFor(who string) []hookEvent {
	var evs []hookEvent
	for _, ev := range hookEvents() {
		if ev.claudeOnly && who != "Claude Code" {
			continue
		}
		evs = append(evs, ev)
	}
	return evs
}

func hookEventNames(evs []hookEvent) string {
	names := make([]string, 0, len(evs))
	for _, ev := range evs {
		names = append(names, ev.event)
	}
	return strings.Join(names, ", ")
}

// isShikiiHook は handler の command が shikii の hook か: `<…/>shikii[…] hook <event>`(PATH の `shikii` でも絶対パスでも、古い版の pre-tool-use でも)。
func isShikiiHook(command string) bool {
	f := strings.Fields(command)
	return len(f) >= 3 && f[1] == "hook" && strings.HasPrefix(filepath.Base(f[0]), "shikii")
}

// hookTargets は hook を書く設定ファイル(家の下、固定)。両方に同じ 2 つを足す。無いツールの設定ファイルも作られる(init と同じ流儀。要らなければ消す)。
var hookTargets = []struct {
	rel, who, note string
}{
	{filepath.Join(".claude", "settings.json"), "Claude Code", ""},
	{filepath.Join(".codex", "hooks.json"), "Codex", " Codex asks you to review new hooks once: run /hooks in Codex and trust them"},
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
	tilde := func(p string) string {
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, home)
		}
		return p // HOME の外(例: /opt/homebrew/bin/shikii)は ~ を付けない(監査 R2)
	}
	if install {
		fmt.Fprintf(stdout, "1. %s: 共通の型とwikiを準備します。利用するプロジェクトで shikii --init を一度実行すると、その .shikii/ へ会話・判定・採点・正解・較正を保存します。未初期化の場所ではhookは動きません。既製の4問（%s）とskillを準備し、既存の設定は保持します。\n", tilde(agent), strings.Join(hookSpecs, ", "))
		for i, t := range hookTargets {
			evs := hookEventsFor(t.who)
			fmt.Fprintf(stdout, "%d. %s: add %d hooks (%s) running `%s hook <event>` (%s; the binary's own path, so the agent's PATH does not matter); shikii hooks from an older version or an older path are removed; a backup of the file as it was goes to %s.bak-shikii (once, only if it existed). Other hooks are kept; a second install adds nothing.%s\n", i+2, tilde(filepath.Join(home, t.rel)), len(evs), hookEventNames(evs), tilde(strings.Fields(evs[0].cmd)[0]), t.who, filepath.Base(t.rel), t.note)
		}
		fmt.Fprintf(stdout, "   The hooks measure the agent's own thinking before a turn (will this request change files? should it agree on an approach first?) and after it (were the changes within what was asked? was the request completed?). Model-based advice waits for calibration; past exchanges from the same project can be quoted before similar requests without labeling them as truths. They never stop a tool. Claude Code and Codex (Cursor and OpenCode have no hook).\n")
		fmt.Fprintf(stdout, "   Recommended model: TypeSafe Jev. With TYPESAFE_API_KEY in the environment the hooks send there and start no local model. Without a key, the local model (%s) is started in the background by the hooks when it is not running (install starts it now if llama-server is on PATH). The answer page is one per project: the first request's hook starts it on a free port, records it in ~/.shikii/agent/pages/, and opens it in the browser\n", hookLocalURL)
	} else {
		for i, t := range hookTargets {
			fmt.Fprintf(stdout, "%d. %s: remove the handlers whose command is `… shikii hook <event>` (other hooks stay)\n", i+1, tilde(filepath.Join(home, t.rel)))
		}
		fmt.Fprintf(stdout, "   %s and each project's .shikii/ are kept\n", tilde(agent))
	}
	if install {
		from, broken := installedVersion(agent)
		to := currentVersion()
		switch {
		case broken:
			fmt.Fprintf(stdout, "%d. %s: 版の印を読めないので、各プロジェクトの .shikii/ は消しません(中身を確かめてから直してください。直すまで --yes も止まります)\n", len(hookTargets)+2, tilde(installedVersionPath(agent)))
		case crossesStoreReset(from, to):
			plan := planWipe(registeredProjects(agent))
			wipes := 0
			for _, p := range plan {
				if p.Action == wipeDo {
					wipes++
				}
			}
			if wipes > 0 {
				fmt.Fprintf(stdout, "%d. %s への更新(前の版 %s): 初期化済みの %d プロジェクトの .shikii/ の中を全部消して初期化し直します(project.json は残す)。残したいものは先に控えてください:\n", len(hookTargets)+2, versionString(to), versionString(from), wipes)
			} else if len(plan) > 0 {
				fmt.Fprintf(stdout, "%d. %s への更新(前の版 %s): 消す対象のプロジェクトはありません(この版で初期化済みか、消さないもの):\n", len(hookTargets)+2, versionString(to), versionString(from))
			}
			for _, p := range plan {
				switch p.Action {
				case wipeDo:
					fmt.Fprintf(stdout, "   - 消す: %s\n", p.Project.Path)
				case wipeDone:
					fmt.Fprintf(stdout, "   - 済み: %s (%s)\n", p.Project.Path, p.Reason)
				case wipeRefuse:
					fmt.Fprintf(stdout, "   - 消しません: %s (%s)\n", p.Project.Path, p.Reason)
				}
			}
		}
	}
	if !yes {
		fmt.Fprintln(stdout, "(shown only; pass --yes to do it)")
		return nil
	}
	if install {
		for _, d := range []string{"decisions", "wiki", filepath.Join("wiki", "specs"), filepath.Join("wiki", "runs")} {
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
		if err := copySkill(filepath.Join(home, ".claude", "skills", "shikii")); err != nil {
			return fmt.Errorf("skill: %w", err)
		}
		fmt.Fprintf(stdout, "%s: the agent's rules put in place\n", tilde(filepath.Join(home, ".claude", "skills", "shikii")))
		// 0.7.0 を跨ぐ最初の実行なら各プロジェクトの .shikii/ を消して初期化し直す(update の最後に新しい本体で呼ばれるのはここ)。
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if _, err := resetStoresOnFirstRun(agent, currentVersion(), reinitWith(context.Background(), exe)); err != nil {
			return err
		}
	}
	for _, t := range hookTargets {
		settings := filepath.Join(home, t.rel)
		changed, err := editHooks(settings, install, hookEventsFor(t.who))
		if err != nil {
			return err
		}
		switch {
		case changed && install:
			fmt.Fprintf(stdout, "%s: hooks added (%s). Start a new terminal for them to take effect.%s\n", tilde(settings), t.who, t.note)
		case changed:
			fmt.Fprintf(stdout, "%s: hooks removed (%s). Start a new terminal for it to take effect\n", tilde(settings), t.who)
		case install:
			fmt.Fprintf(stdout, "%s: hooks were already there, nothing changed (%s)\n", tilde(settings), t.who)
		default:
			fmt.Fprintf(stdout, "%s: no shikii hooks found, nothing changed (%s)\n", tilde(settings), t.who)
		}
	}
	if install {
		if usesHosted() {
			fmt.Fprintf(stdout, "model: TypeSafe Jev (%s; the key is in the environment). No local model is started. SHIKII_MODEL=local selects the local model instead\n", hookHostedURL)
		} else if modelUp() {
			fmt.Fprintf(stdout, "local model: already answering on %s\n", hookLocalURL)
		} else if _, err := lookPath("llama-server"); err != nil {
			fmt.Fprintf(stdout, "local model: none on %s and no llama-server on PATH. Install llama.cpp (brew install llama.cpp); the hooks start the model by themselves after that. Until then they record nothing\n", hookLocalURL)
		} else {
			ensureModel(agent)
			fmt.Fprintf(stdout, "local model: starting in the background (the first run downloads it; a few minutes). Log: %s\n", tilde(filepath.Join(agent, "up.log")))
		}
		fmt.Fprintln(stdout, "answer page: one per project, started by the first request's hook on a free port and opened in the browser (the URL is in shikii status)")
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
		b, err := shikii.AgentSpecsFS.ReadFile(p)
		if err != nil {
			return err
		}
		created, err := seedFile(target, b)
		if err != nil {
			return err
		}
		if created {
			n++
		}

		return nil
	})
	return n, err
}

// editHooks は settings.json の hooks を直す。install なら shikii の handler を今の 2 つに揃える(無い event に足し、今の版に無い `shikii hook …`(古い PreToolUse など)は外す)。
// uninstall なら `shikii hook ` で始まる handler を全部外す。他の hook と他の鍵は残す。書く前に .bak-shikii に控える。変えたかを返す。
func editHooks(path string, install bool, hookEvents []hookEvent) (bool, error) {
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
	want := map[string]bool{}
	for _, ev := range hookEvents {
		want[ev.cmd] = install
		if v, has := hooks[ev.event]; has {
			if _, ok := v.([]any); !ok {
				return false, fmt.Errorf("%s: hooks.%s is not a list (fix it by hand; shikii does not replace what it cannot read)", path, ev.event)
			}
		}
	}
	command := func(h any) string {
		m, _ := h.(map[string]any)
		c, _ := m["command"].(string)
		return c
	}
	stale := func(h any) bool { // 外すもの: shikii の handler のうち、今の版に無い(uninstall なら全部。実行ファイルのパスが変わった古い handler もここで外れる)
		c := command(h)
		return isShikiiHook(c) && !want[c]
	}
	changed := false
	// 1) 全 event を掃く
	for name, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			continue // 読めない形の他人の event は触らない(自分の event なら上で誤りにしている)
		}
		var kept []any
		removed := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			var rest []any
			for _, h := range hs {
				if stale(h) {
					changed, removed = true, true
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
		if !removed {
			continue // 他人の event はそのまま(空の配列も触らない)
		}
		if len(kept) > 0 {
			hooks[name] = kept
		} else {
			delete(hooks, name)
		}
	}
	// 2) install: 無いものを足す
	if install {
		for _, ev := range hookEvents {
			groups, _ := hooks[ev.event].([]any)
			present := false
			for _, g := range groups {
				gm, _ := g.(map[string]any)
				hs, _ := gm["hooks"].([]any)
				for _, h := range hs {
					if command(h) == ev.cmd {
						present = true
					}
				}
			}
			if present {
				continue
			}
			group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": ev.cmd, "timeout": ev.timeout}}}
			hooks[ev.event] = append(groups, group)
			changed = true
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
	if len(b) > 0 && !fileExists(path+".bak-shikii") && !strings.Contains(string(b), " hook user-prompt-submit") && !strings.Contains(string(b), " hook pre-tool-use") && !strings.Contains(string(b), " hook stop") { // 控えは最初の 1 回だけ、shikii の hook が入っていない版(install が作った settings.json は控えない)
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

package main

// shikii hook: as a Claude Code and Codex hook, measures the AI's own "way of thinking" at two points.
// Before thinking (UserPromptSubmit): will this request end up changing files (prompt-kind), and should the approach be agreed with the person before starting (needs-plan).
// After thinking (Stop): were the changes within what was asked (changes-in-scope), and was the request finished (done-as-asked).
// There is no per-tool-call hook: classifying each command is not the AI's way of thinking, and Claude Code's permission system already decides what is allowed.
//
// Storage is each project's .shikii/ (decisions/ wiki/ records/ labels/ sessions/). The shared side holds only templates, tools and the page entry. The model is the local :8080. The answer page is :8096.
// Advice from decisions is given by band only once a line is drawn (hookOutcome). Past exchanges can be quoted even without a line.
// No deny is issued and no tool is stopped. Stopping is the person's job.
// Failures (no model, slow, cannot write) exit 0 silently: a hook must never stop the work. The reason goes to hook.log as one line.

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
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// Fixed rules (no options). Only tests replace them.
var (
	hookLocalURL      = "http://127.0.0.1:8080"   // the local model (same as the MCP settings from init)
	hookHostedURL     = "https://api.typesafe.ai" // the standard TypeSafe Jev. Used only when a key (TYPESAFE_API_KEY / DECISIONS_API_KEY) is set (decided 2026-10-23)
	hookPageURL       = "http://127.0.0.1:8096"   // placeholder for the page URL inside messages. Replaced on output with that project's page URL (a free port) (0.8.0)
	hookDecideTimeout = 5 * time.Second
	hookProbeTimeout  = 300 * time.Millisecond
	hookSessionTTL    = 7 * 24 * time.Hour
	hookLockWait      = 2 * time.Second  // upper limit for waiting on the lock
	hookGitTimeout    = 3 * time.Second  // upper limit for reading the working tree state (git). Past it: none (no truth is attached)
	hookCalibrateGap  = 10 * time.Minute // gap before auto-calibration of the same spec is started again
	// hookCalibrate draws the line. By default it starts a detached separate process (shikii calibrate --write): even a long replay does not hit the hook's timeout. Tests swap in an in-process one
	hookCalibrate = spawnCalibrate
	// hookTune scores the wording variants (shikii tune, without --write: adoption is the person's call). Also a detached process. Tests swap in an in-process one
	hookTune      = spawnTune
	hookTuneEvery = 50 // run again once the person's truths (one per input) have grown by this many since the last auto-tune
	// hookRecalibrateEvery: for a spec whose line could not be drawn, auto-calibration (including replay) is not repeated until truths grow by this many
	hookRecalibrateEvery = 20
	// hookSpawn starts shikii itself in a detached process (the model `up`, the page `serve`). The person never has to type a command. Tests swap in a recorder
	hookSpawn    = spawnShikii      // returns the started child's pid (0 if unknown)
	hookStartGap = 10 * time.Minute // gap before starting the same thing again (time from start to answering, and the retry gap after a failure)
)

const (
	hookTimeout    = 15      // seconds. git 3s + model 5s (the two questions run in parallel) + writing (audit R5: does not wait for the page to start)
	requestMax     = 1500    // length (bytes) of text kept as request / summary
	filesMax       = 30      // upper limit of "changed files" listed in the Stop question (the rest only as a count)
	listingMax     = 4 << 20 // upper limit (bytes) of the kept working tree listing. Past it, changed files count as unknown
	hookLogLineMax = 400     // upper limit (bytes) of one hook.log line. Error text can carry the model's response body
)

// hookSpecs are the 4 questions the hook asks (ids of the embedded specs). The first 2 right after the request, the last 2 at the end of the turn.
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
	case "user-prompt-submit", "stop", "subagent-start", "agent-task":
		// the hook never stops the work: exit 0 no matter what. The reason goes to hook.log
		if err := runHookEvent(args[0]); err != nil {
			hookLog(err.Error())
		}
		return nil
	case "pre-tool-use":
		// an event an older version left in settings. Do nothing, silently (the next `shikii hook install --yes` removes it)
		return nil
	}
	return fmt.Errorf("hook: unknown event %q (install | uninstall | user-prompt-submit | stop | subagent-start | agent-task)", args[0])
}

// ---------------------------------------------------------------- Claude Code / Codex JSON

// hookInput is the part of the JSON Claude Code and Codex pass on stdin that we use (the keys are the same for both).
type hookInput struct {
	SessionID            string          `json:"session_id"`
	AgentID              string          `json:"agent_id"`   // if set, this is not the person's request (a subagent etc.). Do not change the kept request
	AgentType            string          `json:"agent_type"` // SubagentStart: the subagent's type (Explore etc.)
	CWD                  string          `json:"cwd"`
	Prompt               string          `json:"prompt"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	ToolName             string          `json:"tool_name"`  // PreToolUse: the tool about to be used (only Agent / Task are looked at)
	ToolInput            json.RawMessage `json:"tool_input"` // PreToolUse: that tool's input (for Agent: prompt and subagent_type)
	// late is the Stop after a block run at the person's next request (judgeAskedTurn): it judges only. No Stop happened, so the stop
	// checks and rituals are not rechecked (a tree that passes at the next request is not "fixed before the turn ended", 0.9.3 audit 6)
	late bool
}

// hookOut is what the hook returns. Empty means output nothing (no judgment). No permission decision (allow / ask / deny) is output.
type hookOut struct {
	Context string // text passed to the agent (additionalContext. UserPromptSubmit and SubagentStart)
	System  string // text shown to the person in the terminal (systemMessage)
	Block   bool   // stop the Stop (a stop check was hit. 0.7.3). Claude Code passes the reason and continues
	Reason  string // the reason for stopping (to the agent)
}

func (o hookOut) empty() bool { return o == hookOut{} }

// message is the "output text" kept in the record: the first present of the terminal text, the stop reason and the agent context.
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
	if o.Context != "" && event != "Stop" { // Stop does not output hookSpecificOutput, because returning context there keeps the conversation going (system text only)
		out["hookSpecificOutput"] = map[string]any{"hookEventName": event, "additionalContext": o.Context}
	}
	if o.System != "" {
		out["systemMessage"] = o.System
	}
	if o.Block && event == "Stop" && o.Reason != "" { // stop check: the Stop decision; Codex (0.164) reads the same {"decision":"block","reason"} and continues the turn with the reason
		out["decision"] = "block"
		out["reason"] = o.Reason
	}
	if len(out) == 0 {
		return nil
	}
	b, _ := json.Marshal(out)
	return b
}

// ---------------------------------------------------------------- session state

// hookSession is the per-project record (.shikii/sessions/<id>.json). The transcript is not read (its shape is undocumented).
// The working tree listing at request time is the neighbouring <id>.files (too large for the JSON).
type hookSession struct {
	Request           string           `json:"request,omitempty"`             // the person's last request (kept at UserPromptSubmit)
	RequestText       string           `json:"request_text,omitempty"`        // original text for the page. Kept apart from the decision input
	RawPrompt         string           `json:"last_user_prompt,omitempty"`    // the person's last message kept without joining. Used as the source of quotes
	TurnID            string           `json:"turn_id,omitempty"`             // a repeated request with the same text is still a different turn
	Scope             string           `json:"scope,omitempty"`               // the repo's real path. Conversations, scoring and lines are limited to the same work
	Completed         *completedTurn   `json:"completed,omitempty"`           // record of the reply to this request. Paired with the person's next message
	Memories          []memoryDelivery `json:"memories,omitempty"`            // versions of memories delivered to this session (latest 16)
	Previous          string           `json:"previous,omitempty"`            // the assistant's previous message (kept at Stop)
	Planning          *planningFrame   `json:"planning,omitempty"`            // context for the current request that the agent organizes with the original text
	Pending           []pendingItem    `json:"pending,omitempty"`             // prompt-kind decisions waiting for their outcome truth
	Watch             []scopeWatch     `json:"watch,omitempty"`               // changes-in-scope decisions waiting for their outcome truth (are the changed files reverted)
	Asked             []askedItem      `json:"asked,omitempty"`               // decisions in this session that ended up waiting for the person's answer (ask and spot checks)
	Reviews           []askedItem      `json:"reviews,omitempty"`             // decisions waiting for provisional scoring from the conversation and the later spot check
	Reported          []string         `json:"reported_traces,omitempty"`     // traces reported at Stop as waiting for an answer (only mention when there is a new one)
	Judged            bool             `json:"judged,omitempty"`              // the end-of-turn self-review (changes-in-scope / done-as-asked) for the current request is done (reset on the next request)
	Head              string           `json:"head,omitempty"`                // HEAD at request time (so the stop check can see this turn's commits. 0.7.3)
	HeadAt            time.Time        `json:"head_at,omitempty"`             // request time. Commits before it (existing commits of a checked-out branch) are not looked at
	MemoryNoticed     bool             `json:"memory_noticed,omitempty"`      // told once in this session that there is memory not yet in the wiki (0.8.0)
	StoreNoticed      bool             `json:"store_noticed,omitempty"`       // told in this session that a wiki store is left under the old path's name (0.9.2)
	ReflectAsked      []string         `json:"reflect_asked,omitempty"`       // conversations asked to be reflected on before the request (the ritual check looks at it at Stop. 0.8.0)
	ReflectAskedTurn  string           `json:"reflect_asked_turn,omitempty"`  // the turn ReflectAsked was asked in (do not stop on leftovers from another turn. audit R5)
	RitualStopped     string           `json:"ritual_stopped,omitempty"`      // the turn stopped by the ritual check (do not stop again at the Stop after a notification. audit R5)
	GroupingAsked     string           `json:"grouping_asked,omitempty"`      // the turn whose Stop asked only for continues_previous and left the judgment to the next Stop (0.9.3)
	PageNoticed       bool             `json:"page_noticed,omitempty"`        // told the page location in this session (0.8.0)
	HabitsNoticed     bool             `json:"habits_noticed,omitempty"`      // told in this session how to keep work records and rules (0.8.1)
	ClassifyAsked     []string         `json:"classify_asked,omitempty"`      // pages asked before the request to get a kind and a scene (the ritual check wiki-kinds looks at it at Stop. 0.9.0)
	ClassifyAskedTurn string           `json:"classify_asked_turn,omitempty"` // the turn ClassifyAsked was asked in
	SubagentTasks     []subagentTask   `json:"subagent_tasks,omitempty"`      // task text passed to the Agent tool (SubagentStart picks lessons with that text. 0.9.1)
	At                time.Time        `json:"at"`
}

// subagentTask is a task the parent passed to a child agent with the Agent tool (kept at PreToolUse; at SubagentStart the oldest one of the same type is used).
// When several children of the same type start at once, only the order tells which text belongs to which child (the texts come from the parent's work, so a mix-up is still closer than picking by the parent's request)
type subagentTask struct {
	Type   string    `json:"type"`
	Prompt string    `json:"prompt"`
	At     time.Time `json:"at"`
}

// Records older than subagentTaskTTL are not used. Up to subagentTaskMax are kept. When a new record is added, records of the same type older than subagentTaskBurst
// are dropped (so a record whose child never started, because the Agent tool was refused, is not used for a later child and shifts everything by one. audit 0.9.1 mid-1)
const (
	subagentTaskTTL   = 10 * time.Minute
	subagentTaskMax   = 8
	subagentTaskBurst = 30 * time.Second // records for children of the same type started side by side arrive within this span
)

type pendingItem struct {
	Trace string    `json:"trace"`
	Tree  string    `json:"tree"` // hash of the working tree (no content)
	At    time.Time `json:"at"`
	// Settled: after "unchanged" (!edit, partial) was attached at the end of the turn, this keeps the item until the person's next request. If background work later
	// changes files (the Stop after a notification), the same trace can be relabeled edit (weight 1, the newer answer wins)
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

// sessionFilesPath is the kept working tree listing at request time (output of `git ls-files --stage`).
func sessionFilesPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".files"
}

// lockSession is the lock (flock) on the session record. Claude Code runs hooks in parallel (Stop and the next request overlap), so
// read → change → write is done one process at a time. The lock is held briefly (model calls are outside it). If it is not acquired within hookLockWait, go on without it (the hook never stops).
func lockSession(agent, id string) (unlock func()) {
	unlock, ok := lockSessionWithin(agent, id, hookLockWait)
	if !ok {
		hookLog(fmt.Sprintf("session %s: lock busy for %s; proceeding without it", id, hookLockWait))
	}
	return unlock
}

// lockSessionWithin takes the session lock, waiting at most wait (0: one try). ok is false when it was not taken (unlock is then a no-op).
func lockSessionWithin(agent, id string, wait time.Duration) (unlock func(), ok bool) {
	p := sessionPath(agent, id) + ".lock"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return func() {}, false
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return func() {}, false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true
}

// updateSession reads the record → changes it with fn → writes it, inside the lock. The only write path, so parallel hooks do not erase each other's changes.
func updateSession(agent, id string, fn func(s *hookSession)) error {
	unlock := lockSession(agent, id)
	defer unlock()
	s := loadSession(agent, id)
	fn(s)
	syncDeferral(agent, id, s)
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

// saveSession writes to tmp and renames (another process never reads a half-written file. Same style as editHooks).
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

// cut cuts s to at most n bytes (without splitting a multibyte character).
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// pruneSessions removes old session records (.json and .files) (7 days).
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

// ---------------------------------------------------------------- decisions

// hookEndpoints are where the hook sends. The standard is TypeSafe's Jev (when there is a key), then the local model. The first one that can send, in the spec's send_to order, is used.
// Even with a key, SHIKII_MODEL=local means local only (modelChoice).
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

// usesHosted reports whether the hook uses Jev (a key is set and SHIKII_MODEL is not local).
func usesHosted() bool {
	return hostedKey() != "" && modelChoice() != "local"
}

// hookDecide decides with the project's spec, maps the band to behaviour and records it. Without a line the band never becomes act (= the shadow period).
// If the configured language is not English, output text is shown alongside English, and the record gets a display (language, question, answer, output text).
func hookDecide(agent, specID string, inputs map[string]string, project string, originals ...map[string]string) (*decide.Decision, hookOut, error) {
	sp, err := spec.Load(hookSpecPath(agent, specID))
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
	page := pageRef(feedbackScope(filepath.Dir(agent))) // absolute: MCP started with --store .shikii would look up and start a second page under "."
	out.Context = strings.ReplaceAll(out.Context, hookPageURL+"/", page)
	out.System = strings.ReplaceAll(out.System, hookPageURL+"/", page)
	if d.Band != decide.Abstain {
		rec := recordOf(sp, d, inputs)
		rec.ReviewPending = store.ReviewSpec(specID)
		rec.Project = project                                                         // which work the decision belongs to (a marker to separate on the page and in status. Not an input, so the hash does not change)
		if l.Lang != "" && (l.Question != "" || l.Top != "" || out.message() != "") { // no display with only the language (nothing to read)
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

// hookDecideAll asks several specs with the same input in parallel (waiting for each in turn inside one hook gets close to the timeout).
// Returned in specIDs order. A spec not asked (skipped) is nil.
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

// ---------------------------------------------------------------- configured language (shown alongside)

// langCodes maps Claude Code's `language` setting → a language code (the README's 15 languages). English and unknown are "" (nothing alongside).
var langCodes = map[string]string{
	"japanese": "ja", "日本語": "ja", "chinese": "zh", "simplified chinese": "zh", "简体中文": "zh", "traditional chinese": "zh-TW", "繁體中文": "zh-TW",
	"korean": "ko", "한국어": "ko", "german": "de", "deutsch": "de", "spanish": "es", "español": "es", "french": "fr", "français": "fr",
	"italian": "it", "italiano": "it", "portuguese": "pt-BR", "português": "pt-BR", "russian": "ru", "русский": "ru", "ukrainian": "uk", "українська": "uk",
	"polish": "pl", "polski": "pl", "turkish": "tr", "türkçe": "tr", "vietnamese": "vi", "tiếng việt": "vi", "indonesian": "id", "bahasa indonesia": "id",
}

// hookLang turns ~/.claude/settings.json's language into a language code. English, missing or unknown: "".
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

// hookLoc is the wording in the configured language: the language code, and the question and answer from the spec's display.<lang> (empty if none). Lang "" means English only.
// Name / EnName are the question's title on the page (questionTitle) and EnTop the answer in English, for the notices the person reads (never the spec id).
type hookLoc struct {
	Lang, Question, Top string
	Name, EnName, EnTop string
}

func localize(sp *spec.Spec, d *decide.Decision, lang string) hookLoc {
	l := hookLoc{Lang: lang}
	if sp == nil || d == nil {
		return l
	}
	id := d.Top
	if sp.Type == spec.Noul && id == "yes" && d.P < 0.5 { // a noul decision always has Top yes with P = p(yes): in the person's words p<0.5 is the "no" side
		id = "no"
	}
	l.Name, l.EnName = questionTitle(sp, "ja"), questionTitle(sp, "en")
	if l.EnTop = strings.TrimSpace(sp.Display["en"].Options[id]); l.EnTop == "" {
		l.EnTop = id
	}
	if lang == "" {
		return l
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

// topName is the answer in the person's words (the id if none).
func (l hookLoc) topName(id string) string {
	if l.Top != "" {
		return l.Top
	}
	return id
}

// bi shows text in the configured language alongside the English text (only ja has text templates; other languages stay English).
func bi(en, ja string, l hookLoc) string {
	if l.Lang != "ja" || ja == "" {
		return en
	}
	return en + "\n" + ja
}

func answerURL(specID, trace string) string {
	return fmt.Sprintf("%s/?spec=%s&trace=%s", hookPageURL, specID, trace)
}

// hookOutcome is a pure function mapping the band to behaviour. Without a line (shadow) it outputs nothing. No permission decision is output (no tool is stopped).
// The question right after the request (needs-plan) becomes context for the agent; the end-of-turn questions (changes-in-scope / done-as-asked) become text for the person in the terminal
// (returning context at Stop keeps the conversation going. Whether to go on is the person's call).
func hookOutcome(specID string, d *decide.Decision, l hookLoc) hookOut {
	if d == nil || !d.IsCalibrated || d.Band == decide.Abstain {
		return hookOut{}
	}
	url := answerURL(specID, d.TraceID)
	// the texts in System are read by the person in the terminal: the question by its title on the page and the page's plain words (web/scripts/test-glossary.mjs),
	// the probability as the AI's confidence, never the spec id or p= (audit 0.9.3). Context is for the agent and keeps the id and p
	name, enName := l.Name, l.EnName
	if name == "" {
		name = questionTitle(nil, "ja")
	}
	if enName == "" {
		enName = questionTitle(nil, "en")
	}
	enTop := l.EnTop
	if enTop == "" {
		enTop = d.Top
	}
	unsure := func() hookOut {
		return hookOut{System: bi(
			fmt.Sprintf("shikii: the AI is unsure about %q (leaning to %q). Answer later: %s", enName, enTop, url),
			fmt.Sprintf("shikii: 問い「%s」で AI が迷っています(「%s」寄り)。あとで答える: %s", name, l.topName(d.Top), url), l)}
	}
	receipt, receiptJA := "", ""
	personReceipt, personReceiptJA := "", ""
	if r := d.Receipt; r != nil {
		receipt = fmt.Sprintf("; line on %d truths, human-checked precision %.2f", r.N, r.HumanPrecision)
		receiptJA = fmt.Sprintf("。線は正解 %d 件、人が確かめた精度 %.2f", r.N, r.HumanPrecision)
		personReceipt = fmt.Sprintf("; the point where the AI may decide alone was set from %d truths", r.N)
		personReceiptJA = fmt.Sprintf("。AI がひとりで決めてよい境目は正解 %d 件から決めました", r.N)
		if r.HumanCheckedAct > 0 {
			personReceipt += fmt.Sprintf(", and %d%% of the %d checked were right", pct(r.HumanPrecision), r.HumanCheckedAct)
			personReceiptJA += fmt.Sprintf("(確かめた %d 件のうち %d%% が合っていた)", r.HumanCheckedAct, pct(r.HumanPrecision))
		}
	}
	sure := pct(1 - d.P) // a Discard of a yes / no question is confident of the "no" side; P is p(yes)
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
		case decide.Discard: // confident "outside the scope"
			return hookOut{System: bi(
				fmt.Sprintf("shikii: the changes this time look outside what you asked for (%q: the AI is %d%% sure%s). Check the changed files before going on", enName, sure, personReceipt),
				fmt.Sprintf("shikii: 今回変えたものは頼まれた範囲の外に見えます(問い「%s」、AI の自信 %d%%%s)。続ける前に変わったファイルを確かめてください", name, sure, personReceiptJA), l)}
		case decide.Ask:
			return hookOut{} // to provisional scoring from the conversation. The person checks spot checks and holds
		}
	case "done-as-asked":
		switch d.Band {
		case decide.Discard: // confident "not finished"
			return hookOut{System: bi(
				fmt.Sprintf("shikii: what you asked looks unfinished this time (%q: the AI is %d%% sure%s). Something may be left to ask for again", enName, sure, personReceipt),
				fmt.Sprintf("shikii: 今回は頼んだことを終えていないように見えます(問い「%s」、AI の自信 %d%%%s)。言い直すことが残っているかもしれません", name, sure, personReceiptJA), l)}
		case decide.Ask:
			return hookOut{}
		}
	}
	return hookOut{} // prompt-kind, and the confident "no problem" side: record only
}

// ---------------------------------------------------------------- per event

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
		// the Stop after a stop: do not stop again (avoid an infinite loop). Only run the stop checks once more and record whether it was fixed (0.7.3).
		// A turn whose first Stop only asked for continues_previous was not judged yet: this Stop judges it (without blocking) with the grouping the organizing gave (0.9.3)
		root, _, perr := prepareHookProject(in.CWD)
		if perr != nil {
			return nil
		}
		if s := loadSession(root, in.SessionID); s.TurnID == "" || s.GroupingAsked != s.TurnID || s.Judged {
			guardRecheck(root, &in)
			return nil
		}
	}
	common := agent
	agent, _, err = prepareHookProject(in.CWD)
	if errors.Is(err, errProjectInactive) {
		// in an uninitialized place, do not save, change Git or start the model. Only when the folder was moved and the marker still has the old path, say that it has stopped (0.9.2)
		if event == "user-prompt-submit" {
			if b := (hookOut{Context: movedMarkerNotice(in.CWD)}).json(hookEventName(event)); b != nil {
				fmt.Fprintln(stdout, string(b))
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if event != "agent-task" {
		ensureModel(common) // start it in the background if missing (this turn just gets no record; do not wait). Not started when only keeping a task for a child
	}
	if event == "user-prompt-submit" {
		judgeAskedTurn(agent, &in, loadSession(agent, in.SessionID)) // the turn that waited for the Stop after its block, when that Stop never came
	}
	// model calls are outside the lock (they only read a copy of the record). Changes to the record are collected in muts and applied to the record re-read inside the lock
	snapshot := loadSession(agent, in.SessionID)
	var out hookOut
	var muts []func(*hookSession)
	var summary string
	switch event {
	case "user-prompt-submit":
		out, muts, err = hookUserPrompt(agent, &in, snapshot)
	case "stop":
		out, muts, err = hookStop(agent, &in, snapshot)
		muts = append(muts, func(s *hookSession) { summary = stopSummary(agent, s) }) // the waiting-answers summary is counted on the record after this turn's decisions are added
	case "subagent-start":
		out, muts, err = hookSubagentStart(agent, &in, snapshot) // the parent's record only drops the used task for the child
	case "agent-task":
		muts = hookAgentTask(&in) // output nothing (do not stop the tool)
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
	if event == "user-prompt-submit" || event == "stop" {
		sweepDeferrals(agent, in.SessionID) // other sessions' outcomes left waiting by a session that was closed mid-turn (outside this session's lock)
	}
	if event == "user-prompt-submit" && len(muts) > 0 {
		// only when the person's request moved on. A notification has the same TurnID, so it adds no wiki work.
		current := loadSession(agent, in.SessionID)
		if current.TurnID != snapshot.TurnID {
			// memories delivered to the same session are deduplicated together with the write-back.
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
			context, ids, rerr := reflectContext(agent, current.Scope)
			if rerr != nil {
				hookLog("reflect: " + rerr.Error())
				ids = nil // a turn where asking failed does not stop on the previous turn's leftovers (audit R5)
			} else {
				out.Context = joinLines(out.Context, context)
			}
			// write the asked conversations and turn to the record (the ritual check looks at it at Stop: was the reflection done). A turn that did not ask is cleared
			if uerr := updateSession(agent, in.SessionID, func(s *hookSession) {
				if s.TurnID == current.TurnID {
					s.ReflectAsked, s.ReflectAskedTurn = ids, current.TurnID
				}
			}); uerr != nil {
				hookLog("reflect: " + uerr.Error())
			}
			// have pages from before the version bump (no kind, no scene) filled in 5 at a time (0.9.0). The asked pages are written to the record; the ritual check at Stop looks at them
			kctx, kids, kerr := classifyContext(agent, current.Scope)
			if kerr != nil {
				hookLog("classify: " + kerr.Error())
				kids = nil
			}
			out.Context = joinLines(out.Context, kctx)
			if uerr := updateSession(agent, in.SessionID, func(s *hookSession) {
				if s.TurnID == current.TurnID {
					s.ClassifyAsked, s.ClassifyAskedTurn = kids, current.TurnID
				}
			}); uerr != nil {
				hookLog("classify: " + uerr.Error())
			}
			out.Context = joinLines(out.Context, variantsContext(agent))
			out.Context = joinLines(out.Context, wordingCheckContext(agent))
		}
	}
	if event == "stop" {
		if line := wordingSummary(agent); line != "" {
			ensurePage(agent)
			summary = joinLines(summary, line)
		}
		if line := lineSummary(agent); line != "" { // the line was drawn / cannot be drawn (0.8.3). Only once, when the state changes
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
		// drawing lines and trying wordings run outside the lock as separate processes (slow-cycle work, like the lint of Karpathy's wiki)
		for _, id := range hookSpecs {
			if cerr := hookMaybeCalibrate(agent, id); cerr != nil && err == nil {
				err = cerr
			}
			if terr := hookMaybeTune(agent, id); terr != nil && err == nil {
				err = terr
			}
		}
		for _, id := range wikiSpecs { // wiki insertion points and contradictions also get lines on the same cycle once outcome truths pile up
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
	case "agent-task":
		return "PreToolUse"
	}
	return "Stop"
}

// isNotification reports whether the input is one where Claude Code streams the completion of background work (subagents, background commands) as a "user message":
// <task-notification> / [SYSTEM NOTIFICATION - NOT USER INPUT] (not the person's request. Measured: nearly half of the records were these).
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

// hookSubagentStart delivers lessons in the same form when a subagent (Claude Code's Agent tool) starts.
// The subagent's task text arrives in neither UserPromptSubmit nor SubagentStart. Since 0.9.1 the task text the parent passed to the Agent tool is
// kept at PreToolUse (agent-task), and the oldest kept task of a child of the same type picks the lessons. Without one, only the rules that apply to every request (scene "いつも") are delivered.
// Up to 0.9.0 lessons were picked by the parent's request, and lessons unrelated to the child's work arrived (measured: of 37 deliveries to children, 4 helped).
// Deliveries are recorded under an ID separate from the parent's (a hash of the turn and the subagent). Nothing is output if there is no parent request yet. No decisions (the 2 questions) are made.
func hookSubagentStart(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	if snapshot.Request == "" || snapshot.Scope == "" || snapshot.TurnID == "" {
		return hookOut{}, nil, nil
	}
	kind := in.AgentType
	if kind == "" {
		kind = "subagent"
	}
	// the kept task is picked and dropped inside the lock (picking from a copy outside the lock lets children of the same type started side by side use the same task. audit 0.9.1 mid-2)
	var task subagentTask
	var ok bool
	if err := updateSession(agent, in.SessionID, func(s *hookSession) {
		if task, ok = pickSubagentTask(s.SubagentTasks, in.AgentType, time.Now()); ok {
			s.SubagentTasks = dropSubagentTask(s.SubagentTasks, task)
		}
	}); err != nil {
		return hookOut{}, nil, err
	}
	var muts []func(*hookSession)
	sub := hookSession{Scope: snapshot.Scope} // no record of delivered versions: deliver to the subagent even if the parent already received them
	var context, head string
	var err error
	if ok {
		context, err = memoryContextFor(agent, snapshot.Scope, cut(task.Prompt, requestMax), &sub, snapshot.TurnID, in.AgentID)
		head = "shikii: delivering to this subagent (" + kind + ") memories close to its task \"" + cut(strings.Join(strings.Fields(task.Prompt), " "), 80) + "\"."
	} else {
		context, err = alwaysContextFor(agent, snapshot.Scope, snapshot.Request, &sub, snapshot.TurnID, in.AgentID)
		head = "shikii: delivering to this subagent (" + kind + ") the rules that apply to every request."
	}
	if err != nil {
		return hookOut{}, muts, err
	}
	// the report location is delivered to subagents too (do not scatter audit and investigation reports inside the repo. 0.8.1)
	if context == "" {
		return hookOut{Context: subagentHandoffNotice}, muts, nil
	}
	return hookOut{Context: joinLines(joinLines(head, context), subagentHandoffNotice)}, muts, nil
}

// hookAgentTask, at PreToolUse (only the Agent / Task tools), adds the task text and type passed to the child agent to the parent's record. The tool is not stopped
func hookAgentTask(in *hookInput) []func(*hookSession) {
	if in.ToolName != "Agent" && in.ToolName != "Task" {
		return nil
	}
	var input struct {
		Prompt       string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
	}
	if json.Unmarshal(in.ToolInput, &input) != nil || strings.TrimSpace(input.Prompt) == "" {
		return nil
	}
	typ := input.SubagentType
	if typ == "" {
		typ = "general-purpose" // a child started without a type starts as general-purpose (treated as the same type for the 30-second drop rule)
	}
	task := subagentTask{Type: typ, Prompt: cut(strings.TrimSpace(input.Prompt), requestMax), At: time.Now().UTC()}
	return []func(*hookSession){func(s *hookSession) {
		kept := []subagentTask{}
		for _, t := range s.SubagentTasks {
			if time.Since(t.At) >= subagentTaskTTL || t.Type == task.Type && task.At.Sub(t.At) > subagentTaskBurst {
				continue
			}
			kept = append(kept, t)
		}
		kept = append(kept, task)
		if len(kept) > subagentTaskMax {
			kept = kept[len(kept)-subagentTaskMax:]
		}
		s.SubagentTasks = kept
	}}
}

// pickSubagentTask picks, among records of the same type as the started child, the oldest one that is not too old. A child started without a type starts as general-purpose
func pickSubagentTask(tasks []subagentTask, agentType string, now time.Time) (subagentTask, bool) {
	for _, t := range tasks {
		typ := t.Type
		if typ == "" {
			typ = "general-purpose"
		}
		if typ == agentType && now.Sub(t.At) < subagentTaskTTL {
			return t, true
		}
	}
	return subagentTask{}, false
}

// dropSubagentTask drops exactly one used record (if two records have the same text, one stays)
func dropSubagentTask(tasks []subagentTask, used subagentTask) []subagentTask {
	for i, t := range tasks {
		if t.Type == used.Type && t.Prompt == used.Prompt && t.At.Equal(used.At) {
			return append(append([]subagentTask{}, tasks[:i]...), tasks[i+1:]...)
		}
	}
	return tasks
}

// habitsNotice is a one-line note on working habits that arrives even without working rules (once per session). subagentHandoffNotice is where subagents put their reports.
const (
	habitsNotice          = "shikii: Put reports, audits, investigation notes, diffs and images that a person or another agent will read later in the work records .shikii/handoff/<work>/, not inside the repo (shikii handoff write <path>, or handoff_write in shikii_wiki; the person reads them in the knowledge section of the page). Point subagents to the same place for their output. When the person states a rule or correction for the future, turn it into a wiki lesson right away instead of waiting for the next reflection (shikii wiki capture → reflect place → reflect submit). When you create or change a rule, test or procedure (commands or scripts) yourself, write it right away to a howto or rule page in the wiki even if nobody asked (add to or edit the closest page if there is one)."
	subagentHandoffNotice = "shikii: If you leave a report or working files, write them in the work records .shikii/handoff/<work>/, not inside the repo (shikii handoff write <path>, or handoff_write in shikii_wiki). If the parent named a location, use that."
)

// hookUserPrompt asks the two pre-thinking questions (prompt-kind and needs-plan) and keeps the request and the working tree state. The outcome truth of the previous request is decided here (when the next request arrives).
// ackReplies are the bare acknowledgements that are attached to the previous request (0.9.3). An allowlist: anything else, however short,
// is a request of its own and starts a new conversation (Japanese requests are short, and the old 40-character rule merged a Codex
// session's 19 requests into one conversation, so reflection almost never ran). Compared after lowercasing and trimming punctuation and spaces.
var ackReplies = map[string]bool{"y": true, "yes": true, "yeah": true, "yep": true, "yup": true, "ok": true, "okay": true, "k": true, "sure": true, "go": true, "go on": true, "go ahead": true, "continue": true, "next": true, "proceed": true, "do it": true, "lgtm": true, "sounds good": true, "thanks": true, "thank you": true, "please": true, "please do": true, "yes please": true, "ok go": true, "ok thanks": true, "agreed": true, "fine": true, "はい": true, "うん": true, "ええ": true, "了解": true, "了解です": true, "承知しました": true, "わかりました": true, "分かりました": true, "いいよ": true, "いいです": true, "いいですよ": true, "大丈夫": true, "大丈夫です": true, "どうぞ": true, "お願い": true, "お願いします": true, "おねがい": true, "おねがいします": true, "よろしく": true, "よろしくお願いします": true, "続けて": true, "続けてください": true, "続けましょう": true, "では続けましょう": true, "じゃあ続けて": true, "次へ": true, "次": true, "次に進んで": true, "進めて": true, "進めてください": true, "進めましょう": true, "やって": true, "やってください": true, "やりましょう": true, "ではやりましょう": true, "それで": true, "それでお願いします": true, "それでいい": true, "それでいいよ": true, "ありがとう": true, "ありがとうございます": true, "いいね": true, "そうして": true, "そうしましょう": true, "了解しました": true, "わかった": true, "分かった": true}

func isAckReply(s string) bool {
	// full-width letters and digits as their ASCII forms (an IME types a full-width OK), and no variation selectors or joiners (an emoji picker adds U+FE0F after a thumbs-up)
	t := strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r >= 0xFE00 && r <= 0xFE0F, r == 0x200D:
			return -1
		}
		return r
	}, s)
	t = strings.ToLower(strings.TrimSpace(t))
	t = strings.TrimFunc(t, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) })
	t = strings.Join(strings.Fields(t), " ")
	return t == "" || ackReplies[t]
}

// groupedRequest is this message's request and page text, attached to the previous request (joined) or standing alone.
func groupedRequest(snapshot *hookSession, req, prompt, scope string, joined bool) (string, string) {
	request := req
	if joined && snapshot.Request != "" {
		request = cut(snapshot.Request+" / "+req, requestMax)
	}
	text := strings.TrimSpace(prompt)
	if joined && snapshot.Request != "" && snapshot.Scope == scope {
		previous := snapshot.RequestText
		if previous == "" {
			previous = snapshot.Request
		}
		text = previous + " / " + text
	}
	if !strings.HasPrefix(text, request) {
		text = ""
	}
	return request, cut(text, store.RecordTextLimit+utf8.UTFMax)
}

func hookUserPrompt(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	req := cut(strings.TrimSpace(in.Prompt), requestMax)
	if req == "" || strings.HasPrefix(req, "/") || in.AgentID != "" {
		return hookOut{}, nil, nil
	}
	if isNotification(req) {
		// a completion notice of background work is not a request: the 2 questions are not asked and the request, listing and pending are not replaced (the outcome truth is decided at the person's next request).
		// if work continues after the notice, judge the original request once more at the end of that turn (reset Judged)
		if snapshot.Request == "" {
			return hookOut{}, nil, nil
		}
		return hookOut{}, []func(*hookSession){func(s *hookSession) {
			if s.Request == snapshot.Request {
				s.Judged = false
			}
		}}, nil
	}
	// a bare acknowledgement ("ok", "continue") does not replace the request but is attached to the previous one: so the end-of-turn questions do not read "ok" as the request.
	// The list is the provisional decision: when the agent organizes this turn, its continues_previous corrects it (hook plan submit, 0.9.3)
	joined := isAckReply(req)
	inputs := map[string]string{"request": req}
	if snapshot.Previous != "" {
		inputs["previous"] = snapshot.Previous // the context stage (used only when unsure)
	}
	tree, listing, listed, project, scope := treeStateWithScope(in.CWD) // the current tree: both the "now" that settles the previous request's outcome and this request's record (both in one go)
	pageURL, pageStarted := ensureProjectPage(scope)                    // when the agent starts working (first request), start one page for this project (serve opens it in the browser; the hook does not wait. 0.8.0)
	head := gitHead(in.CWD)                                             // the starting point the stop check uses to see this turn's commits
	request, requestText := groupedRequest(snapshot, req, in.Prompt, scope, joined)
	turnID := store.NewTraceID()
	var planning *planningFrame
	if sp, err := spec.Load(hookSpecPath(agent, "needs-plan")); err == nil && needsPlanningContext(sp) {
		planning = newPlanningFrame(snapshot, in, scope, turnID)
	}
	var firstErr error
	pendingNow := snapshot.Pending // the previous request's pending (a copy). Only these traces are settled inside the lock (pending added by an overlapping Stop or the next request is left alone)
	muts := []func(*hookSession){
		func(s *hookSession) {
			if ferr := observeFeedback(agent, s, snapshot, scope, req); ferr != nil {
				hookLog("feedback: " + ferr.Error())
			}
			settleGrouping(agent, s) // the previous turn's grouping, if its organizing and Stop never came
			previous := linkedCompletion(s, snapshot, scope)
			if planning != nil && snapshot.Request != "" {
				// the agent is asked to organize this turn: settling the previous outcome waits for its continues_previous (or this turn's Stop)
				g := &requestGrouping{Joined: joined, Reply: req}
				g.Alone, g.AloneText = groupedRequest(snapshot, req, in.Prompt, scope, false)
				g.Together, g.TogetherText = groupedRequest(snapshot, req, in.Prompt, scope, true)
				if previous != nil {
					c := *previous
					g.Previous = &c
				}
				planning.Grouping = g
			} else if derr := settleDone(agent, previous, req); derr != nil {
				hookLog("outcome: " + derr.Error())
			}
			s.Request, s.Judged, s.TurnID, s.Scope, s.Completed = request, false, turnID, scope, nil
			s.Head, s.HeadAt = head, time.Now()
			s.RequestText = requestText
			s.RawPrompt = cut(strings.TrimSpace(in.Prompt), 4000)
			s.Planning = planning
		},
		// the previous request's outcome truth is decided on the tree at the time the next request arrives (the request boundary = the next request; waiting for Stop mixes two requests)
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
				_ = os.Remove(p) // outside git (or too slow, or too large): do not keep the previous request's listing and compare against it
				return
			}
			// a header saying "which request's listing this is": Stop only compares with the listing of the request it saw (not with one rewritten by the next request)
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
		planning = nil // a calibrated prompt-kind is confident it "does not change files": do not ask for the pre-start organizing (the first mut writes nil to the record)
	}
	var out hookOut
	if planning != nil {
		out.Context = planningNotice(in.SessionID, turnID)
	}
	for i, d := range ds {
		if d == nil || d.Band == decide.Abstain {
			continue
		}
		if hookSpecs[i] == "prompt-kind" && tree != "" { // keep the working tree hash for the outcome truth
			muts = append(muts, func(s *hookSession) {
				s.Pending = append(s.Pending, pendingItem{Trace: d.TraceID, Tree: tree, At: time.Now()})
			})
		}
		muts = append(muts, noteAsked(hookSpecs[i], d))
		out.Context = joinLines(out.Context, outs[i].Context)
		out.System = joinLines(out.System, outs[i].System)
	}
	_ = pageStarted                             // say nothing when it was started (serve opens it in the browser; the hook stays silent while there is no line)
	if pageURL != "" && !snapshot.PageNoticed { // the location of the running page, once per session
		out.Context = joinLines(out.Context, "shikii: this project's page: "+pageURL+"/")
		muts = append(muts, func(s *hookSession) { s.PageNoticed = true })
	}
	// tell once per session how to keep work records and rules, so it arrives even in projects without working rules (CLAUDE.md / AGENTS.md) (0.8.1).
	// only added when the hook already says something this turn (a turn that is silent while there is no line stays silent)
	if out.Context != "" && !snapshot.HabitsNoticed {
		out.Context = joinLines(out.Context, habitsNotice)
		muts = append(muts, func(s *hookSession) { s.HabitsNoticed = true })
	}
	// notice without being told: if this project's agent memory has files not yet moved to the wiki, say so (0.8.0). Once per session
	if !snapshot.MemoryNoticed {
		if notice := memoryImportNotice(scope); notice != "" {
			out.Context = joinLines(out.Context, notice)
			muts = append(muts, func(s *hookSession) { s.MemoryNoticed = true })
		}
	}
	// after the folder was moved or renamed, if a wiki store is left under the old path's name, say so (0.9.2). Once per session
	if !snapshot.StoreNoticed {
		if notice := otherPathNotice(agent, scope); notice != "" {
			out.Context = joinLines(out.Context, notice)
			muts = append(muts, func(s *hookSession) { s.StoreNoticed = true })
		}
	}
	return out, muts, firstErr
}

// planningNotNeeded reports whether the pre-start organizing can be skipped: only when prompt-kind has a line and confidently (act) answered explain, investigate or run (no file changes).
// A reply or choice on a proposal (decide) needs agreement, so it is not skipped. Without a line it is not skipped (the reason to skip has not been measured).
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

// hookStop asks the two post-thinking questions (changes-in-scope and done-as-asked) with the request, the changed file list and the last reply together.
// Only once per request (Judged). Not asked if there is no kept request. If nothing changed, changes-in-scope is not asked.
// It also keeps the assistant's last message and attaches outcome truths to the remaining pending. The waiting-answers summary is added by the caller inside the lock (stopSummary).
func hookStop(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	tree, listing, listed, project, scope := treeStateWithScope(in.CWD)
	last := cut(strings.TrimSpace(in.LastAssistantMessage), requestMax)
	req := snapshot.Request // the request this Stop evaluates. If the kept request changed inside the lock (the next request came first), this Stop's marks are not applied
	pendingNow := snapshot.Pending
	recheck := in.StopHookActive // only the Stop after a Stop that asked for continues_previous comes here with it: judge, never block
	// a short message outside the acknowledgement list, in a turn that was not organized: ask once for continues_previous and keep the
	// grouping open for it (Claude Code does not organize turns that change no files, 0.9.3 real-agent check medium 3)
	askGroup := !recheck && groupingAsk(snapshot)
	var firstErr error
	muts := []func(*hookSession){
		func(s *hookSession) {
			if s.TurnID == snapshot.TurnID && !askGroup {
				settleGrouping(agent, s) // the Stop judges this turn with the grouping it has: a later organizing no longer moves it
				// an organizing that landed while this Stop was asking the model (after its snapshot) regrouped the request this Stop judged:
				// the Stop's grouping wins, as for an organizing after the Stop, so the judgment and the reply are kept (0.9.3 audit 3)
				if g := groupingOf(s); g != nil && req != "" && s.Request != req && (req == g.Alone || req == g.Together) {
					moved := s.Request
					g.Joined = req == g.Together
					s.Request, s.RequestText = req, g.AloneText
					if g.Joined {
						s.RequestText = g.TogetherText
					}
					p := sessionFilesPath(agent, in.SessionID)
					if b, err := os.ReadFile(p); err == nil && strings.HasPrefix(string(b), listingHeader(moved)) {
						if err := writeAtomic(p, []byte(listingHeader(req)+string(b[len(listingHeader(moved)):]))); err != nil {
							hookLog("grouping: " + err.Error())
						}
					}
				}
			}
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
				return hookOut{}, muts, firstErr // the listing already belongs to the next request: this Stop lost what to compare with and does not evaluate (the next request's Stop will)
			}
			changed, known = changedFiles(string(before), listing), true
		}
	}
	// stop checks (0.7.3): apply enabled guards to the changed files, added lines and this turn's commits. If one is hit, stop the Stop and return the reason
	var out hookOut
	if !in.late {
		var gerr error
		if out, gerr = guardStop(agent, in, snapshot, string(before), changed, known, recheck); gerr != nil && firstErr == nil {
			firstErr = gerr
		}
	}
	if out.Block { // even in a turn stopped by the person's check, evaluate and record the ritual check and chain its reasons (audit R5: they used to vanish without showing in the counts)
		if rout, rerr := ritualStop(agent, snapshot.Scope, in, snapshot, changed, known, recheck); rerr != nil && firstErr == nil {
			firstErr = rerr
		} else if rout.Block {
			out.Reason = joinLines(out.Reason, rout.Reason)
		}
		turn := snapshot.TurnID
		judged := snapshot.Judged
		// the turn is judged by the Stop after this block, on the fixed tree and reply (or at the next request if that Stop never comes):
		// before 0.9.3 a turn stopped by the person's check was never judged
		muts = append(muts, func(s *hookSession) {
			s.RitualStopped = turn
			if !judged && s.TurnID == turn {
				s.GroupingAsked = turn
			}
		})
		return out, muts, firstErr
	}
	if snapshot.Judged {
		return out, muts, firstErr // a stopped turn is not judged on the changes and reply before the fix (the next request looks at the fixed tree and reply)
	}
	if askGroup {
		// not judged yet: the request depends on the grouping. The Stop after this block judges it with the grouping the organizing gave
		if rout, rerr := ritualStop(agent, snapshot.Scope, in, snapshot, changed, known, false); rerr != nil && firstErr == nil {
			firstErr = rerr
		} else if rout.Block {
			out.Block, out.Reason = true, rout.Reason
			turn := snapshot.TurnID
			muts = append(muts, func(s *hookSession) { s.RitualStopped, s.GroupingAsked = turn, turn })
			return out, muts, firstErr
		}
		askGroup = false // the check could not run: judge now with the provisional grouping, as before
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
		return id == "changes-in-scope" && (!known || len(changed) == 0) // if nothing changed (or it is unknown), "within scope" cannot be asked
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
		if target.Spec == "changes-in-scope" { // watch whether the changed files are reverted before the next request (outcome truth)
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
			if s.GroupingAsked == s.TurnID {
				s.GroupingAsked = "" // judged: from here the turn is like any other (a notice whose Stop never comes is not judged late, 0.9.3 audit 6)
			}
			if s.Completed != nil && s.Completed.TurnID == s.TurnID {
				s.Completed.Targets = targets
			}
		}
	})
	// ritual check (0.8.0): after recording the decisions, stop once if the asked plan or reflection is not done
	if in.late {
		return out, muts, firstErr
	}
	if rout, rerr := ritualStop(agent, snapshot.Scope, in, snapshot, changed, known, recheck); rerr != nil && firstErr == nil {
		firstErr = rerr
	} else if rout.Block {
		out.Block, out.Reason = true, joinLines(out.Reason, rout.Reason)
		turn := snapshot.TurnID
		muts = append(muts, func(s *hookSession) { s.RitualStopped = turn })
	}
	return out, muts, firstErr
}

// judgeAskedTurn judges, at the person's next request, a turn whose Stop asked only for continues_previous and whose Stop after the
// block never came (the person stopped the continuation, or the agent did not continue). It is that Stop run late: it never blocks, and
// the turn is judged with the grouping it has (the organizing's, or the list's). Notifications, slash commands and children are left alone.
func judgeAskedTurn(agent string, in *hookInput, snapshot *hookSession) {
	prompt := strings.TrimSpace(in.Prompt)
	if snapshot.TurnID == "" || snapshot.GroupingAsked != snapshot.TurnID || snapshot.Judged || in.AgentID != "" || prompt == "" || strings.HasPrefix(prompt, "/") || isNotification(prompt) {
		return
	}
	// the reply is this turn's, kept by its first Stop: Previous still holds the previous turn's reply when that Stop came without one
	reply := ""
	if c := snapshot.Completed; c != nil && c.TurnID == snapshot.TurnID {
		reply = c.Summary
	}
	stop := hookInput{SessionID: in.SessionID, CWD: in.CWD, StopHookActive: true, LastAssistantMessage: reply, late: true}
	_, muts, err := hookStop(agent, &stop, snapshot)
	if err != nil {
		hookLog("grouping: " + err.Error())
	}
	if err := updateSession(agent, in.SessionID, func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		hookLog("grouping: " + err.Error())
	}
}

// listingHeader is the first line of the working tree listing: which request's listing it is (a hash of the request). changedFiles does not read lines without a tab, so it is not compared.
func listingHeader(request string) string {
	sum := sha256.Sum256([]byte(request))
	return "#request " + hex.EncodeToString(sum[:])[:16] + "\n"
}

// stopSummary is the waiting-answers summary (inside the lock, after this turn's decisions are added): decisions in this session waiting for the person's answer that have no answer yet.
// One line only when there is a new one (do not repeat the same count every turn). If the page is not running it is started in the background; the line is always the URL.
func stopSummary(agent string, sess *hookSession) string {
	st := store.Store{Dir: agent}
	waiting := unanswered(st, append(append([]askedItem{}, sess.Asked...), sess.Reviews...))
	n := len(waiting)
	if n == 0 || !hasNew(waiting, sess.Reported) {
		return ""
	}
	sess.Reported = waiting
	page := pageRef(feedbackScope(filepath.Dir(agent))) // absolute: MCP started with --store .shikii would look up and start a second page under "." // start it in the background if not running. The person never types a start command
	return waitingNotice(n, page, hookLoc{Lang: hookLang()})
}

// waitingNotice is the waiting-answers line the person reads at the end of the turn (in the page's plain words, see web/scripts/test-glossary.mjs).
func waitingNotice(n int, page string, l hookLoc) string {
	return bi(
		fmt.Sprintf("shikii: %d of this session's decisions wait for your answer: %s", n, page),
		fmt.Sprintf("shikii: このセッションで、あなたの答えを待っている物が %d 件あります: %s", n, page), l)
}

// changedFiles compares two listings (`git ls-files --stage`: "mode sha stage\tpath") and returns, sorted by name, the paths of files whose content or mode changed, or that were added or removed.
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

// filesInput turns the changed file list into the question input (at most filesMax, the rest as a count). "(unknown)" if unknown, "(none)" if none.
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

// hookMaybeCalibrate follows the same rule as serve's auto-calibration: for a model with no line for the current wording (hash) yet, draw the line once the person's truths (one per input) reach eval.min_n.
// Models with a line are left alone (redrawing happens when drift reports a decline). The work is hookCalibrate (by default a detached process). The same spec runs at most once per hookCalibrateGap.
func hookMaybeCalibrate(agent, specID string) error {
	path := hookSpecPath(agent, specID)
	sp, err := spec.Load(path)
	if err != nil {
		return nil // no spec (the person removed it): do nothing
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
		for _, l := range store.LatestPerInput(labels) { // counted like calibrate: one per input, the person's truths only
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		if len(human) < sp.Eval.MinN {
			continue
		}
		guard := filepath.Join(st.Dir, "calibrating-"+specID)
		if info, err := os.Stat(guard); err == nil && time.Since(info.ModTime()) < hookCalibrateGap {
			continue // the previous calibration is still running (or just failed). Wait a while
		}
		if last, ok := lastCalibrateAttempt(guard, sp.Hash(), model); ok && len(human) < last+hookRecalibrateEvery {
			continue // no line could be drawn for the same wording and model: no replay until truths grow by hookRecalibrateEvery
		}
		_ = os.WriteFile(guard, []byte(fmt.Sprintf("%s %s %d %s\n", time.Now().Format(time.RFC3339), model, len(human), sp.Hash())), 0o600)
		hookCalibrate(agent, path, st.Dir, model)
	}
	return nil
}

// lastCalibrateAttempt returns, from the record of the last auto-calibration (time, model, number of truths, wording hash), the number of truths tried with the same wording and model.
// If a line is drawn it is never called, so a record means "tried but no line" or "still running". Records in an old format count as missing.
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

// humanTruths is the number of the person's truths on the current wording (hash) (one per input, counted across models). Counted like calibrate / tune.
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

// hookMaybeTune lets the machine try the wording variants (on a slow cycle, like the wiki's lint): when the spec has variants (wordings the person wrote),
// the person's truths are at least eval.min_n, and they grew by hookTuneEvery since the last auto-tune, it starts `shikii tune` (without --write) detached.
// Without variants there is nothing to try. A variant that scored higher gets its meaning checked by the agent before the next request (wordingCheckContext). The last count is kept in tuned-<spec>.
func hookMaybeTune(agent, specID string) error {
	path := hookSpecPath(agent, specID)
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
			last = cnt // carry the last count only for the same wording (a changed wording is counted again as a new spec)
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

// spawnTune starts `shikii tune <spec> --store … --local …` in a detached process (the hook does not wait; no --write). Output is appended to hook.log.
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

// spawnCalibrate starts `shikii calibrate <spec> --store … --local … --model … --write` in a detached process (the hook does not wait).
// Output is appended to hook.log. A long replay does not hit the hook's timeout, and if killed midway the spec is not broken (tmp + rename).
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

// settlePending attaches outcome truths to the kept prompt-kind decisions: if the tree at request time differs from the current tree (now), edit (weight 1); if the same, !edit (partial, weight 0.7).
// Called right after a request (when the next request arrives) and at Stop. If now is "" (outside git, or git too slow), they are dropped without a label (nothing to compare).
// only is the pending the caller saw outside the lock: only those still present inside the lock are labeled (pending added later by an overlapping hook is settled on that hook's turn).
// final means "the person's next request arrived" (the request boundary): pending is always cleared here. At Stop (final=false), if changed, edit is attached and it is cleared;
// if unchanged, !edit (partial) is attached once and it is carried over (if it changed by the Stop after a background notification, it is replaced with edit).
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
			continue // nothing to compare: drop without a label
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

// noteAsked is the change that adds a "decision waiting for the person's answer" to the record (applied inside the lock).
func noteAsked(specID string, d *decide.Decision) func(*hookSession) {
	return func(s *hookSession) {
		if waitsForPersonFor(specID, d) {
			s.Asked = append(s.Asked, askedItem{Spec: specID, Trace: d.TraceID})
		}
	}
}

// unanswered is the traces in asked that have no answer yet.
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

// hasNew reports whether waiting has a trace not in reported.
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
	if source == "outcome" {
		return st.AppendLabelsOnce(labels) // decided by hooks that can race or retry: each outcome truth lands once (0.9.3)
	}
	return st.AppendLabels(labels)
}

// modelUp reports whether the local model answers (/health, 300ms. up.go's probe is 3s × 3 requests, too heavy for a hook).
func modelUp() bool {
	c := &http.Client{Timeout: hookProbeTimeout}
	resp, err := c.Get(strings.TrimRight(hookLocalURL, "/") + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// lookPath is the PATH lookup (tests replace it).
var lookPath = exec.LookPath

// treeStateWithScope is the working tree state: the first 16 digits (hash) of the tree id from `git add -A` (respects .gitignore, includes untracked) into a temporary index and `git write-tree`,
// the `git ls-files --stage` listing of that index (listing. At Stop it is compared with the one at request time to get the "changed files". Empty in an empty repo. listed says whether it was obtained),
// plus the real-path scope and project (the work marker put on records: the folder name of the repo's root; outside git, the cwd's folder name).
// HEAD and index state are not included, so commit / stash / checkout alone do not change it; it changes only when file contents change.
// New blobs are written to temporary objects (GIT_OBJECT_DIRECTORY) and removed, so untracked file contents do not stay in the repo's .git/objects (the repo's objects are only read, as alternates).
// If the whole thing exceeds hookGitTimeout, "" (e.g. a huge node_modules with no .gitignore. No truth is attached, but the hook does not stop). Outside git, "".

// Returns the real path of the same repo as the listing, so conversations of another repo with the same name are not mixed in,
// and requests from subdirectories or symlinks are treated as the same work.
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
		return "", "", false, project, scope // outside git: the marker is the cwd's name
	}
	project = filepath.Base(top)
	scope = feedbackScope(top)
	dir = top // from here on, at the repo's root (even a session started in a subdirectory sees the whole repo)
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
	// remove internal records only from the temporary index. git add errors on a negated pattern for an ignored path, so they are removed in a separate step.
	if _, ok := run(env, "rm", "--cached", "-r", "-f", "--ignore-unmatch", "--", ".shikii"); !ok {
		return "", "", false, project, scope
	}
	tree, ok := run(env, "write-tree")
	if !ok {
		return "", "", false, project, scope
	}
	sum := sha256.Sum256([]byte(tree))
	hash = hex.EncodeToString(sum[:])[:16]
	// stop reading the listing at the upper limit (do not read everything into memory in a huge repo and then throw it away)
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
	msg = strings.ReplaceAll(cut(msg, hookLogLineMax), "\n", " ") // do not keep the model's response body (it may contain requests or replies) whole
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}

// ---------------------------------------------------------------- start in the background (the person never types a command)

// spawnShikii starts `shikii <args>` detached with Setsid and appends output to ~/.shikii/agent/<logName>. Neither the hook nor install waits. Returns the child's pid.
func spawnShikii(agent, logName string, args ...string) int {
	exe, err := os.Executable()
	if err != nil {
		hookLog("cannot find shikii: " + err.Error())
		return 0
	}
	if runningAsTestBinary() {
		return 0 // a go test binary runs the whole test suite regardless of its arguments. Starting it in the background makes them pile up (0.9.1: 75 were left on 2026-10-25)
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

// runningAsTestBinary reports whether the current executable was built by go test (its name ends in .test). Guards against real background processes and browsers
func runningAsTestBinary() bool {
	exe, err := os.Executable()
	return err == nil && strings.HasSuffix(filepath.Base(exe), ".test")
}

// startOnce gives the right to start to only one hook via a flock on guard (~/.shikii/agent/starting-<what>), and does not start if the previously started child is still alive (pid)
// or within hookStartGap. During a long model download (even past 10 minutes), a live child prevents a double start. fn returns the started child's pid.
func startOnce(agent, what string, fn func() int) bool {
	return startOnceWithin(agent, what, hookStartGap, fn)
}

// startOnceWithin is startOnce with the gap given. The page uses 30 seconds (can be restarted right after stopping), the model 10 minutes (download takes time).
func startOnceWithin(agent, what string, gap time.Duration, fn func() int) bool {
	_ = os.MkdirAll(agent, 0o755)
	guard := filepath.Join(agent, "starting-"+what)
	f, err := os.OpenFile(guard, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false // another hook is starting it right now
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

// processAlive reports whether the process pid still exists (signal 0).
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// ensureModel starts `shikii up` in the background if the local model (hookLocalURL) does not answer. Without llama-server it does not start and writes one line on how to install to hook.log (once per 10 minutes).
// The first run downloads the model, so for the few turns until it answers there are simply no records. The hook does not wait for this.
func ensureModel(agent string) {
	if usesHosted() || modelUp() {
		return // with a key, Jev is the standard (unless SHIKII_MODEL=local). The local model is not started
	}
	startOnce(agent, "model", func() int {
		if _, err := lookPath("llama-server"); err != nil {
			hookLog("no local model on " + hookLocalURL + " and no llama-server on PATH: install llama.cpp (brew install llama.cpp) and the hooks will start it; until then they record nothing")
			return 0
		}
		return hookSpawn(agent, "up.log", "up")
	})
}

// ensurePage starts the page of this store's (root) project in the background if it is not running (0.8.0: one per project, on a free port).
func ensurePage(root string) {
	ensureProjectPage(filepath.Dir(root))
}

// ---------------------------------------------------------------- install / uninstall

// hookEvent is one entry added to settings. command is the shikii on PATH (same style as the MCP settings). matcher is only for events that select a tool (PreToolUse).
type hookEvent struct {
	event, cmd string
	timeout    int
	claudeOnly bool   // events Codex lacks (SubagentStart, the PreToolUse for child agents) are added only to Claude Code's settings
	matcher    string // tools PreToolUse looks at (Agent / Task only. Other tools do not trigger the hook)
}

// hookEvents are 4: before thinking (UserPromptSubmit), after thinking (Stop), when a subagent starts (SubagentStart. Claude Code only),
// and right before a subagent is started (PreToolUse for Agent / Task only. Keeps the task text passed to the child. Claude Code only. 0.9.1).
// command is the current executable's absolute path + " hook <event>": it works even if the agent's shell PATH has no `shikii` (e.g. ~/go/bin right after go install).
// If the executable moves, rerunning `shikii hook install --yes` removes the handlers with the old path and adds new ones.
func hookEvents() []hookEvent {
	exe := "shikii"
	if p, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		exe = p
	}
	return []hookEvent{
		{"UserPromptSubmit", hookCommand(exe, "user-prompt-submit"), hookTimeout, false, ""},
		{"Stop", hookCommand(exe, "stop"), hookTimeout + 5, false, ""},
		{"SubagentStart", hookCommand(exe, "subagent-start"), hookTimeout, true, ""},    // deliver lessons to subagents too (0.7.3)
		{"PreToolUse", hookCommand(exe, "agent-task"), hookTimeout, true, "Agent|Task"}, // keep the task text passed to the child (0.9.1)
	}
}

// hookCommand is the command line of one hook. The agents run it through a shell, so a path with a space or a quote is quoted
// (0.9.3 and earlier wrote it bare: with a space in the path the shell ran only the part before it, and the hook never worked).
func hookCommand(exe, event string) string { return shellQuote(exe) + " hook " + event }

// hookBinary is the binary of a hook command `<binary> hook <event>`: a bare path (also one with spaces, as 0.9.3 and earlier wrote it),
// or a path in single quotes as hookCommand writes it. ok is false when the command is not of that form.
func hookBinary(command string) (string, bool) {
	c := strings.TrimSpace(command)
	i := strings.LastIndex(c, " hook ")
	if i <= 0 || strings.TrimSpace(c[i+len(" hook "):]) == "" {
		return "", false
	}
	bin := strings.TrimSpace(c[:i])
	if len(bin) >= 2 && bin[0] == '\'' && bin[len(bin)-1] == '\'' {
		bin = strings.ReplaceAll(bin[1:len(bin)-1], `'\''`, "'")
	} else if strings.ContainsAny(bin, "'\"") {
		return "", false
	}
	if strings.ContainsAny(bin, " \t") && !filepath.IsAbs(bin) {
		return "", false // `env shikii`, `shikii-tool run`: not a path to a binary
	}
	return bin, true
}

// hookEventsFor is the hooks added per settings file. Codex has no SubagentStart, so it is left out.
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

// isShikiiHook reports whether a handler's command is a shikii hook: `<…/>shikii[…] hook <event>` (the `shikii` on PATH or an absolute path, and also the old pre-tool-use).
func isShikiiHook(command string) bool {
	bin, ok := hookBinary(command)
	return ok && strings.HasPrefix(filepath.Base(bin), "shikii")
}

// hookTargets are the settings files the hooks are written to (under home, fixed). The same 2 are added to both. Settings files for tools that are not installed are also created (same style as init; delete them if not needed).
var hookTargets = []struct {
	rel, who, note string
}{
	{filepath.Join(".claude", "settings.json"), "Claude Code", ""},
	{filepath.Join(".codex", "hooks.json"), "Codex", " Codex runs only hooks you trusted, and asks again whenever ~/.codex/hooks.json changes (a new path or timeout): open Codex, run /hooks and trust shikii's hooks; until then Codex skips them (codex exec says nothing)"},
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
		return p // outside HOME (e.g. /opt/homebrew/bin/shikii) gets no ~ (audit R2)
	}
	if install {
		fmt.Fprintf(stdout, "1. %s: prepare the shared templates and wiki. Run shikii --init once in each project that uses it; from then on conversations, decisions, scores, truths and calibrations are saved in its .shikii/. The hooks do not run in uninitialized places. Prepares the 4 ready-made questions (%s) and the skill, and keeps existing settings.\n", tilde(agent), strings.Join(hookSpecs, ", "))
		for i, t := range hookTargets {
			evs := hookEventsFor(t.who)
			fmt.Fprintf(stdout, "%d. %s: add %d hooks (%s) running `%s hook <event>` (%s; the binary's own path, so the agent's PATH does not matter); shikii hooks from an older version or an older path are removed; a backup of the file as it was goes to %s.bak-shikii (once, only if it existed). Other hooks are kept; a second install adds nothing.%s\n", i+2, tilde(filepath.Join(home, t.rel)), len(evs), hookEventNames(evs), tilde(func() string { bin, _ := hookBinary(evs[0].cmd); return bin }()), t.who, filepath.Base(t.rel), t.note)
		}
		fmt.Fprintf(stdout, "   The hooks measure the agent's own thinking before a turn (will this request change files? should it agree on an approach first?) and after it (were the changes within what was asked? was the request completed?). Model-based advice waits for calibration; past exchanges from the same project can be quoted before similar requests without labeling them as truths. They never stop a tool. Claude Code and Codex.\n")
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
			fmt.Fprintf(stdout, "%d. %s: the version marker cannot be read, so no project's .shikii/ is removed (check its contents and fix it. Until it is fixed, --yes stops too)\n", len(hookTargets)+2, tilde(installedVersionPath(agent)))
		case crossesStoreReset(from, to):
			plan := planWipe(registeredProjects(agent))
			wipes := 0
			for _, p := range plan {
				if p.Action == wipeDo {
					wipes++
				}
			}
			if wipes > 0 {
				fmt.Fprintf(stdout, "%d. update to %s (previous version %s): everything inside .shikii/ of %d initialized projects is removed and initialized again (project.json is kept). Save anything you want to keep first:\n", len(hookTargets)+2, versionString(to), versionString(from), wipes)
			} else if len(plan) > 0 {
				fmt.Fprintf(stdout, "%d. update to %s (previous version %s): no projects to remove (already initialized in this version, or not removed):\n", len(hookTargets)+2, versionString(to), versionString(from))
			}
			for _, p := range plan {
				switch p.Action {
				case wipeDo:
					note := ""
					if files, err := (handoff.Book{Store: filepath.Join(p.Project.Path, ".shikii")}).List(); err == nil && len(files) > 0 {
						note = fmt.Sprintf(" (%d work records (.shikii/handoff/) are removed too)", len(files))
					}
					fmt.Fprintf(stdout, "   - remove: %s%s\n", p.Project.Path, note)
				case wipeDone:
					fmt.Fprintf(stdout, "   - done: %s (%s)\n", p.Project.Path, p.Reason)
				case wipeRefuse:
					fmt.Fprintf(stdout, "   - not removed: %s (%s)\n", p.Project.Path, p.Reason)
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
		for _, rel := range skillDirs {
			if err := copySkill(filepath.Join(home, rel)); err != nil {
				return fmt.Errorf("skill: %w", err)
			}
			fmt.Fprintf(stdout, "%s: the agent's rules put in place\n", tilde(filepath.Join(home, rel)))
		}
		// point the Codex MCP server shikii wrote at this binary while the block is still shikii's (0.9.1 and earlier wrote the bare name "shikii",
		// which Codex started from a desktop app may not find on its PATH; 0.9.3). A block a person edited is left alone
		codex := filepath.Join(home, ".codex", "config.toml")
		// 0.9.1 and 0.9.2 also wrote --store .shikii, which exits before the handshake when Codex starts the server outside a shikii project; the shared
		// registration becomes args = ["mcp", "--local", ...] with no store and each call passes project (0.9.3)
		if fixed, err := fixCodexServer(codex, codexServerArgs); err != nil {
			fmt.Fprintf(stdout, "%s: %v\n", tilde(codex), err)
		} else if fixed {
			fmt.Fprintf(stdout, "%s: pointed the shikii MCP server shikii wrote at %s with %s (Codex; reopen Codex)\n", tilde(codex), mcpCommand(), codexServerArgsLine)
		} else if codexServerPinsAProject(codex) {
			fmt.Fprintf(stdout, "%s: %s\n", tilde(codex), codexPinnedNote)
		}
		// remove the shikii section an earlier version wrote into each registered project's AGENTS.md (0.9.2: shikii no longer writes into it;
		// only the part between the markers goes, the person's text and the file stay)
		for _, p := range registeredProjects(agent) {
			path := filepath.Join(p.Path, "AGENTS.md")
			if did, err := removeAgentsSection(path); err != nil {
				fmt.Fprintf(stdout, "%s: %v\n", tilde(path), err)
			} else if did {
				fmt.Fprintf(stdout, "%s: removed the shikii section an earlier version wrote (the rest is kept)\n", tilde(path))
			}
		}
		// 0.8.x treats pages with a kind and a scene as "changed", and 0.9.1 and earlier pages written with 0.9.2's English fixed words; both refuse every save
		// (the pages are not broken). Tell this so the MCP of open sessions and
		// other machines using the same wiki are not left on the old version (audit 0.9.0 B3)
		fmt.Fprintln(stdout, "Note: reopen any open agent sessions (and their MCP). 0.9.1 and earlier read wiki pages written by 0.9.2 (English fixed words) as changed and refuse to save, as 0.8.x did with pages that have a kind and a scene. Move every machine that uses the same wiki to 0.9.2 or later.")
		// on the first run that crosses 0.7.0, remove and reinitialize each project's .shikii/ (this is where the new binary is called at the end of update).
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
		fmt.Fprintln(stdout, "answer page: one per project, started by the first request's hook on a free port and opened in the browser (shikii page prints the URL)")
	}
	return nil
}

// seedAgentSpecs copies only the embedded specs that are missing. Returns how many were copied.
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

// editHooks fixes the hooks in settings.json. For install, it aligns shikii's handlers to the current set (adds to missing events, removes `shikii hook …` not in this version, such as the old PreToolUse).
// For uninstall, it removes every handler starting with `shikii hook `. Other hooks and other keys stay. Before writing, it backs up to .bak-shikii. Returns whether it changed anything.
func editHooks(path string, install bool, hookEvents []hookEvent) (bool, error) {
	return editHooksTo(path, install, hookEvents, true)
}

// editHooksTo is editHooks; with write false it only reports whether it would change anything (shikii uninstall without --yes).
func editHooksTo(path string, install bool, hookEvents []hookEvent, write bool) (bool, error) {
	// a settings file kept elsewhere through a link (dotfiles): write the file it points at, so the link stays a link and its mode stays
	// (the backup stays next to the link). Before 0.9.3 the rename below replaced the link with a regular 0600 file (audit 0.9.3 uninstall)
	backup := path + ".bak-shikii"
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil && strings.TrimSpace(string(b)) != "" { // an empty file is the same as {}
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
	stale := func(h any) bool { // to remove: shikii handlers not in this version (all of them for uninstall. Old handlers whose executable path changed are removed here too)
		c := command(h)
		return isShikiiHook(c) && !want[c]
	}
	changed := false
	// 1) sweep all events
	for name, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			continue // someone else's event in an unreadable shape is not touched (for our own events it is an error above)
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
			continue // someone else's event stays as it is (an empty array is not touched either)
		}
		if len(kept) > 0 {
			hooks[name] = kept
		} else {
			delete(hooks, name)
		}
	}
	// 2) install: add what is missing
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
			if ev.matcher != "" {
				group["matcher"] = ev.matcher
			}
			hooks[ev.event] = append(groups, group)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if !write {
		return true, nil
	}
	if len(hooks) > 0 {
		cfg["hooks"] = hooks
	} else {
		delete(cfg, "hooks")
	}
	if len(b) > 0 && !fileExists(backup) && !strings.Contains(string(b), " hook user-prompt-submit") && !strings.Contains(string(b), " hook pre-tool-use") && !strings.Contains(string(b), " hook stop") { // the backup is made only the first time, of a version without shikii hooks (a settings.json created by install is not backed up)
		if err := os.WriteFile(backup, b, 0o600); err != nil {
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
	return true, writePersonFile(path, buf.Bytes(), 0o600)
}

// pct is a probability as a whole percent, as the page shows the AI's confidence.
func pct(p float64) int { return int(math.Round(p * 100)) }

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
	toml "github.com/pelletier/go-toml/v2"
)

// Fixtures for serve + tune: a fake model whose answer changes with the wording, a spec with candidates, a truth file, a wiki.
func newTuneFixture(t *testing.T) (srvURL, specsDir, wiki, labeledPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir = filepath.Join(dir, "decisions")
	specWithVariants(t, dir) // decisions/intent.yaml with variants strict / worse; the wiki is expected next to decisions/, with no flag
	labeledPath = filepath.Join(dir, "rows.json")
	rows := bankRows()
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write eval.labeled into the spec (serve's tune cannot take it as a flag)
	p := filepath.Join(specsDir, "intent.yaml")
	b, _ := os.ReadFile(p)
	src := strings.Replace(string(b), "eval:\n  min_n: 2\n", "eval:\n  min_n: 2\n  labeled: "+labeledPath+"\n", 1)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	wiki = filepath.Join(dir, ".shikii", "wiki")
	model := wordingSensitiveModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL, specsDir, wiki, labeledPath
}

var _ = spec.Choice

// Tests that reproduce H1, M1, M2, M3 and L1 of audit 0.8.3 (brought in from what the auditor wrote in a copy).

func audit083Setup(t *testing.T) (string, string, store.Store, string) {
	model := fakeAgentModel(t, func(state string) float64 { return 0.3 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	src := fmt.Sprintf("%016x", 777)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	return agent, scope, st, src
}

// Audit 0.9.2 (English): behaviour for a person whose language setting is English.
var auditJapanese = regexp.MustCompile("[\u3040-\u30ff\u4e00-\u9fff]")

// asIf096 turns the store at moved (moved from old) into what 0.9.6 left: no records/store-name, no records/store-id, and the shared
// index has only the path-named entry 0.9.6 writes (no store_id). The wiki stays under sha256(old), where 0.9.6 wrote it.
func asIf096(t *testing.T, common, old, moved string) {
	t.Helper()
	root := filepath.Join(moved, ".shikii")
	for _, f := range []string{"store-name", "store-id"} {
		if err := os.Remove(filepath.Join(root, "records", f)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, f := range files {
		os.Remove(f)
	}
	legacy, _ := json.Marshal(map[string]any{"id": projectID(old), "name": filepath.Base(old), "path": old, "url": "/p/" + projectID(old) + "/"})
	if err := os.WriteFile(filepath.Join(common, "projects", projectID(old)+".json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	// precondition, checked without the code under test: the page 0.9.6 wrote is under sha256(old)
	sum := sha256.Sum256([]byte(old))
	if entries, err := os.ReadDir(filepath.Join(root, "wiki", "lessons", hex.EncodeToString(sum[:]))); err != nil || len(entries) == 0 {
		t.Fatalf("precondition: the wiki is under sha256(old): %v", err)
	}
}

func full1Copy(t *testing.T, from, to string) {
	t.Helper()
	if out, err := exec.Command("cp", "-R", from, to).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

// tryLockFromOutside tries to take the flock of the file at path without waiting, and lets it go again when it got it: nil means nobody
// held it, syscall.EWOULDBLOCK means someone does.
func tryLockFromOutside(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}
	return err
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// Pins down the wipe-related findings of audit round 2 (2026-10-23).

func initAt(t *testing.T, dir string) {
	t.Helper()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)
	capture(t, func() error { return cmdInit(nil) })
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// uiAssetRef is the page's own assets that index.html loads (script src and link href).
var uiAssetRef = regexp.MustCompile(`(?:src|href)="([^"]+)"`)

// uiText returns all text of the page: index.html joined with the assets it loads.
// The page is a Svelte bundle (web/dist embedded), so the wording lives in assets/*.js, not index.html.
// Every reference is relative to ./ (no external loads). Anything else fails.
func uiText(t *testing.T, url string) string {
	t.Helper()
	index := getBody(t, url+"/")
	var sb strings.Builder
	sb.WriteString(index)
	for _, m := range uiAssetRef.FindAllStringSubmatch(index, -1) {
		ref := m[1]
		if !strings.HasPrefix(ref, "./") {
			t.Fatalf("index.html must reference only its own assets (relative ./), found %q", ref)
		}
		sb.WriteString(getBody(t, url+ref[1:]))
	}
	return sb.String()
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// lockCallers is the allowlist of the places that call the three functions waiting for the calibration lock (0.9.7, after four audits
// found ways around a general check of every writer-taking function): callee → calling function → how the writer is passed.
// "w" is the calling function's own http.ResponseWriter parameter, never assigned, ranged over or taken by address in it; "nil in go" is a
// literal nil inside a go statement (no reply waits). Any other mention of these names, anywhere in the package (a value, a function
// literal, a package-level variable, another caller), fails. A new path that waits for the lock is added here on purpose.
// Every function that waits for the lock, directly or through another, is a key, so its own callers are bound all the way up (audit F-1,
// F-3); a handler is reached only through its HTTP registration ("registered": passed to HandleFunc in that function).
var lockCallers = map[string]map[string]string{
	"lockCalibration":   {"runTuneCaptured": "w", "maybeCalibrate": "w", "handleTuneVariant": "w", "handleTuneAdopt": "w", "handleTuneReview": "w"},
	"runTuneCaptured":   {"handleTuneSync": "w", "handleTuneRun": "nil in go"},
	"maybeCalibrate":    {"handleLabel": "w"},
	"handleLabel":       {"handler": "registered"},
	"handleTuneSync":    {"handler": "registered"},
	"handleTuneRun":     {"handler": "registered"},
	"handleTuneVariant": {"handler": "registered"},
	"handleTuneAdopt":   {"handler": "registered"},
	"handleTuneReview":  {"handler": "registered"},
}

// codexOutcome is what the properties saw shikii do with one config.toml: whether init added the server, what removing it from the result
// did ("cut", "none" or "refused"), what fixing shikii's own form appended to the person's text did ("fixed", "left", or "skipped" when
// that form cannot be appended), and the text the removal returned.
type codexOutcome struct {
	added   bool
	removal string
	fix     string
	removed string
}

// 0.9.7: Codex's config.toml is read with a TOML parser, not with patterns. Before, every audit found another way of writing the same
// settings that the patterns missed (quotes, \u escapes, dotted keys, comments), and words in a value looked like a shikii server.
// Whatever init does, the file must still be TOML that Codex reads, hold at most one shikii, and keep the person's settings as they were.

func readTOML(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("not TOML Codex can read: %v\n%s", err, b)
	}
	return m
}

// withoutShikiiServer is the parsed settings without mcp_servers.shikii (and without mcp_servers when that was all it held).
func withoutShikiiServer(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	if servers, ok := out["mcp_servers"].(map[string]any); ok {
		rest := map[string]any{}
		for k, v := range servers {
			if k != "shikii" {
				rest[k] = v
			}
		}
		out["mcp_servers"] = rest
		if len(rest) == 0 {
			delete(out, "mcp_servers")
		}
	}
	return out
}

// A context-sensitive fake model: "fix …" is unsure with the text alone (0.65). With the previous exchange (Previous turn) it is confident (0.9). "why …" is confident with the text alone.
func contextModel(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&calls, 1)
		probs := map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}
		switch {
		case strings.Contains(req.State, "why"):
			probs = map[string]float64{"change_request": 0.03, "question": 0.95, "other": 0.02}
		case strings.Contains(req.State, "Previous turn"):
			probs = map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02}
		}
		top, _ := argmax(probs)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// writeContextSpec puts state.context (a layout that adds the previous exchange) into the intent spec and places it in decisions/.
func writeContextSpec(t *testing.T, dir string) string {
	t.Helper()
	sub := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeSpec(t, sub, "false")
	b, _ := os.ReadFile(path)
	src := strings.Replace(string(b), "  required: [message]\n", "  required: [message]\n  context: \"Previous turn: {{ previous }}\\nMessage: {{ message }}\"\n", 1)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readLabels(t *testing.T, storeDir, specID string) []store.Label {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(storeDir, "labels", specID+".jsonl"))
	if err != nil {
		return nil
	}
	var out []store.Label
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var l store.Label
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

// 0.9.3: whether a message continues the previous request is decided by the agent in its pre-start organizing
// (continues_previous in hook plan submit). The fixed acknowledgement list is only the provisional decision and the fallback.

const continuesFirst = "Use the agreed CSV columns and keep the existing filters."

// continuesTurn runs the first request and its Stop (which records done-as-asked), then the person's next message.
// It returns the done-as-asked trace of the first request.
func continuesTurn(t *testing.T, repo, root, reply string) string {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Kept the filters and used the agreed columns."})
	done := ""
	for _, r := range loadSession(root, "planning").Reviews {
		if r.Spec == "done-as-asked" {
			done = r.Trace
		}
	}
	if done == "" {
		t.Fatalf("the first request must have a done-as-asked decision: %+v", loadSession(root, "planning"))
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": reply})
	return done
}

func continuesSubmit(t *testing.T, root string, continues bool) {
	t.Helper()
	payload := map[string]any{
		"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID,
		"agreed":             map[string]any{"unknown": true},
		"open":               map[string]any{"unknown": true},
		"continues_previous": continues,
	}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatalf("plan submit: %v", err)
	}
}

func doneLabel(t *testing.T, root, trace string) (store.Label, bool) {
	t.Helper()
	m, err := (store.Store{Dir: root}).LabeledTraces("done-as-asked")
	if err != nil {
		t.Fatal(err)
	}
	l, ok := m[trace]
	return l, ok
}

// 0.9.3 real-agent check, medium 3: Claude Code does not organize turns that change no files, so a short acknowledgement outside the
// fixed list (a polite Japanese "OK", "let's go with that") stayed a conversation of its own. The Stop now asks once for continues_previous only.

func stopReason(t *testing.T, out string) (string, bool) {
	t.Helper()
	if out == "" {
		return "", false
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil {
		t.Fatalf("the Stop output is not JSON: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	return reason, parsed["decision"] == "block"
}

// 0.9.3 real-agent check, low 4: the agent names the earlier request a message restates (restates in hook plan submit), and shikii compares
// the message with that request's own text by the fixed rule.

const (
	restateFirst  = "invoices/ の請求書ごとに合計を検算するスクリプトを check-totals.sh に書いて、ずれていたら直し、全部通ることを見せて"
	restateSecond = "合計の検算も足して" // short (under 40 characters): request 1 gets no outcome from them, as in the real-agent check
	restateThird  = "READMEにも書いて"
	restateAgain  = "最初に頼んだ、invoices/ の請求書ごとに合計を検算するスクリプトを check-totals.sh に書いて、ずれていたら直し、全部通ることを見せてください。"
	restateOther  = "来週の打ち合わせの議題を、今週の作業の振り返りと、次の版で入れたいことの二つに分けて、箇条書きで用意してください。"
)

// restateTurns runs three requests with their Stops in the session and returns the first one's turn id and done-as-asked trace.
func restateTurns(t *testing.T, repo, root, session string) (string, string) {
	t.Helper()
	turn, done := "", ""
	for i, prompt := range []string{restateFirst, restateSecond, restateThird} {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		if i > 0 { // each short request is its own (the agent organizes the turn)
			restateSubmit(t, root, session, "")
		}
		saveFixtureReflection(t, root, feedbackScope(repo))
		out := runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
		if reason, block := stopReason(t, out); block && strings.Contains(reason, "continues_previous") {
			t.Fatalf("request %d must not be asked for continues_previous: %s", i+1, out)
		}
		if i == 0 {
			s := loadSession(root, session)
			turn = s.TurnID
			for _, r := range s.Reviews {
				if r.Spec == "done-as-asked" {
					done = r.Trace
				}
			}
		}
	}
	if turn == "" || done == "" {
		t.Fatal("the first request must have a turn and a done-as-asked decision")
	}
	return turn, done
}

func restateSubmit(t *testing.T, root, session, restates string) {
	t.Helper()
	payload := map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": false, "restates": restates}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatalf("plan submit with restates: %v", err)
	}
}

// movedProject sets up a project with a wiki page, then moves its folder (os.Rename, as mv does) and returns the old and new paths.
func movedProject(t *testing.T) (agent, old, moved string) {
	t.Helper()
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	old = feedbackScope(repo)
	root := filepath.Join(old, ".shikii")
	o, err := (notebook.Book{Root: root, Scope: old}).Capture(notebook.Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (notebook.Book{Root: root, Scope: old}).Ingest(notebook.Draft{Title: "a page", Lesson: "Check.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
	moved = filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	t.Chdir(moved)
	common, _ := agentHome()
	return common, old, moved
}

func registeredPaths(common string) []string {
	var out []string
	for _, p := range registeredProjects(common) {
		out = append(out, p.Path)
	}
	return out
}

// A layout-sensitive fake model: with the default layout it says change_request for everything (0.6); with plain it reads the text and gets it right (0.9). It adds the layout to the response model name (as the converter does).
func layoutSensitiveModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := "m"
		truth := "change_request"
		if strings.Contains(req.State, "why") {
			truth = "question"
		}
		p := 0.6
		if req.Layout == "plain" {
			name, p = "m+plain", 0.9
		} else {
			truth = "change_request"
		}
		resp := systemone.Response{Model: name, Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			ans := truth
			if strings.Contains(q.Instructions, "Anything goes") {
				ans = "other" // answer other to everything for deliberately bad wording (to make a counterexample)
			}
			probs := map[string]float64{"change_request": (1 - p) / 2, "question": (1 - p) / 2, "other": 0}
			probs[ans] = p
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: ans, Probabilities: probs, Confidence: p}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// tickReplies replaces the periodic check of replies in flight with one the test runs by hand: each call of the returned function runs
// the check of every reply in flight once, so the test decides when a calibration is looked at.
func tickReplies(t *testing.T) func() {
	t.Helper()
	var mu sync.Mutex
	live := map[*int]func(){}
	old := watchReply
	watchReply = func(check func()) func() {
		k := new(int)
		mu.Lock()
		live[k] = check
		mu.Unlock()
		return func() {
			mu.Lock()
			delete(live, k)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { watchReply = old })
	return func() {
		mu.Lock()
		checks := make([]func(), 0, len(live))
		for _, c := range live {
			checks = append(checks, c)
		}
		mu.Unlock()
		for _, c := range checks {
			c()
		}
	}
}

// pastReplyClock makes every write deadline a reply in flight sets from now on already past, as if its write timeout had run out
// before the reply writes. The returned function puts the clock back.
func pastReplyClock(t *testing.T) (back func()) {
	t.Helper()
	old := replyNow
	replyNow = func() time.Time { return time.Unix(1, 0) }
	back = func() { replyNow = old }
	t.Cleanup(back)
	return back
}

// expireWrite sets the reply's write deadline in the past, as if the server's write timeout had run out while the handler worked.
func expireWrite(t *testing.T, w http.ResponseWriter) {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Unix(1, 0)); err != nil {
		t.Error(err)
	}
}

type fetched struct {
	body string
	err  error
}

// fetchAsync sends a request and reads the whole reply in the background; the result arrives on the returned channel.
func fetchAsync(req func() (*http.Response, error)) <-chan fetched {
	got := make(chan fetched, 1)
	go func() {
		resp, err := req()
		var body []byte
		if err == nil {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		got <- fetched{string(body), err}
	}()
	return got
}

// pageOwnerTicksByHand makes a page started by the hook check that it is still the project's page only when the test sends a tick.
func pageOwnerTicksByHand(t *testing.T) chan<- time.Time {
	t.Helper()
	ticks := make(chan time.Time)
	old := pageOwnerTicks
	pageOwnerTicks = func() <-chan time.Time { return ticks }
	t.Cleanup(func() { pageOwnerTicks = old })
	return ticks
}

// pageListening tells when a page started by the hook listens and has written its record (listenPage opens the browser right after).
func pageListening(t *testing.T) <-chan struct{} {
	t.Helper()
	up := make(chan struct{}, 1)
	old := openBrowser
	openBrowser = func(string) { up <- struct{}{} }
	t.Cleanup(func() { openBrowser = old })
	return up
}

func memoryEffectFixture(t *testing.T) (store.Store, string, http.Handler, notebook.Draft, string) {
	t.Helper()
	repo := gitRepo(t)
	agent := hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	id, _ := reviewSeed(t, st, scope, 1)
	draft := notebook.Draft{Sources: []string{id}, Title: "README の導入確認", Lesson: "初期化していない場所で記録されないことも確認する。", Kind: "rule", Scene: "テストの時", AppliesTo: "README の導入手順を更新する時"}
	page, err := (notebook.Book{Root: agent, Scope: scope}).Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.ID = page.Page.ID
	return st, scope, newServer(serveOpts{storeDir: agent, projectScope: scope}), draft, page.Page.Revision
}

func memoryEffectPrompt(t *testing.T, scope, session string) string {
	t.Helper()
	return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "README の導入手順を更新してください"}))
}

func memoryEffectComplete(t *testing.T, st store.Store, scope, session string) string {
	t.Helper()
	id := loadSession(st.Dir, session).TurnID
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": scope, "last_assistant_message": "未初期化の場所では記録されないことを合成テストで確認しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "続いて画面の色を調整してください。"})
	return id
}

func memoryEffectStats(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	report, ok := q["memory_effect"].(map[string]any)
	if !ok {
		t.Fatal("the delivered memories and their effect on decisions can be checked from the reflection")
	}
	return report["stats"].(map[string]any)
}

func memoryEffectSubmit(t *testing.T, h http.Handler, id, key, result, decision, evidence string) map[string]any {
	t.Helper()
	return reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": result, "decision": decision, "evidence": evidence}}})
}

// takePageLock holds the project's page lock the way a running page does (from another open file, as another process would).
func takePageLock(t *testing.T, common, scope string) {
	t.Helper()
	path := filepath.Join(common, "pages", projectID(scope)+".lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

// Properties C1 to C4 of reconnecting a moved project (.shikii/handoff/audit-0.9.7/invariants.md), one named example per way a person
// handles project folders on one machine: move (mv), copy (cp -R), bring in a .shikii from elsewhere, make a request (the hook) and
// shikii --init. Each runs in a Git repository and in a plain folder (there the project is the folder itself, 0.9.7 full audit 1-1).
//   - C1: a store this machine initialized, moved and then asked for, is active at its new path.
//   - C2/C3: a copy, or a .shikii brought in, becomes active only with the person's shikii --init.
//   - C4: the shared index never names a store whose id or folder differs.
// They replace a test that ran random sequences of these steps 250 times: it caught some breakages only after 114 runs.

func relinkEachPlace(t *testing.T, run func(t *testing.T, common, p string)) {
	for _, place := range []string{"git", "plain folder"} {
		t.Run(place, func(t *testing.T) {
			p := t.TempDir()
			if place == "git" {
				p = gitRepo(t)
			}
			hookEnv(t, nil, p)
			common, _ := agentHome()
			run(t, common, feedbackScope(p))
		})
	}
}

func relinkMove(t *testing.T, from string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	return feedbackScope(to)
}

func relinkCopy(t *testing.T, from, name string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("cp", "-R", from, to).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	return feedbackScope(to)
}

func relinkActive(f string) bool {
	active, _ := projectActive(filepath.Join(f, ".shikii"), f)
	return active
}

// relinkIndexNamesItsStores checks C4: every entry of the shared index whose folder is an active project names that folder's store.
func relinkIndexNamesItsStores(t *testing.T, common string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, file := range files {
		b, _ := os.ReadFile(file)
		var p projectEntry
		if json.Unmarshal(b, &p) != nil {
			continue
		}
		if _, err := os.Stat(p.Path); err != nil {
			continue // a path no longer there is not listed (registeredProjects skips it)
		}
		root := filepath.Join(p.Path, ".shikii")
		if relinkActive(p.Path) && (p.StoreID != storeID(root, false) || p.StoreDir != storeDir(root)) {
			t.Fatalf("C4: the index entry for %s names another store: %+v (store id %s, folder %s)", p.Path, p, storeID(root, false), storeDir(root))
		}
	}
}

// restateFarTurns runs request 1 and the seven short requests (each organized as a request of its own, with its Stop) in repo, and returns
// request 1's turn id and done-as-asked trace.
func restateFarTurns(t *testing.T, repo, root, session string) (string, string) {
	t.Helper()
	turn, done := "", ""
	for i, prompt := range append([]string{restateFirst}, restateFarShort...) {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		if i > 0 {
			restateSubmit(t, root, session, "")
		}
		saveFixtureReflection(t, root, feedbackScope(repo))
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
		if i == 0 {
			s := loadSession(root, session)
			turn = s.TurnID
			for _, r := range s.Reviews {
				if r.Spec == "done-as-asked" {
					done = r.Trace
				}
			}
		}
	}
	if turn == "" || done == "" {
		t.Fatal("request 1 must have a turn and a done-as-asked decision")
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("request 1 must have no outcome before its restatement: %+v", l)
	}
	return turn, done
}

// Fake model: asked with many choices, it answers change_request to everything (bad). Asked in yes/no steps, it reads the text and gets it right (good).
// Counts the number of questions in one call and the number of calls.
func stepwiseModel(t *testing.T) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var calls, questions int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&calls, 1)
		atomic.AddInt32(&questions, int32(len(req.Questions)))
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			switch q.Type {
			case "noul":
				p := 0.1
				switch {
				case strings.Contains(q.Instructions, "work") && (strings.Contains(req.State, "fix") || strings.Contains(req.State, "why")):
					p = 0.9
				case strings.Contains(q.Instructions, "change") && strings.Contains(req.State, "fix"):
					p = 0.8
				}
				resp.Answers[k] = systemone.Answer{Type: "noul", Noul: p}
			default:
				probs := map[string]float64{"change_request": 0.8, "question": 0.1, "other": 0.1}
				resp.Answers[k] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: probs, Confidence: 0.8}
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &questions
}

// 0.9.2: when a wording candidate beats the current question, an agent (LLM), not the person, checks whether the meaning is the same and adds a Japanese translation.
// Same → adopt automatically, tell the person with the translation, and let them revert. Cannot decide → ask the person with the translation and the difference. Different → do not adopt.

// wordingCheckFixture puts one adoptable tune result on the hook question changes-in-scope. Adoption (calibration) is swapped for a fake.
func wordingCheckFixture(t *testing.T) (agent, repo string, res *tuneResult) {
	t.Helper()
	repo = gitRepo(t)
	agent = hookEnv(t, nil, repo)
	t.Chdir(repo)
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	sb, _ := os.ReadFile(settings)
	var cfg map[string]any
	_ = json.Unmarshal(sb, &cfg)
	cfg["language"] = "Japanese"
	out, _ := json.Marshal(cfg)
	if err := os.WriteFile(settings, out, 0o600); err != nil {
		t.Fatal(err)
	}
	sp, err := spec.Load(hookSpecPath(agent, "changes-in-scope"))
	if err != nil {
		t.Fatal(err)
	}
	v := spec.Variant{Name: "unasked-first", Question: "Did the agent change any file nobody asked for? Answer no if it did."}
	res = &tuneResult{SpecID: sp.ID, SpecPath: sp.Path(), SpecHash: sp.Hash(), Version: sp.Version, Model: "m", Truths: 50, At: "2026-10-25T10:00:00Z",
		Winner: v.Name, Adoptable: true, WinnerVariant: &v, WinnerWording: wordingOf(sp.WithVariant(v)), Current: wordingOf(sp),
		Candidates: []candidateResult{{Name: "current", TopOne: 0.321}, {Name: v.Name, TopOne: 0.786}}}
	runs := filepath.Join(agent, "wiki", "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	res.RunPage = filepath.Join(runs, "changes-in-scope-20261025-100000.md")
	res.Run = strings.TrimSuffix(filepath.Base(res.RunPage), ".md")
	if err := os.WriteFile(res.RunPage, []byte("# run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveTuneResult(res)
	old := wordingAdopt
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		base, err := os.ReadFile(r.SpecPath)
		if err != nil {
			return "", err
		}
		next := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(base), fmt.Sprintf("version: %d", r.Version+1))
		path := filepath.Join(filepath.Dir(r.SpecPath), fmt.Sprintf("%s-v%d.yaml", r.SpecID, r.Version+1))
		if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
			return "", err
		}
		r.Adopted, r.AdoptedPath = true, path
		return path, nil
	}
	t.Cleanup(func() { wordingAdopt = old })
	return agent, repo, res
}

func submitWordingCheck(t *testing.T, body map[string]any) (map[string]any, error) {
	t.Helper()
	b, _ := json.Marshal(body)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	var out string
	var err error
	out = capture(t, func() error { err = cmdHookVariants([]string{"check"}); return nil })
	var v map[string]any
	_ = json.Unmarshal([]byte(out), &v)
	return v, err
}

func checkBody(res *tuneResult, verdict string) map[string]any {
	return map[string]any{"spec": res.SpecID, "run": res.Run, "verdict": verdict,
		"current_translation": "依頼の範囲の中でファイルを変えたか", "candidate_translation": "頼まれていないファイルを変えたか", "difference": "聞く順だけが違う"}
}

// Audit 0.9.2 (behavior), medium 1 / medium 2 / low 1: a tune result (wiki/runs/<run>.json) keeps absolute paths
// from when it was written. After the project folder moved or was copied, every reader and writer must stay inside this project.

// rewriteRunJSON changes the run's json on disk as it was written (without the rebuilding loadTuneResult does).
func rewriteRunJSON(t *testing.T, agent, run string, change func(*tuneResult)) {
	t.Helper()
	path := filepath.Join(agent, "wiki", "runs", run+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var res tuneResult
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	change(&res)
	out, _ := json.MarshalIndent(&res, "", "  ")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

const movedStore = "/nonexistent/old-name/.shikii"

var noticeURL = regexp.MustCompile(`https?://\S+`)

// 0.9.3: restates reaches any earlier request of this session and project (the person decided: no limit on how far back; the agent judges
// which request it restates). hook plan query still shows only the recent turns; shikii looks the named request up in what it recorded
// for this session itself.

// restateFarShort are seven new requests, each under 40 characters (so request 1 gets no outcome from them) and not acknowledgements.
var restateFarShort = []string{"add a test too", "log the errors", "add a dry-run flag", "rename the output file", "sort the output by date", "add a header row", "write it in the README too"}

// personTruths counts the truths a person (or an outcome) decided. Machine truths such as an agreement of the decision model and the agent
// (source agreed, 0.11) are left out: they never stand for the person.
func personTruths(labels []store.Label) int {
	n := 0
	for _, l := range labels {
		if store.HumanSource(l.Source) {
			n++
		}
	}
	return n
}

// saveFixtureReflection completes required bookkeeping in fixtures whose subject is outcome/grouping behavior.
// Use the public submit path; tests of missing work never call this helper.
func saveFixtureReflection(t *testing.T, root, scope string) {
	t.Helper()
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	var reviews, wiki, memory []map[string]any
	sources, _ := q["sources"].([]any)
	for _, item := range sources {
		s := item.(map[string]any)
		id := s["source_material"].(map[string]any)["id"].(string)
		targets, _ := s["targets"].([]any)
		for _, v := range targets {
			target := v.(map[string]any)
			reviews = append(reviews, map[string]any{"source_id": id, "trace_id": target["trace_id"], "truth": "unknown", "reason": "synthetic fixture; no human assessment"})
		}
		wiki = append(wiki, map[string]any{"sources": []string{id}})
		memories, _ := s["memories"].([]any)
		for _, v := range memories {
			m := v.(map[string]any)
			memory = append(memory, map[string]any{"source_id": m["source_id"], "key": m["quote"].(map[string]any)["key"], "result": "unknown", "evidence": "synthetic fixture; no observed contribution"})
		}
	}
	if len(reviews)+len(wiki)+len(memory) == 0 {
		return
	}
	res := reflectCall(t, h, map[string]any{"operation": "submit", "reviews": reviews, "wiki": wiki, "memory": memory})
	if res["complete"] != true {
		t.Fatalf("fixture reflection must be saved: %v", res)
	}
}

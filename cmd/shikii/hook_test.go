package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// hookEnv puts the tools in a temporary HOME, prepares .shikii for the given work, and replaces the model and the page.
func hookEnv(t *testing.T, model *httptest.Server, roots ...string) (agent string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent = filepath.Join(home, ".shikii", "agent")
	oldLocal, oldPage, oldCal, oldTune, oldSpawn, oldOpen := hookLocalURL, hookPageURL, hookCalibrate, hookTune, hookSpawn, openBrowser
	hookCalibrate = runCalibrateInProcess // tests draw lines in-process (production starts a detached process)
	hookTune = runTuneInProcess
	spawned = nil
	hookSpawn = recordSpawn // real up / serve are not started (only recorded)
	opened = nil
	openBrowser = func(url string) { opened = append(opened, url) }
	hookLocalURL = "http://127.0.0.1:1" // a closed port (no model)
	if model != nil {
		hookLocalURL = model.URL
	}
	hookPageURL = "http://127.0.0.1:1"
	t.Cleanup(func() {
		hookLocalURL, hookPageURL, hookCalibrate, hookTune, hookSpawn, openBrowser = oldLocal, oldPage, oldCal, oldTune, oldSpawn, oldOpen // without restoring these, tests that check TestMain's replacements depend on test order (re-audit GH)
	})
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	spawned = nil // what install started is not counted
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
	// existing projects keep the old format even after an update. Prepare the state for that regression check.
	// the new format's inputs and paths are checked in hook_planning_test.go.
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

// spawned is the record of hookSpawn (tests). opened is the record of URLs opened in the browser.
var spawned, opened []string

// spawnedUps is only the local model starts (up) among those started (excluding the page's serve).
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

// Existing hook tests isolate their original guard/decision by supplying a
// completed reasoning prerequisite. Reasoning tests use runUnreviewedHook and
// exercise the real prepare/record flow, including missing evidence and recovery.
func runHook(t *testing.T, event string, in map[string]any) string {
	if event == "stop" {
		cwd, _ := in["cwd"].(string)
		session, _ := in["session_id"].(string)
		if root, _, err := projectStoreAt(cwd); err == nil {
			supplyReasoningFixture(root, session)
		}
	}
	return runUnreviewedHook(t, event, in)
}

func supplyReasoningFixture(root, session string) {
	if s, err := readSession(root, session); err == nil && s.Reasoning != nil {
		r := s.Reasoning
		entry := &reasoningEntry{workReasoning: workReasoning{Phase: "finish", Focus: "Synthetic prerequisite for another hook contract", Assessment: "retain", Reason: "This fixture isolates the original hook behavior.", Evidence: []string{"current"}, NextAction: "Check the hook under test.", Result: "The fixture supplies only the reasoning prerequisite."}, Generation: r.Generation}
		r.Start, r.Latest, r.Finish = entry, entry, entry
		// Storage-failure tests intentionally make this unavailable; the real
		// hook must observe that failure rather than the fixture ending the test.
		_ = saveSession(root, session, s)
	}
}

// runUnreviewedHook preserves every production prerequisite.
func runUnreviewedHook(t *testing.T, event string, in map[string]any) string {
	b, _ := json.Marshal(in)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	return strings.TrimSpace(capture(t, func() error { return cmdHook([]string{event}) }))
}

// fakeAgentModel is a fake that answers the hook's 4 specs: choice (prompt-kind) is edit 0.9, noul is decided by the function p(state) of the state text.
func fakeAgentModel(t *testing.T, p func(state string) float64) *httptest.Server {
	return fakeAgentModelEdit(t, p, func(string) float64 { return 0.9 })
}

// fakeAgentModelEdit is fakeAgentModel whose choice answer gives edit the probability edit(state) (the rest share what is left).
func fakeAgentModelEdit(t *testing.T, p func(state string) float64, edit func(state string) float64) *httptest.Server {
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
				e := edit(req.State)
				rest := (1 - e) / 10
				ans[k] = map[string]any{"type": "choice", "choice": "edit", "probabilities": map[string]float64{"edit": e, "explain": 4 * rest, "investigate": 3 * rest, "run": 2 * rest, "decide": rest}, "confidence": e}
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

// gitRepo is a temporary git repository (skip if git is missing).
func gitRepo(t *testing.T) string {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	return repo
}

// lowerMinN lowers min_n of a spec copied by install (as if the person edited the spec).
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

// runTuneInProcess is the in-process version of hookTune (for tests).
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

// runCalibrateInProcess runs calibration in-process in tests.
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

// treeSnapshot is only the hash of the working tree content (the first half of treeState).
func treeSnapshot(cwd string) string {
	h, _, _, _ := treeState(cwd)
	return h
}

// hookCommands checks what is registered in settings.json in tests.
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

// treeState reads the working tree state other than scope in tests.
func treeState(cwd string) (hash, listing string, listed bool, project string) {
	hash, listing, listed, project, _ = treeStateWithScope(cwd)
	return
}

// 0.9.2: shikii no longer writes into a person's AGENTS.md; 0.9.7: it no longer edits it either. A section an older version added is only
// told (compat_cleanup_test.go).

func agentsOld(heading string) string {
	return agentsMarker + "\n" + heading + "\n\n- old rules\n" + agentsMarker + "\n"
}

// Reproductions from the independent audit of 0.9.0 (added by the auditor; confirmed to fail before the fix).

// R1 (re-launching within 30 seconds of a refusal shifts by one) is not fixed, because SubagentStart has no call id and can only be matched by time and order;
// it is written as a limitation in integrations/claude-code.md

// doneTraceAfter runs one request and its Stop in a session and returns that request's done-as-asked trace.
func doneTraceAfter(t *testing.T, repo, root, session, prompt string) string {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
	for _, r := range loadSession(root, session).Reviews {
		if r.Spec == "done-as-asked" {
			return r.Trace
		}
	}
	t.Fatalf("the request must have a done-as-asked decision: %+v", loadSession(root, session))
	return ""
}

// 0.9.3 audit 3. Truths about done-as-asked come only from fixed rules on the person's message, never from the agent's
// continues_previous (the agent being judged must not be able to erase a restatement or manufacture a "done" truth).

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func correctionSeed(t *testing.T) (store.Store, string, http.Handler, string) {
	t.Helper()
	st, scope, h, draft, _ := memoryEffectFixture(t)
	body := map[string]any{"operation": "submit", "wiki": []map[string]any{{
		"id": draft.ID, "sources": draft.Sources, "title": draft.Title,
		"lesson": draft.Lesson, "applies_to": draft.AppliesTo,
		"correction": map[string]string{"source_id": draft.Sources[0], "quote": "まだ英語が残っています", "action": "変更した案内の全文を日本語で確認する"},
	}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("save the quote of the person's correction and the action to change next into the lesson")
	}
	ctx := memoryEffectPrompt(t, scope, "correction")
	if !strings.Contains(ctx, "まだ英語が残っています") || !strings.Contains(ctx, "変更した案内の全文を日本語で確認する") || len(ctx) > 1800 {
		t.Fatal("deliver the correction and the action, briefly, to the next similar request")
	}
	id := memoryEffectComplete(t, st, scope, "correction")
	item := memoryEffectItem(t, h, id)
	return st, scope, h, item["quote"].(map[string]any)["key"].(string)
}

// 0.9.3: a deferred outcome whose session was closed or killed mid-turn (no organizing, no Stop, no next request) is settled
// by any later hook of the project or a reflect query once it is older than deferralStale, with the provisional grouping, once.

// a long message on other work: settles the previous outcome as "yes" when it stands alone
const sweepReply = "Draft a migration guide for v0.8 users explaining how backups work now."

// doneLabelCount is how many label lines the trace has (LabeledTraces keeps only the newest).
func doneLabelCount(t *testing.T, root, trace string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "labels", "done-as-asked.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), `"`+trace+`"`)
}

// ageDeferral moves the waiting turn's start back past the limit (as if the session was left alone that long).
func ageDeferral(t *testing.T, root string) {
	t.Helper()
	if err := updateSession(root, "planning", func(s *hookSession) { s.Planning.At = time.Now().Add(-deferralStale - time.Minute) }); err != nil {
		t.Fatal(err)
	}
}

func deferredTurn(t *testing.T, reply string) (repo, root, done string) {
	t.Helper()
	repo, root, _ = planningFixture(t)
	done = continuesTurn(t, repo, root, reply)
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatalf("the outcome must wait for the organizing or the Stop: %+v", loadSession(root, "planning").Planning)
	}
	if _, err := os.Stat(deferralMarkerPath(root, "planning")); err != nil {
		t.Fatalf("an open deferral must have its marker: %v", err)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("not settled yet: %+v", l)
	}
	return repo, root, done
}

func otherPrompt(t *testing.T, repo string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "Explain how the page lists waiting answers."})
}

// the JSON the block asks for, as written in its reason
func askedJSON(t *testing.T, reason string) map[string]any {
	t.Helper()
	start := strings.Index(reason, `{"agreed"`)
	end := strings.Index(reason[max(start, 0):], `"}`)
	if start < 0 || end < 0 {
		t.Fatalf("the block must carry the JSON to pass: %s", reason)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(reason[start:start+end+2]), &payload); err != nil {
		t.Fatalf("the JSON in the block must be valid: %v (%s)", err, reason)
	}
	return payload
}

func guardAndGroupingOnce(t *testing.T, reply string) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	continuesTurn(t, repo, root, reply)
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("the first Stop blocks (guard and grouping ask): %s", out)
	}
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]bool{"unknown": true}, "open": map[string]bool{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
	if _, block := stopReason(t, out); block {
		t.Fatalf("the Stop after the block is not blocked, even with the guard still failing: %s", out)
	}
	if s := loadSession(root, "planning"); !s.Judged {
		t.Fatalf("the Stop after the block judges the turn: %+v", s)
	}
}

// Audit 0.9.2 (translation): the guard texts after the move to English.

// setHookLanguage writes language into the test HOME's ~/.claude/settings.json (after hookEnv installed the hooks there).
func setHookLanguage(t *testing.T, language string) {
	t.Helper()
	home, _ := os.UserHomeDir()
	settings := filepath.Join(home, ".claude", "settings.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(settings); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg["language"] = language
	b, _ := json.Marshal(cfg)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// newest is the first of the order Records returns (newest first).
func newest(recs []store.Record) *store.Record {
	if len(recs) == 0 {
		return nil
	}
	return &recs[0]
}

// pageGlossary reads the old words of the page's glossary (web/scripts/test-glossary.mjs, the first item of each row) for one language,
// so the notices the person reads in the terminal are held to the same words as the page.
func pageGlossary(t *testing.T, lang string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "scripts", "test-glossary.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "\n  "+lang+": [\n")
	if start < 0 {
		t.Fatalf("the glossary has no %s rows", lang)
	}
	end := strings.Index(src[start:], "\n  ],")
	if end < 0 {
		t.Fatalf("the %s rows of the glossary do not end", lang)
	}
	var words []string
	for _, m := range regexp.MustCompile(`(?m)^\s*\['([^']+)',`).FindAllStringSubmatch(src[start:start+end], -1) {
		words = append(words, m[1])
	}
	if len(words) < 10 {
		t.Fatalf("the %s glossary was not read: %q", lang, words)
	}
	return words
}

// noticeProblems lists what a notice to the person must not hold: a spec id, a raw probability (p=, p(yes)=), and the glossary's old words
// (Japanese also: act). The page link is removed first (its query names the spec, which the person does not read).
func noticeProblems(t *testing.T, text string, glossary map[string][]string) []string {
	t.Helper()
	en, ja, _ := strings.Cut(text, "\n")
	var found []string
	for lang, line := range map[string]string{"en": en, "ja": ja} {
		line = noticeURL.ReplaceAllString(line, "")
		if line == "" {
			continue
		}
		bad := append(append([]string{"p=", "p(", "p(はい)"}, hookSpecs...), wikiSpecs...)
		bad = append(bad, glossary[lang]...)
		if lang == "ja" {
			bad = append(bad, "act")
		}
		for _, w := range bad {
			if strings.Contains(strings.ToLower(line), strings.ToLower(w)) {
				found = append(found, lang+": "+w+" in "+line)
			}
		}
	}
	return found
}

// startSubagent runs from the parent passing a task to a child with the Agent tool (PreToolUse agent-task) to that child starting (SubagentStart) (0.9.1)
func startSubagent(t *testing.T, session, scope, agentID, agentType, prompt string) string {
	t.Helper()
	if out := runHook(t, "agent-task", map[string]any{"session_id": session, "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"description": "d", "prompt": prompt, "subagent_type": agentType}}); out != "" {
		t.Fatalf("the hook that keeps the child's task outputs nothing (does not stop the tool): %q", out)
	}
	return runHook(t, "subagent-start", map[string]any{"session_id": session, "cwd": scope, "agent_id": agentID, "agent_type": agentType})
}

func subagentContext(t *testing.T, out string) string {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("SubagentStart returns JSON: %q", out)
	}
	hs, _ := parsed["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "SubagentStart" {
		t.Fatalf("returns it in the SubagentStart context: %s", out)
	}
	ctx, _ := hs["additionalContext"].(string)
	return ctx
}

func memoryEffectItem(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	for _, raw := range q["sources"].([]any) {
		s := raw.(map[string]any)
		if s["source_material"].(map[string]any)["id"] == id {
			items, ok := s["memories"].([]any)
			if !ok || len(items) != 1 {
				t.Fatal("returns the quote actually delivered to that request together with the unassessed memory")
			}
			return items[0].(map[string]any)
		}
	}
	t.Fatal("even after scoring and the wiki are done, the unassessed memory is not forgotten")
	return nil
}

// Audit 0.9.2 (behavior), test holes: checks that survived when their line was broken on purpose.

// hookSettingsWith writes a settings file with one handler per command, each under its event, and returns its path.
func hookSettingsWith(t *testing.T, commands map[string]string) string {
	t.Helper()
	settings := filepath.Join(t.TempDir(), "settings.json")
	hooks := map[string]any{}
	for command, event := range commands {
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
		list, _ := hooks[event].([]any)
		hooks[event] = append(list, group)
	}
	b, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return settings
}

// readHookCommands is every handler's command in a settings file.
func readHookCommands(t *testing.T, settings string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, groups := range cfg.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				got[h.Command] = true
			}
		}
	}
	return got
}

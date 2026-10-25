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

// runHook runs an event with the given JSON on stdin and returns stdout. The hook never returns an error, whatever happens.
func runHook(t *testing.T, event string, in map[string]any) string {
	b, _ := json.Marshal(in)
	old := stdin
	stdin = strings.NewReader(string(b))
	defer func() { stdin = old }()
	return strings.TrimSpace(capture(t, func() error { return cmdHook([]string{event}) }))
}

// fakeAgentModel is a fake that answers the hook's 4 specs: choice (prompt-kind) is edit 0.9, noul is decided by the function p(state) of the state text.
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

// The band → behaviour table. In the shadow (no line) nothing is output, and a permission decision (ask / deny) is never output.
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
		{"changes unsure: to provisional scoring", "changes-in-scope", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.empty()
		}},
		{"done: silent", "done-as-asked", d(decide.Act, "yes", 0.9, true), func(o hookOut) bool { return o.empty() }},
		{"not done: a line for the person", "done-as-asked", d(decide.Discard, "yes", 0.2, true), func(o hookOut) bool {
			return o.Context == "" && strings.Contains(o.System, "unfinished") && strings.Contains(o.System, "0.20")
		}},
		{"done unsure: to provisional scoring", "done-as-asked", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
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
	// the hook output has no permission decision (and no context at Stop)
	if b := (hookOut{Context: "x"}).json("Stop"); b != nil {
		t.Fatalf("Stop must not return context (it would continue the conversation): %s", b)
	}
	if b := string((hookOut{Context: "x", System: "y"}).json("UserPromptSubmit")); !strings.Contains(b, `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(b, `"additionalContext":"x"`) || !strings.Contains(b, `"systemMessage":"y"`) || strings.Contains(b, "permissionDecision") {
		t.Fatalf("UserPromptSubmit output: %s", b)
	}
}

// The shadow period: ask the 4 right after the request and at the end of the turn, only record, and print nothing to stdout. Without a model: exit 0, no output, one line in hook.log.
func TestHookShadowRecordsAndStaysSilent(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 }) // noul is a confident "no" (discard: does not wait for an answer)
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
	// records carry the work marker (the name of the repo's root). It is not an input, so it is not in inputs or the hash
	for _, id := range hookSpecs {
		recs, _ := st.Records(id, 0)
		if recs[0].Project != filepath.Base(repo) || recs[0].Inputs["project"] != "" {
			t.Fatalf("%s: the record must carry the project mark %q outside the inputs: project=%q inputs=%v", id, filepath.Base(repo), recs[0].Project, recs[0].Inputs)
		}
	}
	// status shows counts per work
	if out := capture(t, func() error { return cmdStatus([]string{"--store", agent}) }); !strings.Contains(out, "by project: "+filepath.Base(repo)+"=1") {
		t.Fatalf("status must count records per project: %s", out)
	}
	daa, _ := st.Records("done-as-asked", 0)
	if daa[0].Inputs["summary"] != "Fixed the typo." || daa[0].Inputs["files"] != "a.go\nb.md" {
		t.Fatalf("done-as-asked inputs: %+v", daa[0].Inputs)
	}
	// a confident "no" does not wait for an answer. prompt-kind's edit 0.9 is ask without a line, but it gets a truth from outcomes so it does not wait for the person
	sess := loadSession(agent, "s1")
	if len(sess.Asked) != 0 {
		t.Fatalf("asked: %+v", sess.Asked)
	}

	// no model: do not stop (no output); the reason goes to hook.log
	hookLocalURL = "http://127.0.0.1:1"
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "s2", "cwd": repo, "prompt": "and now the other file"}); out != "" {
		t.Fatalf("a missing model must not produce output, got %q", out)
	}
	logb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
	if !strings.Contains(string(logb), "127.0.0.1:1") {
		t.Fatalf("hook.log must say why: %q", logb)
	}
	// a Stop in a session with no kept request does not ask the end-of-turn questions
	hookLocalURL = model.URL
	runHook(t, "stop", map[string]any{"session_id": "fresh", "cwd": repo, "last_assistant_message": "hello"})
	if recs, _ := st.Records("done-as-asked", 0); len(recs) != 1 {
		t.Fatalf("Stop without a request must not ask done-as-asked: %d records", len(recs))
	}
}

// The end-of-turn questions are asked only once per request. If nothing changed, changes-in-scope is not asked, only done-as-asked. Outside git the changed files are "unknown".
// changedFiles lists added, changed and removed files by name; filesInput cuts at 30.
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
	// even if Stop comes again for the same request (input starting with / or a resume), do not ask again
	runHook(t, "stop", map[string]any{"session_id": "once", "cwd": repo, "last_assistant_message": "It builds, again."})
	if count("done-as-asked") != 1 {
		t.Fatalf("a second Stop on the same request must not ask again: %d", count("done-as-asked"))
	}
	// a new request resets it. If something changed, changes-in-scope too
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
	// outside git: changes-in-scope is not asked, and done-as-asked's files is "unknown"
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

	// changedFiles: changed, added, removed (by name). Same content is not included. filesInput is 30 + the count of the rest
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

// UserPromptSubmit asks prompt-kind and needs-plan and keeps the request and the tree hash. The next request (or Stop if none) attaches edit if the tree changed,
// and !edit (weight 0.7) if not, as the outcome truth; Stop keeps the assistant's last message. Once truths reach min_n, Stop draws the line.
func TestHookPromptKindGetsOutcomeTruthAtStop(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.2 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	pkPath := lowerMinN(t, agent, "prompt-kind", 4)
	traceOf := map[string]string{} // session → prompt-kind trace
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
		// if changed, edit is attached and it is cleared. If not, !edit (partial) is attached and it is carried to the person's next request (so it can be replaced with edit if background work changes it later)
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
	// truths reached min_n (4), so Stop draws the line and writes it to the spec (these truths do not go through serve, so the hook draws it)
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
	// the session that changed gets edit (weight 1), the one that did not gets !edit (weight 0.7)
	if l := labels[traceOf["changed"]]; l.Truth != "edit" || l.Weight != 1 {
		t.Fatalf("the session that changed the tree must get edit w=1, got %+v", l)
	}
	if l := labels[traceOf["same"]]; l.Truth != "!edit" || l.Weight != 0.7 {
		t.Fatalf("the session that left the tree alone must get !edit w=0.7, got %+v", l)
	}
	// the 2nd UserPromptSubmit asks with previous (context): it must not fail. A short reply does not replace the request but is attached to the previous one (the end-of-turn questions do not read "ok" as the request)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": "and the other one"})
	if strings.Contains(out, "systemMessage") || loadSession(agent, "same").Request != "fix the typo in README same / and the other one" || loadSession(agent, "same").Judged {
		t.Fatalf("second prompt: out=%q sess=%+v", out, loadSession(agent, "same"))
	}
	long := "please also rewrite the README section about installation so that it mentions Docker"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": long})
	if loadSession(agent, "same").Request != long {
		t.Fatalf("a full request replaces the previous one: %+v", loadSession(agent, "same"))
	}
	// the request boundary is the next request: if the 1st request changed nothing and things changed after the 2nd, the 1st is !edit and the 2nd edit (waiting for Stop would make both edit)
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
	// a commit alone does not count as "changed" (the hash is the working tree content; HEAD and index are not included)
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
	// empty input and input starting with / are not judged
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "x", "cwd": repo, "prompt": "/model"}); out != "" || loadSession(agent, "x").Request != "" {
		t.Fatal("slash commands must be skipped")
	}
	// a long previous message is cut to 1500 bytes (without splitting multibyte characters)
	longJa := strings.Repeat("日本語の長い説明。", 300)
	runHook(t, "stop", map[string]any{"session_id": "same", "cwd": repo, "last_assistant_message": longJa})
	if prev := loadSession(agent, "same").Previous; len(prev) > requestMax || !utf8.ValidString(prev) || len(prev) < requestMax-3 {
		t.Fatalf("previous must be cut to %d bytes on a rune boundary, got %d bytes valid=%v", requestMax, len(prev), utf8.ValidString(prev))
	}
	// with stop_hook_active, do nothing
	if out := runHook(t, "stop", map[string]any{"session_id": "same", "cwd": repo, "stop_hook_active": true, "last_assistant_message": "loop"}); out != "" || loadSession(agent, "same").Previous == "loop" {
		t.Fatal("stop_hook_active must be a no-op")
	}
}

// Once a line is drawn, Stop speaks up: a confident "not finished" is one line in the terminal (no hookSpecificOutput). If the configured language is Japanese it is shown alongside and the record gets a display. In English there is no display.
func TestHookCalibratedStopSpeaksInTheConfiguredLanguage(t *testing.T) {
	// a fake that answers that a turn ending with "Done" is finished (0.9) and one with "half" is not (0.1)
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
	// English: the shadow record has no display
	setLang("English")
	turn("e1", "rename the config loader and update its three callers", "Done: renamed and updated the callers.")
	recs, _ := st.Records("done-as-asked", 0)
	if len(recs) != 1 || recs[0].Display != nil {
		t.Fatalf("English must leave no display: %+v", recs)
	}
	// Japanese: even in the shadow, the translated question and answer are put on the record (no output text)
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

	// after the line is drawn: "not finished" is one terminal line (English / Japanese side by side), no hookSpecificOutput, and the same text in the record's display.message
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
	// a finished turn stays silent
	if out := turn("j4", "bump the Go version in go.mod and CI", "Done: bumped both."); strings.Contains(out, "unfinished") {
		t.Fatalf("a finished turn must not be flagged: %s", out)
	}
	// switching back to English removes the side-by-side text
	setLang("English")
	if out := turn("e2", "move the helpers into their own package", "half: moved two helpers, the rest still import the old path."); strings.Contains(out, "見える") || !strings.Contains(out, "unfinished") {
		t.Fatalf("English setting must not add Japanese: %s", out)
	}
	// a noul answer reads はい / いいえ even without a display. Top is always yes and P is p(yes), so p<0.5 is "いいえ"
	if l := localize(&spec.Spec{Type: spec.Noul}, &decide.Decision{Top: "yes", P: 0.8}, "ja"); l.Top != "はい" {
		t.Fatalf("noul p=0.8 must read はい in Japanese, got %+v", l)
	}
	if l := localize(&spec.Spec{Type: spec.Noul}, &decide.Decision{Top: "yes", P: 0.1}, "ja"); l.Top != "いいえ" {
		t.Fatalf("noul p=0.1 must read いいえ in Japanese, got %+v", l)
	}
}

// The Stop summary: one line if there are waiting answers (the URL if the page is running, otherwise the start command), no output otherwise. Answered ones drop out of the count. The same count is not repeated every turn.
func TestHookStopSummarizesWhatWaitsForAPerson(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.6 }) // noul is unsure (ask): all wait for an answer
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd}); out != "" {
		t.Fatalf("nothing waits yet: %q", out)
	}
	// the person checks one needs-plan item. prompt-kind gets truths from outcomes, so it does not wait for the person even when unsure. Stop's completion check waits for the AI's provisional scoring and the outcome.
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": cwd, "prompt": "add a --json flag to the status command"})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd, "last_assistant_message": "Added."})
	if !strings.Contains(out, "1 of this session's decisions wait") || !strings.Contains(out, pageNotUp) || strings.Contains(out, "Start the page") {
		t.Fatalf("the line must always say where to answer (the page is started in the background; here it never comes up): %s", out)
	}
	if len(spawned) != 1 || !strings.HasPrefix(spawned[0], "serve --store "+filepath.Join(cwd, ".shikii")+" --port 0 --local ") {
		t.Fatalf("the first request starts this project's page once on a free port; the Stop does not start a second one: %v", spawned)
	}
	if strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("Stop must not return hookSpecificOutput (it would continue the conversation): %s", out)
	}
	// if no decision newly started waiting, the same count is not repeated every turn (the 2nd Stop of the same request does not ask the questions either)
	if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": cwd}); out != "" {
		t.Fatalf("nothing new: the line must not repeat: %s", out)
	}
	// the page is running → not started. Answer 1; when the next request adds 1, 1 remains.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	t.Cleanup(page.Close)
	common, _ := agentHome() // this project's page marker (0.8.0: on a free port)
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

// Without a model the hook starts `shikii up` in the background (once per 10 minutes). install also puts the rules (skill) in place and starts the model. No moment where the person types a command.
func TestHookStartsTheModelAndTheSkillByItself(t *testing.T) {
	oldLook := lookPath
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil } // as if on a machine with llama-server (runs on CI too)
	t.Cleanup(func() { lookPath = oldLook })
	cwd := t.TempDir()
	hookEnv(t, nil, cwd) // the model port is closed
	home, _ := os.UserHomeDir()
	if !fileExists(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md")) {
		t.Fatal("hook install must put the agent's skill in place")
	}
	// install writes the guard, so remove the guard first and watch how it starts right after a request
	_ = os.Remove(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "starting-model"))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": cwd, "prompt": "add a flag to the status command"})
	if ups := spawnedUps(); len(ups) != 1 { // starts of the page (serve) are counted separately (0.8.0)
		t.Fatalf("a missing model must start `shikii up` once: %v", spawned)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": cwd, "prompt": "and document it in the README as well"})
	runHook(t, "stop", map[string]any{"session_id": "m", "cwd": cwd})
	if len(spawnedUps()) != 1 {
		t.Fatalf("within %s the model must not be started again: %v", hookStartGap, spawned)
	}
	// after 10 minutes (the guard's time is old and the previous child is not alive), once more
	old := time.Now().Add(-hookStartGap - time.Minute)
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "starting-model"), []byte("0 "+old.Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": cwd, "prompt": "now run the tests and tell me what fails"})
	if len(spawnedUps()) != 2 {
		t.Fatalf("after the gap the model may be started again: %v", spawned)
	}
	// install's output has no command for the person to type
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if strings.Contains(out, "shikii serve --store") || !strings.Contains(out, "started in the background") {
		t.Fatalf("install must not tell the person to start anything by hand: %s", out)
	}
}

// Catches mutations the audit did not kill: a needs-plan unsure is also one line, the session_id path is sanitized, a broken settings.json is not touched,
// and after a line is drawn UserPromptSubmit returns the context with the correct event name.
func TestHookAuditGaps(t *testing.T) {
	if hookEventName("user-prompt-submit") != "UserPromptSubmit" || hookEventName("stop") != "Stop" {
		t.Fatal("event names must match Claude Code's")
	}
	// session_id stays inside sessions/
	if p := sessionPath("/a", "../../etc/passwd"); filepath.Dir(p) != filepath.Join("/a", "sessions") {
		t.Fatalf("session path must stay inside sessions/: %s", p)
	}
	if p := sessionFilesPath("/a", "s"); p != filepath.Join("/a", "sessions", "s.files") {
		t.Fatalf("files path: %s", p)
	}
	// cut does not split multibyte characters
	if s := cut("日本語の依頼", 7); s != "日本" {
		t.Fatalf("cut must not split a rune: %q", s)
	}
	// an old version's pre-tool-use exits 0 silently (even if left in settings, Claude Code shows no error)
	if out := capture(t, func() error { return cmdHook([]string{"pre-tool-use"}) }); out != "" {
		t.Fatalf("pre-tool-use must be a silent no-op: %q", out)
	}

	// a fake that answers that only requests containing "redesign" need the approach agreed
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "redesign") {
			return 0.9
		}
		return 0.1
	})
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	// a calibrated needs-plan comes back as UserPromptSubmit context (with a wrong event name Claude Code drops it)
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

	// a broken settings.json is not touched and an error is returned. An empty file is the same as {}
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
	if cmds := hookCommands(settings); len(cmds) != 4 {
		t.Fatalf("an empty settings.json must be treated as {}: %v", cmds)
	}
}

// install only shows → --yes writes. The 2nd time adds nothing. Other hooks stay. The old version's PreToolUse shikii hook is removed. uninstall removes only its own. Only missing specs are copied.
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
	if len(cmds) != 4 || !strings.HasSuffix(cmds[0], " hook agent-task") || !strings.HasSuffix(cmds[1], " hook stop") || !strings.HasSuffix(cmds[2], " hook subagent-start") || !strings.HasSuffix(cmds[3], " hook user-prompt-submit") || !filepath.IsAbs(strings.Fields(cmds[0])[0]) {
		t.Fatalf("hooks after install must run the binary by its absolute path: %v", cmds)
	}
	// the hook that keeps child tasks (0.9.1) is attached only to PreToolUse for Agent / Task, and does not join someone else's PreToolUse group (rtk's Bash)
	{
		var cfg map[string]any
		b, _ := os.ReadFile(settings)
		_ = json.Unmarshal(b, &cfg)
		agentGroup, rtkGroup := false, false
		for _, g := range cfg["hooks"].(map[string]any)["PreToolUse"].([]any) {
			gm := g.(map[string]any)
			for _, h := range gm["hooks"].([]any) {
				c := h.(map[string]any)["command"].(string)
				if strings.HasSuffix(c, " hook agent-task") {
					agentGroup = gm["matcher"] == "Agent|Task" && len(gm["hooks"].([]any)) == 1
				}
				if c == "rtk hook claude" {
					rtkGroup = gm["matcher"] == "Bash" && len(gm["hooks"].([]any)) == 1
				}
			}
		}
		if !agentGroup || !rtkGroup {
			t.Fatalf("agent-task must be its own PreToolUse group matching Agent|Task, and the other group must stay as it was:\n%s", b)
		}
	}
	// Codex has no SubagentStart, so only 2 (0.7.3)
	if codex := hookCommands(filepath.Join(home, ".codex", "hooks.json")); len(codex) != 2 || strings.Contains(strings.Join(codex, " "), "subagent-start") || strings.Contains(strings.Join(codex, " "), "agent-task") {
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

	// 2nd time: nothing added, a spec the person edited is not overwritten
	mine := filepath.Join(home, ".shikii", "agent", "decisions", "needs-plan.yaml")
	if err := os.WriteFile(mine, []byte("id: needs-plan\n# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(settings); len(cmds) != 4 {
		t.Fatalf("second install must not duplicate: %v", cmds)
	}
	if !strings.Contains(out, "already there") {
		t.Fatalf("second install must say nothing changed: %s", out)
	}
	if b, _ := os.ReadFile(mine); string(b) != "id: needs-plan\n# mine\n" {
		t.Fatal("a spec the person edited must not be overwritten")
	}

	// a PreToolUse shikii hook left by an old version is removed by install (other PreToolUse hooks stay)
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
	if cmds := hookCommands(settings); len(cmds) != 4 || !strings.Contains(out, "hooks added") {
		t.Fatalf("install must remove the old pre-tool-use hook: %v\n%s", cmds, out)
	}
	if b, _ = os.ReadFile(settings); !strings.Contains(string(b), "rtk hook claude") || strings.Contains(string(b), "Bash|Edit") {
		t.Fatalf("the other PreToolUse hook must stay and the shikii group must go:\n%s", b)
	}

	// uninstall: only its own are removed, other hooks stay
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
	// the backup is of the version without shikii hooks: even with the settings.json after uninstall (no shikii), bak stays the first one
	if bb, _ := os.ReadFile(settings + ".bak-shikii"); string(bb) != other {
		t.Fatalf("the backup must stay the first pre-shikii version:\n%s", bb)
	}
	// when settings.json did not exist: install makes no backup, and the later uninstall does not back up the version with hooks
	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	s2 := filepath.Join(home2, ".claude", "settings.json")
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if fileExists(s2 + ".bak-shikii") {
		t.Fatal("a settings.json that shikii created must never be backed up with shikii's hooks in it")
	}
	// --help prints usage to stdout and is not an error
	if out := capture(t, func() error { return cmdHook([]string{"--help"}) }); !strings.Contains(out, "usage: shikii hook") || strings.Contains(out, "pre-tool-use") {
		t.Fatalf("--help must print the current usage: %s", out)
	}
}

// The 2nd audit: reading the tree state does not dirty the repo's index and .git/objects, parallel updates are not lost, spot checks also wait for an answer,
// and 50 truths for the same input do not try to draw a line (counted like calibrate).
func TestHookSecondAudit(t *testing.T) {
	// treeState: untracked file contents do not stay in .git/objects, and the real index is not touched. The listing contains the path
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

	// spot checks (1 in 20 act decisions) also wait for the person's answer
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

	// parallel updates are not lost (read → change → write inside the lock)
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

	// even with 3 truths for the same input (the same request text), they count as one per input, so no line is drawn (no ERROR written at every Stop)
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

// Codex: the same 2 in ~/.codex/hooks.json too (no matcher). Other hooks stay, the 2nd time adds nothing, the old PreToolUse is removed, and uninstall removes only its own.
// The stdin keys are the same as Claude Code's, so Codex's Stop (with turn_id) makes the same record.
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
	// the same replacements as hookEnv (HOME is the one decided above)
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

	// record the same way with Codex's stdin (with turn_id and model)
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

	// uninstall also removes only its own on the Codex side
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	b, _ = os.ReadFile(codexHooks)
	if len(hookCommands(codexHooks)) != 0 || !strings.Contains(string(b), "deny-check.sh") {
		t.Fatalf("uninstall must remove only shikii's Codex hooks:\n%s", b)
	}
}

// For a spec with wording variants, Stop runs tune in the background (without --write; adoption is the person's call) each time the person's truths reach min_n and grew by hookTuneEvery since last time.
// A spec without variants is not run. Nor when the truths did not grow.
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
	lowerMinN(t, agent, "prompt-kind", 2) // a spec without variants: not run even when truths pile up
	st := store.Store{Dir: agent}
	logHas := func(s string) int {
		lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
		return strings.Count(string(lb), s)
	}
	answer := func(i int) { // 1 request → one truth from the person each for needs-plan and prompt-kind
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
	// not run if they did not grow (stays 1 even if the same Stop repeats)
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
	// a spec without variants is not run even when truths pile up
	if logHas("auto-tune prompt-kind") != 0 {
		t.Fatal("a spec without variants has nothing to try: no auto-tune")
	}
	// adoption is the person's call: the spec's wording has not changed (variants stay)
	if b, _ := os.ReadFile(np); !strings.Contains(string(b), "name: short") || strings.Contains(string(b), "version: 2") {
		t.Fatalf("auto-tune must not adopt: %s", b)
	}
}

// Reproducing the audit (2026-10-19): if the next request B finished writing the record while request A's Stop waits for the model, Stop A's changes must not hit B's record
// (B would become "judged", B's prompt-kind would get an outcome truth, and B's Stop would ask nothing).
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
	// Stop A starts: reads the record outside the lock and asks the model (does not write yet)
	snapshot := loadSession(agent, "r")
	in := hookInput{SessionID: "r", CWD: repo, LastAssistantMessage: "Fixed the typo."}
	_, muts, err := hookStop(agent, &in, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// meanwhile request B's UserPromptSubmit finishes (writes the record)
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
	// A's pending (A's trace) is settled
	if len(labels) != 1 {
		t.Fatalf("A's prompt-kind must be settled exactly once: %v", labels)
	}
	// B's turn: add README and Stop → done-as-asked is the 2nd
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

// Reproducing the audit: a session started from a subdirectory also sees the whole repo. A chmod-only change also counts as "changed". A hook.log line is cut.
// The background start guard is exclusive by flock (once out of 30 in parallel), and does not start past 10 minutes while the previous child is alive. The tune guard counts again when the spec's wording changes.
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
	// a mode-only change
	if err := os.Chmod(filepath.Join(repo, "other.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, modeChanged, _, _ := treeState(sub)
	if got := changedFiles(after, modeChanged); strings.Join(got, ",") != "other.txt" {
		t.Fatalf("a mode-only change must count: %v", got)
	}

	cwd := t.TempDir()
	agent := hookEnv(t, nil, cwd)
	// a hook.log line is cut to hookLogLineMax and newlines are folded (the model's response body is not kept whole)
	hookLog("line1\n" + strings.Repeat("x", 2000))
	lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
	last := lastLine(string(lb))
	if len(last) > hookLogLineMax+40 || strings.Contains(last, "line1\nx") {
		t.Fatalf("hook.log lines must be cut and single-line: %d bytes", len(last))
	}

	// startOnce: only once out of 30 in parallel, and with a live child (its own pid) it does not start even past the gap
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

	// the tune guard: when the wording (hash) changes, the previous count is not carried over
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
	// the wording changed (hash differs) → the previous 40 is not carried over, and it runs at 1 ≥ min_n(1)
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

// A background agent's completion notice (<task-notification>, streamed by Claude Code as a "user message") is not a request:
// the 2 pre-thinking questions are not asked and the request, listing and pending are not replaced. If work continues after the notice, at the end of that turn
// the original request is judged once more (the person's choice, 2026-10-20). Measured: nearly half of the records came from notifications.
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
	// work continued after the notice: at the end of that turn, judge scope and completion once more against the original request
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
	// without a notice, the 3rd Stop of the same request does not ask again
	runHook(t, "stop", map[string]any{"session_id": "bg", "cwd": repo, "last_assistant_message": "German README written, again."})
	if count("done-as-asked") != 2 {
		t.Fatalf("without a new notification the same request is not judged again: %d", count("done-as-asked"))
	}
	// outcome truth: the first Stop attaches "unchanged" (!edit, partial), but the files changed by the Stop after the notice, so
	// the same trace gets edit (weight 1) again and the newer one wins. The person's next request adds nothing
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

// Noticing without being told: if this project's agent memory (~/.claude/projects/<slug>/memory) has files not moved to the wiki,
// the hook before a request tells the agent to move them with the skill's steps (once per session). Files with the moved marker (.shikii/wiki <page id>) are not counted. In a partly moved folder the rest is shown.
// Non-.md files are not counted. In Codex's shared memory only files that name this project (the path, a folder name of 4+ characters as a word, the origin's name) are counted, and MEMORY.md is not looked at.
func TestHookNoticesAgentMemoryNotYetInTheWiki(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	home, _ := os.UserHomeDir()
	// the folder path: check Claude Code's rule (non-alphanumerics → -) with a fixed example (not a mirror test copying the same expression as the code. audit R4)
	os.MkdirAll(filepath.Join(home, ".claude", "projects", "-tmp-x-y-z", "memory"), 0o755)
	if got := agentMemoryDir("/tmp/x.y_z"); got != filepath.Join(home, ".claude", "projects", "-tmp-x-y-z", "memory") {
		t.Fatalf("the memory folder path: %q", got)
	}
	repo := filepath.Join(t.TempDir(), "mywork1") // the folder name has 4+ characters (becomes a naming word)
	os.MkdirAll(repo, 0o755)
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	initAt(t, repo)
	if dir := agentMemoryDir(repo); dir != "" {
		t.Fatalf("empty if there is no memory folder: %q", dir)
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
	// Codex's shared memory
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
	if !strings.Contains(out, "files not yet moved to the wiki: 1 in ") || !strings.Contains(out, "/memory (feedback-a.md)") || strings.Contains(out, "feedback-b.md") || strings.Contains(out, ".bak") || strings.Contains(out, "notes.json") || !strings.Contains(out, "import-memory.md") {
		t.Fatalf("counts only unmoved .md files (text that merely mentions the wiki is not a marker) and shows the skill's steps:\n%s", out)
	}
	if !strings.Contains(out, "; 3 in Codex's ") || !strings.Contains(out, "that name this project (by-path.md, by-repo-name.md, this-project.md)") || strings.Contains(out, "other-project.md") || strings.Contains(out, "perf-") {
		t.Fatalf("in Codex's shared memory, only .md files naming the project by path, folder name (as a word) or origin name are counted, not MEMORY.md or other projects' files:\n%s", out)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "m1", "cwd": repo, "prompt": "次は画面を直して"})
	if strings.Contains(out, "not yet moved to the wiki") {
		t.Fatalf("not said twice in the same session:\n%s", out)
	}
	os.WriteFile(filepath.Join(dir, "feedback-a.md"), []byte("---\nname: feedback-a\n---\n\n正本: .shikii/wiki fedcba9876543210\n"), 0o644)
	for _, name := range []string{"this-project.md", "by-repo-name.md", "by-path.md"} {
		b, _ := os.ReadFile(filepath.Join(codex, name))
		os.WriteFile(filepath.Join(codex, name), append(b, []byte("\n"+base+" では .shikii/wiki 0123456789abcdef に写した\n")...), 0o644)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "m2", "cwd": repo, "prompt": "次は画面を直して"})
	if strings.Contains(out, "not yet moved to the wiki") {
		t.Fatalf("not shown once everything is moved:\n%s", out)
	}
	// naming word boundaries: a short folder name (app) is not counted even as a word, only the path. "." and "/" are not naming words
	if names := projectNames(""); names != nil {
		t.Fatalf("an empty scope names nothing: %v", names)
	}
	if names := projectNames("/work/app"); len(names) != 1 || names[0] != "/work/app" {
		t.Fatalf("a short folder name names the project only by path: %v", names)
	}
	if namesProject("myapp の決まり", []string{"app"}) || !namesProject("app の決まり", []string{"app"}) || !namesProject("/work/app は古い", []string{"/work/app"}) {
		t.Fatal("naming matches at word boundaries; a path matches as a substring")
	}
}

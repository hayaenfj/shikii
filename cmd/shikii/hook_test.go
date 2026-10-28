package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/notebook"
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
			return o.Context == "" && strings.Contains(o.System, "outside what you asked") && strings.Contains(o.System, "90%")
		}},
		{"changes unsure: to provisional scoring", "changes-in-scope", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.empty()
		}},
		{"done: silent", "done-as-asked", d(decide.Act, "yes", 0.9, true), func(o hookOut) bool { return o.empty() }},
		{"not done: a line for the person", "done-as-asked", d(decide.Discard, "yes", 0.2, true), func(o hookOut) bool {
			return o.Context == "" && strings.Contains(o.System, "unfinished") && strings.Contains(o.System, "80%")
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
	// the request that changes nothing (session "same", truth !edit) gets a lower edit probability, so the probabilities tell the
	// answers apart and a line can be drawn (0.10: probabilities that say nothing draw no line)
	model := fakeAgentModelEdit(t, func(string) float64 { return 0.2 }, func(state string) float64 {
		if strings.Contains(state, "README same") {
			return 0.5
		}
		return 0.9
	})
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
	// the 2nd UserPromptSubmit asks with previous (context): it must not fail. A bare acknowledgement does not replace the request but is attached to the previous one (the end-of-turn questions do not read "ok" as the request)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "same", "cwd": repo, "prompt": "ok"})
	if strings.Contains(out, "systemMessage") || loadSession(agent, "same").Request != "fix the typo in README same / ok" || loadSession(agent, "same").Judged {
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
	if !strings.Contains(msg, "looks unfinished") || !strings.Contains(msg, "終えていないように見えます") {
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
	if out := turn("e2", "move the helpers into their own package", "half: moved two helpers, the rest still import the old path."); strings.Contains(out, "見えます") || !strings.Contains(out, "unfinished") {
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
	if cmds := hookCommands(settings); len(cmds) != 5 {
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
	if len(cmds) != 5 || !strings.HasSuffix(cmds[0], " hook agent-task") || !strings.HasSuffix(cmds[1], " hook stop") || !strings.HasSuffix(cmds[2], " hook subagent-start") || !strings.HasSuffix(cmds[3], " hook tool-lesson") || !strings.HasSuffix(cmds[4], " hook user-prompt-submit") || !filepath.IsAbs(strings.Fields(cmds[0])[0]) {
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
	if cmds := hookCommands(settings); len(cmds) != 5 {
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
	if cmds := hookCommands(settings); len(cmds) != 5 || !strings.Contains(out, "hooks added") {
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

	// parallel updates are not lost (read → change → write inside the lock): another hook holds the lock and saves its change at the
	// moment this one finds the lock busy (sessionLockBusy); this one must wait, then read, so both changes stay
	model := fakeAgentModel(t, func(string) float64 { return 0.6 })
	cwd := t.TempDir()
	agent := hookEnv(t, model, cwd)
	unlockOther, ok := lockSessionWithin(agent, "par", 0)
	if !ok {
		t.Fatal("the other hook's lock must be taken")
	}
	calls := 0 // the first busy notice only shows it waits; the other finishes at the second, so giving up after one wait is caught
	sessionLockBusy = func() {
		if calls++; calls != 2 {
			return
		}
		other := loadSession(agent, "par")
		other.Asked = append(other.Asked, askedItem{Spec: "done-as-asked", Trace: "0000000000000001"})
		if err := saveSession(agent, "par", other); err != nil {
			t.Error(err)
		}
		unlockOther()
	}
	t.Cleanup(func() { sessionLockBusy = nil })
	if err := updateSession(agent, "par", func(s *hookSession) {
		s.Asked = append(s.Asked, askedItem{Spec: "done-as-asked", Trace: "0000000000000002"})
	}); err != nil {
		t.Fatal(err)
	}
	sessionLockBusy = nil
	if calls < 2 {
		unlockOther()
		t.Fatal("an update must wait while another hook holds the session lock")
	}
	if got := loadSession(agent, "par").Asked; len(got) != 2 || got[0].Trace != "0000000000000001" || got[1].Trace != "0000000000000002" {
		t.Fatalf("both updates must survive, the waiting one read after the other's write: %+v", got)
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

	// startOnce: a second hook that arrives while the first is starting the thing (inside fn, before the pid is written) does not start it
	// again, and with a live child (its own pid) it does not start even past the gap
	calls, second := 0, true
	startOnce(agent, "thing", func() int {
		calls++
		second = startOnce(agent, "thing", func() int { calls++; return os.Getpid() })
		return os.Getpid()
	})
	if calls != 1 || second {
		t.Fatalf("a hook arriving while another starts the thing must not start it again: %d starts, second=%v", calls, second)
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

// 0.9.3: only a bare acknowledgement is attached to the previous request. Japanese requests are short (most were under the old
// 40-character limit), so a Codex session of 19 requests became one conversation and reflection almost never ran.
// A short request with content starts a new conversation. Audit 3: what people type as a bare acknowledgement in another width or
// with a variation selector is still one.
func TestOnlyBareAcknowledgementsAreReplies(t *testing.T) {
	for _, s := range []string{"y", "Y", "ok", "OK.", "okay", "yes", "go ahead", "continue", "lgtm", "next",
		"\u306f\u3044", "\u7d9a\u3051\u3066", "\u6b21\u3078", "\u3084\u308a\u307e\u3057\u3087\u3046", "\u3067\u306f\u7d9a\u3051\u307e\u3057\u3087\u3046", "\u304a\u9858\u3044\u3057\u307e\u3059", "\u9032\u3081\u3066", "\u4e86\u89e3\u3002", "\u3044\u3044\u3088",
		"ＯＫ", "ｏｋ！", "👍️", "✅", "了解しました", "わかった", "OK️"} {
		if !isAckReply(s) {
			t.Errorf("%q is a bare acknowledgement and is attached to the previous request", s)
		}
	}
	for _, s := range []string{"and the other one", "fix it", "cold\u3092\u66f4\u306b\u9ad8\u901f\u5316", "\u307e\u3060cold\u524a\u308c\u305d\u3046",
		"\u66f4\u306b\u9ad8\u901f\u5316\u3059\u308b\u4f59\u5730\u306f\u3042\u308a\u307e\u3059\u304b\uff1f", "main\u53d6\u308a\u8fbc\u3093\u3067", "\u3053\u306e\u30d6\u30e9\u30f3\u30c1\u306b\u53d6\u308a\u8fbc\u3093\u3067\u3044\u3044\u3088", "1", "y\u3067\u3082\u3044\u3044\u3051\u3069\u30c6\u30b9\u30c8\u3082\u66f8\u3044\u3066",
		"ＯＫ、でもテストも", "２", "違う"} {
		if isAckReply(s) {
			t.Errorf("%q is a request of its own and starts a new conversation", s)
		}
	}
}

// 0.9.2: shikii no longer writes into a person's AGENTS.md; 0.9.7: it no longer edits it either. A section an older version added is only
// told (compat_cleanup_test.go).

func agentsOld(heading string) string {
	return agentsMarker + "\n" + heading + "\n\n- old rules\n" + agentsMarker + "\n"
}

// init does not add a section (a person's AGENTS.md stays byte for byte; none is created), and the update says to reopen the sessions.
func TestInitAndUpdateAddNoSection(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	plain, none := gitRepo(t), gitRepo(t)
	human := "# Office rules\n\n- Invoices go in invoices/.\n"
	os.WriteFile(filepath.Join(plain, "AGENTS.md"), []byte(human), 0o644)
	for _, d := range []string{plain, none} {
		t.Chdir(d)
		capture(t, func() error { return cmdInit(nil) })
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("init adds nothing to a person's AGENTS.md: %q", b)
	}
	if _, err := os.Stat(filepath.Join(none, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("init does not create AGENTS.md")
	}
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	// 0.9.1 and earlier read pages written by 0.9.2 (English fixed words) as changed and refuse every save (as 0.8 did with 0.9.0's kind and scene)
	if !strings.Contains(out, "reopen") || !strings.Contains(out, "0.9.2 or later") {
		t.Fatalf("the update says to reopen open sessions and move every machine using the same wiki to 0.9.2 or later: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("the update leaves AGENTS.md alone: %q", b)
	}
}

// 3. The hook install output (without --yes) shows the projects to be wiped.
func TestHookInstallPreviewListsTheProjectsItWouldWipe(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600) // the --init mark from 0.7.3 and earlier
	os.Remove(installedVersionPath(common))
	// Audit 0.8.1-1 item 15: also show, with a count, that work records (half-written files that cannot be recreated) are wiped
	os.MkdirAll(filepath.Join(repo, ".shikii", "handoff", "job"), 0o700)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "job", "a.md"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "job", "b.md"), []byte("b"), 0o600)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "c.md"), []byte("c"), 0o600) // 2 entries directly under it, 3 files (count files, as the list does)
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, "is removed and initialized again") || !strings.Contains(out, repo) {
		t.Fatalf("the output shows what will be wiped:\n%s", out)
	}
	if !strings.Contains(out, "3 work records (.shikii/handoff/) are removed too") {
		t.Fatalf("show, with a count, that work records are wiped too:\n%s", out)
	}
	if _, err := os.Stat(installedVersionPath(common)); err == nil {
		t.Fatal("showing alone does not write the mark")
	}
	writeInstalledVersion(common, currentVersion())
	if out = capture(t, func() error { return cmdHook([]string{"install"}) }); strings.Contains(out, "消して初期化し直します") {
		t.Fatalf("not shown when no version boundary is crossed:\n%s", out)
	}
}

// Reproductions from the independent audit of 0.9.0 (added by the auditor; confirmed to fail before the fix).

// B1: if even one page in the wiki was edited by hand, Ingest refuses everything, so classify always fails. Yet the hook asks for 5 pages every time and Stop blocks every turn.
func TestAudit090ClassifyAskedOnlyWhenPossible(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	src := fmt.Sprintf("%016x", 900)
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "前の版の記事", Lesson: "前の版の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(a.Page.ID); err != nil {
		t.Fatal(err)
	}
	b, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "人が手で直す記事", Lesson: "手で直す教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "wiki", "lessons", "*", b.Page.ID+".md"))
	if len(matches) != 1 {
		t.Fatalf("page file not found: %v", matches)
	}
	f, _ := os.OpenFile(matches[0], os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("\n人のメモ\n")
	f.Close()
	_, cerr := book.Classify(a.Page.ID, "rule", "作業する時")
	stops := 0
	for i := 0; i < 2; i++ {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "z", "cwd": repo, "prompt": fmt.Sprintf("README を直して %d", i)})
		if s := runHook(t, "stop", map[string]any{"session_id": "z", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(s, "need a kind and a scene") {
			stops++
		}
	}
	if cerr != nil && stops > 0 {
		t.Fatalf("classify always fails (%v), yet Stop blocked %d turns", cerr, stops)
	}
}

// D1: classify overwrites the page's change reason (change_reason) with "gave kind and scene (0.9.0)", and the previous reason disappears from the page's display.
func TestAudit090ClassifyKeepsChangeReason(t *testing.T) {
	agent, scope, _, src := audit083Setup(t)
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "理由のある記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{ID: a.Page.ID, Sources: []string{src}, Title: "理由のある記事", Lesson: "直した教訓。", AppliesTo: "a", ChangeReason: "人の訂正で範囲を狭めた"}); err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(a.Page.ID); err != nil {
		t.Fatal(err)
	}
	res, err := book.Classify(a.Page.ID, "rule", "作業する時")
	if err != nil {
		t.Fatal(err)
	}
	if res.Page.ChangeReason != "人の訂正で範囲を狭めた" {
		t.Fatalf("the change reason was overwritten: %q", res.Page.ChangeReason)
	}
}

// R1 (re-launching within 30 seconds of a refusal shifts by one) is not fixed, because SubagentStart has no call id and can only be matched by time and order;
// it is written as a limitation in integrations/claude-code.md

// The parent called the Agent tool but the child never started (the person refused, etc.); a minute later a child of the same type is
// started for different work. The refused note must not choose that child's lessons.
//   - G1: both calls have the same subagent_type: the child was chosen with the text of the refused work.
//   - R2: the refused call has no type (it starts as general-purpose) and the later one says general-purpose: the discard compared types
//     raw ("" and "general-purpose" looked different), so the refused note stayed and was used by the later child.
func TestRefusedAgentTaskIsNotUsedByALaterChild(t *testing.T) {
	for _, c := range []struct{ name, refused, child string }{
		{"same type", "Explore", "Explore"},
		{"no type, then general-purpose", "", "general-purpose"},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, scope, _, _, _ := memoryEffectFixture(t)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "g1", "cwd": scope, "prompt": "画面の色を直して"})
			input := map[string]any{"prompt": "断られた仕事 AAA の調査"}
			if c.refused != "" {
				input["subagent_type"] = c.refused
			}
			// the refused Agent tool (only PreToolUse arrives)
			runHook(t, "agent-task", map[string]any{"session_id": "g1", "cwd": scope, "tool_name": "Agent", "tool_input": input})
			// the time until the person refuses and the parent reconsiders (1 minute), within the 10-minute limit
			if err := updateSession(st.Dir, "g1", func(s *hookSession) {
				for i := range s.SubagentTasks {
					s.SubagentTasks[i].At = s.SubagentTasks[i].At.Add(-time.Minute)
				}
			}); err != nil {
				t.Fatal(err)
			}
			ctx := subagentContext(t, startSubagent(t, "g1", scope, "child-2", c.child, "README の導入手順を更新してください"))
			if !strings.Contains(ctx, "README の導入手順") {
				t.Fatalf("the started child is chosen by the text of its own work. heading: %q\nremaining notes: %+v", strings.SplitN(ctx, "\n", 2)[0], loadSession(st.Dir, "g1").SubagentTasks)
			}
		})
	}
}

// R3: history was removed before listing, so it also disappeared from the base used to count "rare words" (words in at most half the pages).
// With 2 history pages, README was a rare word in 2 of 4 pages, but after removing them it is in 2 of 2, a common word, and the rule delivered in 0.9.0 is no longer delivered
func TestReaudit091HistoryExclusionKeepsRarity(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	r := draft
	r.ID, r.Title, r.Lesson = "", "README の書き方", "README は英語と日本語の 2 つだけにする。"
	if _, err := book.Ingest(r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		h := draft
		h.ID, h.Kind, h.Scene = "", "history", "画面を直す時"
		h.Title = fmt.Sprintf("画面の色を変えた経緯 %d", i)
		h.AppliesTo = "画面の色を変える時"
		h.Lesson = fmt.Sprintf("画面の色の経緯その %d。", i)
		if _, err := book.Ingest(h); err != nil {
			t.Fatal(err)
		}
	}
	pages, err := book.Recall("README を直す")
	titles := []string{}
	for _, p := range pages {
		titles = append(titles, p.Title)
	}
	if err != nil || len(pages) == 0 {
		t.Fatalf("even with history excluded, do not change the base for rare words (as in 0.9.0, the README rule is among the candidates): %v %v", err, titles)
	}
}

// G2: start 2 children of the same type side by side. SubagentStart chooses from the same copy read outside the lock, so both pick the same note and one note remains
func TestAudit091ParallelSubagentsPickDistinctTasks(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g2", "cwd": scope, "prompt": "画面の色を直して"})
	for _, p := range []string{"README の導入手順を更新してください ONE", "README の導入手順を更新してください TWO"} {
		runHook(t, "agent-task", map[string]any{"session_id": "g2", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": p, "subagent_type": "Explore"}})
	}
	agent := st.Dir
	snapshot := loadSession(agent, "g2") // the copy the 2 hooks read at the same time
	o1, m1, err1 := hookSubagentStart(agent, &hookInput{SessionID: "g2", CWD: scope, AgentID: "c1", AgentType: "Explore"}, snapshot)
	o2, m2, err2 := hookSubagentStart(agent, &hookInput{SessionID: "g2", CWD: scope, AgentID: "c2", AgentType: "Explore"}, snapshot)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	for _, ms := range [][]func(*hookSession){m1, m2} {
		if err := updateSession(agent, "g2", func(s *hookSession) {
			for _, m := range ms {
				m(s)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	h1, h2 := strings.SplitN(o1.Context, "\n", 2)[0], strings.SplitN(o2.Context, "\n", 2)[0]
	left := loadSession(agent, "g2").SubagentTasks
	if h1 == h2 || len(left) != 0 || !strings.Contains(h1, "ONE") || !strings.Contains(h2, "TWO") { // c1 is ONE, c2 is TWO (so it does not pass even if one has no note; recheck GC)
		t.Fatalf("2 children started side by side use separate notes, and no note remains:\n c1=%q\n c2=%q\n remaining=%+v", h1, h2, left)
	}
}

// T6: Claude Code's language setting written in Japanese maps to ja.
func TestAuditTranslationLanguageNameInJapanese(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for language, want := range map[string]string{"\u65e5\u672c\u8a9e": "ja", "Japanese": "ja", " japanese ": "ja", "English": "", "": ""} {
		setHookLanguage(t, language)
		if got := hookLang(); got != want {
			t.Errorf("language %q: hookLang() = %q, want %q", language, got, want)
		}
	}
}

// audit 5 (0.9.3): a turn whose Stop asked for continues_previous is still judged when the Stop after the block never comes and the next request arrives
func TestAudit5AskedTurnIsJudgedWhenTheSecondStopNeverComes(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OKです")
	asked := loadSession(root, "planning").TurnID
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the Stop of a short message asks for continues_previous: %s", out)
	}
	// the person stopped the continuation (Esc): no Stop after the block, the next request comes
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	if s := loadSession(root, "planning"); s.TurnID == asked {
		t.Fatal("premise: the next request moves the turn on")
	}
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("the blocked turn must be judged with done-as-asked too: %d records", len(recs))
	}
}

// audit 5 (0.9.3): restates does not give "no" to a done request because a short command is said again after other work ("commit it" once more)
func TestAudit5RestatesIgnoresShortRepeats(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "planning"
	turn, done := "", ""
	for i, prompt := range []string{"コミットして", "README の導入も直して"} {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		restateSubmit(t, root, session, "")
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
	if done == "" {
		t.Fatal("premise: the first request has a done-as-asked decision")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "コミットして"})
	restateSubmit(t, root, session, turn)
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("a short repeated command must not label an older request: %+v", l)
	}
}

// audit 5 (0.9.3): copying the JSON in the block as it is keeps the list's provisional grouping (a copy alone does not attach the message)
func TestAudit5CopiedGroupingJSONKeepsTheListDecision(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "main取り込んで")
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main."})
	reason, block := stopReason(t, out)
	if !block {
		t.Fatalf("premise: the Stop blocks: %s", out)
	}
	if _, err := planningCall(t, "submit", askedJSON(t, reason)); err != nil {
		t.Fatal(err)
	}
	if s := loadSession(root, "planning"); !strings.HasPrefix(s.Request, "main取り込んで") {
		t.Fatalf("the copied JSON attached a short request to the previous one: %q", s.Request)
	}
}

// a session that moved to its next request between the sweep's first read and its lock keeps that request: the sweep must work on the
// record read inside the lock, never write back the copy it read before (test hole: the re-read was not caught, the dedup of outcome
// truths hid the second label and nothing checked the record)
func TestAudit4SweepDoesNotWriteBackItsFirstRead(t *testing.T) {
	repo, root, _ := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	const next = "Now add the upgrade notes for people on 0.8."
	old := sweepPeeked
	t.Cleanup(func() { sweepPeeked = old })
	sweepPeeked = func(id string) {
		sweepPeeked = func(string) {}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": id, "cwd": repo, "prompt": next})
	}
	sweepDeferrals(root, "")
	if s := loadSession(root, "planning"); s.Request != next {
		t.Fatalf("the sweep must not put back the record it read before the session moved on: request=%q", s.Request)
	}
}

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

// a short restatement (under 40 characters) still says the previous request was not done: the minimum is only for "done"
// (test hole: making the restatement need 40 characters too was not caught)
func TestAudit4ShortRestatementIsNotDone(t *testing.T) {
	const ask = "READMEのインストール手順を直して"
	repo, root, _ := planningFixture(t)
	done := doneTraceAfter(t, repo, root, "planning", ask)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": ask})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	// the first Stop asks for continues_previous (a short message outside the list); without it, the Stop after the block settles
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again.", "stop_hook_active": true})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" || l.Weight != 1 {
		t.Fatalf("a short restatement must label the previous request as not done: %+v %v", l, ok)
	}
}

// a repeated acknowledgement is not a restatement: the same Japanese acknowledgement twice gets no "not done" (test hole: dropping the
// acknowledgement check in settleDone was not caught)
func TestAudit4RepeatedAckIsNotARestatement(t *testing.T) {
	const ack = "よろしくお願いします"
	repo, root, _ := planningFixture(t)
	done := doneTraceAfter(t, repo, root, "planning", ack)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": ack})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("an acknowledgement must not label the previous request: %+v", l)
	}
}

// another session's Stop sweeps too, as the documents say (test hole: removing the sweep from Stop was not caught)
func TestAudit4OtherSessionStopSweeps(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": repo, "last_assistant_message": "Explained."})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("another session's Stop must settle a stale deferral: %+v %v", l, ok)
	}
}

// a session that came back between the sweep's first read and its lock and opened a new deferral keeps it open: the sweep checks
// again inside the lock that the deferral is due (test hole: dropping that second check was not caught)
func TestAudit4SweepLeavesTheNewFreshDeferral(t *testing.T) {
	repo, root, _ := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	old := sweepPeeked
	t.Cleanup(func() { sweepPeeked = old })
	sweepPeeked = func(id string) {
		sweepPeeked = func(string) {}
		runHook(t, "stop", map[string]any{"session_id": id, "cwd": repo, "last_assistant_message": "Wrote them."})
		runHook(t, "user-prompt-submit", map[string]any{"session_id": id, "cwd": repo, "prompt": "Explain how the hook decides which wiki lessons to deliver with each request."})
		if !openDeferral(loadSession(root, id)) {
			t.Error("the new turn's outcome must wait for its organizing")
		}
	}
	sweepDeferrals(root, "")
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("a fresh deferral opened while the sweep waited for the lock must stay open")
	}
}

// audit 6 (0.9.3): a turn whose first Stop came without a reply, judged late at the next request, is not judged with the previous
// turn's reply (Previous still holds it)
func TestAudit6LateJudgmentDoesNotUseThePreviousTurnsReply(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OKです")
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": ""})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the Stop of a short message asks for continues_previous: %s", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("premise: the asked turn is judged late: %d records", len(recs))
	}
	for _, r := range recs {
		if r.Inputs["request"] == "OKです" && strings.Contains(r.Inputs["summary"], "Kept the filters") {
			t.Fatalf("the late judgment used the previous turn's reply as this turn's reply: %q", r.Inputs)
		}
	}
}

// audit 6 (0.9.3): a long request the person's guard stopped is not recorded at the Stop after the block as a short message whose
// grouping was asked for (the page shows the record's reason as the last result of the plan ritual)
func TestAudit6GuardStoppedLongTurnRecordsNoGroupingRitual(t *testing.T) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	long := "please also add a section about the archive format to the README and link it from the install steps"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": long})
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Committed."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the guard stops the turn: %s", out)
	}
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Committed.", "stop_hook_active": true})
	for _, r := range readGuardRecords(root) {
		if r.Guard == ritualPlan && strings.Contains(r.Reason, "short message") {
			t.Fatalf("a long request got a grouping ritual record it was never asked for: %+v", r)
		}
	}
}

// audit 6 (0.9.3): requests that leave the recent turns keep only what restates uses (id, the person's text, the done trace, the time):
// the agent's reply is not kept for the session's lifetime
func TestAudit6EarlierKeepsNoReply(t *testing.T) {
	repo, root, _ := planningFixture(t)
	restateFarTurns(t, repo, root, "planning")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateOther})
	p := loadSession(root, "planning").Planning
	if p == nil || len(p.Earlier) == 0 {
		t.Fatal("premise: older requests moved to Earlier")
	}
	for _, e := range p.Earlier {
		if e.Summary != "" {
			t.Fatalf("Earlier keeps the agent's reply, which restates never reads: %+v", e)
		}
	}
}

// audit 6 (0.9.3): once the Stop after the block has judged the asked turn, the turn is like any other: a completion notice whose
// Stop never comes does not make the next request judge the same request again (a plain turn is not judged again either)
func TestAudit6JudgedAskedTurnIsNotJudgedAgainAfterANotice(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OKです")
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
	if s := loadSession(root, "planning"); !s.Judged {
		t.Fatal("premise: the Stop after the block judges the turn")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>background job finished</task-notification>"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range recs {
		if r.Inputs["request"] == "OKです" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the asked turn was judged %d times (once by the Stop after the block)", n)
	}
}

// audit 6 (0.9.3, test holes J2-J4): a completion notice, a slash command or a child's prompt during the continuation after the block
// is not the person's next request: the asked turn is not judged late by it
func TestAudit6LateJudgmentWaitsForThePersonsRequest(t *testing.T) {
	for name, in := range map[string]map[string]any{
		"notice": {"prompt": "<task-notification>background job finished</task-notification>"},
		"slash":  {"prompt": "/compact"},
		"child":  {"prompt": "search the repository for the archive format", "agent_id": "child-1"},
	} {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, "OKです")
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
			in["session_id"], in["cwd"] = "planning", repo
			runHook(t, "user-prompt-submit", in)
			recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range recs {
				if r.Inputs["request"] == "OKです" {
					t.Fatalf("%s judged the asked turn as if the person had asked the next thing", name)
				}
			}
		})
	}
}

// 0.9.3 audit 3. Truths about done-as-asked come only from fixed rules on the person's message, never from the agent's
// continues_previous (the agent being judged must not be able to erase a restatement or manufacture a "done" truth).

// a short correction or answer ("wrong", "no", "2") is not a sign the previous request was done.
// 0.9.3 audit 4: the 40 for a "done" truth counts characters, not bytes. A Japanese message under 40 characters is over 40 bytes
// (3 bytes a character), so counting bytes would turn most short Japanese corrections and answers back into "done" truths.
func TestAudit3ShortReplyIsNotDoneYes(t *testing.T) {
	const underInCharacters = "違います、README の英語版の手順の方を直して" // 25 characters, 55 bytes
	if n := utf8.RuneCountInString(underInCharacters); n >= outcomeMovedMinRunes || len(underInCharacters) < outcomeMovedMinRunes {
		t.Fatalf("the fixture must be under the minimum in characters and over it in bytes: %d characters, %d bytes", n, len(underInCharacters))
	}
	for _, reply := range []string{"違う", "no", "2", "やり直して", underInCharacters} {
		t.Run(reply, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			done := continuesTurn(t, repo, root, reply)
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Redid it."})
			if l, ok := doneLabel(t, root, done); ok && l.Truth == "yes" {
				t.Fatalf("a short reply %q must not label the previous request as done: %+v", reply, l)
			}
		})
	}
}

// an organizing submitted while the Stop of the same turn is waiting for the model must not make that Stop drop the turn
func TestAudit3SubmitDuringStopKeepsTheJudgment(t *testing.T) {
	repo, root, _ := planningFixture(t)
	// a message of 40 characters or more: the Stop judges at once (a shorter one outside the list is first asked for continues_previous)
	continuesTurn(t, repo, root, "main を取り込んで、衝突があれば直し、テストが全部通ることを確かめてから知らせてください。")
	in := &hookInput{SessionID: "planning", CWD: repo, LastAssistantMessage: "Merged main."}
	snapshot := loadSession(root, "planning")
	_, muts, err := hookStop(root, in, snapshot) // the Stop has read its snapshot and asked the model (outside the lock)
	if err != nil {
		t.Fatal(err)
	}
	continuesSubmit(t, root, true) // the organizing lands before the Stop takes the lock
	if err := updateSession(root, "planning", func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	s := loadSession(root, "planning")
	if !s.Judged || s.Completed == nil || s.Completed.Request != s.Request {
		t.Fatalf("the Stop that judged this turn must keep its judgment and link the reply: judged=%v completed=%+v request=%q", s.Judged, s.Completed, s.Request)
	}
	b := mustRead(t, sessionFilesPath(root, "planning"))
	if !strings.HasPrefix(string(b), listingHeader(s.Request)) {
		t.Fatalf("the listing header must follow the kept request")
	}
}

// a turn whose organizing and Stop never came still settles the previous outcome at the next request (test hole: mutation M4)
func TestAudit3NextRequestSettlesDeferredOutcome(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。")
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("the outcome waits for the organizing or the Stop: %+v", l)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "ok"})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("the next request settles a deferred outcome: %+v %v", l, ok)
	}
}

// F2 control: answering what query returned from the short reflection request alone, everything is saved and left is empty.
func TestAuditFinalF2ShortReflectionAloneSubmits(t *testing.T) {
	_, scope, h, _, _ := memoryEffectFixture(t)
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	stop := func() {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		stop()
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	stop()
	short := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if !strings.Contains(short, "as earlier in this session") || !strings.Contains(short, "wrap up the reflection") {
		t.Fatalf("precondition: the second ask is the short form: %q", short)
	}
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	submit := map[string]any{"operation": "submit"}
	var reviews, memory []map[string]string
	var wiki []map[string]any
	for _, raw := range anySlice(q["sources"]) {
		s := raw.(map[string]any)
		id := s["source_material"].(map[string]any)["id"].(string)
		for _, tr := range anySlice(s["targets"]) {
			reviews = append(reviews, map[string]string{"source_id": id, "trace_id": tr.(map[string]any)["trace_id"].(string), "truth": "unknown", "reason": "\u4f1a\u8a71\u304b\u3089\u306f\u6c7a\u3081\u3089\u308c\u306a\u3044"})
		}
		if s["wiki_pending"] == true {
			wiki = append(wiki, map[string]any{"sources": []string{id}})
		}
		for _, m := range anySlice(s["memories"]) {
			item := m.(map[string]any)
			memory = append(memory, map[string]string{"source_id": item["source_id"].(string), "key": item["quote"].(map[string]any)["key"].(string), "result": "unknown", "decision": "", "evidence": "\u78ba\u304b\u3081\u305f\u7d50\u679c\u304c\u7121\u3044"})
		}
	}
	for _, raw := range anySlice(q["observations"]) {
		wiki = append(wiki, map[string]any{"sources": []string{raw.(map[string]any)["id"].(string)}})
	}
	submit["reviews"], submit["wiki"], submit["memory"] = reviews, wiki, memory
	r := reflectCall(t, h, submit)
	t.Logf("sent: reviews=%d wiki=%d memory=%d reply=%v", len(reviews), len(wiki), len(memory), r)
	if r["complete"] != true || r["left"] != nil {
		t.Fatalf("F2: a reflection sent from the short form alone was not saved: %v", r)
	}
}

func anySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// 5. The hook install output (without --yes) shows what is wiped and what is not (unknown entries, version 2) separately.
func TestHookInstallPreviewExplainsWhatItWouldSkip(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	old, odd, fresh := gitRepo(t), gitRepo(t), gitRepo(t)
	for _, d := range []string{old, odd, fresh} {
		initAt(t, d)
	}
	for _, d := range []string{old, odd} {
		os.WriteFile(filepath.Join(d, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+d+`"}`), 0o600)
	}
	os.WriteFile(filepath.Join(odd, ".shikii", "mystery"), []byte("?"), 0o600)
	os.Remove(installedVersionPath(common))
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, old) || !strings.Contains(out, "is removed and initialized again") {
		t.Fatalf("shows what will be wiped:\n%s", out)
	}
	if !strings.Contains(out, odd) || !strings.Contains(out, "not removed: ") || !strings.Contains(out, "mystery") {
		t.Fatalf("shows, with the reason, that a project with unknown entries is not wiped:\n%s", out)
	}
	if !strings.Contains(out, "done: "+fresh) {
		t.Fatalf("a project initialized with this version (version 2) shows as done:\n%s", out)
	}
}

// 4. The shared-store check compares the actual folders (the same even with a different spelling).
func TestSharedPlaceComparesByIdentity(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	upper := strings.ToUpper(home)
	if _, err := os.Stat(upper); err != nil || upper == home {
		t.Skip("case-sensitive FS")
	}
	if !isSharedPlaceScope(upper) {
		t.Fatal("HOME is a shared store even with a different spelling")
	}
	if !isSharedPlaceScope(filepath.Join(home, "sub", "..")) {
		t.Fatal("HOME/sub/.. is also HOME")
	}
	if isSharedPlaceScope(filepath.Join(home, "sub")) {
		t.Fatal("a folder under HOME is not a shared store")
	}
}

// re3-2 (F4, new by 3509108): the short reflection form lists correction_result as a field of every memory item, next to the values of
// result, without "for a delivered correction". An agent that only has the short form (after compaction) and fills every field it lists
// sends correction_result for an ordinary lesson; that item is refused. The full text qualifies it.
func TestRe3ShortFormCorrectionResultOnlyForCorrections(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "cr")
	id := memoryEffectComplete(t, st, scope, "cr")
	key := "lesson:" + draft.ID + ":" + revision
	submit := func(withCR bool) map[string]any {
		item := map[string]string{"source_id": id, "key": key, "result": "unknown", "decision": "", "evidence": "\u4e0d\u660e"}
		if withCR {
			item["correction_result"] = "unknown" // the short form lists the field with no condition
		}
		return reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{item}})
	}
	short := reflectAskShort("X reflect", []string{id})
	qualified := strings.Contains(short, "delivered correction")
	refused := submit(true)
	t.Logf("with correction_result: %v", refused)
	if refused["complete"] == true {
		t.Skip("an ordinary lesson accepts correction_result: no trap")
	}
	if ok := submit(false); ok["complete"] != true {
		t.Fatalf("control: without the field the same item is saved: %v", ok)
	}
	if !qualified {
		t.Fatalf("F4: the short form lists correction_result for every memory item, but an ordinary lesson refuses it (%v); the full text says it is for a delivered correction\nshort: %q", refused["errors"], short)
	}
}

// G2: an older shikii on this machine wrote a path-named entry for a project 0.9.7 already gave a store id. The entry is not read and not
// removed; the hook tells the agent once per session to have every shikii updated.
func TestOlderShikiiNextToThisOneIsOnlyTold(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	common, _ := agentHome()
	scope := feedbackScope(repo)
	prompt := func(session string) string {
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}))
	}
	if first := prompt("s0"); strings.Contains(first, "older shikii") {
		t.Fatalf("control: no older shikii, nothing told: %q", first)
	}
	if storeID(filepath.Join(scope, ".shikii"), false) == "" {
		t.Fatal("precondition: the store has its id")
	}
	legacy := filepath.Join(common, "projects", projectID(scope)+".json")
	b, _ := json.Marshal(map[string]any{"id": projectID(scope), "name": filepath.Base(scope), "path": scope, "url": "/p/" + projectID(scope) + "/"})
	if err := os.WriteFile(legacy, b, 0o600); err != nil {
		t.Fatal(err)
	}
	first, second := prompt("s1"), prompt("s1")
	if !strings.Contains(first, "older shikii") || !strings.Contains(first, "update every shikii") {
		t.Fatalf("G2: the hook tells the agent to have every shikii updated: %q", first)
	}
	if strings.Contains(second, "older shikii") {
		t.Fatalf("G2: once per session: %q", second)
	}
	if got, err := os.ReadFile(legacy); err != nil || string(got) != string(b) {
		t.Fatalf("G2: the older shikii's entry is left as it is: %q %v", got, err)
	}
	if paths := registeredPaths(common); len(paths) != 1 || paths[0] != scope {
		t.Fatalf("G2: the project is listed once, from its own entry: %v", paths)
	}
}

// G4: shikii never rewrites AGENTS.md. A section an older version added is left, and init and the update say how to remove it.
func TestOlderAgentsSectionIsOnlyTold(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	plain, withOld := gitRepo(t), gitRepo(t)
	human := "# Office rules\n\n- Invoices go in invoices/.\n"
	old := human + "\n" + agentsOld("## shikii (decision rules)")
	os.WriteFile(filepath.Join(plain, "AGENTS.md"), []byte(human), 0o644)
	os.WriteFile(filepath.Join(withOld, "AGENTS.md"), []byte(old), 0o644)
	t.Chdir(plain)
	if out := capture(t, func() error { return cmdInit(nil) }); strings.Contains(out, "AGENTS.md") {
		t.Fatalf("control: nothing said about an AGENTS.md without the section: %s", out)
	}
	t.Chdir(withOld)
	// init first: the update tells only the projects registered on this machine (a map here ran the update first about 1 time in 8)
	for _, step := range []struct {
		name string
		run  func() error
	}{{"init", func() error { return cmdInit(nil) }}, {"update", func() error { return cmdHook([]string{"install", "--yes"}) }}} {
		name, out := step.name, capture(t, step.run)
		if b, _ := os.ReadFile(filepath.Join(withOld, "AGENTS.md")); string(b) != old {
			t.Fatalf("G4 %s: AGENTS.md is left as it is: %q", name, b)
		}
		if !strings.Contains(out, "AGENTS.md: an older shikii added") || !strings.Contains(out, agentsMarker) {
			t.Fatalf("G4 %s: says how to remove the section by hand:\n%s", name, out)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("control: an AGENTS.md without the section is untouched: %q", b)
	}
}

// without an organizing, the list decides (today's behaviour), settled at the turn's Stop at the latest
func TestNoPlanKeepsListDecision(t *testing.T) {
	t.Run("content", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "main取り込んで")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main."})
		// the first Stop asked for continues_previous; the Stop after the block judges with the list's decision when none came
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main.", "stop_hook_active": true})
		if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
			t.Fatalf("no organizing: the list keeps it as its own request, got %q", s.Request)
		}
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("a short new request is too short to show the person moved on (audit 3): %+v", l)
		}
		// an organizing that arrives after the Stop judged the turn no longer moves the request
		if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": true}); err == nil {
			if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
				t.Fatalf("a late organizing must not regroup a judged turn: %q", s.Request)
			}
		}
	})
	t.Run("long", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
		if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" || l.Source != "outcome" {
			t.Fatalf("a long, different request settles the previous outcome by the Stop: %+v %v", l, ok)
		}
	})
	t.Run("ack", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "ok")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Continued."})
		if s := loadSession(root, "planning"); s.Request != continuesFirst+" / ok" {
			t.Fatalf("no organizing: the list attaches ok, got %q", s.Request)
		}
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("no organizing: an acknowledgement does not settle the previous outcome: %+v", l)
		}
	})
}

// continues_previous for another turn or another session changes nothing
func TestPlanContinuesOtherTurnOrSessionIgnored(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	oldTurn := loadSession(root, "planning").TurnID
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "main取り込んで"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "Write the release notes for the next version."})
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": repo, "last_assistant_message": "Wrote them."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "main取り込んで"})
	for _, p := range []map[string]any{
		{"session_id": "planning", "turn_id": oldTurn},
		{"session_id": "other", "turn_id": loadSession(root, "planning").TurnID},
	} {
		p["agreed"], p["open"], p["continues_previous"] = map[string]any{"unknown": true}, map[string]any{"unknown": true}, true
		if _, err := planningCall(t, "submit", p); err == nil {
			t.Fatalf("an organizing for another turn or session must be refused: %v", p)
		}
	}
	if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
		t.Fatalf("another turn's organizing must not regroup this one: %q", s.Request)
	}
	if s := loadSession(root, "other"); s.Request != "main取り込んで" {
		t.Fatalf("another session's organizing must not regroup this one: %q", s.Request)
	}
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

func TestCorrectionTracksBehaviorSeparatelyFromHelpfulness(t *testing.T) {
	for _, outcome := range []string{"followed", "repeated", "not_applicable", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			st, scope, h, key := correctionSeed(t)
			// Even after moving on to the next request after completion, what is evaluated is the request at the time of delivery.
			items, _ := st.MemoryReport(scope)
			id := items.All[0].SourceID
			entry := map[string]string{"source_id": id, "key": key, "result": "unknown", "correction_result": outcome, "decision": "案内全文の確認を行った", "evidence": "合成の変更差分と確認記録を照合した"}
			body := map[string]any{"operation": "submit", "memory": []map[string]string{entry}}
			for range 2 {
				if reflectCall(t, h, body)["complete"] != true {
					t.Fatal("save the observation of the action only once")
				}
			}
			q := reflectCall(t, h, map[string]any{"operation": "query"})
			summary := q["memory_effect"].(map[string]any)
			counts := summary["corrections"].(map[string]any)
			if counts["delivered"] != float64(1) || counts[outcome] != float64(1) || counts["pending"] != float64(0) || summary["stats"].(map[string]any)["helpful"] != float64(0) {
				t.Fatal("do not confuse an observation that followed the correction with one that helped, and do not increase the count on resend")
			}
			entry["correction_result"] = "changed"
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("refuse unknown categories and later rewrites")
			}
			entry["correction_result"] = map[string]string{"followed": "repeated", "repeated": "followed", "unknown": "followed", "not_applicable": "followed"}[outcome]
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("even if other existing fields are the same, do not repaint only the correction's evaluation later")
			}
			_, labels, _ := st.Counts("done-as-asked")
			if len(labels) != 0 {
				t.Fatal("do not mix observations of corrections into calibration")
			}
		})
	}
}

func TestCorrectionRequiresExactHumanReplyAndActualDelivery(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	for _, correction := range []map[string]string{
		{"source_id": draft.Sources[0], "quote": "人が言っていない訂正", "action": "全文を確認する"},
		{"source_id": "ffffffffffffffff", "quote": "まだ英語が残っています", "action": "全文を確認する"},
		{"source_id": draft.Sources[0], "quote": "まだ英語が残っています", "action": ""},
	} {
		data, _ := json.Marshal(map[string]any{"sources": draft.Sources, "title": draft.Title, "lesson": draft.Lesson, "applies_to": draft.AppliesTo, "correction": correction})
		var d notebook.Draft
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
		if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(d); err == nil {
			t.Fatal("a correction needs the original text of the person's reply in the same work and a concrete action")
		}
	}
	// Do not let an ordinary lesson be scored as an example of a person's correction taking effect.
	memoryEffectPrompt(t, scope, "ordinary")
	id := memoryEffectComplete(t, st, scope, "ordinary")
	key := memoryEffectItem(t, h, id)["quote"].(map[string]any)["key"].(string)
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "helpful", "correction_result": "followed", "decision": "確認した", "evidence": "合成の確認結果"}}}
	if reflectCall(t, h, body)["complete"] != false {
		t.Fatal("do not evaluate an action for a correction that was never delivered")
	}
}

func TestCorrectionKeepsDeliveredActionAfterLessonUpdate(t *testing.T) {
	st, scope, h, _ := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	book := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := book.Query("")
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(err)
	}
	page := v.Lessons[0]
	// Even an ordinary update that omits the correction does not lose the existing correction and source.
	source := page.Sources[0].ID
	updated, err := book.Ingest(notebook.Draft{ID: page.ID, Sources: []string{source}, Title: page.Title, Lesson: "新しい教訓に更新した。", Kind: "rule", Scene: "テストの時", AppliesTo: page.AppliesTo})
	if err != nil || updated.Page.Correction == nil || updated.Page.Correction.Action != page.Correction.Action {
		t.Fatal("an ordinary update does not delete the correction", err)
	}
	data, _ := json.Marshal(map[string]any{"id": page.ID, "sources": []string{source}, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo, "correction": map[string]string{"source_id": source, "quote": page.Correction.Quote, "action": "今後の別の行動"}})
	var d notebook.Draft
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	updated, err = book.Ingest(d)
	if err != nil || updated.Page.Revision == page.Revision {
		t.Fatal("an update to the action also makes a new version", err)
	}
	item := memoryEffectItem(t, h, id)
	correction := item["quote"].(map[string]any)["correction"].(map[string]any)
	if correction["action"] != page.Correction.Action {
		t.Fatal("a later action does not re-explain the correction delivered back then")
	}
}

func TestCorrectionPendingAndUnknownDoNotClaimImprovement(t *testing.T) {
	st, scope, h, key := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	counts := q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["pending"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("being delivered alone does not mean the correction took effect")
	}
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "unknown", "evidence": "行動の記録が無い"}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("the older input format is also accepted as unknown")
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	counts = q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["unknown"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("do not treat omission or silence as evidence of improvement")
	}
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

// another session's prompt settles a stale deferral with the provisional grouping, and the original session coming back adds nothing
func TestSweepStaleDeferralByOtherSession(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	otherPrompt(t, repo)
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" || l.Source != "outcome" {
		t.Fatalf("another session's prompt must settle a stale deferral: %+v %v", l, ok)
	}
	s := loadSession(root, "planning")
	if g := groupingOf(s); g == nil || !g.Settled || g.Joined || s.Request != sweepReply {
		t.Fatalf("settled with the provisional grouping (standing alone): %+v request=%q", g, s.Request)
	}
	if _, err := os.Stat(deferralMarkerPath(root, "planning")); !os.IsNotExist(err) {
		t.Fatalf("the marker must go once settled: %v", err)
	}
	// the original session comes back: its organizing, Stop and next request find it settled
	continuesSubmit(t, root, true)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Now add the upgrade notes for people on 0.8."})
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the outcome must be attached once, got %d", n)
	}
	if s := loadSession(root, "planning"); s.Completed != nil && s.Completed.Request == continuesFirst+" / "+sweepReply {
		t.Fatalf("a late organizing must not regroup a settled turn: %+v", s.Completed)
	}
}

// a reflect query (the CLI, MCP shikii_reflect and /v1/reflect all go through runReflect) settles a stale deferral too
func TestSweepStaleDeferralByReflectQuery(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	if _, err := runReflect(root, loadSession(root, "planning").Scope, reflectRequest{Operation: "query"}); err != nil {
		t.Fatal(err)
	}
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("a reflect query must settle a stale deferral: %+v %v", l, ok)
	}
}

// a deferral within the time is left for its own session (not settled, marker kept)
func TestSweepLeavesFreshDeferral(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	otherPrompt(t, repo)
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": repo, "last_assistant_message": "Explained."})
	if _, err := runReflect(root, loadSession(root, "planning").Scope, reflectRequest{Operation: "query"}); err != nil {
		t.Fatal(err)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("a deferral still waiting within the time must not be settled: %+v", l)
	}
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("the deferral must stay open for its own session")
	}
}

// a record that moved on (its turn was judged) no longer waits, whatever the time
func TestSweepMovedOnDeferral(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	if err := updateSession(root, "planning", func(s *hookSession) { s.Judged = true }); err != nil {
		t.Fatal(err)
	}
	otherPrompt(t, repo)
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("a deferral whose session moved on must be settled: %+v %v", l, ok)
	}
}

// the sweep uses the same fixed rules: a short reply or an acknowledgement left waiting gets no "yes"
func TestSweepKeepsOutcomeRules(t *testing.T) {
	for _, reply := range []string{"no", "merge main"} {
		t.Run(reply, func(t *testing.T) {
			repo, root, done := deferredTurn(t, reply)
			ageDeferral(t, root)
			otherPrompt(t, repo)
			if l, ok := doneLabel(t, root, done); ok {
				t.Fatalf("a short reply must not be labeled by the sweep: %+v", l)
			}
			if openDeferral(loadSession(root, "planning")) {
				t.Fatal("the grouping is still made final")
			}
		})
	}
}

// sweeps running at the same time settle a stale deferral once
func TestSweepConcurrentOnce(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			sweepDeferrals(root, "")
		}()
	}
	close(start)
	wg.Wait()
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("concurrent sweeps must attach the outcome once, got %d", n)
	}
}

// a deferral its own session settles between the sweep's first read and its lock is not settled again (the record is read again inside the lock)
func TestSweepRereadsInsideLock(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	old := sweepPeeked
	t.Cleanup(func() { sweepPeeked = old })
	sweepPeeked = func(id string) {
		if err := updateSession(root, id, func(s *hookSession) { settleGrouping(root, s) }); err != nil {
			t.Error(err)
		}
	}
	sweepDeferrals(root, "")
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the outcome must be attached once, got %d", n)
	}
}

// a sweep does not take a session another hook is holding (it is left for the next sweep)
func TestSweepSkipsLockedSession(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	unlock := lockSession(root, "planning")
	sweepDeferrals(root, "")
	unlock()
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("a session whose lock is held must be left alone: %+v", l)
	}
	sweepDeferrals(root, "")
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the next sweep settles it once, got %d", n)
	}
}

// settling a grouping makes it final: the Stop settles it and the next request does not settle it again (test hole: mutation M2)
func TestSettledGroupingIsNotSettledTwice(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Now add the upgrade notes for people on 0.8."})
	sweepDeferrals(root, "")
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the previous outcome must be attached once, got %d", n)
	}
}

// a late Stop of an earlier turn does not settle the grouping of the turn that came after it (test hole: mutation M17)
func TestStaleStopLeavesNewTurnGrouping(t *testing.T) {
	repo, root, _ := deferredTurn(t, sweepReply)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
	done2 := ""
	for _, r := range loadSession(root, "planning").Reviews {
		if r.Spec == "done-as-asked" {
			done2 = r.Trace
		}
	}
	if done2 == "" {
		t.Fatal("the second request must have a done-as-asked decision")
	}
	snapshot := loadSession(root, "planning") // a second Stop of this turn (after a notification) reads its snapshot
	_, muts, err := hookStop(root, &hookInput{SessionID: "planning", CWD: repo, LastAssistantMessage: "Also checked the dates."}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain how the hook decides which wiki lessons to deliver with each request."})
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("the new turn's outcome waits for its organizing")
	}
	if err := updateSession(root, "planning", func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("a late Stop of the earlier turn must not settle the new turn's grouping")
	}
	if l, ok := doneLabel(t, root, done2); ok {
		t.Fatalf("a late Stop must not settle the new turn's outcome: %+v", l)
	}
}

// 0.9.3: the outcome truth goes through AppendLabelsOnce, so a hook that proceeds without the session lock (after waiting 2 seconds)
// or retries after a failed save cannot add the same outcome twice.
func TestOutcomeTruthIsWrittenOnceThroughLabelTrace(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "once", "cwd": repo, "prompt": "update the README install steps for macOS and Linux users"})
	runHook(t, "stop", map[string]any{"session_id": "once", "cwd": repo, "last_assistant_message": "done"})
	st := store.Store{Dir: agent}
	recs, _ := st.Records("done-as-asked", 0)
	if len(recs) != 1 {
		t.Fatalf("one done-as-asked record, got %d", len(recs))
	}
	for i := 0; i < 3; i++ {
		if err := labelTrace(st, "done-as-asked", recs[0].TraceID, "yes", outcomeKeptWeight, "outcome"); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	all, _ := st.Labels("done-as-asked", recs[0].SpecHash, recs[0].Model)
	for _, l := range all {
		if l.TraceID == recs[0].TraceID && l.Source == "outcome" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the same outcome truth lands once, got %d", n)
	}
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

func TestStopAsksContinuesPreviousForShortUnlistedMessage(t *testing.T) {
	for _, reply := range []string{"OKです", "それでいこう"} {
		t.Run(reply, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, reply)
			out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
			reason, block := stopReason(t, out)
			if !block || !strings.Contains(reason, "continues_previous") || !strings.Contains(reason, "hook plan submit") || strings.Contains(reason, "hook plan query") {
				t.Fatalf("a short message outside the list without an organizing: the Stop asks once, only for continues_previous: %s", out)
			}
			if s := loadSession(root, "planning"); s.Judged || s.Request != reply {
				t.Fatalf("the turn is not judged before the grouping is known: judged=%v request=%q", s.Judged, s.Request)
			}
			// the JSON in the block is accepted with the agent's answer in it, and its continues_previous still moves the grouping
			payload := askedJSON(t, reason)
			payload["continues_previous"] = true
			if _, err := planningCall(t, "submit", payload); err != nil {
				t.Fatalf("the asked submit must be accepted: %v", err)
			}
			if s := loadSession(root, "planning"); s.Request != continuesFirst+" / "+reply {
				t.Fatalf("continues_previous after the block must attach the message: %q", s.Request)
			}
			// the Stop after the block is not blocked and judges the attached request
			out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
			if _, block := stopReason(t, out); block {
				t.Fatalf("a second Stop is not blocked: %s", out)
			}
			if s := loadSession(root, "planning"); !s.Judged || s.Completed == nil || s.Completed.Request != continuesFirst+" / "+reply {
				t.Fatalf("the Stop after the block judges the regrouped request: %+v", s)
			}
		})
	}
	t.Run("once per turn", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		continuesTurn(t, repo, root, "OKです")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
		// the Stop after a background task's notice carries no stop_hook_active: the same turn is still not blocked again
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>done</task-notification>"})
		out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
		if _, block := stopReason(t, out); block {
			t.Fatalf("the same turn is asked only once: %s", out)
		}
	})
	t.Run("no submit", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		continuesTurn(t, repo, root, "OKです")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
		out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
		if _, block := stopReason(t, out); block {
			t.Fatalf("a second Stop is not blocked: %s", out)
		}
		if s := loadSession(root, "planning"); !s.Judged || s.Request != "OKです" {
			t.Fatalf("without the organizing, the Stop after the block judges with the list's decision: judged=%v %q", s.Judged, s.Request)
		}
	})
}

// a request that already has the person's truth is not labeled again by a restatement
func TestRestatesKeepsThePersonsTruth(t *testing.T) {
	repo, root, _ := planningFixture(t)
	turn, done := restateTurns(t, repo, root, "planning")
	if err := labelTrace(store.Store{Dir: root}, "done-as-asked", done, "yes", 1, "human"); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateAgain})
	restateSubmit(t, root, "planning", turn)
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(mustRead(t, filepath.Join(root, "labels", "done-as-asked.jsonl")))), "\n") {
		if strings.Contains(line, done) {
			n++
		}
	}
	if l, _ := doneLabel(t, root, done); n != 1 || l.Source != "human" {
		t.Fatalf("the person's truth stays the only label of that request: %d labels, %+v", n, l)
	}
}

// 0.9.3 audit 5 (test hole M23): with an enabled guard that fails, a short unlisted message still makes only one block; the Stop after
// the block is not blocked by the guard either (a second Stop is never blocked), and it judges the turn.
func TestGuardAndGroupingAskStillBlockOnlyOnce(t *testing.T) {
	for name, reply := range map[string]string{
		"short": "\u305d\u308c\u3067\u3044\u3053\u3046",
		"long":  "please also add a section about the archive format to the README and link it from the install steps",
	} {
		t.Run(name, func(t *testing.T) { guardAndGroupingOnce(t, reply) })
	}
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

// Low 2 and T5: the terminal line when a guard stops a turn names the guard, and with the Japanese setting it carries the Japanese
// (as in 0.9.1); the recorded reason, which the page shows as the last stop, carries the Japanese too.
func TestAuditTranslationGuardStopLineInJapanese(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	setHookLanguage(t, "Japanese")
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-foo", Kind: guardKindForbiddenWords, Title: "no foo", Words: []string{"foo"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": repo, "prompt": "add a line to the README"})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\nfoo bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "done"})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil || parsed["decision"] != "block" {
		t.Fatalf("a failing Stop returns decision: block: %s", out)
	}
	sys, _ := parsed["systemMessage"].(string)
	if !strings.Contains(sys, "(no-foo)") || !strings.Contains(sys, "failed, so the agent was told to fix it") {
		t.Errorf("the terminal line names the guard in English: %q", sys)
	}
	if !strings.Contains(sys, "\u6b62\u3081\u308b\u691c\u67fb(no-foo)\u306b\u5f15\u3063\u304b\u304b\u3063\u305f") {
		t.Errorf("with the Japanese setting the terminal line carries the Japanese with the guard id: %q", sys)
	}
	recs := guardRecordsFor(root, "no-foo")
	if len(recs) != 1 || !strings.Contains(recs[0].Reason, "a word not to use") || !strings.Contains(recs[0].Reason, "\u4f7f\u308f\u306a\u3044\u8a9e") {
		t.Errorf("the recorded reason (shown on the page as the last stop) carries English and Japanese: %+v", recs)
	}
}

// The standard is TypeSafe's Jev: with a key (TYPESAFE_API_KEY) the hook sends to hosted without waiting for the local model. Without a key, only the local model.
func TestHookSendsToJevWhenKeyIsSet(t *testing.T) {
	jev := fakeAgentModel(t, func(string) float64 { return 0.3 })
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo) // no local model (a closed port)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	old := hookHostedURL
	hookHostedURL = jev.URL
	t.Cleanup(func() { hookHostedURL = old })
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "j", "cwd": repo, "prompt": "explain what the parser in this repository does"})
	st := store.Store{Dir: agent}
	recs, err := st.Records("prompt-kind", 0)
	if err != nil || len(recs) == 0 || newest(recs).Endpoint != "hosted" {
		t.Fatalf("with a key the built-in decisions go to Jev: %d records, err=%v, last=%+v", len(recs), err, newest(recs))
	}
	for _, s := range spawned {
		if !strings.HasPrefix(s, "serve ") { // the page (serve) is started even with a key. What is not started is the local model (up)
			t.Fatalf("with a key no local model is started: %v", spawned)
		}
	}

	// even with a key, SHIKII_MODEL=local uses the local model (selectable by setting)
	local := fakeAgentModel(t, func(string) float64 { return 0.3 })
	oldLocal := hookLocalURL
	hookLocalURL = local.URL
	t.Cleanup(func() { hookLocalURL = oldLocal })
	t.Setenv("SHIKII_MODEL", "local")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "l", "cwd": repo, "prompt": "explain what the formatter in this repository does"})
	recs, _ = st.Records("prompt-kind", 0)
	if len(recs) == 0 || newest(recs).Endpoint != "local" {
		home, _ := agentHome()
		log, _ := os.ReadFile(filepath.Join(home, "hook.log"))
		t.Fatalf("SHIKII_MODEL=local must use the local model even with a key: %+v\nhook.log: %s", newest(recs), log)
	}
	t.Setenv("SHIKII_MODEL", "")
	hookLocalURL = oldLocal

	t.Setenv("TYPESAFE_API_KEY", "")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "k", "cwd": repo, "prompt": "explain what the lexer in this repository does"})
	after, _ := st.Records("prompt-kind", 0)
	if len(after) != len(recs) {
		t.Fatalf("without a key and without a local model nothing is sent anywhere: %d → %d", len(recs), len(after))
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

// Audit 0.9.3 low 4: the end-of-turn and unsure notices of hookOutcome and the waiting-answers line are read by the person in the terminal.
// They name the question by its title on the page and use the page's plain words; the probability is told as the AI's confidence, never as p=.
func TestHookPersonNoticesUsePlainWordsAndTitle(t *testing.T) {
	glossary := map[string][]string{"ja": pageGlossary(t, "ja"), "en": pageGlossary(t, "en")}
	load := func(id string) *spec.Spec {
		sp, err := spec.Load(filepath.Join("..", "..", "integrations", "claude-code", "specs", id+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	receipt := &decide.Receipt{N: 61, NHuman: 40, HumanCheckedAct: 12, HumanPrecision: 0.96}
	cases := []struct {
		spec string
		band decide.Band
		p    float64
	}{
		{"needs-plan", decide.Ask, 0.53},
		{"changes-in-scope", decide.Discard, 0.10},
		{"done-as-asked", decide.Discard, 0.10},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			sp := load(c.spec)
			d := &decide.Decision{TraceID: "0123456789abcdef", Band: c.band, Top: "yes", P: c.p, IsCalibrated: true, Receipt: receipt}
			o := hookOutcome(c.spec, d, localize(sp, d, "ja"))
			if o.System == "" || o.Context != "" {
				t.Fatalf("a notice for the person only: %+v", o)
			}
			if bad := noticeProblems(t, o.System, glossary); len(bad) > 0 {
				t.Fatalf("the notice holds inner words:\n%s\n%s", strings.Join(bad, "\n"), o.System)
			}
			en, ja, _ := strings.Cut(o.System, "\n")
			if !strings.Contains(en, `"`+sp.Display["en"].Title+`"`) || !strings.Contains(ja, "「"+sp.Display["ja"].Title+"」") {
				t.Fatalf("the notice names the question by its title:\n%s", o.System)
			}
			if c.band == decide.Discard && (!strings.Contains(en, "90%") || !strings.Contains(ja, "90%")) {
				t.Fatalf("the confidence is said plainly (90%% sure of the no side):\n%s", o.System)
			}
		})
	}
	t.Run("waiting answers", func(t *testing.T) {
		msg := waitingNotice(3, "http://127.0.0.1:1/", hookLoc{Lang: "ja"})
		if bad := noticeProblems(t, msg, glossary); len(bad) > 0 || !strings.Contains(msg, "3") {
			t.Fatalf("the waiting-answers line holds inner words:\n%s\n%s", strings.Join(bad, "\n"), msg)
		}
	})
	// the check itself catches the old text (the notices before 0.9.3's fix)
	old := "shikii: this turn's changes look outside what you asked for (changes-in-scope p(yes)=0.10; line on 61 truths)\nshikii: このターンで変えたものは頼まれた範囲の外に見える(changes-in-scope p(はい)=0.10。線は正解 61 件)"
	if len(noticeProblems(t, old, glossary)) < 6 {
		t.Fatalf("the check misses the old notice's words: %q", noticeProblems(t, old, glossary))
	}
}

// 0.9.7: moving or renaming the project folder stopped shikii until a person followed the manual steps. The request's hook now reconnects
// the project by itself, says so once, and the turn goes on as usual: the wiki is there and the index points at the new path only.
func TestMovedProjectReconnectsByItself(t *testing.T) {
	common, old, moved := movedProject(t)
	prompt := map[string]any{"session_id": "s1", "cwd": moved, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}
	first := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	if !strings.Contains(first, "moved or renamed from "+old) || !strings.Contains(first, "nothing needs fixing") {
		t.Fatalf("says once that it reconnected the project: %q", first)
	}
	if loadSession(filepath.Join(moved, ".shikii"), "s1").TurnID == "" {
		t.Fatal("and the turn goes on as usual (the request is recorded)")
	}
	if active, err := projectActive(filepath.Join(moved, ".shikii"), moved); err != nil || !active {
		t.Fatalf("the project is active at the new path: %v %v", active, err)
	}
	if paths := registeredPaths(common); len(paths) != 1 || paths[0] != moved {
		t.Fatalf("the shared index lists the new path only: %v", paths)
	}
	if _, err := os.Lstat(filepath.Join(common, "projects", projectID(old)+".json")); !os.IsNotExist(err) {
		t.Fatalf("the old path's entry is removed from the shared index: %v", err)
	}
	if live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live(); err != nil || len(live) != 1 {
		t.Fatalf("the wiki is there: %+v %v", live, err)
	}
	prompt["prompt"] = "\u6b21\u306f CHANGELOG \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "moved or renamed") {
		t.Fatalf("only once: %q", again)
	}
}

// 0.9.2: adopting a wording adds <id>-v<N>.yaml. The hook read <id>.yaml directly, so adopting did not change the wording of the hook's 4 questions.
// hookSpecPath returns the file with the highest version.
func TestHookSpecPathUsesAdoptedVersion(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	base := filepath.Join(agent, "decisions", "changes-in-scope.yaml")
	if got := hookSpecPath(agent, "changes-in-scope"); got != base {
		t.Fatalf("before adoption, the original file: %s", got)
	}
	b, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, base)
	next := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(b), fmt.Sprintf("version: %d", sp.Version+1))
	if next == string(b) {
		t.Fatal("the version line was not found")
	}
	path := filepath.Join(agent, "decisions", fmt.Sprintf("changes-in-scope-v%d.yaml", sp.Version+1))
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hookSpecPath(agent, "changes-in-scope"); got != path {
		t.Fatalf("uses the adopted version: %s", got)
	}
	if got := hookSpecPath(agent, "done-as-asked"); filepath.Base(got) != "done-as-asked.yaml" {
		t.Fatalf("other questions stay as they were: %s", got)
	}
	// an undone version (not ending in .yaml) is not used
	if err := os.Rename(path, path+".reverted"); err != nil {
		t.Fatal(err)
	}
	if got := hookSpecPath(agent, "changes-in-scope"); got != base {
		t.Fatalf("an undone version is not used: %s", got)
	}
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

// A subagent's (Claude Code's Agent tool) task text arrives in neither UserPromptSubmit nor SubagentStart. Since 0.7.3 lessons are delivered at SubagentStart.
// 0.9.1: the task text the parent passed to the Agent tool is kept at PreToolUse and lessons are picked with it (picking by the parent's request delivered lessons unrelated to the child's work).
// The parent session's record (request, delivered memory versions) is not changed, and the delivery is recorded under an ID separate from the parent's (so reflection can mark it helpful / unused). Nothing is output without a parent request.
func TestSubagentStartDeliversLessonsForItsOwnTask(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	if out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"}); out != "" {
		t.Fatalf("delivers nothing if there is no parent request yet: %s", out)
	}
	// the parent's request is unrelated to README. The child is given the README work
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "画面の色を直して、監査も頼みます"})
	before := loadSession(st.Dir, "sub")
	ctx := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください"))
	if !strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "close to its task") {
		t.Fatalf("delivers lessons close to the task text given to the child: %s", ctx)
	}
	after := loadSession(st.Dir, "sub")
	if after.Request != before.Request || after.TurnID != before.TurnID || len(after.Memories) != len(before.Memories) || len(after.SubagentTasks) != 0 {
		t.Fatalf("the parent's record is unchanged except for dropping the used child task: before=%+v after=%+v", before, after)
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.Parent == before.TurnID {
			sub = d
		}
	}
	if sub.Agent != "agent-1" || sub.Request != "README の導入手順を更新してください" || len(sub.Quotes) == 0 || sub.Quotes[0].ID != draft.ID {
		t.Fatalf("records the delivery to the child under an ID separate from the parent's, with the child's task text and the quoted versions: %+v", deliveries)
	}
	// the same child starting again does not fail (if the task is passed again, the same ID and the same quotes are not saved again)
	if ctx2 := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください")); !strings.Contains(ctx2, draft.Lesson) {
		t.Fatalf("delivers the second time too: %s", ctx2)
	}
}

// 0.9.1: a child with no kept task (no PreToolUse, as in Codex; a different type; too old) does not get lessons picked by the parent's request,
// only the rules that apply to every request (rules with the scene "いつも"). Kept tasks of the same type are used one at a time, oldest first
func TestSubagentWithoutItsTaskGetsOnlyAlwaysRules(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Kind, always.Scene, always.AppliesTo = "", "見える文は日本語", "報告も含めて見える文は日本語で書く。", "rule", "いつも", "どの依頼でも"
	page, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always)
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s4", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a1", "agent_type": "Explore"}))
	if !strings.Contains(ctx, always.Lesson) || strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "the rules that apply to every request") {
		t.Fatalf("a child with no kept task gets only the \"いつも\" rules, not lessons close to the parent's request: %s", ctx)
	}
	// a kept task for a child of another type is not used. Kept tasks of the same type are used oldest first
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください", "subagent_type": "Plan"}})
	// WebFetch also has a prompt in its input. Tools other than Agent / Task are not kept even with a prompt
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "WebFetch", "tool_input": map[string]any{"url": "https://example.com", "prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a2", "agent_type": "Explore"})); strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a task for a child of another type (Plan) is not used for an Explore child: %s", ctx)
	}
	if got := loadSession(st.Dir, "s4").SubagentTasks; len(got) != 1 || got[0].Type != "Plan" {
		t.Fatalf("only Agent tool tasks are kept (not WebFetch), and unused records stay: %+v", got)
	}
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a3", "agent_type": "Plan"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a child of the same type is picked with the kept task text: %s", ctx)
	}
	// a child started without a type starts as general-purpose
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Task", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a4", "agent_type": "general-purpose"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a record without a type is used for a general-purpose child: %s", ctx)
	}
	_ = page
}

// Audit (2026-10-23): deliveries to subagents can be assessed in reflection alongside the parent's conversation, and are not silent on a restart after the lesson's version changed.
func TestSubagentDeliveriesAreAssessedWithTheParentAndSurviveRevisions(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub2").TurnID
	startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	deliveries, _ := st.MemoryDeliveries(scope)
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.Parent != "" {
			sub = d
		}
	}
	if sub.Parent != parent || sub.Agent != "agent-x" {
		t.Fatalf("a delivery to a subagent carries the parent's turn and the agent: %+v", deliveries)
	}
	// after the lesson's version changed, it is delivered even when the same subagent starts again (it used to be refused as another quote for the same id, and stayed silent)
	draft.Lesson = draft.Lesson + " 変更後も確認する。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	out := startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	if !strings.Contains(out, "変更後も確認する") {
		t.Fatalf("delivered on a restart after the version changed: %q", out)
	}
	// reflection: the parent's conversation (once the reply and the next message are there) also lists the delivery to the subagent
	runHook(t, "stop", map[string]any{"session_id": "sub2", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "次は画面の色を直して。"})
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	sources, _ := q["sources"].([]any)
	found := 0
	for _, s := range sources {
		src := s.(map[string]any)
		if src["source_material"].(map[string]any)["id"] != parent {
			continue
		}
		for _, m := range src["memories"].([]any) {
			if m.(map[string]any)["parent"] == parent {
				found++
			}
		}
	}
	if found < 1 {
		t.Fatalf("the delivery to the subagent shows up for assessment alongside the parent's conversation: %v", q["sources"])
	}
}

// Audit R2: a delivery to a subagent can be assessed by the source_id (delivery id) in the reflection output and leaves the observation queue.
// If the parent's conversation (reply and next message) is complete, the delivery id need not be a conversation id. Submit's conversation count also folds into the parent.
func TestSubagentDeliveryCanBeAssessedByItsOwnSourceID(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub3", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub3").TurnID
	startSubagent(t, "sub3", scope, "agent-y", "Explore", "README の導入手順を更新してください")
	runHook(t, "stop", map[string]any{"session_id": "sub3", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub3", "cwd": scope, "prompt": "次は画面の色を直して。"})
	deliveries, _ := st.MemoryDeliveries(scope)
	var subID string
	for _, d := range deliveries {
		if d.Parent == parent {
			subID = d.ID
		}
	}
	if subID == "" {
		t.Fatal("there is a delivery to the subagent")
	}
	key := "lesson:" + draft.ID + ":" + revision
	// before assessment: the parent's conversation is complete, so the delivery to the subagent is waiting for assessment, not for the "next request" (audit R3)
	if rep, err := st.MemoryReport(scope); err != nil || rep.Stats.Pending != 2 || rep.Stats.AwaitingFeedback != 0 {
		t.Fatalf("once the parent's conversation is complete, the delivery to the subagent is not waiting for the next request either: %+v %v", rep.Stats, err)
	}
	// even with one more delivery from another session, the parent's and the subagent's deliveries can be assessed at once (up to 0.9.0 this checked that parent and subagent fold into "at most 2 conversations". audit R3)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	other := loadSession(st.Dir, "other").TurnID
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "次は画面の色を直して。"})
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]any{
		{"source_id": parent, "key": key, "result": "helpful", "decision": "手順に確認を足した", "evidence": "返答に確認の手順が入った"},
		{"source_id": subID, "key": key, "result": "unused", "decision": "サブエージェントは手順を読まなかった", "evidence": "返答に手順への言及が無い"},
		{"source_id": other, "key": key, "result": "helpful", "decision": "別のセッションでも手順に確認を足した", "evidence": "返答に確認の手順が入った"},
	}})
	if out["complete"] != true {
		t.Fatalf("the parent's and the subagent's deliveries can be assessed at once as the same conversation (2 conversations with one other added): %v", out)
	}
	rep, err := st.MemoryReport(scope)
	if err != nil || rep.Stats.Pending != 0 || rep.Stats.AwaitingFeedback != 0 || rep.Stats.Helpful != 2 || rep.Stats.Unused != 1 {
		t.Fatalf("once assessed they leave the observation queue: %+v %v", rep.Stats, err)
	}
}

// Audit 0.9.1 (test gaps GM2-4, 14): kept child tasks are used oldest first within the same type, and ones older than 10 minutes are not used.
// When adding, expired ones are dropped and at most 8 are kept
func TestSubagentTasksAreOldestFirstWithinLimits(t *testing.T) {
	now := time.Now()
	tasks := []subagentTask{
		{Type: "Explore", Prompt: "古すぎる", At: now.Add(-11 * time.Minute)},
		{Type: "Explore", Prompt: "古い", At: now.Add(-2 * time.Second)},
		{Type: "Explore", Prompt: "新しい", At: now.Add(-1 * time.Second)},
	}
	if got, ok := pickSubagentTask(tasks, "Explore", now); !ok || got.Prompt != "古い" {
		t.Fatalf("uses the oldest record within the time limit (not ones older than 10 minutes): %+v %v", got, ok)
	}
	add := func(s *hookSession, prompt string) {
		in := &hookInput{ToolName: "Agent", ToolInput: json.RawMessage(`{"prompt":"` + prompt + `","subagent_type":"Plan"}`)}
		for _, m := range hookAgentTask(in) {
			m(s)
		}
	}
	// when adding, expired ones (even of another type) are dropped
	expired := &hookSession{SubagentTasks: []subagentTask{{Type: "Explore", Prompt: "期限切れ", At: now.Add(-11 * time.Minute)}}}
	add(expired, "仕事 X")
	if len(expired.SubagentTasks) != 1 || expired.SubagentTasks[0].Prompt != "仕事 X" {
		t.Fatalf("drops expired records when adding: %+v", expired.SubagentTasks)
	}
	// dropping records older than 30 seconds applies only to the same type (records of other types stay. re-audit GB)
	other := &hookSession{SubagentTasks: []subagentTask{{Type: "Explore", Prompt: "1 分前の Explore", At: now.Add(-time.Minute)}}}
	add(other, "仕事 Y")
	if len(other.SubagentTasks) != 2 || other.SubagentTasks[0].Prompt != "1 分前の Explore" {
		t.Fatalf("records of another type are not dropped by the 30-second rule: %+v", other.SubagentTasks)
	}
	s := &hookSession{SubagentTasks: []subagentTask{{Type: "Plan", Prompt: "期限切れ", At: now.Add(-11 * time.Minute)}}}
	for i := 0; i < 10; i++ {
		in := &hookInput{ToolName: "Agent", ToolInput: json.RawMessage(`{"prompt":"仕事 ` + string(rune('A'+i)) + `","subagent_type":"Plan"}`)}
		for _, m := range hookAgentTask(in) {
			m(s)
		}
	}
	if len(s.SubagentTasks) != subagentTaskMax || s.SubagentTasks[0].Prompt != "仕事 C" || s.SubagentTasks[len(s.SubagentTasks)-1].Prompt != "仕事 J" {
		t.Fatalf("drops expired ones when adding and keeps only the newest 8: %+v", s.SubagentTasks)
	}
}

// The binary's own guard (0.9.1): from a go test executable (*.test), even with a forgotten stub, never start real background processes or open a browser
func TestSpawnRefusesFromATestBinary(t *testing.T) {
	if pid := spawnShikii(t.TempDir(), "up.log", "up"); pid != 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL) // anything started without the guard is killed with its whole group before it multiplies
		t.Fatalf("a test executable started a real up: pid %d", pid)
	}
	if !runningAsTestBinary() {
		t.Fatal("cannot recognize the go test executable")
	}
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

func TestMemoryEffectPreservesDeliveredRevisionAndIndependentPending(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	ctx := memoryEffectPrompt(t, scope, "effect")
	if !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("the related lesson is actually delivered")
	}
	key := "lesson:" + draft.ID + ":" + revision
	id := memoryEffectComplete(t, st, scope, "effect")
	// Finish the wiki and the usual scoring first, so only the memory assessment is left.
	feedback, _ := st.RecentFeedback(scope)
	for _, f := range feedback {
		if f.ID != id {
			continue
		}
		for _, target := range f.Targets {
			if _, err := runReview(st.Dir, scope, reviewRequest{Operation: "submit", SourceID: id, TraceID: target.TraceID, Truth: "yes", Reason: "合成の実行結果を確認"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(notebook.Draft{Sources: []string{id}}); err != nil {
		t.Fatal(err)
	}
	draft.Lesson = "更新された別の教訓。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	item := memoryEffectItem(t, h, id)
	quote := item["quote"].(map[string]any)
	if quote["key"] != key || quote["lesson"] == draft.Lesson || !strings.Contains(ctx, quote["lesson"].(string)) {
		t.Fatal("assesses the revision and quote as delivered, not the lesson updated later")
	}
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["helpful"] != float64(0) || stats["pending"] != float64(1) {
		t.Fatal("scoring completion as yes alone does not make the memory's effect a success")
	}
	result := memoryEffectSubmit(t, h, id, key, "helpful", "未初期化の場所の検査を追加する判断に使った", "合成の隔離フォルダで出力と記録が無いことを検査した")
	if result["complete"] != true {
		t.Fatalf("saves a provisional assessment of the memory, backed by the decision and the result: %v", result)
	}
	stats = memoryEffectStats(t, h)
	if stats["helpful"] != float64(1) || stats["pending"] != float64(0) {
		t.Fatal("keeps the number of quotes delivered apart from the number of observations that they helped")
	}
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || personTruths(labels) != 0 {
		t.Fatal("provisional memory assessments are not mixed into truths or calibration")
	}
}

func TestMemoryEffectKeepsTheEscapedRawQuoteExactly(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	request := "README の導入手順を更新してください"
	if err := st.AppendFeedback(scope, store.Feedback{ID: "0000000000000001", Request: request, Summary: strings.Repeat("\x01", 600), Reply: "<引用>\n過去の発言" + strings.Repeat("\x02", 600)}); err != nil {
		t.Fatal(err)
	}
	ctx := memoryEffectPrompt(t, scope, "raw-effect")
	if len(ctx) > 1800 || !strings.Contains(ctx, "feedback:0000000000000001") {
		t.Fatal("unsorted raw text is also actually delivered within the limit")
	}
	var quotes []map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(ctx, "\n", 2)[1]), &quotes); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: scope})
	// A request with no next message yet cannot be assessed as a successful memory.
	id := loadSession(agent, "raw-effect").TurnID
	if memoryEffectSubmit(t, h, id, quotes[0]["key"].(string), "helpful", "仮の判断", "確認前")["complete"] != false {
		t.Fatal("no effect is attached to a request that has not ended")
	}
	id = memoryEffectComplete(t, st, scope, "raw-effect")
	quote := memoryEffectItem(t, h, id)["quote"].(map[string]any)
	actual, _ := json.Marshal(quotes[0])
	saved, _ := json.Marshal(quote)
	if string(actual) != string(saved) || len(quote["summary"].(string)) >= 600 {
		t.Fatal("what is saved is not the whole raw text but exactly the quote delivered after escaping and cutting")
	}
}

func TestMemoryEffectSeparatesUsefulUnusedHarmfulAndUnknown(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	key := "lesson:" + draft.ID + ":" + revision
	for i, result := range []string{"helpful", "unused", "harmful", "unknown"} {
		session := fmt.Sprintf("case-%d", i)
		memoryEffectPrompt(t, scope, session)
		id := memoryEffectComplete(t, st, scope, session)
		if memoryEffectSubmit(t, h, id, key, result, "合成の判断記録", "合成の検査結果")["complete"] != true {
			t.Fatal("examples that did not help, and unknown ones, are saved in the same unit")
		}
	}
	stats := memoryEffectStats(t, h)
	for _, name := range []string{"helpful", "unused", "harmful", "unknown"} {
		if stats[name] != float64(1) {
			t.Fatalf("does not count only the good examples: %s=%v", name, stats[name])
		}
	}
	if stats["delivered"] != float64(4) || stats["pending"] != float64(0) {
		t.Fatal("the delivered count is the sum of all categories and the unassessed")
	}
	t.Run("two-quotes", func(t *testing.T) {
		st, scope, h, draft, _ := memoryEffectFixture(t)
		draft.ID, draft.Title, draft.Lesson = "", "README の導入手順の補足", "モデルの起動方法も確認する。"
		if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
			t.Fatal(err)
		}
		memoryEffectPrompt(t, scope, "two-quotes")
		id := memoryEffectComplete(t, st, scope, "two-quotes")
		q := reflectCall(t, h, map[string]any{"operation": "query"})
		entries := []map[string]string{}
		for _, raw := range q["sources"].([]any) {
			source := raw.(map[string]any)
			if source["source_material"].(map[string]any)["id"] != id {
				continue
			}
			for i, raw := range source["memories"].([]any) {
				key := raw.(map[string]any)["quote"].(map[string]any)["key"].(string)
				entries = append(entries, map[string]string{"source_id": id, "key": key, "result": []string{"helpful", "unused"}[i], "decision": "合成の判断", "evidence": "合成の結果"})
			}
		}
		if len(entries) != 2 || reflectCall(t, h, map[string]any{"operation": "submit", "memory": entries})["complete"] != true {
			t.Fatal("two memories delivered to one request are assessed separately")
		}
		stats := memoryEffectStats(t, h)
		if stats["delivered"] != float64(2) || stats["helpful"] != float64(1) || stats["unused"] != float64(1) {
			t.Fatal("the unit is memories actually delivered, not requests")
		}
	})
}

// The reflection request (about 1,700 characters) follows the same rule: the full text the first time in a session and every
// noticeFullEvery asks, a short form in between, also after a turn that ended without the reflection (the Stop check repeats the steps).
func TestReflectionNoticeIsShortAfterTheFirstAsk(t *testing.T) {
	_, scope, _, _, _ := memoryEffectFixture(t)
	const full = "keep the short lesson and the detailed body apart"
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	if !strings.Contains(first, "wrap up the reflection") || !strings.Contains(first, full) {
		t.Fatalf("precondition and the first ask: the full text: %q", first)
	}
	// the turn ends without the reflection: the Stop check repeats the steps, and the next ask is the short form
	stop := runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	if !strings.Contains(string(stop), "reflect query") || !strings.Contains(string(stop), "submit reviews") {
		t.Fatalf("the Stop check repeats the steps of the missed reflection: %s", stop)
	}
	short := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if !strings.Contains(short, "wrap up the reflection") || strings.Contains(short, full) || !strings.Contains(short, `{"sources":[`) || !strings.Contains(short, "reflect query") {
		t.Fatalf("the short form names the command and the conversations: %q", short)
	}
}

// 0.9.5 check: Claude Code and OpenCode start `shikii mcp --store .shikii` inside the project, so a decision made through MCP (reflect
// place) looked up the page under the scope "." and started a second `shikii serve --store .shikii` next to the hook's page, whose
// process list did not say which project it belonged to. The page is looked up and started under the project's absolute path.
func TestDecisionFromRelativeStoreUsesTheProjectsPage(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	hookEnv(t, model, repo)
	t.Chdir(repo)
	spawned = nil
	if _, _, err := hookDecide(".shikii", "needs-plan", map[string]string{"request": "rename a variable"}, repo); err != nil {
		t.Fatal(err)
	}
	want := "--store " + filepath.Join(feedbackScope(repo), ".shikii") + " "
	if len(spawned) != 1 || !strings.Contains(spawned[0], want) {
		t.Fatalf("the page is started for the project's absolute path (%q): %q", want, spawned)
	}
}

// The text asking for reflection contains none of the conversation's raw text (request, summary, the person's reply) (checked by content, not size)
func TestReflectAskCarriesNoOriginalText(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	var ctx string
	var ids []string
	for n := 1; n <= 2; n++ {
		id, _ := reviewSeed(t, st, scope, n)
		ids = append(ids, id)
		ctx = feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": fmt.Sprint(n), "cwd": repo, "prompt": "合成の記録を調べてください。今回の内容を確認します。"}))
	}
	t.Logf("text size: %d bytes", len(ctx))
	// If the text does not appear, checking that no raw text is in it means nothing (final audit 0.9.1 T2). The asking text and the ids of the conversations to reflect on are there
	if !strings.Contains(ctx, "wrap up the reflection") {
		t.Fatalf("the text asking for reflection does not appear: %q", ctx)
	}
	for _, id := range ids {
		if !strings.Contains(ctx, id) {
			t.Errorf("the id %s of a conversation to reflect on is missing: %q", id, ctx)
		}
	}
	for _, raw := range []string{"README の起動手順を日本語にする", "起動手順を直しました", "まだ英語が残っています"} {
		if strings.Contains(ctx, raw) {
			t.Errorf("the conversation's raw text %q got into the text asking for reflection", raw)
		}
	}
}

func TestHookKeepsFullClosingTextForPeople(t *testing.T) {
	var modelSawEnd atomic.Bool
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "END-OF-FULL-REPLY") || strings.Contains(state, "END-OF-FULL-REQUEST") {
			modelSawEnd.Store(true)
		}
		return .6
	})
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	request := strings.Repeat("README の日本語の起動手順を更新してください。", 100) + "END-OF-FULL-REQUEST"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "full-text", "cwd": repo, "prompt": request})
	closing := strings.Repeat("日本語の起動手順を確認しました。", 100) + "\nEND-OF-FULL-REPLY"
	runHook(t, "stop", map[string]any{"session_id": "full-text", "cwd": repo, "last_assistant_message": closing})
	st := store.Store{Dir: agent}
	records, _ := st.Records("done-as-asked", 0)
	if len(records) != 1 || len(records[0].Inputs["summary"]) > requestMax || modelSawEnd.Load() {
		t.Fatal("keep the input limit for what is given to the decision model")
	}
	before := records[0]
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "full-text", "cwd": repo, "prompt": "ok"}) // a bare acknowledgement: no outcome truth, so the score stays with the agent
	feedback, _ := st.RecentFeedback(feedbackScope(repo))
	view, err := reviewQuery(st, feedbackScope(repo))
	if err != nil || len(view["pending"].([]reviewTask)) != 1 || view["pending"].([]reviewTask)[0].Targets[0].OriginalInputs["summary"] != closing {
		t.Fatal("the agent doing tentative scoring can also read to the end with query")
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: feedbackScope(repo)})
	reviewCall(t, h, map[string]any{"operation": "submit", "source_id": feedback[0].ID, "trace_id": before.TraceID, "truth": "unknown", "reason": "次の発言は相づちだけで、完了の評価を含まない"})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/queue?spec=done-as-asked", nil))
	var queue struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &queue)
	if len(queue.Items) != 1 {
		t.Fatal("the decision to check reaches the page")
	}
	originals, _ := queue.Items[0]["original_inputs"].(map[string]any)
	if originals["summary"] != closing || originals["request"] != request {
		t.Fatal("a reply over 1,500 bytes also reaches the page to its end")
	}
	after, _ := st.FindRecord(before.SpecID, before.TraceID)
	if after.InputsHash != before.InputsHash || after.SpecHash != before.SpecHash || after.Inputs["summary"] != before.Inputs["summary"] {
		t.Fatal("the original text for the page does not change the original decision, inputs or hash")
	}
}

// 0.9.7: a store keeps its name (records/store-name, the hash of the path it was first opened at) for good, so moving the folder renames
// nothing and no records are moved. The real hook flows run (a request, the end of the turn, a delivered lesson, an assessment), the
// folder is moved and reconnected by the next request, the flows run again, and the names under the store are the same as before.
func TestMovingTheFolderRenamesNothingInTheStore(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	flows := func(st store.Store, scope, session string) {
		memoryEffectPrompt(t, scope, session)
		id := memoryEffectComplete(t, st, scope, session)
		memoryEffectSubmit(t, h, id, "lesson:"+draft.ID+":"+revision, "helpful", "added a check", "synthetic test result")
	}
	names := func(root string) []string {
		var out []string
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && (filepath.Base(filepath.Dir(p)) == "feedback" || filepath.Base(filepath.Dir(p)) == "memories" || filepath.Base(filepath.Dir(p)) == "memory-reviews" || filepath.Base(filepath.Dir(p)) == "wiki-sources" || filepath.Base(filepath.Dir(p)) == "lessons") {
				out = append(out, filepath.Base(p))
			}
			return nil
		})
		sort.Strings(out)
		return out
	}
	flows(st, scope, "s1")
	name := store.StoreName(st.Dir, scope)
	before := names(st.Dir)
	moved := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(scope, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	t.Chdir(moved)
	movedStore := store.Store{Dir: filepath.Join(moved, ".shikii")}
	flows(movedStore, moved, "s2")
	after := names(movedStore.Dir)
	if store.StoreName(movedStore.Dir, moved) != name {
		t.Fatalf("the store keeps its name after the move: %s -> %s", name, store.StoreName(movedStore.Dir, moved))
	}
	for _, n := range after {
		found := false
		for _, b := range before {
			found = found || b == n
		}
		if !found {
			t.Errorf("a new name appeared after the move: %s (before: %v)", n, before)
		}
	}
	if len(before) < 5 {
		t.Fatalf("precondition: the flows wrote the store: %v", before)
	}
}

// Audit 0.9.2 (behavior), test holes: checks that survived when their line was broken on purpose.

// Test hole: hookSpecPath picks <id>-v<N>.yaml only when its id is the same (a file named needs-plan-v9.yaml holding another question is not used).
func TestHookSpecPathIgnoresAnotherIDsVersion(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	b, _ := os.ReadFile(res.SpecPath)
	other := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(b), "version: 99")
	if other == string(b) {
		t.Fatal("the fixture's spec has a version line")
	}
	stray := filepath.Join(agent, "decisions", "needs-plan-v9.yaml")
	if err := os.WriteFile(stray, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := hookSpecPath(agent, "needs-plan"); p != filepath.Join(agent, "decisions", "needs-plan.yaml") {
		t.Fatalf("a version file of another id is not used: %s", p)
	}
}

// Test hole: when the question changed after it was measured (another hash), the agent is not asked to check that old result
// (adopting it would fail with "the spec changed", and the person would be told it could not be adopted).
func TestWordingCheckNotAskedAfterTheQuestionChanged(t *testing.T) {
	agent, repo, res := wordingCheckFixture(t)
	sp, err := spec.Load(res.SpecPath)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(res.SpecPath)
	q := strings.TrimSpace(sp.Question)
	changed := strings.Replace(string(b), q, q+" (changed)", 1)
	if changed == string(b) {
		t.Fatal("the question is in the file as is")
	}
	if err := os.WriteFile(res.SpecPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "Fix the typo in the README."}))
	if strings.Contains(ctx, "hook variants check") {
		t.Fatalf("do not ask to check a result measured on another question: %q", ctx)
	}
	_ = agent
}

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

// Another tool whose name starts with shikii (shikii-notify) has its own hooks: install keeps them whatever way they are written,
// because a hook is shikii's only when it also runs one of shikii's own hook events.
func TestHookInstallKeepsAnotherToolsHookWhoseNameStartsWithShikii(t *testing.T) {
	others := []string{`"/opt/tools/shikii-notify" hook done`, "shikii-notify hook done", "/opt/tools/shikii-notify hook done", "'/opt/my tools/shikii-notify' hook done"}
	commands := map[string]string{}
	for _, c := range others {
		commands[c] = "Notification"
	}
	settings := hookSettingsWith(t, commands)
	if _, err := editHooks(settings, true, hookEventsFor("Claude Code")); err != nil {
		t.Fatal(err)
	}
	got := readHookCommands(t, settings)
	for _, c := range others {
		if !got[c] {
			t.Fatalf("install must keep the other tool's hook %s, the hooks are %v", c, got)
		}
	}
}

// A shikii hook a person wrote by hand (with ~, $HOME, double quotes or a backslash) is not in the form shikii writes, so install
// leaves the person's line as it is and adds shikii's own hook next to it (doctor tells the person they may now have two).
func TestHookInstallLeavesAHandWrittenShikiiHookAlone(t *testing.T) {
	// with quotes or a backslash: not a form shikii writes, so shikii cannot tell what it runs. (A bare ~/… or $HOME/… path has no
	// quote, so it is read as shikii wrote paths up to 0.9.10 and replaced; TestUninstAuditHooksWithASpaceInThePath.)
	hand := []string{`"$HOME"/go/bin/shikii hook stop`, `"/a b/shikii" hook stop`, `/a\ b/shikii hook stop`, "/gone/shikii hook stop 2>/dev/null"}
	commands := map[string]string{}
	for _, c := range hand {
		commands[c] = "Stop"
	}
	settings := hookSettingsWith(t, commands)
	if _, err := editHooks(settings, true, hookEventsFor("Claude Code")); err != nil {
		t.Fatal(err)
	}
	got := readHookCommands(t, settings)
	for _, c := range hand {
		if !got[c] {
			t.Fatalf("install must leave the hand-written hook %s, the hooks are %v", c, got)
		}
	}
	for _, ev := range hookEventsFor("Claude Code") {
		if !got[ev.cmd] {
			t.Fatalf("install must add shikii's own %s hook, the hooks are %v", ev.event, got)
		}
	}
}

// A hook that chains shikii's hook after another command (or sets a variable for it) is the person's: hook install and uninstall
// leave the whole command as it is, doctor names it as written by hand instead of calling it missing, and a tool that only mentions a
// folder named shikii is not called a shikii hook at all (0.10.0's audit of a0942d5).
func TestHookInstallLeavesAChainedCommandAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin", "shikii")
	chained := []string{
		"/usr/local/bin/notify-done hook stop; " + bin + " hook stop",
		"/usr/bin/env SHIKII_X=1 " + bin + " hook stop",
		"/usr/bin/afplay /System/Library/Sounds/Glass.aiff && " + bin + " hook stop",
		"'/opt/a' && '" + bin + "' hook stop",
	}
	for _, c := range chained {
		if isShikiiHook(c) {
			t.Fatalf("%q is not in a form shikii writes", c)
		}
		if !handWrittenShikiiHook(c) {
			t.Fatalf("%q runs shikii, so doctor and uninstall name it", c)
		}
	}
	commands := map[string]string{}
	for _, c := range chained {
		commands[c] = "Stop"
	}
	settings := hookSettingsWith(t, commands)
	for _, install := range []bool{true, false} {
		if _, err := editHooks(settings, install, hookEventsFor("Claude Code")); err != nil {
			t.Fatal(err)
		}
		got := readHookCommands(t, settings)
		for _, c := range chained {
			if !got[c] {
				t.Fatalf("install=%v removed the person's command %q: %v", install, c, got)
			}
		}
	}
	for _, other := range []string{"python3 -m mytool --project ~/git/shikii hook done", "/opt/tools/shikii-notify hook stop", "mytool --dir ~/shikii-data hook stop"} {
		if handWrittenShikiiHook(other) {
			t.Fatalf("%q is another tool, not a shikii hook", other)
		}
	}
}

// A command that wraps shikii's hook in another command (nice, timeout, caffeinate) is the person's: shikii's own hooks never have
// a space in a bare word (0.9.3 and earlier wrote a path with spaces bare, and the shell split it: those hooks never ran). A path
// 0.9.2 and earlier wrote bare with ~ = # inside (iCloud's com~apple) still runs, and is still shikii's (0.10.1's audit).
func TestHookInstallLeavesAWrappedCommandAndKeepsOldPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin", "shikii")
	wrapped := []string{
		"/usr/bin/nice -n 10 " + bin + " hook stop",
		"/usr/bin/timeout 30 " + bin + " hook stop",
		"/usr/bin/caffeinate -i " + bin + " hook stop",
		"/usr/local/bin/notify-done hook stop " + bin + " hook stop",
	}
	for _, c := range wrapped {
		if isShikiiHook(c) || !handWrittenShikiiHook(c) {
			t.Fatalf("%q is the person's command that runs shikii (isShikiiHook=%v)", c, isShikiiHook(c))
		}
	}
	for _, c := range []string{
		"/Users/a/Library/Mobile/com~apple~CloudDocs/bin/shikii hook stop",
		"/Users/a/com~apple/shikii hook user-prompt-submit",
		"/Users/a/x=y/bin/shikii hook stop",
		"/Users/a/#tools/bin/shikii hook stop",
	} {
		if !isShikiiHook(c) {
			t.Fatalf("%q is a hook shikii 0.9.2 wrote, which still runs", c)
		}
	}
	commands := map[string]string{}
	for _, c := range wrapped {
		commands[c] = "Stop"
	}
	settings := hookSettingsWith(t, commands)
	for _, install := range []bool{true, false} {
		if _, err := editHooks(settings, install, hookEventsFor("Claude Code")); err != nil {
			t.Fatal(err)
		}
		got := readHookCommands(t, settings)
		for _, c := range wrapped {
			if !got[c] {
				t.Fatalf("install=%v removed the person's command %q", install, c)
			}
		}
	}
	writeHookFiles(t, home, wrapped[0])
	for _, line := range registeredBinaryLines(home, mcpCommand()) {
		if strings.HasPrefix(line, "MISSING") || !strings.Contains(line, "by hand") {
			t.Fatalf("doctor names the wrapped command as written by hand: %s", line)
		}
	}
}

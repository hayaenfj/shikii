package main

import (
	"encoding/json"
	"fmt"
	"net"
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
	"github.com/hayaenfj/shikii/internal/store"
)

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
			return strings.Contains(o.Context, "specific task choice") && strings.Contains(o.Context, "0.88")
		}},
		{"no plan needed: silent", "needs-plan", d(decide.Discard, "yes", 0.1, true), func(o hookOut) bool { return o.empty() }},
		{"needs plan unsure: one line with the URL", "needs-plan", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.System == "" && strings.Contains(o.Context, "internal review")
		}},
		{"changes in scope: silent", "changes-in-scope", d(decide.Act, "yes", 0.9, true), func(o hookOut) bool { return o.empty() }},
		{"changes out of scope: a line for the person, no context", "changes-in-scope", d(decide.Discard, "yes", 0.1, true), func(o hookOut) bool {
			return o.System == "" && strings.Contains(o.Context, "correct any actual omission")
		}},
		{"changes unsure: internal evidence review", "changes-in-scope", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.System == "" && o.Context != ""
		}},
		{"done: silent", "done-as-asked", d(decide.Act, "yes", 0.9, true), func(o hookOut) bool { return o.empty() }},
		{"not done: a line for the person", "done-as-asked", d(decide.Discard, "yes", 0.2, true), func(o hookOut) bool {
			return o.System == "" && strings.Contains(o.Context, "correct any actual omission")
		}},
		{"done unsure: internal evidence review", "done-as-asked", d(decide.Ask, "yes", 0.6, true), func(o hookOut) bool {
			return o.System == "" && o.Context != ""
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
		if strings.Contains(out, "systemMessage") || !strings.Contains(out, "Preparation registers the start") {
			t.Fatalf("shadow decisions must not suppress the reasoning guidance: %q", out)
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
	if strings.Contains(out, "systemMessage") || strings.Contains(out, "hookSpecificOutput") {
		t.Fatal("internal completion review must not quiz a person", out)
	}
	if d := bySummary("half: I removed").Display; d == nil || !strings.Contains(d.Message, "internal review") {
		t.Fatal("retain the internal review with the record", d)
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
	supplyReasoningFixture(agent, "r")
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

// a short restatement (under 40 characters) still says the previous request was not done: the minimum is only for "done"
// (test hole: making the restatement need 40 characters too was not caught)
func TestAudit4ShortRestatementIsNotDone(t *testing.T) {
	const ask = "READMEのインストール手順を直して"
	repo, root, _ := planningFixture(t)
	done := doneTraceAfter(t, repo, root, "planning", ask)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": ask})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	// Save the grouping before the next Stop settles this turn.
	restateSubmit(t, root, "planning", "")
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
	if _, err := planningCall(t, "submit", askedJSON(t, recoveryOutput(t, out)["reason"].(string))); err != nil {
		t.Fatal(err)
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
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	if _, err := planningCall(t, "submit", askedJSON(t, recoveryOutput(t, out)["reason"].(string))); err != nil {
		t.Fatal(err)
	}
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
	supplyReasoningFixture(root, "planning")
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

// Without an organizing, the list decides provisionally; required grouping stays open until saved.
func TestNoPlanKeepsListDecision(t *testing.T) {
	t.Run("content", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "main取り込んで")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main."})
		// No saved grouping: the second Stop remains incomplete.
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main.", "stop_hook_active": true})
		if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
			t.Fatalf("no organizing: the list keeps it as its own request, got %q", s.Request)
		}
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("a short new request is too short to show the person moved on (audit 3): %+v", l)
		}
		if loadSession(root, "planning").Judged {
			t.Fatal("required grouping is still missing")
		}
		// Recovery remains possible after an incomplete stop; the saved grouping governs completion.
		if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": true}); err != nil {
			t.Fatal(err)
		}
		if s := loadSession(root, "planning"); s.Request != continuesFirst+" / main取り込んで" {
			t.Fatalf("the saved grouping must apply before completion: %q", s.Request)
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
	unlock, ok := lockSessionWithin(root, "planning", 0)
	if !ok {
		t.Fatal("the sweep test needs to own the session lock")
	}
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
		requireRecoveryHalt(t, out)
		if s := loadSession(root, "planning"); s.Judged || s.Request != "OKです" {
			t.Fatalf("without the saved organizing, the turn stays unjudged: judged=%v %q", s.Judged, s.Request)
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
// the block is not blocked by the guard either, and saved required work permits judgment.
func TestGuardAndGroupingAskStillBlockOnlyOnce(t *testing.T) {
	for name, reply := range map[string]string{
		"short": "\u305d\u308c\u3067\u3044\u3053\u3046",
		"long":  "please also add a section about the archive format to the README and link it from the install steps",
	} {
		t.Run(name, func(t *testing.T) { guardAndGroupingOnce(t, reply) })
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

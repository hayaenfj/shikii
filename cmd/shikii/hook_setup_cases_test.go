package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// The shadow period: ask the 4 right after the request and at the end of the turn, only record, and print nothing to stdout. Without a model: exit 0, no output, one line in hook.log.
func TestHookShadowRecordsAndStaysSilent(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 }) // noul is a confident "no" (discard: does not wait for an answer)
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "fix the typo in a.go"}); strings.Contains(out, "systemMessage") || !strings.Contains(out, "it registers the start") {
		t.Fatalf("shadow decisions stay silent while required work guidance is delivered, got %q", out)
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
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "s2", "cwd": repo, "prompt": "and now the other file"}); strings.Contains(out, "systemMessage") || !strings.Contains(out, "it registers the start") {
		t.Fatalf("a missing model still delivers the mandatory work guidance, got %q", out)
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
	if hs["hookEventName"] != "UserPromptSubmit" || !strings.Contains(fmt.Sprint(hs["additionalContext"]), "specific task choice") {
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
	if cmds := hookCommands(settings); len(cmds) != 6 {
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
	if len(cmds) != 6 || !strings.HasSuffix(cmds[0], " hook agent-task") || !strings.HasSuffix(cmds[1], " hook session-start") || !strings.HasSuffix(cmds[2], " hook stop") || !strings.HasSuffix(cmds[3], " hook subagent-start") || !strings.HasSuffix(cmds[4], " hook tool-lesson") || !strings.HasSuffix(cmds[5], " hook user-prompt-submit") || !filepath.IsAbs(strings.Fields(cmds[0])[0]) {
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
	// Codex has no SubagentStart and no PreToolUse hooks, so 3: UserPromptSubmit, Stop and SessionStart (0.7.3; 0.11)
	if codex := hookCommands(filepath.Join(home, ".codex", "hooks.json")); len(codex) != 3 || strings.Contains(strings.Join(codex, " "), "subagent-start") || strings.Contains(strings.Join(codex, " "), "agent-task") {
		t.Fatalf("Codex gets UserPromptSubmit, Stop and SessionStart only: %v", codex)
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), "node other.mjs") || !strings.Contains(string(b), "rtk hook claude") || !strings.Contains(string(b), `"language": "Japanese"`) || !strings.Contains(string(b), `"SessionStart"`) {
		t.Fatalf("other hooks and keys must stay (SessionStart now holds shikii's hook, 0.11):\n%s", b)
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
	if cmds := hookCommands(settings); len(cmds) != 6 {
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
	if cmds := hookCommands(settings); len(cmds) != 6 || !strings.Contains(out, "hooks added") {
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
	if len(cmds) != 3 || !strings.HasSuffix(cmds[0], " hook session-start") || !strings.HasSuffix(cmds[1], " hook stop") || !strings.HasSuffix(cmds[2], " hook user-prompt-submit") {
		t.Fatalf("Codex must get the 3 hooks and lose the old one: %v", cmds)
	}
	b, _ := os.ReadFile(codexHooks)
	if strings.Contains(string(b), "apply_patch") || !strings.Contains(string(b), "deny-check.sh") || !strings.Contains(string(b), `"description": "mine"`) {
		t.Fatalf("Codex hooks.json must drop the old PreToolUse group and keep what was there:\n%s", b)
	}
	if fileExists(codexHooks + ".bak-shikii") {
		t.Fatal("a file that already had shikii hooks in it must not be backed up")
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if cmds := hookCommands(codexHooks); len(cmds) != 3 {
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
	if lb, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log")); strings.Contains(string(lb), "auto-tune needs-plan") {
		t.Fatalf("a changed wording still needs contrasting truths:\n%s", lb)
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
	// Shared writers must understand this version's learning metadata before it is saved.
	if !strings.Contains(out, "reopen") || !strings.Contains(out, "0.32.0 or later") || !strings.Contains(out, "before saving learning") || !strings.Contains(out, "new work context") {
		t.Fatalf("the update must name the supported version and restart requirement before saving learning: %s", out)
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

// An interrupted recovery with no saved grouping must remain unjudged when the next request arrives.
func TestAskedTurnStaysUnjudgedWhenRecoveryNeverCompletes(t *testing.T) {
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
	if len(recs) != 1 {
		t.Fatalf("the unfinished turn must not have a done-as-asked judgment: %d records", len(recs))
	}
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
			if o.System != "" || !strings.Contains(o.Context, "internal review") || !strings.Contains(o.Context, "specific task choice") {
				t.Fatal("send internal advice to the agent only", o)
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
	if len(queue.Items) != 0 {
		t.Fatal("internal completion remains off the human queue")
	}
	stored, _ := st.FindRecord(before.SpecID, before.TraceID)
	originals, _ := st.RecordText(*stored)
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

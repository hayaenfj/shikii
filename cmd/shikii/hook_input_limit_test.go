package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hook reads at most 4 MiB of JSON from the agent. A longer input is cut, so it is not JSON any more: the hook does nothing,
// prints nothing, exits 0 and leaves one line in hook.log ("the hook never stops the work"). The same turn with an input under
// the limit is checked as usual, so the size alone decides.
func TestHookInputOverTheLimitDoesNothingAndIsLogged(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, ".shikii", "agent", "hook.log")
	hookLogText := func() string { b, _ := os.ReadFile(logPath); return string(b) }

	// a request over the limit is not kept: no turn, no record
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "big", "cwd": repo, "prompt": "add a line to the README " + strings.Repeat("x", 4<<20)}); out != "" {
		t.Errorf("a request over the limit prints nothing: %.200s", out)
	}
	if s := loadSession(root, "big"); s.TurnID != "" || s.Request != "" {
		t.Errorf("a request over the limit is not kept: turn=%q", s.TurnID)
	}
	if log := hookLogText(); strings.Count(log, "stdin is not the agent's hook JSON") != 1 {
		t.Errorf("the cut input leaves one line in hook.log: %q", log)
	}

	// an ordinary request, then a change the guard must stop
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "big", "cwd": repo, "prompt": "add a line to the README"})
	if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the Stop over the limit: nothing printed, no guard record, one more line in hook.log
	if out := runHook(t, "stop", map[string]any{"session_id": "big", "cwd": repo, "last_assistant_message": strings.Repeat("y", 4<<20)}); out != "" {
		t.Errorf("a Stop over the limit prints nothing: %.200s", out)
	}
	if recs := guardRecordsFor(root, "no-word"); len(recs) != 0 {
		t.Errorf("a Stop over the limit checks nothing: %+v", recs)
	}
	if log := hookLogText(); strings.Count(log, "stdin is not the agent's hook JSON") != 2 {
		t.Errorf("the cut Stop input leaves its line in hook.log: %q", log)
	}
	// the same Stop under the limit is checked and stopped
	out := runHook(t, "stop", map[string]any{"session_id": "big", "cwd": repo, "last_assistant_message": strings.Repeat("y", 3<<20)})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") {
		t.Errorf("the same Stop under the limit is stopped by the guard: %.300s", out)
	}
	t.Logf("hook.log after the two cut inputs:\n%s", hookLogText())
	t.Logf("the Stop under the limit: %.160s", out)
}

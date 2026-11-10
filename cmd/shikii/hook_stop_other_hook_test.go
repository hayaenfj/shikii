package main

import (
	"os"
	"strings"
	"testing"
)

// The protection the old condition was written for stays: shikii itself stopped this session's turn, then the session
// record disappeared. The Stop after that block cannot be verified, so it still ends as incomplete.
func TestStopAfterShikiisOwnBlockStillHaltsWhenTheRecordIsGone(t *testing.T) {
	_, root, stop := pendingRecoveryPlan(t) // shikii blocked the first Stop of session "audit" (plan-before-work); stop carries stop_hook_active
	if err := os.Remove(sessionPath(root, "audit")); err != nil {
		t.Fatal(err)
	}
	requireRecoveryHalt(t, runHook(t, "stop", stop))
}

// The stop_hook_active flag only says that some Stop hook (not necessarily shikii's) continued the turn. In a session where shikii
// kept no turn (its only message was a slash command, or it started before the project was initialized), there is nothing of
// shikii's to verify, so the Stop must stay silent instead of ending the turn with "required work could not be verified".
func TestStopAfterAnotherHooksBlockStaysSilentWithoutATurn(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })

	for name, before := range map[string]func(session string){
		"no record at all": func(string) {},
		"only a slash command": func(session string) {
			runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "/review"})
		},
	} {
		session := "h3-" + strings.ReplaceAll(name, " ", "-")
		before(session)
		out := runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "stop_hook_active": true, "last_assistant_message": "done"})
		if strings.Contains(out, `"continue":false`) || strings.Contains(out, "stopReason") {
			t.Errorf("%s: a Stop continued by another tool's hook must not be halted by shikii: %s", name, out)
		}
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
	"strings"
)

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

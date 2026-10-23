package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
	"strings"
)

// 標準は TypeSafe の Jev: 鍵(TYPESAFE_API_KEY)があれば hook は手元のモデルを待たずに hosted へ送る。鍵が無ければ手元のモデルだけ。
func TestHookSendsToJevWhenKeyIsSet(t *testing.T) {
	jev := fakeAgentModel(t, func(string) float64 { return 0.3 })
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo) // 手元のモデルは無い(閉じた口)
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
		if !strings.HasPrefix(s, "serve ") { // 画面(serve)は鍵があっても起こす。起こさないのは手元のモデル(up)
			t.Fatalf("with a key no local model is started: %v", spawned)
		}
	}

	// 鍵があっても SHIKII_MODEL=local なら手元のモデルを使う(設定で選べる)
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

// newest は Records が返す並び(新しい順)の先頭。
func newest(recs []store.Record) *store.Record {
	if len(recs) == 0 {
		return nil
	}
	return &recs[0]
}

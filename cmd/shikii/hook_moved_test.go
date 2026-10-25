package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.9.2: フォルダの名前を変えた後、wiki の置き場が前のパスの名のまま残ると、wiki は黙って空になっていた。
// 依頼の前の hook は、残った置き場をエージェントにセッションに 1 回伝える。
func TestHookTellsWikiLeftUnderOldPath(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	old := notebook.Book{Root: agent, Scope: "/work/old-name"}
	if _, err := old.Capture(notebook.Observation{Kind: "test", Title: "確かめた", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(old.Scope))
	place := "records/wiki-sources/" + hex.EncodeToString(sum[:])
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	first := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	if !strings.Contains(first, "前のパスの名のまま残った wiki の置き場") || !strings.Contains(first, place) {
		t.Fatalf("残った置き場を伝える: %q", first)
	}
	prompt["prompt"] = "次は CHANGELOG の誤字を直してください。"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "前のパスの名のまま") {
		t.Fatalf("同じセッションでは 1 回だけ: %q", again)
	}
}

// 0.9.2: フォルダを移しただけだと project.json の scope が前のパスのままで、hook は黙って止まっていた。
// 前のパスがもう無い(移した)時は、止まっていることと直し方をエージェントに伝える。前のパスが有る(写した)時は今までどおり黙る。
func TestHookTellsMovedProjectMarker(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	t.Chdir(repo)
	mark := func(scope string) {
		data, _ := json.Marshal(map[string]any{"version": projectInitVersionNow, "scope": scope})
		if err := os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gone := filepath.Join(t.TempDir(), "old-name") // もう無いフォルダ
	mark(gone)
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	moved := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	if !strings.Contains(moved, "shikii は動いていません") || !strings.Contains(moved, gone) {
		t.Fatalf("移した時は止まっていることと前のパスを伝える: %q", moved)
	}
	mark(t.TempDir()) // 前のパスが有る = 写した
	if copied := runHook(t, "user-prompt-submit", prompt); copied != "" {
		t.Fatalf("写した時は黙る: %q", copied)
	}
}

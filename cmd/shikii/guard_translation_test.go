package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// Low 1: a forbidden-words guard can be enabled even when a word is part of the English positive example ("his" is inside "this", "is", "line").
func TestAuditTranslationGuardEnableWithEnglishWords(t *testing.T) {
	for _, words := range [][]string{{"his"}, {"is"}, {"line"}, {"clean"}, {"t", "h", "i", "s", "l", "n", "e", "c", "a"}, {"ab", "aab"}} {
		g := guard{ID: "w", Kind: guardKindForbiddenWords, Title: "w", Words: words}
		if r := evalGuard(g, guardPositive(g)); !r.Pass {
			t.Errorf("words %v: the positive example must pass, but it stopped: %s", words, r.Reason)
		}
		neg, _ := guardNegative(g)
		if r := evalGuard(g, neg); r.Pass {
			t.Errorf("words %v: the negative example must stop", words)
		}
	}
	root := t.TempDir()
	if err := saveGuard(root, guard{ID: "no-his", Kind: guardKindForbiddenWords, Title: "his", Words: []string{"his"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := captureErr(func() error { return guardSetEnabled(root, "no-his", true) }); err != nil {
		t.Fatalf("guard enable refused a guard whose word is inside the positive example: %v", err)
	}
}

func captureErr(fn func() error) (string, error) {
	old := stdout
	var b strings.Builder
	stdout = &b
	defer func() { stdout = old }()
	err := fn()
	return b.String(), err
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

// With the English setting the terminal line and the reason stay English only.
func TestAuditTranslationGuardStopLineInEnglish(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
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
	_ = json.Unmarshal([]byte(out), &parsed)
	sys, _ := parsed["systemMessage"].(string)
	recs := guardRecordsFor(root, "no-foo")
	if !strings.Contains(sys, "(no-foo)") || strings.Contains(sys, "\u6b62") || len(recs) != 1 || strings.Contains(recs[0].Reason, "\u4f7f") {
		t.Errorf("with the English setting there is no Japanese: %q %+v", sys, recs)
	}
}

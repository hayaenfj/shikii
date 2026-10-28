package main

// Confirmation audit of the 0.11.0 fix (commit 3f0f657, 2026-10-28): concrete inputs against the fixed properties 9 and 15 of
// .shikii/handoff/audit-0.11/invariants.md and the three small fixes of the same commit. Each test is written to fail on the fixed HEAD
// unless its comment says it confirms the fix; the report (.shikii/handoff/audit-0.11/audit-confirm-1.md) names the failure output.

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	confirmWord     = "\u6839"                                                                               // the forbidden word of these tests
	confirmWordLine = "\u8a18\u9332\u306f\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u306e\u6839\u306b\u7f6e\u304f" // a line holding it
)

// confirmProject is a project with an enabled forbidden-words guard, a README without the word, and a session whose first request was kept.
func confirmProject(t *testing.T) (root, scope, readme string) {
	t.Helper()
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{confirmWord}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": scope, "prompt": "README \u3092\u76f4\u3057\u3066"})
	readme = filepath.Join(scope, "README.md")
	if err := os.WriteFile(readme, []byte("# shikii\n\nintro\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return st.Dir, scope, readme
}

// Confirms the fix of decide: when what the project knows cannot be read, the decision is still returned (200, band, trace_id, no
// knowledge) and recorded exactly as when it can be read (the control call after the folder is readable again).
func TestConfirm011DecideReturnsTheRecordedDecisionWhenTheKnowledgeCannotBeRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads a folder without permission")
	}
	st, scope, _, _, _ := memoryEffectFixture(t)
	srv := httptest.NewServer(newServer(serveOpts{storeDir: st.Dir, projectScope: scope, ef: endpointFlags{local: hookLocalURL, model: "kev-4b"}}))
	t.Cleanup(srv.Close)
	dirs, _ := filepath.Glob(filepath.Join(st.Dir, "wiki", "lessons", "*")) // the folder of the project's pages
	if len(dirs) != 1 {
		t.Fatalf("one pages folder: %v", dirs)
	}
	if err := os.Chmod(dirs[0], 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dirs[0], 0o700) })
	body := map[string]any{"spec": "needs-plan", "inputs": map[string]string{"request": "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"}}
	status, d := postJSON(t, srv.URL+"/v1/decide", body)
	if status != 200 || d["band"] == nil || d["trace_id"] == nil {
		t.Fatalf("the decision alone, 200: %d %v", status, d)
	}
	if d["similar_decisions"] != nil || d["lessons"] != nil {
		t.Fatalf("no knowledge when it could not be read: %v", d)
	}
	recs, err := st.Records("needs-plan", 0)
	if err != nil {
		t.Fatal(err)
	}
	// the control: the same call with the folder readable records the same way and carries the lesson
	if err := os.Chmod(dirs[0], 0o700); err != nil {
		t.Fatal(err)
	}
	status2, d2 := postJSON(t, srv.URL+"/v1/decide", body)
	recs2, err := st.Records("needs-plan", 0)
	if err != nil {
		t.Fatal(err)
	}
	if status2 != 200 || d2["band"] != d["band"] || len(recs2)-len(recs) != len(recs) {
		t.Fatalf("the fallback changes neither the band nor the recording: %d %v then %d %v; %d then %d records", status, d["band"], status2, d2["band"], len(recs), len(recs2))
	}
	if d2["lessons"] == nil {
		t.Fatalf("control: with the folder readable the lesson comes with the band: %v", d2)
	}
	if len(recs) == 1 && recs[0].TraceID != d["trace_id"] {
		t.Fatalf("the recorded decision is the one returned: %+v %v", recs, d["trace_id"])
	}
}

// Property 15: the sentences left beside the ones the fix rewrote still say the hook before a tool never denies. The implementation
// (guardToolCall) returns permissionDecision deny from that hook for an enabled guard.
func TestConfirm011DocsStillSayTheToolHookNeverDenies(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	stale := []struct{ file, text string }{
		{"docs/guide.md", "the hook only adds context (it never allows, denies or asks)"},
		{"docs/guide.ja.md", "\u6587\u8108\u3092\u8db3\u3059\u3060\u3051(\u8a31\u53ef\u3082\u62d2\u5426\u3082\u78ba\u8a8d\u3082\u3057\u306a\u3044)"},
		{"integrations/claude-code.md", "as context only: it never allows, denies or asks"},
		{"integrations/claude-code.ja.md", "\u6587\u8108\u3068\u3057\u3066\u8db3\u3059\u3060\u3051\u3002\u8a31\u53ef\u3082\u62d2\u5426\u3082\u78ba\u8a8d\u3082\u3057\u306a\u3044"},
		{"docs/architecture.md", "context only, it never allows, denies or asks"},
		{"docs/architecture.ja.md", "\u8a31\u53ef\u3082\u62d2\u5426\u3082\u78ba\u8a8d\u3082\u3057\u306a\u3044"},
	}
	for _, c := range stale {
		if strings.Contains(read(c.file), c.text) {
			t.Errorf("%s still says %q, but hook tool-lesson returns permissionDecision deny for an enabled guard", c.file, c.text)
		}
	}
}

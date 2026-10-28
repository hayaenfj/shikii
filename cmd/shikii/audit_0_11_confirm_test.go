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

// Property 9 (MultiEdit): MultiEdit applies its edits one after another, so a later edit's old_string may hold the line an earlier edit
// added. toolAddedLines pools every edit's old lines as "kept", so that line is not counted as added and the guard sees nothing.
func TestConfirm011MultiEditLineAddedByAnEarlierEditIsNotKeptByALaterOne(t *testing.T) {
	root, scope, readme := confirmProject(t)
	edits := []map[string]any{
		{"old_string": "# shikii", "new_string": "# shikii\n" + confirmWordLine},
		{"old_string": "# shikii\n" + confirmWordLine, "new_string": "# shikii\n" + confirmWordLine + "\nmore"},
	}
	out, spec := auditToolCall(t, scope, "c", scope, "MultiEdit", map[string]any{"file_path": readme, "edits": edits})
	if spec["permissionDecision"] != "deny" {
		t.Fatalf("the second edit keeps the line the first edit adds; the line is still added to the file, so the call is refused:\n%q", out)
	}
	if recs := guardRecordsFor(root, "no-ne"); len(recs) != 1 || recs[0].Pass || !recs[0].Tool {
		t.Fatalf("and the stop is recorded for the turn: %+v", recs)
	}
}

// Property 9 (the "inside the project" rule of the fix): a file inside the project is still checked when its path reaches the project
// another way. The fix compares the raw file_path with the project's real path (EvalSymlinks), so a path through a link, another case
// on a case-insensitive disk, or a path relative to the cwd is taken for a file outside the project and the word passes.
func TestConfirm011AFileInsideTheProjectIsCheckedWhateverSpellingReachesIt(t *testing.T) {
	_, scope, _ := confirmProject(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(scope, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	t.Run("through a link to the project folder", func(t *testing.T) {
		out, spec := auditToolCall(t, scope, "c", link, "Write", map[string]any{"file_path": filepath.Join(link, "README.md"), "content": confirmWordLine + "\n"})
		if spec["permissionDecision"] != "deny" {
			t.Fatalf("README.md reached through a link to the project is the project's file:\n%q", out)
		}
	})
	t.Run("relative to the cwd", func(t *testing.T) {
		out, spec := auditToolCall(t, scope, "c", scope, "Write", map[string]any{"file_path": "README.md", "content": confirmWordLine + "\n"})
		if spec["permissionDecision"] != "deny" {
			t.Fatalf("README.md relative to the cwd inside the project is the project's file:\n%q", out)
		}
	})
	t.Run("another case on a case-insensitive disk", func(t *testing.T) {
		other := strings.ToUpper(scope)
		if other == scope || !sameFolder(other, scope) {
			t.Skip("the disk is case-sensitive or the path has no letters")
		}
		out, spec := auditToolCall(t, scope, "c", scope, "Write", map[string]any{"file_path": filepath.Join(other, "README.md"), "content": confirmWordLine + "\n"})
		if spec["permissionDecision"] != "deny" {
			t.Fatalf("README.md spelled in another case reaches the same file on this disk:\n%q", out)
		}
	})
}

// The fix says Write and NotebookEdit do not count a line the file already holds. For NotebookEdit the file is JSON (.ipynb) whose
// cell sources are quoted strings, so no raw line of the file ever equals a line of new_source: a cell that already holds the word is
// refused on any edit, as before the fix.
func TestConfirm011NotebookEditKeepsTheLinesTheCellAlreadyHolds(t *testing.T) {
	_, scope, _ := confirmProject(t)
	nb := filepath.Join(scope, "notes.ipynb")
	cell := `{"cells":[{"cell_type":"markdown","id":"a","metadata":{},"source":["` + confirmWordLine + `\n"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}` + "\n"
	if err := os.WriteFile(nb, []byte(cell), 0o644); err != nil {
		t.Fatal(err)
	}
	out, spec := auditToolCall(t, scope, "c", scope, "NotebookEdit", map[string]any{"notebook_path": nb, "cell_id": "a", "edit_mode": "replace", "new_source": confirmWordLine + "\nmore\n"})
	if spec["permissionDecision"] != nil {
		t.Fatalf("a line the cell already holds is not added (the fix's claim for NotebookEdit):\n%q", out)
	}
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

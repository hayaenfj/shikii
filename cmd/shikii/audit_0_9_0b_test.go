package main

// The recheck of 0.9.0 (added by the auditor).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// New-1: on an AGENTS.md with a marker only inside the person's text and no section, the update (hook install) creates a section (against "do not create one in a file without a section").
func TestAudit090bHookInstallDoesNotCreateSectionForInlineMarker(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	human := "# 作業規則\n\n- shikii の節は `" + agentsMarker + "` で囲む決まりだが、この repo では節を置かない\n"
	os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(human), 0o644)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if b, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md")); string(b) != human {
		t.Fatalf("created a section in an AGENTS.md without one:\n%s", b)
	}
}

// New-2: just because another page is missing its source (lint reports a problem, but saving is not refused), the hook stops asking for pages to classify. classify passes.
func TestAudit090bSourceIssueElsewhereDoesNotBlockAsk(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	other := fmt.Sprintf("%016x", 778)
	if err := st.AppendFeedback(scope, store.Feedback{ID: other, Request: "r2", Summary: "s2", Reply: "y"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "前の版の記事", Lesson: "前の版の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(a.Page.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{other}, Title: "出典の欠けた記事", Lesson: "別の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"}); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(agent, "*", "*", "sources", other+".json"))
	if len(m) == 0 {
		m, _ = filepath.Glob(filepath.Join(agent, "**", other+".json"))
	}
	if len(m) == 0 {
		filepath.Walk(agent, func(p string, info os.FileInfo, err error) error {
			if err == nil && filepath.Base(p) == other+".json" {
				m = append(m, p)
			}
			return nil
		})
	}
	if len(m) != 1 {
		t.Fatalf("source file not found: %v", m)
	}
	os.Remove(m[0])
	text, ids, err := classifyContext(agent, scope)
	if err != nil {
		t.Fatal(err)
	}
	_, cerr := book.Classify(a.Page.ID, "rule", "作業する時")
	if cerr == nil && len(ids) == 0 {
		t.Fatalf("classify passes (%v), yet the hook asks for no pages and says it cannot classify: %s", cerr, text)
	}
}

// Check: classifying the 5 pages the hook asked for, one by one, all pass and Stop does not block. Classifying different pages at the same time is not refused.
func TestAudit090bClassifyFiveInOrderAndInParallel(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	src := fmt.Sprintf("%016x", 901)
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	var ids []string
	for i := 0; i < 10; i++ {
		r, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("前の版の記事 %d", i), Lesson: fmt.Sprintf("前の版の教訓 %d。", i), AppliesTo: "a", Kind: "rule", Scene: "作業する時", Related: func() []string {
			if len(ids) > 0 {
				return []string{ids[len(ids)-1]}
			}
			return nil
		}()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Page.ID)
	}
	for _, id := range ids {
		if err := book.RewriteWithoutKindScene(id); err != nil {
			t.Fatal(err)
		}
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "q", "cwd": repo, "prompt": "README を直して"})
	s := loadSession(root, "q")
	if len(s.ClassifyAsked) != 5 {
		t.Fatalf("asks for 5: %v", s.ClassifyAsked)
	}
	for _, id := range s.ClassifyAsked {
		if _, err := book.Classify(id, "fact", "作業する時"); err != nil {
			t.Fatalf("passes when done in order: %s %v", id, err)
		}
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "q", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "need a kind and a scene") {
		t.Fatalf("blocked even though classifying is done: %s", out)
	}
	un, _ := book.Unclassified()
	var wg sync.WaitGroup
	errs := make([]error, len(un))
	for i, p := range un {
		wg.Add(1)
		go func(i int, id string) { defer wg.Done(); _, errs[i] = book.Classify(id, "fact", "作業する時") }(i, p.ID)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("classifying different pages at the same time is not refused: %s %v", un[i].ID, err)
		}
	}
	// Lesson, related and change reason do not change
	live, _ := book.Live()
	for _, p := range live {
		if !strings.HasPrefix(p.Lesson, "前の版の教訓") || (p.Title != "前の版の記事 0" && len(p.Related) != 1) {
			t.Fatalf("classify changed the lesson or related: %+v", p)
		}
	}
}

// Check: edge cases of counting marker pairs (empty file, file without a trailing newline, a section made by 0.8.3, file ending in the closing marker without a newline, the second run does nothing).
func TestAudit090bAgentsSectionEdges(t *testing.T) {
	old083 := agentsMarker + "\n## shikii(判定の作法)\n\n- 同じ種類の判断は自分で決めず\n- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)\n" + agentsMarker + "\n"
	cases := []struct{ name, in, wantPrefix, wantSuffix string }{
		{"empty", "", agentsMarker, agentsMarker + "\n"},
		{"no newline", "# 規則", "# 規則\n\n" + agentsMarker, agentsMarker + "\n"},
		{"0.8.3 section (added to a file without a newline)", "# 規則\n\n" + old083, "# 規則\n\n" + agentsMarker, agentsMarker + "\n"},
		{"ends with the closing marker, no newline", "# 規則\n\n" + strings.TrimSuffix(old083, "\n"), "# 規則\n\n" + agentsMarker, agentsMarker + "\n"},
		{"person's text after the section", "# 規則\n\n" + old083 + "\n## 後ろ\n", "# 規則\n\n" + agentsMarker, agentsMarker + "\n\n## 後ろ\n"},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "AGENTS.md")
		os.WriteFile(path, []byte(c.in), 0o644)
		if _, err := addAgentsSection(path); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		b, _ := os.ReadFile(path)
		got := string(b)
		if !strings.HasPrefix(got, c.wantPrefix) || !strings.HasSuffix(got, c.wantSuffix) || strings.Count(got, agentsMarker) != 2 || !strings.Contains(got, "wiki classify") {
			t.Fatalf("%s: %q", c.name, got)
		}
		if did, err := addAgentsSection(path); did || err != nil {
			t.Fatalf("%s: the second run does nothing: %v %v", c.name, did, err)
		}
		if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
			t.Fatalf("%s: a temporary file was left: %v", c.name, entries)
		}
	}
	// A link inside the repo (CRLF) stays a link and stays CRLF
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(strings.ReplaceAll("# 規則\n\n"+old083, "\n", "\r\n")), 0o600)
	os.Symlink("CLAUDE.md", filepath.Join(dir, "AGENTS.md"))
	if did, err := addAgentsSection(filepath.Join(dir, "AGENTS.md")); !did || err != nil {
		t.Fatal(did, err)
	}
	info, _ := os.Lstat(filepath.Join(dir, "AGENTS.md"))
	real, _ := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if info.Mode()&os.ModeSymlink == 0 || real.Mode().Perm() != 0o600 || strings.Contains(strings.ReplaceAll(string(b), "\r\n", ""), "\n") || !strings.Contains(string(b), "wiki classify") {
		t.Fatalf("link, permissions, CRLF: link=%v perm=%v %q", info.Mode()&os.ModeSymlink != 0, real.Mode().Perm(), b)
	}
}

// Hole: with 3 marker-only lines (a person wrote a marker-only line inside the section), do not touch it. Removing the len(marks) == 2 condition did not make the existing tests fail.
func TestAudit090bAgentsThreeMarkerLinesUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	in := "# 規則\n\n" + agentsMarker + "\n## shikii(判定の作法)\n\n- 古い\n" + agentsMarker + "\n- 人が節の中に足した大事な決まり\n" + agentsMarker + "\n"
	os.WriteFile(path, []byte(in), 0o644)
	addAgentsSection(path)
	if b, _ := os.ReadFile(path); string(b) != in {
		t.Fatalf("with 3 marker-only lines, do not touch it:\n%s", b)
	}
}

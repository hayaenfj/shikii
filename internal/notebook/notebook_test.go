package notebook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func seed(t *testing.T, b Book, n int) string {
	t.Helper()
	id := fmt.Sprintf("%016x", n)
	err := (store.Store{Dir: b.Root}).AppendFeedback(b.Scope, store.Feedback{
		ID: id, Request: "README install instructions", Summary: "updated instructions", Reply: "model startup instructions are missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func draft(id, title string) Draft {
	return Draft{Title: title, Lesson: "Check model startup instructions when updating README installation instructions.", AppliesTo: "README installation instructions", Kind: "rule", Scene: "README を直す時", Sources: []string{id}}
}

func hasIssue(issues []Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func TestNotebookPreservesSourcesAndRevisionHistory(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	d := draft(id, "Installation instructions")
	first, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	sources, _ := b.directory(false, "sources")
	original, err := os.ReadFile(filepath.Join(sources, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := immutable(filepath.Join(sources, id+".json"), []byte("replacement")); err == nil {
		t.Fatal("a saved source record cannot be overwritten with different content")
	}
	pages, _ := b.directory(false, "pages")
	log, _ := os.ReadFile(filepath.Join(pages, "log.md"))
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(pages, "log.md"))
	if !bytes.Equal(log, again) {
		t.Fatal("repeating the same ingestion does not grow the history")
	}
	id2 := seed(t, b, 2)
	d.ID, d.Sources, d.Lesson = first.Page.ID, []string{id2}, "Also check model startup instructions on a fresh installation."
	second, err := b.Ingest(d)
	if err != nil || len(second.Page.Sources) != 2 || second.Page.Revision == first.Page.Revision {
		t.Fatalf("an update keeps the previous sources: %+v %v", second, err)
	}
	versions, _ := filepath.Glob(filepath.Join(pages, "revisions", first.Page.ID+"-*.md"))
	if len(versions) != 2 {
		t.Fatalf("does not delete the previous revision: %v", versions)
	}
	for i := 3; i < 265; i++ {
		seed(t, b, i)
	}
	material, err := b.ReadSources([]string{id})
	if err != nil || len(material["source_material"].([]store.Feedback)) != 1 {
		t.Fatalf("an original that dropped out of recent conversations still stays as a source: %v %v", material, err)
	}
	after, _ := os.ReadFile(filepath.Join(sources, id+".json"))
	if !bytes.Equal(original, after) {
		t.Fatal("updating a lesson does not rewrite the source record")
	}
	view, err := b.Query("model startup instructions")
	if err != nil || len(view.Lessons) != 1 || len(view.Issues) != 0 || len(view.Pending) != 2 {
		t.Fatalf("lessons and unorganized conversations are searched separately: %+v %v", view, err)
	}
	for _, query := range []string{"startup", "model startup"} {
		view, err := b.Query(query)
		if err != nil || len(view.Lessons) != 1 {
			t.Fatalf("finds a long lesson with a short search term: query=%q lessons=%d err=%v", query, len(view.Lessons), err)
		}
	}
	if view, err := b.Query("billing payments"); err != nil || len(view.Lessons) != 0 {
		t.Fatal("returns no lesson for unrelated search terms")
	}
	if info, err := os.Stat(filepath.Join(sources, id+".json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("only the owner can read source records")
	}
}

func TestNotebookRejectsForeignSourcesTamperingAndSymlinks(t *testing.T) {
	t.Run("work boundary", func(t *testing.T) {
		root := t.TempDir()
		a, b := Book{Root: root, Scope: "/a/project"}, Book{Root: t.TempDir(), Scope: "/b/project"} // each project has its own store
		id := seed(t, a, 1)
		if _, err := a.Ingest(draft(id, "local lesson")); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Ingest(draft(id, "foreign")); err == nil {
			t.Fatal("must not quote an observation from another project with the same name")
		}
		if _, err := b.ReadSources([]string{id}); err == nil {
			t.Fatal("reading source records is also limited to the same work")
		}
		if _, err := a.Ingest(draft("../../outside", "invalid")); err == nil {
			t.Fatal("a source ID must not be a path")
		}
	})
	t.Run("source changed", func(t *testing.T) {
		b := Book{Root: t.TempDir(), Scope: "/project"}
		id := seed(t, b, 1)
		if _, err := b.Ingest(draft(id, "original")); err != nil {
			t.Fatal(err)
		}
		dir, _ := b.directory(false, "sources")
		path := filepath.Join(dir, id+".json")
		original, _ := os.ReadFile(path)
		_ = os.WriteFile(path, bytes.ReplaceAll(original, []byte("missing"), []byte("present")), 0o600)
		view, err := b.Query("")
		if err != nil || len(view.Lessons) != 0 || !hasIssue(view.Issues, "changed-source") {
			t.Fatalf("drops a lesson whose source changed from the results: %+v %v", view, err)
		}
		if _, err := b.ReadSources([]string{id}); err == nil {
			t.Fatal("does not return a modified original as a trusted source")
		}
		if _, err := b.Ingest(draft(id, "another title")); err == nil {
			t.Fatal("re-ingesting under another title does not legitimize the changed source")
		}
		report, err := b.Lint()
		if err != nil || !hasIssue(report.Issues, "changed-source") {
			t.Fatalf("the check shows the source changed: %+v %v", report, err)
		}
		_ = os.Remove(path)
		view, _ = b.Query("")
		if len(view.Lessons) != 0 || !hasIssue(view.Issues, "missing-source") {
			t.Fatal("also detects a missing original")
		}
	})
	t.Run("generated page changed", func(t *testing.T) {
		b := Book{Root: t.TempDir(), Scope: "/project"}
		result, err := b.Ingest(draft(seed(t, b, 1), "original"))
		if err != nil {
			t.Fatal(err)
		}
		dir, _ := b.directory(false, "pages")
		f, err := os.OpenFile(filepath.Join(dir, result.Page.ID+".md"), os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("\nchanged body\n")
		f.Close()
		view, err := b.Query("")
		if err != nil || len(view.Lessons) != 0 || !hasIssue(view.Issues, "changed-page") {
			t.Fatal("also detects a rewrite of only the body")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root, outside := t.TempDir(), t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "wiki"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "wiki", "lessons")); err != nil {
			t.Fatal(err)
		}
		b := Book{Root: root, Scope: "/project"}
		if _, err := b.Ingest(draft(seed(t, b, 1), "escaped")); err == nil {
			t.Fatal("does not write through a symlink in the generated area")
		}
		files, _ := os.ReadDir(outside)
		if len(files) != 0 {
			t.Fatal("does not change anything outside the generated area")
		}
	})
}

func TestNotebookReplacesLessonsAndMaintainsIndexOnASlowCycle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	old, err := b.Ingest(draft(seed(t, b, 1), "old instructions"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(seed(t, b, 2), "new instructions")
	d.Replaces = []string{old.Page.ID}
	newer, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := b.Query("")
	if len(view.Lessons) != 1 || view.Lessons[0].ID != newer.Page.ID {
		t.Fatal("does not recommend the lesson from before the update at the same time")
	}
	d.ID, d.Replaces = old.Page.ID, []string{newer.Page.ID}
	if _, err := b.Ingest(d); err == nil {
		t.Fatal("lesson replacements must not form a cycle")
	}
	// When the interpretation of a replacement is corrected it can be cleared, and restoring it again keeps the history.
	undo := draft(newer.Page.Sources[0].ID, newer.Page.Title)
	undo.ID, undo.Replaces = newer.Page.ID, []string{}
	restored, err := b.Ingest(undo)
	if err != nil {
		t.Fatal(err)
	}
	view, _ = b.Query("")
	if len(view.Lessons) != 2 {
		t.Fatal("an explicit empty replaces clears the replacement")
	}
	undo.Replaces = []string{old.Page.ID}
	again, err := b.Ingest(undo)
	if err != nil || again.Page.Previous != restored.Page.Revision || again.Page.Revision == newer.Page.Revision {
		t.Fatal("returning to the same content also leaves history as a new change")
	}
	dir, _ := b.directory(false, "pages")
	_ = os.Remove(filepath.Join(dir, "index.md"))
	report, err := b.Lint()
	if err != nil || len(report.Repaired) != 1 || len(report.Issues) != 0 {
		t.Fatalf("the index can be repaired automatically: %+v %v", report, err)
	}
	if err := b.MaybeLint(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.md"), []byte("stale index"), 0o600)
	if err := b.MaybeLint(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "index.md"))
	if string(data) != "stale index" {
		t.Fatal("does not repeat the check every turn on the same day")
	}
	before := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, ".last-lint"), before, before)
	if err := b.MaybeLint(); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "index.md"))
	if string(data) == "stale index" {
		t.Fatal("repairs the index in the next cycle")
	}
	_ = os.Remove(filepath.Join(dir, old.Page.ID+".md"))
	report, err = b.Lint()
	if err != nil || !hasIssue(report.Issues, "missing-replaced-page") {
		t.Fatal("detects links to pages that disappeared")
	}
}

func TestNotebookConcurrentIngestKeepsEveryLessonAndLog(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := 0; i < 12; i++ {
		ids = append(ids, seed(t, b, i))
	}
	dir, err := b.directory(true, "pages")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() { _, err := b.Ingest(draft(ids[0], "Instructions 0")); blocked <- err }()
	select {
	case err := <-blocked:
		unlock()
		t.Fatalf("ingestion waits while another update is running: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.Ingest(draft(id, fmt.Sprintf("Instructions %d", i)))
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	pages, issues, err := b.pages()
	if err != nil || len(pages) != 12 || len(issues) != 0 {
		t.Fatalf("parallel ingestion does not lose lessons: pages=%d issues=%v err=%v", len(pages), issues, err)
	}
	dir, _ = b.directory(false, "pages")
	log, _ := os.ReadFile(filepath.Join(dir, "log.md"))
	if strings.Count(string(log), " | ingest ") != 12 {
		t.Fatal("does not lose the change history either")
	}
	report, err := b.Lint()
	if err != nil || len(report.Issues) != 0 || len(report.Repaired) != 0 {
		t.Fatalf("sources, index and past revisions stay consistent after parallel work: %+v %v", report, err)
	}
}

// 0.9.0: a page with a lesson is not saved without a kind (one of 5) and a scene (a "when ..." phrase, up to SceneMax characters; 24 before 0.9.2). Every path (CLI, MCP, HTTP, reflection)
// goes through here, so every agent adds them. When editing an existing page they are inherited from it if omitted. A page from the previous version (no kind or scene)
// cannot be saved when edited without them (so they fill in after the upgrade).
func TestIngestRequiresKindAndScene(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	d := draft(id, "決まりの記事")
	d.Kind, d.Scene = "", ""
	if _, err := b.Ingest(d); err == nil || !strings.Contains(err.Error(), "kind") || !strings.Contains(err.Error(), "scene") {
		t.Fatalf("refuses a lesson without kind and scene and says how to fix it: %v", err)
	}
	for _, bad := range []struct{ kind, scene string }{{"memo", "コミットする時"}, {"rule", ""}, {"rule", strings.Repeat("あ", SceneMax+1)}, {"rule", "二行\n目"}} {
		d.Kind, d.Scene = bad.kind, bad.scene
		if _, err := b.Ingest(d); err == nil {
			t.Fatalf("kind is one of 5, scene is one line up to SceneMax characters: %+v", bad)
		}
	}
	d.Kind, d.Scene = "rule", "コミットする時"
	first, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	if first.Page.Kind != "rule" || first.Page.Scene != "コミットする時" {
		t.Fatalf("keeps kind and scene on the page: %+v", first.Page)
	}
	// Inherited if omitted when editing
	d.ID, d.Kind, d.Scene, d.Lesson = first.Page.ID, "", "", d.Lesson+" 追記。"
	again, err := b.Ingest(d)
	if err != nil || again.Page.Kind != "rule" || again.Page.Scene != "コミットする時" {
		t.Fatalf("kind and scene are inherited if omitted when editing: %+v %v", again.Page, err)
	}
	// A page from the previous version without kind or scene cannot be saved when edited without them
	legacyDraft := draft(id, "前の版の記事")
	lres, err := b.Ingest(legacyDraft)
	if err != nil {
		t.Fatal(err)
	}
	legacy := *lres.Page
	if err := b.RewriteWithoutKindScene(legacy.ID); err != nil {
		t.Fatal(err)
	}
	d.ID, d.Title = legacy.ID, legacy.Title
	if _, err := b.Ingest(d); err == nil {
		t.Fatal("a page from the previous version without kind or scene gets them when edited")
	}
	d.Kind, d.Scene = "howto", "公開する時"
	if res, err := b.Ingest(d); err != nil || res.Page.Kind != "howto" {
		t.Fatalf("saves once they are given: %+v %v", res.Page, err)
	}
}

// 0.9.0: pages made before the upgrade (no kind or scene) are listed by Unclassified, and Classify adds only kind and scene
// (lesson, title and body unchanged; it becomes a new revision linked to the previous one). Once added, the page leaves the list.
func TestClassifyLegacyPages(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	first, err := b.Ingest(draft(id, "決まりの記事"))
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	lres, err := b.Ingest(draft(id, "前の版の記事"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := *lres.Page
	if err := b.RewriteWithoutKindScene(legacy.ID); err != nil {
		t.Fatal(err)
	}
	un, err := b.Unclassified()
	if err != nil || len(un) != 1 || un[0].ID != legacy.ID {
		t.Fatalf("lists only pages missing kind or scene: %+v %v", un, err)
	}
	if _, err := b.Classify(legacy.ID, "memo", "公開する時"); err == nil {
		t.Fatal("kind is one of 5")
	}
	if _, err := b.Classify("00000000000000cc", "rule", "公開する時"); err == nil {
		t.Fatal("cannot classify a page that does not exist")
	}
	res, err := b.Classify(legacy.ID, "howto", "公開する時")
	if err != nil {
		t.Fatal(err)
	}
	if p := res.Page; p.Kind != "howto" || p.Scene != "公開する時" || p.Lesson != legacy.Lesson || p.Title != legacy.Title || p.Previous == "" {
		t.Fatalf("makes a new revision with only kind and scene added: %+v", p)
	}
	if un, _ := b.Unclassified(); len(un) != 0 {
		t.Fatalf("once added, the page leaves the list: %+v", un)
	}
}

// 0.9.0: the first reply of a wiki search (CLI, MCP, HTTP) carries the count of pages without kind and scene and up to 5 of them (it reaches agents without a hook too).
func TestQueryPageListsUnclassifiedPages(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	res, err := b.Ingest(draft(id, "前の版の記事"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := b.QueryPage("", "")
	if err != nil || v.UnclassifiedCount != 0 || len(v.Unclassified) != 0 {
		t.Fatalf("shows nothing when all are filled in: %+v %v", v.Unclassified, err)
	}
	if err := b.RewriteWithoutKindScene(res.Page.ID); err != nil {
		t.Fatal(err)
	}
	v, err = b.QueryPage("", "")
	if err != nil || v.UnclassifiedCount != 1 || len(v.Unclassified) != 1 || v.Unclassified[0].ID != res.Page.ID || !strings.Contains(v.UnclassifiedHow, "classify") {
		t.Fatalf("carries the count of pages without kind and scene, and how to fill them in: %+v %d %q %v", v.Unclassified, v.UnclassifiedCount, v.UnclassifiedHow, err)
	}
}

// audit 0.9.0 M12: refuse kind or scene on a draft without a lesson (one that only marks source records as organized) (they would go unused and look like a wrong save).
func TestReviewOnlyDraftRejectsKindAndScene(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	for _, d := range []Draft{{Sources: []string{id}, Kind: "rule"}, {Sources: []string{id}, Scene: "README を直す時"}} {
		if _, err := b.Ingest(d); err == nil {
			t.Fatalf("refuses kind or scene on a draft without a lesson: %+v", d)
		}
	}
	if _, err := b.Ingest(Draft{Sources: []string{id}}); err != nil {
		t.Fatalf("passes with only source records: %v", err)
	}
}

// 0.9.2 (audit 0.9.1 test-speed low-7): a page's last update (the "new" mark and the order on the knowledge screen) is decided by the save time in
// the wiki log (log.md), not by the file time. Copying the wiki to another machine with cp set every file time to that moment, and every page got "new".
// Only pages missing from the log (a wiki whose log is gone) fall back to the file time.
func TestPageTimesUseLogNotFileTime(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	before := time.Now().UTC().Add(-time.Second)
	res, err := b.Ingest(draft(id, "決まりの記事"))
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Add(time.Second)
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	copied := time.Date(2031, 1, 2, 3, 4, 5, 0, time.UTC) // the time of the cp
	if err := os.Chtimes(filepath.Join(dir, res.Page.ID+".md"), copied, copied); err != nil {
		t.Fatal(err)
	}
	// Logs of operations that do not change the page (page reviews) do not count as a last update
	f, err := os.OpenFile(filepath.Join(dir, "log.md"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, "- 2030-01-01T00:00:00Z | maintain %s@k\n", res.Page.ID)
	f.Close()
	times, err := b.PageTimes()
	if err != nil {
		t.Fatal(err)
	}
	if at := times[res.Page.ID]; at.Before(before) || at.After(after) {
		t.Fatalf("the last update is the time of the save in the log (%s to %s): %s", before, after, at)
	}
	if err := os.Remove(filepath.Join(dir, "log.md")); err != nil {
		t.Fatal(err)
	}
	if times, err := b.PageTimes(); err != nil || !times[res.Page.ID].Equal(copied) {
		t.Fatalf("pages missing from the log fall back to the file time: %v %v", times, err)
	}
}

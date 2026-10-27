package notebook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	// Another update holds the wiki's lock and ingests a lesson at the moment this ingestion finds the lock busy (lockBusy). This one must
	// wait, then read the pages and the log, so neither lesson nor log line is lost.
	unlock, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	waited := false
	lockBusy = func() {
		if waited {
			return
		}
		waited = true
		unlock()
		if _, err := b.Ingest(draft(ids[1], "Instructions 1")); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { lockBusy = nil })
	if _, err := b.Ingest(draft(ids[0], "Instructions 0")); err != nil {
		t.Fatal(err)
	}
	lockBusy = nil
	if !waited {
		unlock()
		t.Fatal("ingestion waits while another update is running")
	}
	for i, id := range ids[2:] {
		if _, err := b.Ingest(draft(id, fmt.Sprintf("Instructions %d", i+2))); err != nil {
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

// Also runs through JSON with an old-format reader, to detect unknown fields being silently dropped.
func articleDraft(t *testing.T, d Draft, fields map[string]any) Draft {
	t.Helper()
	data, _ := json.Marshal(d)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	for k, v := range fields {
		obj[k] = v
	}
	data, _ = json.Marshal(obj)
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func articleField(t *testing.T, p *Page, key string) any {
	t.Helper()
	data, _ := json.Marshal(p)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	return obj[key]
}

func TestNotebookArticleKeepsSummaryAndVersions(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	d := draft(seed(t, b, 1), "Installation")
	legacy, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := b.directory(false, "pages")
	legacyPath := filepath.Join(dir, "revisions", legacy.Page.ID+"-"+legacy.Page.Revision+".md")
	before, _ := os.ReadFile(legacyPath)
	body := "## Background\n\n" + strings.Repeat("Model lifecycle evidence. ", 120) + "\n\n## Unresolved\n\nquartzretention remains unverified."
	rich := articleDraft(t, d, map[string]any{"body": body})
	got, err := b.Ingest(rich)
	if err != nil || articleField(t, got.Page, "body") != body || got.Page.Lesson != d.Lesson || got.Page.Previous != legacy.Page.Revision {
		t.Fatalf("keeps the search summary and the long knowledge body separately: %+v %v", got, err)
	}
	v, err := b.Query("quartzretention")
	if err != nil || len(v.Lessons) != 1 || articleField(t, &v.Lessons[0], "body") != body {
		t.Fatalf("finds words that appear only in the body: %+v %v", v, err)
	}
	if recalled, err := b.Recall("quartzretention"); err != nil || len(recalled) != 0 {
		t.Fatal("does not deliver automatically on a chance match in a long body")
	}
	update := draft(d.Sources[0], d.Title)
	update.ID, update.Lesson = got.Page.ID, "Check startup and shutdown."
	next, err := b.Ingest(update)
	if err != nil || articleField(t, next.Page, "body") != body || next.Page.Previous != got.Page.Revision {
		t.Fatal("an old-format update does not erase the body", err)
	}
	cleared, err := b.Ingest(articleDraft(t, update, map[string]any{"body": ""}))
	if err != nil || articleField(t, cleared.Page, "body") != nil {
		t.Fatal("an explicit empty body clears it", err)
	}
	after, _ := os.ReadFile(legacyPath)
	if !bytes.Equal(before, after) {
		t.Fatal("does not rewrite an existing revision")
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("checks old-format and new-format revisions together: %+v %v", report, err)
	}
	for _, body := range []string{strings.Repeat("x", (16<<10)+1), string([]byte{0xff})} {
		update.Body = &body
		if _, err := b.Ingest(update); err == nil {
			t.Fatal("rejects an oversized body and invalid UTF-8")
		}
	}
	empty := ""
	if _, err := b.Ingest(Draft{Sources: d.Sources, Body: &empty}); err == nil {
		t.Fatal("passing only a body does not mark the sources as organized")
	}
}

func TestNotebookRelatedPagesAndExactLookup(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	a, err := b.Ingest(draft(seed(t, b, 1), "Startup"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(seed(t, b, 2), "Shutdown")
	linked, err := b.Ingest(articleDraft(t, d, map[string]any{"related": []string{a.Page.ID}}))
	if err != nil || articleField(t, linked.Page, "related") == nil {
		t.Fatal("saves relations between knowledge pages", err)
	}
	v, err := b.Query("lesson:" + linked.Page.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != linked.Page.ID {
		t.Fatal("reads a page directly from a related link", err)
	}
	if view, err := b.Query("lesson:ffffffffffffffff"); err != nil || len(view.Lessons) != 0 {
		t.Fatal("does not return another search result in place of a missing page")
	}
	dir, _ := b.directory(false, "pages")
	markdown, _ := os.ReadFile(filepath.Join(dir, linked.Page.ID+".md"))
	if !bytes.Contains(markdown, []byte("]("+a.Page.ID+".md)")) {
		t.Fatal("related pages open from the generated Markdown too")
	}
	d.ID = linked.Page.ID
	d.Lesson = "New shutdown summary."
	kept, err := b.Ingest(d)
	if err != nil || articleField(t, kept.Page, "related") == nil {
		t.Fatal("keeps omitted relations", err)
	}
	// Relations may be mutual. This means something different from a replacement cycle.
	reverse := draft(a.Page.Sources[0].ID, a.Page.Title)
	reverse.ID = a.Page.ID
	if _, err := b.Ingest(articleDraft(t, reverse, map[string]any{"related": []string{linked.Page.ID}})); err != nil {
		t.Fatal("allows mutual related links", err)
	}
	other := Book{Root: t.TempDir(), Scope: "/other"} // another project has its own store
	foreign, err := other.Ingest(draft(seed(t, other, 3), "Foreign"))
	if err != nil {
		t.Fatal(err)
	}
	for _, related := range [][]string{{d.ID}, {foreign.Page.ID}, {"ffffffffffffffff"}, {"../outside"}} {
		if _, err := b.Ingest(articleDraft(t, d, map[string]any{"related": related})); err == nil {
			t.Fatalf("relates only other pages of the same project: %v", related)
		}
	}
	duplicates := make([]string, 12)
	for i := range duplicates {
		duplicates[i] = a.Page.ID
	}
	merged, err := b.Ingest(articleDraft(t, d, map[string]any{"related": duplicates}))
	if err != nil || len(merged.Page.Related) != 1 {
		t.Fatal("relations are not refused by count; duplicates are removed", err)
	}
	_ = os.Remove(filepath.Join(dir, a.Page.ID+".md"))
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "missing-related-page") {
		t.Fatalf("detects related pages that disappeared later: %+v %v", report, err)
	}
	if _, err := b.Ingest(articleDraft(t, d, map[string]any{"related": []string{}})); err != nil {
		t.Fatal("an explicit empty array clears broken relations", err)
	}
}

func TestNotebookLegacyRevisionCompatibility(t *testing.T) {
	p := Page{ID: "0000000000000001", Title: "Example", Lesson: "Summary", AppliesTo: "Installation", Status: "hypothesis", Sources: []Source{{ID: "0000000000000002", Hash: "example"}}}
	legacyJSON := `{"id":"0000000000000001","title":"Example","lesson":"Summary","applies_to":"Installation","status":"hypothesis","sources":[{"id":"0000000000000002","hash":"example"}],"revision":""}`
	if revision(p) != digest([]byte(legacyJSON))[:16] {
		t.Fatal("does not change the hash of existing pages without body or relations")
	}
}

// When another agent edits a lesson while classify runs, classify must not overwrite it with the old lesson it read (the edit would silently
// revert). Checked in one run, without waiting on a race: the edit is saved right after classify has read the page and before it saves.
func TestAudit090ClassifyDoesNotRevertConcurrentEdit(t *testing.T) {
	root := t.TempDir()
	scope := "/audit/race"
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: "00000000000003e7", Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	b := Book{Root: root, Scope: scope}
	draft := Draft{Sources: []string{"00000000000003e7"}, Title: "contested page", Lesson: "Old lesson.", AppliesTo: "a", Kind: "rule", Scene: "when working"}
	res, err := b.Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	id := res.Page.ID
	if err := b.RewriteWithoutKindScene(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { classifyRead = func() {} })
	var editErr error
	met := 0
	classifyRead = func() {
		classifyRead = func() {}
		met++
		edit := draft
		edit.ID, edit.Lesson = id, "Edited lesson."
		_, editErr = b.Ingest(edit)
	}
	_, classifyErr := b.Classify(id, "fact", "when working")
	if met != 1 || editErr != nil {
		t.Fatalf("the edit is saved between classify's read and its save: met %d, %v", met, editErr)
	}
	live, err := b.Live()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live {
		if p.ID == id && p.Lesson != "Edited lesson." {
			t.Fatalf("a successfully saved lesson edit was reverted by classify to %q (classify gave %v)", p.Lesson, classifyErr)
		}
	}
	if classifyErr == nil {
		t.Fatal("classify refuses to save over a page edited after it read it")
	}
}

// Audit 0.9.2 (English): what shikii writes for an English user must be English.
var japanese = regexp.MustCompile("[\u3040-\u30ff\u4e00-\u9fff]")

// Every wiki page markdown (read by agents that open .shikii/wiki, and by people) carries Japanese labels on main.
func TestAuditEnglishWikiPageMarkdownHasNoJapanese(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	body := "Exit 1 means a mismatch."
	d := Draft{Title: "Check invoice totals", Lesson: "Run scripts/check-totals.sh before sending invoices.", AppliesTo: "invoices in invoices/",
		Kind: "howto", Scene: "when checking invoices", Sources: []string{id}, ChangeReason: "the script was added", Body: &body}
	res, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := b.directory(false, "pages")
	md, err := os.ReadFile(filepath.Join(pages, res.Page.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if loc := japanese.FindIndex(md); loc != nil {
		t.Fatalf("an English page contains Japanese: %q", md[max(0, loc[0]-40):min(len(md), loc[1]+80)])
	}
}

// The scene limit (24 characters) was sized for Japanese scenes. The English rendering of a typical scene is longer.
func TestAuditEnglishSceneOfTypicalLengthIsAccepted(t *testing.T) {
	for _, scene := range []string{"when checking invoice totals", "when sending an invoice to a client"} {
		if !validKindScene("howto", scene) {
			t.Errorf("an ordinary English scene is refused (%d characters): %q", len([]rune(scene)), scene)
		}
	}
}

// Pages written before 0.9.2 (with the Japanese labels) stay readable and can be updated after the labels became English.
// Without this, updating shikii would turn every existing page into a changed page (left out of search, refusing saves).
func TestPagesWrittenWithOldLabelsStayValid(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	body := "Exit 1 means a mismatch."
	res, err := b.Ingest(Draft{Title: "Check invoice totals", Lesson: "Run the check before sending invoices.", AppliesTo: "invoices", Kind: "howto", Scene: "when checking invoices",
		Sources: []string{id}, ChangeReason: "the script was added", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	old, err := b.renderPage(*res.Page, legacyLabels) // what 0.9.1 wrote for the same page
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := b.directory(false, "pages")
	for _, f := range []string{filepath.Join(dir, res.Page.ID+".md"), filepath.Join(dir, "revisions", res.Page.ID+"-"+res.Page.Revision+".md")} {
		if err := os.WriteFile(f, old, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := b.Lint()
	if err != nil || len(report.Issues) != 0 {
		t.Fatalf("a page with the old labels is not a changed page: %+v %v", report.Issues, err)
	}
	if live, _ := b.Live(); len(live) != 1 {
		t.Fatalf("a page with the old labels is still found: %+v", live)
	}
	if _, err := b.Classify(res.Page.ID, "rule", "when checking invoices"); err != nil {
		t.Fatalf("a page with the old labels can be updated: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, res.Page.ID+".md"))
	if japanese.Match(md) {
		t.Fatalf("the updated page is written with the English labels: %s", md)
	}
}

// A scene that is too long says so (with its length and the limit), instead of the general "a lesson needs kind and scene" (audit 0.9.2 English, medium 2).
func TestTooLongSceneSaysItIsTooLong(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	d := draft(id, "A rule page")
	d.Scene = "when " + strings.Repeat("checking the invoices ", 4)
	_, err := b.Ingest(d)
	if err == nil || !strings.Contains(err.Error(), "too long") || !strings.Contains(err.Error(), fmt.Sprint(SceneMax)) {
		t.Fatalf("a too-long scene says it is too long and gives the limit: %v", err)
	}
}

// Audit 0.9.2 (translation): tests that tie the English texts to what other code and older stores rely on.

// copyTree copies testdata into a fresh store (the testdata itself is never written).
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// T1: testdata/v091 is a store written by the real v0.9.1 (a page with a correction, a related page, a reason for the change, a body and
// two revisions; built from the v0.9.1 tag with its internal/notebook). main reads its pages as unchanged and can update them.
// TestPagesWrittenWithOldLabelsStayValid renders the old page with legacyLabels itself, so it cannot notice when legacyLabels drifts from 0.9.1.
func TestAuditTranslationStoreWrittenByV091(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", "v091"), root)
	b := Book{Root: root, Scope: "/work/project"}
	const invoice, install = "4961f1812030f9d6", "d9aa97698883d93e"
	report, err := b.Lint()
	if err != nil || len(report.Issues) != 0 {
		t.Fatalf("pages written by v0.9.1 are not changed pages: %+v %v", report.Issues, err)
	}
	live, err := b.Live()
	if err != nil || len(live) != 2 {
		t.Fatalf("both pages written by v0.9.1 are found: %+v %v", live, err)
	}
	for _, p := range live {
		if p.ID == invoice && (p.Correction == nil || len(p.Related) != 1 || p.ChangeReason == "" || p.Body == "") {
			t.Fatalf("the v0.9.1 page keeps its correction, related page, reason and body: %+v", p)
		}
	}
	res, err := b.Classify(invoice, "rule", "when sending invoices")
	if err != nil {
		t.Fatalf("a page written by v0.9.1 can be updated: %v", err)
	}
	dir, _ := b.directory(false, "pages")
	md, _ := os.ReadFile(filepath.Join(dir, invoice+".md"))
	if japanese.Match(md) || !strings.Contains(string(md), "The person's correction: always run the check script") || !strings.Contains(string(md), "## Related") {
		t.Fatalf("the updated page is written with the English labels and keeps its fields: %s", md)
	}
	if res.Page == nil || res.Page.Previous != "25162fe9c6010c36" {
		t.Fatalf("the update follows the v0.9.1 revision: %+v", res.Page)
	}
	if report, err = b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("after the update the store is clean: %+v %v", report.Issues, err)
	}
}

// T3: the guidance for pages without a kind and a scene keeps the command and its arguments (not only the word "classify").
func TestAuditTranslationUnclassifiedHowKeepsTheCommand(t *testing.T) {
	for _, must := range []string{"shikii wiki classify <page ID> <rule|howto|criteria|fact|history> <scene>", "shikii_wiki operation classify with id, kind and scene"} {
		if !strings.Contains(unclassifiedHow, must) {
			t.Errorf("unclassifiedHow lost %q: %s", must, unclassifiedHow)
		}
	}
}

// 0.9.7 audit B-4: a page's source links through any hash-named folder were taken as unchanged. Only the names this store had before
// 0.9.7 are accepted; a link edited to another folder is a changed page.
func TestAuditHandEditedLinkToAnyHashIsUnchanged(t *testing.T) {
	root := t.TempDir()
	b := Book{Root: root, Scope: "/work/p"}
	o, err := b.Capture(Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(Draft{Title: "page", Lesson: "Check.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "wiki", "lessons", b.storeName())
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			p := filepath.Join(dir, e.Name())
			data, _ := os.ReadFile(p)
			edited := strings.ReplaceAll(string(data), "wiki-sources/"+b.storeName()+"/", "wiki-sources/"+strings.Repeat("e", 64)+"/")
			if edited != string(data) {
				n++
				_ = os.WriteFile(p, []byte(edited), 0o600)
			}
		}
	}
	if n != 1 {
		t.Fatalf("precondition: %d", n)
	}
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "changed-page") {
		t.Fatalf("a link edited by hand is a changed page: %+v %v", report.Issues, err)
	}
}

// Behaviour check 0.9.2 (low 8 / low 4): agents without hooks captured the person's rule as a kind:"human" source, cited it as
// correction.source_id and were refused three times by "the correction quote must be copied verbatim ...", which did not say why.
// The check stays as strict as before (a correction quotes the person's next message in a conversation the hook recorded);
// the refusal now says what is needed and what to do without hooks.
func TestCorrectionRefusalSaysWhatIsNeeded(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	conversation := seed(t, b, 1)
	captured, err := b.Capture(Observation{Kind: "human", Title: "the person's rule", Content: "Always write invoices in yen.", Reference: "chat", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		sources []string
		c       store.Correction
		want    []string
	}{
		{"a captured source is not a recorded message", []string{captured.ID}, store.Correction{SourceID: captured.ID, Quote: "Always write invoices in yen.", Action: "write yen"},
			[]string{captured.ID, "conversation recorded by the hook", "without correction", "sources"}},
		{"the quote is not in the person's reply", []string{conversation}, store.Correction{SourceID: conversation, Quote: "words nobody said", Action: "check"},
			[]string{conversation, "verbatim", "reply"}},
		{"the correction source is not cited", []string{conversation}, store.Correction{SourceID: captured.ID, Quote: "Always write invoices in yen.", Action: "write yen"},
			[]string{captured.ID, "sources"}},
	}
	for _, tc := range cases {
		d := draft(tc.sources[0], "invoice rule")
		d.Sources, d.Correction = tc.sources, &tc.c
		_, err := b.Ingest(d)
		if err == nil {
			t.Fatalf("%s: a correction that does not quote a recorded person's message is refused", tc.name)
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal says %q: %v", tc.name, w, err)
			}
		}
	}
	// the right quote from the recorded conversation is still accepted (the check is not weakened, and not closed either)
	d := draft(conversation, "startup rule")
	d.Correction = &store.Correction{SourceID: conversation, Quote: "model startup instructions are missing", Action: "check model startup instructions"}
	if _, err := b.Ingest(d); err != nil {
		t.Fatalf("a verbatim quote from the person's reply is accepted: %v", err)
	}
}

// 0.9.7: the store keeps the names 0.9.6 gave it (the hash of the path it was first opened at), so a store 0.9.6 wrote is read as it is,
// at its own path and, once its name is recorded, after a move; nothing is moved or renamed.
func TestStoreWrittenBy096KeepsItsNames(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", "v091"), root)
	before, _ := os.ReadDir(filepath.Join(root, "wiki", "lessons"))
	at := Book{Root: root, Scope: "/work/project"}
	if live, err := at.Live(); err != nil || len(live) != 2 {
		t.Fatalf("read at its own path: %+v %v", live, err)
	}
	moved := Book{Root: root, Scope: "/moved/somewhere/project"}
	if live, err := moved.Live(); err != nil || len(live) != 2 {
		t.Fatalf("read after a move: %+v %v", live, err)
	}
	if report, err := moved.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("nothing to fix after a move: %+v %v", report.Issues, err)
	}
	after, _ := os.ReadDir(filepath.Join(root, "wiki", "lessons"))
	if len(before) != len(after) || before[0].Name() != after[0].Name() || after[0].Name() != digest([]byte("/work/project")) {
		t.Fatalf("nothing is moved or renamed: %v -> %v", before, after)
	}
}

// Other readers refuse a cursor that carries a byte.
func TestQueryRefusesByteCursor(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i := 0; i < 120; i++ {
		o := partsCapture(t, b, fmt.Sprintf("m%d", i), strings.Repeat("x", 10), int64(i+1))
		d := draft(o.ID, fmt.Sprintf("Lesson %d about memories", i))
		d.Lesson = strings.Repeat(fmt.Sprintf("lesson %d text ", i), 40)
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	v, err := b.QueryPage("", "")
	if err != nil || v.NextCursor == "" {
		t.Fatalf("expected a second page: %v", err)
	}
	if _, err := b.QueryPage("", auditForge(t, v.NextCursor, func(c *continuation) { c.Byte = 5 })); err == nil {
		t.Fatal("QueryPage accepted a cursor with a byte offset")
	}
}

// A store written by the real v0.9.1 (testdata/v091, under /work/project) is read as it is at its own path and after a move, and can be
// updated. A store moved before 0.9.7 ever opened it is reconnected first (the hook or --init), which records its name from the old path.
func TestStoreWrittenByV091NeedsNoFixingAfterAMove(t *testing.T) {
	for _, scope := range []string{"/work/project", "/moved/somewhere/project"} {
		root := t.TempDir()
		copyTree(t, filepath.Join("testdata", "v091"), root)
		store.RecordStoreNameFrom(root, "/work/project")
		b := Book{Root: root, Scope: scope}
		if live, err := b.Live(); err != nil || len(live) != 2 {
			t.Fatalf("%s: both pages written by v0.9.1 are found: %+v %v", scope, live, err)
		}
		if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
			t.Fatalf("%s: pages written by v0.9.1 are not changed pages: %+v %v", scope, report.Issues, err)
		}
		if _, err := b.Classify("4961f1812030f9d6", "rule", "when sending invoices"); err != nil {
			t.Fatalf("%s: a page written by v0.9.1 can be updated: %v", scope, err)
		}
		if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
			t.Fatalf("%s: after the update the store is clean: %+v %v", scope, report.Issues, err)
		}
	}
}

// 0.9.2 re-audit, test hole: a page made from a long source stays live (its source can be read by ingest, query and lint),
// and capturing the same long source again is not refused.
func TestLessonFromLongSourceStaysLive(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	long := Observation{Kind: "agent", Title: "long memory", Content: strings.Repeat("A long memory file, kept verbatim. ", 6000), Reference: "memory/long.md", Version: "v", ObservedAt: time.Unix(1, 0)}
	o, err := b.Capture(long)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(draft(o.ID, "Lesson from a long memory")); err != nil {
		t.Fatalf("a page can be made from a long source: %v", err)
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 1 || len(v.Issues) != 0 {
		t.Fatalf("a page citing a long source is listed without issues: %v lessons=%d issues=%+v", err, len(v.Lessons), v.Issues)
	}
	if again, err := b.Capture(long); err != nil || again.ID != o.ID {
		t.Fatalf("capturing a sorted long source again is not refused: %v", err)
	}
}

// Audit 0.9.2 (behavior), low 3: a page written by 0.9.1 (with the fixed words of before 0.9.2) and saved again by 0.9.2 with the same content
// (or classified again with the same kind and scene) was refused with "an immutable wiki source or revision has changed": the content is the same,
// so the revision is the same, but the revision file's bytes use the old words. A revision file that is the same page (matchesPage) is accepted.
func TestAuditResaveUnchangedLegacyPage(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	body := "Exit 1 means a mismatch."
	d := Draft{Title: "Check invoice totals", Lesson: "Run the check before sending invoices.", AppliesTo: "invoices", Kind: "howto", Scene: "when checking invoices",
		Sources: []string{id}, ChangeReason: "the script was added", Body: &body}
	res, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := b.renderPage(*res.Page, legacyLabels)
	dir, _ := b.directory(false, "pages")
	revisionFile := filepath.Join(dir, "revisions", res.Page.ID+"-"+res.Page.Revision+".md")
	for _, f := range []string{filepath.Join(dir, res.Page.ID+".md"), revisionFile} {
		if err := os.WriteFile(f, old, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d.ID = res.Page.ID
	if _, err := b.Ingest(d); err != nil {
		t.Errorf("saving the same content again: %v", err)
	}
	if _, err := b.Classify(res.Page.ID, "howto", "when checking invoices"); err != nil {
		t.Errorf("classifying again with the same kind and scene: %v", err)
	}
	if kept, _ := os.ReadFile(revisionFile); !bytes.Equal(kept, old) {
		t.Error("the revision file written by the earlier version is not rewritten")
	}
	// a revision file that is not the same page is still refused
	if err := os.WriteFile(revisionFile, append(append([]byte{}, old...), []byte("tampered\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(d); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Errorf("a changed revision file is refused: %v", err)
	}
}

// Behaviour check 0.9.2 re-check (claude-en): a wiki-only sort named two conversations and, by mistake, the turn id
// 911c2872465d2337. The whole item was refused with "wiki source does not belong to this project's observations",
// which does not say which id, so the agent never resent the two good ids and both conversations stayed "wiki unsorted".
func TestWikiSourceErrorNamesTheUnknownID(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	good := seed(t, b, 1)
	bad := "911c2872465d2337"
	_, err := b.Ingest(Draft{Sources: []string{good, bad}})
	if err == nil {
		t.Fatal("an unknown source id was accepted")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Fatalf("the error does not name the unknown id %s: %v", bad, err)
	}
}

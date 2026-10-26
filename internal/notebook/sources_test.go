package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestCapturedSourcesStayScopedImmutableAndBounded(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/one"}
	o := Observation{Kind: "test", Title: "Smoke check", Content: "PASS", Reference: "go test ./...", Version: "fixture-v1", ObservedAt: time.Unix(1, 0)}
	r, err := b.Capture(o)
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.Capture(o)
	if err != nil || again.ID != r.ID {
		t.Fatal("resending the same observation does not add another", err)
	}
	if _, err := (Book{Root: b.Root, Scope: "/two"}).ReadSources([]string{r.ID}); err == nil {
		t.Fatal("does not read sources of another project")
	}
	for _, bad := range []Observation{
		{Kind: "confirmed", Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
		{Kind: o.Kind, Title: o.Title, Content: strings.Repeat("x", store.MaxLineBytes), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
		{Kind: o.Kind, Title: o.Title, Content: strings.Repeat("\x01", store.MaxLineBytes/6+1), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt}, // escaped as \u0001: the JSON goes over the line limit
	} {
		if _, err := b.Capture(bad); err == nil {
			t.Fatal("does not save unknown kinds or a record over the store's line limit")
		}
	}
	dir, _ := b.directory(false, "sources")
	path := filepath.Join(dir, r.ID+".json")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "PASS", "FAIL", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadSources([]string{r.ID}); err == nil {
		t.Fatal("detects changes made before quoting by the content ID too")
	}
	if _, err := b.Ingest(draft(r.ID, "Changed source")); err == nil {
		t.Fatal("a modified source record cannot become a lesson")
	}
}

func TestSourceOriginalsSurviveRecordRemovalAndDetectChanges(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	st := store.Store{Dir: b.Root}
	request, summary := "README installation", strings.Repeat("A", 1500)
	rec := store.Record{TraceID: "0000000000000012", SpecID: "done-as-asked", Inputs: map[string]string{"request": request, "summary": summary}, At: time.Now()}
	rec.InputsHash = store.HashInputs(rec.Inputs)
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	full := summary + strings.Repeat("B", 18000) + "END-OF-ORIGINAL"
	if err := st.SaveRecordText(rec, map[string]string{"summary": full}); err != nil {
		t.Fatal(err)
	}
	f := store.Feedback{ID: "0000000000000013", Request: request, Summary: summary, Reply: "Include the startup check", Targets: []store.ReviewTarget{{Spec: rec.SpecID, TraceID: rec.TraceID}}}
	if err := st.AppendFeedback(b.Scope, f); err != nil {
		t.Fatal(err)
	}
	page, err := b.Ingest(draft(f.ID, "Installation observations"))
	if err != nil {
		t.Fatal(err)
	}
	if page.Page.Sources[0].TextHash == "" {
		t.Fatal("includes the hash of the saved original in the source")
	}
	if err := os.RemoveAll(filepath.Join(b.Root, "records", "text")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(b.Root, "records", rec.SpecID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	material, err := b.ReadSources([]string{f.ID})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(material)
	if !strings.Contains(string(data), "END-OF-ORIGINAL") {
		t.Fatal("the wiki original stays after the decision log is deleted")
	}
	dir, _ := b.directory(false, "sources")
	if err := os.WriteFile(filepath.Join(dir, f.ID+".text.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadSources([]string{f.ID}); err == nil {
		t.Fatal("detects changes to long text too")
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 0 || !hasIssue(v.Issues, "changed-source-text") {
		t.Fatal("does not deliver a lesson whose long text was modified", err)
	}
}

// 0.9.2: after moving or renaming the folder, a wiki store (named by sha256(path)) left under the old name silently emptied the wiki.
// lint reports a store under the old name as other-path-store. Even after moving the store to the new name, source records captured under the previous path (the path is in the id)
// cannot be checked by their id under the new path; since 0.9.5 the page's citation checks them, so lint has nothing more to report.
func TestLintFindsWikiLeftUnderOldPath(t *testing.T) {
	root := t.TempDir()
	old := Book{Root: root, Scope: "/work/old"}
	conv := seed(t, old, 1)
	if _, err := old.Ingest(draft(conv, "page citing a conversation")); err != nil {
		t.Fatal(err)
	}
	o, err := old.Capture(Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Ingest(Draft{Title: "page citing a source record", Lesson: "Check before writing.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
	moved := Book{Root: root, Scope: "/work/new"} // after renaming the folder
	h := digest([]byte(old.Scope))
	stores, err := moved.OtherPathStores()
	if err != nil || strings.Join(stores, ",") != "records/wiki-sources/"+h+",wiki/lessons/"+h {
		t.Fatalf("returns the 2 stores under the old name: %v %v", stores, err)
	}
	if again, _ := old.OtherPathStores(); len(again) != 0 {
		t.Fatalf("the store for the current path is not counted: %v", again)
	}
	report, err := moved.Lint()
	if err != nil {
		t.Fatal(err)
	}
	other := 0
	for _, issue := range report.Issues {
		if issue.Kind == "other-path-store" {
			other++
		}
	}
	if other != 2 {
		t.Fatalf("reports a store under the old name even when the new path has no wiki yet: %+v", report.Issues)
	}
	// Copy the source store to the new name (the old one is not deleted). A store that has been fully copied is no longer reported
	n := digest([]byte(moved.Scope))
	if err := os.MkdirAll(filepath.Join(root, "records/wiki-sources", n), 0o700); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "records/wiki-sources", h))
	entries = slices.DeleteFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".") }) // a lock is made per store
	for i, e := range entries {
		data, _ := os.ReadFile(filepath.Join(root, "records/wiki-sources", h, e.Name()))
		if i == 0 {
			continue // while one file is left uncopied, it is still reported
		}
		_ = os.WriteFile(filepath.Join(root, "records/wiki-sources", n, e.Name()), data, 0o600)
	}
	if stores, _ := moved.OtherPathStores(); len(stores) != 2 {
		t.Fatalf("reports while a file is left uncopied: %v", stores)
	}
	data, _ := os.ReadFile(filepath.Join(root, "records/wiki-sources", h, entries[0].Name()))
	_ = os.WriteFile(filepath.Join(root, "records/wiki-sources", n, entries[0].Name()), data, 0o600)
	if stores, _ := moved.OtherPathStores(); strings.Join(stores, ",") != "wiki/lessons/"+h {
		t.Fatalf("a fully copied store is not counted: %v", stores)
	}
	// Move the page store to the new name and rewrite the names in the page's source links too (as the how-to page says)
	if err := os.Rename(filepath.Join(root, "wiki/lessons", h), filepath.Join(root, "wiki/lessons", n)); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(filepath.Join(root, "wiki/lessons", n), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() && strings.HasSuffix(p, ".md") {
			data, _ := os.ReadFile(p)
			_ = os.WriteFile(p, []byte(strings.ReplaceAll(string(data), h, n)), 0o600)
		}
		return nil
	})
	report, err = moved.Lint()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 0 { // 0.9.5: a cited source captured under the previous path is checked by its citation, so nothing is left to capture again
		t.Fatalf("nothing is reported after the move: %+v", report.Issues)
	}
	if _, err := moved.ReadSources([]string{o.ID}); err != nil {
		t.Fatalf("the page's source captured under the previous path is still readable: %v", err)
	}
}

// moveWikiStores does what the how-to page says after renaming the folder: copy both stores to the new path's name (the old ones are kept) and
// rewrite the store's name in the pages' source links.
func moveWikiStores(t *testing.T, root, from, to string) {
	t.Helper()
	h, n := digest([]byte(from)), digest([]byte(to))
	for _, area := range []string{"records/wiki-sources", "wiki/lessons"} {
		_ = filepath.Walk(filepath.Join(root, area, h), func(p string, info os.FileInfo, err error) error {
			if err != nil || strings.HasPrefix(info.Name(), ".lock") {
				return err
			}
			rel, _ := filepath.Rel(filepath.Join(root, area, h), p)
			target := filepath.Join(root, area, n, rel)
			if info.IsDir() {
				return os.MkdirAll(target, 0o700)
			}
			data, _ := os.ReadFile(p)
			if strings.HasSuffix(p, ".md") {
				data = []byte(strings.ReplaceAll(string(data), h, n))
			}
			return os.WriteFile(target, data, 0o600)
		})
	}
}

// 0.9.5: a source record's id holds the path it was captured under, so after renaming the folder every source captured before could not be read
// (wiki query) or cited again (ingest: "the hash of the saved source record does not match"), and each update of a page needed the same evidence
// captured again. The recaptured copy then sat beside the old id on the page, because an update keeps every earlier source.
// A record a page already cites is checked by the hash in that citation instead, and a recaptured copy (same content, the current path's id)
// takes the old id's place.
func TestSourcesCapturedUnderThePreviousPathStayUsable(t *testing.T) {
	root := t.TempDir()
	old := Book{Root: root, Scope: "/work/old"}
	obs := Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}
	o, err := old.Capture(obs)
	if err != nil {
		t.Fatal(err)
	}
	stray, err := old.Capture(Observation{Kind: "test", Title: "never cited", Content: "x", Reference: "r", Version: "v1", ObservedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	d := Draft{Title: "page citing a source record", Lesson: "Check before writing.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}
	cited, err := old.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	moveWikiStores(t, root, old.Scope, "/work/new")
	moved := Book{Root: root, Scope: "/work/new"}

	got, err := moved.ReadSources([]string{o.ID})
	if err != nil || len(got["observations"].([]Observation)) != 1 {
		t.Fatalf("a cited source captured under the previous path is still readable: %v %v", got, err)
	}
	d.ID, d.Lesson = cited.Page.ID, "Check before writing, twice."
	if _, err := moved.Ingest(d); err != nil {
		t.Fatalf("a cited source captured under the previous path can be cited again when updating its page: %v", err)
	}
	if _, err := moved.Ingest(Draft{Title: "other", Lesson: "l", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{stray.ID}}); err == nil {
		t.Fatal("a source captured under the previous path that no page cites still cannot be checked, so it is refused")
	}

	again, err := moved.Capture(obs)
	if err != nil || again.ID == o.ID {
		t.Fatalf("capturing the same evidence under the new path gives a new id: %v %v", again.ID, err)
	}
	d.Lesson, d.Sources = "Check before writing, three times.", []string{again.ID}
	updated, err := moved.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Page.Sources) != 1 || updated.Page.Sources[0].ID != again.ID {
		t.Fatalf("the recaptured copy takes the old id's place instead of sitting beside it: %+v", updated.Page.Sources)
	}

	report, err := moved.Lint()
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range report.Issues {
		if issue.Kind != "other-path-store" || !strings.HasSuffix(issue.Store, digest([]byte(old.Scope))) {
			t.Errorf("after the move only the old stores are reported, never a source to capture again: %+v", issue)
		}
	}
}

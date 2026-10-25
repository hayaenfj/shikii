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
// cannot be checked under the new path and cannot be used as sources when editing that page. lint reports them as source-from-other-path (conversation sources are not reported).
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
	cited, err := old.Ingest(Draft{Title: "page citing a source record", Lesson: "Check before writing.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}})
	if err != nil {
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
	var fromOther []Issue
	for _, issue := range report.Issues {
		if issue.Kind == "source-from-other-path" {
			fromOther = append(fromOther, issue)
		} else {
			t.Errorf("the only finding left after the move is source records captured under the previous path: %+v", issue)
		}
	}
	if len(fromOther) != 1 || fromOther[0].Page != cited.Page.ID || fromOther[0].Source != o.ID {
		t.Fatalf("reports source records captured under the previous path: %+v", report.Issues)
	}
}

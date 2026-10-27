package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if _, err := (Book{Root: t.TempDir(), Scope: "/two"}).ReadSources([]string{r.ID}); err == nil {
		t.Fatal("does not read sources of another project (each project has its own store)")
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

// 0.9.5 check: updating a page by id without its lesson was refused with "reviewing a source without a lesson accepts only source ids",
// which did not tell the agent what to send. It now says that an update sends the lesson, title and applies_to again.
func TestUpdateWithoutLessonSaysWhatToSend(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	r, err := b.Ingest(draft(seed(t, b, 1), "a page"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Ingest(Draft{ID: r.Page.ID, Sources: []string{seed(t, b, 2)}, ChangeReason: "check"})
	if err == nil || !strings.Contains(err.Error(), "title") || !strings.Contains(err.Error(), "applies_to") {
		t.Fatalf("the refusal says to send the lesson, title and applies_to: %v", err)
	}
}

// 0.9.5 check: long-lived pages grow by a section per update (a real page reached 15,838 of 16,384 bytes of body). Going over was refused with
// "wiki lesson exceeds the text limits", which named neither the field nor what to do. The refusal now names the field, its size and the limit,
// and says to move older parts to a new page linked with related.
func TestTextLimitRefusalNamesTheField(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "a long page")
	body := strings.Repeat("x", 16<<10+1)
	d.Body = &body
	_, err := b.Ingest(d)
	if err == nil || !strings.Contains(err.Error(), "body") || !strings.Contains(err.Error(), "16385") || !strings.Contains(err.Error(), "related") {
		t.Fatalf("the refusal names the field, its size and how to split: %v", err)
	}
}

// legacySource saves a waiting source record with the id versions before 0.9.7 gave it: the hash of the path it was captured under and its content.
func legacySource(t *testing.T, b Book, capturedAt string, o Observation) string {
	t.Helper()
	dir, err := b.directory(true, "sources")
	if err != nil {
		t.Fatal(err)
	}
	o.ObservedAt = o.ObservedAt.UTC()
	data, _ := json.Marshal(o)
	o.ID = digest(append([]byte(capturedAt+"\n"), data...))[:16]
	data, _ = json.Marshal(o)
	if err := os.WriteFile(filepath.Join(dir, o.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	ids, _ := capturedIDs(dir)
	list, _ := json.Marshal(append(ids, o.ID))
	if err := os.WriteFile(filepath.Join(dir, ".captured.json"), list, 0o600); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// A waiting source captured before 0.9.7 under a path the store does not know cannot be checked: it is not offered for sorting and lint
// names it. Once the path is remembered (a move found by the hook remembers the previous path), it checks and is offered again.
func TestOldSourceIDsCheckWithTheRememberedPath(t *testing.T) {
	root := t.TempDir()
	b := Book{Root: root, Scope: "/work/new"}
	id := legacySource(t, b, "/work/old", Observation{Kind: "test", Title: "waiting", Content: "x", Reference: "r", Version: "v1", ObservedAt: time.Unix(3, 0)})
	if v, err := b.Query(""); err != nil || len(v.Observations) != 0 {
		t.Fatalf("a source that cannot be checked is not offered, and the wiki stays readable: %+v %v", v.Observations, err)
	}
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "source-from-other-path") {
		t.Fatalf("lint names it: %+v %v", report.Issues, err)
	}
	if err := RememberPath(root, "/work/old"); err != nil {
		t.Fatal(err)
	}
	if v, err := b.Query(""); err != nil || len(v.Observations) != 1 || v.Observations[0].ID != id {
		t.Fatalf("with the previous path remembered it checks and is offered: %+v %v", v.Observations, err)
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("and nothing is left to fix: %+v %v", report.Issues, err)
	}
}

// A store under another name beside this one (written at another path by an older shikii, or copied in by hand) is named until every
// file in it is in this store.
func TestStoreUnderAnotherNameIsNamedUntilCopied(t *testing.T) {
	root := t.TempDir()
	b := Book{Root: root, Scope: "/work/p"}
	if _, err := b.Capture(Observation{Kind: "test", Title: "here", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "records", "wiki-sources", digest([]byte("/work/elsewhere")))
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "0000000000000001.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stores, err := b.OtherPathStores(); err != nil || len(stores) != 1 || !strings.HasSuffix(stores[0], filepath.Base(other)) {
		t.Fatalf("the other store is named: %v %v", stores, err)
	}
	own := filepath.Join(root, "records", "wiki-sources", store.StoreName(root, b.Scope))
	if err := os.WriteFile(filepath.Join(own, "0000000000000001.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stores, err := b.OtherPathStores(); err != nil || len(stores) != 0 {
		t.Fatalf("once copied it is not named: %v %v", stores, err)
	}
}

// 0.9.7: the wiki and its sources live under fixed names inside .shikii, not under a hash of the project's path. Before, moving or renaming
// the project folder emptied the wiki until a person followed eleven manual steps, and each version's audit found another place the steps
// missed. A Book opened at the new path sees everything the old path wrote, with no files touched in between.
func TestWikiNeedsNoFixingAfterTheFolderMoves(t *testing.T) {
	root := t.TempDir()
	old := Book{Root: root, Scope: "/work/old"}
	obs := Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}
	o, err := old.Capture(obs)
	if err != nil {
		t.Fatal(err)
	}
	stray, err := old.Capture(Observation{Kind: "test", Title: "not cited yet", Content: "x", Reference: "r", Version: "v1", ObservedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	d := Draft{Title: "page citing a source", Lesson: "Check before writing.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}
	page, err := old.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}

	moved := Book{Root: root, Scope: "/work/new"}
	if live, err := moved.Live(); err != nil || len(live) != 1 {
		t.Fatalf("the page written at the old path is there after the move: %+v %v", live, err)
	}
	if report, err := moved.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("nothing needs fixing after the move: %+v %v", report.Issues, err)
	}
	if pending, err := moved.PendingObservations(); err != nil || len(pending) != 1 || pending[0].ID != stray.ID {
		t.Fatalf("a source not cited yet is still offered for sorting: %+v %v", pending, err)
	}
	d.ID, d.Lesson = page.Page.ID, "Check before writing, twice."
	if _, err := moved.Ingest(d); err != nil {
		t.Fatalf("the page can be updated with its source: %v", err)
	}
	if _, err := moved.Ingest(Draft{Title: "other", Lesson: "l", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{stray.ID}}); err != nil {
		t.Fatalf("a source captured before the move can be cited: %v", err)
	}
	if again, err := moved.Capture(obs); err != nil || again.ID != o.ID {
		t.Fatalf("the same evidence has the same id wherever the folder is: %s %s %v", again.ID, o.ID, err)
	}
}

// Re-audit 0.9.2 (test hole): removing the size check of sourceRecordDigest survived every test, because an oversized record
// also has a different hash. The streamed hash refuses a record over the store's line limit and takes one at the limit.
func TestSourceRecordDigestRefusesOverTheLineLimit(t *testing.T) {
	dir := t.TempDir()
	at, over := filepath.Join(dir, "at.json"), filepath.Join(dir, "over.json")
	if err := os.WriteFile(at, []byte(strings.Repeat("a", store.MaxLineBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(over, []byte(strings.Repeat("a", store.MaxLineBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if hash, err := sourceRecordDigest(at); err != nil || hash != digest([]byte(strings.Repeat("a", store.MaxLineBytes))) {
		t.Fatalf("a record at the line limit is hashed whole: %v", err)
	}
	if _, err := sourceRecordDigest(over); err == nil || !strings.Contains(err.Error(), "line limit") {
		t.Fatalf("a record over the line limit is refused: %v", err)
	}
}

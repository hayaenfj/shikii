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

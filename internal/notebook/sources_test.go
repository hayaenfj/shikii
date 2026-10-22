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
		t.Fatal("同じ観察の再送は増やさない", err)
	}
	if _, err := (Book{Root: b.Root, Scope: "/two"}).ReadSources([]string{r.ID}); err == nil {
		t.Fatal("別プロジェクトの出典を読まない")
	}
	for _, bad := range []Observation{
		{Kind: "confirmed", Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
		{Kind: o.Kind, Title: o.Title, Content: strings.Repeat("x", (32<<10)+1), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
		{Kind: o.Kind, Title: o.Title, Content: strings.Repeat("\x01", 32<<10), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
	} {
		if _, err := b.Capture(bad); err == nil {
			t.Fatal("不明な種類や過大な本文・JSONを保存しない")
		}
	}
	dir, _ := b.directory(false, "sources")
	path := filepath.Join(dir, r.ID+".json")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "PASS", "FAIL", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadSources([]string{r.ID}); err == nil {
		t.Fatal("引用前の改変も内容IDで検出する")
	}
	if _, err := b.Ingest(draft(r.ID, "Changed source")); err == nil {
		t.Fatal("改変した原資料を教訓化できない")
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
		t.Fatal("保存した原文のハッシュを出典に含める")
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
		t.Fatal("判定の記録を削除してもwikiの原文は残る")
	}
	dir, _ := b.directory(false, "sources")
	if err := os.WriteFile(filepath.Join(dir, f.ID+".text.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadSources([]string{f.ID}); err == nil {
		t.Fatal("長文の改変も検出する")
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 0 || !hasIssue(v.Issues, "changed-source-text") {
		t.Fatal("長文の改変した教訓を配送しない", err)
	}
}

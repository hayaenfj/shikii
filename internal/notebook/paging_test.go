package notebook

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWikiSearchAndListingContinueBeyond128Articles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var last Page
	for i := range 140 {
		d := draft(source, fmt.Sprintf("Knowledge %03d", i))
		d.AppliesTo = strings.Repeat("Knowledge scope ", 55)
		body := strings.Repeat("Detailed evidence. ", 100)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatalf("does not refuse page %d by count alone: %v", i+1, err)
		}
		last = *r.Page
	}
	for _, query := range []string{"Knowledge", ""} {
		seen, cursor := map[string]bool{}, ""
		var firstCursor string
		for calls := 0; ; calls++ {
			if calls > 140 {
				t.Fatal("the cursor does not advance")
			}
			v, err := b.QueryPage(query, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if query == "" {
				for _, p := range v.Lessons {
					if seen[p.ID] {
						t.Fatal("duplicate in the list")
					}
					seen[p.ID] = true
				}
			} else {
				if v.Search == nil || v.Search.Total != 140 {
					t.Fatal("searches every page")
				}
				for _, c := range v.Search.Candidates {
					if seen[c.ID] {
						t.Fatal("duplicate candidate")
					}
					seen[c.ID] = true
				}
			}
			if calls == 0 {
				firstCursor = v.NextCursor
			}
			if v.NextCursor == "" {
				break
			}
			cursor = v.NextCursor
		}
		if len(seen) != 140 || firstCursor == "" {
			t.Fatalf("reaches the end without truncating: %d cursor=%q", len(seen), firstCursor)
		}
		if _, err := b.QueryPage(query+" changed", firstCursor); err == nil {
			t.Fatal("refuses a cursor whose search terms changed")
		}
		if _, err := (Book{Root: b.Root, Scope: "/another"}).QueryPage(query, firstCursor); err == nil {
			t.Fatal("refuses a cursor from another project")
		}
	}
	v, err := b.Query("lesson:" + last.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != last.ID {
		t.Fatal("the last page can also be read directly", err)
	}
	before, _ := b.QueryPage("Knowledge", "")
	d := draft(source, last.Title)
	d.ID, d.Lesson = last.ID, "Revised knowledge"
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if _, err := b.QueryPage("Knowledge", before.NextCursor); err == nil {
		t.Fatal("refuses a cursor after the page revision changed")
	}
	if _, err := b.Lint(); err != nil {
		t.Fatal("a large list can also be checked", err)
	}
}

func TestWikiSourceContinuationUsesBytesInsteadOfCounts(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := range 18 {
		o, err := b.Capture(Observation{Kind: "test", Title: fmt.Sprint("Evidence ", i), Content: strings.Repeat("x", 9000), Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	seen, cursor := map[string]bool{}, ""
	var first string
	for calls := 0; ; calls++ {
		if calls > len(ids) {
			t.Fatal("the source cursor does not advance")
		}
		got, err := b.ReadSourcesPage(ids, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range got["observations"].([]Observation) {
			if seen[o.ID] || len(o.Content) != 9000 {
				t.Fatal("missing or duplicate original text")
			}
			seen[o.ID] = true
		}
		cursor, _ = got["next_cursor"].(string)
		if calls == 0 {
			first = cursor
		}
		if cursor == "" {
			break
		}
	}
	if len(seen) != 18 || first == "" {
		t.Fatal("does not cut source records by count", len(seen))
	}
	if _, err := b.ReadSourcesPage(ids[:17], first); err == nil {
		t.Fatal("refuses a cursor whose target changed")
	}
	if _, err := b.ReadSourcesPage(ids, "invalid"); err == nil {
		t.Fatal("refuses a broken cursor")
	}
	if _, err := (Book{Root: b.Root, Scope: "/other"}).ReadSourcesPage(ids, first); err == nil {
		t.Fatal("keeps the project boundary of source records")
	}
}

func TestWikiCitationsLinksAndCaptureQueueHaveNoCountCeiling(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := range 140 {
		o, err := b.Capture(Observation{Kind: "test", Title: fmt.Sprint("Evidence ", i), Content: "Checked", Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatalf("does not refuse ingestion with %d unorganized records: %v", i+1, err)
		}
		ids = append(ids, o.ID)
	}
	d := draft(ids[0], "Combined knowledge")
	d.Sources = ids[:18]
	r, err := b.Ingest(d)
	if err != nil || len(r.Page.Sources) != 18 {
		t.Fatal("can cite 18 source records", err)
	}
	d.ID = r.Page.ID
	d.Sources = ids[18:36]
	r, err = b.Ingest(d)
	if err != nil || len(r.Page.Sources) != 36 {
		t.Fatal("keeps the quotes from before the update", err)
	}
	links := []string{}
	for i := range 12 {
		r, err := b.Ingest(draft(ids[i], fmt.Sprint("Topic ", i)))
		if err != nil {
			t.Fatal(err)
		}
		links = append(links, r.Page.ID)
	}
	d.Related = links
	d.Replaces = links[:5]
	if _, err := b.Ingest(d); err != nil {
		t.Fatal("keeps 12 related and 5 replaced pages", err)
	}
	dir, _ := b.directory(false, "pages")
	checkpoints := map[string]maintenanceCheckpoint{}
	for i := range 150 {
		checkpoints[fmt.Sprintf("%016x", i)] = maintenanceCheckpoint{MaintenanceReview: MaintenanceReview{Reason: strings.Repeat("Checked ", 100)}}
	}
	data, _ := json.Marshal(checkpoints)
	if err := writeFile(dir+"/.maintenance.json", data); err != nil {
		t.Fatal(err)
	}
	if got, err := readCheckpoints(dir); err != nil || len(got) != 150 {
		t.Fatal("does not lose the review log to counts or small file limits", err)
	}
}

// audit 0.9.0 M4: the guidance about pages without kind and scene is only in the first reply, not in continuation replies (do not hand over the same 5 pages again and again).
func TestUnclassifiedOnlyOnTheFirstPage(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var legacy string
	for i := range 60 {
		d := draft(source, fmt.Sprintf("Knowledge %03d", i))
		d.AppliesTo = strings.Repeat("Knowledge scope ", 55)
		body := strings.Repeat("Detailed evidence. ", 100)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		legacy = r.Page.ID
	}
	if err := b.RewriteWithoutKindScene(legacy); err != nil {
		t.Fatal(err)
	}
	first, err := b.QueryPage("", "")
	if err != nil || first.NextCursor == "" || first.UnclassifiedCount != 1 {
		t.Fatalf("the first reply has the guidance and a continuation: count=%d cursor=%q %v", first.UnclassifiedCount, first.NextCursor, err)
	}
	next, err := b.QueryPage("", first.NextCursor)
	if err != nil || next.UnclassifiedCount != 0 || len(next.Unclassified) != 0 || next.UnclassifiedHow != "" {
		t.Fatalf("not included in continuation replies: %+v %v", next.Remaining, err)
	}
}

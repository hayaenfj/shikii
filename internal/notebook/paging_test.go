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
			t.Fatalf("記事%dを件数だけで拒否しない: %v", i+1, err)
		}
		last = *r.Page
	}
	for _, query := range []string{"Knowledge", ""} {
		seen, cursor := map[string]bool{}, ""
		var firstCursor string
		for calls := 0; ; calls++ {
			if calls > 140 {
				t.Fatal("続きが前進しない")
			}
			v, err := b.QueryPage(query, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if query == "" {
				for _, p := range v.Lessons {
					if seen[p.ID] {
						t.Fatal("一覧の重複")
					}
					seen[p.ID] = true
				}
			} else {
				if v.Search == nil || v.Search.Total != 140 {
					t.Fatal("全記事を検索対象にする")
				}
				for _, c := range v.Search.Candidates {
					if seen[c.ID] {
						t.Fatal("候補の重複")
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
			t.Fatalf("切捨てず最後まで到達: %d cursor=%q", len(seen), firstCursor)
		}
		if _, err := b.QueryPage(query+" changed", firstCursor); err == nil {
			t.Fatal("検索条件を変えた続きを拒否する")
		}
		if _, err := (Book{Root: b.Root, Scope: "/another"}).QueryPage(query, firstCursor); err == nil {
			t.Fatal("別プロジェクトの続きを拒否する")
		}
	}
	v, err := b.Query("lesson:" + last.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != last.ID {
		t.Fatal("末尾の記事も直接読める", err)
	}
	before, _ := b.QueryPage("Knowledge", "")
	d := draft(source, last.Title)
	d.ID, d.Lesson = last.ID, "Revised knowledge"
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if _, err := b.QueryPage("Knowledge", before.NextCursor); err == nil {
		t.Fatal("記事の版が変わった続きを拒否する")
	}
	if _, err := b.Lint(); err != nil {
		t.Fatal("大きい一覧も点検できる", err)
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
			t.Fatal("原資料の続きが前進しない")
		}
		got, err := b.ReadSourcesPage(ids, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range got["observations"].([]Observation) {
			if seen[o.ID] || len(o.Content) != 9000 {
				t.Fatal("原文の欠落・重複")
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
		t.Fatal("原資料を件数で切らない", len(seen))
	}
	if _, err := b.ReadSourcesPage(ids[:17], first); err == nil {
		t.Fatal("対象を変えた続きを拒否する")
	}
	if _, err := b.ReadSourcesPage(ids, "invalid"); err == nil {
		t.Fatal("壊れた続きを拒否する")
	}
	if _, err := (Book{Root: b.Root, Scope: "/other"}).ReadSourcesPage(ids, first); err == nil {
		t.Fatal("原資料のプロジェクト境界を保つ")
	}
}

func TestWikiCitationsLinksAndCaptureQueueHaveNoCountCeiling(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := range 140 {
		o, err := b.Capture(Observation{Kind: "test", Title: fmt.Sprint("Evidence ", i), Content: "Checked", Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatalf("未整理%d件で取り込みを拒否しない: %v", i+1, err)
		}
		ids = append(ids, o.ID)
	}
	d := draft(ids[0], "Combined knowledge")
	d.Sources = ids[:18]
	r, err := b.Ingest(d)
	if err != nil || len(r.Page.Sources) != 18 {
		t.Fatal("18件の原資料を引用できる", err)
	}
	d.ID = r.Page.ID
	d.Sources = ids[18:36]
	r, err = b.Ingest(d)
	if err != nil || len(r.Page.Sources) != 36 {
		t.Fatal("更新前の引用も保持する", err)
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
		t.Fatal("関連12件と置き換え5件を保持する", err)
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
		t.Fatal("見直しの記録も件数や小さいファイル上限で失わない", err)
	}
}

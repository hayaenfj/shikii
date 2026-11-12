package notebook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryUpgradeRegressionUpgradeReplacement(t *testing.T) {
	for _, broken := range []bool{false, true} {
		name := "valid-original"
		if broken {
			name = "unavailable-original"
		}
		t.Run(name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/synthetic-project"}
			first, err := b.Ingest(draft(seed(t, b, 1), "Synthetic original preference"))
			if err != nil {
				t.Fatal(err)
			}
			u, err := b.MemoryUpgrade()
			if err != nil || u.Total != 1 || u.Reviewed != 0 {
				t.Fatalf("\u6e96\u5099\u5931\u6557: %+v %v", u, err)
			}
			if broken {
				dir, e := b.directory(false, "sources")
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Remove(filepath.Join(dir, first.Page.Sources[0].ID+".json")); e != nil {
					t.Fatal(e)
				}
				u, e = b.MemoryUpgrade()
				if e != nil || u.Status != "blocked" || len(u.Blocked) != 1 {
					t.Fatalf("\u4e0d\u8db3\u72b6\u614b\u306e\u5bfe\u7167\u5931\u6557: %+v %v", u, e)
				}
				t.Logf("\u7f6e\u63db\u524d: status=%s total=%d reviewed=%d blocked=%d", u.Status, u.Total, u.Reviewed, len(u.Blocked))
			}
			replacement := draft(seed(t, b, 2), "Synthetic replacement procedure")
			replacement.Replaces = []string{first.Page.ID}
			newPage, err := b.Ingest(replacement)
			if err != nil {
				t.Fatalf("\u7f6e\u63db\u306e\u6e96\u5099\u5931\u6557: %v", err)
			}
			u, err = b.MemoryUpgrade()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("\u7f6e\u63db\u5f8c: status=%s total=%d reviewed=%d tasks=%d blocked=%d new_sources=%d original_cited=%v", u.Status, u.Total, u.Reviewed, len(u.Tasks), len(u.Blocked), len(newPage.Page.Sources), newPage.Page.Sources[0].ID == first.Page.Sources[0].ID)
			if u.Status == "complete" {
				t.Errorf("\u672a\u78ba\u8a8d\u306e\u5225\u8a18\u4e8b\u3078\u306e\u7f6e\u63db\u3060\u3051\u3067\u66f4\u65b0\u78ba\u8a8d\u304c\u5b8c\u4e86\u3057\u305f")
			}
		})
	}
}

func TestMemoryUpgradeRegressionExcerpts(t *testing.T) {
	body := "## \u5408\u6210\u898b\u51fa\u3057\n\n\u6761\u4ef6A\u3067\u306f\u30c6\u30ca\u30f3\u30c8\u306e\u6240\u6709\u8005\u3092\u78ba\u8a8d\u3059\u308b\u3002\nCondition B keeps the current tenant boundary.\n\u4f8b\u5916C\u3067\u306f\u8ffd\u52a0\u306e\u78ba\u8a8d\u3092\u884c\u3046\u3002\n"
	quotes := FocusExcerpts(body, "\u30c6\u30ca\u30f3\u30c8 tenant", 180)
	if len(quotes) == 0 {
		t.Fatal("\u610f\u5473\u306e\u3042\u308b\u5f15\u7528\u304c\u7121\u3044")
	}
	for _, q := range quotes {
		if body[q.Start:q.End] != q.Text || strings.HasPrefix(q.Text, "#") || !(strings.HasSuffix(q.Text, "\u3002") || strings.HasSuffix(q.Text, ".")) {
			t.Fatalf("\u5f15\u7528\u306e\u5358\u4f4d\u307e\u305f\u306f\u5143\u4f4d\u7f6e\u3092\u4fdd\u305f\u306a\u3044: %+v", q)
		}
	}
	t.Logf("\u6587\u30fb\u884c\u306e\u5883\u754c\u3068\u5143\u4f4d\u7f6e\u3092\u4fdd\u6301\u3057\u305f\u5f15\u7528: count=%d", len(quotes))
}

func TestMemoryUpgradeRegressionUpgradeControls(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/synthetic-project"}
	first, err := b.Ingest(draft(seed(t, b, 1), "Synthetic unchanged preference"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := b.directory(false, "sources")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, first.Page.Sources[0].ID+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status != "blocked" || u.Reviewed != 0 || len(u.Blocked) != 1 {
		t.Fatalf("\u4e0d\u8db3\u539f\u8cc7\u6599\u3092\u672a\u89e3\u6c7a\u3067\u7dad\u6301\u3057\u306a\u3044: %+v %v", u, err)
	}
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || len(u.Tasks) != 1 {
		t.Fatalf("\u539f\u8cc7\u6599\u306e\u5fa9\u65e7\u5931\u6557: %+v %v", u, err)
	}
	task := u.Tasks[0]
	if _, err = b.Maintain(MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Reason: "\u5408\u6210\u539f\u8cc7\u6599\u3068\u73fe\u5728\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u3066\u4fdd\u6301\u3059\u308b\u3002"}); err != nil {
		t.Fatal(err)
	}
	u, err = (Book{Root: b.Root, Scope: b.Scope}).MemoryUpgrade()
	if err != nil || u.Status != "complete" || u.Reviewed != 1 {
		t.Fatalf("\u901a\u5e38\u306e\u78ba\u8a8d\u3067\u5b8c\u4e86\u3057\u306a\u3044: %+v %v", u, err)
	}
	t.Logf("\u539f\u8cc7\u6599\u306e\u5fa9\u65e7\u3068maintenance\u5f8c\u306e\u307f\u5b8c\u4e86: status=%s reviewed=%d total=%d", u.Status, u.Reviewed, u.Total)
}

func TestMemoryUpgradeRegressionSourcePaging(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/synthetic-project"}
	body := ""
	for i := 0; i < 2000; i++ {
		body += "\u65e5\u672c\u8a9e<&>\u3068\u5f15\u7528\\\"\u306e\u539f\u8cc7\u6599\u3002\n"
	}
	o, err := b.capture(Observation{Kind: "test", Title: "\u5408\u6210\u539f\u8cc7\u6599", Content: body, Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	recovered, cursor := "", ""
	pages := 0
	for ; pages < 100; pages++ {
		v, e := b.ReadSourcesPage([]string{o.ID}, cursor, 4096)
		if e != nil {
			t.Fatal(e)
		}
		if encodedSize(v) > 4096 {
			t.Fatalf("\u539f\u8cc7\u6599\u5fdc\u7b54\u304c4096\u3092\u8d85\u904e: %d", encodedSize(v))
		}
		part := v["observations"].([]Observation)
		if len(part) != 1 {
			t.Fatal("\u539f\u8cc7\u6599\u304c\u6d88\u3048\u305f")
		}
		recovered += part[0].Content
		next, _ := v["next_cursor"].(string)
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatal("cursor\u304c\u9032\u307e\u306a\u3044")
		}
		cursor = next
	}
	if pages >= 100 || recovered != body {
		t.Fatalf("\u539f\u8cc7\u6599\u306e\u5b8c\u5168\u56de\u5fa9\u306b\u5931\u6557: pages=%d bytes=%d/%d", pages+1, len(recovered), len(body))
	}
	t.Logf("Unicode\u3068JSON\u30a8\u30b9\u30b1\u30fc\u30d7\u3092\u542b\u3080\u539f\u8cc7\u6599\u3092\u5168\u56de\u5fa9: pages=%d bytes=%d", pages+1, len(body))
}

func TestMemoryUpgradeDeletionIsExcludedWithoutInventingReview(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/synthetic-project"}
			source := seed(t, b, 1)
			saved, e := b.Ingest(draft(source, "Synthetic target removed during upgrade"))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.MemoryUpgrade(); e != nil {
				t.Fatal(e)
			}
			if _, e = b.Manage(Change{ID: saved.Page.ID, Revision: saved.Page.Revision, Action: "delete"}); e != nil {
				t.Fatal(e)
			}
			directory, e := b.directory(false, "sources")
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(directory, source+".json")
			original, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if missing {
				if e = os.Remove(path); e != nil {
					t.Fatal(e)
				}
			}
			u, e := b.MemoryUpgrade()
			if e != nil {
				t.Fatal(e)
			}
			if missing {
				if u.Status == "complete" || u.Reviewed != 0 || len(u.Blocked) != 1 {
					t.Fatal("deletion must not hide unavailable originals", u)
				}
				if e = os.WriteFile(path, original, 0600); e != nil {
					t.Fatal(e)
				}
				u, e = b.MemoryUpgrade()
				if e != nil {
					t.Fatal(e)
				}
			}
			raw, _ := json.Marshal(u)
			var fields map[string]any
			_ = json.Unmarshal(raw, &fields)
			if u.Status != "complete" || u.Reviewed != 0 || fields["withdrawn"] != float64(1) {
				t.Fatal("a valid deletion closes its target as excluded, never reviewed", u, fields)
			}
			resumed, e := (Book{Root: b.Root, Scope: b.Scope}).MemoryUpgrade()
			if e != nil || resumed.Reviewed != 0 || resumed.Status != "complete" {
				t.Fatal("retain the explicit exclusion after resume", resumed, e)
			}
		})
	}
}

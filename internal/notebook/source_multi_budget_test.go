package notebook

import (
	"strings"
	"testing"
	"time"
)

func TestMultipleSourcesIncludeContinuationInBudget(t *testing.T) {
	for _, host := range []string{"JSON", "MCP"} {
		t.Run(host, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/synthetic-delivery-project"}
			ids := []string{}
			expected := map[string]string{}
			for _, n := range []int{61000, 61001} {
				o, err := b.capture(Observation{Kind: "test", Title: "boundary", Content: strings.Repeat("x", n), Reference: "synthetic", Version: "audit", ObservedAt: time.Unix(1, 0).UTC()})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, o.ID)
				expected[o.ID] = o.Content
			}
			got := map[string]string{}
			cursor, calls := "", 0
			for {
				page, err := b.ReadSourcesPageMeasured(ids, cursor, replyBudget, func(v any) int { return sourceFillSize(host, v) })
				if err != nil {
					t.Fatal(err)
				}
				if used := sourceFillSize(host, page); used > replyBudget {
					t.Fatalf("complete page exceeds budget: got=%d max=%d", used, replyBudget)
				}
				part, split := page["observation_part"].(map[string]any)
				for _, o := range page["observations"].([]Observation) {
					if split && part["id"] == o.ID && part["from"] != len(got[o.ID]) {
						t.Fatal("source parts must be contiguous", part)
					}
					got[o.ID] += o.Content
				}
				calls++
				next, _ := page["next_cursor"].(string)
				if next == "" {
					break
				}
				if next == cursor || calls > 20 {
					t.Fatal("continuation must progress")
				}
				cursor = next
			}
			if calls < 2 || len(got) != len(expected) {
				t.Fatal("all sources must be recovered across pages", calls, len(got))
			}
			for id, text := range expected {
				if got[id] != text {
					t.Fatal("no source bytes may be lost or duplicated", id)
				}
			}
		})
	}
}

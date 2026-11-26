package notebook

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResponseExcerptZeroBudgetDoesNotParse(t *testing.T) {
	body := strings.Repeat("The routing tenant remains unresolved.\n", 160)
	for _, budget := range []int{0, -1} {
		allocs := testing.AllocsPerRun(2, func() {
			out := FocusExcerpts(body, "routing", budget)
			raw, _ := json.Marshal(out)
			if string(raw) != "[]" {
				t.Fatal("empty excerpts changed shape", string(raw))
			}
		})
		if allocs > 4 {
			t.Errorf("zero budget parsed text: %.0f allocations", allocs)
		}
	}
	if len(FocusExcerpts(body, "", 400)) == 0 {
		t.Fatal("empty context still needs the first excerpts")
	}
}
func TestResponseOrdinarySearchProjectionAllocation(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 700; i++ {
		fmt.Fprintf(&body, "unrelatedtoken%d ", i)
	}
	pages := make([]Page, 8)
	for i := range pages {
		pages[i] = Page{ID: fmt.Sprintf("%016x", i+1), Title: "Routing tenant", AppliesTo: "routing", Body: body.String()}
	}
	var got []Page
	var matches []Match
	// 20 calls are enough: a call allocates the same bytes every time (about 29 KB, against the 128 KB budget).
	allocated := bytesPerOp(20, func() {
		got, matches = rankPages(pages, "routing tenant", false)
	})
	if len(got) != 8 || len(matches) != 8 || !reflect.DeepEqual(matches[0].Terms, []string{"routing", "tenant"}) {
		t.Fatal("all matching candidates and terms must survive")
	}
	if allocated > 128<<10 {
		t.Fatalf("ordinary search retained non-query vocabulary: %d B/op", allocated)
	}
}

func TestResponseSourceDecodeSharedOnlyWithinValidatedRead(t *testing.T) {
	book := Book{Root: t.TempDir(), Scope: "/synthetic"}
	o, e := book.capture(Observation{Kind: "test", Title: "Synthetic decode", Content: strings.Repeat("Original owner condition.\n", 1800), Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if e != nil {
		t.Fatal(e)
	}
	book = book.ReadSnapshot()
	var value map[string]any
	allocated := bytesPerOp(100, func() {
		value, e = book.ReadSources([]string{o.ID})
		if e != nil {
			t.Fatal(e)
		}
	})
	got := value["observations"].([]Observation)
	if len(got) != 1 || got[0].Content != o.Content {
		t.Fatal("decoded original changed")
	}
	got[0].Content = "caller change"
	next, e := book.ReadSources([]string{o.ID})
	if e != nil || next["observations"].([]Observation)[0].Content != o.Content {
		t.Fatal("caller changed shared decode", e)
	}
	if allocated > 32<<10 {
		t.Fatalf("validated source was repeatedly decoded: %d B/op", allocated)
	}
	if e = book.ValidateRead(); e != nil {
		t.Fatal(e)
	}
}

package notebook

import (
	"fmt"
	"reflect"
	"testing"
)

// The same request terms must give all pages the same accumulation order.
// Otherwise rounding alone can turn equal contributions into different ranks.
func TestSearchTermOrderPreservesScoresAndTies(t *testing.T) {
	pages := make([]Page, 7)
	for i := range pages {
		pages[i] = Page{ID: fmt.Sprintf("%016x", 7-i), Body: "owner"}
		if i < 2 {
			pages[i].Title, pages[i].AppliesTo, pages[i].Body = "alpha", "beta", "owner tenant"
		}
	}
	first, a := rankPages(pages, "alpha beta owner tenant", false)
	second, b := rankPages(pages, "alpha owner beta tenant", false)
	if !reflect.DeepEqual(first, pages[:2]) || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(a, b) {
		t.Fatalf("term order changed scores or tied ranks: first=%v second=%v", a, b)
	}
}

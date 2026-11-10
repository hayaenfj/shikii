package notebook

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A search reply carries the bodies of the first two matches only while they
// fit in one reply. A match whose body does not fit stays in search.candidates
// with its id and revision, and that article is read whole with lesson:<id>.
func TestSearchKeepsTheCandidateWhoseBodyDoesNotFit(t *testing.T) {
	// JSON writes each of < > & as six bytes, so 16000 bytes of comparisons are about 33 KiB in a reply.
	comparisons := strings.Repeat("if a < b && c > d {\n", 800)
	cases := []struct {
		name   string
		bodies []string
		with   int // how many bodies fit in the first reply
	}{
		{"both bodies fit", []string{"Compare the short way.", "Compare the other short way."}, 2},
		{"the second body does not fit beside the first", []string{comparisons, comparisons + "second"}, 1},
		{"the only body does not fit", []string{strings.Repeat("<", 16000)}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/project"}
			source := roundTripSource(t, b, 1)
			saved := map[string]Page{}
			for i, body := range c.bodies {
				d := Draft{Title: fmt.Sprintf("Comparison rules %d", i), Lesson: "Write comparisons the agreed way.", AppliesTo: "comparison code", Body: &body, Kind: "rule", Scene: "when comparing", Sources: []string{source}}
				r, err := b.Ingest(d)
				if err != nil {
					t.Fatal(err)
				}
				saved[r.Page.ID] = *r.Page
			}
			v, err := b.QueryPage("comparison", "")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(v)
			if len(data) > replyBudget {
				t.Errorf("the reply is %d bytes, over the budget of %d", len(data), replyBudget)
			}
			if v.Search == nil || v.Search.Total != len(c.bodies) || len(v.Search.Candidates) != len(c.bodies) || v.NextCursor != "" {
				t.Fatalf("every match must stay a candidate of this reply: %+v next=%q", v.Search, v.NextCursor)
			}
			if len(v.Lessons) != c.with || len(v.Matches) != c.with {
				t.Fatalf("%d bodies and %d matches came with the reply, want %d", len(v.Lessons), len(v.Matches), c.with)
			}
			supplied := map[string]bool{}
			for _, p := range v.Lessons {
				supplied[p.ID] = p.Body == saved[p.ID].Body
			}
			for _, candidate := range v.Search.Candidates {
				want, ok := saved[candidate.ID]
				if !ok || candidate.Revision != want.Revision || candidate.Title != want.Title {
					t.Fatalf("a candidate does not name its article and revision: %+v", candidate)
				}
				if supplied[candidate.ID] {
					continue
				}
				// The candidate without a body is opened by its id: one article always comes whole.
				whole, err := b.QueryPage("lesson:"+candidate.ID, "")
				if err != nil || len(whole.Lessons) != 1 || whole.Lessons[0].Revision != want.Revision || whole.Lessons[0].Body != want.Body || whole.NextCursor != "" {
					t.Errorf("the omitted body of %s cannot be read with lesson:<id>: %v", candidate.ID, err)
				}
			}
			for i := range v.Lessons {
				v.Lessons[i].Body = fmt.Sprintf("(%d bytes)", len(v.Lessons[i].Body))
			}
			shown, _ := json.Marshal(v)
			t.Logf("reply of %d bytes (budget %d), bodies shortened: %s", len(data), replyBudget, shown)
		})
	}
}

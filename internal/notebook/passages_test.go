package notebook

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestKnowledgeOrientationBudgetsAndExactUnicode(t *testing.T) {
	body := "Earlier context.\n\n" + strings.Repeat("\u73fe\u5728\u306e\u6761\u4ef6\u3002", 20) + "\n\nUnclassified independent details."
	quote := strings.Repeat("\u73fe\u5728\u306e\u6761\u4ef6\u3002", 20)
	p := Page{ID: "0000000000000001", Revision: "0000000000000002", Body: body, Lesson: "Use the current condition.", AppliesTo: "This workflow", Passages: []KnowledgePassage{{Role: "current", Quote: quote, Conditions: "Current workflow", Reason: "The cited verification applies."}, {Role: "history", Quote: "Earlier context.", Conditions: "Earlier workflow", Reason: "Earlier conditions differ."}}}
	for _, budget := range []int{0, 1, 2, 15, 200, 600, 2400} {
		v := Orient(p, "independent details", budget)
		seen := make([]bool, len(body))
		total := 0
		for _, x := range v.Excerpts() {
			if x.Start < 0 || x.End > len(body) || x.Start >= x.End || !utf8.ValidString(x.Text) || body[x.Start:x.End] != x.Text {
				t.Fatal("orientation must return exact UTF-8 source ranges", budget, x)
			}
			for i := x.Start; i < x.End; i++ {
				if seen[i] {
					t.Fatal("a passage must not be emitted under two roles", budget, x)
				}
				seen[i] = true
			}
			total += len(x.Text)
		}
		if total > budget || !v.NeedsInterpretation {
			t.Fatal("budget and unclassified remainder must remain explicit", budget, total, v)
		}
	}
}

package notebook

import (
	"strings"
	"testing"
)

func TestExcerptsKeepSentenceBoundariesAndOmitBareHeadings(t *testing.T) {
	body := "## Routing\n\n" + strings.Repeat("The routing condition preserves the tenant. ", 40) + "\n\n## X"
	xs := FocusExcerpts(body, "routing tenant", 850)
	if len(xs) == 0 {
		t.Fatal("return relevant complete sentences")
	}
	for _, x := range xs {
		text := strings.TrimSpace(x.Text)
		if body[x.Start:x.End] != x.Text || strings.HasPrefix(text, "##") || !strings.HasSuffix(text, ".") {
			t.Fatal("quotes must be exact meaningful sentences, not byte fragments or headings", x)
		}
	}
	if xs := FocusExcerpts("## Heading only\n\n", "heading", 10); len(xs) != 0 {
		t.Fatal("a heading fragment is not a usable quote", xs)
	}
}

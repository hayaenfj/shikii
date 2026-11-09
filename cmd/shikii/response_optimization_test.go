package main

import (
	"encoding/json"
	"github.com/hayaenfj/shikii/internal/notebook"
	"reflect"
	"strings"
	"testing"
)

func TestResponsePreparationSizerReusesOnlyExactGeneration(t *testing.T) {
	calls := 0
	sizer := workPreparationSizer([]func(any) int{func(v any) int {
		calls++
		return workContextSize(toolResult(200, mustResponseJSON(t, v), "shikii_wiki"))
	}})
	reading := &workReadingTrace{Bytes: 1}
	row := map[string]any{"text": strings.Repeat("<>&\n~ original ", 50), "metadata_from": map[string]any{"delivery": 1}}
	value := map[string]any{"knowledge": []any{row}, "reading": reading}
	check := func() {
		t.Helper()
		if got, want := sizer(value), workContextSize(toolResult(200, mustResponseJSON(t, value), "shikii_wiki")); got != want {
			t.Fatalf("stale host size: got=%d want=%d", got, want)
		}
	}
	check()
	check()
	if calls != 1 {
		t.Errorf("identical generation rendered twice: %d", calls)
	}
	row["text"] = "changed <&>\n"
	check()
	reading.Bytes = 17
	check()
	saved := row["metadata_from"]
	delete(row, "metadata_from")
	check()
	row["metadata_from"] = saved
	check()
	n := calls
	check()
	if calls != n {
		t.Error("unchanged rollback generation was not reused")
	}
}
func mustResponseJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestResponseBriefReflectionDoesNotCopyDiscardedBody(t *testing.T) {
	page := notebook.Page{ID: "0000000000000001", Revision: "0000000000000002", Title: "Routing", Lesson: "Keep owner unresolved", AppliesTo: "When routing"}
	view := reflectView{Lessons: []notebook.Page{page}, Corrections: map[string]any{"request": "Original <&> condition"}}
	short, e := preparedReflectionContext(view, true)
	if e != nil {
		t.Fatal(e)
	}
	view.Lessons[0].Body = strings.Repeat("Discarded synthetic body.\n", 500)
	var out map[string]any
	measure := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			out, e = preparedReflectionContext(view, true)
			if e != nil {
				b.Fatal(e)
			}
		}
	})
	if !reflect.DeepEqual(out, short) {
		t.Fatal("brief values changed with discarded body")
	}
	if measure.AllocedBytesPerOp() > 16<<10 {
		t.Fatalf("brief context copied discarded body: %d B/op", measure.AllocedBytesPerOp())
	}
}

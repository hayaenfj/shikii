package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"reflect"
	"strings"
	"testing"
)

func TestResponseBriefReflectionRetainsCompleteContext(t *testing.T) {
	for _, lessons := range [][]notebook.Page{nil, {}, {{ID: "0000000000000001", Revision: "0000000000000002", Title: "Routing <&>", Lesson: "Unresolved owner", AppliesTo: "Original routing condition", Body: strings.Repeat("Original condition\n", 100)}}} {
		for _, brief := range []bool{false, true} {
			view := reflectView{HowToSubmit: "Original instructions", Sources: []reflectSource{{Source: store.Feedback{ID: "0000000000000003", Request: "Original request", Summary: "Original summary", Reply: "Original reply"}, WikiPending: true}}, Observations: []notebook.ObservationHead{{ID: "0000000000000004", Content: "Original evidence"}}, Maintenance: []notebook.MaintenanceTask{{PageID: "0000000000000005", Reasons: []string{"Original reason"}}}, MemoryEffect: store.MemorySummary{Window: 13}, Lessons: lessons, Corrections: map[string]any{"full": "Original <&> condition\n", "count": json.Number("9007199254740993")}, MorePending: 7}
			got, e := preparedReflectionContext(view, brief)
			want, we := responseOriginalReflectionContext(view, brief)
			if fmt.Sprint(e) != fmt.Sprint(we) || !reflect.DeepEqual(got, want) {
				t.Fatalf("reflection field changed: brief=%v got=%v want=%v", brief, got, want)
			}
		}
	}
}
func TestResponseSizerMatchesEveryHostShape(t *testing.T) {
	for _, host := range []string{"json", "mcp"} {
		calls := 0
		hostSize := func(v any) int {
			calls++
			raw, _ := json.Marshal(v)
			if host == "mcp" {
				return workContextSize(map[string]any{"jsonrpc": "2.0", "id": json.Number("9007199254740993"), "result": toolResult(200, raw, "shikii_wiki")}) + 1
			}
			return len(raw) + 1
		}
		size := workPreparationSizer([]func(any) int{hostSize})
		value := map[string]any{"knowledge": []any{}, "integer": json.Number("9007199254740993"), "fraction": json.Number("0.1234567890123456789")}
		for _, text := range []string{"", strings.Repeat("Original <&> condition\n~ ", 12), strings.Repeat("Original <&> condition\n~ ", 60), "\u2028\u2029\t\""} {
			value["first"], value["second"] = text, text
			want := hostSize(value)
			before := calls
			if got := size(value); got != want {
				t.Fatal("host shape size changed", host, got, want)
			}
			if got := size(value); got != want || calls != before+1 {
				t.Fatal("exact repeated shape was not reused", host)
			}
		}
		// A separate post-save sizing pass must consult the host again.
		fresh := workPreparationSizer([]func(any) int{hostSize})
		n := calls
		fresh(value)
		if calls != n+1 {
			t.Fatal("final host verification reused an earlier pass")
		}
		bad := map[string]any{"invalid": make(chan int)}
		n = calls
		size(bad)
		size(bad)
		if calls != n+2 {
			t.Fatal("encoding errors were cached")
		}
	}
	for _, code := range []int{400, 500} {
		raw := []byte(`{"error":"original <&> condition"}`)
		value := toolResult(code, raw, "shikii_wiki")
		if value["isError"] != true || value["structuredContent"] != nil {
			t.Fatal("error response shape changed")
		}
		if !bytes.Equal(raw, []byte(value["content"].([]map[string]any)[0]["text"].(string))) {
			t.Fatal(errors.New("error text changed"))
		}
	}
}
func responseOriginalReflectionContext(view reflectView, brief ...bool) (map[string]any, error) {
	view.Sources, view.Observations, view.Maintenance = nil, nil, nil
	view.HowToSubmit = ""
	data, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	var context map[string]any
	if err = json.Unmarshal(data, &context); err != nil {
		return nil, err
	}

	for _, key := range []string{"how_to_submit", "sources", "observations", "maintenance", "more_pending"} {
		delete(context, key)
	}
	if len(brief) > 0 && brief[0] {
		lessons, _ := context["lessons"].([]any)
		references := []any{}
		for _, raw := range lessons {
			page := raw.(map[string]any)
			id, _ := page["id"].(string)
			revision, _ := page["revision"].(string)
			references = append(references, map[string]any{"id": id, "revision": revision, "title": page["title"], "lesson": page["lesson"], "applies_to": page["applies_to"], "incomplete": true, "query": map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}})
		}
		if len(references) > 0 {
			context["lessons"], context["incomplete"], context["more_lessons"] = references, true, 0
			context["next_action"] = "Brief supporting knowledge retains exact revision queries instead of whole bodies. Read a referenced article when its conditions or exceptions matter. Human corrections and required source material are retained."
		}
	}
	if workContextSize(context) <= workSourceBudget {
		return context, nil
	}
	lessons, _ := context["lessons"].([]any)
	context["lessons"] = []any{}
	context["incomplete"] = true
	context["more_lessons"] = 0
	context["next_action"] = "Some related lessons are referenced instead of supplied whole. Read each incomplete article's exact query with shikii_wiki or wiki query before using it. If more_lessons remains, read the complete supporting context with shikii_reflect query. Human corrections are retained; references alone do not establish meaning."
	if workContextSize(context) > workSourceBudget-1024 {
		return nil, errors.New("the supporting human corrections exceed the preparation budget; read shikii reflect query and use the individual operations instead of an incomplete prepared context")
	}
	kept := []any{}
	for i, lesson := range lessons {
		candidate := append(append([]any{}, kept...), lesson)
		context["lessons"] = candidate
		if workContextSize(context) <= workSourceBudget-1024 {
			kept = candidate
			continue
		}
		page := lesson.(map[string]any)
		id, _ := page["id"].(string)
		revision, _ := page["revision"].(string)
		reference := map[string]any{"id": id, "revision": revision, "title": page["title"], "incomplete": true, "query": map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}}
		candidate = append(append([]any{}, kept...), reference)
		context["lessons"] = candidate
		if workContextSize(context) > workSourceBudget-1024 {
			context["lessons"] = kept
			context["more_lessons"] = len(lessons) - i
			break
		}
		kept = candidate
	}
	return context, nil
}

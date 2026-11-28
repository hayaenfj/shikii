package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// walkPath follows a name such as "article.lesson" through decoded JSON and returns the value it ends at.
func walkPath(root map[string]any, path string) (any, bool) {
	var at any = root
	for _, part := range strings.Split(path, ".") {
		object, ok := at.(map[string]any)
		if !ok {
			return nil, false
		}
		if at, ok = object[part]; !ok {
			return nil, false
		}
	}
	return at, true
}

// fieldNames reads metadata_from.fields of a delivered entry.
func fieldNames(t *testing.T, e map[string]any) []string {
	t.Helper()
	from, _ := e["metadata_from"].(map[string]any)
	raw, _ := from["fields"].([]any)
	if len(raw) == 0 {
		t.Fatalf("the entry refers to an earlier delivery: %v", e)
	}
	names := []string{}
	for _, name := range raw {
		names = append(names, name.(string))
	}
	return names
}

// assertFieldsPointAtValues walks every name of fields through the earlier delivery's entry: each must end at a value, and the four
// places of the texts must end at the text.
func assertFieldsPointAtValues(t *testing.T, names []string, earlier map[string]any, texts map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("the name %q is written twice in %v", name, names)
		}
		seen[name] = true
		value, found := walkPath(earlier, name)
		if !found {
			t.Errorf("the name %q leads nowhere in the delivery it refers to: %v", name, earlier)
			continue
		}
		if want, isText := texts[name]; isText && value != want {
			t.Errorf("the name %q leads to %v, not to the text %q", name, value, want)
		}
	}
}

// fieldsFixture is a candidate that holds its own summary, conditions, id and revision, as a shortened one does, and the page behind it.
func fieldsFixture() (notebook.Page, func() map[string]any) {
	p := orientationPage(40)
	p.Lesson = strings.Repeat(p.Lesson+" ", 12)
	whole := notebook.Orient(p, "owner approval", 300)
	return p, func() map[string]any {
		e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "reused": true, "body_included": false, "query": "lesson:" + p.ID + "@" + p.Revision}
		bindOrientation(e, whole)
		return e
	}
}

// deliverTwice delivers a candidate, remembers it, and delivers the same candidate again with the state a delivery left. It returns
// the first entry as written (decoded) and the second entry.
func deliverTwice(t *testing.T, candidate func() map[string]any, w *workState) (first, second map[string]any) {
	t.Helper()
	one := candidate()
	reply := map[string]any{"knowledge": []any{one}}
	if _, err := limitWorkPreparation(notebook.Book{}, w, reply, map[string]string{}, 65536); err != nil {
		t.Fatal(err)
	}
	rememberWorkMetadata(w, reply)
	two := candidate()
	next := map[string]any{"knowledge": []any{two}}
	if _, err := limitWorkPreparation(notebook.Book{}, w, next, map[string]string{}, 65536); err != nil {
		t.Fatal(err)
	}
	return jsonOf(t, one), jsonOf(t, two)
}

func newDeliveryState() *workState {
	return &workState{Nonce: "synthetic", Reasoning: &reasoningProgress{Reading: &workReadingTrace{}}}
}

// The names of metadata_from.fields are the places where the earlier delivery held each text, walked as paths through that delivery.
func TestMetadataFromNamesWhereTheEarlierDeliveryHeldEachText(t *testing.T) {
	t.Run("a candidate that held its own fields", func(t *testing.T) {
		p, candidate := fieldsFixture()
		first, second := deliverTwice(t, candidate, newDeliveryState())
		names := fieldNames(t, second)
		assertFieldsPointAtValues(t, names, first, map[string]string{"id": p.ID, "revision": p.Revision, "summary": p.Lesson, "applies_to": p.AppliesTo})
		for _, want := range []string{"id", "revision", "summary", "applies_to", "orientation.current", "orientation.history", "title", "rationale"} {
			if !slices.Contains(names, want) {
				t.Errorf("fields name %q: %v", want, names)
			}
		}
		for _, repeated := range []string{"orientation.id", "orientation.revision", "orientation.summary", "orientation.conditions"} {
			if slices.Contains(names, repeated) {
				t.Errorf("the orientation of the first delivery did not write %s: %v", repeated, names)
			}
		}
	})

	t.Run("a candidate whose summary differs from the orientation's", func(t *testing.T) {
		p, candidate := fieldsFixture()
		other := func() map[string]any {
			e := candidate()
			e["summary"] = "A summary the candidate words differently."
			return e
		}
		first, second := deliverTwice(t, other, newDeliveryState())
		names := fieldNames(t, second)
		assertFieldsPointAtValues(t, names, first, map[string]string{"orientation.summary": p.Lesson, "summary": "A summary the candidate words differently.", "applies_to": p.AppliesTo})
		if !slices.Contains(names, "orientation.summary") || !slices.Contains(names, "summary") || slices.Contains(names, "orientation.conditions") {
			t.Errorf("the orientation wrote its summary and held nothing else back from the candidate's conditions: %v", names)
		}
	})

	t.Run("an article that held the texts", func(t *testing.T) {
		first, second, third := deltaPrepare(t)
		article, _ := first["knowledge"].([]any)[0].(map[string]any)["article"].(map[string]any)
		texts := map[string]string{"article.lesson": article["lesson"].(string), "article.applies_to": article["applies_to"].(string)}
		for _, response := range []map[string]any{second, third} {
			names := fieldNames(t, response["knowledge"].([]any)[0].(map[string]any))
			assertFieldsPointAtValues(t, names, first["knowledge"].([]any)[0].(map[string]any), texts)
			for _, want := range []string{"article.lesson", "article.applies_to", "orientation.current", "orientation.history"} {
				if !slices.Contains(names, want) {
					t.Errorf("fields name %q: %v", want, names)
				}
			}
			for _, repeated := range []string{"orientation.summary", "orientation.conditions"} {
				if slices.Contains(names, repeated) {
					t.Errorf("the first delivery's orientation did not write %s, the article held it: %v", repeated, names)
				}
			}
		}
	})
}

// A record saved before Held existed, or with a place nobody writes, still gives the names it always gave.
func TestMetadataFromKeepsItsOldNamesForOlderOrDamagedRecords(t *testing.T) {
	_, candidate := fieldsFixture()
	signature, ok := workMetadataSignature(candidate())
	if !ok {
		t.Fatal("control: the candidate has a signature")
	}
	old := []string{"orientation.id", "orientation.revision", "orientation.summary", "orientation.conditions", "orientation.current", "orientation.history", "title", "summary", "applies_to", "rationale"}
	for name, record := range map[string]string{
		"saved by an older version": `{"signature":"` + signature + `","delivery":1}`,
		"an empty held":             `{"signature":"` + signature + `","delivery":1,"held":{}}`,
		"a place that is not known": `{"signature":"` + signature + `","delivery":1,"held":{"summary":"article.secret","id":"summary","revision":"","conditions":"orientation.conditions"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var state workDeliveryState
			if err := json.Unmarshal([]byte(`{"sequence":1,"metadata":{"knowledge1":`+record+`}}`), &state); err != nil {
				t.Fatal(err)
			}
			w := newDeliveryState()
			w.Delivery = &state
			e := candidate()
			reply := map[string]any{"knowledge": []any{e}}
			if _, err := limitWorkPreparation(notebook.Book{}, w, reply, map[string]string{}, 65536); err != nil {
				t.Fatal(err)
			}
			names := fieldNames(t, jsonOf(t, e))
			if !slices.Equal(names, old) {
				t.Fatalf("the names of an older record are those it always had:\n got %v\nwant %v", names, old)
			}
		})
	}
	// the record of a delivery whose orientation wrote everything is written as it always was
	if raw, _ := json.Marshal(workMetadataRef{Signature: "s", Delivery: 1}); string(raw) != `{"signature":"s","delivery":1}` {
		t.Errorf("a record without held is saved as before: %s", raw)
	}
	// and a record with it is read back
	var back workMetadataRef
	raw, _ := json.Marshal(workMetadataRef{Signature: "s", Delivery: 2, Held: map[string]string{"summary": "article.lesson"}})
	if err := json.Unmarshal(raw, &back); err != nil || back.Held["summary"] != "article.lesson" {
		t.Errorf("a record with held is read back: %s %v", raw, err)
	}
}

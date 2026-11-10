package spec

import (
	"strings"
	"testing"
)

func choiceWithID(id string, options ...Option) *Spec {
	if len(options) == 0 {
		options = []Option{{ID: "a", Description: "the first"}, {ID: "b", Description: "the second"}}
	}
	return &Spec{
		ID: id, Version: 1, Type: Choice, Question: "Which one?", Options: options,
		State:      State{Template: "{{ message }}", Required: []string{"message"}},
		Policy:     Policy{SendTo: []string{"local"}, PII: boolPtr(false)},
		Thresholds: Thresholds{Act: 0.7, Ask: 0.5},
		Eval:       Eval{MinN: 2},
	}
}

// The id names the spec's files in the store (records/<id>.jsonl, labels/<id>.jsonl). An id with a path
// separator puts them in another folder (or tries to leave the store), and "guards" is the file the stop checks
// write their own records to. Such a spec must not load.
func TestValidateRefusesAnIDThatIsNotOneFileNameOfItsOwn(t *testing.T) {
	for _, id := range []string{"a/b", "../labels/x", "../../outside", "/abs", `a\b`, "a\nb", "a\x00b", "guards"} {
		if err := choiceWithID(id).Validate(); err == nil || !strings.Contains(err.Error(), "id") {
			t.Errorf("id %q must be refused, got %v", id, err)
		}
	}
}

// Guard: the ids in use stay valid, also ones with dots, digits or letters outside ASCII, and names that only
// resemble the reserved one.
func TestValidateKeepsPlainIDs(t *testing.T) {
	for _, id := range []string{"prompt-kind", "same_issue", "inquiry-route-v2", "v1.2", "..x", "guards-2", "my-guards", "\u554f\u3044\u5408\u308f\u305b"} {
		if err := choiceWithID(id).Validate(); err != nil {
			t.Errorf("id %q must stay valid: %v", id, err)
		}
	}
	if err := choiceWithID("").Validate(); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Errorf("an empty id keeps its own message: %v", err)
	}
}

// Two options with the same id are one option: the request to the model carries the key twice and the answer
// can only hold one of them, so "at least 2 options" is not met.
func TestValidateRefusesTwoOptionsWithTheSameID(t *testing.T) {
	sp := choiceWithID("route", Option{ID: "a", Description: "the first"}, Option{ID: "a", Description: "the same id again"})
	if err := sp.Validate(); err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Fatalf("a duplicated option id must be refused with the id named, got %v", err)
	}
	three := choiceWithID("route", Option{ID: "a", Description: "one"}, Option{ID: "b", Description: "two"}, Option{ID: "a", Description: "again"})
	if err := three.Validate(); err == nil {
		t.Fatalf("a duplicated option id among three must be refused")
	}
}

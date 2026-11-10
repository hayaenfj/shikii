package spec

import "testing"

func renderSpec(template string, required ...string) *Spec {
	return &Spec{
		ID: "r", Type: Noul, Question: "Is it so?",
		State:      State{Template: template, Required: required},
		Policy:     Policy{SendTo: []string{"local"}, PII: boolPtr(false)},
		Thresholds: Thresholds{Act: 0.7, Ask: 0.5},
		Eval:       Eval{MinN: 2},
	}
}

// An input's text is data: a "{{ name }}" a person typed in one input must not be filled with another input.
// Each input here quotes the other's placeholder, so every replacement order of the old loop rewrites one of them
// (the test does not depend on map order).
func TestRenderDoesNotExpandPlaceholdersInsideInputText(t *testing.T) {
	sp := renderSpec("Request: {{ request }}\nSummary: {{ summary }}", "request", "summary")
	inputs := map[string]string{
		"request": "explain the {{ summary }} placeholder",
		"summary": "answered about {{ request }}",
	}
	want := "Request: explain the {{ summary }} placeholder\nSummary: answered about {{ request }}"
	got, missing := sp.Render(inputs)
	if len(missing) != 0 {
		t.Fatalf("missing: %v", missing)
	}
	if got != want {
		t.Fatalf("input text was treated as a template:\n got: %q\nwant: %q", got, want)
	}
}

// Every spelling that counts as a placeholder for the inputs hash and for state.context (placeholders():
// any spacing inside the braces) must also be filled. Left as is, the model reads the literal braces for every input.
func TestRenderFillsEveryPlaceholderSpellingTheHashCounts(t *testing.T) {
	for _, tpl := range []string{"A: {{a }}", "A: {{ a}}", "A: {{  a  }}", "A: {{\ta\t}}"} {
		sp := renderSpec(tpl, "a")
		if got := placeholders(tpl); len(got) != 1 || got[0] != "a" {
			t.Fatalf("%q: placeholders() = %v, want [a]", tpl, got)
		}
		got, missing := sp.Render(map[string]string{"a": "hello"})
		if len(missing) != 0 || got != "A: hello" {
			t.Fatalf("%q rendered as %q (missing %v), want %q", tpl, got, missing, "A: hello")
		}
	}
}

// Guard: what Render did before stays the same. Both documented spellings are filled, repeated placeholders are
// all filled, and a placeholder with no input (an optional one) is left as written.
func TestRenderKeepsDocumentedBehaviour(t *testing.T) {
	sp := renderSpec("{{ a }}|{{a}}|{{ a }}|{{ optional }}", "a")
	got, missing := sp.Render(map[string]string{"a": "x", "unused": "y"})
	if len(missing) != 0 || got != "x|x|x|{{ optional }}" {
		t.Fatalf("got %q (missing %v)", got, missing)
	}
	if _, missing := sp.Render(map[string]string{"a": "  "}); len(missing) != 1 || missing[0] != "a" {
		t.Fatalf("a blank required input must be reported missing: %v", missing)
	}
}

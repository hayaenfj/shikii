package spec

import (
	"strings"
	"testing"
)

// The minimal question is built from state.template alone. An input the template does not name (here the
// context step's "previous") must never reach it, also when another input's text quotes its placeholder, and the
// text must be the same every time: the fill reads the template once, so nothing depends on map order.
// 200 renders of the same inputs are compared (a check that the result is fixed, not a wait for a race).
func TestRenderOfTheMinimalQuestionNeverCarriesAnInputItDoesNotName(t *testing.T) {
	sp := renderSpec("Request: {{ request }}\nSummary: {{ summary }}", "request", "summary")
	sp.State.Context = "Request: {{ request }}\nSummary: {{ summary }}\nBefore: {{ previous }}"
	if err := sp.Validate(); err != nil {
		t.Fatal(err)
	}
	const secret = "PREVIOUS-TURN-TEXT"
	inputs := map[string]string{
		"request":  "see {{ previous }} and {{previous}} and {{ summary }}",
		"summary":  "about {{ request }} / {{ previous }} / {{ extra }}",
		"previous": secret + " {{ request }}",
		"extra":    "EXTRA-TEXT {{ previous }}",
		"unused":   "UNUSED-TEXT",
	}
	wantMinimal := "Request: see {{ previous }} and {{previous}} and {{ summary }}\nSummary: about {{ request }} / {{ previous }} / {{ extra }}"
	wantContext := wantMinimal + "\nBefore: " + secret + " {{ request }}"

	csp := sp.ContextSpec()
	for i := 0; i < 200; i++ {
		got, missing := sp.Render(inputs)
		if len(missing) != 0 {
			t.Fatalf("render %d: missing %v", i, missing)
		}
		if got != wantMinimal {
			t.Fatalf("render %d of the minimal question differs:\n got: %q\nwant: %q", i, got, wantMinimal)
		}
		for _, leaked := range []string{secret, "EXTRA-TEXT", "UNUSED-TEXT"} {
			if strings.Contains(got, leaked) {
				t.Fatalf("render %d: the minimal question carries %q, an input its template does not name:\n%s", i, leaked, got)
			}
		}
		ctx, missing := csp.Render(inputs)
		if len(missing) != 0 || ctx != wantContext {
			t.Fatalf("render %d of the context question differs (missing %v):\n got: %q\nwant: %q", i, missing, ctx, wantContext)
		}
	}

	// The inputs the minimal question is keyed by are the ones its template names, whatever else is passed.
	keyed := sp.StateInputs(inputs)
	if len(keyed) != 2 || keyed["request"] != inputs["request"] || keyed["summary"] != inputs["summary"] {
		t.Fatalf("the minimal question's inputs must be request and summary only: %v", keyed)
	}
}

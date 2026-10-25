package spec

import "testing"

// The arrangement (layout) is part of the wording: it goes into the hash, a variant can change it, and unknown names are rejected.
// With the same weights, the arrangement alone moved the result by 13 points (docs/bench.md), so tune measures and picks it instead of a person choosing by hand.
func TestLayoutIsPartOfTheWordingAndIsValidated(t *testing.T) {
	sp := choiceSpec()
	before := sp.Hash()
	if err := sp.Validate(); err != nil {
		t.Fatal(err)
	}
	sp.Layout = "plain"
	if err := sp.Validate(); err != nil {
		t.Fatalf("plain is a known layout: %v", err)
	}
	if sp.Hash() == before {
		t.Fatalf("the layout changes how the model is asked, so it must change the hash")
	}
	sp.Layout = "sideways"
	if err := sp.Validate(); err == nil {
		t.Fatalf("an unknown layout must be rejected")
	}
	sp.Layout = ""
	v := sp.WithVariant(Variant{Name: "p", Layout: "plain"})
	if v.Layout != "plain" || v.Hash() == sp.Hash() {
		t.Fatalf("a variant may change only the layout: %+v", v)
	}
	sp.Variants = []Variant{{Name: "bad", Layout: "sideways"}}
	if err := sp.Validate(); err == nil {
		t.Fatalf("a variant with an unknown layout must be rejected")
	}
	sp.Variants = []Variant{{Name: "only-layout", Layout: "state-first"}}
	if err := sp.Validate(); err != nil {
		t.Fatalf("a layout-only variant changes something: %v", err)
	}
}

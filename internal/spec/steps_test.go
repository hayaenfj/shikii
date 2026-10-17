package spec

import (
	"strings"
	"testing"
)

func choiceSpec() *Spec {
	return &Spec{
		ID: "intent", Type: Choice, Question: "What is asked?",
		Options:    []Option{{"change_request", "wants a change"}, {"question", "wants an answer"}, {"other", "none"}},
		State:      State{Template: "{{ message }}", Required: []string{"message"}},
		Policy:     Policy{SendTo: []string{"local"}, PII: boolPtr(false)},
		Thresholds: Thresholds{Act: 0.75, Ask: 0.6},
		Eval:       Eval{MinN: 2},
	}
}

// 分解(steps): はい/いいえ の列。各選択肢はちょうど 1 つの葉。途中の段は yes か no の片方だけで確定し、最後の段は両方で確定する。
func TestStepsMustCoverEveryOptionExactlyOnce(t *testing.T) {
	good := []Step{{Ask: "Does it ask for work?", No: "other"}, {Ask: "Does it ask to change files?", Yes: "change_request", No: "question"}}
	sp := choiceSpec()
	sp.Steps = good
	if err := sp.Validate(); err != nil {
		t.Fatalf("a well-formed chain must validate: %v", err)
	}
	bad := map[string][]Step{
		"option missing":        {{Ask: "work?", No: "other"}, {Ask: "change?", Yes: "change_request"}},
		"option twice":          {{Ask: "work?", No: "other"}, {Ask: "change?", Yes: "change_request", No: "other"}},
		"middle step with both": {{Ask: "work?", Yes: "change_request", No: "other"}, {Ask: "x?", Yes: "question"}},
		"unknown option":        {{Ask: "work?", No: "nope"}, {Ask: "change?", Yes: "change_request", No: "question"}},
		"empty ask":             {{Ask: "", No: "other"}, {Ask: "change?", Yes: "change_request", No: "question"}},
	}
	for name, steps := range bad {
		sp := choiceSpec()
		sp.Steps = steps
		if err := sp.Validate(); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
	n := &Spec{ID: "n", Type: Noul, Question: "Is it?", State: State{Template: "{{ a }}", Required: []string{"a"}}, Policy: Policy{SendTo: []string{"local"}, PII: boolPtr(false)}, Thresholds: Thresholds{Act: 0.75, Ask: 0.6}, Eval: Eval{MinN: 2},
		Steps: []Step{{Ask: "x?", Yes: "yes", No: "no"}}}
	if err := n.Validate(); err == nil {
		t.Fatal("steps are for choice specs only")
	}
}

// 分解は聞き方なので鍵(hash)に入る。steps の無い spec の鍵は変わらない。
func TestStepsChangeTheHashAndTravelWithVariants(t *testing.T) {
	sp := choiceSpec()
	flat := sp.Hash()
	withSteps := choiceSpec()
	withSteps.Steps = []Step{{Ask: "work?", No: "other"}, {Ask: "change?", Yes: "change_request", No: "question"}}
	if withSteps.Hash() == flat {
		t.Fatal("adding steps must change the calibration key")
	}
	reworded := choiceSpec()
	reworded.Steps = []Step{{Ask: "any work?", No: "other"}, {Ask: "change?", Yes: "change_request", No: "question"}}
	if reworded.Hash() == withSteps.Hash() {
		t.Fatal("rewording a step must change the key")
	}
	v := Variant{Name: "two-step", Steps: withSteps.Steps}
	sp.Variants = []Variant{v}
	if err := sp.Validate(); err != nil {
		t.Fatalf("a steps-only variant changes something and must validate: %v", err)
	}
	c := sp.WithVariant(v)
	if len(c.Steps) != 2 || c.Hash() != withSteps.Hash() {
		t.Fatalf("WithVariant must apply the steps: %+v", c.Steps)
	}
	c.Steps[0].Ask = "mutated"
	if sp.Variants[0].Steps[0].Ask == "mutated" {
		t.Fatal("WithVariant must copy the steps, not share them")
	}
	sp.Variants = []Variant{{Name: "bad", Steps: []Step{{Ask: "x?", No: "other"}}}}
	if err := sp.Validate(); err == nil || !strings.Contains(err.Error(), "steps") {
		t.Fatalf("a variant's steps are validated like the spec's: %v", err)
	}
}

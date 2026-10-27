package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func noulSpec() *Spec {
	return &Spec{
		ID: "t", Type: Noul, Question: "Is it so?",
		State:      State{Template: "A: {{ a }}\nB: {{ b }}", Required: []string{"a", "b"}},
		Policy:     Policy{SendTo: []string{"hosted"}, PII: boolPtr(false)},
		Thresholds: Thresholds{Act: 0.7, Ask: 0.5},
		Eval:       Eval{MinN: 1},
	}
}

// Calibration key: the hash changes when the question, template or options change, and not when thresholds or policy change.
func TestHashTracksCalibratedContract(t *testing.T) {
	base := noulSpec()
	h := base.Hash()

	q := noulSpec()
	q.Question = "Is it so? (one word changed)"
	if q.Hash() == h {
		t.Fatal("changing the question must change the hash; the calibration was fitted against that wording")
	}

	tm := noulSpec()
	tm.State.Template = "B: {{ b }}\nA: {{ a }}"
	if tm.Hash() == h {
		t.Fatal("changing the state template must change the hash")
	}

	th := noulSpec()
	th.Thresholds = Thresholds{Act: 0.9, Ask: 0.8}
	th.Policy.SendTo = []string{"local"}
	if th.Hash() != h {
		t.Fatal("thresholds and policy must not affect the hash; moving them keeps the calibration valid")
	}

	c1 := &Spec{Type: Choice, Question: "q", State: State{Template: "x"},
		Options: []Option{{ID: "a", Description: "A"}, {ID: "b", Description: "B"}}}
	c2 := &Spec{Type: Choice, Question: "q", State: State{Template: "x"},
		Options: []Option{{ID: "a", Description: "A"}, {ID: "b", Description: "B (reworded)"}}}
	if c1.Hash() == c2.Hash() {
		t.Fatal("changing an option description must change the hash")
	}
}

func TestRenderRefusesMissingRequired(t *testing.T) {
	s := noulSpec()
	if _, missing := s.Render(map[string]string{"a": "x", "b": "  "}); len(missing) != 1 || missing[0] != "b" {
		t.Fatalf("blank required input must be reported as missing, got %v", missing)
	}
	state, missing := s.Render(map[string]string{"a": "x", "b": "y"})
	if missing != nil || state != "A: x\nB: y" {
		t.Fatalf("render: state=%q missing=%v", state, missing)
	}
}

func TestCalibrationForRequiresHashAndModel(t *testing.T) {
	s := noulSpec()
	s.Calibration = []Calibration{
		{Model: "jev-1.13.0", SpecHash: s.Hash(), Method: "platt", Bias: 1, Temperature: 0.5},
		{Model: "jev-1.12.0", SpecHash: "stale-hash", Method: "platt"},
	}
	if c := s.CalibrationFor("jev-1.13.0"); c == nil || c.Bias != 1 {
		t.Fatal("matching hash+model must be found")
	}
	if s.CalibrationFor("jev-1.12.0") != nil {
		t.Fatal("a calibration with a stale hash must not be used")
	}
	if s.CalibrationFor("jev-1.14.0") != nil {
		t.Fatal("a calibration for another model must not be used")
	}
	s.Question += " edited"
	if s.CalibrationFor("jev-1.13.0") != nil {
		t.Fatal("editing the question must invalidate the calibration")
	}
}

// The same model keeps only one line. When the wording changes and a new line is drawn, the old-hash line is replaced (if it stayed, check would keep failing as stale).
// Lines for other models stay as they are.
func TestPutCalibrationReplacesStaleBlockOfSameModel(t *testing.T) {
	s := noulSpec()
	s.PutCalibration(Calibration{Model: "m", SpecHash: "old-hash", Method: "platt", Bias: 1})
	s.PutCalibration(Calibration{Model: "other", SpecHash: "old-hash", Method: "identity"})
	s.PutCalibration(Calibration{Model: "m", SpecHash: s.Hash(), Method: "platt", Bias: 2})
	if len(s.Calibration) != 2 {
		t.Fatalf("want one block per model, got %d: %+v", len(s.Calibration), s.Calibration)
	}
	got := s.CalibrationFor("m")
	if got == nil || got.SpecHash != s.Hash() || got.Bias != 2 {
		t.Fatalf("the current-hash block must win: %+v", s.Calibration)
	}
	for _, c := range s.Calibration {
		if c.Model == "other" && c.SpecHash != "old-hash" {
			t.Fatalf("other model's block must be untouched: %+v", c)
		}
	}
}

// Save also writes back variants (so proposed candidates stay in the spec). Comments and other fields are untouched.
func TestSaveWritesVariantsAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.yaml")
	src := "# head comment\nid: t\nversion: 1\ntype: noul\nquestion: Is it so?  # trailing\nstate:\n  template: \"A: {{ a }}\"\n  required: [a]\npolicy:\n  send_to: [local]\n  pii: false\nthresholds:\n  act: 0.7\n  ask: 0.5\neval:\n  min_n: 2\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Variants = []Variant{{Name: "v1", Question: "Is it really so?"}}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# head comment") || !strings.Contains(string(b), "# trailing") || !strings.Contains(string(b), "variants:") {
		t.Fatalf("save must add variants and keep comments:\n%s", b)
	}
	again, err := Load(path)
	if err != nil || len(again.Variants) != 1 || again.Variants[0].Question != "Is it really so?" {
		t.Fatalf("variants round trip: %v %+v", err, again.Variants)
	}
	again.Variants = nil
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "variants:") {
		t.Fatalf("emptying variants must remove the key:\n%s", b)
	}
}

// display holds translations for the screen. It is not part of the calibration key (Hash). Validation rejects unknown options and levels with the wrong count.
func TestDisplayTranslationsDoNotTouchHashButAreValidated(t *testing.T) {
	s := noulSpec()
	before := s.Hash()
	s.Display = map[string]Display{"ja": {Title: "同じ案件か", Options: map[string]string{"yes": "同じ", "no": "違う"}}}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid display rejected: %v", err)
	}
	if s.Hash() != before {
		t.Fatal("display must not change the calibration key")
	}
	bad := noulSpec()
	bad.Display = map[string]Display{"ja": {Options: map[string]string{"maybe": "たぶん"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("display for an option that does not exist must be rejected")
	}
	sc := noulSpec()
	sc.Type = Score
	sc.Levels = []string{"a", "b", "c"}
	sc.Thresholds.Level = 0
	sc.Display = map[string]Display{"ja": {Levels: []string{"低", "高"}}}
	if err := sc.Validate(); err == nil {
		t.Fatal("display levels with a different count must be rejected")
	}
}

func TestValidateRejectsBadSpecs(t *testing.T) {
	cases := map[string]func(*Spec){
		"no id":           func(s *Spec) { s.ID = "" },
		"no question":     func(s *Spec) { s.Question = " " },
		"bad type":        func(s *Spec) { s.Type = "boolean" },
		"no send_to":      func(s *Spec) { s.Policy.SendTo = nil },
		"bad destination": func(s *Spec) { s.Policy.SendTo = []string{"cloud"} },
		"ask above act":   func(s *Spec) { s.Thresholds = Thresholds{Act: 0.5, Ask: 0.7} },
		"choice 1 option": func(s *Spec) { s.Type = Choice; s.Options = []Option{{ID: "a", Description: "x"}} },
		"option no desc":  func(s *Spec) { s.Type = Choice; s.Options = []Option{{ID: "a", Description: "x"}, {ID: "b"}} },
		"score 1 level":   func(s *Spec) { s.Type = Score; s.Levels = []string{"only"} },
		"score 11 levels": func(s *Spec) { s.Type = Score; s.Levels = strings.Split("a b c d e f g h i j k", " ") },
		"level on noul":   func(s *Spec) { s.Thresholds.Level = 1 },
		"level too high":  func(s *Spec) { s.Type = Score; s.Levels = []string{"a", "b", "c"}; s.Thresholds.Level = 3 },
		"level zero-ish":  func(s *Spec) { s.Type = Score; s.Levels = []string{"a", "b", "c"}; s.Thresholds.Level = -1 },
		"no template":     func(s *Spec) { s.State.Template = "" },
		"no pii":          func(s *Spec) { s.Policy.PII = nil }, // no default. The author chooses true / false
		"min_n zero":      func(s *Spec) { s.Eval.MinN = 0 },    // audit S4: with 0, a line can be drawn from one label
	}
	for name, mutate := range cases {
		s := noulSpec()
		mutate(s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	if err := noulSpec().Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.yaml")
	src := `id: rt
version: 1
type: noul
question: Is it?
state:
  template: "{{ a }}"
  required: [a]
policy:
  send_to: [local]
  pii: false
thresholds:
  act: 0.7
  ask: 0.5
eval:
  labeled: x.json
  min_n: 3
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.PutCalibration(Calibration{Model: "m", SpecHash: s.Hash(), Method: "platt", Bias: 2.2, Temperature: 0.3, N: 78})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := again.CalibrationFor("m"); c == nil || c.Bias != 2.2 || c.Temperature != 0.3 {
		t.Fatalf("calibration did not survive save/load: %+v", again.Calibration)
	}
	// the same key is replaced, not added
	again.PutCalibration(Calibration{Model: "m", SpecHash: again.Hash(), Method: "platt", Bias: 9})
	if len(again.Calibration) != 1 || again.Calibration[0].Bias != 9 {
		t.Fatalf("PutCalibration must replace same (hash, model): %+v", again.Calibration)
	}
}

// Save replaces only calibration and keeps the comments and wording people wrote.
func TestSavePreservesCommentsAndOnlyTouchesCalibration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.yaml")
	src := `# Leading comment: this question was verified on 78 pairs
id: keep
version: 1
type: noul
question: Is it?   # trailing comment
state:
  template: "{{ a }}"
  required: [a]
policy:
  send_to: [local]
  pii: false
thresholds:
  act: 0.7
  ask: 0.5
eval:
  labeled: x.json
  min_n: 3
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.PutCalibration(Calibration{Model: "m", SpecHash: s.Hash(), Method: "platt", Bias: 1.5, Temperature: 0.4, N: 10})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	text := string(out)
	for _, want := range []string{"# Leading comment: this question was verified on 78 pairs", "# trailing comment", "required: [a]", "send_to: [local]", "method: platt", "bias: 1.5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved file lost %q:\n%s", want, text)
		}
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := again.CalibrationFor("m"); c == nil || c.Bias != 1.5 {
		t.Fatalf("calibration not readable after save: %+v", again.Calibration)
	}
	// the second time replaces it (the key is not added again)
	again.PutCalibration(Calibration{Model: "m", SpecHash: again.Hash(), Method: "platt", Bias: 2, Temperature: 0.4})
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ = os.ReadFile(path)
	if strings.Count(string(out), "calibration:") != 1 || !strings.Contains(string(out), "bias: 2") {
		t.Fatalf("second save must replace, not duplicate:\n%s", out)
	}
	// emptying it removes the key
	again.Calibration = nil
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ = os.ReadFile(path)
	if strings.Contains(string(out), "calibration") || !strings.Contains(string(out), "# Leading comment") {
		t.Fatalf("empty calibration must remove the key and keep comments:\n%s", out)
	}
}

func boolPtr(b bool) *bool { return &b }

// state.context is a layout that "adds context and asks again only when unsure". It uses every required input and adds one or more inputs not in template.
func TestContextTemplateMustAddAnInputAndKeepRequired(t *testing.T) {
	sp := choiceSpec()
	sp.State.Context = "Previous turn:\n{{ previous }}\n\nMessage:\n{{ message }}"
	if err := sp.Validate(); err != nil {
		t.Fatalf("a context template that adds an input must validate: %v", err)
	}
	if got := sp.State.ContextInputs(); len(got) != 1 || got[0] != "previous" {
		t.Fatalf("the added inputs are the placeholders beyond required: %v", got)
	}
	bad := map[string]string{
		"drops the required input": "Previous turn:\n{{ previous }}",
		"adds nothing":             "Message again:\n{{ message }}",
		"adds only text":           "Think carefully.\n{{ message }}",
	}
	for name, tpl := range bad {
		sp := choiceSpec()
		sp.State.Context = tpl
		if err := sp.Validate(); err == nil || !strings.Contains(err.Error(), "state.context") {
			t.Fatalf("%s must be rejected with a state.context error, got %v", name, err)
		}
	}
}

// The context question has its own hash. The minimal question's hash does not change when context is added (existing lines stay).
// One spec can hold calibration for both steps, and only lines for old hashes are replaced.
func TestContextRungHasItsOwnHashAndCalibration(t *testing.T) {
	sp := choiceSpec()
	before := sp.Hash()
	if sp.HasContext() || sp.ContextHash() != "" || sp.ContextSpec() != nil {
		t.Fatalf("a spec without state.context has no context rung")
	}
	sp.State.Context = "Previous turn:\n{{ previous }}\n\nMessage:\n{{ message }}"
	if sp.Hash() != before {
		t.Fatalf("adding state.context must not change the minimal question's hash: %s → %s", before, sp.Hash())
	}
	csp := sp.ContextSpec()
	if csp == nil || csp.Hash() != sp.ContextHash() || csp.Hash() == sp.Hash() {
		t.Fatalf("the context rung is the spec asked with the context template, under its own hash: %+v", csp)
	}
	if csp.State.Template != sp.State.Context || csp.State.Context != "" || strings.Join(csp.State.Required, ",") != "message,previous" {
		t.Fatalf("the context spec asks with the context template and requires the added inputs: %+v", csp.State)
	}
	if !sp.LiveHash(sp.Hash()) || !sp.LiveHash(sp.ContextHash()) || sp.LiveHash("stale") {
		t.Fatalf("both rungs' hashes are live; others are stale")
	}
	// WithVariant carries context over (the adopted version also asks in two steps). WithoutContext asks only the minimal question.
	v := sp.WithVariant(Variant{Name: "t", Template: "msg: {{ message }}"})
	if v.State.Context != sp.State.Context {
		t.Fatalf("a template variant must keep the context template: %+v", v.State)
	}
	if m := sp.WithoutContext(); m.HasContext() || m.Hash() != sp.Hash() {
		t.Fatalf("WithoutContext is the minimal question under the same hash: %+v", m.State)
	}
	// calibration: two step lines coexist for the same model, and the old-hash line is replaced
	sp.PutCalibration(Calibration{Model: "m", SpecHash: "stale", Method: "identity"})
	sp.PutCalibration(Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity"})
	sp.PutCalibration(Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "identity"})
	sp.PutCalibration(Calibration{Model: "other", SpecHash: sp.Hash(), Method: "identity"})
	if len(sp.Calibration) != 3 {
		t.Fatalf("one line per (model, rung), stale lines replaced: %+v", sp.Calibration)
	}
	if sp.CalibrationFor("m") == nil || sp.CalibrationFor("m").SpecHash != sp.Hash() {
		t.Fatalf("the minimal spec finds its own line")
	}
	if c := sp.ContextSpec().CalibrationFor("m"); c == nil || c.SpecHash != sp.ContextHash() {
		t.Fatalf("the context spec finds the context line")
	}
	sp.PutCalibration(Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "vector", Temperature: 1})
	if len(sp.Calibration) != 3 || sp.ContextSpec().CalibrationFor("m").Method != "vector" {
		t.Fatalf("re-fitting a rung replaces that rung's line only: %+v", sp.Calibration)
	}
}

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

// Save does not drop candidates (variants) another process added in the meantime: it merges the file's candidates with ours by name.
// Names reject separators and spaces (they go into wiki/runs file names).
func TestSaveMergesVariantsWrittenByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	src := "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := Load(path) // an old copy (e.g. background tune or calibrate --write)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(path) // the page's entry: a person adds candidate A
	if err != nil {
		t.Fatal(err)
	}
	fresh.Variants = append(fresh.Variants, Variant{Name: "a-from-page", Question: "Does it ask for something?"})
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	// the old copy adds candidate B and saves → A must not disappear
	stale.Variants = append(stale.Variants, Variant{Name: "b-from-tune", Question: "Is this a request for work?"})
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, v := range got.Variants {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "a-from-page,b-from-tune" {
		t.Fatalf("both candidates must survive, in file order then new ones: %v", names)
	}
	// on the same name ours wins (e.g. rewriting after adoption)
	got.Variants[0].Question = "Does it ask for anything?"
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(path)
	if again.Variants[0].Question != "Does it ask for anything?" || len(again.Variants) != 2 {
		t.Fatalf("same name must be replaced, not duplicated: %+v", again.Variants)
	}
	// name validation
	for _, bad := range []string{"", "x/../../outside", "with space", "a\nb", strings.Repeat("n", 61)} {
		again.Variants = []Variant{{Name: bad, Question: "q"}}
		if err := again.Validate(); err == nil {
			t.Fatalf("variant name %q must be refused", bad)
		}
	}
}

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

// Breakdown (steps): a list of yes/no questions. Each option is exactly one leaf. A middle step settles on only one of yes or no; the last step settles on both.
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

// The breakdown is part of the wording, so it goes into the key (hash). Specs without steps keep their key.
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

// Linux's exFAT and FAT drivers refuse to change a file's mode (EPERM): saving a spec (adopting a wording, tune, calibrate --write) goes
// on there, as PlaceOnce does (0.9.10 final audit, suspicion 2). Any other chmod failure still stops the save, and the spec stays as it was.
func TestSaveGoesOnWhenTheDiskRefusesModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.yaml")
	src := "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	old := chmodSaved
	t.Cleanup(func() { chmodSaved = old })
	for _, c := range []struct {
		refusal error
		saved   bool
	}{{syscall.EPERM, true}, {syscall.EIO, false}} {
		s, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		s.Variants = append(s.Variants, Variant{Name: fmt.Sprintf("v-%v", c.saved), Question: "Does it ask for something?"})
		chmodSaved = func(string, os.FileMode) error { return &os.PathError{Op: "chmod", Err: c.refusal} }
		err = s.Save()
		after, _ := os.ReadFile(path)
		if c.saved && (err != nil || string(after) == string(before)) {
			t.Fatalf("%v: the spec is saved: %v", c.refusal, err)
		}
		if !c.saved && (err == nil || string(after) != string(before)) {
			t.Fatalf("%v: the save stops and the spec stays: %v", c.refusal, err)
		}
	}
}

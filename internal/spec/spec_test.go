package spec

import (
	"os"
	"path/filepath"
	"strings"
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

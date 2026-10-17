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
		Policy:     Policy{SendTo: []string{"hosted"}, PII: "never"},
		Thresholds: Thresholds{Act: 0.7, Ask: 0.5},
	}
}

// 較正の鍵: 質問・テンプレ・選択肢が変わればハッシュが変わり、閾値・policy では変わらない。
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

// 同じモデルの線は 1 本だけ。文言を変えて引き直したら、古いハッシュの線は置き換わる(残ると check が stale で落ち続ける)。
// 別のモデルの線はそのまま残る。
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
		"bad pii":         func(s *Spec) { s.Policy.PII = "maybe" },
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
  pii: never
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
	// 同じ鍵は置き換え、増えない
	again.PutCalibration(Calibration{Model: "m", SpecHash: again.Hash(), Method: "platt", Bias: 9})
	if len(again.Calibration) != 1 || again.Calibration[0].Bias != 9 {
		t.Fatalf("PutCalibration must replace same (hash, model): %+v", again.Calibration)
	}
}

// Save は calibration だけを差し替え、人が書いたコメントと文言はそのまま残す。
func TestSavePreservesCommentsAndOnlyTouchesCalibration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.yaml")
	src := `# 先頭のコメント: この質問文は 78 ペアで検証した
id: keep
version: 1
type: noul
question: Is it?   # 行末コメント
state:
  template: "{{ a }}"
  required: [a]
policy:
  send_to: [local]
  pii: never
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
	for _, want := range []string{"# 先頭のコメント: この質問文は 78 ペアで検証した", "# 行末コメント", "required: [a]", "send_to: [local]", "method: platt", "bias: 1.5"} {
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
	// 2 回目は置き換え(キーが増えない)
	again.PutCalibration(Calibration{Model: "m", SpecHash: again.Hash(), Method: "platt", Bias: 2, Temperature: 0.4})
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ = os.ReadFile(path)
	if strings.Count(string(out), "calibration:") != 1 || !strings.Contains(string(out), "bias: 2") {
		t.Fatalf("second save must replace, not duplicate:\n%s", out)
	}
	// 空にすればキーが消える
	again.Calibration = nil
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ = os.ReadFile(path)
	if strings.Contains(string(out), "calibration") || !strings.Contains(string(out), "# 先頭のコメント") {
		t.Fatalf("empty calibration must remove the key and keep comments:\n%s", out)
	}
}

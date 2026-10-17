// Package spec は判定の仕様(Decision Spec)を読み書きする。
// 1 判定 = 1 YAML。モデル非依存の部分(質問・state・閾値)と、
// モデル依存の部分(較正)を分けて持ち、較正は (spec のハッシュ, モデル) に鍵付けする。
package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Type は質問の型。System One 形式の 3 つ。
type Type string

const (
	Noul   Type = "noul"
	Choice Type = "choice"
	Score  Type = "score"
)

// Option は choice の選択肢。説明は必須(ラベルだけだと誤ルーティングする)。
type Option struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

// State は判定に渡す文の組み立て方。Required を欠いた入力では判定しない。
type State struct {
	Template string   `yaml:"template"`
	Required []string `yaml:"required"`
	// Context は「迷った時だけ文脈を足して聞き直す」並べ方(任意)。required の入力を全部使い、template に無い入力を 1 つ以上足す。
	// 足した入力が揃っていて、最小の質問の答えが ask 帯の時だけ使う。文脈を足した聞き方は別の指紋(ContextHash)を持ち、較正も別に引く。
	Context string `yaml:"context,omitempty"`
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

// placeholders は template の中の {{ name }} を、出た順(重複なし)で返す。
func placeholders(tpl string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(tpl, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// ContextInputs は context が足す入力(required に無い placeholder)を名前順で返す。context が無ければ nil。
func (s State) ContextInputs() []string {
	if s.Context == "" {
		return nil
	}
	req := map[string]bool{}
	for _, r := range s.Required {
		req[r] = true
	}
	var out []string
	for _, p := range placeholders(s.Context) {
		if !req[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func (s State) validateContext() error {
	if s.Context == "" {
		return nil
	}
	for _, r := range s.Required {
		if !strings.Contains(s.Context, "{{ "+r+" }}") && !strings.Contains(s.Context, "{{"+r+"}}") {
			return fmt.Errorf("state.context: does not mention required input %q", r)
		}
	}
	if len(s.ContextInputs()) == 0 {
		return fmt.Errorf("state.context: must add at least one input that state.template does not use (it is the context asked for only when the minimal question is unsure)")
	}
	return nil
}

// Policy は送ってよい先。tainted セッションでは hosted が自動で外れる。
type Policy struct {
	SendTo []string `yaml:"send_to"` // "hosted" | "local"
	PII    *bool    `yaml:"pii"`     // true: 本文に個人情報が入り得る(hosted には送らない)。false: 入らない。既定は無い(書かなければ誤り)。何が個人情報かはこの道具は判断しない
}

// Thresholds は名前付きの閾値。業務コードは帯だけを見る。
// Level は score 専用: 帯を「最上位レベルの確率」ではなく P(level ≥ Level) で決める。
// 順序尺度では隣り合うレベルに確率が割れるので、最上位だけ見ると実際より弱く見える。
type Thresholds struct {
	Act   float64 `yaml:"act"`
	Ask   float64 `yaml:"ask"`
	Level int     `yaml:"level,omitempty"`
}

// Eval はラベル付きデータの所在と、較正に必要な最低件数。
type Eval struct {
	Labeled string `yaml:"labeled,omitempty"`
	MinN    int    `yaml:"min_n"`
}

// Calibration は較正コマンドが生成する。手で書かない。
// SpecHash と Model の両方が一致するものだけが使われる。
type Calibration struct {
	Model       string             `yaml:"model"`
	SpecHash    string             `yaml:"spec_hash"`
	Method      string             `yaml:"method"` // "platt"(noul) | "vector"(choice/score) | "identity"(検証済み、定数なし)
	Bias        float64            `yaml:"bias,omitempty"`
	Temperature float64            `yaml:"temperature,omitempty"`
	Biases      map[string]float64 `yaml:"biases,omitempty"`           // vector: 選択肢ごとの bias
	PrecisionAt float64            `yaml:"precision_at_act,omitempty"` // act 閾値での正答率(較正後)
	FittedOn    string             `yaml:"fitted_on,omitempty"`
	N           int                `yaml:"n"`
	Brier       float64            `yaml:"brier"`
	ECE         float64            `yaml:"ece"`
	FittedAt    string             `yaml:"fitted_at"`
}

// Spec は 1 判定の仕様。
type Spec struct {
	ID          string        `yaml:"id"`
	Version     int           `yaml:"version"`
	Type        Type          `yaml:"type"`
	Question    string        `yaml:"question"`
	Options     []Option      `yaml:"options,omitempty"` // choice
	Levels      []string      `yaml:"levels,omitempty"`  // score(低い順)
	State       State         `yaml:"state"`
	Policy      Policy        `yaml:"policy"`
	Thresholds  Thresholds    `yaml:"thresholds"`
	Eval        Eval          `yaml:"eval"`
	Calibration []Calibration `yaml:"calibration,omitempty"`
	// Display は画面の表示用の訳(言語コード → 題名・選択肢・段階)。モデルには送らず、較正の鍵にも入らない。
	Display map[string]Display `yaml:"display,omitempty"`
	// Variants は聞き方の候補(`shikii tune` が正解の貯金で採点する)。鍵には入らない。採用されたものは新しい版として別ファイルになる。
	Variants []Variant `yaml:"variants,omitempty"`
	Steps    []Step    `yaml:"steps,omitempty"` // choice の分解: はい/いいえ の列。あれば多択を 1 つ聞く代わりに全段を 1 回で聞き、枝の積で分布を作る

	path string
}

// Variant は聞き方の候補。書いた項目だけを上書きする(無い項目は今の spec のまま)。
type Variant struct {
	Name     string            `yaml:"name"`
	Question string            `yaml:"question,omitempty"`
	Options  map[string]string `yaml:"options,omitempty"`  // choice: id → 説明の言い換え
	Levels   []string          `yaml:"levels,omitempty"`   // score: 段階の言い換え(数は同じ)
	Template string            `yaml:"template,omitempty"` // state の並べ方
	Steps    []Step            `yaml:"steps,omitempty"`    // choice: 多択を はい/いいえ の列に分解する(空なら今のまま)
}

// Step は分解の 1 段: はい/いいえ の質問と、どちらの答えでどの選択肢に確定するか。
// 途中の段は yes か no の片方だけで確定し(もう一方は次の段へ)、最後の段は両方で確定する。
// 各選択肢はちょうど 1 つの葉に現れる。分解は聞き方なので鍵(hash)に入る。
type Step struct {
	Ask string `yaml:"ask"`
	Yes string `yaml:"yes,omitempty"`
	No  string `yaml:"no,omitempty"`
}

// validateSteps は分解の形を確かめる。
func validateSteps(typ Type, options []Option, steps []Step) error {
	if len(steps) == 0 {
		return nil
	}
	if typ != Choice {
		return fmt.Errorf("steps: only a choice question can be decomposed into yes/no steps")
	}
	known := map[string]bool{}
	for _, o := range options {
		known[o.ID] = true
	}
	seen := map[string]int{}
	leaf := func(i int, id string) error {
		if !known[id] {
			return fmt.Errorf("steps[%d]: %q is not an option of this spec", i, id)
		}
		seen[id]++
		return nil
	}
	for i, st := range steps {
		if strings.TrimSpace(st.Ask) == "" {
			return fmt.Errorf("steps[%d]: ask is required", i)
		}
		last := i == len(steps)-1
		switch {
		case st.Yes != "" && st.No != "":
			if !last {
				return fmt.Errorf("steps[%d]: only the last step may settle both yes and no; a middle step settles one and passes the other on", i)
			}
		case st.Yes == "" && st.No == "":
			return fmt.Errorf("steps[%d]: a step must settle yes, no or (last step) both", i)
		case last:
			return fmt.Errorf("steps[%d]: the last step must settle both yes and no", i)
		}
		for _, id := range []string{st.Yes, st.No} {
			if id != "" {
				if err := leaf(i, id); err != nil {
					return err
				}
			}
		}
	}
	for _, o := range options {
		if seen[o.ID] != 1 {
			return fmt.Errorf("steps: option %q must appear exactly once as a leaf, appears %d times", o.ID, seen[o.ID])
		}
	}
	return nil
}

func copySteps(in []Step) []Step {
	if len(in) == 0 {
		return nil
	}
	return append([]Step(nil), in...)
}

// WithVariant は候補を当てた写し(path は無し。鍵は変わる)。
func (s *Spec) WithVariant(v Variant) *Spec {
	c := *s
	c.Calibration = nil
	c.Variants = nil
	c.path = ""
	if v.Question != "" {
		c.Question = v.Question
	}
	if v.Template != "" {
		c.State = State{Template: v.Template, Required: append([]string(nil), s.State.Required...), Context: s.State.Context}
	}
	if len(v.Options) > 0 {
		c.Options = make([]Option, len(s.Options))
		for i, o := range s.Options {
			if d, ok := v.Options[o.ID]; ok {
				o.Description = d
			}
			c.Options[i] = o
		}
	}
	if len(v.Levels) > 0 {
		c.Levels = append([]string(nil), v.Levels...)
	}
	if len(v.Steps) > 0 {
		c.Steps = copySteps(v.Steps)
	} else {
		c.Steps = copySteps(s.Steps)
	}
	return &c
}

// Display は 1 言語ぶんの表示用の訳。Options は choice の id(noul なら yes / no)→ 表示名、Levels は score の段階(低い順、数は levels と同じ)。
type Display struct {
	Title    string            `yaml:"title,omitempty"`
	Question string            `yaml:"question,omitempty"`
	Options  map[string]string `yaml:"options,omitempty"`
	Levels   []string          `yaml:"levels,omitempty"`
}

// Load は YAML を読み、検証する。
func Load(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Spec
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.path = path
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// Validate は仕様の整合を確かめる。
func (s *Spec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(s.Question) == "" {
		return fmt.Errorf("question is required")
	}
	switch s.Type {
	case Noul:
	case Choice:
		if len(s.Options) < 2 {
			return fmt.Errorf("choice needs at least 2 options")
		}
		for _, o := range s.Options {
			if o.ID == "" || strings.TrimSpace(o.Description) == "" {
				return fmt.Errorf("choice option %q needs id and description", o.ID)
			}
		}
	case Score:
		if len(s.Levels) < 2 || len(s.Levels) > 10 {
			return fmt.Errorf("score needs 2..10 levels, got %d", len(s.Levels))
		}
	default:
		return fmt.Errorf("unknown type %q", s.Type)
	}
	if err := validateSteps(s.Type, s.Options, s.Steps); err != nil {
		return err
	}
	if strings.TrimSpace(s.State.Template) == "" {
		return fmt.Errorf("state.template is required")
	}
	if err := s.State.validateContext(); err != nil {
		return err
	}
	if len(s.Policy.SendTo) == 0 {
		return fmt.Errorf("policy.send_to is required")
	}
	for _, d := range s.Policy.SendTo {
		if d != "hosted" && d != "local" {
			return fmt.Errorf("policy.send_to: unknown destination %q", d)
		}
	}
	if s.Eval.MinN < 1 {
		return fmt.Errorf("eval.min_n must be at least 1 (how many truths before a calibration may be written; 50 is a sensible start)")
	}
	if s.Policy.PII == nil {
		return fmt.Errorf("policy.pii is required: true (the text can contain personal information: never sent to a hosted endpoint) or false (it cannot). shikii does not decide what counts as personal information; you do")
	}
	if !(0 < s.Thresholds.Ask && s.Thresholds.Ask <= s.Thresholds.Act && s.Thresholds.Act <= 1) {
		return fmt.Errorf("thresholds must satisfy 0 < ask <= act <= 1")
	}
	if s.Thresholds.Level != 0 {
		if s.Type != Score {
			return fmt.Errorf("thresholds.level is only for score")
		}
		if s.Thresholds.Level < 1 || s.Thresholds.Level > len(s.Levels)-1 {
			return fmt.Errorf("thresholds.level must be in 1..%d", len(s.Levels)-1)
		}
	}
	for lang, d := range s.Display {
		for id := range d.Options {
			if !s.hasTruthID(id) {
				return fmt.Errorf("display.%s.options: %q is not an option of this spec", lang, id)
			}
		}
		if len(d.Levels) > 0 && len(d.Levels) != len(s.Levels) {
			return fmt.Errorf("display.%s.levels: %d entries, spec has %d levels", lang, len(d.Levels), len(s.Levels))
		}
	}
	names := map[string]bool{}
	for i, v := range s.Variants {
		if strings.TrimSpace(v.Name) == "" || names[v.Name] || v.Name == "current" {
			return fmt.Errorf("variants[%d]: name must be unique, non-empty and not \"current\"", i)
		}
		names[v.Name] = true
		if v.Question == "" && len(v.Options) == 0 && len(v.Levels) == 0 && v.Template == "" && len(v.Steps) == 0 {
			return fmt.Errorf("variants[%d] (%s): changes nothing", i, v.Name)
		}
		if err := validateSteps(s.Type, s.Options, v.Steps); err != nil {
			return fmt.Errorf("variants[%d] (%s).%w", i, v.Name, err)
		}
		for id := range v.Options {
			if s.Type != Choice || !s.hasTruthID(id) {
				return fmt.Errorf("variants[%d] (%s).options: %q is not an option of this spec", i, v.Name, id)
			}
		}
		if len(v.Levels) > 0 && len(v.Levels) != len(s.Levels) {
			return fmt.Errorf("variants[%d] (%s).levels: %d entries, spec has %d levels", i, v.Name, len(v.Levels), len(s.Levels))
		}
		if v.Template != "" {
			for _, r := range s.State.Required {
				if !strings.Contains(v.Template, "{{ "+r+" }}") && !strings.Contains(v.Template, "{{"+r+"}}") {
					return fmt.Errorf("variants[%d] (%s).template: does not mention required input %q", i, v.Name, r)
				}
			}
		}
	}
	return nil
}

// hasTruthID は表示用の訳が指す id が spec にあるか(choice は option の id、noul は yes / no)。
// PIISensitive は本文に個人情報が入り得ると宣言されているか(hosted に送らない)。
func (s *Spec) PIISensitive() bool { return s.Policy.PII != nil && *s.Policy.PII }

func (s *Spec) hasTruthID(id string) bool {
	switch s.Type {
	case Noul:
		return id == "yes" || id == "no"
	case Choice:
		for _, o := range s.Options {
			if o.ID == id {
				return true
			}
		}
	}
	return false
}

// Hash は較正を鍵付けするハッシュ。質問・型・state テンプレ・選択肢・レベルから作る。
// 閾値や policy は含めない(動かしても較正は有効)。
func (s *Spec) Hash() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	// thresholds.level は帯の決め方で、較正の鍵には入れない(閾値と同じ扱い)
	w(string(s.Type), s.Question, s.State.Template)
	for _, o := range s.Options {
		w(o.ID, o.Description)
	}
	for _, l := range s.Levels {
		w(l)
	}
	for _, st := range s.Steps { // 分解は聞き方。無い spec の鍵は変わらない
		w("step", st.Ask, st.Yes, st.No)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Render はテンプレに入力を埋める。欠けている required の名前を返す。
func (s *Spec) Render(inputs map[string]string) (state string, missing []string) {
	for _, r := range s.State.Required {
		if strings.TrimSpace(inputs[r]) == "" {
			missing = append(missing, r)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return "", missing
	}
	state = s.State.Template
	for k, v := range inputs {
		state = strings.ReplaceAll(state, "{{ "+k+" }}", v)
		state = strings.ReplaceAll(state, "{{"+k+"}}", v)
	}
	return state, nil
}

// CalibrationFor は現在のハッシュとモデルに一致する較正を返す。無ければ nil。
func (s *Spec) CalibrationFor(model string) *Calibration {
	h := s.Hash()
	for i := range s.Calibration {
		c := &s.Calibration[i]
		if c.Model == model && c.SpecHash == h {
			return c
		}
	}
	return nil
}

// PutCalibration は同じ (hash, model) の較正を置き換え、無ければ追加する。
func (s *Spec) PutCalibration(c Calibration) {
	// 同じモデルの線は段ごとに 1 本(最小の質問と、あれば文脈の段)。古いハッシュの線(文言を変える前のもの)は置き換える。別のモデルの線は残す。
	kept := make([]Calibration, 0, len(s.Calibration)+1) // 写し(ContextSpec)と配列を共有しているので、その場で書き換えない
	for _, cur := range s.Calibration {
		if cur.Model != c.Model || (cur.SpecHash != c.SpecHash && s.LiveHash(cur.SpecHash)) {
			kept = append(kept, cur)
		}
	}
	s.Calibration = append(kept, c)
}

// HasContext は「迷った時だけ文脈を足す」段があるか。
func (s *Spec) HasContext() bool { return s.State.Context != "" }

// ContextSpec は文脈を足した聞き方そのもの: template を context に替え、足した入力を required にした写し。
// 指紋(Hash)は別になるので、較正も別に引かれる。較正の一覧とファイルの場所は共有する。context が無ければ nil。
func (s *Spec) ContextSpec() *Spec {
	if !s.HasContext() {
		return nil
	}
	c := *s
	c.State = State{Template: s.State.Context, Required: append(append([]string(nil), s.State.Required...), s.State.ContextInputs()...)}
	c.Variants = nil
	c.Steps = copySteps(s.Steps)
	return &c
}

// ContextHash は文脈の段の指紋。無ければ空。
func (s *Spec) ContextHash() string {
	if c := s.ContextSpec(); c != nil {
		return c.Hash()
	}
	return ""
}

// LiveHash は h が今の聞き方のどちらかの段の指紋か(それ以外は文言を変える前の古い指紋)。
func (s *Spec) LiveHash(h string) bool {
	return h == s.Hash() || (s.HasContext() && h == s.ContextHash())
}

// WithoutContext は最小の質問だけで聞く写し(指紋は同じ)。tune の採点や聞き直しのように、段を上がらずに測りたい時に使う。
func (s *Spec) WithoutContext() *Spec {
	c := *s
	c.State.Context = ""
	return &c
}

// Save は読み込んだパスに書き戻す。書き換えるのは `calibration` キーだけで、
// それ以外の文言・コメント・並びはファイルのまま残す(較正ブロックは生成物、他は人が書くもの)。
func (s *Spec) Save() error {
	if s.path == "" {
		return fmt.Errorf("spec has no path")
	}
	src, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: top level must be a mapping", s.path)
	}
	root := doc.Content[0]
	// 書き戻すのは生成物の 2 つだけ(較正ブロックと、tune が足す候補)。文言・コメント・他の項目は触らない。
	if err := setMappingKey(root, "calibration", s.Calibration, len(s.Calibration) == 0, "較正ブロックは `shikii calibrate --write` が生成する。手で書かない。"); err != nil {
		return err
	}
	if variantsChanged(root, s.Variants) { // 触っていなければ再エンコードしない(人のコメントが残る)
		if err := setMappingKey(root, "variants", s.Variants, len(s.Variants) == 0, "聞き方の候補。`shikii tune` が採点する(--propose で足される)。鍵には入らない。"); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	// 同じフォルダに書いてから rename(途中で落ちても元の spec は壊れない。serve が毎回読み直すため)
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// setMappingKey は root の key を value で置き換える(無ければ末尾に足し、empty なら消す)。
// variantsChanged はファイルの variants と今の値が違うか。
func variantsChanged(root *yaml.Node, now []Variant) bool {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "variants" {
			var onDisk []Variant
			if err := root.Content[i+1].Decode(&onDisk); err != nil {
				return true
			}
			return !reflect.DeepEqual(onDisk, now)
		}
	}
	return len(now) > 0
}

func setMappingKey(root *yaml.Node, key string, value any, empty bool, headComment string) error {
	at := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			at = i
			break
		}
	}
	switch {
	case empty && at >= 0:
		root.Content = append(root.Content[:at], root.Content[at+2:]...)
	case empty:
	default:
		var val yaml.Node
		if err := val.Encode(value); err != nil {
			return err
		}
		if at >= 0 {
			root.Content[at+1] = &val
		} else {
			k := &yaml.Node{Kind: yaml.ScalarNode, Value: key, HeadComment: headComment}
			root.Content = append(root.Content, k, &val)
		}
	}
	return nil
}

// Path は読み込み元。
func (s *Spec) Path() string { return s.path }

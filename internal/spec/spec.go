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
}

// Policy は送ってよい先。tainted セッションでは hosted が自動で外れる。
type Policy struct {
	SendTo []string `yaml:"send_to"` // "hosted" | "local"
	PII    string   `yaml:"pii"`     // "never"(本文を残してよく外にも送れる) | "possible"(本文を残さない) | "local"(本文を手元にだけ残し、外には送らない)
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
	Labeled string `yaml:"labeled"`
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

	path string
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
	if strings.TrimSpace(s.State.Template) == "" {
		return fmt.Errorf("state.template is required")
	}
	if len(s.Policy.SendTo) == 0 {
		return fmt.Errorf("policy.send_to is required")
	}
	for _, d := range s.Policy.SendTo {
		if d != "hosted" && d != "local" {
			return fmt.Errorf("policy.send_to: unknown destination %q", d)
		}
	}
	switch s.Policy.PII {
	case "", "never", "possible", "local":
	default:
		return fmt.Errorf("policy.pii: unknown value %q (never | possible | local)", s.Policy.PII)
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
	return nil
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
	// 同じモデルの線は 1 本だけ。古いハッシュの線(文言を変える前のもの)は置き換える。別のモデルの線は残す。
	kept := s.Calibration[:0]
	replaced := false
	for _, cur := range s.Calibration {
		if cur.Model != c.Model {
			kept = append(kept, cur)
			continue
		}
		if !replaced {
			kept = append(kept, c)
			replaced = true
		}
	}
	if !replaced {
		kept = append(kept, c)
	}
	s.Calibration = kept
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

	// 既存の calibration キーを探す
	at := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "calibration" {
			at = i
			break
		}
	}
	switch {
	case len(s.Calibration) == 0 && at >= 0:
		root.Content = append(root.Content[:at], root.Content[at+2:]...)
	case len(s.Calibration) == 0:
		// 何も書くものがない
	default:
		var val yaml.Node
		if err := val.Encode(s.Calibration); err != nil {
			return err
		}
		if at >= 0 {
			root.Content[at+1] = &val
		} else {
			key := &yaml.Node{Kind: yaml.ScalarNode, Value: "calibration",
				HeadComment: "較正ブロックは `shikii calibrate --write` が生成する。手で書かない。"}
			root.Content = append(root.Content, key, &val)
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
	return os.WriteFile(s.path, buf.Bytes(), 0o644)
}

// Path は読み込み元。
func (s *Spec) Path() string { return s.path }

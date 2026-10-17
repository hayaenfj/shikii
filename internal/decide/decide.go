// Package decide は仕様 + 入力 → 帯(act / ask / discard / abstain)。
// エンドポイントの選択・送信可否の統制・較正の適用をここで行い、業務コードには帯だけを渡す。
package decide

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// Band は判定の帯。
type Band string

const (
	Act     Band = "act"     // 自動で実行してよい
	Ask     Band = "ask"     // 利用者に確認
	Discard Band = "discard" // 捨てる
	Abstain Band = "abstain" // 判定していない(入力不足・送れる先が無い)
)

// Session は呼び出し側の状態。Tainted なら hosted に送らない。
type Session struct {
	Tainted bool
}

// Endpoint は送り先。Kind は spec の policy.send_to と対応する。
type Endpoint struct {
	Kind   string // "hosted" | "local"
	Client *systemone.Client
}

// Decision は 1 回の判定の結果と来歴。
type Decision struct {
	TraceID      string             `json:"trace_id"`
	SpecID       string             `json:"spec_id"`
	SpecHash     string             `json:"spec_hash"`
	Type         string             `json:"type"`
	InputsHash   string             `json:"inputs_hash"`
	Model        string             `json:"model,omitempty"`
	Endpoint     string             `json:"endpoint,omitempty"`
	Raw          map[string]float64 `json:"raw,omitempty"`
	Calibrated   map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated bool               `json:"is_calibrated"`
	Top          string             `json:"top,omitempty"`
	P            float64            `json:"p"`
	Score        float64            `json:"score,omitempty"`
	Confidence   float64            `json:"confidence,omitempty"`
	Band         Band               `json:"band"`
	Reason       string             `json:"reason,omitempty"`
	Tainted      bool               `json:"tainted,omitempty"` // 本番データを読んだセッションの判定。記録に残り、聞き直しでも hosted に送らない
	Rung         string             `json:"rung,omitempty"`    // どの段で決まったか: minimal(最小の質問) | context(迷ったので文脈を足して聞き直した)
	Minimal      *store.Pass        `json:"minimal,omitempty"` // 文脈を足した時の、最小の質問の答え(その段の指紋と raw)。人の正解はこちらにも付く
	At           time.Time          `json:"at"`
}

// questionKey は 1 spec = 1 質問なので固定。
const questionKey = "q"

// BuildQuestion は spec を System One の質問に変換する。choice の選択肢は spec の並びで送る。
func BuildQuestion(sp *spec.Spec) systemone.Question {
	return BuildQuestionOrdered(sp, nil)
}

// BuildQuestionOrdered は choice の選択肢を order(spec.Options の添字の並び)で送る。
// nil なら spec の並び。順序バイアスの測定(shikii bias)が順列を回すのに使う。
func BuildQuestionOrdered(sp *spec.Spec, order []int) systemone.Question {
	q := systemone.Question{Type: string(sp.Type), Instructions: sp.Question}
	switch sp.Type {
	case spec.Choice:
		oc := make(systemone.OrderedCriteria, 0, len(sp.Options))
		if order == nil {
			for _, o := range sp.Options {
				oc = append(oc, systemone.Criterion{ID: o.ID, Description: o.Description})
			}
		} else {
			for _, i := range order {
				o := sp.Options[i]
				oc = append(oc, systemone.Criterion{ID: o.ID, Description: o.Description})
			}
		}
		q.Criteria = oc
	case spec.Score:
		q.Criteria = sp.Levels
	}
	return q
}

// AskOrdered は選択肢の並びを指定して 1 回聞き、生の分布を返す(較正も記録もしない)。順序バイアスの測定用。
func AskOrdered(ctx context.Context, sp *spec.Spec, state string, order []int, c *systemone.Client) (map[string]float64, string, error) {
	resp, err := c.Ask(ctx, state, map[string]systemone.Question{questionKey: BuildQuestionOrdered(sp, order)})
	if err != nil {
		return nil, "", err
	}
	ans, ok := resp.Answers[questionKey]
	if !ok {
		return nil, "", fmt.Errorf("response has no answer for %q", questionKey)
	}
	return rawDistribution(sp, ans), resp.Model, nil
}

// PickEndpoint は policy.send_to の順に、送ってよい最初のエンドポイントを選ぶ。
// tainted セッションでは hosted を飛ばす。無ければ nil。
func PickEndpoint(sp *spec.Spec, sess Session, eps []Endpoint) *Endpoint {
	for _, kind := range sp.Policy.SendTo {
		// tainted なセッションと、個人情報が入り得ると宣言された spec(pii: true)は、hosted を使わない
		if kind == "hosted" && (sess.Tainted || sp.PIISensitive()) {
			continue
		}
		for i := range eps {
			if eps[i].Kind == kind && eps[i].Client != nil {
				return &eps[i]
			}
		}
	}
	return nil
}

// Decide は 1 回の判定。モデル呼び出しが失敗した時だけ error を返す。
// 判定しないと決めた場合は error ではなく Abstain と理由を返す。
func Decide(ctx context.Context, sp *spec.Spec, inputs map[string]string, sess Session, eps []Endpoint) (*Decision, error) {
	d := &Decision{
		TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type),
		InputsHash: store.HashInputs(inputs), At: time.Now(), Tainted: sess.Tainted,
	}

	state, missing := sp.Render(inputs)
	if len(missing) > 0 {
		d.Band = Abstain
		d.Reason = "missing required input: " + strings.Join(missing, ", ")
		return d, nil
	}

	ep := PickEndpoint(sp, sess, eps)
	if ep == nil {
		d.Band = Abstain
		d.Reason = fmt.Sprintf("no permitted endpoint (send_to=%v, tainted=%v, pii=%v)", sp.Policy.SendTo, sess.Tainted, sp.PIISensitive())
		return d, nil
	}
	d.Endpoint = ep.Kind

	// 第 1 段: 最小の質問
	d.Rung = "minimal"
	if err := askOnce(ctx, sp, d, state, ep); err != nil {
		return nil, err
	}
	// 第 2 段: 迷った(ask)時だけ、文脈の入力が揃っていれば文脈を足して聞き直す。
	// 文脈を足した聞き方は別の指紋なので、答えはその指紋で記録され、較正もその指紋の線で掛かる。最小の質問の答えは Minimal に残す。
	csp := sp.ContextSpec()
	if !unsure(sp, d.Band) || csp == nil {
		return d, nil
	}
	cstate, missing := csp.Render(inputs)
	if len(missing) > 0 {
		return d, nil // 足す文脈が無い: ここで止まる(人へ)
	}
	d.Minimal = &store.Pass{SpecHash: d.SpecHash, Raw: d.Raw, Calibrated: d.Calibrated, IsCalibrated: d.IsCalibrated, Top: d.Top, P: d.P, Band: string(d.Band)}
	d.SpecHash = csp.Hash()
	if err := askOnce(ctx, csp, d, cstate, ep); err != nil {
		return nil, err
	}
	d.Rung = "context"
	return d, nil
}

// askOnce は 1 つの段を聞く: 多択(または分解した はい/いいえ の列)を 1 回の呼び出しで聞き、raw・較正・top・帯を d に書く。
func askOnce(ctx context.Context, sp *spec.Spec, d *Decision, state string, ep *Endpoint) error {
	if len(sp.Steps) > 0 {
		return decideBySteps(ctx, sp, d, state, ep)
	}
	resp, err := ep.Client.Ask(ctx, state, map[string]systemone.Question{questionKey: BuildQuestion(sp)})
	if err != nil {
		return err
	}
	ans, ok := resp.Answers[questionKey]
	if !ok {
		return fmt.Errorf("response has no answer for %q", questionKey)
	}
	// 較正の鍵には実際に答えたモデルを使う。"jev-latest" が別版に切り替わったら
	// 較正は失効し、act が出なくなる(それが望む挙動)。
	d.Model = resp.Model
	if d.Model == "" {
		d.Model = ep.Client.Model
	}

	d.Raw = rawDistribution(sp, ans)
	d.Calibrated, d.IsCalibrated = applyCalibration(sp, d.Model, d.Raw)

	switch sp.Type {
	case spec.Noul:
		d.Top = "yes"
		d.P = d.Calibrated["yes"]
		d.Confidence = abs(2*d.P - 1)
	case spec.Choice:
		d.Top, d.P = argmax(d.Calibrated)
		d.Confidence = ans.Confidence
	case spec.Score:
		d.Top, d.P = argmax(d.Calibrated)
		d.Score = expectedLevel(d.Calibrated)
		d.Confidence = ans.Confidence
		if sp.Thresholds.Level > 0 {
			// 順序尺度: 帯は P(level ≥ k)。隣接レベルに割れた確率を合算する
			d.P = MassAtOrAbove(d.Calibrated, sp.Thresholds.Level)
		}
	}
	d.Band = band(sp.Thresholds, d.P, d.IsCalibrated)
	return nil
}

// unsure は「迷った」か: ask 帯はいつでも迷い。一番高い選択肢で帯を決める型(choice と、level 無しの score)では
// discard(どの選択肢も ask に届かない)も迷い。noul と level 付きの score の discard は確信した「いいえ / 低い」なので迷いではない。
func unsure(sp *spec.Spec, b Band) bool {
	if b == Ask {
		return true
	}
	argmaxBand := sp.Type == spec.Choice || (sp.Type == spec.Score && sp.Thresholds.Level == 0)
	return b == Discard && argmaxBand
}

// StepQuestions は分解の全段を 1 回の呼び出しに載せる質問の集まり(鍵は step-0, step-1, …)。
func StepQuestions(sp *spec.Spec) map[string]systemone.Question {
	qs := make(map[string]systemone.Question, len(sp.Steps))
	for i, st := range sp.Steps {
		qs[stepKey(i)] = systemone.Question{Type: string(spec.Noul), Instructions: st.Ask}
	}
	return qs
}

func stepKey(i int) string { return fmt.Sprintf("step-%d", i) }

// StepDistribution は各段の P(yes) から元の選択肢の分布を作る: 枝の確率の積。
// 途中の段で確定する選択肢は「そこまでの残り × その枝」、残りは次の段へ。
func StepDistribution(sp *spec.Spec, pYes []float64) map[string]float64 {
	dist := make(map[string]float64, len(sp.Options))
	for _, o := range sp.Options {
		dist[o.ID] = 0
	}
	mass := 1.0
	for i, st := range sp.Steps {
		p := pYes[i]
		switch {
		case st.Yes != "" && st.No != "":
			dist[st.Yes] += mass * p
			dist[st.No] += mass * (1 - p)
			mass = 0
		case st.Yes != "":
			dist[st.Yes] += mass * p
			mass *= 1 - p
		default:
			dist[st.No] += mass * (1 - p)
			mass *= p
		}
	}
	return dist
}

// decideBySteps は分解した choice を 1 回の呼び出しで聞き、積の分布を多択の答えと同じ形にする(較正・帯・記録は共通)。
func decideBySteps(ctx context.Context, sp *spec.Spec, d *Decision, state string, ep *Endpoint) error {
	resp, err := ep.Client.Ask(ctx, state, StepQuestions(sp))
	if err != nil {
		return err
	}
	pYes := make([]float64, len(sp.Steps))
	for i := range sp.Steps {
		ans, ok := resp.Answers[stepKey(i)]
		if !ok {
			return fmt.Errorf("response has no answer for %q", stepKey(i))
		}
		pYes[i] = ans.Noul
	}
	d.Model = resp.Model
	if d.Model == "" {
		d.Model = ep.Client.Model
	}
	d.Raw = StepDistribution(sp, pYes)
	d.Calibrated, d.IsCalibrated = applyCalibration(sp, d.Model, d.Raw)
	d.Top, d.P = argmax(d.Calibrated)
	d.Confidence = abs(2*d.P - 1)
	d.Band = band(sp.Thresholds, d.P, d.IsCalibrated)
	return nil
}

// band は閾値と較正の有無から帯を決める。較正の無い判定は act を出さない。
func band(t spec.Thresholds, p float64, calibrated bool) Band {
	switch {
	case p >= t.Act && calibrated:
		return Act
	case p >= t.Ask:
		return Ask
	default:
		return Discard
	}
}

func rawDistribution(sp *spec.Spec, ans systemone.Answer) map[string]float64 {
	switch sp.Type {
	case spec.Noul:
		return map[string]float64{"yes": ans.Noul, "no": 1 - ans.Noul}
	default:
		out := make(map[string]float64, len(ans.Probabilities))
		for k, v := range ans.Probabilities {
			out[k] = v
		}
		return out
	}
}

// applyCalibration は (hash, model) に一致する較正があれば当てる。
//   - "identity": 定数なし。較正済みモデルの出力をそのまま信用してよいと検証した印(設計 §7.1)
//   - "platt":    noul のみ。σ((Δ−bias)/T)
//   - "vector":   choice/score。softmax((log p_k + b_k)/T)
func applyCalibration(sp *spec.Spec, model string, raw map[string]float64) (map[string]float64, bool) {
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	c := sp.CalibrationFor(model)
	if c == nil {
		return out, false
	}
	if c.Method == "identity" {
		return out, true
	}
	if c.Method == "vector" && sp.Type != spec.Noul && c.Temperature > 0 {
		return calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(raw), true
	}
	if sp.Type != spec.Noul || c.Method != "platt" || c.Temperature <= 0 {
		return out, false
	}
	p := calib.Platt{Bias: c.Bias, Temperature: c.Temperature}.Apply(calib.Logit(raw["yes"]))
	out["yes"], out["no"] = p, 1-p
	return out, true
}

func argmax(m map[string]float64) (string, float64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 同点時の決定性
	best, bp := "", -1.0
	for _, k := range keys {
		if m[k] > bp {
			best, bp = k, m[k]
		}
	}
	return best, bp
}

// MassAtOrAbove は score の分布で level ≥ k の確率の和。キーはレベル番号("0".."n-1")。
func MassAtOrAbove(m map[string]float64, k int) float64 {
	// 加算は鍵の順に固定する(map の走査順は毎回変わり、閾値ちょうどで帯が揺れる)
	var s float64
	for _, key := range sortedKeys(m) {
		if i, err := strconv.Atoi(key); err == nil && i >= k {
			s += m[key]
		}
	}
	return s
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// expectedLevel は score の期待値 Σ(level × p)。キーはレベル番号("0".."n-1")。
func expectedLevel(m map[string]float64) float64 {
	var e float64
	for _, k := range sortedKeys(m) {
		if i, err := strconv.Atoi(k); err == nil {
			e += float64(i) * m[k]
		}
	}
	return e
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

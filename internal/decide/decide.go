// Package decide turns spec + input → band (act / ask / discard / abstain).
// It picks the endpoint, controls whether text may be sent, and applies calibration here, handing business code only the band.
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

// Band is the band of a decision.
type Band string

const (
	Act     Band = "act"     // may run automatically
	Ask     Band = "ask"     // confirm with the user
	Discard Band = "discard" // drop it
	Abstain Band = "abstain" // not decided (missing input, or nowhere it may be sent)
)

// Session is the caller's state. When Tainted, nothing is sent to hosted.
type Session struct {
	Tainted bool
}

// Endpoint is a destination. Kind matches the spec's policy.send_to.
type Endpoint struct {
	Kind   string // "hosted" | "local"
	Client *systemone.Client
}

// Decision is the result and provenance of one decision.
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
	Tainted      bool               `json:"tainted,omitempty"` // a decision from a session that read production data. Recorded, and never sent to hosted even when re-asked
	Rung         string             `json:"rung,omitempty"`    // which step decided it: minimal (the minimal question) | context (unsure, so context was added and asked again)
	Minimal      *store.Pass        `json:"minimal,omitempty"` // when context was added, the minimal question's answer (that step's hash and raw). A person's truth is attached here too
	Receipt      *Receipt           `json:"receipt,omitempty"` // the receipt attached to an act decision (what it relied on to say it may act). The caller (cmd) attaches it after looking at the store
	At           time.Time          `json:"at"`
}

// Receipt is the receipt of an act decision: the line's key and method, the date it was drawn, the number of truths (from people), and the number and precision of act decisions a person checked.
// It exists to say "people checked n decisions of this kind and precision was x" instead of "95% confident". No line, no receipt.
type Receipt struct {
	SpecHash        string  `json:"spec_hash"`
	Model           string  `json:"model"`
	Method          string  `json:"method"`
	FittedAt        string  `json:"fitted_at"`
	N               int     `json:"n"`
	NHuman          int     `json:"n_human"`
	PrecisionAtFit  float64 `json:"precision_at_fit"`
	HumanCheckedAct int     `json:"human_checked_act"` // number of decisions that become act with the line applied and that a person (or the outcome) checked
	HumanPrecision  float64 `json:"human_precision"`   // their precision (0 when there are none, meaning nobody has checked yet)
}

// questionKey is fixed because 1 spec = 1 question.
const questionKey = "q"

// BuildQuestion converts a spec into a System One question. Choice options are sent in spec order.
func BuildQuestion(sp *spec.Spec) systemone.Question {
	return BuildQuestionOrdered(sp, nil)
}

// BuildQuestionOrdered sends the choice options in order (a list of indexes into spec.Options).
// nil means spec order. The order-bias measurement (shikii bias) uses it to cycle through permutations.
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

// AskOrdered asks once with the given option order and returns the raw distribution (no calibration, no record). For measuring order bias.
func AskOrdered(ctx context.Context, sp *spec.Spec, state string, order []int, c *systemone.Client) (map[string]float64, string, error) {
	resp, err := c.AskLayout(ctx, state, map[string]systemone.Question{questionKey: BuildQuestionOrdered(sp, order)}, sp.Layout)
	if err != nil {
		return nil, "", err
	}
	ans, ok := resp.Answers[questionKey]
	if !ok {
		return nil, "", fmt.Errorf("response has no answer for %q", questionKey)
	}
	return rawDistribution(sp, ans), resp.Model, nil
}

// PickEndpoint picks the first endpoint text may be sent to, in policy.send_to order.
// In a tainted session hosted is skipped. nil if none.
func PickEndpoint(sp *spec.Spec, sess Session, eps []Endpoint) *Endpoint {
	for _, kind := range sp.Policy.SendTo {
		// tainted sessions and specs declared as possibly holding personal information (pii: true) do not use hosted
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

// Decide makes one decision. It returns an error only when the model call fails.
// When it decides not to decide, it returns Abstain and a reason instead of an error.
func Decide(ctx context.Context, sp *spec.Spec, inputs map[string]string, sess Session, eps []Endpoint) (*Decision, error) {
	d := &Decision{
		TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type),
		InputsHash: store.HashInputs(sp.StateInputs(inputs)), At: time.Now(), Tainted: sess.Tainted,
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

	// step 1: the minimal question
	d.Rung = "minimal"
	if err := askOnce(ctx, sp, d, state, ep); err != nil {
		return nil, err
	}
	// step 2: only when unsure (ask), and when the context inputs are present, add context and ask again.
	// The context question has its own hash, so the answer is recorded under that hash and calibrated with that hash's line. The minimal question's answer stays in Minimal.
	csp := sp.ContextSpec()
	if !unsure(sp, d.Band) || csp == nil {
		return d, nil
	}
	cstate, missing := csp.Render(inputs)
	if len(missing) > 0 {
		return d, nil // no context to add: stop here (to a person)
	}
	minimal := *d // if the context step's call fails, return the minimal answer (unsure). Do not throw the answer away
	d.Minimal = &store.Pass{SpecHash: d.SpecHash, InputsHash: d.InputsHash, Raw: d.Raw, Calibrated: d.Calibrated, IsCalibrated: d.IsCalibrated, Top: d.Top, P: d.P, Band: string(d.Band)}
	d.SpecHash = csp.Hash()
	d.InputsHash = store.HashInputs(csp.StateInputs(inputs))
	if err := askOnce(ctx, csp, d, cstate, ep); err != nil {
		minimal.Reason = "context rung failed: " + err.Error()
		return &minimal, nil
	}
	d.Rung = "context"
	return d, nil
}

// DiscardUnsure reports whether discard means "unsure" for this spec (types whose band comes from the highest option: choice, and score without level).
// For noul and score with level, discard is a confident "no / low". Moving up a step and counting what went to a person use the same rule.
func DiscardUnsure(sp *spec.Spec) bool {
	return sp.Type == spec.Choice || (sp.Type == spec.Score && sp.Thresholds.Level == 0)
}

// askOnce asks one step: it asks the multi-choice (or the broken-down yes/no list) in one call and writes raw, calibration, top and band into d.
func askOnce(ctx context.Context, sp *spec.Spec, d *Decision, state string, ep *Endpoint) error {
	if len(sp.Steps) > 0 {
		return decideBySteps(ctx, sp, d, state, ep)
	}
	resp, err := ep.Client.AskLayout(ctx, state, map[string]systemone.Question{questionKey: BuildQuestion(sp)}, sp.Layout)
	if err != nil {
		return err
	}
	ans, ok := resp.Answers[questionKey]
	if !ok {
		return fmt.Errorf("response has no answer for %q", questionKey)
	}
	// Use the model that actually answered for the calibration key. When "jev-latest" switches to another version,
	// the calibration expires and act stops appearing (the intended behaviour).
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
			// ordinal scale: the band is P(level ≥ k). Sum the probability split across neighbouring levels
			d.P = MassAtOrAbove(d.Calibrated, sp.Thresholds.Level)
		}
	}
	d.Band = band(sp.Thresholds, d.P, d.IsCalibrated)
	return nil
}

// unsure reports whether it is "unsure": the ask band is always unsure. For types whose band comes from the highest option (choice, and score without level),
// discard (no option reaches ask) is unsure too. For noul and score with level, discard is a confident "no / low", so not unsure.
func unsure(sp *spec.Spec, b Band) bool {
	return b == Ask || (b == Discard && DiscardUnsure(sp))
}

// StepQuestions is the set of questions that puts every step of the breakdown into one call (keys step-0, step-1, …).
func StepQuestions(sp *spec.Spec) map[string]systemone.Question {
	qs := make(map[string]systemone.Question, len(sp.Steps))
	for i, st := range sp.Steps {
		qs[stepKey(i)] = systemone.Question{Type: string(spec.Noul), Instructions: st.Ask}
	}
	return qs
}

func stepKey(i int) string { return fmt.Sprintf("step-%d", i) }

// StepDistribution builds the original options' distribution from each step's P(yes): the product of branch probabilities.
// An option settled at a middle step gets "remaining so far × that branch"; the rest goes on to the next step.
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

// decideBySteps asks a broken-down choice in one call and shapes the product distribution like a multi-choice answer (calibration, band and record are shared).
func decideBySteps(ctx context.Context, sp *spec.Spec, d *Decision, state string, ep *Endpoint) error {
	resp, err := ep.Client.AskLayout(ctx, state, StepQuestions(sp), sp.Layout)
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

// band decides the band from the thresholds and whether calibration exists. A decision without calibration never gives act.
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

// applyCalibration applies the calibration matching (hash, model), if any.
//   - "identity": no constants. A mark that the calibrated model's output was verified as trustworthy as is (design §7.1)
//   - "platt":    noul only. σ((Δ−bias)/T)
//   - "vector":   choice/score. softmax((log p_k + b_k)/T)
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
	sort.Strings(keys) // deterministic on ties
	best, bp := "", -1.0
	for _, k := range keys {
		if m[k] > bp {
			best, bp = k, m[k]
		}
	}
	return best, bp
}

// MassAtOrAbove is the sum of probabilities with level ≥ k in a score distribution. Keys are level numbers ("0".."n-1").
func MassAtOrAbove(m map[string]float64, k int) float64 {
	// add in a fixed key order (map iteration order changes every time, and the band wobbles right at the threshold)
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

// expectedLevel is the score's expected value Σ(level × p). Keys are level numbers ("0".."n-1").
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

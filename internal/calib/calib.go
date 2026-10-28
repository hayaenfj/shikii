// Package calib calibrates probabilities and measures how good a calibration is.
// The only calibrator is Platt (σ((Δ−bias)/T)). Δ is the logit of the raw probability.
package calib

import (
	"errors"
	"math"
	"sort"
)

// Sample is one label. Delta is the logit of the raw probability; Weight is the weight of a weak label (1 is normal).
type Sample struct {
	Delta  float64
	Truth  bool
	Weight float64
}

// Logit turns a probability into a logit. 0/1 are pulled slightly inward.
func Logit(p float64) float64 {
	const eps = 1e-6
	p = math.Min(math.Max(p, eps), 1-eps)
	return math.Log(p / (1 - p))
}

// Sigmoid is σ(z).
func Sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }

// Platt holds the calibration constants. Apply computes σ((Δ−Bias)/Temperature).
type Platt struct {
	Bias        float64
	Temperature float64
}

// Apply returns the calibrated probability.
func (p Platt) Apply(delta float64) float64 {
	return Sigmoid((delta - p.Bias) / p.Temperature)
}

// Fit fits Platt by weighted maximum likelihood. Platt-style smoothing of the targets
// (y+ = (N+ +1)/(N+ +2), y- = 1/(N- +2)) keeps it from overfitting.
// It cannot fit without both positives and negatives. A negative slope (higher probability means false) is an error.
func Fit(samples []Sample) (Platt, error) {
	var nPos, nNeg float64
	for _, s := range samples {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		if s.Truth {
			nPos += w
		} else {
			nNeg += w
		}
	}
	if nPos == 0 || nNeg == 0 {
		return Platt{}, ErrOneSided
	}
	yPos := (nPos + 1) / (nPos + 2)
	yNeg := 1 / (nNeg + 2)

	// Weighted cross-entropy (against the smoothed targets). The Newton step is shrunk until this goes down.
	// With a saturated σ the Hessian approaches 0 and plain Newton throws the slope toward infinity (measured on 2026-10-17:
	// it jumped to T=5e-7 and the Brier of separated data got worse, 0.02 → 0.11).
	loss := func(a, c float64) float64 {
		var sum float64
		for _, s := range samples {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			y := yNeg
			if s.Truth {
				y = yPos
			}
			z := a*s.Delta + c
			// log σ(z) and log(1−σ(z)) without loss of precision
			logP := -math.Log1p(math.Exp(-z))
			logQ := -math.Log1p(math.Exp(z))
			if z > 30 {
				logP, logQ = 0, -z
			} else if z < -30 {
				logP, logQ = z, 0
			}
			sum -= w * (y*logP + (1-y)*logQ)
		}
		return sum
	}

	// Newton's method in the form z = a·Δ + c. a = 1/T, c = −bias/T.
	a, c := 1.0, 0.0
	cur := loss(a, c)
	for iter := 0; iter < 200; iter++ {
		var g0, g1, h00, h01, h11 float64
		for _, s := range samples {
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			y := yNeg
			if s.Truth {
				y = yPos
			}
			p := Sigmoid(a*s.Delta + c)
			r := p - y
			g0 += w * r * s.Delta
			g1 += w * r
			q := w * p * (1 - p)
			h00 += q * s.Delta * s.Delta
			h01 += q * s.Delta
			h11 += q
		}
		// a small L2 term avoids singularity
		const l2 = 1e-6
		h00 += l2
		h11 += l2
		det := h00*h11 - h01*h01
		if det == 0 {
			break
		}
		da := (h11*g0 - h01*g1) / det
		dc := (h00*g1 - h01*g0) / det
		// Line search: halve the step until the loss goes down (the sign of the slope is not constrained; inverted data
		// converges to a negative slope and is rejected by the final check)
		step := 1.0
		improved := false
		for k := 0; k < 40; k++ {
			na, nc := a-step*da, c-step*dc
			if next := loss(na, nc); next < cur {
				a, c, cur = na, nc, next
				improved = true
				break
			}
			step /= 2
		}
		if !improved {
			break
		}
		if math.Abs(step*da) < 1e-10 && math.Abs(step*dc) < 1e-10 {
			break
		}
	}
	if a <= 0 {
		return Platt{}, ErrSlopeNotPositive
	}
	return Platt{Bias: -c / a, Temperature: 1 / a}, nil
}

// ErrSlopeNotPositive: the fitted slope is not positive, so a higher probability goes with false. Like an overlap, the answers cannot be
// told apart with this question and model; callers report it as that, not as a failure.
// ErrOneSided: every truth is on one side, so no line can be fitted yet (callers report it as a refusal, not as a failure).
var ErrOneSided = errors.New("need both positive and negative samples")

var ErrSlopeNotPositive = errors.New("fitted slope is not positive: higher probability predicts false")

// Brier is mean(w·(p−y)²). 0 is perfect.
func Brier(probs []float64, truths []bool, weights []float64) float64 {
	var sum, wsum float64
	for i, p := range probs {
		w := weightAt(weights, i)
		y := 0.0
		if truths[i] {
			y = 1
		}
		sum += w * (p - y) * (p - y)
		wsum += w
	}
	if wsum == 0 {
		return 0
	}
	return sum / wsum
}

// Discrimination is how well the probabilities rank the truths: AUC (0.5 is chance, below 0.5 points the wrong way) and its
// Hanley–McNeil 95% interval. It does not depend on how often each answer is true, so it reads the same for a question
// answered "yes" nine times in ten as for an even one (0.10).
type Discrimination struct {
	AUC, Low, High float64
	Pos, Neg       int
}

// AUC is the weighted share of (positive, negative) pairs where the positive has the higher probability (a tie counts half).
// ok=false when one side has no truths.
func AUC(probs []float64, truths []bool, weights []float64) (Discrimination, bool) {
	var d Discrimination
	var wins, pairs float64
	for i, p := range probs {
		if !truths[i] {
			continue
		}
		d.Pos++
		for j, q := range probs {
			if truths[j] {
				continue
			}
			w := weightAt(weights, i) * weightAt(weights, j)
			pairs += w
			switch {
			case p > q:
				wins += w
			case p == q:
				wins += w / 2
			}
		}
	}
	d.Neg = len(probs) - d.Pos
	if d.Pos == 0 || d.Neg == 0 || pairs == 0 {
		return d, false
	}
	a := wins / pairs
	q1, q2 := a/(2-a), 2*a*a/(1+a)
	se := math.Sqrt((a*(1-a) + float64(d.Pos-1)*(q1-a*a) + float64(d.Neg-1)*(q2-a*a)) / float64(d.Pos*d.Neg))
	d.AUC, d.Low, d.High = a, math.Max(0, a-1.96*se), math.Min(1, a+1.96*se)
	return d, true
}

// Bin is one bar of the reliability diagram.
type Bin struct {
	Lo, Hi     float64
	N          float64 // weighted count
	Confidence float64 // mean confidence
	Accuracy   float64 // accuracy
}

// Reliability splits probabilities into equal-width bins and returns the mean confidence and accuracy of each bin.
func Reliability(probs []float64, truths []bool, weights []float64, bins int) []Bin {
	if bins <= 0 {
		bins = 10
	}
	out := make([]Bin, bins)
	for i := range out {
		out[i].Lo = float64(i) / float64(bins)
		out[i].Hi = float64(i+1) / float64(bins)
	}
	for i, p := range probs {
		k := int(p * float64(bins))
		if k >= bins {
			k = bins - 1
		}
		if k < 0 {
			k = 0
		}
		w := weightAt(weights, i)
		b := &out[k]
		b.N += w
		b.Confidence += w * p
		if truths[i] {
			b.Accuracy += w
		}
	}
	for i := range out {
		if out[i].N > 0 {
			out[i].Confidence /= out[i].N
			out[i].Accuracy /= out[i].N
		}
	}
	return out
}

// ECE is the sum of |confidence − accuracy| over the bins of the reliability diagram, weighted by count.
func ECE(probs []float64, truths []bool, weights []float64, bins int) float64 {
	rel := Reliability(probs, truths, weights, bins)
	var total float64
	for _, b := range rel {
		total += b.N
	}
	if total == 0 {
		return 0
	}
	var e float64
	for _, b := range rel {
		if b.N > 0 {
			e += (b.N / total) * math.Abs(b.Confidence-b.Accuracy)
		}
	}
	return e
}

// Separation is the lowest positive and the highest negative, and how many cross the other side's edge. It is reported to show
// which examples overlap; an overlap alone does not refuse a line (0.10: AUC decides whether the probabilities tell the answers apart).
// Weak labels with weight below 1 (borderline, merely not undone, etc.) are left out of the check:
// examples known to be ambiguous naturally land at the edges, and calling that "inverted" would be a false alarm.
type Separation struct {
	PosMin  float64
	NegMax  float64
	Overlap int // count past the other side's edge
	Weak    int // number of weak labels left out of the check
}

// CheckSeparation looks at how positives and negatives separate on Δ (only labels with weight 1 or more).
func CheckSeparation(samples []Sample) Separation {
	sep := Separation{PosMin: math.Inf(1), NegMax: math.Inf(-1)}
	var pos, neg []float64
	for _, s := range samples {
		if s.Weight > 0 && s.Weight < 1 {
			sep.Weak++
			continue
		}
		if s.Truth {
			pos = append(pos, s.Delta)
			sep.PosMin = math.Min(sep.PosMin, s.Delta)
		} else {
			neg = append(neg, s.Delta)
			sep.NegMax = math.Max(sep.NegMax, s.Delta)
		}
	}
	if len(pos) == 0 || len(neg) == 0 {
		return sep
	}
	sort.Float64s(pos)
	sort.Float64s(neg)
	for _, d := range pos {
		if d < sep.NegMax {
			sep.Overlap++
		}
	}
	for _, d := range neg {
		if d > sep.PosMin {
			sep.Overlap++
		}
	}
	return sep
}

func weightAt(ws []float64, i int) float64 {
	if i < len(ws) && ws[i] > 0 {
		return ws[i]
	}
	return 1
}

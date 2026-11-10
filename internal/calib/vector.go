package calib

import (
	"errors"
	"math"
	"sort"

	"github.com/hayaenfj/shikii/internal/probability"
)

// Vector is multi-class calibration (vector scaling): softmax((log p_k + b_k) / T).
// T fixes the overall strength of confidence; b_k fixes each choice's habit (e.g. escaping to "other" on a short state).
type Vector struct {
	Temperature float64
	Biases      map[string]float64
}

// Apply returns the calibrated distribution. Biases for keys not in raw are ignored, and only raw's keys are returned.
func (v Vector) Apply(raw map[string]float64) map[string]float64 {
	keys := sortedKeys(raw)
	z := make([]float64, len(keys))
	for i, k := range keys {
		z[i] = (logClamp(raw[k]) + v.Biases[k]) / v.Temperature
	}
	q := softmax(z)
	out := make(map[string]float64, len(keys))
	for i, k := range keys {
		out[k] = q[i]
	}
	return out
}

// MultiSample is one label. Raw maps choice → probability; Truth is the correct key.
type MultiSample struct {
	Raw    map[string]float64
	Truth  string
	Weight float64
}

// FitVector fits T and b by weighted maximum likelihood. b is projected onto Σb=0 to make it unique.
// The correct answer is smoothed to 1−ε (ε = 1/(N+K)) so that biases of choices without labels do not fly to −∞.
// The ε goes to the options in the sample's own raw shares, not evenly: an even share asks for ε/K on an option
// the model left at the floor (1e-6), which only a large positive bias can give, and that bias then turns a few
// percent on a later input into the top answer.
// The set of all choices is the union of the keys of the samples' Raw.
func FitVector(samples []MultiSample) (Vector, error) {
	if len(samples) == 0 {
		return Vector{}, errors.New("no samples")
	}
	keySet := map[string]bool{}
	seen := map[string]bool{} // options that were the truth at least once
	var totalW float64
	for _, s := range samples {
		for k := range s.Raw {
			keySet[k] = true
		}
		if _, ok := s.Raw[s.Truth]; !ok {
			return Vector{}, errors.New("truth " + s.Truth + " is not among the options")
		}
		seen[s.Truth] = true
		totalW += weightOf(s.Weight)
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	K := len(keys)
	if K < 2 {
		return Vector{}, errors.New("need at least 2 options")
	}
	idx := map[string]int{}
	for i, k := range keys {
		idx[k] = i
	}
	eps := 1 / (totalW + float64(K))
	shares := make([][]float64, len(samples)) // each sample's raw distribution over all options (clamped, sums to 1)
	for n, s := range samples {
		shares[n] = make([]float64, K)
		var sum float64
		for i, k := range keys {
			shares[n][i] = math.Max(s.Raw[k], 1e-6)
			sum += shares[n][i]
		}
		for i := range shares[n] {
			shares[n][i] /= sum
		}
	}

	// parameters: alpha = log(1/T), b[K]
	alpha := 0.0
	b := make([]float64, K)
	const l2 = 1e-3

	nll := func(alpha float64, b []float64) float64 {
		a := math.Exp(alpha)
		var total float64
		for n, s := range samples {
			w := weightOf(s.Weight)
			z := make([]float64, K)
			for i, k := range keys {
				z[i] = a*logClamp(s.Raw[k]) + b[i]
			}
			logQ := logSoftmax(z)
			t := idx[s.Truth]
			for i := range keys {
				y := eps * shares[n][i]
				if i == t {
					y += 1 - eps
				}
				total -= w * y * logQ[i]
			}
		}
		for _, bi := range b {
			total += l2 * bi * bi
		}
		return total
	}

	grad := func(alpha float64, b []float64) (float64, []float64) {
		a := math.Exp(alpha)
		var ga float64
		gb := make([]float64, K)
		for n, s := range samples {
			w := weightOf(s.Weight)
			z := make([]float64, K)
			lp := make([]float64, K)
			for i, k := range keys {
				lp[i] = logClamp(s.Raw[k])
				z[i] = a*lp[i] + b[i]
			}
			q := softmax(z)
			t := idx[s.Truth]
			for i := range keys {
				y := eps * shares[n][i]
				if i == t {
					y += 1 - eps
				}
				r := w * (q[i] - y)
				gb[i] += r
				ga += r * lp[i]
			}
		}
		ga *= a // d/dalpha = a · d/da
		for i := range gb {
			gb[i] += 2 * l2 * b[i]
		}
		return ga, gb
	}

	cur := nll(alpha, b)
	step := 0.5
	for iter := 0; iter < 2000; iter++ {
		ga, gb := grad(alpha, b)
		// backtracking line search
		var nAlpha float64
		nb := make([]float64, K)
		improved := false
		for tries := 0; tries < 30; tries++ {
			nAlpha = alpha - step*ga
			for i := range b {
				nb[i] = b[i] - step*gb[i]
			}
			project(nb)
			if v := nll(nAlpha, nb); v < cur {
				cur, alpha, b = v, nAlpha, nb
				improved = true
				step *= 1.2
				break
			}
			step *= 0.5
		}
		if !improved {
			break
		}
		gn := ga * ga
		for _, g := range gb {
			gn += g * g
		}
		if gn < 1e-14 {
			break
		}
	}
	// An option that was never the truth has no evidence for a lift: the smoothing only keeps its bias from flying
	// to minus infinity. Cap it at the mean bias of the options that were the truth.
	var seenMean float64
	seenN := 0
	for i, k := range keys {
		if seen[k] {
			seenMean += b[i]
			seenN++
		}
	}
	if seenN > 0 && seenN < K {
		seenMean /= float64(seenN)
		for i, k := range keys {
			if !seen[k] && b[i] > seenMean {
				b[i] = seenMean
			}
		}
		project(b)
	}
	T := math.Exp(-alpha)
	if T <= 0 || math.IsNaN(T) || math.IsInf(T, 0) {
		return Vector{}, errors.New("fitted temperature is not finite")
	}
	// What was fitted is z = a·log p + b. Apply uses (log p + b')/T, so convert to b' = b/a = b·T.
	biases := make(map[string]float64, K)
	for i, k := range keys {
		biases[k] = b[i] * T
	}
	return Vector{Temperature: T, Biases: biases}, nil
}

// project makes Σb = 0 (removes the degree of freedom that comes from softmax's shift invariance).
func project(b []float64) {
	var mean float64
	for _, v := range b {
		mean += v
	}
	mean /= float64(len(b))
	for i := range b {
		b[i] -= mean
	}
}

func softmax(z []float64) []float64 { return probability.Softmax(z, 1) }

// logSoftmax is log(softmax(z)) without going through the probabilities: a probability that underflows to 0 still has
// a finite log, so the loss keeps growing with the slope instead of going flat.
func logSoftmax(z []float64) []float64 {
	m := math.Inf(-1)
	for _, v := range z {
		m = math.Max(m, v)
	}
	var sum float64
	for _, v := range z {
		sum += math.Exp(v - m)
	}
	lse := m + math.Log(sum)
	out := make([]float64, len(z))
	for i, v := range z {
		out[i] = v - lse
	}
	return out
}

func logClamp(p float64) float64 {
	return math.Log(math.Max(p, 1e-6))
}

func weightOf(w float64) float64 {
	if w <= 0 {
		return 1
	}
	return w
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package calib

import (
	"errors"
	"math"
	"sort"
)

// Vector は多クラスの較正(vector scaling): softmax((log p_k + b_k) / T)。
// T が全体の確信の強さ、b_k が選択肢ごとの癖(例: 短い state で "other" に逃げる)を直す。
type Vector struct {
	Temperature float64
	Biases      map[string]float64
}

// Apply は較正後の分布。raw に無い鍵の bias は無視し、raw の鍵だけを返す。
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

// MultiSample はラベル 1 件。Raw は選択肢→確率、Truth は正解の鍵。
type MultiSample struct {
	Raw    map[string]float64
	Truth  string
	Weight float64
}

// FitVector は重み付き最尤で T と b を当てる。b は Σb=0 に射影して一意にする。
// ラベルの無い選択肢の bias が −∞ に飛ばないよう、正解を 1−ε で平滑化する(ε = 1/(N+K))。
// 全選択肢の集合は samples の Raw の鍵の和。
func FitVector(samples []MultiSample) (Vector, error) {
	if len(samples) == 0 {
		return Vector{}, errors.New("no samples")
	}
	keySet := map[string]bool{}
	var totalW float64
	for _, s := range samples {
		for k := range s.Raw {
			keySet[k] = true
		}
		if _, ok := s.Raw[s.Truth]; !ok {
			return Vector{}, errors.New("truth " + s.Truth + " is not among the options")
		}
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

	// パラメータ: alpha = log(1/T), b[K]
	alpha := 0.0
	b := make([]float64, K)
	const l2 = 1e-3

	nll := func(alpha float64, b []float64) float64 {
		a := math.Exp(alpha)
		var total float64
		for _, s := range samples {
			w := weightOf(s.Weight)
			z := make([]float64, K)
			for i, k := range keys {
				z[i] = a*logClamp(s.Raw[k]) + b[i]
			}
			q := softmax(z)
			t := idx[s.Truth]
			for i := range keys {
				y := eps / float64(K)
				if i == t {
					y += 1 - eps
				}
				total -= w * y * math.Log(math.Max(q[i], 1e-12))
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
		for _, s := range samples {
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
				y := eps / float64(K)
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
		// 後退直線探索
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
	T := math.Exp(-alpha)
	if T <= 0 || math.IsNaN(T) || math.IsInf(T, 0) {
		return Vector{}, errors.New("fitted temperature is not finite")
	}
	// 当てたのは z = a·log p + b。Apply は (log p + b')/T なので b' = b/a = b·T に直す。
	biases := make(map[string]float64, K)
	for i, k := range keys {
		biases[k] = b[i] * T
	}
	return Vector{Temperature: T, Biases: biases}, nil
}

// project は Σb = 0 にする(softmax の平行移動不変性で生じる自由度を潰す)。
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

func softmax(z []float64) []float64 {
	m := math.Inf(-1)
	for _, v := range z {
		m = math.Max(m, v)
	}
	out := make([]float64, len(z))
	var sum float64
	for i, v := range z {
		out[i] = math.Exp(v - m)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
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

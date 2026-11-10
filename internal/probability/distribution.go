// Package probability contains deterministic operations on probability distributions.
package probability

import (
	"math"
	"sort"
)

// Argmax breaks ties by key order and preserves the empty distribution result.
func Argmax(m map[string]float64) (string, float64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bp := "", -1.0
	for _, k := range keys {
		if m[k] > bp {
			best, bp = k, m[k]
		}
	}
	return best, bp
}

// Softmax subtracts the maximum before exponentiation; temperature scales logits.
func Softmax(z []float64, temperature float64) []float64 {
	m := math.Inf(-1)
	for _, v := range z {
		m = math.Max(m, v)
	}
	out := make([]float64, len(z))
	var sum float64
	for i, v := range z {
		out[i] = math.Exp((v - m) / temperature)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

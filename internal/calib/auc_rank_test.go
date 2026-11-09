package calib

import (
	"math"
	"testing"
)

// Independent pairwise definition, retained as the oracle for ranking changes.
func pairwiseAUCOracle(probs []float64, truths []bool, weights []float64) (Discrimination, bool) {
	var d Discrimination
	wins, pairs := 0.0, 0.0
	weight := func(i int) float64 {
		if i < len(weights) && weights[i] > 0 {
			return weights[i]
		}
		return 1
	}
	for i, p := range probs {
		if !truths[i] {
			continue
		}
		d.Pos++
		for j, q := range probs {
			if truths[j] {
				continue
			}
			w := weight(i) * weight(j)
			pairs += w
			if p > q {
				wins += w
			} else if p == q {
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

func TestAUCRankingKeepsPairwiseResults(t *testing.T) {
	for _, n := range []int{0, 1, 2, 7, 100, 1000} {
		for _, mode := range []string{"unit", "integer", "fractional", "extreme", "nan", "infinite", "single-side"} {
			probs, truths, weights := make([]float64, n), make([]bool, n), make([]float64, n)
			for i := range probs {
				probs[i] = float64((i*13)%17) / 16
				truths[i] = i%2 == 0
				weights[i] = 1
				switch mode {
				case "integer":
					weights[i] = float64(i % 4)
				case "fractional":
					weights[i] = 0.1 * float64(i%7+1)
				case "extreme":
					weights[i] = 1e-163
					if i%3 == 0 {
						weights[i] = 1e308
					}
				case "nan":
					if i%5 == 0 {
						probs[i] = math.NaN()
					}
				case "infinite":
					if i%3 == 0 {
						probs[i] = math.Inf(1)
					} else if i%3 == 1 {
						probs[i] = math.Inf(-1)
					}
				case "single-side":
					truths[i] = true
				}
			}
			got, ok := AUC(probs, truths, weights)
			want, wok := pairwiseAUCOracle(probs, truths, weights)
			if ok != wok || got.Pos != want.Pos || got.Neg != want.Neg {
				t.Fatal("ranking must preserve counts and eligibility", n, mode, got, want)
			}
			for _, pair := range [][2]float64{{got.AUC, want.AUC}, {got.Low, want.Low}, {got.High, want.High}} {
				if math.Float64bits(pair[0]) != math.Float64bits(pair[1]) {
					t.Fatal("ranking must preserve every result bit, including fallback inputs", n, mode, pair)
				}
			}
		}
	}
	for _, probs := range [][]float64{{math.Inf(-1), math.Inf(1), 0, 0}, {0, 0, 0, 0}, {math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}} {
		truths := []bool{true, false, true, false}
		got, ok := AUC(probs, truths, nil)
		want, wok := pairwiseAUCOracle(probs, truths, nil)
		if ok != wok || got != want {
			t.Fatal("ties and infinite probabilities retain the pairwise definition", got, want)
		}
	}
}

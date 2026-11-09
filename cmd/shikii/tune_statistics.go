package main

import (
	"math"
)

// signTestP is the one-sided sign test: among the n = wins+losses rows that disagree, the chance that wins or more fall on the candidate's side (1/2 each if equally good).
func signTestP(wins, losses int) float64 {
	n := wins + losses
	if n == 0 {
		return 1
	}
	p := 0.0
	for x := wins; x <= n; x++ {
		p += binom(n, x)
	}
	return p / math.Pow(2, float64(n))
}

func binom(n, k int) float64 {
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(n-k+i) / float64(i)
	}
	return r
}

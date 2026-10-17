// Package calib は確率の較正と、較正の良さの指標。
// 較正器は Platt(σ((Δ−bias)/T))のみ。Δ は生の確率のロジット。
package calib

import (
	"errors"
	"math"
	"sort"
)

// Sample はラベル 1 件。Delta は生の確率のロジット、Weight は弱いラベルの重み(1 が通常)。
type Sample struct {
	Delta  float64
	Truth  bool
	Weight float64
}

// Logit は確率をロジットに変える。0/1 は少し内側に寄せる。
func Logit(p float64) float64 {
	const eps = 1e-6
	p = math.Min(math.Max(p, eps), 1-eps)
	return math.Log(p / (1 - p))
}

// Sigmoid は σ(z)。
func Sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }

// Platt は較正定数。Apply が σ((Δ−Bias)/Temperature)。
type Platt struct {
	Bias        float64
	Temperature float64
}

// Apply は較正後の確率。
func (p Platt) Apply(delta float64) float64 {
	return Sigmoid((delta - p.Bias) / p.Temperature)
}

// Fit は重み付き最尤で Platt を当てる。Platt 流の目標値の平滑化
// (y+ = (N+ +1)/(N+ +2), y- = 1/(N- +2))で過学習を抑える。
// 正例と負例の両方が無いと当てられない。傾きが負(確率が高いほど偽)なら誤り。
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
		return Platt{}, errors.New("need both positive and negative samples")
	}
	yPos := (nPos + 1) / (nPos + 2)
	yNeg := 1 / (nNeg + 2)

	// 重み付きの交差エントロピー(平滑化した目標値に対して)。Newton の歩幅はこれが下がる所まで縮める。
	// 飽和した σ では Hessian が 0 に近づき、素の Newton は傾きを無限大に飛ばす(2026-10-17 の実測で
	// T=5e-7 まで飛び、分離したデータの Brier が 0.02 → 0.11 に悪化した)。
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
			// log σ(z) と log(1−σ(z)) を桁落ちなしで
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

	// z = a·Δ + c の形で Newton 法。a = 1/T, c = −bias/T。
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
		// 小さな L2 で特異を避ける
		const l2 = 1e-6
		h00 += l2
		h11 += l2
		det := h00*h11 - h01*h01
		if det == 0 {
			break
		}
		da := (h11*g0 - h01*g1) / det
		dc := (h00*g1 - h01*g0) / det
		// 直線探索: 損失が下がる歩幅まで半分にする(傾きの符号は制約しない。逆転したデータは
		// 負の傾きに収束し、最後の検査で拒否される)
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
		return Platt{}, errors.New("fitted slope is not positive: higher probability predicts false")
	}
	return Platt{Bias: -c / a, Temperature: 1 / a}, nil
}

// Brier は mean(w·(p−y)²)。0 が完全。
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

// Bin は信頼度図の 1 本。
type Bin struct {
	Lo, Hi     float64
	N          float64 // 重み付き件数
	Confidence float64 // 平均確信度
	Accuracy   float64 // 正答率
}

// Reliability は確率を等幅の bin に分けて、bin ごとの平均確信度と正答率を出す。
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

// ECE は信頼度図の bin ごとの |確信度 − 正答率| を件数で加重した和。
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

// Separation は正例の下限と負例の上限。Inverted なら単調な較正では直せない
// (質問文かモデルを変えるしかない)。
// 重み 1 未満の弱いラベル(境界・取り消されなかっただけ等)は判定に入れない:
// 曖昧だと分かっている例が端に来るのは当然で、それで「逆転」と言うのは誤報になる。
type Separation struct {
	PosMin   float64
	NegMax   float64
	Overlap  int // 相手側の端を越えた件数
	Inverted bool
	Weak     int // 判定から外した弱いラベルの数
}

// CheckSeparation は Δ 上での正負の分離を見る(重み 1 以上のラベルだけ)。
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
	sep.Inverted = sep.PosMin < sep.NegMax
	return sep
}

func weightAt(ws []float64, i int) float64 {
	if i < len(ws) && ws[i] > 0 {
		return ws[i]
	}
	return 1
}

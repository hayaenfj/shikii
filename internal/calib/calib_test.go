package calib

import (
	"math"
	"math/rand"
	"testing"
)

// 既知の bias/T で生成したデータから、Fit がその値を取り戻す。
func TestFitRecoversKnownPlatt(t *testing.T) {
	truth := Platt{Bias: 2.2, Temperature: 0.3}
	rng := rand.New(rand.NewSource(1))
	var samples []Sample
	for i := 0; i < 4000; i++ {
		delta := rng.NormFloat64()*1.5 + 2.2
		p := truth.Apply(delta)
		samples = append(samples, Sample{Delta: delta, Truth: rng.Float64() < p, Weight: 1})
	}
	got, err := Fit(samples)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.Bias-truth.Bias) > 0.1 || math.Abs(got.Temperature-truth.Temperature) > 0.05 {
		t.Fatalf("fit drifted: got bias=%.3f T=%.3f want bias=%.3f T=%.3f", got.Bias, got.Temperature, truth.Bias, truth.Temperature)
	}
}

func TestFitRejectsDegenerateData(t *testing.T) {
	if _, err := Fit([]Sample{{Delta: 1, Truth: true}, {Delta: 2, Truth: true}}); err == nil {
		t.Fatal("single-class data must not fit")
	}
	// 確率が高いほど偽: 傾きが負になるので誤りとして返す
	inv := []Sample{{Delta: 3, Truth: false}, {Delta: 2.5, Truth: false}, {Delta: -2, Truth: true}, {Delta: -3, Truth: true}}
	if _, err := Fit(inv); err == nil {
		t.Fatal("negative slope must be rejected")
	}
}

func TestBrierAndECE(t *testing.T) {
	probs := []float64{1, 0, 1, 0}
	truths := []bool{true, false, true, false}
	if b := Brier(probs, truths, nil); b != 0 {
		t.Fatalf("perfect predictions must have Brier 0, got %f", b)
	}
	if e := ECE(probs, truths, nil, 10); e != 0 {
		t.Fatalf("perfect predictions must have ECE 0, got %f", e)
	}
	// 全部 0.9 と言って半分しか当たらない: 過信。ECE ≈ 0.4、Brier = 0.5·0.01 + 0.5·0.81 = 0.41
	over := []float64{0.9, 0.9, 0.9, 0.9}
	half := []bool{true, false, true, false}
	if e := ECE(over, half, nil, 10); math.Abs(e-0.4) > 1e-9 {
		t.Fatalf("overconfident ECE want 0.4, got %f", e)
	}
	if b := Brier(over, half, nil); math.Abs(b-0.41) > 1e-9 {
		t.Fatalf("overconfident Brier want 0.41, got %f", b)
	}
	// 重み: 2 件目を重さ 0 相当(=1 に落ちる)ではなく 3 にすると Brier は (0.01+3·0.81+0.01+0.81)/6
	w := []float64{1, 3, 1, 1}
	if b := Brier(over, half, w); math.Abs(b-(0.01+3*0.81+0.01+0.81)/6) > 1e-9 {
		t.Fatalf("weighted Brier wrong: %f", b)
	}
}

func TestReliabilityBins(t *testing.T) {
	probs := []float64{0.05, 0.95, 0.95, 1.0}
	truths := []bool{false, true, false, true}
	rel := Reliability(probs, truths, nil, 10)
	if rel[0].N != 1 || rel[0].Accuracy != 0 || rel[0].Confidence != 0.05 {
		t.Fatalf("bin 0: %+v", rel[0])
	}
	// p=1.0 は最後の bin に入る
	last := rel[9]
	if last.N != 3 || math.Abs(last.Accuracy-2.0/3) > 1e-9 || math.Abs(last.Confidence-(0.95+0.95+1.0)/3) > 1e-9 {
		t.Fatalf("bin 9: %+v", last)
	}
}

func TestCheckSeparationDetectsInversion(t *testing.T) {
	clean := []Sample{{Delta: 2.54, Truth: true}, {Delta: 4, Truth: true}, {Delta: 1.96, Truth: false}, {Delta: 0, Truth: false}}
	if s := CheckSeparation(clean); s.Inverted || s.Overlap != 0 || s.PosMin != 2.54 || s.NegMax != 1.96 {
		t.Fatalf("clean data flagged: %+v", s)
	}
	// had-scorer で実測した逆転: 関連の下限 1.80 < 境界 1.82
	inverted := []Sample{{Delta: 1.80, Truth: true}, {Delta: 4, Truth: true}, {Delta: 1.82, Truth: false}, {Delta: 0, Truth: false}}
	if s := CheckSeparation(inverted); !s.Inverted || s.Overlap != 2 {
		t.Fatalf("inversion not detected: %+v", s)
	}
	// 境界(重み 0.5)の負例が正例より上に来ても逆転とは言わない。曖昧と分かっている例だから。
	weak := []Sample{{Delta: 2.54, Truth: true, Weight: 1}, {Delta: 4, Truth: true, Weight: 1},
		{Delta: 3.0, Truth: false, Weight: 0.5}, {Delta: 1.96, Truth: false, Weight: 1}}
	if s := CheckSeparation(weak); s.Inverted || s.Weak != 1 || s.NegMax != 1.96 {
		t.Fatalf("weak labels must not drive the inversion verdict: %+v", s)
	}
	// 同じ負例が重み 1 なら逆転
	weak[2].Weight = 1
	if s := CheckSeparation(weak); !s.Inverted {
		t.Fatalf("full-weight negative above pos_min must be an inversion: %+v", s)
	}
}

func TestLogitClamps(t *testing.T) {
	if math.IsInf(Logit(0), 0) || math.IsInf(Logit(1), 0) || math.IsNaN(Logit(0)) {
		t.Fatal("logit must clamp 0 and 1")
	}
	if math.Abs(Sigmoid(Logit(0.3))-0.3) > 1e-9 {
		t.Fatal("sigmoid∘logit must be identity in (0,1)")
	}
}

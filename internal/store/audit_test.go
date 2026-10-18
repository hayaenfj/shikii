package store

import (
	"testing"
	"time"
)

// 抜き取り: act で自動で動いた判定のうち 20 件に 1 件を人に見せる。trace_id で決まるので、同じ判定は何度見ても同じ答え。
func TestAuditSampleIsOneInTwentyAndStable(t *testing.T) {
	n := 0
	for i := 0; i < 4000; i++ {
		id := NewTraceID()
		a := AuditSample(id)
		if a != AuditSample(id) {
			t.Fatalf("sampling must be a function of the trace id")
		}
		if a {
			n++
		}
	}
	if n < 140 || n > 260 { // 期待 200(1/20)。±30% の外なら比率が違う
		t.Fatalf("expected about 1 in 20 sampled, got %d of 4000", n)
	}
	if AuditSample("") {
		t.Fatalf("an empty trace is never sampled")
	}
}

// 人の正解と機械の正解を分ける: 機械が決めた正解は上の段の答え(context)だけ。聞き直し(replay)とファイル(file)の正解は人が付けたもの。
func TestHumanSourceExcludesMachineTruths(t *testing.T) {
	for _, s := range []string{"ui", "manual", "ask", "audit", "mcp", "outcome", "replay", "file", ""} {
		if !HumanSource(s) {
			t.Fatalf("%q is a truth a person (or the outcome) decided", s)
		}
	}
	for _, s := range []string{"context", "judge"} {
		if HumanSource(s) {
			t.Fatalf("%q is a machine truth (the model's own answer, or another model's)", s)
		}
	}
}

// 監査 4 回目の 2: 同じ入力に人の正解と機械の正解(judge / context)があれば、新しさに関わらず人の正解が勝つ。人の正解どうしは新しい方。
func TestLatestPerInputPrefersHumanTruths(t *testing.T) {
	in := HashInputs(map[string]string{"a": "x"})
	t0 := time.Now()
	human := Label{InputsHash: in, Truth: "yes", Source: "ui", At: t0}
	judge := Label{InputsHash: in, Truth: "no", Source: "judge", At: t0.Add(time.Hour)}
	if got := LatestPerInput([]Label{human, judge}); len(got) != 1 || got[0].Source != "ui" {
		t.Fatalf("a later machine truth must not push out a human truth: %+v", got)
	}
	if got := LatestPerInput([]Label{judge, human}); len(got) != 1 || got[0].Source != "ui" {
		t.Fatalf("order of appearance must not matter: %+v", got)
	}
	fix := Label{InputsHash: in, Truth: "no", Source: "ui", At: t0.Add(2 * time.Hour)}
	if got := LatestPerInput([]Label{human, fix}); len(got) != 1 || got[0].Truth != "no" {
		t.Fatalf("a later human correction wins over an earlier human truth: %+v", got)
	}
	ctx := Label{InputsHash: in, Truth: "no", Source: "context", At: t0.Add(3 * time.Hour)}
	if got := LatestPerInput([]Label{ctx}); len(got) != 1 || got[0].Source != "context" {
		t.Fatalf("with no human truth the machine truth stands: %+v", got)
	}
}

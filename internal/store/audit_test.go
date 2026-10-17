package store

import "testing"

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

// 人の正解と機械の正解を分ける: 上の段の答え(context)・聞き直し(replay)・ファイル(file)は人が見ていない。
func TestHumanSourceExcludesMachineTruths(t *testing.T) {
	for _, s := range []string{"ui", "manual", "ask", "audit", "mcp", "outcome", ""} {
		if !HumanSource(s) {
			t.Fatalf("%q is a truth a person (or the outcome) gave", s)
		}
	}
	for _, s := range []string{"context", "replay", "file"} {
		if HumanSource(s) {
			t.Fatalf("%q is a machine truth", s)
		}
	}
}

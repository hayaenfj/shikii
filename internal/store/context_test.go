package store

import (
	"path/filepath"
	"testing"
	"time"
)

// 文脈の段で決まった記録に人が答えると、正解は両方の段に付く: 最終の答え(文脈の指紋・文脈の raw)と、最小の質問(その指紋・その raw)。
func TestLabelsForReachBothRungs(t *testing.T) {
	r := rec("ctx-hash", "m")
	r.Type, r.Rung = "choice", "context"
	r.Raw = map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02}
	r.Top, r.P, r.Band = "change_request", 0.9, "act"
	r.Minimal = &Pass{SpecHash: "min-hash", Raw: map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}, Top: "change_request", P: 0.65, Band: "ask"}
	labels, err := LabelsFor(&r, "question", 1, "ui")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 {
		t.Fatalf("a context decision yields a truth for each rung, got %d", len(labels))
	}
	final, minimal := labels[0], labels[1]
	if final.SpecHash != "ctx-hash" || final.Raw["change_request"] != 0.9 || final.Rung != "context" || final.Truth != "question" {
		t.Fatalf("first label is the final answer under the context hash: %+v", final)
	}
	if minimal.SpecHash != "min-hash" || minimal.Raw["change_request"] != 0.65 || minimal.Rung != "minimal" || minimal.Truth != "question" || minimal.TraceID != r.TraceID || minimal.InputsHash != r.InputsHash {
		t.Fatalf("second label teaches the minimal rung the same truth: %+v", minimal)
	}
	// 最小の段だけの記録は 1 つ
	plain := rec("min-hash", "m")
	if ls, err := LabelsFor(&plain, "yes", 1, "ui"); err != nil || len(ls) != 1 || ls[0].Rung != "minimal" {
		t.Fatalf("a minimal-only record yields one label: %v %+v", err, ls)
	}
}

// 段ごとの数: 最小で決まった / 文脈で決まった / 人に回った(ask のまま)。進み具合は 1000 判定あたりの人の判断。
func TestFunnelCountsWhereDecisionsSettle(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "s")}
	add := func(rung, band string) {
		r := rec("h", "m")
		r.Rung, r.Band, r.At = rung, band, time.Now()
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	add("", "act") // 古い記録(rung 無し)は最小の段
	add("minimal", "discard")
	add("minimal", "ask")
	add("context", "act")
	add("context", "ask")
	f, err := st.Funnel("s")
	if err != nil {
		t.Fatal(err)
	}
	if f.Records != 5 || f.Minimal != 2 || f.Context != 1 || f.Human != 2 {
		t.Fatalf("funnel: %+v", f)
	}
	if f.HumanPerThousand() != 400 {
		t.Fatalf("2 of 5 go to a person = 400 per 1000, got %v", f.HumanPerThousand())
	}
	if empty, _ := st.Funnel("none"); empty.Records != 0 || empty.HumanPerThousand() != 0 {
		t.Fatalf("no records: zero, not a division error: %+v", empty)
	}
}

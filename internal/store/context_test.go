package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 文脈の段で決まった記録に人が答えると、正解は両方の段に付く: 最終の答え(文脈のハッシュ・文脈の raw)と、最小の質問(そのハッシュ・その raw)。
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

// 段ごとの数: 最小で決まった / 文脈で決まった / 人に回った。「人に回った」は decide の「迷い」と同じ規則:
// ask はいつでも、discard は一番高い選択肢で帯を決める型(choice、level 無しの score)の時だけ(監査 3 回目の 4)。
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
	add("context", "discard")
	// noul: discard は確信した「いいえ」= 決まった
	f, err := st.Funnel("s", false)
	if err != nil {
		t.Fatal(err)
	}
	if f.Records != 6 || f.Minimal != 2 || f.Context != 2 || f.Human != 2 {
		t.Fatalf("noul funnel: %+v", f)
	}
	// choice: discard も迷い = 人へ(README の「the tests … 0.45 → 人へ」はこちら)
	f, _ = st.Funnel("s", true)
	if f.Minimal != 1 || f.Context != 1 || f.Human != 4 {
		t.Fatalf("choice funnel: %+v", f)
	}
	if f.HumanPerThousand() != 667 {
		t.Fatalf("4 of 6 go to a person = 667 per 1000, got %v", f.HumanPerThousand())
	}
	if empty, _ := st.Funnel("none", true); empty.Records != 0 || empty.HumanPerThousand() != 0 {
		t.Fatalf("no records: zero, not a division error: %+v", empty)
	}
}

// 監査 6: 1 つの答えから出る 2 件のラベルは 1 回の書き込みで追記する(途中で失敗して片方だけ残らない)。
func TestAppendLabelsWritesAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	st := Store{Dir: filepath.Join(dir, "s")}
	r := rec("h", "m")
	a, b := LabelFor(&r, "yes", 1, "ui")
	if b != nil {
		t.Fatal(b)
	}
	m := a
	m.SpecHash = "h2"
	if err := st.AppendLabels([]Label{a, m}); err != nil {
		t.Fatal(err)
	}
	if ls, _ := st.Labels("s", "h", "m"); len(ls) != 1 {
		t.Fatalf("both labels land: %+v", ls)
	}
	if ls, _ := st.Labels("s", "h2", "m"); len(ls) != 1 {
		t.Fatalf("both labels land: %+v", ls)
	}
	// 書けない置き場: 1 件も残らない
	bad := Store{Dir: filepath.Join(dir, "file")}
	if err := os.WriteFile(bad.Dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bad.AppendLabels([]Label{a, m}); err == nil {
		t.Fatalf("an unwritable store must fail")
	}
}

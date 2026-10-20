package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestReviewAuditRateFollowsHumanChecks(t *testing.T) {
	reviews, labels := map[string]Review{}, map[string]Label{}
	start := time.Now().Add(-time.Hour)
	for n := 0; n < 20; n++ {
		trace := fmt.Sprintf("%016x", n)
		reviews[trace] = Review{TraceID: trace, InputsHash: trace, Truth: "yes", Sampled: true, At: start.Add(time.Duration(n) * time.Second)}
		labels[trace] = Label{TraceID: trace, Truth: "yes", Source: "audit", At: time.Now()}
	}
	if got := ReviewQuality(reviews, nil); got.Every != 5 || got.Checked != 0 {
		t.Fatal("AI の採点の量で抜き取りを減らさない")
	}
	if got := ReviewQuality(reviews, labels); got.Every != 20 || got.RandomChecked != 20 || got.RandomCorrect != 20 {
		t.Fatalf("人の無作為の確認で安定したら頻度を下げる: %+v", got)
	}
	reviews["unknown"] = Review{TraceID: "unknown", InputsHash: "unknown", Truth: "unknown", Sampled: true, At: start}
	labels["unknown"] = Label{TraceID: "unknown", Truth: "yes", Source: "audit", At: time.Now()}
	if got := ReviewQuality(reviews, labels); got.Uncertain != 1 || got.UncertainChecked != 1 || got.Checked != 20 || got.RandomChecked != 20 {
		t.Fatalf("保留の確認を採点の正誤と分ける: %+v", got)
	}
	delete(reviews, "unknown")
	delete(labels, "unknown")
	reviews["focused"] = Review{TraceID: "focused", InputsHash: "focused", Truth: "yes", At: start}
	labels["focused"] = Label{TraceID: "focused", Truth: "no", Source: "ui", At: time.Now()}
	if got := ReviewQuality(reviews, labels); got.Every != 5 || got.RandomChecked != 20 || got.RandomCorrect != 20 {
		t.Fatalf("無作為でない確認の不一致でも頻度を上げ、無作為の成績は保つ: %+v", got)
	}
	delete(reviews, "focused")
	delete(labels, "focused")
	l := labels["0000000000000001"]
	l.Truth = "no"
	labels[l.TraceID] = l
	if got := ReviewQuality(reviews, labels); got.Every != 5 || got.RandomCorrect != 19 {
		t.Fatalf("不一致が出た種類は確認を増やす: %+v", got)
	}
	// 意図して選んだ難問を大量に確認しても、無作為の枠の成績は置き換えない。
	for n := 20; n < 50; n++ {
		trace := fmt.Sprintf("%016x", n)
		reviews[trace] = Review{TraceID: trace, InputsHash: trace, Truth: "yes", At: time.Now()}
		labels[trace] = Label{TraceID: trace, Truth: "yes", Source: "ui", At: time.Now()}
	}
	if got := ReviewQuality(reviews, labels); got.Every != 5 || got.RandomChecked != 20 {
		t.Fatalf("確認の選び方を混ぜない: %+v", got)
	}
	for n := 0; n < 200; n++ {
		trace := fmt.Sprintf("%016x", n)
		if ReviewSample(trace, 5) != ReviewSample(trace, 5) {
			t.Fatal("同じ判定の抜き取りは変わらない")
		}
		if ReviewSample(trace, 20) && !ReviewSample(trace, 5) {
			t.Fatal("頻度を上げると元の抜き取りも含む")
		}
	}
}

func TestReviewKeepsFirstScoreAndHumanTruth(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	r := Review{TraceID: "0000000000000001", Spec: "done-as-asked", Scope: "/work/a", InputsHash: "same-input", Truth: "no", Reason: "依頼した手順が未完了", Source: Feedback{ID: "0000000000000002", Request: "手順を更新", Summary: "変更した", Reply: "不足がある"}}
	dir := filepath.Join(st.Dir, "records", "reviews")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := os.OpenFile(filepath.Join(dir, r.Spec+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := syscall.Flock(int(guard.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
	finished := make(chan error, 1)
	go func() { _, err := st.SaveReview(r); finished <- err }()
	select {
	case <-finished:
		t.Fatal("別の採点が保存中は同じ履歴の更新を待つ")
	case <-time.After(75 * time.Millisecond):
	}
	_ = syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.SaveReview(r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	path := filepath.Join(st.Dir, "records", "reviews", r.Spec+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, c := range data {
		if c == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("同じ仮採点の並列保存は一度だけ: %d", lines)
	}
	r.Truth = "yes"
	if _, err := st.SaveReview(r); err == nil {
		t.Fatal("後から AI が採点を塗り替えて成績を良くできない")
	}
	human := Label{TraceID: r.TraceID, SpecID: r.Spec, SpecHash: "test-hash", Model: "test-model", Truth: "yes", Source: "audit", At: time.Now()}
	machine := human
	machine.Truth, machine.Source, machine.At = "no", "judge", time.Now().Add(time.Second)
	if err := st.AppendLabel(human); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(machine); err != nil {
		t.Fatal(err)
	}
	latest, err := st.LabeledTraces(r.Spec)
	if err != nil || latest[r.TraceID].Source != "audit" {
		t.Fatal("後の AI のラベルより人の答えを優先する")
	}
}

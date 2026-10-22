package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// 人の手間は「今の規則で人が答える判定」を数える: 結果から正解が付く判定の迷いは数えず、AI の仮採点の抜き取りと保留は数える。
func TestCountPersonWork(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour)
	audit := ""
	for audit == "" {
		if id := store.NewTraceID(); store.AuditSample(id) {
			audit = id
		}
	}
	recs := []store.Record{
		{TraceID: "a1", Band: "ask", At: now},
		{TraceID: "a2", Band: "ask", At: old},
		{TraceID: "d1", Band: "discard", At: now},
		{TraceID: audit, Band: "act", IsCalibrated: true, At: now},
		{TraceID: "r1", Band: "discard", At: now},
		{TraceID: "r2", Band: "discard", At: now},
		{TraceID: "r3", Band: "discard", At: now},
	}
	reviews := map[string]store.Review{"r1": {Sampled: true}, "r2": {Truth: "unknown"}, "r3": {Sampled: true}}
	labs := []store.Label{{TraceID: "a1", Source: "ui"}, {TraceID: "a2", Source: "outcome"}, {TraceID: "r3", Source: "outcome"}, {TraceID: "d1", Source: "judge"}}

	w := countPersonWork("needs-plan", false, recs, labs, reviews, now)
	// 迷い a1/a2、抜き取り audit、仮採点の抜き取り r1・保留 r2(r3 は結果が先に答えた)
	if w.Records != 7 || w.Waits != 5 || w.RecentRecords != 6 || w.RecentWaits != 4 || w.ByPerson != 1 || w.ByResult != 2 {
		t.Fatalf("needs-plan: %+v", w)
	}
	w = countPersonWork("prompt-kind", true, recs, labs, reviews, now)
	// 結果から正解が付く判定の迷いと不確かな discard は人を待たない
	if w.Waits != 3 {
		t.Fatalf("prompt-kind: asks must not wait for a person: %+v", w)
	}
	// 多択の不確かな discard は人へ(r3 も帯で人へ回る)
	if got := countPersonWork("same-issue", true, recs, labs, reviews, now).Waits; got != 7 {
		t.Fatalf("a choice spec sends unsure discards to a person: %d", got)
	}
	var b strings.Builder
	w.print(&b, "  ")
	if !strings.Contains(b.String(), "3 of 7 decisions wait for a person") || !strings.Contains(b.String(), "by results 2") {
		t.Fatalf("print: %s", b.String())
	}
}

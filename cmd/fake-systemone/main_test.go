package main

import (
	"encoding/json"
	"math"
	"testing"
)

// 偽モデルの契約: 同じ入力には同じ答え、語が重なる選択肢が上に来る、分布は正規化されている。
func TestFakeIsDeterministicAndKeywordDriven(t *testing.T) {
	crit, _ := json.Marshal(map[string]string{
		"change_request": "Change something - edit, fix, add, remove. Includes \"fix the typo\".",
		"question":       "Explain or investigate without changing anything. Includes \"why\".",
		"other":          "Not a request - greetings, thanks.",
	})
	q := question{Type: "choice", Instructions: "What is asked?", Criteria: crit}
	a1, err := answer("Fix the typo in README.md", q, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := answer("Fix the typo in README.md", q, 1.0)
	if a1["choice"] != a2["choice"] || !equalProbs(a1["probabilities"], a2["probabilities"]) {
		t.Fatalf("same input must give the same answer: %v vs %v", a1, a2)
	}
	if a1["choice"] != "change_request" {
		t.Fatalf("keyword overlap must rank change_request first for a fix request: %v", a1)
	}
	b, _ := answer("Why does the build fail?", q, 1.0)
	if b["choice"] != "question" {
		t.Fatalf("'why' must rank question first: %v", b)
	}
	var sum float64
	for _, p := range a1["probabilities"].(map[string]float64) {
		sum += p
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("probabilities must sum to 1, got %f", sum)
	}
	// 低い temperature は同じ順位でより端に寄る(較正デモ用)
	c, _ := answer("Fix the typo in README.md", q, 0.35)
	if c["probabilities"].(map[string]float64)["change_request"] <= a1["probabilities"].(map[string]float64)["change_request"] {
		t.Fatalf("lower temperature must sharpen the distribution: %v vs %v", c, a1)
	}
}

func TestFakeScoreAndNoulShapes(t *testing.T) {
	levels, _ := json.Marshal([]string{"unrelated", "same domain", "same case follow-up"})
	s, err := answer("follow-up record of the same case", question{Type: "score", Instructions: "how related?", Criteria: levels}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if s["score"].(float64) < 1.0 || len(s["legend"].(map[string]string)) != 3 {
		t.Fatalf("score must lean to the matching level and carry a legend: %v", s)
	}
	n, err := answer("the message conveys urgency now", question{Type: "noul", Instructions: "the message conveys urgency"}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if p := n["noul"].(float64); p <= 0.5 || p > 1 {
		t.Fatalf("noul with overlapping words must lean yes: %v", n)
	}
	if _, err := answer("x", question{Type: "boolean"}, 1.0); err == nil {
		t.Fatal("unknown type must be rejected")
	}
}

func equalProbs(a, b any) bool {
	ma, ok1 := a.(map[string]float64)
	mb, ok2 := b.(map[string]float64)
	if !ok1 || !ok2 || len(ma) != len(mb) {
		return false
	}
	for k, v := range ma {
		if mb[k] != v {
			return false
		}
	}
	return true
}

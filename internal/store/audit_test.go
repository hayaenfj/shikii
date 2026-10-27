package store

import (
	"fmt"
	"hash/fnv"
	"testing"
	"time"
)

// Spot check: 1 in 20 of the decisions that acted automatically is shown to a person. It is decided by trace_id, so the same decision always gives the same answer.
func TestAuditSampleIsOneInTwentyAndStable(t *testing.T) {
	// Fixed ids, so the count is exact on every run: fnv-1a (32 bits) of the id is a multiple of 20 for 218 of these 4000 ids.
	n := 0
	for i := 0; i < 4000; i++ {
		id := fmt.Sprintf("%016x", i)
		h := fnv.New32a()
		h.Write([]byte(id))
		if want := h.Sum32()%20 == 0; AuditSample(id) != want {
			t.Fatalf("AuditSample(%q) = %v, want %v (fnv-1a of the id modulo 20 is 0)", id, !want, want)
		}
		if AuditSample(id) {
			n++
		}
	}
	if n != 218 {
		t.Fatalf("218 of the 4000 fixed ids are sampled (about 1 in 20), got %d", n)
	}
	if AuditSample("") {
		t.Fatalf("an empty trace is never sampled")
	}
}

// Separate people's truths from machine truths: the only machine-decided truth is the upper step's answer (context). Truths from re-asking (replay) and files (file) were given by people.
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

// audit round 4, item 2: if one input has both a person's truth and a machine truth (judge / context), the person's wins regardless of age. Between people's truths, the newer one.
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

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Synthetic regression fixture; the original evidence is retained.
func auditDeliveryRun(t *testing.T, b notebook.Book, task, evidence string, sufficient *bool, budget int) (map[string]any, bool) {
	t.Helper()
	p, err := workCall(t, b, "prepare", map[string]any{"request": task, "query": "Reading source recovery", "max_bytes": budget})
	if err != nil {
		t.Fatal("\u6e96\u5099\u5931\u6557", err)
	}
	anyAdvice := false
	for i := 0; i < 3; i++ {
		next, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "max_bytes": budget})
		if e != nil {
			t.Fatal("\u62e1\u5f35\u5931\u6557", e)
		}
		if raw, ok := next["attention"]; ok && raw != nil {
			a := loopObject(t, raw)
			if a["loop"] != nil {
				anyAdvice = true
			}
		}
	}
	result := map[string]any{"changed": "\u5408\u6210\u306e\u78ba\u8a8d\u7d50\u679c\u3092\u4fdd\u5b58\u3057\u305f\u3002", "evidence": []string{"new1"}}
	if sufficient != nil {
		result["sufficient"] = *sufficient
	}
	end, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(evidence)}, "result": result})
	if e != nil || end["complete"] != true {
		t.Fatal("\u7d50\u679c\u4fdd\u5b58\u5931\u6557", e, end)
	}
	return end, anyAdvice
}

func TestWorkRegressionProcedureUndeliveredAdvice(t *testing.T) {
	for _, long := range []bool{false, true} {
		name := "\u77ed\u3044\u4f9d\u983c_\u5bfe\u7167"
		if long {
			name = "\u9577\u3044\u4f9d\u983c_\u62d2\u5426"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			yes := true
			task := func(i int) string {
				s := fmt.Sprintf("\u5408\u6210\u8ab2\u984c%d\u3002", i)
				if long {
					s += strings.Repeat("\u8aac\u660e\u7528\u306e\u5408\u6210\u6761\u4ef6\u3002", 550)
				}
				return s
			}
			if long {
				// The request can no longer be omitted to make room for advice.
				// Reject this former omission case before any quality report.
				p, e := workCall(t, b, "prepare", map[string]any{"request": task(0), "query": "Reading source recovery", "max_bytes": 8192})
				if e == nil || !strings.Contains(e.Error(), "max_bytes") || p != nil {
					t.Fatal("oversized task must fail before validation or adoption", e)
				}
				return
			}
			gen, gdel := auditDeliveryRun(t, b, task(0), "\u5408\u6210\u306e\u751f\u6210\u5143\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002", nil, 8192)
			t.Logf("\u751f\u6210 status=%v \u52a9\u8a00\u914d\u4fe1=%v", loopObject(t, gen["procedure"])["status"], gdel)
			first, fdel := auditDeliveryRun(t, b, task(1), "\u5408\u6210\u306e\u72ec\u7acb\u6761\u4ef6A\u3092\u78ba\u8a8d\u3057\u305f\u3002", &yes, 8192)
			last, ldel := auditDeliveryRun(t, b, task(2), "\u5408\u6210\u306e\u72ec\u7acb\u6761\u4ef6B\u3092\u78ba\u8a8d\u3057\u305f\u3002", &yes, 8192)
			v := loopObject(t, last["procedure"])
			t.Logf("\u691c\u8a3c1 cases=%v \u52a9\u8a00\u914d\u4fe1=%v \u691c\u8a3c2 status=%v cases=%v saved_bytes=%v \u52a9\u8a00\u914d\u4fe1=%v", loopObject(t, first["procedure"])["cases"], fdel, v["status"], v["cases"], v["saved_bytes"], ldel)
			raw, e := b.ReadSources([]string{v["source"].(string)})
			if e != nil {
				t.Fatal(e)
			}
			var sample notebook.ProcedureSample
			if e = json.Unmarshal([]byte(raw["observations"].([]notebook.Observation)[0].Content), &sample); e != nil {
				t.Fatal(e)
			}
			t.Logf("\u4fdd\u5b58\u3055\u308c\u305f\u6bd4\u8f03 baseline_bytes=%d actual_bytes=%d mixed=%v", sample.Trace.BaselineBytes, sample.Trace.ActualBytes, sample.Trace.Mixed)
			if !long && (!fdel || !ldel || v["status"] != "adopted") {
				t.Fatal("\u5bfe\u7167\u304c\u6210\u7acb\u3057\u306a\u3044")
			}
			if long && !fdel && !ldel && v["status"] == "adopted" {
				t.Fatal("RSI-R2-B01: \u672a\u914d\u4fe1\u306e\u52a9\u8a00\u304c\u8cbb\u7528\u524a\u6e1b\u306e\u691c\u8a3c\u3092\u901a\u904e\u3057\u3066\u63a1\u7528\u3055\u308c\u305f")
			}
		})
	}
}

func TestWorkRegressionProcedureBudgetRecovery(t *testing.T) {
	for _, cross := range []bool{false, true} {
		name := "\u67a0\u5185_\u5bfe\u7167"
		if cross {
			name = "\u67a0\u8d85\u904e"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			p := readingLearningPrepare(t, b, "\u5408\u6210\u306e\u4e0a\u9650\u5883\u754c\u8ab2\u984c\u3002", "")
			token, e := workDecode(p["receipt"].(string))
			if e != nil {
				t.Fatal(e)
			}
			dir, e := openWorkSessions(b.Root)
			if e != nil {
				t.Fatal(e)
			}
			defer dir.Close()
			w, name, e := loadWorkState(dir, b.Scope, token)
			if e != nil {
				t.Fatal(e)
			}
			// Synthetic regression fixture; the original evidence is retained.
			n := (16 << 20) - 10000
			if cross {
				n = (16 << 20) - 1
			}
			w.Reasoning.Procedure.Trace.BaselineBytes = n
			w.Reasoning.Procedure.Trace.ActualBytes = n
			if e = saveWorkState(dir, name, b.Scope, token.Turn, w, false); e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 3; i++ {
				if _, e = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
					t.Fatal(e)
				}
			}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "\u5408\u6210\u306e\u539f\u7d50\u679c\u3092\u4fdd\u5b58\u3057\u305f\u3002"}})
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("\u4e0a\u9650\u8d85\u904e=%v complete=%v storage=%v errors=%v", cross, first["complete"], first["storage"], first["errors"])
			if !cross {
				if first["complete"] != true {
					t.Fatal("\u67a0\u5185\u5bfe\u7167\u304c\u5b8c\u4e86\u3057\u306a\u3044")
				}
				return
			}
			for i := 0; i < 2; i++ {
				next, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
				if e != nil {
					t.Fatal(e)
				}
				t.Logf("\u81ea\u52d5\u56de\u5fa9%d=%v", i+1, next["procedure_recovery"])
			}
			w, _, e = loadWorkState(dir, b.Scope, token)
			if e != nil {
				t.Fatal(e)
			}
			trace := w.Reasoning.Procedure.Pending.Trace
			t.Logf("\u5883\u754c\u5f8c baseline_bytes=%d actual_bytes=%d safe=%v pending=%v", trace.BaselineBytes, trace.ActualBytes, trace.Safe, pendingWorkProcedure(w))
			if first["complete"] == false && pendingWorkProcedure(w) {
				t.Fatal("RSI-R2-B02: \u4e0a\u9650\u5f8c\u306e\u56fa\u5b9a\u6e08\u307ftrace\u3092\u81ea\u52d5\u518d\u9001\u3057\u7d9a\u3051\u3001\u539f\u7d50\u679c\u3092\u5b8c\u4e86\u3067\u304d\u306a\u3044")
			}
		})
	}
}

func TestWorkRegressionProcedureCanonicalEvidence(t *testing.T) {
	for _, equivalent := range []bool{false, true} {
		name := "\u540c\u4e00\u6587\u5b57\u5217_\u5bfe\u7167"
		if equivalent {
			name = "\u6b63\u898f\u7b49\u4fa1\u306e\u5225\u8868\u73fe"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			yes := true
			original := "\u5408\u6210\u306e\u30ac\u30fc\u30c9\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002"
			same := original
			if equivalent {
				same = "\u5408\u6210\u306e\u30ab\u3099\u30fc\u30c8\u3099\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002"
			}
			autonomyRun(t, b, "\u5408\u6210\u306e\u751f\u6210\u5143\u8ab2\u984c\u3002", original, nil)
			_, a := autonomyRun(t, b, "\u5408\u6210\u306e\u5225\u540d\u8ab2\u984c\u3002", same, &yes)
			_, last := autonomyRun(t, b, "\u5408\u6210\u306e\u72ec\u7acb\u8ab2\u984c\u3002", "\u5408\u6210\u306e\u5225\u306e\u5408\u8a08\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002", &yes)
			av, lv := loopObject(t, a["procedure"]), loopObject(t, last["procedure"])
			t.Logf("\u6b63\u898f\u7b49\u4fa1\u306e\u5225\u8868\u73fe=%v \u751f\u6210\u5143\u518d\u5229\u7528cases=%v \u6700\u7d42status=%v cases=%v", equivalent, av["cases"], lv["status"], lv["cases"])
			if !equivalent && (av["cases"] != float64(0) || lv["status"] == "adopted") {
				t.Fatal("\u5bfe\u7167\u4e0d\u6210\u7acb")
			}
			if equivalent && lv["status"] == "adopted" {
				t.Fatal("RSI-R2-B03: \u751f\u6210\u5143\u3068\u6b63\u898f\u7b49\u4fa1\u306a\u540c\u3058\u7d50\u679c\u304c\u72ec\u7acb\u306a\u652f\u6301\u3068\u306a\u3063\u305f")
			}
		})
	}
}

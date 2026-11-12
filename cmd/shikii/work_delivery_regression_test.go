package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func auditContractHTTP(t *testing.T, n int) (int, int, int) {
	t.Helper()
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Routing policy", "body": strings.Repeat("Routing checks the synthetic owner condition.\n", n)})
	raw, _ := json.Marshal(map[string]any{"operation": "prepare", "work": map[string]any{"request": "Check synthetic routing.", "query": "routing", "max_bytes": 8192}})
	h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
	req := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	compact, _ := json.Marshal(v)
	return rec.Code, len(compact), rec.Body.Len()
}

func TestWorkDeliveryRegressionC01HTTPBudget(t *testing.T) {
	code, compact, wire := auditContractHTTP(t, 55)
	if code != 200 {
		t.Fatal("\u6e96\u5099\u5931\u6557", code)
	}
	t.Logf("max_bytes=8192 compact=%d wire=%d status=%d", compact, wire, code)
	if wire > 8192 {
		t.Errorf("C1\u9055\u53cd: \u6210\u529f\u3057\u305fHTTP\u5fdc\u7b54\u304cmax_bytes\u3092\u8d85\u3048\u305f: %d > 8192", wire)
	}
}

func TestWorkDeliveryRegressionC01Control(t *testing.T) {
	code, compact, wire := auditContractHTTP(t, 25)
	t.Logf("max_bytes=8192 compact=%d wire=%d status=%d", compact, wire, code)
	if code != 200 || wire > 8192 {
		t.Fatal("\u8fd1\u3044\u5bfe\u7167\u304c\u4e0a\u9650\u5185\u306b\u306a\u3089\u306a\u3044", code, wire)
	}
}

func auditContractSource(t *testing.T, titleBytes, budget int) (int, error, string) {
	t.Helper()
	b, _ := loopBook(t)
	o, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: strings.Repeat("<", titleBytes), Content: strings.Repeat("\u78ba\u8a8d\u3057\u305f\u6761\u4ef6\u3002", 700), Reference: strings.Repeat("r", 1000), Version: strings.Repeat("v", 200), ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p, err := workCall(t, b, "prepare", map[string]any{"request": "Check source cursor.", "query": "source"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": o.ID, "max_bytes": budget})
	if err != nil {
		t.Fatal("\u6e96\u5099\u5931\u6557", err)
	}
	src := v["source"].(map[string]any)
	body := src["observations"].([]any)[0].(map[string]any)["content"].(string)
	cursor, _ := src["next_cursor"].(string)
	if cursor != "" {
		_, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": o.ID, "cursor": cursor, "max_bytes": budget})
	}
	return len(body), err, fmt.Sprint(v["output"])
}

func TestWorkDeliveryRegressionC02SourceCursor(t *testing.T) {
	n, err, _ := auditContractSource(t, 150, 8192)
	t.Logf("title_bytes=150 max_bytes=8192 body_bytes=%d continuation_error=%v", n, err)
	if n == 0 || err != nil {
		t.Errorf("C2\u9055\u53cd: \u539f\u8cc7\u6599\u3092\u8fd4\u3059cursor\u304c\u9032\u307e\u306a\u3044: body=%d err=%v", n, err)
	}
}

func TestWorkDeliveryRegressionC02Control(t *testing.T) {
	for _, c := range []struct{ title, budget int }{{100, 8192}, {150, 16384}} {
		t.Run(fmt.Sprintf("title%d-budget%d", c.title, c.budget), func(t *testing.T) {
			n, err, _ := auditContractSource(t, c.title, c.budget)
			t.Logf("body_bytes=%d continuation_error=%v", n, err)
			if n == 0 || err != nil {
				t.Fatal("\u8fd1\u3044\u5bfe\u7167\u304c\u9032\u307e\u306a\u3044", n, err)
			}
		})
	}
}

func auditContractCorrection(t *testing.T, result, evidence string) map[string]any {
	t.Helper()
	st, scope, h, key := correctionSeed(t)
	report, err := st.MemoryReport(scope)
	if err != nil || len(report.All) == 0 {
		t.Fatal("\u6e96\u5099\u5931\u6557", err)
	}
	id := report.All[0].SourceID
	in := map[string]string{"source_id": id, "key": key, "result": result, "correction_result": "repeated"}
	if evidence != "" {
		in["decision"] = "Checked the synthetic correction recurrence."
		in["evidence"] = evidence
	}
	r := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{in}})
	if r["complete"] == true {
		saved, e := st.MemoryAssessments(scope)
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range saved {
			if a.SourceID == id && a.Key == key {
				t.Logf("saved_result=%s correction=%s decision=%q evidence=%q", a.Result, a.CorrectionResult, a.Decision, a.Evidence)
			}
		}
	}
	return r
}

func TestWorkDeliveryRegressionC03CorrectionEvidence(t *testing.T) {
	r := auditContractCorrection(t, "unused", "")
	if r["complete"] == true {
		t.Error("C5\u9055\u53cd: \u8a02\u6b63\u518d\u767a\u306e\u5224\u65ad\u30fb\u8a3c\u62e0\u3092\u7701\u7565\u3057\u305f\u5165\u529b\u304c\u6210\u529f\u3057\u305f")
	}
}

func TestWorkDeliveryRegressionC03Control(t *testing.T) {
	r := auditContractCorrection(t, "unused", "The synthetic check found the same condition recurring.")
	if r["complete"] != true {
		t.Fatal("\u6839\u62e0\u3092\u6307\u5b9a\u3057\u305f\u8fd1\u3044\u5bfe\u7167\u304c\u5931\u6557\u3057\u305f", r)
	}
}

func auditContractRecoveryWithTrials(t *testing.T, fieldBytes int) map[string]any {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Check recovery with trial context.", "")
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "knowledge1"), nil)
	for i := 0; i < 3; i++ {
		if _, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		plan := trialPlan("small", "new1")
		for _, key := range []string{"case", "question", "expected", "method", "conditions", "decision"} {
			plan[key] = strings.Repeat("c", fieldBytes) + fmt.Sprint(i)
		}
		created := trialRecord(t, b, p["receipt"], map[string]any{"plan": plan}, map[string]any{"observations": []any{trialObservation(fmt.Sprintf("Synthetic plan evidence %d.", i))}})
		ref := trialSavedRef(t, created, "trial")
		trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "The synthetic condition held.", "evidence": []string{"new1"}}}, map[string]any{"observations": []any{trialObservation(fmt.Sprintf("Synthetic result evidence %d.", i))}})
	}
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "procedure" {
			return errors.New("synthetic pending index")
		}
		return nil
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked synthetic condition."}})
	workAfterWrite = old
	if err != nil || r["complete"] != false {
		t.Fatal("setup", r, err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "max_bytes": 8192})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("size=%d recovery=%v output=%v", workContextSize(v), v["procedure_recovery"], v["output"])
	return v
}

func TestWorkDeliveryRegressionC04RecoveryBudget(t *testing.T) {
	v := auditContractRecoveryWithTrials(t, 1100)
	if workContextSize(v) > 8192 {
		t.Errorf("\u4e0a\u9650\u691c\u67fb\u5f8c\u306e\u56de\u5fa9\u3067\u4e0a\u9650\u3092\u8d85\u3048\u305f: %d", workContextSize(v))
	}
}

func TestWorkDeliveryRegressionC04Control(t *testing.T) {
	v := auditContractRecoveryWithTrials(t, 100)
	if workContextSize(v) > 8192 || v["procedure_recovery"].(map[string]any)["status"] != "saved" {
		t.Fatal("\u8fd1\u3044\u5bfe\u7167\u304c\u4e88\u7b97\u5185\u3067\u56de\u5fa9\u3057\u306a\u3044", workContextSize(v))
	}
}

func TestWorkDeliverySourcePagesMakeCompleteProgress(t *testing.T) {
	b, _ := loopBook(t)
	body := strings.Repeat("\u78ba\u8a8d\u3057\u305f\u6761\u4ef6\u3002", 700)
	o, e := b.CaptureObserved(notebook.Observation{Kind: "test", Title: strings.Repeat("<", 150), Content: body, Reference: strings.Repeat("r", 1000), Version: strings.Repeat("v", 200), ObservedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	p, e := workCall(t, b, "prepare", map[string]any{"request": "Recover the complete synthetic UTF-8 source.", "query": "source"})
	if e != nil {
		t.Fatal(e)
	}
	cursor, joined := "", ""
	seen := map[string]bool{}
	for page := 0; page < 64; page++ {
		v, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": o.ID, "cursor": cursor, "max_bytes": 8192})
		if e != nil {
			t.Fatal(e)
		}
		if workContextSize(v)+1 > 8192 {
			t.Fatal("the final encoded response exceeds its budget")
		}
		source := v["source"].(map[string]any)
		part := source["observations"].([]any)[0].(map[string]any)["content"].(string)
		if part == "" {
			t.Fatal("a successful source page must advance")
		}
		joined += part
		cursor, _ = source["next_cursor"].(string)
		if cursor == "" {
			if joined != body {
				t.Fatal("paged recovery changed the original body")
			}
			return
		}
		if seen[cursor] {
			t.Fatal("a source cursor repeated without progress")
		}
		seen[cursor] = true
	}
	t.Fatal("the bounded synthetic source did not finish")
}

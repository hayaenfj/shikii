package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// A receipt without hook ids has no words of the person to quote. What the refusals of agreed say there.
func TestAuditGAgreedOnAStandaloneReceipt(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prepared, _, err := auditDCLI(t, `{"query":"CSV export","request":"Keep the CSV filters."}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt := auditJSON(prepared["receipt"])
	for _, input := range []string{
		`{"receipt":` + receipt + `,"agreed":{"summary":"Keep the filters."}}`,
		`{"receipt":` + receipt + `,"agreed":{"summary":"Keep the filters.","quotes":[{"ref":"current","text":"Keep the CSV filters."}]}}`,
	} {
		saved, _, err := auditDCLI(t, input, "work", "record")
		text := auditJSON(saved["errors"])
		if err != nil {
			text = err.Error()
		}
		t.Logf("%.90s -> %.400s", input[len(receipt)+12:], text)
		if strings.Contains(text, "ref one of ,") || strings.HasSuffix(strings.TrimRight(text, `"}]`), "the references here: ") || strings.Contains(text, "the references here: \"") {
			t.Errorf("the refusal names an empty list of references for a receipt that has none: %s", text)
		}
	}
}

// The body limit of the entries that read with decodeObject: every one answers 413 and names the limit in bytes, never 400 "bad json".
func TestAuditGBodyOverTheLimitAtEveryPassingOverEntry(t *testing.T) {
	_, _, h, _, _ := memoryEffectFixture(t)
	body := "{" + strings.Repeat(" ", maxBody+10) + "}"
	for _, path := range []string{"/v1/decide", "/v1/label", "/v1/redo", "/v1/answers/restore", "/v1/tune/run", "/v1/tune/adopt", "/v1/tune/review", "/v1/tune/variant", "/v1/wiki/claim"} {
		r := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(r, req)
		got := strings.Join(strings.Fields(r.Body.String()), " ")
		if r.Code != 413 || strings.Contains(got, "bad json") || !strings.Contains(got, "body larger than "+strconv.Itoa(maxBody)+" bytes") {
			t.Errorf("%-20s a body over %d bytes -> %d %s", path, maxBody, r.Code, got)
		}
	}
}

// The clean-up of a refused preparation without hook ids: it takes away its own record and lock and nothing else; a refused
// preparation with hook ids keeps the session's record; a preparation that is not refused keeps its record.
func TestAuditGRefusedPreparationsCleanOnlyTheirOwn(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	kept, _, err := auditDCLI(t, `{"query":"CSV export","request":"Keep the CSV filters."}`, "work", "prepare")
	if err != nil || kept["receipt"] == nil {
		t.Fatal(err)
	}
	before := auditETree(t, f.root)
	sessionBefore, _ := readFileString(sessionPath(f.root, "planning"))
	big, _ := json.Marshal("Check the export page: " + strings.Repeat("<td>&</td>", 1500))
	_, _, err = auditDCLI(t, `{"query":"export page table","request":`+string(big)+`,"max_bytes":8192}`, "work", "prepare")
	if err == nil || !strings.Contains(err.Error(), "exceed max_bytes") {
		t.Fatalf("precondition: the standalone preparation is refused: %v", err)
	}
	after := auditETree(t, f.root)
	added, removed := auditENew(before, after), auditENew(after, before)
	t.Logf("a refused standalone preparation: added %v, removed %v", added, removed)
	for _, name := range append(added, removed...) {
		if strings.HasPrefix(name, "sessions/") {
			t.Errorf("a refused standalone preparation changed the sessions folder: %s", name)
		}
	}
	// with hook ids, refused by a bound it cannot meet
	withIDs := strings.TrimSuffix(auditJSON(ids), "}") + `,"query":"CSV export filters","max_bytes":8192}`
	_, _, err = auditDCLI(t, withIDs, "work", "prepare")
	sessionAfter, _ := readFileString(sessionPath(f.root, "planning"))
	t.Logf("a preparation with hook ids and max_bytes 8192: err=%v; the session record kept: %v", err, sessionAfter != "")
	if sessionAfter == "" || err != nil && sessionAfter != sessionBefore {
		t.Errorf("a refused preparation with hook ids changed or removed the session's record")
	}
}

func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

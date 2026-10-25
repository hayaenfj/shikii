package main

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// Audit 0.9.2 (behavior), low 2: the agent's adoption and the page's adoption of the same run share one lock.

// Low 2: the agent's adoption (hook variants check, another process) and the page's adoption could run at the same time.
// While the agent adopts (calibration takes time), the page's adopt and review are refused with 409.
func TestAuditPageAdoptDuringAgentAdoption(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	srv := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent}))
	t.Cleanup(srv.Close)
	inner := wordingAdopt
	var adoptCode, reviewCode int
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		adoptCode, _ = postJSON(t, srv.URL+"/v1/tune/adopt", map[string]any{"spec": res.SpecID, "run": res.Run, "meaning_same": true})
		reviewCode, _ = postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": true})
		return inner(r, ef, storeDir, labeled, reports...)
	}
	if out, err := submitWordingCheck(t, checkBody(res, "same")); err != nil || out["adopted"] != true {
		t.Fatalf("the agent's adoption: %v %v", out, err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if adoptCode != 409 || reviewCode != 409 {
		t.Fatalf("while the agent adopts, the page is refused: adopt %d, review %d (checks %d, adopted=%v)", adoptCode, reviewCode, len(got.MeaningChecks), got.Adopted)
	}
	if !got.Adopted || len(got.MeaningChecks) != 1 {
		t.Fatalf("only the agent's adoption is kept: %+v", got)
	}
}

// Low 2, the other way: while the page adopts a run, the agent's check of the same run is refused (it does not adopt it a second time).
func TestAgentCheckRefusedWhilePageAdopts(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	unlock, err := lockTuneRun(filepath.Join(agent, "wiki"), res.Run)
	if err != nil {
		t.Fatal(err)
	}
	_, err = submitWordingCheck(t, checkBody(res, "same"))
	unlock()
	if err == nil || !strings.Contains(err.Error(), "being adopted") {
		t.Fatalf("refused while the run is being adopted: %v", err)
	}
	if got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run); got.Adopted || len(got.MeaningChecks) != 0 {
		t.Fatalf("nothing is recorded while refused: %+v", got)
	}
	if out, err := submitWordingCheck(t, checkBody(res, "same")); err != nil || out["adopted"] != true {
		t.Fatalf("after the lock is released, the check adopts: %v %v", out, err)
	}
}

package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit 0.9.2 (behavior), medium 1 / medium 2 / low 1: a tune result (wiki/runs/<run>.json) keeps absolute paths
// from when it was written. After the project folder moved or was copied, every reader and writer must stay inside this project.

// rewriteRunJSON changes the run's json on disk as it was written (without the rebuilding loadTuneResult does).
func rewriteRunJSON(t *testing.T, agent, run string, change func(*tuneResult)) {
	t.Helper()
	path := filepath.Join(agent, "wiki", "runs", run+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var res tuneResult
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	change(&res)
	out, _ := json.MarshalIndent(&res, "", "  ")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

const movedStore = "/nonexistent/old-name/.shikii"

// Medium 1: after the folder moved, the result's run_page still pointed at the old place, so the agent's adoption and check were not saved
// (the new version was in use, but the person was not told and could not undo it).
func TestAuditMovedFolderAdoptionIsSaved(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.SpecPath = filepath.Join(movedStore, "decisions", filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join(movedStore, "wiki", "runs", filepath.Base(r.RunPage))
	})
	out, err := submitWordingCheck(t, checkBody(res, "same"))
	if err != nil || out["adopted"] != true {
		t.Fatalf("adopt: %v %v", out, err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if !got.Adopted || !got.AutoAdopted || len(got.MeaningChecks) != 1 {
		t.Fatalf("the adoption and the check are kept in this wiki's result: adopted=%v auto=%v checks=%d (in use: %s)", got.Adopted, got.AutoAdopted, len(got.MeaningChecks), hookSpecPath(agent, res.SpecID))
	}
	if md, _ := os.ReadFile(res.RunPage); !strings.Contains(string(md), "**Adopted**") {
		t.Fatalf("the note goes to this wiki's run page: %q", md)
	}
	if s := wordingSummary(agent); s == "" {
		t.Fatal("the person is told about the adoption")
	}
}

// Medium 2: in a copied project the recorded paths still point at the original project, which still exists. Adoption wrote a new version
// into the original project's decisions/ and its wiki. It must read and write only this project.
func TestAuditCopiedProjectAdoptsInsideThisProject(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	other := filepath.Join(t.TempDir(), ".shikii")
	od, ow := filepath.Join(other, "decisions"), filepath.Join(other, "wiki", "runs")
	for _, d := range []string{od, ow} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(res.SpecPath)
	_ = os.WriteFile(filepath.Join(od, filepath.Base(res.SpecPath)), b, 0o600)
	_ = os.WriteFile(filepath.Join(ow, res.Run+".md"), []byte("# run\n"), 0o600)
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.SpecPath = filepath.Join(od, filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join(ow, res.Run+".md")
	})
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
		t.Fatal(err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if !got.Adopted || !strings.HasPrefix(got.AdoptedPath, agent+string(filepath.Separator)) {
		t.Fatalf("the new version is written inside this project: adopted=%v %s", got.Adopted, got.AdoptedPath)
	}
	if files, _ := filepath.Glob(filepath.Join(od, "*-v*.yaml")); len(files) > 0 {
		t.Fatalf("nothing is written into the original project's decisions: %v", files)
	}
	if _, err := os.Stat(filepath.Join(ow, res.Run+".json")); err == nil {
		t.Fatal("nothing is written into the original project's wiki")
	}
	if md, _ := os.ReadFile(filepath.Join(ow, res.Run+".md")); string(md) != "# run\n" {
		t.Fatalf("the original project's run page is untouched: %q", md)
	}
}

// Low 1 (and the UI audit's medium 2): undo on the page for a wording the agent adopted before the folder moved returned 500
// (it renamed the old absolute path).
func TestAuditRevertAfterMove(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
		t.Fatal(err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	real := got.AdoptedPath
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.AdoptedPath = filepath.Join(movedStore, "decisions", filepath.Base(r.AdoptedPath))
		r.SpecPath = filepath.Join(movedStore, "decisions", filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join(movedStore, "wiki", "runs", filepath.Base(r.RunPage))
	})
	srv := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent}))
	t.Cleanup(srv.Close)
	if code, r := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": false}); code != 200 || r["reverted"] != true {
		t.Fatalf("undo after the folder moved: %d %v (in use: %s)", code, r, hookSpecPath(agent, res.SpecID))
	}
	if _, err := os.Stat(real); err == nil {
		t.Fatal("the adopted file in this project is no longer used")
	}
	if _, err := os.Stat(real + ".reverted"); err != nil {
		t.Fatal("the adopted file is kept as .reverted")
	}
	if after, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run); !after.Reverted || after.AdoptedPath != real {
		t.Fatalf("the undo is kept in this wiki's result, with this project's path: %+v", after)
	}
}

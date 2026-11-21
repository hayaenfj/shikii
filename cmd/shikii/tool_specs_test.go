package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// embeddedSpecIDs are the ids of the ready-made questions in the binary.
func embeddedSpecIDs(t *testing.T) []string {
	t.Helper()
	var ids []string
	err := fs.WalkDir(shikii.AgentSpecsFS, "integrations/claude-code/specs", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ids = append(ids, strings.TrimSuffix(filepath.Base(p), ".yaml"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// unlistedSpecs names the embedded questions that are in none of the lists, or in more than one. Each ready-made question belongs to
// exactly one list, because the list decides when it is asked and whether the Stop hook draws its line.
func unlistedSpecs(embedded []string, lists ...[]string) []string {
	var problems []string
	for _, id := range embedded {
		n := 0
		for _, l := range lists {
			if slices.Contains(l, id) {
				n++
			}
		}
		if n != 1 {
			problems = append(problems, fmt.Sprintf("%s is in %d lists", id, n))
		}
	}
	for _, l := range lists {
		for _, id := range l {
			if !slices.Contains(embedded, id) {
				problems = append(problems, id+" is listed but not embedded")
			}
		}
	}
	return problems
}

func TestEveryReadyMadeQuestionIsInExactlyOneList(t *testing.T) {
	if p := unlistedSpecs(embeddedSpecIDs(t), hookSpecs, wikiSpecs, deliverySpecs, toolSpecs); len(p) > 0 {
		t.Fatalf("every embedded question is in exactly one list: %v", p)
	}
}

func TestUnlistedSpecsNamesWhatIsWrong(t *testing.T) {
	got := unlistedSpecs([]string{"a", "b", "c"}, []string{"a", "b"}, []string{"b", "gone"})
	want := []string{"b is in 2 lists", "c is in 0 lists", "gone is listed but not embedded"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// quote-supports is asked by a tool (chiten) about an excerpt of a file in the person's project, so it stays on this machine unless
// the project adds hosted itself.
func TestQuoteSupportsIsSeededAndStaysLocal(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, filepath.Join(dir, "quote-supports.yaml"))
	if string(sp.Type) != "noul" {
		t.Fatalf("a yes/no question: %s", sp.Type)
	}
	if !slices.Equal(sp.Policy.SendTo, []string{"local"}) {
		t.Fatalf("an excerpt of the person's files is not sent to hosted by default: %v", sp.Policy.SendTo)
	}
	// before a line is drawn, the local model put passages that do support a claim at 0.44 to 0.53 and contradicted ones at 0.15 or
	// below (2026-11-21). Under the usual ask of 0.55 a supporting passage read as "does not support"; below 0.44 it stays unsure.
	if sp.Thresholds.Ask >= 0.44 || sp.Thresholds.Ask <= 0.15 {
		t.Fatalf("ask sits between the contradicted and the supporting passages of an uncalibrated model: %v", sp.Thresholds.Ask)
	}
	for _, in := range []string{"claim", "quote", "context"} {
		if !slices.Contains(sp.State.Required, in) {
			t.Fatalf("%s is a required input: %v", in, sp.State.Required)
		}
	}
}

// The person answers quote-supports on the page; once min_n truths are in, the Stop hook draws its line like the wiki's questions.
func TestTheStopHookDrawsTheLineForTheToolQuestions(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "q", "cwd": repo, "prompt": "explain the parser"})
	st := store.Store{Dir: agent}
	for _, id := range toolSpecs {
		sp := mustLoad(t, lowerMinN(t, agent, id, 2))
		for i, truth := range []string{"yes", "no"} {
			inputs := map[string]string{"claim": fmt.Sprintf("claim %d", i), "quote": "q", "context": "c"}
			at := time.Now().UTC()
			rec := store.Record{TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", Inputs: inputs, InputsHash: store.HashInputs(inputs), Top: "yes", P: .6, Band: "ask", At: at}
			if err := st.AppendRecord(rec); err != nil {
				t.Fatal(err)
			}
			if err := st.AppendLabel(store.Label{TraceID: rec.TraceID, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "test-model", InputsHash: rec.InputsHash, Inputs: inputs, Truth: truth, Weight: 1, Source: "ui", At: at}); err != nil {
				t.Fatal(err)
			}
		}
	}
	var asked []string
	oldCal, oldGap := hookCalibrate, hookCalibrateGap
	hookCalibrate = func(agent, path, storeDir, model string) { asked = append(asked, filepath.Base(path)) }
	hookCalibrateGap = 0
	t.Cleanup(func() { hookCalibrate, hookCalibrateGap = oldCal, oldGap })
	runHook(t, "stop", map[string]any{"session_id": "q", "cwd": repo, "last_assistant_message": "Explained."})
	for _, id := range toolSpecs {
		if !slices.Contains(asked, id+".yaml") {
			t.Fatalf("the Stop hook draws the line for %s once its truths reach min_n (it tried: %v)", id, asked)
		}
		// once drawn, the line notice names the question by its title, like the other ready-made questions
		path := hookSpecPath(agent, id)
		sp := mustLoad(t, path)
		sp.Calibration = []spec.Calibration{{Model: "test-model", SpecHash: sp.Hash(), Method: "platt", N: 2}}
		if err := sp.Save(); err != nil {
			t.Fatal(err)
		}
		if line := lineSummary(agent); !strings.Contains(line, questionTitle(sp, "en")) {
			t.Fatalf("the line notice names %s by its title %q: %q", id, questionTitle(sp, "en"), line)
		}
	}
}

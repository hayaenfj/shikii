package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Tests of audit D of 0.31.0 (second round) for the work receipt: what a receipt without hook ids is told to cite, replies that do
// not fit, and what the same request prepared again keeps. Each failed on the candidate, or on a copy with the named line taken out.

var auditDIDs = regexp.MustCompile(`\{"session_id":"[^"]*","turn_id":"[0-9a-f]{16}"\}`)

// auditDCLI runs a command an agent calls with input on standard input.
func auditDCLI(t *testing.T, input string, args ...string) (map[string]any, string, error) {
	t.Helper()
	var out string
	var err error
	withStdin(t, input, func() { out, err = captureErr(func() error { return cliCall(args) }) })
	var reply map[string]any
	_ = json.Unmarshal([]byte(out), &reply)
	return reply, out, err
}

// A-1 [INVARIANT 10]: a receipt without hook ids whose reply was shortened is told to cite current (work_budget.go:96-98 puts the
// text with current back over the one work.go:584-586 made for such a receipt). The help's second form, as written, in a wiki of 40
// lessons.
func TestAuditD_A1_StandaloneShortenedReplyOffersCurrent(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", 150)
		applies := fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 12)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("CSV export filters %d", i), Lesson: "Keep the filters when exporting CSV.", AppliesTo: applies, Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{
		`{"query":"CSV export filters","request":"Check the CSV export"}`,                  // the help's second form, the CLI's default bound
		`{"query":"CSV export filters","request":"Check the CSV export","max_bytes":8192}`, // the smallest bound
		`{"query":"zzzz nothing matches","request":"Check the CSV export"}`,                // control: a reply that fits (passes)
	} {
		prepared, out, err := auditDCLI(t, input, "work", "prepare")
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		how, _ := prepared["how_to_record"].(string)
		shortened := prepared["output"] != nil
		t.Logf("%s -> %d bytes, shortened=%v, how_to_record names current=%v", input, len(out), shortened, strings.Contains(how, "(current,"))
		if strings.Contains(how, "(current,") {
			t.Errorf("a receipt without hook ids is told to cite current (shortened=%v): %.210s", shortened, how)
			receipt, _ := json.Marshal(prepared["receipt"])
			saved, _, _ := auditDCLI(t, `{"receipt":`+string(receipt)+`,"result":{"changed":"Nothing.","checked":"Read the export.","evidence":["current"]}}`, "work", "record")
			raw, _ := json.Marshal(saved["errors"])
			t.Logf("  following it with evidence [current]: complete=%v errors=%s", saved["complete"], raw)
		}
	}
}

// A-2 [INVARIANT 10]: refusals of a receipt without hook ids that still offer current (work_trials.go:142 and :170,
// work_reasoning.go:408; work_trials.go:135 has the same words).
func TestAuditD_A2_StandaloneRefusalsNameCurrent(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export filters", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, raw := range prepared["knowledge"].([]any) {
		if e := raw.(map[string]any); e["id"] == page.Page.ID {
			ref = e["ref"].(string)
		}
	}
	errorsOf := func(saved map[string]any) string {
		raw, _ := json.Marshal(saved["errors"])
		return string(raw)
	}
	// 1) text as evidence (what an agent sends first)
	saved, _ := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked at it"}}})
	if text := errorsOf(saved); strings.Contains(text, "use current") {
		t.Errorf("1) evidence as text: the refusal says to use current: %s", text)
		again, _ := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Renamed the button, again.", "evidence": []any{"current"}}})
		t.Logf("   following it with [current]: %s", errorsOf(again))
	}
	// 2) an explicit ending judgment while that result waits for its evidence
	saved, _ = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"],
		"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}},
		"reasoning":    map[string]any{"phase": "finish", "focus": "The button", "assessment": "retain", "reason": "It reads Export.", "evidence": []any{"new1"}, "next_action": "None.", "result": "Renamed."}})
	if text := errorsOf(saved); strings.Contains(text, "refs are current") {
		t.Errorf("2) an ending judgment while the result waits: the refusal says the refs are current, newN or knowledgeN: %s", text)
	}
	// 3) a page another writer edited after this receipt read it
	if ref == "" {
		t.Fatal("the page was not delivered")
	}
	edited := "The export keeps the filters and the totals."
	if _, err := b.Ingest(notebook.Draft{ID: page.Page.ID, Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export filters", Kind: "rule", Scene: "when exporting CSV", Body: &edited}); err != nil {
		t.Fatal(err)
	}
	saved, _ = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Renamed the button, third.", "evidence": []any{ref}}})
	if text := errorsOf(saved); strings.Contains(text, "(current, an observation)") {
		t.Errorf("3) evidence %s after another writer's edit: the refusal offers current: %s", ref, text)
	}
	// 4) the page is no longer live: another lesson replaced it after this receipt read it
	if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Replaces: []string{page.Page.ID}, Title: "CSV export keeps filters and totals", Lesson: "Keep the filters and the totals when exporting CSV.", AppliesTo: "CSV export filters", Kind: "rule", Scene: "when exporting CSV", Body: &edited}); err != nil {
		t.Fatal(err)
	}
	saved, _ = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Renamed the button, fourth.", "evidence": []any{ref}}})
	if text := errorsOf(saved); !strings.Contains(text, "no longer a live page") || strings.Contains(text, "current") {
		t.Errorf("4) evidence %s after the page was replaced: the refusal says the page is gone and does not offer current: %s", ref, text)
	}
}

func auditDList(v any) []any {
	list, _ := v.([]any)
	return list
}

// M06 (work_result_recovery.go:112, markReport(w, pending.Source) taken out): the source that binds evidence to an earlier report
// again ("Recovered work result evidence") is a report of the work about itself, and not evidence of a later result (INVARIANT 10).
func TestAuditDGap_RecoveredEvidenceSourceIsNotEvidence(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) map[string]any {
		t.Helper()
		fields["receipt"] = prepared["receipt"]
		saved, err := workCall(t, b, "record", fields)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	first := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked at it"}}})
	id, _ := first["storage"].(map[string]any)["result_source"].(string)
	if id == "" || first["complete"] == true {
		t.Fatalf("precondition: the report is saved and waits for its evidence: %v", first)
	}
	repaired := record(map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}}, "result": map[string]any{"ref": id, "evidence": []any{"new1"}}})
	recovery := ""
	for _, raw := range auditDList(repaired["saved"]) {
		if item := raw.(map[string]any); item["kind"] == "result-recovery" {
			recovery, _ = item["id"].(string)
		}
	}
	if recovery == "" {
		raw, _ := json.Marshal(repaired)
		t.Fatalf("precondition: the repair saved the source that binds the evidence: %.600s", raw)
	}
	later := record(map[string]any{"result": map[string]any{"changed": "Renamed the button and the menu entry.", "evidence": []any{recovery}}})
	raw, _ := json.Marshal(later["errors"])
	if later["complete"] == true || !strings.Contains(string(raw), "a report this work wrote about itself") {
		t.Errorf("a later result with nothing observed cites the source that bound the evidence of the earlier report (%s) and is not refused: complete=%v errors=%s", recovery, later["complete"], raw)
	}
}

// M10 (work.go:472, the condition made false): the same request prepared a second time does not give observed1 out again
// (INVARIANT 9: the numbers go on, also when the same request is prepared again).
func TestAuditDGap_ASecondPreparationGoesOnNumbering(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	observe := func(prepared map[string]any, title string) string {
		t.Helper()
		saved, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": title, "content": title + " match."}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range auditDList(saved["saved"]) {
			if item := raw.(map[string]any); item["kind"] == "observation" {
				return item["ref"].(string)
			}
		}
		t.Fatalf("no observation saved: %v", saved)
		return ""
	}
	first, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	one := observe(first, "Totals")
	second, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export columns"})
	if err != nil {
		t.Fatal(err)
	}
	two := observe(second, "Dates")
	if one != "observed1" || two == one {
		t.Errorf("the second preparation of the same request names its first observation %s, the name the first preparation gave to another source (%s)", two, one)
	}
}

// A-4 [INVARIANT 4, 14]: a request of the person longer than the widened default. The call the notice asks for (no max_bytes) is
// refused and the agent holds no receipt; the changelog says such a request "still comes whole".
func TestAuditD_A4_VeryLongRequest(t *testing.T) {
	for _, size := range []int{58 << 10, 62 << 10, 66 << 10, 100 << 10} { // 58 KiB is the control (passes)
		t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			t.Chdir(repo)
			prompt := "Use the agreed CSV columns and keep the existing filters. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", size/56)
			notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
			prepared, out, err := auditDCLI(t, strings.TrimSuffix(auditDIDs.FindString(notice), "}")+`,"query":"CSV export filters"}`, "work", "prepare")
			task, _ := prepared["task"].(string)
			t.Logf("a request of %d bytes: err=%v, reply %d bytes, receipt=%v, task %d bytes", len(prompt), err, len(out), prepared["receipt"] != nil, len(task))
			if err != nil {
				t.Errorf("the call the notice asks for is refused, and the agent holds no receipt: %v", err)
			}
		})
	}
}

// A-5 [INVARIANT 14]: docs/guide.md:180 and guide.ja.md:31, the help of shikii work and recording.md:107 say that without max_bytes
// the CLI keeps a prepare reply within 28672 bytes, "a size an agent's shell shows whole". With a long request it does not (the fix
// of A-5 of the first round widens the default), and none of them says so.
func TestAuditD_A5_DefaultReplyOverTheDocumentedBound(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prompt := "Use the agreed CSV columns and keep the existing filters. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", (40<<10)/56)
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	_, out, err := auditDCLI(t, strings.TrimSuffix(auditDIDs.FindString(notice), "}")+`,"query":"CSV export filters"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	help := agentUsages["work"].text()
	note := help[strings.Index(help, "max_bytes ("):]
	t.Logf("the help says: %.200s", note)
	if len(out) <= cliPrepareBytes {
		t.Fatalf("precondition: the reply of a request of %d bytes is over the CLI's own bound: %d bytes", len(prompt), len(out))
	}
	if !strings.Contains(note, "A request longer than that is not cut: the reply grows to hold it.") {
		t.Errorf("a request of %d bytes, no max_bytes: the reply is %d bytes, and the help says only that the CLI keeps it within %d: %s", len(prompt), len(out), cliPrepareBytes, note)
	}
	for _, doc := range []string{"docs/guide.md", "docs/guide.ja.md", "skills/shikii/references/recording.md"} {
		text, err := os.ReadFile(filepath.Join(repository, doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(text), "\n") {
			// the guides also name the bound of reflect query, on lines of their own
			if strings.Contains(line, "28672") && (strings.HasSuffix(doc, "recording.md") || strings.Contains(strings.ToLower(line), "prepare")) && !strings.Contains(line, "is not cut") && !strings.Contains(line, "\u5207\u3089\u305a") {
				t.Errorf("%s names the CLI's bound without the request that is longer than it: %.160s", doc, line)
			}
		}
	}
}

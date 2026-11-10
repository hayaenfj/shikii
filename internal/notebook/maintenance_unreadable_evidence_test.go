package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// outcomeFixture builds a synthetic wiki with procedures and their execution reports.
type outcomeFixture struct {
	t      *testing.T
	b      Book
	source string // the source every article cites
	second int64
}

func newOutcomeFixture(t *testing.T) *outcomeFixture {
	t.Helper()
	f := &outcomeFixture{t: t, b: Book{Root: t.TempDir(), Scope: "/synthetic/evidence"}}
	f.source = f.capture("test", "Article source", "The parsers were checked on plain exports.").ID
	return f
}

func (f *outcomeFixture) capture(kind, title, content string) Observation {
	f.t.Helper()
	f.second++
	o, err := f.b.Capture(Observation{Kind: kind, Title: title, Content: content, Reference: "synthetic", Version: "fixture", ObservedAt: time.Unix(f.second, 0)})
	if err != nil {
		f.t.Fatal(err)
	}
	return o
}

func (f *outcomeFixture) page(kind, title, lesson, appliesTo, body string) Page {
	f.t.Helper()
	d := Draft{Title: title, Lesson: lesson, AppliesTo: appliesTo, Kind: kind, Scene: "checking an export", Sources: []string{f.source}}
	if body != "" {
		d.Body = &body
	}
	saved, err := f.b.Ingest(d)
	if err != nil {
		f.t.Fatal(err)
	}
	return *saved.Page
}

// report records one execution of the procedure and returns the ids of its evidence and of its saved report.
func (f *outcomeFixture) report(p Page, conditions, failure, correction, result string) (evidence, report string) {
	f.t.Helper()
	checked := f.capture("test", "Execution evidence", "Checked: "+result)
	refs, err := f.b.OutcomeSources([]string{checked.ID})
	if err != nil {
		f.t.Fatal(err)
	}
	details := OutcomeDetails{PageID: p.ID, Revision: p.Revision, Conditions: conditions, Action: "Run the raw parser", Failure: failure, Correction: correction, Result: result, Evidence: refs}
	content, _ := json.Marshal(details)
	snapshot := f.capture("agent", "Execution report", string(content))
	refs, err = f.b.OutcomeSources([]string{snapshot.ID})
	if err != nil {
		f.t.Fatal(err)
	}
	if err = f.b.RecordOutcome(ProcedureOutcome{OutcomeDetails: details, Snapshot: refs[0]}); err != nil {
		f.t.Fatal(err)
	}
	// As after a work record: the sources of the report are sorted and leave the capture queue.
	if _, err = f.b.Ingest(Draft{Sources: []string{checked.ID, snapshot.ID}}); err != nil {
		f.t.Fatal(err)
	}
	return checked.ID, snapshot.ID
}

func (f *outcomeFixture) sourcePath(id string) string {
	f.t.Helper()
	dir, err := f.b.directory(false, "sources")
	if err != nil {
		f.t.Fatal(err)
	}
	return filepath.Join(dir, id+".json")
}

// lose removes a saved source and returns a function that puts the original file back.
func (f *outcomeFixture) lose(id string) (restore func()) {
	f.t.Helper()
	path := f.sourcePath(id)
	original, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		f.t.Fatal(err)
	}
	return func() {
		if err := os.WriteFile(path, original, 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
}

// tamper rewrites a saved source into another valid observation and returns a function that puts the original back.
func (f *outcomeFixture) tamper(id string) (restore func()) {
	f.t.Helper()
	path := f.sourcePath(id)
	original, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	if !strings.Contains(string(original), "fixture") {
		f.t.Fatal("the fixture changed: the source has no version to rewrite")
	}
	if err = os.WriteFile(path, []byte(strings.Replace(string(original), "fixture", "tampered-marker", 1)), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return func() {
		if err := os.WriteFile(path, original, 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
}

func maintenanceTask(tasks []MaintenanceTask, id string) *MaintenanceTask {
	for i := range tasks {
		if tasks[i].PageID == id {
			return &tasks[i]
		}
	}
	return nil
}

func unreadableReports(task *MaintenanceTask) int {
	n := 0
	for _, o := range task.Outcomes {
		if len(o.Outcome.Unknown) == 1 && strings.Contains(o.Outcome.Unknown[0], "cannot be read") {
			n++
		}
	}
	return n
}

const (
	fixtureConditions = "Quartz exports on Linux"
	fixtureFailure    = "The raw parser lost quoted fields."
	fixtureCorrection = "Use the structured parser."
	fixtureResult     = "Every quoted field was kept."
)

// quartzFixture holds a procedure with one reported failure and an unrelated rule.
func quartzFixture(t *testing.T) (f *outcomeFixture, procedure, rule Page, evidence, report string) {
	t.Helper()
	f = newOutcomeFixture(t)
	procedure = f.page("howto", "Quartz export workflow", "Check Quartz exports.", "Quartz exports", "Run the raw parser for Quartz exports.")
	rule = f.page("rule", "Commit order", "Run the focused test before a commit.", "commits", "")
	evidence, report = f.report(procedure, fixtureConditions, fixtureFailure, fixtureCorrection, fixtureResult)
	return f, procedure, rule, evidence, report
}

// One saved source file that can no longer be read (lost or changed outside shikii) must not take the whole
// maintenance list down: the list, and with it the article list, the reflection and the work preparation, still
// open. The report whose source is gone is named as unreadable with the source to recover, nothing of its content
// is shown, and the article it belongs to cannot be marked as reviewed until the source is back.
func TestMaintenanceStaysListedWhenOutcomeEvidenceIsUnreadable(t *testing.T) {
	for _, which := range []string{"evidence", "report"} {
		for _, damage := range []string{"missing", "changed"} {
			t.Run(which+" "+damage, func(t *testing.T) {
				f, procedure, rule, evidence, report := quartzFixture(t)
				b := f.b
				before, err := b.Maintenance()
				if err != nil || maintenanceTask(before, procedure.ID) == nil || maintenanceTask(before, rule.ID) == nil {
					t.Fatalf("the fixture changed: before the damage both articles wait for a review: %+v %v", before, err)
				}
				beforeJSON, _ := json.Marshal(before)
				lost := map[string]string{"evidence": evidence, "report": report}[which]
				restore := map[string]func(string) func(){"missing": f.lose, "changed": f.tamper}[damage](lost)

				tasks, err := b.Maintenance()
				if err != nil {
					t.Fatalf("the whole maintenance list fails: %v", err)
				}
				listed, _ := json.Marshal(tasks)
				if other := maintenanceTask(tasks, rule.ID); other == nil {
					t.Errorf("the unrelated article is no longer listed: %s", listed)
				} else if was, _ := json.Marshal(maintenanceTask(before, rule.ID)); string(was) != string(mustJSON(other)) {
					t.Errorf("the unrelated article is listed differently:\n%s\n%s", was, mustJSON(other))
				}
				task := maintenanceTask(tasks, procedure.ID)
				if task == nil || task.OutcomeTotal != 1 || len(task.Outcomes) != 1 {
					t.Fatalf("the procedure with the damaged report is no longer listed with it: %s", listed)
				}
				shown := task.Outcomes[0]
				if shown.Outcome.SourceID != report || len(shown.Outcome.Unknown) != 1 || !strings.Contains(shown.Outcome.Unknown[0], "cannot be read") ||
					!strings.Contains(shown.Outcome.Unknown[0], lost+" ("+damage+")") || !strings.Contains(shown.Outcome.Unknown[0], "Recover") {
					t.Errorf("the damaged report must be named as unreadable, with the source to recover (%s, %s): %+v", lost, damage, shown.Outcome)
				}
				if o := shown.Outcome; o.Conditions != "" || o.Action != "" || o.Failure != "" || o.Correction != "" || o.Result != "" || len(o.Evidence) != 0 || shown.ProposedCorrection != "" || len(shown.CurrentPassages) != 0 {
					t.Errorf("a report that could not be checked is shown as if it had been: %+v", shown)
				}
				for _, secret := range []string{fixtureConditions, "quoted field", "structured parser", "Run the raw parser", "tampered-marker", b.Root} {
					if strings.Contains(string(listed), secret) {
						t.Errorf("the list shows %q, content that could not be checked (or the folder of the store): %s", secret, listed)
					}
				}
				if v, err := b.Query(""); err != nil || len(v.Lessons) != 2 || len(v.Maintenance) != 2 {
					t.Errorf("the article list fails or lost articles: %d lessons, %d reviews, %v", len(v.Lessons), len(v.Maintenance), err)
				}
				if scoped, err := b.OutcomeMaintenance([]string{procedure.ID}); err != nil || len(scoped) != 1 || unreadableReports(&scoped[0]) != 1 {
					t.Errorf("the review of the procedure just used fails or hides the unreadable report: %+v %v", scoped, err)
				}

				// The article with the unreadable report cannot be marked as reviewed, and stays listed.
				if _, err := b.Maintain(MaintenanceReview{PageID: procedure.ID, Revision: task.Revision, Key: task.Key, Reason: "read the report"}); err == nil {
					t.Error("an article whose report cannot be checked was marked as reviewed")
				}
				if again, err := b.Maintenance(); err != nil || maintenanceTask(again, procedure.ID) == nil {
					t.Errorf("after the refused review the procedure must still wait for one: %+v %v", again, err)
				}
				// Asking for the reports of this very procedure still refuses to present what it cannot check.
				if reports, err := b.ProcedureOutcomes(procedure, "Quartz exports"); err == nil {
					t.Errorf("the reports of the procedure were returned without their checked sources: %+v", reports)
				}

				// Once the source is back, the list is what it was, and the review can be recorded.
				restore()
				recovered, err := b.Maintenance()
				if got, _ := json.Marshal(recovered); err != nil || string(got) != string(beforeJSON) {
					t.Fatalf("after recovering the source the list must be what it was (%v):\n%s\n%s", err, beforeJSON, got)
				}
				if _, err := b.Maintain(MaintenanceReview{PageID: procedure.ID, Revision: task.Revision, Key: task.Key, Reason: "read the recovered report"}); err != nil {
					t.Errorf("after recovering the source the review must be recorded: %v", err)
				}
				other := maintenanceTask(tasks, rule.ID)
				if _, err := b.Maintain(MaintenanceReview{PageID: rule.ID, Revision: other.Revision, Key: other.Key, Reason: "still current"}); err != nil {
					t.Errorf("the unrelated article cannot be reviewed: %v", err)
				}
			})
		}
	}
}

func mustJSON(v any) []byte { data, _ := json.Marshal(v); return data }

// Guard: with every source readable, a report is listed in full as before, and its article can be reviewed.
func TestMaintenanceListsAReadableReportInFull(t *testing.T) {
	f, procedure, rule, evidence, report := quartzFixture(t)
	tasks, err := f.b.Maintenance()
	if err != nil || len(tasks) != 2 || tasks[0].PageID != procedure.ID || tasks[1].PageID != rule.ID {
		t.Fatalf("the procedure with a report comes first, then the rule: %+v %v", tasks, err)
	}
	task := tasks[0]
	if task.OutcomeTotal != 1 || len(task.Outcomes) != 1 || task.Reasons[0] != "outcome-feedback" || !strings.Contains(task.NextAction, "Compare the reported failure") {
		t.Fatalf("the report must be listed with its task: %+v", task)
	}
	shown := task.Outcomes[0]
	o := shown.Outcome
	if o.SourceID != report || o.Revision != procedure.Revision || o.RevisionChanged || o.Conditions != fixtureConditions || o.Action != "Run the raw parser" || o.Failure != fixtureFailure || o.Correction != fixtureCorrection || o.Result != fixtureResult {
		t.Fatalf("the report must be shown as recorded: %+v", o)
	}
	if len(o.Evidence) != 1 || o.Evidence[0].SourceID != evidence || o.Evidence[0].Kind != "test" || o.Evidence[0].Content != "Checked: "+fixtureResult || len(o.Unknown) != 0 {
		t.Fatalf("the evidence must be shown with its content and no unknowns: %+v", o)
	}
	if shown.ProposedCorrection != fixtureCorrection || len(shown.CurrentPassages) == 0 || !strings.Contains(shown.CurrentPassages[0].Text, "Run the raw parser for Quartz exports.") {
		t.Fatalf("the proposed correction and the current passage must be shown: %+v", shown)
	}
	if _, err := f.b.Maintain(MaintenanceReview{PageID: procedure.ID, Revision: task.Revision, Key: task.Key, Reason: "compared the report with the passage"}); err != nil {
		t.Fatalf("an article whose report can be checked must be reviewable: %v", err)
	}
}

// The index of the reports is a file too. When its entry was rewritten, the saved report no longer matches it:
// the list goes on, the report is named as unreadable, and the rewritten entry is not shown or reviewed.
func TestMaintenanceNamesAReportWhoseIndexEntryWasRewritten(t *testing.T) {
	f, procedure, rule, _, report := quartzFixture(t)
	dir, err := f.b.directory(false, "searches")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".searches.json")
	index, err := os.ReadFile(path)
	if err != nil || strings.Count(string(index), fixtureResult) != 1 {
		t.Fatalf("the fixture changed: the index must hold the reported result once: %s %v", index, err)
	}
	if err = os.WriteFile(path, []byte(strings.Replace(string(index), fixtureResult, "Every quoted field was rewritten.", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := f.b.Maintenance()
	if err != nil {
		t.Fatalf("the whole maintenance list fails: %v", err)
	}
	listed := mustJSON(tasks)
	task := maintenanceTask(tasks, procedure.ID)
	if task == nil || maintenanceTask(tasks, rule.ID) == nil || len(task.Outcomes) != 1 || unreadableReports(task) != 1 {
		t.Fatalf("both articles stay listed, the procedure with its report as unreadable: %s", listed)
	}
	if said := task.Outcomes[0].Outcome.Unknown[0]; !strings.Contains(said, "does not match its saved report "+report) || !strings.Contains(said, "Recover") {
		t.Errorf("the report must be named with the saved report its entry no longer matches: %s", said)
	}
	for _, unchecked := range []string{"rewritten", "quoted field", "structured parser", fixtureConditions} {
		if strings.Contains(string(listed), unchecked) {
			t.Errorf("the list shows %q from an index entry that could not be checked: %s", unchecked, listed)
		}
	}
	if _, err := f.b.Maintain(MaintenanceReview{PageID: procedure.ID, Revision: task.Revision, Key: task.Key, Reason: "read the report"}); err == nil {
		t.Error("an article whose report cannot be checked was marked as reviewed")
	}
}

// The list carries two tasks. Articles whose report cannot be read cannot be reviewed, so they must not hold both
// places for as long as their sources are lost: at most one place goes to them while other work waits.
func TestMaintenanceKeepsAPlaceBesideUnreadableReports(t *testing.T) {
	f := newOutcomeFixture(t)
	first := f.page("howto", "Quartz export workflow", "Check Quartz exports.", "Quartz exports", "Run the raw parser for Quartz exports.")
	second := f.page("howto", "Basalt import workflow", "Check Basalt imports.", "Basalt imports", "Run the raw parser for Basalt imports.")
	rule := f.page("rule", "Commit order", "Run the focused test before a commit.", "commits", "")
	firstEvidence, _ := f.report(first, fixtureConditions, fixtureFailure, fixtureCorrection, fixtureResult)
	secondEvidence, _ := f.report(second, "Basalt imports on Linux", "The raw parser lost nested rows.", "Use the nested parser.", "Every nested row was kept.")
	before, err := f.b.Maintenance()
	if err != nil || len(before) != 2 || maintenanceTask(before, first.ID) == nil || maintenanceTask(before, second.ID) == nil {
		t.Fatalf("the fixture changed: with readable reports the two procedures come first: %+v %v", before, err)
	}
	f.lose(firstEvidence)
	f.lose(secondEvidence)
	tasks, err := f.b.Maintenance()
	if err != nil || len(tasks) != 2 {
		t.Fatalf("the list must still carry two tasks: %+v %v", tasks, err)
	}
	other := maintenanceTask(tasks, rule.ID)
	if other == nil || unreadableReports(&tasks[0])+unreadableReports(&tasks[1]) != 1 {
		t.Fatalf("one place names an unreadable report and the other goes to work that can be done: %s", mustJSON(tasks))
	}
	// With no other work waiting, both unreadable reports are named.
	if _, err := f.b.Maintain(MaintenanceReview{PageID: rule.ID, Revision: other.Revision, Key: other.Key, Reason: "still current"}); err != nil {
		t.Fatal(err)
	}
	tasks, err = f.b.Maintenance()
	if err != nil || len(tasks) != 2 || unreadableReports(&tasks[0]) != 1 || unreadableReports(&tasks[1]) != 1 {
		t.Fatalf("with nothing else waiting both unreadable reports must be named: %s %v", mustJSON(tasks), err)
	}
}

// The collective view of a work preparation contrasts two differing reports of one procedure. A report whose
// source can no longer be read must not fail the preparation of every work that finds the procedure: the
// report is named as unreadable, nothing of it is shown, and no difference is claimed from what could not be checked.
func TestCollectiveNamesAnUnreadableReportInsteadOfFailing(t *testing.T) {
	f := newOutcomeFixture(t)
	p := f.page("howto", "Quartz export workflow", "Check Quartz exports.", "Quartz exports", "Run the raw parser for Quartz exports.")
	ref := []ArticleReference{{ID: p.ID, Revision: p.Revision}}
	_, olderReport := f.report(p, fixtureConditions, fixtureFailure, fixtureCorrection, fixtureResult)
	single, err := f.b.Collective(nil, ref)
	if err != nil || len(single.Contrasts) != 0 {
		t.Fatalf("the fixture changed: one report is no contrast: %+v %v", single, err)
	}
	restore := f.lose(olderReport)
	if single, err = f.b.Collective(nil, ref); err != nil || len(single.Contrasts) != 0 {
		t.Fatalf("one unreadable report must neither fail the view nor become a contrast: %+v %v", single, err)
	}
	restore()

	newerEvidence, newerReport := f.report(p, "Quartz exports on macOS", "", "", "The raw parser kept every field.")
	before, err := f.b.Collective(nil, ref)
	if err != nil || len(before.Contrasts) != 1 || len(before.Contrasts[0].Reports) != 2 || before.Contrasts[0].Conditions != "different_text" || !strings.Contains(before.Contrasts[0].Unknown[0], "Different reported results") {
		t.Fatalf("the fixture changed: two differing reports are one contrast: %+v %v", before, err)
	}
	beforeJSON := mustJSON(before)

	restore = f.tamper(newerEvidence)
	view, err := f.b.Collective(nil, ref)
	if err != nil {
		t.Fatalf("the collective view fails: %v", err)
	}
	shown := mustJSON(view)
	if len(view.Contrasts) != 1 || len(view.Contrasts[0].Reports) != 2 {
		t.Fatalf("the two reports must still be listed: %s", shown)
	}
	c := view.Contrasts[0]
	var unreadable, readable *ProcedureOutcomeView
	for i := range c.Reports {
		if len(c.Reports[i].Unknown) == 1 && strings.Contains(c.Reports[i].Unknown[0], "cannot be read") {
			unreadable = &c.Reports[i]
		} else {
			readable = &c.Reports[i]
		}
	}
	if unreadable == nil || readable == nil {
		t.Fatalf("one report is unreadable and the other is shown: %s", shown)
	}
	if unreadable.SourceID != newerReport || !strings.Contains(unreadable.Unknown[0], newerEvidence+" (changed)") || unreadable.Conditions != "" || unreadable.Result != "" || len(unreadable.Evidence) != 0 {
		t.Errorf("the unreadable report must be named with its source and without content: %+v", unreadable)
	}
	if readable.SourceID != olderReport || readable.Result != fixtureResult || len(readable.Evidence) != 1 {
		t.Errorf("the readable report must be shown as before: %+v", readable)
	}
	if c.Conditions != "not_compared" || len(c.Unknown) != 1 || !strings.Contains(c.Unknown[0], "cannot be compared") || strings.Contains(c.Unknown[0], "Different reported results") || c.Judgment != "uninterpreted" || c.Independence != "not_established" {
		t.Errorf("no difference may be claimed from a report that could not be checked: %+v", c)
	}
	for _, secret := range []string{"Quartz exports on macOS", "kept every field", "tampered-marker", f.b.Root} {
		if strings.Contains(string(shown), secret) {
			t.Errorf("the view shows %q, content that could not be checked (or the folder of the store): %s", secret, shown)
		}
	}
	// Asking for the reports of this very procedure still refuses to present what it cannot check.
	if reports, err := f.b.ProcedureOutcomes(p, ""); err == nil {
		t.Errorf("the reports of the procedure were returned without their checked sources: %+v", reports)
	}

	// Guard: once the source is back, the contrast is what it was.
	restore()
	recovered, err := f.b.Collective(nil, ref)
	if got := mustJSON(recovered); err != nil || string(got) != string(beforeJSON) {
		t.Fatalf("after recovering the source the contrast must be what it was (%v):\n%s\n%s", err, beforeJSON, got)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The refusals of work that kept coming back in this machine's agent sessions (counted on 2026-11-11) said that something was wrong
// but not what or what to do: "prepare reads context; send content with record" (which field?), "only the context of the ongoing
// request in the same project can be handled" (which of four reasons?), "unknown prepared memory reference" (which refs exist?).
// Each now names the cause and the next call.
func TestWorkRefusalsNameTheCauseAndTheNextCall(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prompt := func(text string) string {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": text})
		return loadSession(root, "planning").TurnID
	}
	lacks := func(name string, err error, want ...string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s must be refused", name)
			return
		}
		for _, w := range want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal lacks %q: %v", name, w, err)
			}
		}
	}

	// prepare names the content fields it was sent
	turn := prompt(planningTestRequest)
	_, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export", "continues_previous": true})
	lacks("prepare with continues_previous", err, "prepare reads context; send content with record", "prepare was sent continues_previous")
	_, err = workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export", "result": map[string]any{"changed": "x"}, "observations": []any{map[string]any{"kind": "test", "title": "t", "content": "c"}}})
	lacks("prepare with a result and observations", err, "prepare was sent result, observations")

	// a receipt of an earlier request, after a newer request started
	first, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	prompt("Also check the totals of the CSV export.")
	_, err = workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "result": map[string]any{"changed": "Nothing.", "checked": "Read the export."}})
	lacks("a record with the receipt of an earlier request", err, "only the context of the ongoing request in the same project can be handled", "a newer request has started in this session", "prepare again with the ids of the latest hook notice")

	// the other reasons, on the session record itself
	frame := func(change func(*hookSession)) error {
		s := &hookSession{Scope: "/project", TurnID: "0123456789abcdef", Planning: &planningFrame{TurnID: "0123456789abcdef", At: time.Now()}}
		change(s)
		return currentPlanningFrame(s, "0123456789abcdef", "/project")
	}
	if err := frame(func(*hookSession) {}); err != nil {
		t.Errorf("the ongoing request is accepted: %v", err)
	}
	lacks("another project", frame(func(s *hookSession) { s.Scope = "/elsewhere" }), "only the context of the ongoing request in the same project can be handled", "another project folder", "run the command in that project")
	lacks("no organizing for this turn", frame(func(s *hookSession) { s.Planning = nil }), "a newer request has started in this session")
	lacks("a closed turn", frame(func(s *hookSession) { s.Judged, s.Planning.Result = true, &planningReceipt{} }), "closed at the end of its turn", "the next request's hook notice")
	lacks("an old request", frame(func(s *hookSession) { s.Planning.At = time.Now().Add(-hookSessionTTL - time.Hour) }), "older than", "prepare again")
	if err := frame(func(s *hookSession) { s.Judged = true }); err != nil {
		t.Errorf("a turn judged before its organizing was saved still takes it: %v", err)
	}

	// a memory or review reference the receipt did not prepare
	turn = loadSession(root, "planning").TurnID
	second, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
		"memory":  []any{map[string]any{"ref": "knowledge1", "result": "helpful", "decision": "d", "evidence": "e"}},
		"reviews": []any{map[string]any{"ref": "lesson:0123456789abcdef", "answer": "yes", "reason": "r"}}})
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]string{}
	for _, raw := range saved["errors"].([]any) {
		item := raw.(map[string]any)
		items[item["kind"].(string)] = item["error"].(string)
	}
	for kind, want := range map[string][]string{
		"memory": {`unknown prepared memory reference "knowledge1"`, "this receipt prepared no memory to assess", "leave memory out"},
		"review": {`unknown prepared review reference "lesson:0123456789abcdef"`, "this receipt prepared no review", "leave reviews out"},
	} {
		for _, w := range want {
			if !strings.Contains(items[kind], w) {
				t.Errorf("the %s refusal lacks %q: %q", kind, w, items[kind])
			}
		}
	}
	// With prepared references, the refusal lists them in their order under the field of the reply that holds them. The field is
	// the one the refusing call names (recordReflection), so the same record goes through it once the receipt's work holds
	// references. (Before, this test handed unknownPreparedRef the field's name itself, and passed while the call named
	// pending[].memories[], a field the reply does not have: audit of 0.31.0, B-U4 and D U-2.)
	held := loadSession(root, "planning")
	if held.Planning == nil || held.Planning.Work == nil {
		t.Fatal("precondition: the session holds the work of the receipt")
	}
	held.Planning.Work.Memory = map[string]workMemoryRef{"memory2": {}, "memory10": {}, "memory1": {}}
	held.Planning.Work.Reviews = map[string]workReviewRef{"review2": {}, "review10": {}, "review1": {}}
	if err := saveSession(root, "planning", held); err != nil {
		t.Fatal(err)
	}
	saved, err = workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
		"memory":  []any{map[string]any{"ref": "knowledge1", "result": "helpful", "decision": "d", "evidence": "e"}},
		"reviews": []any{map[string]any{"ref": "lesson:0123456789abcdef", "answer": "yes", "reason": "r"}}})
	if err != nil {
		t.Fatal(err)
	}
	items = map[string]string{}
	for _, raw := range saved["errors"].([]any) {
		item := raw.(map[string]any)
		items[item["kind"].(string)] = item["error"].(string)
	}
	for kind, want := range map[string]string{
		"memory": `unknown prepared memory reference "knowledge1"; use the refs of pending[].memory[] in the reply of prepare: memory1, memory2, memory10`,
		"review": `unknown prepared review reference "lesson:0123456789abcdef"; use the refs of pending[].reviews[] in the reply of prepare: review1, review2, review10`,
	} {
		if items[kind] != want {
			t.Errorf("the %s refusal lists the prepared refs in their order under the reply's field:\n got %q\nwant %q", kind, items[kind], want)
		}
	}

	// a source reference that is this call's own observation, which was not saved
	saved, err = workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
		"observations": []any{map[string]any{"kind": "check", "title": "t", "content": "c"}},
		"notes":        []any{map[string]any{"title": "CSV totals", "lesson": "Check the totals.", "applies_to": "CSV export", "kind": "rule", "scene": "when exporting CSV", "body": "b", "sources": []any{"new1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range saved["errors"].([]any) {
		text, _ := raw.(map[string]any)["error"].(string)
		if strings.Contains(text, `unknown source reference "new1"`) {
			found = true
			for _, w := range []string{"observation 1 of this call was not saved", "resend it with what cites it"} {
				if !strings.Contains(text, w) {
					t.Errorf("the refusal of a note citing an unsaved observation lacks %q: %q", w, text)
				}
			}
		}
	}
	if !found {
		t.Errorf("a note citing an observation that was not saved is refused: %v", saved["errors"])
	}
	if got := unknownSourceRef("evidence9", 0, []string{"current", "source1", "result"}); !strings.Contains(got, `unknown source reference "evidence9"`) || !strings.Contains(got, "usable here: current, result, source1") {
		t.Errorf("an unknown source reference lists the usable ones: %q", got)
	}
}

// related in a note names articles the receipt has read. An agent that wrote page ids there (the ids it sees in knowledge) was
// answered "unknown related article" and nothing else (2026-11-12, while recording this very work). The id of an article the
// receipt read is accepted like its ref, and any other value is answered with the refs it could use and how to read another page.
func TestRelatedInANoteTakesTheArticlesTheReceiptRead(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, raw := range prepared["knowledge"].([]any) {
		if entry := raw.(map[string]any); entry["id"] == page.Page.ID {
			ref, _ = entry["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("the page is among the prepared knowledge: %v", prepared["knowledge"])
	}
	note := func(title string, related ...string) map[string]any {
		return map[string]any{"receipt": prepared["receipt"],
			"observations": []any{map[string]any{"kind": "test", "title": "Totals " + title, "content": "The totals of " + title + " match."}},
			"notes":        []any{map[string]any{"target": "new", "title": "CSV totals " + title, "lesson": "Check the totals of " + title + ".", "applies_to": "CSV export totals", "kind": "rule", "scene": "when exporting CSV", "body": "Checked " + title + ".", "sources": []any{"new1"}, "related": related}}}
	}
	refused := func(saved map[string]any) string {
		text := ""
		for _, raw := range saved["errors"].([]any) {
			if item := raw.(map[string]any); item["kind"] == "note" {
				text, _ = item["error"].(string)
			}
		}
		return text
	}
	saved, err := workCall(t, b, "record", note("with an unread page", "0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	got := refused(saved)
	for _, w := range []string{`unknown related article "0123456789abcdef"`, "related takes the articles this receipt read: " + ref, "prepare with this receipt and query lesson:0123456789abcdef", "or leave it out"} {
		if !strings.Contains(got, w) {
			t.Errorf("the refusal of an unread page in related lacks %q: %q", w, got)
		}
	}
	for _, related := range []string{ref, page.Page.ID} {
		saved, err = workCall(t, b, "record", note("related by "+related, related))
		if err != nil {
			t.Fatal(err)
		}
		if errs, _ := saved["errors"].([]any); len(errs) > 0 && strings.Contains(refused(saved), "related") {
			t.Errorf("related %q, an article the receipt read, is accepted: %v", related, errs)
		}
	}
}

// Found while recording this very work (2026-11-12): a receipt could not cite what it had itself written. A note that named, as
// its source, the id of an observation the same receipt had saved was refused ("unknown source reference"), and a result that
// named, as evidence, an article the same receipt had edited was refused with "read it again" and nothing else (the edit made
// the article newer than the revision the receipt read). The receipt knows both: the id of a source it holds is accepted like its
// ref, and evidence follows an article to the revision the receipt's own note wrote. An id the receipt does not hold, and an
// article someone else changed, are still refused, with what to cite instead.
func TestAReceiptCitesWhatItSavedAndTheArticleItEdited(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, raw := range prepared["knowledge"].([]any) {
		if entry := raw.(map[string]any); entry["id"] == page.Page.ID {
			ref, _ = entry["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("the page is among the prepared knowledge: %v", prepared["knowledge"])
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
	refused := func(saved map[string]any, kind string) string {
		errs, _ := saved["errors"].([]any)
		for _, raw := range errs {
			if item := raw.(map[string]any); strings.HasPrefix(item["kind"].(string), kind) {
				return item["error"].(string)
			}
		}
		return ""
	}
	newNote := func(title string, source string) map[string]any {
		return map[string]any{"notes": []any{map[string]any{"target": "new", "title": "CSV totals " + title, "lesson": "Check the totals of " + title + ".", "applies_to": "CSV export totals", "kind": "rule", "scene": "when exporting CSV", "body": "Checked " + title + ".", "sources": []any{source}}}}
	}

	// the receipt saves an observation and edits the article it read
	saved := record(map[string]any{
		"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}},
		"notes":        []any{map[string]any{"target": ref, "sources": []any{"new1"}, "change_reason": "Add the totals.", "edits": []any{map[string]any{"field": "body", "before": "The export keeps the filters.", "after": "The export keeps the filters and the totals."}}}}})
	if got := refused(saved, "note"); got != "" {
		t.Fatalf("the edit of the article is saved: %s", got)
	}
	observed := ""
	for _, raw := range saved["saved"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "observation" {
			observed, _ = item["id"].(string)
		}
	}
	if observed == "" {
		t.Fatalf("the observation is saved with its id: %v", saved["saved"])
	}

	// a later note cites that observation by the id the reply gave
	if got := refused(record(newNote("by the id of its own observation", observed)), "note"); got != "" {
		t.Errorf("the id of a source this receipt saved is accepted as a source: %s", got)
	}
	got := refused(record(newNote("by an id the receipt does not hold", "0123456789abcdef")), "note")
	for _, w := range []string{`unknown source reference "0123456789abcdef"`, "usable here: "} {
		if !strings.Contains(got, w) {
			t.Errorf("the refusal of a source the receipt does not hold lacks %q: %q", w, got)
		}
	}

	// the result cites the article the receipt itself edited
	saved = record(map[string]any{"result": map[string]any{"changed": "The article says the totals.", "checked": "Read the export.", "evidence": []any{ref, observed}}})
	if got := refused(saved, "result"); got != "" {
		t.Errorf("evidence follows the article to the revision this receipt's own note wrote, and takes the id of a source the receipt holds: %s", got)
	}

	// someone else changes the article: the receipt has not read that revision
	live, err := b.LiveWhere(func(p notebook.Page) bool { return p.ID == page.Page.ID })
	if err != nil || len(live) != 1 {
		t.Fatal(err, len(live))
	}
	if _, err := b.Patch(notebook.Patch{ID: page.Page.ID, Revision: live[0].Revision, Sources: []string{source.ID}, ChangeReason: "Another writer.", Edits: []notebook.TextEdit{{Field: "body", Before: "and the totals.", After: "and the totals of every page."}}}); err != nil {
		t.Fatal(err)
	}
	saved = record(map[string]any{"result": map[string]any{"changed": "The article says the totals of every page.", "checked": "Read the export again.", "evidence": []any{ref}}})
	got = refused(saved, "result")
	for _, w := range []string{"evidence " + ref, "lesson:" + page.Page.ID, "has a newer revision", "prepare with this receipt and query lesson:" + page.Page.ID, "or cite another reference"} {
		if !strings.Contains(got, w) {
			t.Errorf("the refusal of an article someone else changed lacks %q: %q", w, got)
		}
	}
}

// A result is saved as a source of its own, and that source takes the next observedN name. A result that named that reference as
// its evidence was accepted: with nothing observed, evidence:["observed1"] cited the report itself and the reply said complete
// (found 2026-11-12 by the negative example of another test). A result is not its own evidence: the reference is refused with
// what to cite instead, under any of its names (observedN, result, its id). An observation the work really saved stays citable.
func TestAResultIsNotItsOwnEvidence(t *testing.T) {
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
	refused := func(saved map[string]any) string {
		errs, _ := saved["errors"].([]any)
		for _, raw := range errs {
			if item := raw.(map[string]any); strings.HasPrefix(item["kind"].(string), "result") {
				return item["error"].(string)
			}
		}
		return ""
	}
	for i, ref := range []string{"observed1", "result"} { // each result is a new report: the first is observed1, the second observed2
		saved := record(map[string]any{"result": map[string]any{"changed": fmt.Sprintf("Renamed the button, report %d.", i), "evidence": []any{ref}}})
		got := refused(saved)
		for _, w := range []string{fmt.Sprintf("evidence %q is this result itself", ref), "new1", "or leave evidence out"} {
			if !strings.Contains(got, w) || saved["complete"] == true {
				t.Errorf("a result that cites itself as %s is refused with %q: complete=%v %q", ref, w, saved["complete"], got)
			}
		}
	}
	// a receipt without hook ids has no current request: the reply does not offer it, and the refusal names nothing the result
	// cannot cite (before, it named observed1 and result, the result's own reports, whose refusal named current again: B-I1)
	if how, _ := prepared["how_to_record"].(string); strings.Contains(how, "(current,") || !strings.Contains(how, "(newN for observation N") {
		t.Errorf("a receipt without hook ids is not told to cite current: %s", how)
	}
	got := refused(record(map[string]any{"result": map[string]any{"changed": "Renamed the button, citing the request.", "evidence": []any{"current"}}}))
	got, _, _ = strings.Cut(got, "; the result is saved and waits") // what follows says how the saved result is repaired, not what to cite
	if !strings.Contains(got, `unknown source reference "current"`) || !strings.Contains(got, "new1") || strings.Contains(got, "observed") || strings.Contains(got, "result") {
		t.Errorf("the refusal of current names what this result can cite: %q", got)
	}
	// an earlier report of this work is not evidence either, under its observedN name (audit of 0.31.0, A-1)
	got = refused(record(map[string]any{"result": map[string]any{"changed": "Renamed the button, citing my first report.", "evidence": []any{"observed1"}}}))
	if !strings.Contains(got, `evidence "observed1" is a report this work wrote about itself`) || strings.Contains(got, "or current") {
		t.Errorf("an earlier report of the same work is refused as evidence: %q", got)
	}
	saved := record(map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}}, "result": map[string]any{"changed": "Renamed the button, checked.", "checked": "Read the page.", "evidence": []any{"new1"}}})
	if got := refused(saved); got != "" {
		t.Fatalf("a result that cites an observation of its call is saved: %s", got)
	}
	observed := ""
	for _, raw := range saved["saved"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "observation" {
			observed, _ = item["ref"].(string)
		}
	}
	if saved = record(map[string]any{"result": map[string]any{"changed": "Renamed the button, checked again.", "evidence": []any{observed}}}); observed == "" || refused(saved) != "" {
		t.Errorf("a later result cites the observation the work saved (%s): %s", observed, refused(saved))
	}
}

// Found on 2026-11-15 while recording three notes in one call: the refusal said only "the recording batch exceeds its fixed item
// limits", so the agent had to read the code to learn that a record takes at most two notes. The refusal now names each field over
// its limit, the number sent and the limit, and says to send the rest in another record with the same receipt.
func TestARecordOverItsItemLimitsNamesTheLimit(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{"title": "CSV filters", "lesson": "Keep the filters.", "applies_to": "CSV export", "kind": "howto", "scene": "exporting CSV", "sources": []string{"new1"}}
	observation := map[string]any{"kind": "test", "title": "CSV check", "content": "The filters were kept."}
	_, err = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "notes": []any{note, note, note}, "observations": []any{observation, observation, observation, observation, observation}})
	if err == nil {
		t.Fatal("a record with three notes and five observations must be refused")
	}
	for _, w := range []string{"the recording batch exceeds its fixed item limits", "notes: 3 sent, at most 2", "observations: 5 sent, at most 4",
		"nothing of this record was saved", "send it again with each field within its limit", "send the rest in another work record with the same receipt"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal lacks %q: %v", w, err)
		}
	}
}

// Audit of 0.33.0 (A-1): a record over its item limits is refused whole before anything is saved, but the refusal said only "send
// the rest in another work record with the same receipt". An agent that followed it sent only the items over the limit, and the
// items within the limit, never saved, were lost. The refusal now says that nothing of the record was saved and that the record goes
// again within the limits, with the rest in a later record. Following it as written keeps every note.
func TestARefusedOverLimitRecordSaysNothingWasSaved(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export", "request": "Check the CSV export."})
	if err != nil {
		t.Fatal(err)
	}
	observation := func(title string) map[string]any {
		return map[string]any{"kind": "test", "title": "Check " + title, "content": "Checked " + title + "."}
	}
	note := func(title, source string) map[string]any {
		return map[string]any{"target": "new", "title": "CSV " + title, "lesson": "Check the " + title + " of the CSV export.", "applies_to": "CSV export " + title,
			"body": "Checked the " + title + ".", "kind": "rule", "scene": "when exporting CSV", "sources": []any{source}}
	}
	saved := func() map[string]bool {
		t.Helper()
		live, err := b.LiveWhere(func(p notebook.Page) bool { return strings.HasPrefix(p.Title, "CSV ") })
		if err != nil {
			t.Fatal(err)
		}
		titles := map[string]bool{}
		for _, p := range live {
			titles[p.Title] = true
		}
		return titles
	}
	_, err = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"],
		"observations": []any{observation("filters"), observation("totals"), observation("headers")},
		"notes":        []any{note("filters", "new1"), note("totals", "new2"), note("headers", "new3")}})
	if err == nil {
		t.Fatal("a record with three notes must be refused")
	}
	if titles := saved(); len(titles) != 0 {
		t.Fatalf("precondition: the refused record saved nothing, so the refusal must not say only the rest is missing: %v", titles)
	}
	for _, w := range []string{"notes: 3 sent, at most 2", "nothing of this record was saved", "send it again with each field within its limit", "send the rest in another work record with the same receipt"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal of a record over its limits lacks %q: %v", w, err)
		}
	}

	// Follow the refusal as written: the record again within its limits, then the rest with the same receipt. A note close to an
	// earlier one waits for its placement, which the reply's next_action settles with target new.
	var record func(call map[string]any)
	record = func(call map[string]any) {
		t.Helper()
		r, err := workCall(t, b, "record", call)
		if errs, _ := r["errors"].([]any); err != nil || len(errs) > 0 {
			t.Fatalf("a record within the limits is saved: %v %v", r["errors"], err)
		}
		pending, _ := r["pending"].([]any)
		for _, raw := range pending {
			if ref, _ := raw.(map[string]any)["ref"].(string); strings.HasPrefix(ref, "note") {
				record(map[string]any{"receipt": prepared["receipt"], "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
			}
		}
	}
	record(map[string]any{"receipt": prepared["receipt"], "observations": []any{observation("filters"), observation("totals")}, "notes": []any{note("filters", "new1"), note("totals", "new2")}})
	record(map[string]any{"receipt": prepared["receipt"], "observations": []any{observation("headers")}, "notes": []any{note("headers", "new1")}})
	titles := saved()
	for _, title := range []string{"CSV filters", "CSV totals", "CSV headers"} {
		if !titles[title] {
			t.Errorf("following the refusal lost %q: %v", title, titles)
		}
	}
}

// Found on 2026-11-15 in OpenCode 2.3.0's run of the shikii_wiki tool: four refusals in a row came from one cause, the fields of
// work sent beside it instead of inside it. The client dropped every argument its tool schema does not name (receipt, result), so
// shikii never saw them and refused what was left with a different cause each time: "prepare/record needs a work object" (twice),
// an invalid receipt for work:{query}, and "record needs content" for work:{receipt}. The agent fixed one field per call. Each of
// these refusals over MCP now says that every field of the call except operation and project goes inside work, with the shape of
// a record.
func TestARefusalOfWorkWithoutItsFieldsSaysTheyGoInsideWork(t *testing.T) {
	repo, root, _ := planningFixture(t)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	refusal := func(args string) string {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(args)})
		reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)
		result, _ := reply["result"].(map[string]any)
		if result == nil || result["isError"] != true {
			t.Fatalf("%s must be refused: %v", args, reply)
		}
		return result["content"].([]map[string]any)[0]["text"].(string)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(prepared["receipt"])
	for _, c := range []struct{ name, args, cause string }{
		{"prepare whose receipt the client dropped", `{"operation":"prepare","query":"memory-upgrade"}`, "prepare/record needs a work object"},
		{"record whose receipt and result the client dropped", `{"operation":"record","work":{"query":"CSV export"}}`, "invalid work receipt"},
		{"record whose result the client dropped", `{"operation":"record","work":{"receipt":` + string(receipt) + `}}`, "record needs content that was judged or observed"},
	} {
		text := refusal(c.args)
		for _, w := range []string{c.cause, "every field of the call except operation and project goes inside work", `{\"operation\":\"record\",\"work\":{\"receipt\":`} {
			if !strings.Contains(text, w) {
				t.Errorf("%s: the refusal lacks %q: %s", c.name, w, text)
			}
		}
	}
	// a refusal that is not about a missing field keeps its own words: a receipt that was sent but is not one
	if text := refusal(`{"operation":"record","work":{"receipt":"not-a-receipt","result":{"changed":"x"}}}`); strings.Contains(text, "goes inside work") {
		t.Errorf("a receipt that was sent is not a field left out: %s", text)
	}
	// What the refusal says of operation and project holds (audit of 0.33.0, U-7: it said every field goes inside work): project
	// inside work is a field work does not have, and beside work the same record goes through.
	record := func(work map[string]any, project bool) string {
		args := map[string]any{"operation": "record", "work": work}
		if project {
			args["project"] = repo
		}
		raw, _ := json.Marshal(args)
		return string(raw)
	}
	result := map[string]any{"changed": "Checked the CSV export."}
	if text := refusal(record(map[string]any{"project": repo, "receipt": prepared["receipt"], "result": result}, false)); !strings.Contains(text, `unknown field \"project\" in work`) {
		t.Errorf("project inside work is refused: %s", text)
	}
	if got, _ := mcpToolCall(t, h, "shikii_wiki", record(map[string]any{"receipt": prepared["receipt"], "result": result}, true)); got["structuredContent"].(map[string]any)["complete"] != true {
		t.Errorf("project beside work records: %v", got)
	}
}

// Found on 2026-11-15 in the same OpenCode runs (twice in two runs): the agent captured evidence.txt with wiki capture, cited the
// returned source id in result.evidence and was refused with "use a prepared article, trial or captured source", which named the
// very thing it had sent. The receipt holds only the sources its own record saved (observedN, or newN of the call); a source
// captured outside it is not one. The refusal now says which sources count and that one captured outside the receipt does not.
func TestEvidenceCapturedOutsideTheReceiptIsRefusedWithoutContradiction(t *testing.T) {
	repo, root, _ := planningFixture(t)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	captured, _ := mcpToolCall(t, h, "shikii_wiki", `{"operation":"capture","observation":{"kind":"test","title":"CSV check","content":"The filters were kept.","reference":"export.csv","version":"1","observed_at":"2026-11-15T00:00:00Z"}}`)
	id, _ := captured["structuredContent"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("capture returned no source id: %v", captured)
	}
	got, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Kept the filters.", "evidence": []string{id}}})
	text := fmt.Sprint(got, err)
	for _, w := range []string{"unknown reasoning evidence", id, "a source this receipt holds", "a source captured outside this receipt", "is not one"} {
		if !strings.Contains(text, w) {
			t.Errorf("the refusal lacks %q: %s", w, text)
		}
	}
	if strings.Contains(text, "trial or captured source") {
		t.Errorf("the refusal tells the agent to use what it sent: %s", text)
	}
}

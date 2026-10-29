package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// The findings of the first confirmation audit of 0.12.0 (.shikii/handoff/audit-0.12/audit-confirm-1.md): each failed before the fix.

// Property 9: a comment line is not a command, and nothing after a << is read (the rule after three audits).
func TestACommentIsNotACommandAndNothingAfterAHereDocumentIsRead(t *testing.T) {
	if got := commandPhrases("# usage: cat <<EOF\ngit commit -m x"); len(got) != 1 || got[0] != "git commit" {
		t.Fatalf("a comment line is not a command and does not start a here-document: %v", got)
	}
	if got := commandPhrases("echo a#b\ngit commit -m x"); len(got) != 1 || got[0] != "git commit" {
		t.Fatalf("a # inside a word is not a comment: %v", got)
	}
	if got := commandPhrases("echo '#' x\ngit commit -m x"); len(got) != 1 || got[0] != "git commit" {
		t.Fatalf("a quoted # is not a comment: %v", got)
	}
	for _, c := range []string{"cat<<EOF\ngit commit -m x\nEOF\nmake test", "cat 0<<EOF\ngit commit -m x\nEOF\nmake test", "cat <<EOF >x; true\ngit commit -m x\nEOF\nmake test"} {
		if got := commandPhrases(c); got != nil {
			t.Fatalf("commandPhrases(%q) = %v, want nothing (nothing after a << is read)", c, got)
		}
	}
}

// Property 11: a new page saved with related in the same submit changes the reviewed page's key (a new dependency); the review is still
// recorded.
func TestANewRelatedPageInTheSameSubmitStillRecordsTheReview(t *testing.T) {
	_, _, h, draft, _ := memoryEffectFixture(t)
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	tasks, _ := q["maintenance"].([]any)
	var task map[string]any
	for _, x := range tasks {
		if m := x.(map[string]any); m["page_id"] == draft.ID {
			task = m
		}
	}
	if task == nil {
		t.Fatalf("precondition: the fixture page waits for its first review: %v", tasks)
	}
	result := reflectCall(t, h, map[string]any{"operation": "submit",
		"wiki":        []map[string]any{{"title": "Related note", "lesson": "Something else to remember about the README.", "applies_to": "another step", "kind": "rule", "scene": draft.Scene, "sources": draft.Sources, "related": []string{draft.ID}}},
		"maintenance": []map[string]any{{"page_id": draft.ID, "revision": task["revision"], "key": task["key"], "reason": "read the body and the sources; linked a new note"}},
	})
	receipts, _ := result["maintenance"].([]any)
	if len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil || result["complete"] != true {
		t.Fatalf("the review of a page whose key this submit's own new related page changed is recorded: %v", result)
	}
}

// Property 13: the rules the brief leaves out are named by their ids, so the agent can read each with wiki query lesson:<id>.
func TestTheBriefNamesTheAlwaysRulesItLeftOut(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	ids := []string{}
	for i := 0; i < sessionBriefRules+2; i++ {
		d := draft
		d.ID, d.Title, d.Lesson, d.Scene, d.AppliesTo = "", fmt.Sprintf("Always rule %d", i), fmt.Sprintf("Rule number %d: never forget it.", i), "\u3044\u3064\u3082", "every request"
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Page.ID)
	}
	brief, err := sessionBrief(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !strings.Contains(brief, id) {
			t.Fatalf("every always rule is either quoted or named by its id (%s missing):\n%s", id, brief)
		}
	}
	if !strings.Contains(brief, "shikii wiki query lesson:") {
		t.Fatalf("the way to read a named rule is given:\n%s", brief)
	}
}

// Property 14 (after three audits each found one more encoding): the check reads the character itself and the Go escapes this project
// writes Japanese with (\u with 4 hex digits, \U with 8). Other encodings are out of its scope, not listed.
func TestTheForbiddenCharacterIsRecognizedAsWrittenHere(t *testing.T) {
	for _, text := range []string{string(forbiddenRune), "\\u" + "5370", "\\U" + "00005370", "x\\u" + "5370y"} {
		if !spellsTheForbiddenCharacter("before " + text + " after") {
			t.Fatalf("%q spells the character", text)
		}
	}
	for _, text := range []string{"plain text", "\\u" + "5371", "\\U" + "00015370", "U+5370 (the code point named in a document)", "\\u" + "537"} {
		if spellsTheForbiddenCharacter(text) {
			t.Fatalf("%q does not spell the character", text)
		}
	}
}

// Property 10: a head's deliveries are cut short, their corrections included.
func TestAHeadCutsItsCorrectionsShortToo(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	long := strings.Repeat("the request goes on and on ", 50)
	quote := strings.Repeat("q", 400)
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("%016x", 0x500000+i)
		if err := st.AppendFeedback(scope, store.Feedback{ID: id, Request: long, Summary: long, Reply: long, At: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
		q := store.MemoryQuote{Key: "lesson:" + draft.ID + ":" + revision, ID: draft.ID, Revision: revision, Lesson: "short", AppliesTo: draft.AppliesTo, Sources: draft.Sources, Correction: &store.Correction{SourceID: draft.Sources[0], Quote: quote, Action: quote}}
		if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: id, Scope: scope, Request: long, Phrase: "request", Quotes: []store.MemoryQuote{q}}); err != nil {
			t.Fatal(err)
		}
	}
	view, err := reflectQuery(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	heads := 0
	for _, s := range view.Sources {
		if !s.Head {
			continue
		}
		heads++
		for _, m := range s.Memories {
			if c := m.Quote.Correction; c != nil && (len(c.Quote) > reflectHeadBytes || len(c.Action) > reflectHeadBytes) {
				t.Fatalf("a head's delivery cuts its correction too: quote=%d action=%d", len(c.Quote), len(c.Action))
			}
		}
	}
	if heads == 0 {
		t.Fatal("precondition: at least one head")
	}
}

// Confirmation audit: the request door asks lesson-applies for at most lessonAppliesMaxAsks candidates too.
func TestTheRequestDoorAsksLessonAppliesForAFewCandidatesOnly(t *testing.T) {
	repo := gitRepo(t)
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "Bump the README badges") {
			return 0.9
		}
		return 0.1
	})
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	source, _ := reviewSeed(t, st, scope, 1)
	book := notebook.Book{Root: agent, Scope: scope}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{source}, Title: "Bump the README badges with the version", Lesson: versionLessonText, AppliesTo: "when bumping shikii's version", Kind: "rule", Scene: "when bumping the version"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := book.Ingest(notebook.Draft{Sources: []string{source}, Title: fmt.Sprintf("Softer page colours %d", i), Lesson: fmt.Sprintf("Rule %d: soften the colours of the page with the palette file.", i), AppliesTo: "when the colours of the page change", Kind: "rule", Scene: "when styling"}); err != nil {
			t.Fatal(err)
		}
	}
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	sp := lowerMinN(t, agent, "lesson-applies", 2)
	for i := 0; i < 3; i++ {
		s := string(rune('a' + i))
		toolHook(t, scope, "v"+s, toolPrompt, "Edit", edit)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "r" + s, "cwd": scope, "prompt": toolPrompt})
	}
	recs, _ := st.Records("lesson-applies", 0)
	for _, r := range recs {
		truth := "no"
		if strings.Contains(r.Inputs["title"], "Bump") {
			truth = "yes"
		}
		capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", r.TraceID, truth}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{sp, "--store", agent, "--model", "kev-4b", "--write"}) })
	before, _ := st.Records("lesson-applies", 0)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "cap", "cwd": scope, "prompt": toolPrompt})
	after, _ := st.Records("lesson-applies", 0)
	if n := len(after) - len(before); n < 1 || n > lessonAppliesMaxAsks {
		t.Fatalf("one request asks lesson-applies at most %d times (asked %d)", lessonAppliesMaxAsks, n)
	}
}

// Confirmation audit: a stopped lesson-applies resumes when another model answers the 1-in-20 ask, like the hook's questions.
func TestAStoppedLessonAppliesResumesOnANewModel(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	sp, err := spec.Load(hookSpecPath(st.Dir, lessonAppliesSpec))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(stoppedLine{Model: "another-model", SpecHash: sp.Hash(), AUC: 0.5, Low: 0.4, High: 0.6, Pos: 60, Neg: 60, At: time.Now()})
	if err := os.WriteFile(stoppedPath(st.Dir, lessonAppliesSpec, sp.Hash()), b, 0o600); err != nil {
		t.Fatal(err)
	}
	page := notebook.Page{ID: draft.ID, Title: draft.Title, Lesson: draft.Lesson, AppliesTo: draft.AppliesTo}
	request := ""
	for i := 0; i < 400; i++ {
		r := fmt.Sprintf("update the README install steps, try %d", i)
		if stoppedAsk(lessonAppliesSpec, map[string]string{"request": r, "target": lessonAppliesRequestTarget, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo}) {
			request = r
			break
		}
	}
	if request == "" {
		t.Fatal("precondition: no request hashes to the 1-in-20 ask")
	}
	if trace, skip := lessonApplies(st.Dir, scope, request, lessonAppliesRequestTarget, page); trace == "" || skip {
		t.Fatalf("precondition: asked anyway (trace %q, skip %v)", trace, skip)
	}
	if _, err := os.Stat(stoppedPath(st.Dir, lessonAppliesSpec, sp.Hash())); err == nil {
		t.Fatal("the stop measured on another model is lifted once this model answered")
	}
}

// Confirmation audit: Maintain names the real reason when the page's own sources cannot be checked.
func TestMaintainNamesTheSourcesWhenTheyCannotBeChecked(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	tasks, _ := q["maintenance"].([]any)
	var task map[string]any
	for _, x := range tasks {
		if m := x.(map[string]any); m["page_id"] == draft.ID {
			task = m
		}
	}
	if task == nil {
		t.Fatalf("precondition: %v", tasks)
	}
	matches, _ := filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", "*", draft.Sources[0]+".json"))
	if len(matches) != 1 {
		t.Fatalf("precondition: %v", matches)
	}
	if err := os.Remove(matches[0]); err != nil {
		t.Fatal(err)
	}
	_, err := (notebook.Book{Root: st.Dir, Scope: scope}).Maintain(notebook.MaintenanceReview{PageID: draft.ID, Revision: task["revision"].(string), Key: task["key"].(string), Reason: "read it"})
	if err == nil || !strings.Contains(err.Error(), "sources cannot be checked") {
		t.Fatalf("the refusal names the sources: %v", err)
	}
}

// Confirmation audit: one broken assessment file does not stop the reflection query either.
func TestOneBrokenAssessmentFileDoesNotStopTheReflectionQuery(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	if ctx := feedbackContext(t, toolHook(t, scope, "k1", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("precondition:\n%s", ctx)
	}
	d := lastToolDelivery(t, st, scope)
	memoryEffectComplete(t, st, scope, "k1")
	assess(t, st, scope, d, ids["version"], "unused")
	files, _ := filepath.Glob(filepath.Join(st.Dir, "records", "memory-reviews", "*", "*.json"))
	if len(files) != 1 {
		t.Fatalf("one assessment file: %v", files)
	}
	if err := os.WriteFile(files[0], []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reflectQuery(st.Dir, scope); err != nil {
		t.Fatalf("the reflection query treats a broken assessment as not yet assessed: %v", err)
	}
}

// Confirmation audit 2: an assessment file whose content cannot be read is listed as not yet assessed, and the assessment the agent then
// sends replaces it (before, the save refused it with the same error for ever).
func TestABrokenAssessmentFileIsReplacedByTheNextAssessment(t *testing.T) {
	st, scope, h, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	if ctx := feedbackContext(t, toolHook(t, scope, "k1", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("precondition:\n%s", ctx)
	}
	d := lastToolDelivery(t, st, scope)
	memoryEffectComplete(t, st, scope, "k1")
	assess(t, st, scope, d, ids["version"], "unused")
	files, _ := filepath.Glob(filepath.Join(st.Dir, "records", "memory-reviews", "*", "*.json"))
	if len(files) != 1 {
		t.Fatalf("one assessment file: %v", files)
	}
	if err := os.WriteFile(files[0], []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": d.ID, "key": ids["version"], "result": "unused", "decision": "d", "evidence": "e"}}})
	if receipt := out["memory"].([]any)[0].(map[string]any); receipt["error"] != nil || out["complete"] != true {
		t.Fatalf("the assessment replaces the broken file: %v", out)
	}
	report, err := st.MemoryReport(scope)
	if err != nil || report.Stats.Unused != 1 || report.Stats.Pending != 0 {
		t.Fatalf("the delivery is assessed once more: %+v %v", report.Stats, err)
	}
}

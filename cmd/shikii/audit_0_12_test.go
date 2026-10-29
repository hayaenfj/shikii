package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The findings of the three independent audits of 0.12.0 (.shikii/handoff/audit-0.12/audit-final-{A,B,C}.md), each as a test that
// failed before the fix and passes after it.

// Property 9 (audits A, confirmation 1 and 2 each found a here-document form misread, so the rule is one line now): a command is read
// up to its first <<, wherever it stands; what comes before it on that line is read, nothing after it is.
func TestNothingAfterAHereDocumentOperatorIsRead(t *testing.T) {
	for command, want := range map[string][]string{
		"grep -n '<<EOF' build.sh\ngit commit -m x":          nil,
		"make test && grep -rn \"<<\" scripts\ngit commit":   {"make test"},
		"echo $((1<<2))\ngit commit -m x":                    nil,
		"cat <<EOF>out.txt\ngit commit -m x\nEOF\nmake test": nil,
		"cat<<EOF\ngit commit -m x\nEOF\nmake test":          nil,
		"go test ./... <<< 'git commit'":                     {"go test"},
	} {
		if got := commandPhrases(command); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("commandPhrases(%q) = %v, want %v", command, got, want)
		}
	}
}

// Audit A, property 9: the third word of shikii and npm skips a lone short option's value.
func TestNestedThirdWordSkipsALoneShortOptionsValue(t *testing.T) {
	for command, want := range map[string]string{
		"npm run -w web check":             "npm run check",
		"shikii wiki -s mystore query foo": "shikii wiki query",
		"shikii hook --yes install":        "shikii hook install",
		"npm run check":                    "npm run check",
	} {
		if got := commandPhrases(command); len(got) != 1 || got[0] != want {
			t.Fatalf("commandPhrases(%q) = %v, want [%s]", command, got, want)
		}
	}
}

// Audit A, property 1: a delivery that carries only the person's answer has no door, and the pair effects count lessons only.
func TestADeliveryOfThePersonsAnswerAloneHasNoDoor(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	source, _ := reviewSeed(t, st, scope, 1)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	r, err := book.Ingest(notebook.Draft{Sources: []string{source}, Title: "Translate the README setup steps", Lesson: "When you translate the README setup steps into Japanese, check the install commands too.", AppliesTo: "when translating the README setup steps", Kind: "rule", Scene: "when translating"})
	if err != nil {
		t.Fatal(err)
	}
	key := "lesson:" + r.Page.ID + ":" + r.Page.Revision
	trace := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	ctx, err := memoryContext(st.Dir, scope, similarSetupRequest, &hookSession{TurnID: "aaaaaaaaaaaaaa01"})
	if err != nil || !strings.Contains(ctx, key) || !strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("precondition: lesson and answer both delivered: %v\n%s", err, ctx)
	}
	s := &hookSession{TurnID: "aaaaaaaaaaaaaa02", Memories: []memoryDelivery{{Key: key, At: time.Now().UTC()}}}
	ctx, err = memoryContext(st.Dir, scope, similarSetupRequest, s)
	if err != nil || strings.Contains(ctx, key) || !strings.Contains(ctx, "decision:"+trace) {
		t.Fatalf("precondition: only the answer delivered: %v\n%s", err, ctx)
	}
	ds, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == "aaaaaaaaaaaaaa02" {
			if d.Phrase != "" {
				t.Fatalf("a delivery of the person's answer alone has no door, got %q", d.Phrase)
			}
			return
		}
	}
	t.Fatal("delivery not recorded")
}

// Audit A, property 2: the silence rests on the assessments themselves, not on the window of the latest 256 deliveries.
func TestTheSilenceSurvivesManyLaterDeliveries(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	commit := map[string]any{"command": "git commit -m x"}
	var d store.MemoryDelivery
	for _, session := range []string{"a", "b"} {
		if ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Bash", commit)); !strings.Contains(ctx, ids["commit"]) {
			t.Fatalf("precondition: delivered in %s:\n%s", session, ctx)
		}
		d = lastToolDelivery(t, st, scope)
		memoryEffectComplete(t, st, scope, session)
		assess(t, st, scope, d, ids["commit"], "unused")
	}
	if out := toolHook(t, scope, "c", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("precondition: silent at git commit:\n%s", out)
	}
	for i := 0; i < 260; i++ {
		q := d.Quotes[0]
		q.Trace = ""
		if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: fmt.Sprintf("%016x", 0x100000+i), Scope: scope, Request: "another request", Quotes: []store.MemoryQuote{q}}); err != nil {
			t.Fatal(err)
		}
	}
	if out := toolHook(t, scope, "e", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("unused twice and never helpful at git commit: still silent after 260 later deliveries:\n%s", out)
	}
}

// Audit A, property 2: silenced at git commit, the lesson still comes through another door it names when both are in one command line.
func TestACompoundCommandDeliversThroughTheDoorThatIsNotSilenced(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	commit := map[string]any{"command": "git commit -m x"}
	for _, session := range []string{"a", "b"} {
		if ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Bash", commit)); !strings.Contains(ctx, ids["commit"]) {
			t.Fatalf("precondition: delivered in %s:\n%s", session, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		memoryEffectComplete(t, st, scope, session)
		assess(t, st, scope, d, ids["commit"], "unused")
	}
	if out := toolHook(t, scope, "c", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("precondition: silent at git commit alone:\n%s", out)
	}
	ctx := feedbackContext(t, toolHook(t, scope, "d", toolPrompt, "Bash", map[string]any{"command": "git commit -m x && gh release create v0.12.0 --draft"}))
	if !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("the lesson names gh release, which is not silenced: it arrives through that door:\n%s", ctx)
	}
	if d := lastToolDelivery(t, st, scope); d.Phrase != "gh release" {
		t.Fatalf("the door recorded is the one it came through: %q", d.Phrase)
	}
}

// Audit A: one unreadable assessment file does not silence the deliveries.
func TestOneBrokenAssessmentFileDoesNotSilenceDeliveries(t *testing.T) {
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
	if ctx := feedbackContext(t, toolHook(t, scope, "k2", toolPrompt, "Bash", map[string]any{"command": "git commit -m x"})); !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("the commit lesson (never assessed) still arrives before git commit:\n%s", ctx)
	}
	if ctx := memoryEffectPrompt(t, scope, "k3"); !strings.Contains(ctx, "lesson:") {
		t.Fatalf("the request's delivery is not silenced by a broken assessment file either:\n%s", ctx)
	}
}

// Audit A, property 7: the assessment is saved and its receipt is clean even when the decision's record cannot be found.
func TestAnAssessmentIsReceivedEvenWhenItsDecisionRecordIsGone(t *testing.T) {
	st, scope, h, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	if ctx := feedbackContext(t, toolHook(t, scope, "m", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("precondition:\n%s", ctx)
	}
	d := lastToolDelivery(t, st, scope)
	if d.Quotes[0].Trace == "" {
		t.Fatal("precondition: trace")
	}
	memoryEffectComplete(t, st, scope, "m")
	if err := os.Remove(filepath.Join(st.Dir, "records", "lesson-applies.jsonl")); err != nil {
		t.Fatal(err)
	}
	result := memoryEffectSubmit(t, h, d.ID, ids["version"], "helpful", "assessed in a test", "a test")
	receipts, _ := result["memory"].([]any)
	if len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil || result["complete"] != true {
		t.Fatalf("the assessment is saved and the receipt is clean (the truth is best effort): %v", result)
	}
}

// Audit A: at the request door the lesson-applies model call runs outside the session lock, as at the tool door.
func TestTheRequestDoorAsksTheModelOutsideTheSessionLock(t *testing.T) {
	repo := gitRepo(t)
	var agentDir string
	held := map[string]bool{}
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "lesson title:") && agentDir != "" {
			for _, session := range []string{"lock-request", "lock-tool"} {
				if unlock, ok := lockSessionWithin(agentDir, session, 0); ok {
					unlock()
				} else {
					held[session] = true
				}
			}
		}
		return .6
	})
	agentDir = hookEnv(t, model, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agentDir}, feedbackScope(repo)
	id, _ := reviewSeed(t, st, scope, 1)
	book := notebook.Book{Root: agentDir, Scope: scope}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{id}, Title: "Softer page colours", Lesson: "Soften the colours of the page with the palette file, never by hand.", Kind: "rule", Scene: "when styling", AppliesTo: "when the colours of the page change"}); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{id}, Title: "Check the exit code before committing", Lesson: commitLessonText, AppliesTo: "when committing in shikii", Kind: "rule", Scene: "when committing"}); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "lock-request", "cwd": scope, "prompt": "make the colours of the page softer"}))
	if !strings.Contains(ctx, "lesson:") {
		t.Fatalf("precondition: a lesson was delivered at the request:\n%s", ctx)
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "lock-tool", toolPrompt, "Bash", map[string]any{"command": "git commit -m x"})); !strings.Contains(ctx, "lesson:") {
		t.Fatalf("precondition: a lesson was delivered at the tool:\n%s", ctx)
	}
	if held["lock-tool"] || held["lock-request"] {
		t.Fatalf("the session lock is never held while the model answers lesson-applies: tool=%v request=%v", held["lock-tool"], held["lock-request"])
	}
}

// Audit A: when every candidate is a trusted no, the hook asks the model for at most lessonAppliesMaxAsks candidates per tool call.
func TestTheHookAsksLessonAppliesForAFewCandidatesOnly(t *testing.T) {
	repo := gitRepo(t)
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "Bump the README badges") {
			return 0.9
		}
		return 0.1 // every lesson about committing is a confident no
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
		if _, err := book.Ingest(notebook.Draft{Sources: []string{source}, Title: fmt.Sprintf("Commit rule %d", i), Lesson: fmt.Sprintf("Rule %d about git commit.", i), AppliesTo: "when running git commit", Kind: "rule", Scene: "when committing"}); err != nil {
			t.Fatal(err)
		}
	}
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	commit := map[string]any{"command": "git commit -m x"}
	spec := lowerMinN(t, agent, "lesson-applies", 2)
	for i := 0; i < 2; i++ {
		s := string(rune('a' + i))
		toolHook(t, scope, "v"+s, toolPrompt, "Edit", edit)
		toolHook(t, scope, "c"+s, toolPrompt, "Bash", commit)
	}
	recs, _ := st.Records("lesson-applies", 0)
	for _, r := range recs {
		truth := "no"
		if strings.Contains(r.Inputs["title"], "Bump") {
			truth = "yes"
		}
		capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", r.TraceID, truth}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{spec, "--store", agent, "--model", "kev-4b", "--write"}) })
	before, _ := st.Records("lesson-applies", 0)
	if out := toolHook(t, scope, "cap", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("precondition: every commit lesson is a trusted no, nothing is delivered:\n%s", out)
	}
	after, _ := st.Records("lesson-applies", 0)
	if n := len(after) - len(before); n < 1 || n > lessonAppliesMaxAsks {
		t.Fatalf("one tool call asks lesson-applies at most %d times (asked %d of 6 candidates)", lessonAppliesMaxAsks, n)
	}
}

// Audit B, property 11: the key the query offers for a page whose related page lost its source is the key submit checks.
func TestAMaintenanceKeyStillMatchesWhenARelatedPageLosesItsSource(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	id2, _ := reviewSeed(t, st, scope, 2)
	p := draft
	p.ID, p.Title, p.Lesson, p.Sources, p.AppliesTo = "", "Related page", "Something else to remember.", []string{id2}, "another step"
	rp, err := book.Ingest(p)
	if err != nil {
		t.Fatal(err)
	}
	x := draft
	x.Related = []string{rp.Page.ID}
	if _, err := book.Ingest(x); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", "*", id2+".json"))
	if len(matches) != 1 {
		t.Fatalf("precondition: one source record for %s: %v", id2, matches)
	}
	if err := os.Remove(matches[0]); err != nil {
		t.Fatal(err)
	}
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	tasks, _ := q["maintenance"].([]any)
	var task map[string]any
	for _, x := range tasks {
		if m := x.(map[string]any); m["page_id"] == draft.ID {
			task = m
		}
	}
	if task == nil {
		t.Fatalf("precondition: the query still offers the page for review: %v", tasks)
	}
	result := reflectCall(t, h, map[string]any{"operation": "submit", "maintenance": []map[string]any{{"page_id": draft.ID, "revision": task["revision"], "key": task["key"], "reason": "read the body and the sources"}}})
	receipts, _ := result["maintenance"].([]any)
	if len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil {
		t.Fatalf("the key the query offered is the key submit checks: %v", result)
	}
}

// Audit B, property 13: the work record is a markdown or text file; a JSON dump newer than the plan is not "the work record".
func TestTheWorkRecordIsAMarkdownOrTextFileNotAJSONDump(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	book := handoff.Book{Store: st.Dir}
	if _, err := book.Write("plan/scope.md", "# Scope\n\n## Where the work stands\nA and B are in; C is next.\n", false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := book.Write("audit/measurements.json", `{"runs":[{"n":1,"p":0.61}]}`, false); err != nil {
		t.Fatal(err)
	}
	brief, err := sessionBrief(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(brief, "audit/measurements.json") || !strings.Contains(brief, "plan/scope.md") {
		t.Fatalf("the plan, not the JSON dump, is the work record:\n%s", brief)
	}
}

// Audit B, property 13: with more always rules than fit, the brief says how many are left and how to read them.
func TestTheBriefSaysHowManyAlwaysRulesItLeftOut(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	for i := 0; i < sessionBriefRules+2; i++ {
		d := draft
		d.ID, d.Title, d.Lesson, d.Scene, d.AppliesTo = "", fmt.Sprintf("Always rule %d", i), fmt.Sprintf("Rule number %d: never forget it.", i), "\u3044\u3064\u3082", "every request"
		if _, err := book.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	brief, err := sessionBrief(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(brief, "(lesson:") != sessionBriefRules || !strings.Contains(brief, "2 more rules for every request") {
		t.Fatalf("%d rules are listed and the rest are counted, with the way to read them:\n%s", sessionBriefRules, brief)
	}
}

// Audit B, property 10 (size): a head carries its deliveries to assess as keys, with the texts cut short.
func TestAHeadCarriesItsDeliveriesCutShort(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	long := strings.Repeat("the request goes on and on ", 50) // 1,350 bytes, under the 1,500 a field keeps
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("%016x", 0x300000+i)
		if err := st.AppendFeedback(scope, store.Feedback{ID: id, Request: long, Summary: long, Reply: long, At: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
		q := store.MemoryQuote{Key: "lesson:" + draft.ID + ":" + revision, ID: draft.ID, Revision: revision, Lesson: strings.Repeat("a long lesson text ", 40), AppliesTo: draft.AppliesTo, Sources: draft.Sources}
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
			if m.Quote.Key == "" || len(m.Quote.Lesson) > reflectHeadBytes || len(m.Request) > reflectHeadBytes {
				t.Fatalf("a head's delivery keeps its key and cuts its texts to %d bytes: lesson=%d request=%d", reflectHeadBytes, len(m.Quote.Lesson), len(m.Request))
			}
		}
	}
	if heads == 0 {
		t.Fatal("precondition: at least one head")
	}
}

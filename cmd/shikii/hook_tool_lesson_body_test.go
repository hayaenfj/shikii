package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.12 item 2: a lesson that names the file or command only inside a code span of its body (`cmd/shikii/version.go`, `make test`, a
// fenced block) is delivered too: in hayabusa-lang 104 of 152 pages name their command or file only there. Plain body text does not
// count (a body names many things in passing), and a page whose title or applies_to names the target still ranks first.
func TestToolLessonFindsNamesInsideBodyCodeSpans(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	ingest := func(title, lesson, applies, body string) string {
		r, err := book.Ingest(notebook.Draft{Sources: draft.Sources, Title: title, Lesson: lesson, AppliesTo: applies, Body: &body, Kind: "howto", Scene: "when releasing"})
		if err != nil {
			t.Fatal(err)
		}
		return "lesson:" + r.Page.ID + ":" + r.Page.Revision
	}
	span := ingest("Bump the badges with the version", "Update both README badges when the version changes.", "when bumping the version", "## Steps\n1. Edit `cmd/shikii/version.go`.\n2. Run the tests:\n```sh\nmake test\n```\n")
	plain := ingest("Release record", "Write the release record after publishing.", "when publishing", "The version lives in cmd/shikii/version.go and make test runs before the tag.")
	ctx := feedbackContext(t, toolHook(t, scope, "span", toolPrompt, "Edit", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}))
	if !strings.Contains(ctx, span) || strings.Contains(ctx, plain) || lessonKeys(ctx) != 1 {
		t.Fatalf("the page naming version.go inside a code span of its body arrives; the one naming it in plain body text does not:\n%s", ctx)
	}
	ctx = feedbackContext(t, toolHook(t, scope, "cmd", toolPrompt, "Bash", map[string]any{"command": "make test"}))
	if !strings.Contains(ctx, span) || strings.Contains(ctx, plain) {
		t.Fatalf("the same for a command named inside a fenced block of the body:\n%s", ctx)
	}
	condition := ingest("Check the version test", "The version test holds the number.", "when editing cmd/shikii/version.go", "")
	ctx = feedbackContext(t, toolHook(t, scope, "rank", toolPrompt, "Edit", map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}))
	if !strings.Contains(ctx, condition) || lessonKeys(ctx) != 1 {
		t.Fatalf("the page whose condition names the file comes before one naming it only in its body:\n%s", ctx)
	}
}

// 0.12 audit C: a name only in a code span of the body ranks after a name in the lesson (the documents: "ranked after pages naming it
// in their title, applies_to or lesson"). Before the fix both scored 1 and the page order decided.
func TestABodyOnlyMatchRanksAfterALessonOnlyMatch(t *testing.T) {
	target := toolTarget{label: "Bash make test", phrases: commandPhrases("make test")}
	lessonOnly := notebook.Page{Title: "Release record", Lesson: "Run make test before the tag.", AppliesTo: "when publishing"}
	bodyOnly := notebook.Page{Title: "Bump the badges", Lesson: "Update both README badges.", AppliesTo: "when bumping the version", Body: "Run `make test` first."}
	ls, lc := toolLessonScore(lessonOnly, target)
	bs, bc := toolLessonScore(bodyOnly, target)
	a, b := toolCandidate{page: lessonOnly, score: ls, inCondition: lc}, toolCandidate{page: bodyOnly, score: bs, inCondition: bc}
	if !a.before(b) || b.before(a) {
		t.Fatalf("a lesson-only match ranks before a body-only match: lesson %d/%v, body %d/%v", ls, lc, bs, bc)
	}
	// and a condition match (applies_to) still ranks before both
	condition := notebook.Page{Title: "Check the tests", Lesson: "The tests hold the number.", AppliesTo: "when running make test"}
	cs, cc := toolLessonScore(condition, target)
	c := toolCandidate{page: condition, score: cs, inCondition: cc}
	if !c.before(a) || a.before(c) {
		t.Fatalf("a condition match ranks before a lesson-only match: condition %d/%v, lesson %d/%v", cs, cc, ls, lc)
	}
	// at the hook: with both pages live, the one naming make test in its lesson is the one delivered, whatever the page order
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	keys := map[string]string{}
	for name, p := range map[string]notebook.Page{"lesson": lessonOnly, "body": bodyOnly} {
		body := p.Body
		r, err := book.Ingest(notebook.Draft{Sources: draft.Sources, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Body: &body, Kind: "howto", Scene: "when releasing"})
		if err != nil {
			t.Fatal(err)
		}
		keys[name] = "lesson:" + r.Page.ID + ":" + r.Page.Revision
	}
	ctx := feedbackContext(t, toolHook(t, scope, "rank", toolPrompt, "Bash", map[string]any{"command": "make test"}))
	if !strings.Contains(ctx, keys["lesson"]) || strings.Contains(ctx, keys["body"]) || lessonKeys(ctx) != 1 {
		t.Fatalf("the page naming make test in its lesson comes before the one naming it only in its body:\n%s", ctx)
	}
}

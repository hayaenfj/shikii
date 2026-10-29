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

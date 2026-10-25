package notebook

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Audit 0.9.2 (English): what shikii writes for an English user must be English.
var japanese = regexp.MustCompile("[\u3040-\u30ff\u4e00-\u9fff]")

// Every wiki page markdown (read by agents that open .shikii/wiki, and by people) carries Japanese labels on main.
func TestAuditEnglishWikiPageMarkdownHasNoJapanese(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	body := "Exit 1 means a mismatch."
	d := Draft{Title: "Check invoice totals", Lesson: "Run scripts/check-totals.sh before sending invoices.", AppliesTo: "invoices in invoices/",
		Kind: "howto", Scene: "when checking invoices", Sources: []string{id}, ChangeReason: "the script was added", Body: &body}
	res, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := b.directory(false, "pages")
	md, err := os.ReadFile(filepath.Join(pages, res.Page.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if loc := japanese.FindIndex(md); loc != nil {
		t.Fatalf("an English page contains Japanese: %q", md[max(0, loc[0]-40):min(len(md), loc[1]+80)])
	}
}

// The scene limit (24 characters) was sized for Japanese scenes. The English rendering of a typical scene is longer.
func TestAuditEnglishSceneOfTypicalLengthIsAccepted(t *testing.T) {
	for _, scene := range []string{"when checking invoice totals", "when sending an invoice to a client"} {
		if !validKindScene("howto", scene) {
			t.Errorf("an ordinary English scene is refused (%d characters): %q", len([]rune(scene)), scene)
		}
	}
}

// Pages written before 0.9.2 (with the Japanese labels) stay readable and can be updated after the labels became English.
// Without this, updating shikii would turn every existing page into a changed page (left out of search, refusing saves).
func TestPagesWrittenWithOldLabelsStayValid(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	body := "Exit 1 means a mismatch."
	res, err := b.Ingest(Draft{Title: "Check invoice totals", Lesson: "Run the check before sending invoices.", AppliesTo: "invoices", Kind: "howto", Scene: "when checking invoices",
		Sources: []string{id}, ChangeReason: "the script was added", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	old, err := b.renderPage(*res.Page, legacyLabels) // what 0.9.1 wrote for the same page
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := b.directory(false, "pages")
	for _, f := range []string{filepath.Join(dir, res.Page.ID+".md"), filepath.Join(dir, "revisions", res.Page.ID+"-"+res.Page.Revision+".md")} {
		if err := os.WriteFile(f, old, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := b.Lint()
	if err != nil || len(report.Issues) != 0 {
		t.Fatalf("a page with the old labels is not a changed page: %+v %v", report.Issues, err)
	}
	if live, _ := b.Live(); len(live) != 1 {
		t.Fatalf("a page with the old labels is still found: %+v", live)
	}
	if _, err := b.Classify(res.Page.ID, "rule", "when checking invoices"); err != nil {
		t.Fatalf("a page with the old labels can be updated: %v", err)
	}
	md, _ := os.ReadFile(filepath.Join(dir, res.Page.ID+".md"))
	if japanese.Match(md) {
		t.Fatalf("the updated page is written with the English labels: %s", md)
	}
}

// A scene that is too long says so (with its length and the limit), instead of the general "a lesson needs kind and scene" (audit 0.9.2 English, medium 2).
func TestTooLongSceneSaysItIsTooLong(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	d := draft(id, "A rule page")
	d.Scene = "when " + strings.Repeat("checking the invoices ", 4)
	_, err := b.Ingest(d)
	if err == nil || !strings.Contains(err.Error(), "too long") || !strings.Contains(err.Error(), fmt.Sprint(SceneMax)) {
		t.Fatalf("a too-long scene says it is too long and gives the limit: %v", err)
	}
}

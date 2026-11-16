package notebook

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The session a conversation took place in says who assesses its deliveries; it is not what was said. A lesson made from the
// conversation cites the same source hash with and without it, so a shikii from before the field, which reads the record
// without the session, still finds the page's source unchanged.
func TestASourceHashDoesNotDependOnTheConversationsSession(t *testing.T) {
	hash := func(session string) string {
		t.Helper()
		b := Book{Root: t.TempDir(), Scope: "/project/session-hash"}
		f := store.Feedback{ID: "00000000000000a1", Request: "README install instructions", Summary: "updated instructions", Reply: "model startup instructions are missing", At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Session: session}
		if err := (store.Store{Dir: b.Root}).AppendFeedback(b.Scope, f); err != nil {
			t.Fatal(err)
		}
		res, err := b.Ingest(Draft{Sources: []string{f.ID}, Title: "Install instructions", Lesson: "Name the model startup step.", AppliesTo: "README install instructions", Kind: "rule", Scene: "when writing documents"})
		if err != nil || res.Page == nil || len(res.Page.Sources) != 1 {
			t.Fatalf("a lesson made from the conversation: %v %v", res, err)
		}
		if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
			t.Fatalf("the page's source is found unchanged (session %q): %v %v", session, report.Issues, err)
		}
		// without the archived copy, reading the source checks the conversation's own record (which names the session) against the hash
		archived, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", f.ID+".json"))
		if err != nil || len(archived) != 1 {
			t.Fatalf("the archived copy of the source: %v %v", archived, err)
		}
		if err := os.Remove(archived[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := b.ReadSources([]string{f.ID}); err != nil {
			t.Fatalf("read from the conversation's record, the source is still found unchanged (session %q): %v", session, err)
		}
		return res.Page.Sources[0].Hash
	}
	if without, with := hash(""), hash("session-a"); without != with {
		t.Fatalf("the source hash does not depend on the session: %s (none) / %s (named)", without, with)
	}
}

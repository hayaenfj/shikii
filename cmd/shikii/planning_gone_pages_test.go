package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// An article a work withdrew, or a person deleted, is not handed out. Naming its id in pages does not read its body for a decision:
// the reply says it was not read and why, and the live article named beside it is still read.
func TestPlanningPagesDoNotReadAWithdrawnOrDeletedArticle(t *testing.T) {
	for _, state := range []string{"withdrawn", "deleted"} {
		t.Run(state, func(t *testing.T) {
			b, source := loopBook(t)
			live := loopPage(t, b, source, map[string]any{"title": "Live article", "body": "The live body."})
			gone := loopPage(t, b, source, map[string]any{"title": "Gone article", "body": "The body that is no longer handed out."})
			var err error
			if state == "withdrawn" {
				_, err = b.Withdraw(notebook.Withdrawal{ID: gone.ID, Revision: gone.Revision, Sources: []string{source}, Reason: "the request was deleted"})
			} else {
				_, err = b.Manage(notebook.Change{ID: gone.ID, Revision: gone.Revision, Action: "delete"})
			}
			if err != nil {
				t.Fatal(err)
			}
			read, err := b.Query("lesson:" + gone.ID)
			if err != nil || len(read.Lessons) != 1 {
				t.Fatalf("the article is still readable by its id: %+v %v", read, err)
			}
			sources := map[string]string{"current_user": "Review these articles"}
			notice, k, err := preparePlanningKnowledge(b.Root, b.Scope, planningRequest{Pages: []string{live.ID, gone.ID}}, sources)
			if err != nil {
				t.Fatal(err)
			}
			if len(k.Read) != 1 || k.Read[0].ID != live.ID || !strings.Contains(sources["article:"+live.ID+":"+live.Revision], "The live body.") {
				t.Fatalf("only the live article is read: %+v", k.Read)
			}
			for key, text := range sources {
				if strings.Contains(text, "no longer handed out") {
					t.Fatalf("the body of a %s article was handed out in %s", state, key)
				}
			}
			if !strings.Contains(notice, gone.ID) || !strings.Contains(notice, state) {
				t.Fatalf("the reply says the %s article was not read, and why: %q", state, notice)
			}
		})
	}
}

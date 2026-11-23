package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// goneBook is a wiki with a live article and one that is withdrawn (a work took it back) or deleted (a person did). The gone one's
// summary and body are words nothing else in the wiki says, so a test finds them wherever they leak to.
func goneBook(t *testing.T, state string, goneFields map[string]any) (b notebook.Book, live, gone notebook.Page) {
	t.Helper()
	b, source := loopBook(t)
	live = loopPage(t, b, source, map[string]any{"title": "Live article", "lesson": "The live summary.", "body": "The live body."})
	fields := map[string]any{"title": "Gone article", "lesson": "The summary that is no longer handed out."}
	for k, v := range goneFields {
		fields[k] = v
	}
	gone = loopPage(t, b, source, fields)
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
	return b, live, gone
}

// goneQueries are the ways a query can name the gone article: by id, by the revision it had while it was live (an older text that
// still says it is live), and by its current revision.
func goneQueries(t *testing.T, b notebook.Book, gone notebook.Page) []string {
	t.Helper()
	now, err := b.Query("lesson:" + gone.ID)
	if err != nil || len(now.Lessons) != 1 || now.Lessons[0].Revision == gone.Revision {
		t.Fatalf("the article has a revision of its own after it was taken away: %+v %v", now.Lessons, err)
	}
	return []string{"lesson:" + gone.ID, "lesson:" + gone.ID + "@" + gone.Revision, "lesson:" + gone.ID + "@" + now.Lessons[0].Revision}
}

// An article a work withdrew, or a person deleted, is not handed out. Naming its id in pages does not read its body for a decision:
// the reply says it was not read and why, and the live article named beside it is still read.
func TestPlanningPagesDoNotReadAWithdrawnOrDeletedArticle(t *testing.T) {
	for _, state := range []string{"withdrawn", "deleted"} {
		t.Run(state, func(t *testing.T) {
			b, live, gone := goneBook(t, state, map[string]any{"body": "The body that is no longer handed out."})
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

// Naming the id in query ("lesson:<id>") is the same as naming it in pages: the query returns the article whatever its state, and none
// of it (not the summary, not the conditions, not the body, with a body or without) reaches the decision. The reply says it was not
// read and why, once when pages names it too, and the live article named in the same query is a candidate as before.
func TestPlanningQueryDoesNotHandOutAWithdrawnOrDeletedArticle(t *testing.T) {
	for _, state := range []string{"withdrawn", "deleted"} {
		for name, fields := range map[string]map[string]any{"with a body": {"body": "The body that is no longer handed out."}, "without a body": nil} {
			t.Run(state+" "+name, func(t *testing.T) {
				b, live, gone := goneBook(t, state, fields)
				want := "Not read: " + gone.ID + " is " + state
				for _, query := range goneQueries(t, b, gone) {
					sources := map[string]string{"current_user": "Review these articles"}
					notice, k, err := preparePlanningKnowledge(b.Root, b.Scope, planningRequest{Query: query}, sources)
					if err != nil {
						t.Fatalf("%s: %v", query, err)
					}
					if len(k.Candidates) != 0 || len(k.Read) != 0 {
						t.Fatalf("%s: a %s article is neither a candidate nor read: %+v %+v", query, state, k.Candidates, k.Read)
					}
					for key, text := range sources {
						if strings.Contains(text, "no longer handed out") || strings.Contains(key, gone.ID) {
							t.Fatalf("%s: a %s article was handed out in %s: %q", query, state, key, text)
						}
					}
					if !strings.Contains(notice, want) {
						t.Fatalf("%s: the reply says the %s article was not read, and why: %q", query, state, notice)
					}
					if again, _, err := preparePlanningKnowledge(b.Root, b.Scope, planningRequest{Query: query, Pages: []string{gone.ID}}, map[string]string{}); err != nil || strings.Count(again, gone.ID+" is "+state) != 1 {
						t.Fatalf("%s: an article named in both query and pages is said once: %q %v", query, again, err)
					}
				}
				sources := map[string]string{"current_user": "Review these articles"}
				control, kLive, err := preparePlanningKnowledge(b.Root, b.Scope, planningRequest{Query: "lesson:" + live.ID}, sources)
				if err != nil || control != "" || len(kLive.Candidates) != 1 || kLive.Candidates[0].ID != live.ID || !strings.Contains(sources["lesson:"+live.ID+":"+live.Revision], "The live summary.") {
					t.Fatalf("a live article is a candidate as before: %q %+v %v", control, kLive, err)
				}
			})
		}
	}
}

// A work prepared without a request (the agent supplies the task) puts what its query finds into knowledge with the whole article. The
// same applies there, and to a later prepare with the receipt and a query: no withdrawn or deleted article is handed out, and
// context_notice says it was not read and why. A live article is handed out as before.
func TestWorkPrepareDoesNotHandOutAWithdrawnOrDeletedArticleNamedInQuery(t *testing.T) {
	for _, state := range []string{"withdrawn", "deleted"} {
		t.Run(state, func(t *testing.T) {
			b, live, gone := goneBook(t, state, map[string]any{"body": "The body that is no longer handed out."})
			notRead := func(query string, reply map[string]any) {
				t.Helper()
				if entries, _ := reply["knowledge"].([]any); len(entries) != 0 {
					t.Fatalf("%s: a %s article is not put in knowledge: %v", query, state, entries)
				}
				if text, _ := reply["context_notice"].(string); !strings.Contains(text, "Not read: "+gone.ID+" is "+state) {
					t.Fatalf("%s: context_notice says the %s article was not read, and why: %v", query, state, reply["context_notice"])
				}
				if raw, _ := json.Marshal(reply); strings.Contains(string(raw), "no longer handed out") {
					t.Fatalf("%s: the reply carries the text of a %s article", query, state)
				}
			}
			value, err := runWork(b.Root, b.Scope, "prepare", workRequest{Query: "lesson:" + live.ID, Request: "Review the article."})
			if err != nil {
				t.Fatal(err)
			}
			liveReply := loopObject(t, value)
			if entries, _ := liveReply["knowledge"].([]any); len(entries) != 1 || liveReply["context_notice"] != nil {
				t.Fatalf("a live article is handed out as before: %v", liveReply)
			}
			for _, query := range goneQueries(t, b, gone) {
				value, err = runWork(b.Root, b.Scope, "prepare", workRequest{Query: query, Request: "Review the article."})
				if err != nil {
					t.Fatalf("%s: %v", query, err)
				}
				notRead(query, loopObject(t, value))
				value, err = runWork(b.Root, b.Scope, "prepare", workRequest{Receipt: liveReply["receipt"].(string), Query: query})
				if err != nil {
					t.Fatalf("%s: %v", query, err)
				}
				notRead(query, loopObject(t, value))
			}
		})
	}
}

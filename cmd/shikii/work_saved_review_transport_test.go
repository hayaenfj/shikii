package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkSavedReviewArticlesFitTransport(t *testing.T) {
	for _, transport := range []string{"cli", "mcp"} {
		t.Run(transport, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			t.Chdir(repo)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			observation, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic delivery evidence", Content: "Owner approval is required.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			source := observation.ID
			body := strings.Repeat("Retain the source and its conditions.\n", 400) + "Owner approval is required."
			pages := []notebook.Page{}
			for _, title := range []string{"Amber archive", "Cobalt archive"} {
				pages = append(pages, loopPage(t, b, source, map[string]any{"title": title, "body": title + "\n" + body, "kind": "howto"}))
			}
			prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + pages[0].ID, "request": "Append checked results to two articles."})
			if err != nil {
				t.Fatal(err)
			}
			refs := []string{prepared["knowledge"].([]any)[0].(map[string]any)["ref"].(string), interimArticleRef(t, b, prepared["receipt"], pages[1].ID)}
			notes := []any{}
			for _, ref := range refs {
				notes = append(notes, map[string]any{"target": ref, "sources": []string{"new1"}, "change_reason": "Append the checked result.", "edits": []any{map[string]any{"field": "body", "before": "Owner approval is required.", "after": "Owner approval is required. Restore was not executed."}}})
			}
			req := map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Validation report", "content": "Restore was not executed."}}, "notes": notes}
			var raw []byte
			var saved map[string]any
			var outputBytes int
			if transport == "mcp" {
				in, _ := json.Marshal(map[string]any{"operation": "record", "work": req})
				h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
				got, bytes := mcpToolCall(t, h, "shikii_wiki", string(in))
				outputBytes = bytes
				saved = got["structuredContent"].(map[string]any)
				if got["structuredContent"].(map[string]any)["complete"] != true {
					t.Fatal(got)
				}
				raw, _ = json.Marshal(got)
			} else {
				in, _ := json.Marshal(req)
				var out string
				var err error
				withStdin(t, string(in), func() { out, err = captureErr(func() error { return cmdWork([]string{"record"}) }) })
				if err != nil {
					t.Fatal(err)
				}
				raw = []byte(out)
				outputBytes = len(raw)
				if json.Unmarshal(raw, &saved) != nil || saved["complete"] != true {
					t.Fatal(string(raw))
				}
			}
			t.Logf("transport=%s bytes=%d article_body_bytes=%d", transport, outputBytes, len(body))
			limit := 28672
			if transport == "mcp" {
				limit = 65536
			}
			if outputBytes > limit {
				t.Errorf("record response exceeds transport budget: %d > %d", outputBytes, limit)
			}

			if saved["review_complete"] != false {
				t.Fatal("omitting delivery must not finish review", saved)
			}
			reviews, _ := saved["procedure_reviews"].([]any)
			if len(reviews) != 2 {
				t.Fatal("both saved article reviews must remain visible", saved)
			}
			omitted := 0
			maintenance := []any{}
			for _, rawReview := range reviews {
				review := rawReview.(map[string]any)
				var article map[string]any
				if review["article"] == nil {
					omitted++
					query, ok := review["article_query"].(map[string]any)
					exact := "lesson:" + review["page_id"].(string) + "@" + review["revision"].(string)
					if !ok || query["operation"] != "query" || query["query"] != exact || !strings.Contains(review["next_action"].(string), "omitted") {
						t.Fatal("omission must retain exact full recovery", review)
					}
					view, err := b.Query(query["query"].(string))
					if err != nil || len(view.Lessons) != 1 {
						t.Fatal(view, err)
					}
					article = loopObject(t, view.Lessons[0])
				} else {
					article = review["article"].(map[string]any)
				}
				want := article["title"].(string) + "\n" + body + " Restore was not executed."
				if article["body"] != want || article["id"] != review["page_id"] || article["revision"] != review["revision"] || len(article["sources"].([]any)) != 2 {
					t.Fatal("the complete saved article and sources must recover unchanged", article)
				}
				maintenance = append(maintenance, map[string]any{"ref": review["ref"], "reason": "Read the returned or recovered complete article and checked its retained approval and sources."})
			}
			if omitted == 0 {
				t.Error("the oversized saved articles require explicit recovery")
			}
			checked, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": maintenance})
			if err != nil || checked["complete"] != true || checked["review_complete"] != true {
				t.Fatal("reading and recording the retained references must finish review", checked, err)
			}
		})
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkExcerptReuse(t *testing.T) {
	t.Run("reuse excerpts without claiming a full read", func(t *testing.T) {
		b, page := readingLearningBook(t)
		p := readingLearningPrepare(t, b, "Recover the source condition.", "brief")
		first := loopObject(t, p["knowledge"].([]any)[0])
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
		if err != nil {
			t.Fatal(err)
		}
		e := loopObject(t, v["knowledge"].([]any)[0])
		if e["reused"] != true || e["reused_content"] != "excerpts" || e["body_included"] != false {
			t.Fatal("repeat delivery must reuse only the supplied excerpts", e["reused"], e["reused_content"])
		}
		if e["ref"] != first["ref"] || e["query"] == nil || e["orientation"] == nil || e["body_excerpts"] != nil || e["text"] != nil {
			t.Fatal("retain identity, conditions and recovery without duplicate excerpt text")
		}
		before, _ := json.Marshal(first)
		after, _ := json.Marshal(e)
		if len(after)+512 >= len(before) || loopObject(t, v["reading"])["mixed"] != true {
			t.Fatal("reuse must save bytes and stay outside reading-depth comparisons")
		}
		t.Logf("knowledge entry UTF-8 bytes: %d -> %d", len(before), len(after))
		full, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
		if err != nil {
			t.Fatal(err)
		}
		e = loopObject(t, full["knowledge"].([]any)[0])
		if e["body_included"] != true || loopObject(t, e["article"])["body"] != page.Body {
			t.Fatal("explicit recovery must return the entire exact revision")
		}
	})
	t.Run("explicit depth and fresh receipt deliver excerpts", func(t *testing.T) {
		b, _ := readingLearningBook(t)
		p := readingLearningPrepare(t, b, "Recover the source condition.", "brief")
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "brief"}})
		if err != nil {
			t.Fatal(err)
		}
		if loopObject(t, v["knowledge"].([]any)[0])["reused"] == true {
			t.Fatal("explicit reading intent must retain its requested material")
		}
		fresh := readingLearningPrepare(t, b, "Recover the source condition.", "brief")
		if loopObject(t, fresh["knowledge"].([]any)[0])["reused"] == true {
			t.Fatal("another receipt must not inherit excerpt delivery")
		}
	})
	t.Run("changed revision supplies new evidence", func(t *testing.T) {
		b, page := readingLearningBook(t)
		p := readingLearningPrepare(t, b, "Recover the source condition.", "brief")
		regressionChangeArticle(t, b, page, "updated")
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
		if err != nil {
			t.Fatal(err)
		}
		e := loopObject(t, v["knowledge"].([]any)[0])
		if e["reused"] == true || e["revision"] == page.Revision || e["body_excerpts"] == nil {
			t.Fatal("changed article text must remain readable")
		}
	})
	t.Run("a different query delivers newly selected passages", func(t *testing.T) {
		b, source := loopBook(t)
		body := ""
		for _, term := range []string{"almond", "birch", "cedar", "dahlia", "elm", "saffron"} {
			body += term + " " + strings.Repeat("This section retains a separate source condition. ", 3) + "\n\n"
		}
		loopPage(t, b, source, map[string]any{"title": "Reading source recovery", "body": body, "kind": "howto"})
		p := readingLearningPrepare(t, b, "Recover the source condition.", "brief")
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery saffron"})
		if err != nil {
			t.Fatal(err)
		}
		first := loopObject(t, p["knowledge"].([]any)[0])
		e := loopObject(t, v["knowledge"].([]any)[0])
		if workHash(first["orientation"]) == workHash(e["orientation"]) {
			t.Fatal("control: the second query must select different passages")
		}
		if e["reused"] == true || !strings.Contains(e["text"].(string), "saffron") {
			t.Fatal("same-revision delivery must retain newly selected evidence")
		}
	})
	t.Run("changed premise is delivered on every retry", func(t *testing.T) {
		b, page := readingLearningBook(t)
		path := filepath.Join(b.Scope, "context.txt")
		if err := os.WriteFile(path, []byte("CONTEXT_A"), 0600); err != nil {
			t.Fatal(err)
		}
		watch := []string{"context.txt"}
		if _, err := b.Ingest(notebook.Draft{ID: page.ID, Title: page.Title, Lesson: page.Lesson, Kind: page.Kind, Scene: page.Scene, AppliesTo: page.AppliesTo, Sources: []string{page.Sources[0].ID}, Watch: &watch}); err != nil {
			t.Fatal(err)
		}
		p := readingLearningPrepare(t, b, "Recover the source condition.", "brief")
		if err := os.WriteFile(path, []byte("CONTEXT_B"), 0600); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
			if err != nil {
				t.Fatal(err)
			}
			e := loopObject(t, v["knowledge"].([]any)[0])
			if e["reused"] == true || len(e["recheck"].([]any)) != 1 {
				t.Fatal("a live premise warning must remain visible")
			}
		}
	})
	t.Run("budget-omitted passages are not reused", func(t *testing.T) {
		b, _ := readingLearningBook(t)
		p := readingLearningPrepare(t, b, strings.Repeat("Check the supplied condition. ", 180), "brief")
		// A large task leaves room for the inventory but forces shorter passages.
		limited, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "brief"}, "max_bytes": 8192})
		if err != nil || limited["output"] == nil {
			t.Fatal("control: preparation must shorten its supplied context", err)
		}
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
		if err != nil {
			t.Fatal(err)
		}
		e := loopObject(t, v["knowledge"].([]any)[0])
		if e["reused"] == true || e["body_excerpts"] == nil {
			t.Fatal("budget-omitted passages must be supplied again")
		}
	})
}

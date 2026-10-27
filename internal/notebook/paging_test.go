package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestWikiSearchAndListingContinueBeyond128Articles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var last Page
	for i := range 140 {
		d := draft(source, fmt.Sprintf("Knowledge %03d", i))
		d.AppliesTo = strings.Repeat("Knowledge scope ", 55)
		body := strings.Repeat("Detailed evidence. ", 100)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatalf("does not refuse page %d by count alone: %v", i+1, err)
		}
		last = *r.Page
	}
	for _, query := range []string{"Knowledge", ""} {
		seen, cursor := map[string]bool{}, ""
		var firstCursor string
		for calls := 0; ; calls++ {
			if calls > 140 {
				t.Fatal("the cursor does not advance")
			}
			v, err := b.QueryPage(query, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if query == "" {
				for _, p := range v.Lessons {
					if seen[p.ID] {
						t.Fatal("duplicate in the list")
					}
					seen[p.ID] = true
				}
			} else {
				if v.Search == nil || v.Search.Total != 140 {
					t.Fatal("searches every page")
				}
				for _, c := range v.Search.Candidates {
					if seen[c.ID] {
						t.Fatal("duplicate candidate")
					}
					seen[c.ID] = true
				}
			}
			if calls == 0 {
				firstCursor = v.NextCursor
			}
			if v.NextCursor == "" {
				break
			}
			cursor = v.NextCursor
		}
		if len(seen) != 140 || firstCursor == "" {
			t.Fatalf("reaches the end without truncating: %d cursor=%q", len(seen), firstCursor)
		}
		if _, err := b.QueryPage(query+" changed", firstCursor); err == nil {
			t.Fatal("refuses a cursor whose search terms changed")
		}
		if _, err := (Book{Root: b.Root, Scope: "/another"}).QueryPage(query, firstCursor); err == nil {
			t.Fatal("refuses a cursor from another project")
		}
	}
	v, err := b.Query("lesson:" + last.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != last.ID {
		t.Fatal("the last page can also be read directly", err)
	}
	before, _ := b.QueryPage("Knowledge", "")
	d := draft(source, last.Title)
	d.ID, d.Lesson = last.ID, "Revised knowledge"
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if _, err := b.QueryPage("Knowledge", before.NextCursor); err == nil {
		t.Fatal("refuses a cursor after the page revision changed")
	}
	if _, err := b.Lint(); err != nil {
		t.Fatal("a large list can also be checked", err)
	}
}

func TestWikiSourceContinuationUsesBytesInsteadOfCounts(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := range 18 {
		o, err := b.Capture(Observation{Kind: "test", Title: fmt.Sprint("Evidence ", i), Content: strings.Repeat("x", 9000), Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	seen, cursor := map[string]bool{}, ""
	var first string
	for calls := 0; ; calls++ {
		if calls > len(ids) {
			t.Fatal("the source cursor does not advance")
		}
		got, err := b.ReadSourcesPage(ids, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range got["observations"].([]Observation) {
			if seen[o.ID] || len(o.Content) != 9000 {
				t.Fatal("missing or duplicate original text")
			}
			seen[o.ID] = true
		}
		cursor, _ = got["next_cursor"].(string)
		if calls == 0 {
			first = cursor
		}
		if cursor == "" {
			break
		}
	}
	if len(seen) != 18 || first == "" {
		t.Fatal("does not cut source records by count", len(seen))
	}
	if _, err := b.ReadSourcesPage(ids[:17], first); err == nil {
		t.Fatal("refuses a cursor whose target changed")
	}
	if _, err := b.ReadSourcesPage(ids, "invalid"); err == nil {
		t.Fatal("refuses a broken cursor")
	}
	if _, err := (Book{Root: b.Root, Scope: "/other"}).ReadSourcesPage(ids, first); err == nil {
		t.Fatal("keeps the project boundary of source records")
	}
}

func TestWikiCitationsLinksAndCaptureQueueHaveNoCountCeiling(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := range 140 {
		o, err := b.Capture(Observation{Kind: "test", Title: fmt.Sprint("Evidence ", i), Content: "Checked", Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatalf("does not refuse ingestion with %d unorganized records: %v", i+1, err)
		}
		ids = append(ids, o.ID)
	}
	d := draft(ids[0], "Combined knowledge")
	d.Sources = ids[:18]
	r, err := b.Ingest(d)
	if err != nil || len(r.Page.Sources) != 18 {
		t.Fatal("can cite 18 source records", err)
	}
	d.ID = r.Page.ID
	d.Sources = ids[18:36]
	r, err = b.Ingest(d)
	if err != nil || len(r.Page.Sources) != 36 {
		t.Fatal("keeps the quotes from before the update", err)
	}
	links := []string{}
	for i := range 12 {
		r, err := b.Ingest(draft(ids[i], fmt.Sprint("Topic ", i)))
		if err != nil {
			t.Fatal(err)
		}
		links = append(links, r.Page.ID)
	}
	d.Related = links
	d.Replaces = links[:5]
	if _, err := b.Ingest(d); err != nil {
		t.Fatal("keeps 12 related and 5 replaced pages", err)
	}
	dir, _ := b.directory(false, "pages")
	checkpoints := map[string]maintenanceCheckpoint{}
	for i := range 150 {
		checkpoints[fmt.Sprintf("%016x", i)] = maintenanceCheckpoint{MaintenanceReview: MaintenanceReview{Reason: strings.Repeat("Checked ", 100)}}
	}
	data, _ := json.Marshal(checkpoints)
	if err := writeFile(dir+"/.maintenance.json", data); err != nil {
		t.Fatal(err)
	}
	if got, err := readCheckpoints(dir); err != nil || len(got) != 150 {
		t.Fatal("does not lose the review log to counts or small file limits", err)
	}
}

// audit 0.9.0 M4: the guidance about pages without kind and scene is only in the first reply, not in continuation replies (do not hand over the same 5 pages again and again).
func TestUnclassifiedOnlyOnTheFirstPage(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var legacy string
	for i := range 60 {
		d := draft(source, fmt.Sprintf("Knowledge %03d", i))
		d.AppliesTo = strings.Repeat("Knowledge scope ", 55)
		body := strings.Repeat("Detailed evidence. ", 100)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		legacy = r.Page.ID
	}
	if err := b.RewriteWithoutKindScene(legacy); err != nil {
		t.Fatal(err)
	}
	first, err := b.QueryPage("", "")
	if err != nil || first.NextCursor == "" || first.UnclassifiedCount != 1 {
		t.Fatalf("the first reply has the guidance and a continuation: count=%d cursor=%q %v", first.UnclassifiedCount, first.NextCursor, err)
	}
	next, err := b.QueryPage("", first.NextCursor)
	if err != nil || next.UnclassifiedCount != 0 || len(next.Unclassified) != 0 || next.UnclassifiedHow != "" {
		t.Fatalf("not included in continuation replies: %+v %v", next.Remaining, err)
	}
}

// 0.9.5: the cursor's snapshot covered the whole reply, including the conversations waiting for reflection, which grow by one each turn of any
// agent. Reading the rest of the knowledge list while an agent worked failed with "the wiki cursor does not match ..." although no page changed.
// The snapshot now covers only what is split into parts (the pages, or the search candidates); a changed page still refuses the cursor.
func TestCursorSurvivesANewConversation(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var last Page
	for i := range 80 {
		d := draft(source, fmt.Sprintf("Knowledge %03d", i))
		d.AppliesTo = strings.Repeat("Knowledge scope ", 60)
		body := strings.Repeat("Detailed evidence. ", 200)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		last = *r.Page
	}
	for _, query := range []string{"", "Knowledge"} {
		first, err := b.QueryPage(query, "")
		if err != nil || first.NextCursor == "" {
			t.Fatalf("the list comes in parts: %v %q", err, first.NextCursor)
		}
		seed(t, b, 100) // an agent's turn records a conversation while the page reads on
		if _, err := b.QueryPage(query, first.NextCursor); err != nil {
			t.Fatalf("query %q: a new conversation does not invalidate the cursor: %v", query, err)
		}
	}
	first, _ := b.QueryPage("", "")
	d := draft(source, last.Title)
	d.ID, d.Lesson = last.ID, "Revised knowledge"
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if _, err := b.QueryPage("", first.NextCursor); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("a changed page still refuses the cursor, with ErrStaleCursor: %v", err)
	}
}

// readAll reads every reply and joins each observation's parts by id.
func partsReadAll(t *testing.T, b Book, ids []string) (map[string]string, []map[string]any) {
	t.Helper()
	joined, replies, cursor := map[string]string{}, []map[string]any{}, ""
	for n := 0; ; n++ {
		if n > 400 {
			t.Fatal("reading parts did not end (the same cursor came back)")
		}
		out, err := b.ReadSourcesPage(ids, cursor)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(out)
		if len(data) > replyBudget {
			t.Fatalf("a reply went over the budget: %d bytes", len(data))
		}
		var wire struct{ Observations []Observation }
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		for _, o := range wire.Observations { // join what the agent receives (JSON), not the Go strings
			joined[o.ID] += o.Content
		}
		replies = append(replies, out)
		next, _ := out["next_cursor"].(string)
		if next == "" {
			return joined, replies
		}
		if next == cursor {
			t.Fatal("the same cursor came back")
		}
		cursor = next
	}
}

// Several ids where the long one is in the middle; bodies that expand 6x when escaped.
func TestLongSourceAmongOthers(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	short1 := partsCapture(t, b, "short1", "a short note", 1)
	long := partsCapture(t, b, "long", strings.Repeat("\x01\"<>& ", 40000)+"END", 2)
	quotes := partsCapture(t, b, "quotes", strings.Repeat("\x02", 300000), 3)
	short2 := partsCapture(t, b, "short2", "another short note", 4)
	want := map[string]string{short1.ID: short1.Content, long.ID: long.Content, quotes.ID: quotes.Content, short2.ID: short2.Content}
	joined, _ := partsReadAll(t, b, []string{short1.ID, long.ID, quotes.ID, short2.ID})
	for id, body := range want {
		if joined[id] != body {
			t.Fatalf("source %s: joined %d bytes, want %d", id, len(joined[id]), len(body))
		}
	}
}

// The last part of a long source is a tail too; it must say which part it is, or a reader takes the tail for the whole body.
func TestLastPartIsMarked(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	long := partsCapture(t, b, "long", strings.Repeat("A long memory file. ", 8000), 1)
	_, replies := partsReadAll(t, b, []string{long.ID})
	if len(replies) < 2 {
		t.Fatal("expected parts")
	}
	last := replies[len(replies)-1]
	part, ok := last["observation_part"].(map[string]any)
	if !ok {
		t.Fatalf("the last part (a tail of %d bytes) has no observation_part", len(last["observations"].([]Observation)[0].Content))
	}
	if part["to"] != part["bytes"] || part["from"] == 0 {
		t.Fatalf("the last part must cover the tail: %+v", part)
	}
}

// A cursor whose byte is in the middle of a character, or at or past the end of the body, is refused.
// (A byte at a character boundary inside an observation is accepted; the reply says which part it is.)
func TestForgedByteIsRefused(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	long := partsCapture(t, b, "long", strings.Repeat("\u9577\u3044\u30e1\u30e2\u30ea\u3002", 20000), 1)
	short := partsCapture(t, b, "short", strings.Repeat("\u77ed\u3044\u3002", 10), 2)
	ids := []string{long.ID, short.ID}
	first, err := b.ReadSourcesPage(ids, "")
	if err != nil {
		t.Fatal(err)
	}
	cursor := first["next_cursor"].(string)
	for name, edit := range map[string]func(*continuation){
		"middle of a character": func(c *continuation) { c.Byte++ },
		"past the end":          func(c *continuation) { c.Byte = len(long.Content) },
	} {
		if out, err := b.ReadSourcesPage(ids, auditForge(t, cursor, edit)); err == nil {
			got := ""
			if obs := out["observations"].([]Observation); len(obs) > 0 {
				got = obs[0].Content
				if len(got) > 12 {
					got = got[:12]
				}
			}
			t.Errorf("%s: a forged cursor was accepted (starts %q)", name, got)
		}
	}
}

// archiveConversation writes a conversation source and its long originals (.text.json, up to 2 MB) the way ingest archives them.
func archiveConversation(t *testing.T, b Book, id string, originals []Original) {
	t.Helper()
	dir, err := b.directory(true, "sources")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := json.Marshal(store.Feedback{ID: id, Request: "translate the guide " + id, Summary: "translated", Reply: "read the whole text"})
	text, _ := json.Marshal(originals)
	if err := os.WriteFile(filepath.Join(dir, id+".json"), f, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".text.json"), text, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readAllOriginals reads every reply, checks each stays within replyBudget, and joins the originals by id
// (whole ones from originals, long ones from the original_part pieces in order).
func readAllOriginals(t *testing.T, b Book, ids []string) (map[string][]Original, map[string]int, int) {
	t.Helper()
	whole, pieces, conversations := map[string][]Original{}, map[string]string{}, map[string]int{}
	cursor, replies := "", 0
	for ; ; replies++ {
		if replies > 400 {
			t.Fatal("reading parts did not end")
		}
		out, err := b.ReadSourcesPage(ids, cursor)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(out)
		if len(data) > replyBudget {
			t.Fatalf("a reply went over the budget: %d KiB (limit %d KiB)", len(data)>>10, replyBudget>>10)
		}
		var wire struct {
			Material  []store.Feedback      `json:"source_material"`
			Originals map[string][]Original `json:"originals"`
			Part      *struct {
				ID       string `json:"id"`
				From, To int
				Bytes    int    `json:"bytes"`
				JSON     string `json:"json"`
			} `json:"original_part"`
			Next string `json:"next_cursor"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		for _, f := range wire.Material {
			conversations[f.ID]++
		}
		for id, o := range wire.Originals {
			whole[id] = o
		}
		if wire.Part != nil {
			if wire.Part.From != len(pieces[wire.Part.ID]) || wire.Part.To != wire.Part.From+len(wire.Part.JSON) {
				t.Fatalf("original_part says which part it is: from %d to %d, joined so far %d", wire.Part.From, wire.Part.To, len(pieces[wire.Part.ID]))
			}
			pieces[wire.Part.ID] += wire.Part.JSON
			if wire.Part.To == wire.Part.Bytes {
				var o []Original
				if err := json.Unmarshal([]byte(pieces[wire.Part.ID]), &o); err != nil {
					t.Fatalf("the joined parts are the originals' JSON: %v", err)
				}
				whole[wire.Part.ID] = o
			}
		}
		if wire.Next == "" {
			return whole, conversations, replies + 1
		}
		if wire.Next == cursor {
			t.Fatal("the same cursor came back")
		}
		cursor = wire.Next
	}
}

// Audit 0.9.2 (later audits): the originals of a conversation (up to 2 MB) were returned whole in one reply, far over replyBudget.
// Like a long observation, they now come in parts (original_part) that each fit, and the parts join back into the originals.
func TestLongOriginalsComeInParts(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	long := []Original{{TraceID: "00000000000000a1", Inputs: map[string]string{"request": strings.Repeat("caf\xc3\xa9 \xe2\x82\xac\"\x01 ", 1<<20/12) + "END"}}}
	short := []Original{{TraceID: "00000000000000a2", Inputs: map[string]string{"request": "a short original"}}}
	archiveConversation(t, b, "0000000000000001", short)
	archiveConversation(t, b, "0000000000000002", long)
	archiveConversation(t, b, "0000000000000003", short)
	ids := []string{"0000000000000001", "0000000000000002", "0000000000000003"}
	got, conversations, replies := readAllOriginals(t, b, ids)
	if replies < 3 {
		t.Fatalf("a 1 MB original needs several replies, got %d", replies)
	}
	want := map[string][]Original{ids[0]: short, ids[1]: long, ids[2]: short}
	for id, o := range want {
		if !reflect.DeepEqual(got[id], o) {
			t.Fatalf("source %s: the originals join back whole", id)
		}
		if conversations[id] != 1 {
			t.Fatalf("source %s: the conversation itself is returned once, not with every part (%d)", id, conversations[id])
		}
	}
}

// A cursor inside the originals must point at a character boundary inside them.
func TestForgedOriginalCursorIsRefused(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	long := []Original{{TraceID: "00000000000000a1", Inputs: map[string]string{"request": strings.Repeat("\xe2\x82\xac\xe2\x82\xac\xe2\x82\xac\xe2\x82\xac", 1<<20/12)}}}
	archiveConversation(t, b, "0000000000000002", long)
	ids := []string{"0000000000000002"}
	first, err := b.ReadSourcesPage(ids, "")
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("expected parts")
	}
	text, _ := json.Marshal(long)
	for name, edit := range map[string]func(*continuation){
		"middle of a character": func(c *continuation) { c.Byte++ },
		"past the end":          func(c *continuation) { c.Byte = len(text) },
	} {
		if _, err := b.ReadSourcesPage(ids, auditForge(t, cursor, edit)); err == nil {
			t.Errorf("%s: a forged cursor was accepted", name)
		}
	}
}

// 0.9.2 re-audit 2: the listing says "read the whole body with wiki query <source id>". One long source is returned in parts
// that each stay within replyBudget, with next_cursor and observation_part, and the parts join back into the body.
func TestLongSourceIsReadInParts(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	body := strings.Repeat("\u9577\u3044\u30e1\u30e2\u30ea\u306e\u672c\u6587\u3002A long memory file. ", 9000) + "END" // multi-byte, about 430 KB
	o, err := b.Capture(Observation{Kind: "agent", Title: "long memory", Content: body, Reference: "memory/long.md", Version: "v", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	got, cursor, n := "", "", 0
	for {
		out, err := b.ReadSourcesPage([]string{o.ID}, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if data, _ := json.Marshal(out); len(data) > replyBudget {
			t.Fatalf("a reply went over the budget: %d KiB (limit %d KiB), next_cursor %q", len(data)>>10, replyBudget>>10, out["next_cursor"])
		}
		got += out["observations"].([]Observation)[0].Content
		n++
		next, _ := out["next_cursor"].(string)
		if next == "" {
			break
		}
		if out["observation_part"] == nil {
			t.Fatal("a part of a long source says which part it is (observation_part)")
		}
		cursor = next
	}
	if got != body || n < 2 {
		t.Fatalf("the joined parts must be the body: %d / %d bytes (%d replies)", len(got), len(body), n)
	}
}

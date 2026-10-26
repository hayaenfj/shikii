package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

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

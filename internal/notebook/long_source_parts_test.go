package notebook

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func partsCapture(t *testing.T, b Book, title, body string, at int64) Observation {
	t.Helper()
	o, err := b.Capture(Observation{Kind: "agent", Title: title, Content: body, Reference: "memory/" + title + ".md", Version: "v", ObservedAt: time.Unix(at, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return o
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

func auditForge(t *testing.T, cursor string, edit func(*continuation)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var c continuation
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	edit(&c)
	data, _ = json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// A cursor whose byte is in the middle of a character, or points at a source that is not cut, is refused.
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

// Other readers refuse a cursor that carries a byte.
func TestQueryRefusesByteCursor(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i := 0; i < 120; i++ {
		o := partsCapture(t, b, fmt.Sprintf("m%d", i), strings.Repeat("x", 10), int64(i+1))
		d := draft(o.ID, fmt.Sprintf("Lesson %d about memories", i))
		d.Lesson = strings.Repeat(fmt.Sprintf("lesson %d text ", i), 40)
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	v, err := b.QueryPage("", "")
	if err != nil || v.NextCursor == "" {
		t.Fatalf("expected a second page: %v", err)
	}
	if _, err := b.QueryPage("", auditForge(t, v.NextCursor, func(c *continuation) { c.Byte = 5 })); err == nil {
		t.Fatal("QueryPage accepted a cursor with a byte offset")
	}
}

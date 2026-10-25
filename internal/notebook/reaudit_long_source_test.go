package notebook

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 0.9.2 re-audit 1: the hook before each request (Recall) checks the hash of every source a page cites. Sources can now be up to 4 MiB,
// so the hash is taken while streaming the file instead of holding it in memory (the hash value stays the same).
func TestRecallDoesNotHoldCitedSourcesInMemory(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	const pages, size = 20, 3 << 20
	for i := 0; i < pages; i++ {
		o, err := b.Capture(Observation{Kind: "agent", Title: fmt.Sprintf("memory %d", i), Content: strings.Repeat(fmt.Sprintf("line %d of a long memory file. ", i), size/32), Reference: "memory.md", Version: "v", ObservedAt: time.Unix(int64(i+1), 0)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Ingest(draft(o.ID, fmt.Sprintf("Lesson number %d about memories", i))); err != nil {
			t.Fatal(err)
		}
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := b.Recall("README installation instructions")
	if err != nil || len(got) == 0 {
		t.Fatal(err, len(got))
	}
	runtime.ReadMemStats(&after)
	if alloc := (after.TotalAlloc - before.TotalAlloc) >> 20; alloc > 16 {
		t.Fatalf("Recall read every cited source whole: allocated %d MiB (%d pages x %d MiB sources)", alloc, pages, size>>20)
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

// 0.9.2 re-audit, test hole: a page made from a long source stays live (its source can be read by ingest, query and lint),
// and capturing the same long source again is not refused.
func TestLessonFromLongSourceStaysLive(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	long := Observation{Kind: "agent", Title: "long memory", Content: strings.Repeat("A long memory file, kept verbatim. ", 6000), Reference: "memory/long.md", Version: "v", ObservedAt: time.Unix(1, 0)}
	o, err := b.Capture(long)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(draft(o.ID, "Lesson from a long memory")); err != nil {
		t.Fatalf("a page can be made from a long source: %v", err)
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 1 || len(v.Issues) != 0 {
		t.Fatalf("a page citing a long source is listed without issues: %v lessons=%d issues=%+v", err, len(v.Lessons), v.Issues)
	}
	if again, err := b.Capture(long); err != nil || again.ID != o.ID {
		t.Fatalf("capturing a sorted long source again is not refused: %v", err)
	}
}

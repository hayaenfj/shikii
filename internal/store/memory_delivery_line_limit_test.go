package store

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A delivery the store accepts must be one the store reads back. The reader skips a line longer than its limit
// without a word, and the fields a caller passes through (the door phrase, the subagent's id, the parent turn)
// have no length limit of their own, so a long one made a record that was written and never read: the delivery
// was missing from the report, and every resend appended it again.
func TestSaveMemoryDeliveryRefusesALineItsReaderWouldSkip(t *testing.T) {
	for _, c := range []struct {
		name string
		grow func(*MemoryDelivery, string)
	}{
		{"phrase", func(d *MemoryDelivery, long string) { d.Phrase = long }},
		{"agent", func(d *MemoryDelivery, long string) { d.Agent = long }},
		{"parent", func(d *MemoryDelivery, long string) { d.Parent = long }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, d, _ := memoryStoreFixture(t)
			c.grow(&d, strings.Repeat("x", 40<<10))
			first := s.SaveMemoryDelivery(d)
			second := s.SaveMemoryDelivery(d) // a resend of the same delivery
			got, err := s.MemoryDeliveries(d.Scope)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(s.memoryPath(d.Scope))
			lines := strings.Count(string(data), "\n")
			if first == nil && len(got) == 0 {
				t.Fatalf("the save succeeded but the delivery cannot be read back (%d lines, %d bytes on disk; resend error: %v)", lines, len(data), second)
			}
			if first == nil {
				t.Fatalf("a delivery with a %d-byte %s was saved", 40<<10, c.name)
			}
			if lines != 0 {
				t.Fatalf("a refused delivery left %d lines on disk", lines)
			}
			if !strings.Contains(first.Error(), "too long") {
				t.Fatalf("the error must say the delivery is too long: %v", first)
			}
		})
	}
}

// The limit is the reader's: a line a little under it is saved and read back, a line a little over it is refused.
// (The margin of 64 bytes covers the time of saving, whose length is not fixed.)
func TestSaveMemoryDeliveryLimitIsTheReadersLimit(t *testing.T) {
	sized := func(d MemoryDelivery, total int) MemoryDelivery {
		d.Agent = "a"
		line, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		d.Agent = strings.Repeat("a", 1+total-len(line))
		return d
	}
	s, d, _ := memoryStoreFixture(t)
	under := sized(d, 32<<10-64)
	if err := s.SaveMemoryDelivery(under); err != nil {
		t.Fatalf("a line under the reader's limit must be saved: %v", err)
	}
	if got, err := s.MemoryDeliveries(d.Scope); err != nil || len(got) != 1 || got[0].Agent != under.Agent {
		t.Fatalf("a line under the reader's limit must be read back: %d %v", len(got), err)
	}
	s2, d2, _ := memoryStoreFixture(t)
	if err := s2.SaveMemoryDelivery(sized(d2, 32<<10+64)); err == nil {
		got, _ := s2.MemoryDeliveries(d2.Scope)
		t.Fatalf("a line over the reader's limit was saved (read back: %d)", len(got))
	}
}

// Guard: a delivery of ordinary size is saved once and read back, also with the longest values its callers
// pass today (a 512-byte tool label, a 1500-byte request, a file name as the door phrase).
func TestSaveMemoryDeliveryKeepsOrdinaryDeliveries(t *testing.T) {
	s, d, _ := memoryStoreFixture(t)
	d.Request = strings.Repeat("r", feedbackTextMax)
	d.Parent, d.Agent = "0000000000000009", strings.Repeat("a", 200)
	d.Tool, d.Phrase = strings.Repeat("t", toolMax), strings.Repeat("p", 255)
	for range 2 {
		if err := s.SaveMemoryDelivery(d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.MemoryDeliveries(d.Scope)
	if err != nil || len(got) != 1 || got[0].Phrase != d.Phrase || got[0].Agent != d.Agent {
		t.Fatalf("an ordinary delivery must be read back once: %d %v", len(got), err)
	}
	data, _ := os.ReadFile(s.memoryPath(d.Scope))
	if strings.Count(string(data), "\n") != 1 {
		t.Fatalf("a resend must not append the delivery again")
	}
}

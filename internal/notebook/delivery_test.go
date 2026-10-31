package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryJournalBoundsEncodedBytesAndRefusesLinks(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	// HTML/control characters expand in JSON; ordinary ASCII misses this bound.
	entries := make([]DeliveryEntry, 512)
	for i := range entries {
		entries[i] = DeliveryEntry{ID: "0000000000000001", Revision: "0000000000000002", Retrieval: "candidate", Reason: "delivered", Discovery: strings.Repeat("<\x01", 2000)}
	}
	c := DeliveryCase{Mode: "request", Request: "Synthetic context", Complete: true, Entries: entries}
	if err := b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	cases, err := b.DeliveryCases()
	if err != nil || len(cases) != 1 || cases[0].Complete {
		t.Fatal("oversized inventory must be explicit", len(cases), err)
	}
	if _, err := b.HistoricalPages(cases[0]); err == nil {
		t.Fatal("a partial inventory cannot be compared")
	}
	dir, err := b.directory(false, "deliveries")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".deliveries.json"))
	if err != nil || len(raw) > deliveryJournalLimit {
		t.Fatal("encoded journal exceeded its read budget", len(raw), err)
	}
	var single []json.RawMessage
	if json.Unmarshal(raw, &single) != nil || len(single[0]) > 512<<10 {
		t.Fatal("encoded case exceeded its budget")
	}
	// A bounded window is retention, not a probabilistic or concurrent test.
	for i := 0; i < 33; i++ {
		if err := b.SaveDeliveryCase(DeliveryCase{Mode: "request", Request: "Small context", Complete: true}); err != nil {
			t.Fatal(err)
		}
	}
	cases, err = b.DeliveryCases()
	if err != nil || len(cases) != 32 {
		t.Fatal("case retention is unbounded", len(cases), err)
	}
	path := filepath.Join(dir, ".deliveries.json")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DeliveryCases(); err == nil {
		t.Fatal("diagnostic reader followed a link")
	}
	if err := b.SaveDeliveryCase(c); err == nil {
		t.Fatal("diagnostic writer followed a link")
	}
	kept, _ := os.ReadFile(outside)
	if string(kept) != "untouched" {
		t.Fatal("outside file changed")
	}
}

package notebook

import (
	"os"
	"path/filepath"
	"testing"
)

// os.Root.OpenFile follows a link in the last name even with O_NOFOLLOW when the link stays inside the root, so the journal
// replaced by a link to another file in the same folder used to be read and written through it.
func TestDeliveryJournalRefusesALinkToAFileInTheSameFolder(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	c := DeliveryCase{Mode: "request", Request: "Small context", Complete: true}
	if err := b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	dir, err := b.directory(false, "deliveries")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".deliveries.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beside.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("beside.json", path); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DeliveryCases(); err == nil {
		t.Fatal("the delivery journal was read through a link to a file in the same folder")
	}
	// The lock beside the journal: a link there must not be taken as the lock.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".lock")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink("beside.json", filepath.Join(dir, ".lock")); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveDeliveryCase(c); err == nil {
		t.Fatal("the delivery lock was taken through a link to a file in the same folder")
	}
}

package notebook

import (
	"strings"
	"testing"
)

// Behaviour check 0.9.2 re-check (claude-en): a wiki-only sort named two conversations and, by mistake, the turn id
// 911c2872465d2337. The whole item was refused with "wiki source does not belong to this project's observations",
// which does not say which id, so the agent never resent the two good ids and both conversations stayed "wiki unsorted".
func TestWikiSourceErrorNamesTheUnknownID(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	good := seed(t, b, 1)
	bad := "911c2872465d2337"
	_, err := b.Ingest(Draft{Sources: []string{good, bad}})
	if err == nil {
		t.Fatal("an unknown source id was accepted")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Fatalf("the error does not name the unknown id %s: %v", bad, err)
	}
}

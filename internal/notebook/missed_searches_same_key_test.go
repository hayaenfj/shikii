package notebook

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// A journal written by hand holds what an earlier version left: 0.38.0 kept the same words typed in another case as two misses.
func writeJournalRows(t *testing.T, b Book, rows ...string) string {
	t.Helper()
	path := journalPath(t, b)
	file := `{ "lookups": [], "misses": [ ` + strings.Join(rows, ", ") + ` ] }`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func missRow(query, at, result, session string) string {
	row := `{"query":"` + query + `","at":"` + at + `"`
	if result != "" {
		row += `,"result":"` + result + `"`
	}
	if session != "" {
		row += `,"session":"` + session + `"`
	}
	return row + `}`
}

// A search that finds nothing again takes its result from the newest miss of the same words that has one, not from the newest miss
// of them: a journal of 0.38.0 may hold the words twice (typed in another case), and the result may be on the older of the two. The
// miss that is returned is that one, and the journal keeps one miss, tied to the result.
func TestRecordMissTakesTheResultFromTheNewestMissOfTheSameWordsThatHasOne(t *testing.T) {
	const (
		t0 = "2026-11-01T09:00:00Z"
		t1 = "2026-11-01T09:01:00Z"
		t2 = "2026-11-01T09:02:00Z"
	)
	for _, c := range []struct {
		name       string
		rows       []string
		result, at string // the result that is kept, and the time of the miss it was taken from
	}{
		{"the older one has the result", []string{missRow("Saffron Lanterns", t0, resultA, ""), missRow("saffron lanterns", t1, "", "")}, resultA, t0},
		{"the newer one has the result", []string{missRow("Saffron Lanterns", t0, "", ""), missRow("saffron lanterns", t1, resultA, "")}, resultA, t1},
		{"both have one: the newer", []string{missRow("Saffron Lanterns", t0, resultA, ""), missRow("saffron lanterns", t1, resultB, "")}, resultB, t1},
		{"three of them: the newest that has one", []string{missRow("Saffron Lanterns", t0, resultA, ""), missRow("saffron lanterns", t1, resultB, ""), missRow("SAFFRON lanterns", t2, "", "")}, resultB, t1},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
			writeJournalRows(t, b, c.rows...)
			again := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
			earlier, err := b.recordMissAt("saffron lanterns", "", "", emptyView(), again)
			if err != nil {
				t.Fatal(err)
			}
			if earlier == nil || earlier.Result != c.result || earlier.At.Format(time.RFC3339) != c.at {
				t.Fatalf("the earlier miss is the newest one that has a result: %+v, want the result %s of the miss at %s", earlier, c.result, c.at)
			}
			if got := missesOnDisk(t, b); len(got) != 1 || got[0].Result != c.result || !got[0].At.Equal(again) {
				t.Fatalf("one miss with the new time, tied to the result: %+v", got)
			}
		})
	}
}

// The listing of open misses names the same words once: the newest miss of them, with the result of the newest miss of the same
// words that has one. The older miss is skipped, so it does not use up one of the misses a call examines, and the file of the journal
// is not rewritten.
func TestOpenMissesNameTheSameWordsOnce(t *testing.T) {
	b := missFixture(t)
	file := writeJournalRows(t, b,
		missRow("Saffron Lanterns", "2026-11-01T09:00:00Z", resultA, ""),
		missRow("kestrel harbor", "2026-11-01T09:01:00Z", "", ""),
		missRow("saffron lanterns", "2026-11-01T09:02:00Z", "", ""),
	)
	open, err := b.OpenMisses(time.Time{}, 8, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 || open[0].Query != "saffron lanterns" || open[0].Result != resultA || open[1].Query != "kestrel harbor" {
		t.Fatalf("the same words once, newest first, with the result of the one that has it: %+v", open)
	}
	if after, err := os.ReadFile(journalPath(t, b)); err != nil || string(after) != file {
		t.Fatalf("listing wrote the journal: %s (%v)", after, err)
	}
	// the miss of another session that is skipped does not take the words from the one that is listed
	writeJournalRows(t, b,
		missRow("saffron lanterns", "2026-11-01T09:00:00Z", resultA, missSessionOne),
		missRow("Saffron Lanterns", "2026-11-01T09:01:00Z", "", missSessionTwo),
	)
	if open, err := b.OpenMisses(time.Time{}, 8, missSessionOne); err != nil || len(open) != 1 || open[0].Session != missSessionOne || open[0].Result != resultA {
		t.Fatalf("the session of the older miss is listed its own: %+v %v", open, err)
	}
	if open, err := b.OpenMisses(time.Time{}, 8, missSessionTwo); err != nil || len(open) != 1 || open[0].Session != missSessionTwo || open[0].Result != resultA {
		t.Fatalf("the session of the newer miss is listed its own, with the result of the words: %+v %v", open, err)
	}
}

// A miss of words that another miss of the same words repeats is skipped and not counted among the misses a call examines: one
// that is listed does not push the sixteenth distinct words out.
func TestOpenMissesDoNotCountTheMissThatRepeatsTheWordsOfAnotherAmongThoseTheyExamine(t *testing.T) {
	b := missFixture(t)
	rows := []string{}
	for i := range openMissExamineMax {
		rows = append(rows, missRow(fmt.Sprintf("words %02d", i), fmt.Sprintf("2026-11-01T09:%02d:00Z", i), "", ""))
	}
	// the newest of them typed again in another case, as the newest row of the journal
	rows = append(rows, missRow("WORDS 15", "2026-11-01T09:30:00Z", "", ""))
	writeJournalRows(t, b, rows...)
	open, err := b.OpenMisses(time.Time{}, 50, "")
	if err != nil || len(open) != openMissExamineMax || open[len(open)-1].Query != "words 00" {
		t.Fatalf("the sixteen distinct words are all listed: %d %+v %v", len(open), open, err)
	}
}

// The miss of words typed in lower case is closed by the same words typed in upper case, as the key of the miss and the key of the
// search are the same (case, spacing and composition do not make another search): CloseMiss.
func TestCloseMissClosesTheMissOfTheSameWordsTypedInUpperCase(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	if _, err := b.recordMissAt("saffron lanterns", "", "", emptyView(), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := b.CloseMiss("SAFFRON  Lanterns"); err != nil {
		t.Fatal(err)
	}
	if got := missesOnDisk(t, b); len(got) != 0 {
		t.Fatalf("the same words in upper case close the miss: %+v", got)
	}
}

// The lookup that returns an article closes the miss of the same words typed in another case, in both directions.
func TestALookupClosesTheMissOfLowerCaseWordsWhenTheyAreTypedInUpperCase(t *testing.T) {
	b := missFixture(t)
	if _, err := b.recordMissAt("quartz exports", "", "", emptyView(), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	view, err := b.QueryPage("QUARTZ  EXPORTS", "")
	if err != nil || len(view.Lessons) == 0 {
		t.Fatalf("the fixture changed: %+v %v", view.Search, err)
	}
	if _, err := b.RecordLookup("QUARTZ  EXPORTS", view.Lessons); err != nil {
		t.Fatal(err)
	}
	if got := missesOnDisk(t, b); len(got) != 0 {
		t.Fatalf("the lookup of the same words in upper case closes the miss: %+v", got)
	}
}

package notebook

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"
	"time"
)

const (
	missSessionOne = "1111111111111111"
	missSessionTwo = "2222222222222222"
)

// sessionsAndResultsOnDisk reads, as plain JSON, the session and the result of each miss in the journal file.
func sessionsAndResultsOnDisk(t *testing.T, b Book) (sessions, results map[string]string) {
	t.Helper()
	data, err := os.ReadFile(journalPath(t, b))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Misses []struct{ Query, Session, Result string } `json:"misses"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	sessions, results = map[string]string{}, map[string]string{}
	for _, m := range file.Misses {
		sessions[m.Query], results[m.Query] = m.Session, m.Result
	}
	return sessions, results
}

// A miss keeps the session of the search that found nothing last (none for an entrance that knows no session), and a record ties a
// miss only when its session made it or when nobody knows who did. A session in another form is refused before anything is written.
func TestAMissKeepsTheSessionOfTheSearchThatMissedLastAndOnlyThatSessionTiesIt(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	miss := func(query, session string, minutes int) *Miss {
		t.Helper()
		earlier, err := b.recordMissAt(query, "", session, emptyView(), base.Add(time.Duration(minutes)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return earlier
	}
	check := func(when string, wantSessions, wantResults map[string]string) {
		t.Helper()
		sessions, results := sessionsAndResultsOnDisk(t, b)
		if !maps.Equal(sessions, wantSessions) || !maps.Equal(results, wantResults) {
			t.Fatalf("%s: sessions %v (want %v), results %v (want %v)", when, sessions, wantSessions, results, wantResults)
		}
	}
	miss("saffron lanterns", missSessionOne, 0)
	miss("juniper meadow", "", 1)
	check("each miss keeps the session of its search", map[string]string{"saffron lanterns": missSessionOne, "juniper meadow": ""}, map[string]string{"saffron lanterns": "", "juniper meadow": ""})

	words := []string{"saffron lanterns", "juniper meadow"}
	resultA := savedResult(t, b, "Checked the saffron lanterns and the juniper meadow by hand.")
	resultB := savedResult(t, b, "Lit the saffron lanterns and mown the juniper meadow.")
	if err := b.NoteMissResult(words, resultA, missSessionTwo); err != nil {
		t.Fatal(err)
	}
	check("the record of another session ties only the miss nobody is known to have made", map[string]string{"saffron lanterns": missSessionOne, "juniper meadow": ""}, map[string]string{"saffron lanterns": "", "juniper meadow": resultA})
	if err := b.NoteMissResult(words, resultB, missSessionOne); err != nil {
		t.Fatal(err)
	}
	check("the record of the session that searched ties its miss", map[string]string{"saffron lanterns": missSessionOne, "juniper meadow": ""}, map[string]string{"saffron lanterns": resultB, "juniper meadow": resultB})

	if earlier := miss("saffron lanterns", missSessionTwo, 2); earlier == nil || earlier.Result != resultB || earlier.Session != missSessionOne {
		t.Fatalf("another session that misses the words is told the earlier result: %+v", earlier)
	}
	check("the miss is now of the session that missed last, and keeps the result", map[string]string{"saffron lanterns": missSessionTwo, "juniper meadow": ""}, map[string]string{"saffron lanterns": resultB, "juniper meadow": resultB})
	miss("saffron lanterns", "", 3)
	check("a search that knows no session leaves the miss with none", map[string]string{"saffron lanterns": "", "juniper meadow": ""}, map[string]string{"saffron lanterns": resultB, "juniper meadow": resultB})

	before, _ := os.ReadFile(journalPath(t, b))
	for _, bad := range []string{"session-one", "ABCDEF0123456789", "111111111111111", "11111111111111111"} {
		if earlier, err := b.RecordMiss("kestrel harbor", "", bad, emptyView()); err == nil || earlier != nil {
			t.Fatalf("a session that is not 16 lower case hexadecimal digits is refused: %q %+v", bad, earlier)
		}
	}
	if after, _ := os.ReadFile(journalPath(t, b)); string(after) != string(before) {
		t.Fatal("a refused session wrote the journal")
	}
}

// The open misses of a session are the ones it made and the ones nobody is known to have made, newest first; the listing of every
// session asks with no session. A miss of another session is skipped without counting against the misses one call examines.
func TestOpenMissesOfASessionAreItsOwnAndTheUnknownOnes(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	add := func(query, session string, minutes int) {
		t.Helper()
		if _, err := b.recordMissAt(query, "", session, emptyView(), base.Add(time.Duration(minutes)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	add("first of one", missSessionOne, 0)
	add("no session", "", 1)
	add("of two", missSessionTwo, 2)
	for i := range openMissExamineMax { // as many newer misses of the other session as one call examines
		add(fmt.Sprintf("later of two %02d", i), missSessionTwo, 3+i)
	}
	for _, c := range []struct {
		session string
		since   time.Time
		want    []string
	}{
		{missSessionOne, time.Time{}, []string{"no session", "first of one"}},
		{missSessionOne, base.Add(time.Minute), []string{"no session"}},
		{"3333333333333333", time.Time{}, []string{"no session"}},
	} {
		open, err := b.OpenMisses(c.since, 8, c.session)
		if err != nil || !slices.Equal(openQueries(open), c.want) {
			t.Fatalf("session %s since %v: %q, want %q (%v)", c.session, c.since, openQueries(open), c.want, err)
		}
	}
	all, err := b.OpenMisses(time.Time{}, 64, "")
	if err != nil || len(all) != openMissExamineMax || all[0].Query != fmt.Sprintf("later of two %02d", openMissExamineMax-1) || all[0].Session != missSessionTwo {
		t.Fatalf("with no session the misses of every session are listed, the newest first: %q (%v)", openQueries(all), err)
	}
}

// The reader keeps a miss whose session is none or 16 lower case hexadecimal digits, and drops a miss with a session in any other
// form without failing the journal.
func TestSearchJournalReadsASessionOnlyInTheFormOfSixteenHexDigits(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	const file = `{ "lookups": [], "misses": [
		{"query":"with a session","at":"2026-11-01T09:00:00Z","session":"` + missSessionOne + `"},
		{"query":"without one","at":"2026-11-01T09:01:00Z"},
		{"query":"a name","at":"2026-11-01T09:02:00Z","session":"claude-session"},
		{"query":"upper case","at":"2026-11-01T09:03:00Z","session":"ABCDEF0123456789"},
		{"query":"too long","at":"2026-11-01T09:04:00Z","session":"11111111111111112"},
		{"query":"a number","at":"2026-11-01T09:05:00Z","session":7} ] }`
	if err := os.WriteFile(journalPath(t, b), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	open, err := b.OpenMisses(time.Time{}, 8, "")
	if err != nil {
		t.Fatalf("a session in another form does not make the journal unreadable: %v", err)
	}
	got := map[string]string{}
	for _, m := range open {
		got[m.Query] = m.Session
	}
	if want := (map[string]string{"with a session": missSessionOne, "without one": ""}); !maps.Equal(got, want) {
		t.Fatalf("only a session in the form the journal writes is read: %v, want %v", got, want)
	}
}

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func feedbackWindowFixture(t *testing.T, count int) (Store, string) {
	t.Helper()
	s := Store{Dir: t.TempDir()}
	scope := "/synthetic/window"
	path := s.feedbackPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range count {
		raw, _ := json.Marshal(Feedback{ID: fmt.Sprintf("%016x", i%273+1), Request: "Synthetic request.", Summary: "Synthetic summary.", Reply: fmt.Sprintf("reply %d", i), At: time.Unix(int64(i+1), 0)})
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return s, scope
}

func TestFeedbackWindowPreservesRowsBeforeDeduplication(t *testing.T) {
	for _, n := range []int{0, 1, 255, 256, 257, 800} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, scope := feedbackWindowFixture(t, n)
			got, err := s.RecentFeedback(scope)
			if err != nil {
				t.Fatal(err)
			}
			want := min(n, 256)
			if len(got) != want {
				t.Fatal(n, len(got), want)
			}
			for i, v := range got {
				if v.Reply != fmt.Sprintf("reply %d", n-i-1) {
					t.Fatal(i, v.Reply)
				}
			}
		})
	}
}

// writeFeedbackLines writes the lines to the feedback file of the scope as they are (broken ones included).
func writeFeedbackLines(t *testing.T, s Store, scope string, lines ...string) {
	t.Helper()
	path := s.feedbackPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0600); err != nil {
		t.Fatal(err)
	}
}

func feedbackLine(t *testing.T, id, reply string, at int64) string {
	t.Helper()
	raw, err := json.Marshal(Feedback{ID: id, Request: "Synthetic request.", Summary: "Synthetic summary.", Reply: reply, At: time.Unix(at, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func feedbackIDs(items []Feedback) []string {
	ids := []string{}
	for _, f := range items {
		ids = append(ids, f.ID)
	}
	return ids
}

// FeedbackSince is not a window: it reads the file from its start, whatever the count and the size (RecentFeedback keeps the newest 256 of
// the last 4 MiB), and returns every conversation after the time, newest first.
func TestFeedbackSinceReadsTheWholeFileWhateverItsCountAndSize(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 600)
	got, err := s.FeedbackSince(scope, time.Time{})
	if err != nil || len(got) != 273 { // the fixture repeats 273 ids, so 273 are left after the duplicates
		t.Fatalf("every id of 600 lines, not the newest 256 lines: %d %v", len(got), err)
	}
	if got[0].Reply != "reply 599" || got[len(got)-1].Reply != "reply 327" {
		t.Fatalf("the newest line of each id, newest first: %q ... %q", got[0].Reply, got[len(got)-1].Reply)
	}
	// 1100 records of about 4 KB each are more than 4 MiB: the oldest ones are beyond the tail RecentFeedback reads.
	big := strings.Repeat("x", 1400)
	lines := []string{}
	for i := 1; i <= 1100; i++ {
		raw, _ := json.Marshal(Feedback{ID: fmt.Sprintf("%016x", i), Request: big, Summary: big, Reply: big, At: time.Unix(int64(i), 0)})
		lines = append(lines, string(raw)+"\n")
	}
	writeFeedbackLines(t, s, scope, lines...)
	got, err = s.FeedbackSince(scope, time.Time{})
	if err != nil || len(got) != 1100 || got[len(got)-1].ID != fmt.Sprintf("%016x", 1) {
		t.Fatalf("all 1100 records of a file larger than 4 MiB, the oldest last: %d %v", len(got), err)
	}
	if got, err := (Store{Dir: t.TempDir()}).FeedbackSince(scope, time.Time{}); err != nil || got != nil {
		t.Fatalf("a project with no feedback file has no conversations: %d %v", len(got), err)
	}
}

func TestFeedbackSinceKeepsOnlyTheNewestRecordOfAnId(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	writeFeedbackLines(t, s, scope,
		feedbackLine(t, "000000000000000a", "first, replaced", 10),
		feedbackLine(t, "000000000000000b", "other", 20),
		feedbackLine(t, "000000000000000a", "second, kept", 30),
		feedbackLine(t, "000000000000000c", "newer than the time, then replaced by an older one", 50),
		feedbackLine(t, "000000000000000c", "the last of its id is not after the time", 5),
	)
	got, err := s.FeedbackSince(scope, time.Unix(7, 0))
	if err != nil || fmt.Sprint(feedbackIDs(got)) != "[000000000000000a 000000000000000b]" || got[0].Reply != "second, kept" {
		t.Fatalf("the newest record of an id stands, and an id whose last record is not after the time is not there: %v %v", got, err)
	}
	// An older record of an id that the time leaves out does not come back when the newest one is after the time.
	got, err = s.FeedbackSince(scope, time.Unix(25, 0))
	if err != nil || fmt.Sprint(feedbackIDs(got)) != "[000000000000000a]" || got[0].Reply != "second, kept" {
		t.Fatalf("only the newest record of the id that is after the time: %v %v", got, err)
	}
}

func TestFeedbackSinceSkipsBrokenLinesAndTheLineStillBeingWritten(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	tooLong := Feedback{ID: "00000000000000f1", Request: "r", Summary: "s", Reply: strings.Repeat("y", 40<<10), At: time.Unix(15, 0)}
	rawLong, _ := json.Marshal(tooLong)
	tooLong.ID, tooLong.Reply = "00000000000000f2", strings.Repeat("z", 100<<10)
	rawLonger, _ := json.Marshal(tooLong) // longer than the buffer the file is read with
	writeFeedbackLines(t, s, scope,
		feedbackLine(t, "0000000000000001", "kept", 10),
		"this is not json\n",
		`{"id":"00000000000000e1","request":"r","summary":"s"}`+"\n", // no reply
		string(rawLong)+"\n",                                         // a line over 32 KiB
		string(rawLonger)+"\n",                                       // a line over 64 KiB
		feedbackLine(t, "0000000000000002", "kept after the broken lines", 20),
		strings.TrimSuffix(feedbackLine(t, "0000000000000003", "complete but not ended by a newline yet", 30), "\n"),
	)
	got, err := s.FeedbackSince(scope, time.Time{})
	if err != nil || fmt.Sprint(feedbackIDs(got)) != "[0000000000000002 0000000000000001]" {
		t.Fatalf("broken and too long lines are skipped, the line without its newline is not taken: %v %v", feedbackIDs(got), err)
	}
}

func TestFeedbackSinceReturnsOnlyWhatIsAfterTheTime(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	writeFeedbackLines(t, s, scope,
		feedbackLine(t, "0000000000000001", "before", 10),
		feedbackLine(t, "0000000000000002", "at the time", 20),
		feedbackLine(t, "0000000000000003", "after", 30),
	)
	got, err := s.FeedbackSince(scope, time.Unix(20, 0))
	if err != nil || fmt.Sprint(feedbackIDs(got)) != "[0000000000000003]" {
		t.Fatalf("a record at the very time is not after it: %v %v", feedbackIDs(got), err)
	}
	if got, err = s.FeedbackSince(scope, time.Time{}); err != nil || len(got) != 3 {
		t.Fatalf("with no time every conversation is returned: %v %v", feedbackIDs(got), err)
	}
}

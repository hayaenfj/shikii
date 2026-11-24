package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func feedbackReplies(items []FeedbackLine) []string {
	out := []string{}
	for _, f := range items {
		out = append(out, f.Reply)
	}
	return out
}

func feedbackLineIDs(items []FeedbackLine) []string {
	out := []string{}
	for _, f := range items {
		out = append(out, f.ID)
	}
	return out
}

// hashOfLine is the hash a mark holds for a line of the file, computed here: SHA-256 of the line without its newline, in hex.
func hashOfLine(line string) string {
	sum := sha256.Sum256([]byte(strings.TrimSuffix(line, "\n")))
	return hex.EncodeToString(sum[:])
}

// FeedbackAfter is not a window: it reads the file from the mark to its end, whatever the count and the size (RecentFeedback keeps the
// newest 256 of the last 4 MiB), and returns the conversations in the order they were written.
func TestFeedbackAfterReadsTheWholeFileWhateverItsCountAndSize(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 600)
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || len(got) != 273 { // the fixture repeats 273 ids, so 273 are left after the duplicates
		t.Fatalf("every id of 600 lines, not the newest 256 lines: %d %v", len(got), err)
	}
	if got[0].Reply != "reply 327" || got[len(got)-1].Reply != "reply 599" {
		t.Fatalf("the newest line of each id, in the order of those lines: %q ... %q", got[0].Reply, got[len(got)-1].Reply)
	}
	// 1100 records of about 4 KB each are more than 4 MiB: the oldest ones are beyond the tail RecentFeedback reads.
	big := strings.Repeat("x", 1400)
	lines := []string{}
	for i := 1; i <= 1100; i++ {
		raw, _ := json.Marshal(Feedback{ID: fmt.Sprintf("%016x", i), Request: big, Summary: big, Reply: big, At: time.Unix(int64(i), 0)})
		lines = append(lines, string(raw)+"\n")
	}
	writeFeedbackLines(t, s, scope, lines...)
	got, err = s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || len(got) != 1100 || got[0].ID != fmt.Sprintf("%016x", 1) {
		t.Fatalf("all 1100 records of a file larger than 4 MiB, the oldest first: %d %v", len(got), err)
	}
	if got, err := (Store{Dir: t.TempDir()}).FeedbackAfter(scope, FeedbackMark{}); err != nil || got != nil {
		t.Fatalf("a project with no feedback file has no conversations: %d %v", len(got), err)
	}
}

// Of a repeated ID the newest line stands, at its own place in the file; an older line of it before the mark is not what was meant, so the
// conversation is read again when a newer line of it is after the mark.
func TestFeedbackAfterKeepsOnlyTheNewestLineOfAnId(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	first := feedbackLine(t, "000000000000000a", "first, replaced", 10)
	other := feedbackLine(t, "000000000000000b", "other", 20)
	writeFeedbackLines(t, s, scope,
		first,
		other,
		feedbackLine(t, "000000000000000a", "second, kept", 30),
		feedbackLine(t, "000000000000000c", "replaced by a line with an older time", 50),
		feedbackLine(t, "000000000000000c", "the last of its id, whatever its time", 5),
	)
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || fmt.Sprint(feedbackReplies(got)) != "[other second, kept the last of its id, whatever its time]" {
		t.Fatalf("the newest line of an id stands, in file order, whatever its time says: %v %v", feedbackReplies(got), err)
	}
	got, err = s.FeedbackAfter(scope, FeedbackMark{Offset: int64(len(first)), Hash: hashOfLine(first)})
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[000000000000000b 000000000000000a 000000000000000c]" {
		t.Fatalf("after the first line: %v %v", feedbackLineIDs(got), err)
	}
	got, err = s.FeedbackAfter(scope, FeedbackMark{Offset: int64(len(first) + len(other)), Hash: hashOfLine(other)})
	if err != nil || fmt.Sprint(feedbackReplies(got)) != "[second, kept the last of its id, whatever its time]" {
		t.Fatalf("after the second line, the first line of an id is not read again, the newer one is: %v %v", feedbackReplies(got), err)
	}
}

// A broken or too long line is skipped; a line with no newline yet is not taken, and no mark is ever past it.
func TestFeedbackAfterSkipsBrokenLinesAndTheLineStillBeingWritten(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	tooLong := Feedback{ID: "00000000000000f1", Request: "r", Summary: "s", Reply: strings.Repeat("y", 40<<10), At: time.Unix(15, 0)}
	rawLong, _ := json.Marshal(tooLong)
	tooLong.ID, tooLong.Reply = "00000000000000f2", strings.Repeat("z", 100<<10)
	rawLonger, _ := json.Marshal(tooLong) // longer than the buffer the file is read with
	complete := feedbackLine(t, "0000000000000002", "kept after the broken lines", 20)
	writeFeedbackLines(t, s, scope,
		feedbackLine(t, "0000000000000001", "kept", 10),
		"this is not json\n",
		`{"id":"00000000000000e1","request":"r","summary":"s"}`+"\n", // no reply
		string(rawLong)+"\n",                                         // a line over 32 KiB
		string(rawLonger)+"\n",                                       // a line over 64 KiB
		complete,
		strings.TrimSuffix(feedbackLine(t, "0000000000000003", "complete but not ended by a newline yet", 30), "\n"),
	)
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[0000000000000001 0000000000000002]" {
		t.Fatalf("broken and too long lines are skipped, the line without its newline is not taken: %v %v", feedbackLineIDs(got), err)
	}
	raw, _ := os.ReadFile(s.feedbackPath(scope))
	if last := got[len(got)-1].Mark.Offset; last != int64(strings.LastIndex(string(raw), "\n")+1) {
		t.Fatalf("no mark is past the line being written: %d of %d bytes", last, len(raw))
	}
	if got[1].Mark.Hash != hashOfLine(complete) {
		t.Fatalf("the hash is that of the line without its newline: %s", got[1].Mark.Hash)
	}
}

// Each conversation carries the mark just after its line, and a read from that mark returns what comes after it.
func TestFeedbackAfterGivesTheMarkOfEachLineAndResumesFromIt(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lines := []string{
		feedbackLine(t, "0000000000000001", "one", 30), // times do not matter: only the place does
		"this is not json\n",
		feedbackLine(t, "0000000000000002", "two", 10),
		feedbackLine(t, "0000000000000003", "three", 20),
	}
	writeFeedbackLines(t, s, scope, lines...)
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || len(got) != 3 {
		t.Fatalf("three conversations: %v %v", feedbackLineIDs(got), err)
	}
	end := func(n int) int64 { // the offset after the first n lines of the file
		total := 0
		for _, line := range lines[:n] {
			total += len(line)
		}
		return int64(total)
	}
	for i, want := range []struct {
		line int // the place in lines of the conversation
	}{{0}, {2}, {3}} {
		if got[i].Mark != (FeedbackMark{Offset: end(want.line + 1), Hash: hashOfLine(lines[want.line])}) {
			t.Fatalf("conversation %d: mark %+v, want offset %d hash %s", i, got[i].Mark, end(want.line+1), hashOfLine(lines[want.line]))
		}
	}
	for i := range got {
		rest, err := s.FeedbackAfter(scope, got[i].Mark)
		if err != nil || len(rest) != len(got)-i-1 {
			t.Fatalf("after conversation %d: %v %v", i, feedbackLineIDs(rest), err)
		}
	}
}

// A mark whose offset no longer ends the line with its hash is looked up by the hash: the lines before it were removed, or the file
// was replaced. When no line has the hash, the read starts at the beginning.
func TestFeedbackAfterChecksTheMarkAgainstTheFile(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	one := feedbackLine(t, "0000000000000001", "one", 10)
	two := feedbackLine(t, "0000000000000002", "two", 20)
	three := feedbackLine(t, "0000000000000003", "three", 30)
	four := feedbackLine(t, "0000000000000004", "four", 40)
	writeFeedbackLines(t, s, scope, one, two, three, four)
	mark := FeedbackMark{Offset: int64(len(one) + len(two)), Hash: hashOfLine(two)}
	after := func(label, want string) {
		t.Helper()
		got, err := s.FeedbackAfter(scope, mark)
		if err != nil || fmt.Sprint(feedbackReplies(got)) != want {
			t.Fatalf("%s: %v %v, want %s", label, feedbackReplies(got), err, want)
		}
	}
	after("the file as it was", "[three four]")
	writeFeedbackLines(t, s, scope, two, three, four) // the line before the mark was removed: the offset points into the next line
	after("a line before the mark was removed", "[three four]")
	writeFeedbackLines(t, s, scope, three, four) // the line the mark stands for was removed too
	after("the line of the mark is gone", "[three four]")
	writeFeedbackLines(t, s, scope, four, two, three) // another file: the line is found elsewhere
	after("the line is elsewhere", "[three]")
	writeFeedbackLines(t, s, scope, one, feedbackLine(t, "0000000000000002", "TWO", 20)) // the same offset ends a line with another hash
	after("another line at the same offset", "[one TWO]")
	writeFeedbackLines(t, s, scope, one[:10]) // shorter than the offset
	after("a file shorter than the offset", "[]")
	for _, m := range []FeedbackMark{{Offset: 0, Hash: mark.Hash}, {Offset: mark.Offset}, {Offset: -4, Hash: mark.Hash}} {
		writeFeedbackLines(t, s, scope, one, two)
		got, err := s.FeedbackAfter(scope, m)
		if err != nil || len(got) != 2 {
			t.Fatalf("a mark with no offset or no hash is the beginning: %+v %v %v", m, feedbackReplies(got), err)
		}
	}
}

// What RecentFeedback takes from a file and what FeedbackAfter takes from the beginning are the same conversations (the first newest first,
// the second in file order): the two read lines by the same rules.
func TestFeedbackAfterTakesTheSameLinesAsRecentFeedbackOnOddFiles(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	line := func(id string, i int, extra string) string {
		return fmt.Sprintf(`{"id":"%s","request":"r%s","summary":"s","reply":"x","at":"%s"}`+"\n", id, extra, time.Unix(int64(i), 0).UTC().Format(time.RFC3339))
	}
	writeFeedbackLines(t, s, scope,
		line("a", 1, ""),
		"garbage\n",
		line("b", 2, strings.Repeat("y", 40<<10)), // 40 KiB: too long
		line("c", 3, strings.Repeat("y", 70<<10)), // 70 KiB: over the buffer
		line("a", 4, ""), // repeated id
		`{"id":"d","request":"r","summary":"s","reply":"","at":"1970-01-01T00:00:00Z"}`+"\n", // empty reply
		line("e", 5, ""),
		strings.TrimSuffix(line("f", 6, ""), "\n"), // partial last line
	)
	recent, err := s.RecentFeedback(scope)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != len(after) {
		t.Fatalf("recent %v, after %v", feedbackIDs(recent), feedbackLineIDs(after))
	}
	for i, v := range recent {
		if w := after[len(after)-1-i].Feedback; !reflect.DeepEqual(v, w) {
			t.Fatalf("recent %v, after %v", feedbackIDs(recent), feedbackLineIDs(after))
		}
	}
}

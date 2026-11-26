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

// A line that is in the file twice, byte for byte, must not make the read skip what lies between the copies. The history was [X, L, A, B, L']
// (L' is a second copy of L), a run recorded the place just after L, then X was removed and the length of X was the length of A, B and L
// together: the old offset is now the end of L' and L' has the hash of the mark, but A and B were never read. The read goes on after the first
// line that has the hash, so A and B are read (L' is read again: a repeat is the price, a skip is not). So the read goes on after the
// first copy even when the mark was made at the second.
func TestFeedbackAfterDoesNotSkipLinesBetweenTwoCopiesOfTheMarkedLine(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lineL := feedbackLine(t, "000000000000000a", "L-reply", 10)
	lineA := feedbackLine(t, "000000000000000b", "A-reply "+strings.Repeat("a", 30), 20)
	lineB := feedbackLine(t, "000000000000000c", "B-reply "+strings.Repeat("b", 30), 30)
	want := len(lineA) + len(lineB) + len(lineL) // the length X must have
	base := feedbackLine(t, "000000000000000d", "X", 5)
	if want < len(base) {
		t.Fatalf("the fixture cannot make X as short as %d bytes", want)
	}
	lineX := feedbackLine(t, "000000000000000d", "X"+strings.Repeat("x", want-len(base)), 5)
	if len(lineX) != want {
		t.Fatalf("X has %d bytes, want %d", len(lineX), want)
	}
	// What a run saw: [X, L, A, B, L'] and the place just after L.
	writeFeedbackLines(t, s, scope, lineX, lineL, lineA, lineB, lineL)
	mark := FeedbackMark{Offset: int64(len(lineX) + len(lineL)), Hash: hashOfLine(lineL)}
	got, err := s.FeedbackAfter(scope, mark)
	if err != nil || fmt.Sprint(feedbackReplies(got)) != "[A-reply "+strings.Repeat("a", 30)+" B-reply "+strings.Repeat("b", 30)+" L-reply]" {
		t.Fatalf("the file the mark was made on: %v %v", feedbackReplies(got), err)
	}
	// After forget removed X: the old offset is the end of L', whose hash is the mark's.
	writeFeedbackLines(t, s, scope, lineL, lineA, lineB, lineL)
	if end := int64(len(lineL) + len(lineA) + len(lineB) + len(lineL)); end != mark.Offset {
		t.Fatalf("the fixture is wrong: the old offset %d is not the end of the file %d", mark.Offset, end)
	}
	got, err = s.FeedbackAfter(scope, mark)
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[000000000000000b 000000000000000c 000000000000000a]" {
		t.Fatalf("A and B must be read after X was removed: %v %v", feedbackLineIDs(got), err)
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

// A line that is in the file more than once, byte for byte, carries which copy it is: the number of lines with the same bytes before it
// (0 for the first). The read goes on after that copy, so a copy at the end of the file that was read is not read again.
func TestFeedbackAfterGivesTheCopyOfALineThatIsInTheFileMoreThanOnce(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lineL := feedbackLine(t, "000000000000000a", "L-reply", 10)
	lineA := feedbackLine(t, "000000000000000b", "A-reply", 20)
	lineB := feedbackLine(t, "000000000000000c", "B-reply", 30)
	writeFeedbackLines(t, s, scope, lineL, lineA, lineL, lineB, lineL)
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[000000000000000b 000000000000000c 000000000000000a]" {
		t.Fatalf("the newest line of an id stands: %v %v", feedbackLineIDs(got), err)
	}
	for i, want := range []int{0, 0, 2} { // A and B are in the file once; the line that stands for L is its third copy
		if got[i].Mark.Copy != want {
			t.Errorf("conversation %d: copy %d, want %d", i, got[i].Mark.Copy, want)
		}
	}
	rest, err := s.FeedbackAfter(scope, got[2].Mark)
	if err != nil || len(rest) != 0 {
		t.Fatalf("after the third copy nothing is left, but %v came again: %v", feedbackLineIDs(rest), err)
	}
	rest, err = s.FeedbackAfter(scope, got[0].Mark)
	if err != nil || fmt.Sprint(feedbackLineIDs(rest)) != "[000000000000000c 000000000000000a]" {
		t.Fatalf("after A: %v %v", feedbackLineIDs(rest), err)
	}
}

// The copy is counted from the beginning of the file, not from where the read started: a read that goes on after an earlier copy gives
// the later copies the numbers they have in the file.
func TestFeedbackAfterCountsCopiesFromTheBeginningOfTheFile(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lineL := feedbackLine(t, "000000000000000a", "L-reply", 10)
	lineA := feedbackLine(t, "000000000000000b", "A-reply", 20)
	lineB := feedbackLine(t, "000000000000000c", "B-reply", 30)
	writeFeedbackLines(t, s, scope, lineL, lineA, lineL, lineB, lineL)
	firstCopy := FeedbackMark{Offset: int64(len(lineL)), Hash: hashOfLine(lineL)} // the mark of the first copy, which is how a run that read up to it made it
	got, err := s.FeedbackAfter(scope, firstCopy)
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[000000000000000b 000000000000000c 000000000000000a]" {
		t.Fatalf("what comes after the first copy: %v %v", feedbackLineIDs(got), err)
	}
	if last := got[2].Mark; last.Copy != 2 || last.Hash != hashOfLine(lineL) {
		t.Fatalf("the line that stands for L is its third copy in the file, whichever line the read started after: %+v", last)
	}
	secondCopy := FeedbackMark{Offset: int64(len(lineL) + len(lineA) + len(lineL)), Hash: hashOfLine(lineL), Copy: 1}
	got, err = s.FeedbackAfter(scope, secondCopy)
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[000000000000000c 000000000000000a]" || got[1].Mark.Copy != 2 {
		t.Fatalf("after the second copy: %v %+v %v", feedbackLineIDs(got), got, err)
	}
}

// When fewer copies are in the file than the mark names (lines were removed by hand, or the file was replaced), the read goes on after
// the first copy: a conversation may be read again, none is skipped. A mark with no copy (one made before the field) is the first copy.
func TestFeedbackAfterGoesOnAfterTheFirstCopyWhenTheNamedCopyIsGone(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lineL := feedbackLine(t, "000000000000000a", "L-reply", 10)
	lineA := feedbackLine(t, "000000000000000b", "A-reply", 20)
	lineB := feedbackLine(t, "000000000000000c", "B-reply", 30)
	second := FeedbackMark{Offset: int64(len(lineL) + len(lineA) + len(lineL)), Hash: hashOfLine(lineL), Copy: 1}
	for _, c := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"the file the mark was made on", []string{lineL, lineA, lineL, lineB}, "[000000000000000c]"},
		{"the first copy was removed", []string{lineA, lineL, lineB}, "[000000000000000c]"},
		{"the second copy was removed", []string{lineL, lineA, lineB}, "[000000000000000b 000000000000000c]"},
		{"the line is gone", []string{lineA, lineB}, "[000000000000000b 000000000000000c]"},
	} {
		writeFeedbackLines(t, s, scope, c.lines...)
		got, err := s.FeedbackAfter(scope, second)
		if err != nil || fmt.Sprint(feedbackLineIDs(got)) != c.want {
			t.Errorf("%s: %v %v, want %s", c.name, feedbackLineIDs(got), err, c.want)
		}
	}
	writeFeedbackLines(t, s, scope, lineL, lineA, lineL, lineB)
	old := FeedbackMark{Offset: second.Offset, Hash: second.Hash} // no copy: the first
	got, err := s.FeedbackAfter(scope, old)
	if err != nil || fmt.Sprint(feedbackLineIDs(got)) != "[000000000000000b 000000000000000a 000000000000000c]" {
		t.Errorf("a mark without a copy goes on after the first copy: %v %v", feedbackLineIDs(got), err)
	}
}

// The copy is the number of the same lines before it in the file, and a named copy is used only where that line ends at the offset the mark
// remembers. When a copy before the place is taken out by hand and more copies are after it, the named copy is a later line that ends
// elsewhere, so the read goes on after the first copy and shows what lies between again: B is never skipped.
func TestFeedbackAfterReadsAgainFromTheFirstCopyWhenAnEarlierCopyIsTakenOutByHand(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lineL := feedbackLine(t, "000000000000000a", "L-reply", 10)
	lineA := feedbackLine(t, "000000000000000b", "A-reply", 20)
	lineB := feedbackLine(t, "000000000000000c", "B-reply", 30)
	writeFeedbackLines(t, s, scope, lineL, lineA, lineL) // what the run read: the window is on the second copy
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || len(got) != 2 || got[1].Mark.Copy != 1 {
		t.Fatalf("premise: %v %+v %v", feedbackLineIDs(got), got, err)
	}
	mark := got[1].Mark
	writeFeedbackLines(t, s, scope, lineA, lineL, lineB, lineL) // B and a copy of L written after; the first copy taken out by hand
	rest, err := s.FeedbackAfter(scope, mark)
	if err != nil || fmt.Sprint(feedbackLineIDs(rest)) != "[000000000000000c 000000000000000a]" {
		t.Fatalf("B was written after the place and must be read: %v %v", feedbackLineIDs(rest), err)
	}
}

// A line before the named copy that is not a copy was removed (shikii forget does that to a finished session's lines): the named copy ends
// somewhere else, so the read goes on after the first copy and the conversations between the copies come again, none is skipped.
func TestFeedbackAfterReadsAgainFromTheFirstCopyWhenAnotherLineBeforeTheNamedCopyWasRemoved(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 0)
	lineX := feedbackLine(t, "000000000000000d", "X-reply", 5)
	lineL := feedbackLine(t, "000000000000000a", "L-reply", 10)
	lineA := feedbackLine(t, "000000000000000b", "A-reply", 20)
	writeFeedbackLines(t, s, scope, lineX, lineL, lineA, lineL)
	got, err := s.FeedbackAfter(scope, FeedbackMark{})
	if err != nil || len(got) != 3 || got[2].Mark.Copy != 1 {
		t.Fatalf("premise: %v %+v %v", feedbackLineIDs(got), got, err)
	}
	mark := got[2].Mark
	if rest, err := s.FeedbackAfter(scope, mark); err != nil || len(rest) != 0 {
		t.Fatalf("on the file the mark was made on nothing is left: %v %v", feedbackLineIDs(rest), err)
	}
	writeFeedbackLines(t, s, scope, lineL, lineA, lineL) // X removed
	rest, err := s.FeedbackAfter(scope, mark)
	if err != nil || fmt.Sprint(feedbackLineIDs(rest)) != "[000000000000000b 000000000000000a]" {
		t.Fatalf("after the first copy, so A is read again: %v %v", feedbackLineIDs(rest), err)
	}
}

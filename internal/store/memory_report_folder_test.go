package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// reportAnswersAsAsking runs the report and compares every quote with what asking for it says (asked), and returns how many quotes
// are assessed. It is the check of a report on a folder whose names are not what shikii writes.
func reportAnswersAsAsking(t *testing.T, s Store, scope string, quotes int) (assessed int) {
	t.Helper()
	want, err := asked(s, scope)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.All) != quotes {
		t.Fatalf("every quote is counted: %d of %d", len(r.All), quotes)
	}
	for _, item := range r.All {
		w := want[[2]string{item.SourceID, item.Quote.Key}]
		if !reflect.DeepEqual(item.Assessment, w) {
			t.Fatalf("%s %s: the report says %+v, asking for the quote says %+v", item.SourceID, item.Quote.Key, item.Assessment, w)
		}
		if w != nil {
			assessed++
		}
	}
	return assessed
}

// A disk that ignores case, or reads two spellings as one letter, finds a file by a name that is not its listed one: asking for a quote
// finds <hash>.json under <HASH>.json, and on an APFS volume that ignores case also under <hash> with the long s (U+017F) for the s of ".json", which such
// a volume reads as s. The report says what asking says on that disk, so a folder with any name that is not exactly the one shikii
// writes (lower-case hexadecimal and ".json") is answered by asking for each quote. On a disk that tells the spellings apart asking
// finds nothing and so does the report; on this machine's APFS the long s makes the two differ if the report trusts a list that folds
// case with strings.ToLower.
func TestMemoryReportSaysWhatAskingSaysForNamesADiskReadsAsTheSame(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rename func(file string) string // the name the assessed quote's file is given
	}{
		{"the long s (U+017F) for the s of .json", func(f string) string { return strings.TrimSuffix(f, ".json") + ".j\u017fon" }},
		{"upper-case hexadecimal", func(f string) string { return strings.ToUpper(strings.TrimSuffix(f, ".json")) + ".json" }},
		{"upper-case extension", func(f string) string { return strings.TrimSuffix(f, ".json") + ".JSON" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, scope, pairs := reportFixture(t, 6, 3) // 12 quotes, 4 assessed
			first := pairs[2]                         // the first assessed quote
			path := s.memoryAssessmentPath(scope, first[0], first[1])
			renamed := filepath.Join(filepath.Dir(path), tc.rename(filepath.Base(path)))
			if err := os.Rename(path, renamed); err != nil {
				t.Fatal(err)
			}
			if a, err := s.memoryAssessment(scope, first[0], first[1]); err != nil || a == nil {
				t.Logf("on this disk asking does not find the renamed file (%v): the two sides are the same here", err)
			}
			reportAnswersAsAsking(t, s, scope, len(pairs))
		})
	}
}

// A folder that holds a name of another shape (a note, the leftover of a save) is not listed: every quote is asked for by
// memoryAssessment, which opens a file only when it is there. The count of opens says the list was not used.
func TestMemoryReportAsksForEachQuoteWhenAFolderHoldsAnotherName(t *testing.T) {
	stray := []string{
		"notes.txt",
		"abcd.json", // hexadecimal and ".json", but not 64 digits
		strings.Repeat("a", 65) + ".json",
		strings.Repeat("0", 63) + "g.json", // 64 characters, one of them not hexadecimal
		"." + strings.Repeat("a", 26) + ".tmp",
	}
	for _, name := range stray {
		t.Run(name, func(t *testing.T) {
			s, scope, pairs := reportFixture(t, 6, 3) // 12 quotes, 4 assessed
			dir := filepath.Dir(s.memoryAssessmentPath(scope, pairs[0][0], pairs[0][1]))
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := reportAnswersAsAsking(t, s, scope, len(pairs)); got != 4 {
				t.Fatalf("%d quotes assessed, want 4", got)
			}
			old := openInRoot
			t.Cleanup(func() { openInRoot = old })
			opened := 0
			openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
				if strings.HasSuffix(name, ".json") {
					opened++
				}
				return old(r, name, flag, perm)
			}
			if _, err := s.MemoryReport(scope); err != nil {
				t.Fatal(err)
			}
			if opened != len(pairs) {
				t.Fatalf("opened %d files for %d quotes with a name of another shape in the folder, want one ask for each quote", opened, len(pairs))
			}
		})
	}
}

// A folder that is gone by the end of the report is not an error: asking for each quote answers "not there" for a folder that is not
// there, and the quotes the report read before are as they were.
func TestMemoryReportSaysNothingWhenTheFolderIsGoneAtTheEnd(t *testing.T) {
	s, scope, pairs := reportFixture(t, 6, 3)
	folder := filepath.Dir(s.memoryAssessmentPath(scope, pairs[0][0], pairs[0][1]))
	old := openInRoot
	t.Cleanup(func() { openInRoot = old })
	removed := false
	openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
		f, err := old(r, name, flag, perm)
		if strings.HasSuffix(name, ".json") && !removed {
			removed = true
			if err := os.RemoveAll(folder); err != nil {
				t.Fatal(err)
			}
		}
		return f, err
	}
	r, err := s.MemoryReport(scope)
	if !removed {
		t.Fatal("the folder was never removed: the fixture opened no assessment file")
	}
	if err != nil || len(r.All) != len(pairs) {
		t.Fatalf("a folder that is gone at the end: %v (%d quotes)", err, len(r.All))
	}
}

// The folder is opened once and read to the end of the report. A path that stopped naming that folder while the report ran (a link put
// in its place: asking for the next quote said "links cannot be used in the record folder"; another folder put there) is said at the
// end, not read on through without a word.
func TestMemoryReportSaysAFolderThatChangedDuringTheReport(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		swap       func(t *testing.T, folder string)
	}{
		{"a link put in its place", "links cannot be used in the record folder", func(t *testing.T, folder string) {
			moved := filepath.Join(t.TempDir(), "moved")
			if err := os.Rename(folder, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, folder); err != nil {
				t.Fatal(err)
			}
		}},
		{"another folder put in its place", "the record folder changed", func(t *testing.T, folder string) {
			if err := os.Rename(folder, filepath.Join(t.TempDir(), "moved")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(folder, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, scope, pairs := reportFixture(t, 6, 3)
			folder := filepath.Dir(s.memoryAssessmentPath(scope, pairs[0][0], pairs[0][1]))
			old := openInRoot
			t.Cleanup(func() { openInRoot = old })
			swapped := false
			openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
				f, err := old(r, name, flag, perm)
				if strings.HasSuffix(name, ".json") && !swapped {
					swapped = true
					tc.swap(t, folder) // right after the first assessment file is opened
				}
				return f, err
			}
			r, err := s.MemoryReport(scope)
			if !swapped {
				t.Fatal("the folder was never swapped: the fixture opened no assessment file")
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("the folder changed during the report and the report said %v (%d quotes), want %q", err, len(r.All), tc.want)
			}
		})
	}
}

// A folder that stays where it is is not said to have changed: the check at the end of a report never fails a report on a folder
// nothing happened to.
func TestMemoryReportDoesNotSayAFolderChangedWhenItDidNot(t *testing.T) {
	s, scope, pairs := reportFixture(t, 6, 3)
	r, err := s.MemoryReport(scope)
	if err != nil || len(r.All) != len(pairs) {
		t.Fatalf("a folder that did not change: %v (%d quotes)", err, len(r.All))
	}
}

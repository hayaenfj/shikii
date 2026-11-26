package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// reportFixture saves n deliveries of two quotes each and assesses every k-th quote (helpful, in the request it was delivered in).
// It returns the store, the scope and the (delivery, key) pairs in delivery order.
func reportFixture(t *testing.T, n, every int) (Store, string, [][2]string) {
	t.Helper()
	s, d, _ := memoryStoreFixture(t)
	var pairs [][2]string
	count := 0
	for i := 0; i < n; i++ {
		d.ID = fmt.Sprintf("%016x", 0x100+i)
		d.Quotes = nil
		for q := 0; q < 2; q++ {
			key := fmt.Sprintf("lesson:%016x:%016x", 0x200+i%5, 0x300+q)
			d.Quotes = append(d.Quotes, MemoryQuote{Key: key, ID: fmt.Sprintf("%016x", 0x200+i%5), Revision: fmt.Sprintf("%016x", 0x300+q), Lesson: "synthetic lesson", AppliesTo: "synthetic scene", Sources: []string{"0000000000000004"}})
			pairs = append(pairs, [2]string{d.ID, key})
		}
		if err := s.SaveMemoryDelivery(d); err != nil {
			t.Fatal(err)
		}
		for _, q := range d.Quotes {
			count++
			if every > 0 && count%every == 0 {
				a := MemoryAssessment{SourceID: d.ID, Key: q.Key, Result: "helpful", Decision: "used it", Evidence: "synthetic", InRequest: true}
				if err := s.SaveMemoryAssessment(d.Scope, a); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return s, d.Scope, pairs
}

// asked is what the report did before it listed the folder: ask memoryAssessment for each quote, newest delivery first, with a broken
// file not assessed. It is the oracle the report is compared with (a quote's assessment, or the error that stops the report).
func asked(s Store, scope string) (map[[2]string]*MemoryAssessment, error) {
	deliveries, err := s.MemoryDeliveries(scope)
	if err != nil {
		return nil, err
	}
	out := map[[2]string]*MemoryAssessment{}
	for i := len(deliveries) - 1; i >= 0; i-- {
		for _, q := range deliveries[i].Quotes {
			a, err := s.memoryAssessment(scope, deliveries[i].ID, q.Key)
			if errors.Is(err, errAssessmentBroken) {
				a, err = nil, nil
			}
			if err != nil {
				return nil, err
			}
			out[[2]string{deliveries[i].ID, q.Key}] = a
		}
	}
	return out, nil
}

// The report opens an assessment file only for a quote whose file is in the folder, not one by one for every quote (a project that
// has been used for a while holds hundreds of deliveries and almost none of them are assessed), and says the same as asking for each
// quote: the assessed ones carry their assessment, the others are counted as pending, none is dropped.
func TestMemoryReportOpensOnlyTheAssessmentsInTheFolder(t *testing.T) {
	s, scope, pairs := reportFixture(t, 40, 5) // 80 quotes, 16 assessed
	old := openInRoot
	t.Cleanup(func() { openInRoot = old })
	opened := 0
	openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
		if strings.HasSuffix(name, ".json") {
			opened++
		}
		return old(r, name, flag, perm)
	}
	r, err := s.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	if opened != 16 {
		t.Fatalf("opened %d assessment files for 80 quotes of which 16 are assessed, want exactly 16", opened)
	}
	want, err := asked(s, scope)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.Delivered != 80 || r.Stats.Helpful != 16 || r.Stats.Pending != 64 || len(r.All) != 80 || len(r.Recent) != 6 {
		t.Fatalf("every quote is counted, 16 helpful and 64 pending: %+v recent %d all %d", r.Stats, len(r.Recent), len(r.All))
	}
	for _, item := range r.All {
		if !reflect.DeepEqual(item.Assessment, want[[2]string{item.SourceID, item.Quote.Key}]) {
			t.Fatalf("%s %s: the report says %+v, asking for the quote says %+v", item.SourceID, item.Quote.Key, item.Assessment, want[[2]string{item.SourceID, item.Quote.Key}])
		}
	}
	assessed := 0
	for _, p := range pairs {
		if want[p] != nil {
			assessed++
		}
	}
	if assessed != 16 {
		t.Fatalf("the fixture assessed %d quotes, want 16", assessed)
	}
}

// What stands where the assessments are changes nothing about what the report says: it says what asking for each quote says, an
// error included. A link or a folder in a file's place and a link in the way are said, never hidden or read; a file that cannot be
// read as an assessment is a quote not yet assessed; a folder that is not there assesses none.
func TestMemoryReportSaysWhatAskingForEachQuoteSays(t *testing.T) {
	modes := []struct {
		name    string
		prepare func(t *testing.T, s Store, scope string, first [2]string)
		wantErr bool // the report must fail (checked besides the comparison)
		wantNil bool // no quote is assessed (checked besides the comparison)
	}{
		{name: "assessed and not", prepare: func(*testing.T, Store, string, [2]string) {}},
		{name: "no folder", wantNil: true, prepare: func(t *testing.T, s Store, scope string, _ [2]string) {
			if err := os.RemoveAll(filepath.Dir(s.memoryAssessmentPath(scope, "0000000000000000", "k"))); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "broken file", prepare: func(t *testing.T, s Store, scope string, first [2]string) {
			if err := os.WriteFile(s.memoryAssessmentPath(scope, first[0], first[1]), []byte("{unfinished"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "oversized file", prepare: func(t *testing.T, s Store, scope string, first [2]string) {
			if err := os.WriteFile(s.memoryAssessmentPath(scope, first[0], first[1]), make([]byte, (32<<10)+1), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a file of another quote under this name", prepare: func(t *testing.T, s Store, scope string, first [2]string) {
			other := s.memoryAssessmentPath(scope, "ffffffffffffffff", "lesson:ffffffffffffffff:ffffffffffffffff")
			if err := os.WriteFile(other, []byte(`{"source_id":"ffffffffffffffff","key":"lesson:ffffffffffffffff:ffffffffffffffff","result":"helpful","decision":"d","evidence":"e","at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(other, s.memoryAssessmentPath(scope, first[0], first[1])); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a link in a file's place", wantErr: true, prepare: func(t *testing.T, s Store, scope string, first [2]string) {
			// the link points at a good assessment of the same quote beside it: reading through it would give an answer
			path := s.memoryAssessmentPath(scope, first[0], first[1])
			data, err := json.Marshal(MemoryAssessment{SourceID: first[0], Key: first[1], Result: "helpful", Decision: "used it", Evidence: "synthetic", At: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(filepath.Dir(path), "target.json")
			if err := os.WriteFile(target, data, 0o600); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(path)
			if err := os.Symlink("target.json", path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a folder in a file's place", wantErr: true, prepare: func(t *testing.T, s Store, scope string, first [2]string) {
			path := s.memoryAssessmentPath(scope, first[0], first[1])
			_ = os.Remove(path)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a link for the project's folder", wantErr: true, prepare: func(t *testing.T, s Store, scope string, _ [2]string) {
			folder := filepath.Dir(s.memoryAssessmentPath(scope, "0000000000000000", "k"))
			outside := filepath.Join(t.TempDir(), "reviews")
			if err := os.Rename(folder, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, folder); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a link for memory-reviews", wantErr: true, prepare: func(t *testing.T, s Store, scope string, _ [2]string) {
			folder := filepath.Dir(filepath.Dir(s.memoryAssessmentPath(scope, "0000000000000000", "k")))
			outside := filepath.Join(t.TempDir(), "reviews")
			if err := os.Rename(folder, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, folder); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a file in the folder's place", wantErr: true, prepare: func(t *testing.T, s Store, scope string, _ [2]string) {
			folder := filepath.Dir(s.memoryAssessmentPath(scope, "0000000000000000", "k"))
			if err := os.RemoveAll(folder); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(folder, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a folder that cannot be opened", wantErr: os.Geteuid() != 0, prepare: func(t *testing.T, s Store, scope string, _ [2]string) {
			folder := filepath.Dir(s.memoryAssessmentPath(scope, "0000000000000000", "k"))
			if err := os.Chmod(folder, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
		}},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			s, scope, pairs := reportFixture(t, 6, 3)
			m.prepare(t, s, scope, pairs[0])
			want, wantErr := asked(s, scope)
			r, err := s.MemoryReport(scope)
			if (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
				t.Fatalf("the report says %v, asking for each quote says %v", err, wantErr)
			}
			if err != nil {
				if !m.wantErr {
					t.Fatalf("a report that asking for each quote makes without an error stopped: %v", err)
				}
				return
			}
			if m.wantErr {
				t.Fatal("an error that asking for each quote gives was hidden")
			}
			if len(r.All) != len(pairs) {
				t.Fatalf("every quote is counted: %d of %d", len(r.All), len(pairs))
			}
			assessed := 0
			for _, item := range r.All {
				w := want[[2]string{item.SourceID, item.Quote.Key}]
				if !reflect.DeepEqual(item.Assessment, w) {
					t.Fatalf("%s %s: the report says %+v, asking for the quote says %+v", item.SourceID, item.Quote.Key, item.Assessment, w)
				}
				if item.Assessment != nil {
					assessed++
				}
			}
			if m.wantNil && assessed != 0 {
				t.Fatalf("no folder, but %d quotes are assessed", assessed)
			}
		})
	}
}

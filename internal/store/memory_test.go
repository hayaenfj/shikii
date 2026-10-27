package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func memoryStoreFixture(t *testing.T) (Store, MemoryDelivery, MemoryAssessment) {
	t.Helper()
	s := Store{Dir: t.TempDir()}
	key := "lesson:0000000000000001:0000000000000002"
	d := MemoryDelivery{ID: "0000000000000003", Scope: "/synthetic/project", Request: "synthetic request", Quotes: []MemoryQuote{{Key: key, ID: "0000000000000001", Revision: "0000000000000002", Lesson: "synthetic lesson", AppliesTo: "synthetic scene", Sources: []string{"0000000000000004"}}}}
	a := MemoryAssessment{SourceID: d.ID, Key: key, Result: "unknown", Evidence: "no record of the decision that used it"}
	return s, d, a
}

func TestMemoryStoreSerializesRetriesAndPreservesTheFirstObservation(t *testing.T) {
	s, d, a := memoryStoreFixture(t)
	if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: "synthetic request", Summary: "synthetic reply", Reply: "next request"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := s.SaveMemoryDelivery(d); err != nil {
				t.Error(err)
				return
			}
			if err := s.SaveMemoryAssessment(d.Scope, a); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	data, err := os.ReadFile(s.memoryPath(d.Scope))
	if err != nil || strings.Count(string(data), "\n") != 1 {
		t.Fatal("parallel resends do not duplicate the delivery")
	}
	d.Quotes[0].Lesson = "changed to another quote later"
	if s.SaveMemoryDelivery(d) == nil {
		t.Fatal("the quote at delivery time is not overwritten")
	}
	a.Result, a.Decision = "helpful", "treated as a success later"
	if s.SaveMemoryAssessment(d.Scope, a) == nil {
		t.Fatal("concurrent runs do not overwrite the original evaluation")
	}
	r, err := s.MemoryReport(d.Scope)
	if err != nil || r.Stats.Delivered != 1 || r.Stats.Unknown != 1 || r.Stats.Helpful != 0 {
		t.Fatalf("the category totals are not duplicated either: %+v %v", r.Stats, err)
	}
}

func TestMemoryStoreBoundsHistoryAndRejectsLinkedRecords(t *testing.T) {
	t.Run("history", func(t *testing.T) {
		s, d, _ := memoryStoreFixture(t)
		for n := 0; n < feedbackRecent+3; n++ {
			d.ID = fmt.Sprintf("%016x", n+100)
			if err := s.SaveMemoryDelivery(d); err != nil {
				t.Fatal(err)
			}
		}
		f, err := os.OpenFile(s.memoryPath(d.Scope), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("{broken line}\n{\"half-written\":")
		_ = f.Close()
		r, err := s.MemoryReport(d.Scope)
		if err != nil || r.Stats.Delivered != feedbackRecent || r.Stats.Pending != feedbackRecent || r.Stats.Helpful != 0 || r.All[len(r.All)-1].SourceID != fmt.Sprintf("%016x", 103) {
			t.Fatalf("counts the latest unevaluated ones too, and does not use broken lines: %+v %v", r.Stats, err)
		}
	})
	for _, part := range []string{"delivery", "assessment", "parent"} {
		t.Run(part, func(t *testing.T) {
			s, d, a := memoryStoreFixture(t)
			if err := s.SaveMemoryDelivery(d); err != nil {
				t.Fatal(err)
			}
			if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: d.Request, Summary: "synthetic reply", Reply: "next request"}); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			marker := filepath.Join(outside, "marker")
			path, target := s.memoryAssessmentPath(d.Scope, d.ID, a.Key), marker
			a.At = time.Now().UTC()
			original, _ := json.Marshal(a)
			if part == "delivery" {
				path = s.memoryPath(d.Scope)
				original, _ = os.ReadFile(path)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if part == "parent" {
				marker = filepath.Join(outside, filepath.Base(path))
				path, target = filepath.Dir(path), outside
			}
			if err := os.WriteFile(marker, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveMemoryAssessment(d.Scope, a); err == nil {
				t.Fatal("links for deliveries or evaluations are not used for saving")
			}
			if _, err := s.MemoryReport(d.Scope); err == nil {
				t.Fatal("unreadable records are not hidden as a zero-item result")
			}
			data, _ := os.ReadFile(marker)
			if string(data) != string(original) {
				t.Fatal("outside files are not rewritten")
			}
		})
	}
}

// 0.9.5 check: after moving the project folder the how-to renames records/memories/<sha256(old)>.jsonl to the new name, but each line still
// names the old path, and reading dropped every such line: the reflection asked to assess nothing and the effect counts went to zero.
// The file already belongs to one project by its name, so a line is not dropped for naming an earlier path.
func TestMemoryDeliveriesSurviveAFolderMove(t *testing.T) {
	s, d, _ := memoryStoreFixture(t)
	if err := s.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	moved := "/synthetic/renamed"
	if err := os.Rename(s.memoryPath(d.Scope), s.memoryPath(moved)); err != nil {
		t.Fatal(err)
	}
	got, err := s.MemoryDeliveries(moved)
	if err != nil || len(got) != 1 || got[0].ID != d.ID {
		t.Fatalf("a delivery saved under the previous path is read after the move: %v %+v", err, got)
	}
}

// Behaviour check 0.9.2 re-check (claude-en): the agent assessed a delivered plain lesson (not a correction) with
// correction_result "not_applicable", then "unknown". Both were refused with "behaviour observations can only be attached
// to corrections delivered to this request", which does not name the field, so the agent gave up and the assessment stayed pending.
func TestCorrectionResultOnPlainLessonNamesTheField(t *testing.T) {
	s, d, a := memoryStoreFixture(t)
	if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: "synthetic request", Summary: "synthetic reply", Reply: "next request"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	for _, cr := range []string{"not_applicable", "unknown"} {
		a.CorrectionResult, a.Decision = cr, "ran the check script the lesson describes"
		err := s.SaveMemoryAssessment(d.Scope, a)
		if err == nil {
			continue // accepting it (and dropping the field) also clears the assessment
		}
		if !strings.Contains(err.Error(), "correction_result") {
			t.Fatalf("correction_result=%q on a plain lesson was refused without naming the field: %v", cr, err)
		}
	}
}

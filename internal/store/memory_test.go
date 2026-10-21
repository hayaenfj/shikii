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
	d := MemoryDelivery{ID: "0000000000000003", Scope: "/synthetic/project", Request: "合成の依頼", Quotes: []MemoryQuote{{Key: key, ID: "0000000000000001", Revision: "0000000000000002", Lesson: "合成の教訓", AppliesTo: "合成の場面", Sources: []string{"0000000000000004"}}}}
	a := MemoryAssessment{SourceID: d.ID, Key: key, Result: "unknown", Evidence: "使った判断の記録が無い"}
	return s, d, a
}

func TestMemoryStoreSerializesRetriesAndPreservesTheFirstObservation(t *testing.T) {
	s, d, a := memoryStoreFixture(t)
	if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: "合成の依頼", Summary: "合成の返答", Reply: "次の依頼"}); err != nil {
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
		t.Fatal("並行した再送でも配送を重複させない")
	}
	d.Quotes[0].Lesson = "後から別の引用へ変更"
	if s.SaveMemoryDelivery(d) == nil {
		t.Fatal("配送時の引用を塗り替えない")
	}
	a.Result, a.Decision = "helpful", "後から成功扱い"
	if s.SaveMemoryAssessment(d.Scope, a) == nil {
		t.Fatal("同時実行でも元の評価を塗り替えない")
	}
	r, err := s.MemoryReport(d.Scope)
	if err != nil || r.Stats.Delivered != 1 || r.Stats.Unknown != 1 || r.Stats.Helpful != 0 {
		t.Fatalf("分類の合計も重複しない: %+v %v", r.Stats, err)
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
		_, _ = f.WriteString("{壊れた行}\n{\"書きかけ\":")
		_ = f.Close()
		r, err := s.MemoryReport(d.Scope)
		if err != nil || r.Stats.Delivered != feedbackRecent || r.Stats.Pending != feedbackRecent || r.Stats.Helpful != 0 || r.All[len(r.All)-1].SourceID != fmt.Sprintf("%016x", 103) {
			t.Fatalf("直近の未評価も含めて数え、壊れた行は使わない: %+v %v", r.Stats, err)
		}
	})
	for _, part := range []string{"delivery", "assessment", "parent"} {
		t.Run(part, func(t *testing.T) {
			s, d, a := memoryStoreFixture(t)
			if err := s.SaveMemoryDelivery(d); err != nil {
				t.Fatal(err)
			}
			if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: d.Request, Summary: "合成の返答", Reply: "次の依頼"}); err != nil {
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
				t.Fatal("配送や評価のリンクを保存に使わない")
			}
			if _, err := s.MemoryReport(d.Scope); err == nil {
				t.Fatal("読めない記録をゼロ件の成績として隠さない")
			}
			data, _ := os.ReadFile(marker)
			if string(data) != string(original) {
				t.Fatal("外のファイルを書き換えない")
			}
		})
	}
}

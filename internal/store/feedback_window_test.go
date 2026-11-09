package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

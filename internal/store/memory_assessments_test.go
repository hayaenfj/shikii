package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavedMemoryEvidenceChecksFilesAndDoesNotNeedDeliveryHistory(t *testing.T) {
	for _, mode := range []string{"rotated", "broken", "wrong name", "oversized", "file link", "directory link"} {
		t.Run(mode, func(t *testing.T) {
			s, d, a := memoryStoreFixture(t)
			if err := s.SaveMemoryDelivery(d); err != nil {
				t.Fatal(err)
			}
			if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: d.Request, Summary: "synthetic reply", Reply: "next request"}); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveMemoryAssessment(d.Scope, a); err != nil {
				t.Fatal(err)
			}
			path := s.memoryAssessmentPath(d.Scope, a.SourceID, a.Key)
			want := 1
			switch mode {
			case "rotated":
				if err := os.WriteFile(s.memoryPath(d.Scope), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "broken":
				if err := os.WriteFile(path, []byte("{unfinished"), 0600); err != nil {
					t.Fatal(err)
				}
				want = 0
			case "wrong name":
				if err := os.Rename(path, filepath.Join(filepath.Dir(path), "another.json")); err != nil {
					t.Fatal(err)
				}
				want = 0
			case "oversized":
				if err := os.WriteFile(path, make([]byte, (32<<10)+1), 0600); err != nil {
					t.Fatal(err)
				}
				want = 0
			case "file link":
				outside := filepath.Join(t.TempDir(), "record")
				if err := os.Rename(path, outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "directory link":
				outside := filepath.Join(t.TempDir(), "records")
				if err := os.Rename(filepath.Dir(path), outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.MemoryAssessments(d.Scope)
			if mode == "file link" || mode == "directory link" {
				if err == nil {
					t.Fatal("linked evidence is refused")
				}
			} else if err != nil || len(got) != want {
				t.Fatalf("evidence=%v, error=%v, wanted %d", got, err, want)
			}
		})
	}
}

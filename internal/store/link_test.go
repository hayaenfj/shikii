package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRejectsLinkedJSONLPaths(t *testing.T) {
	for _, kind := range []string{"records", "labels", "feedback", "reviews", "originals"} {
		for _, where := range []string{"file", "directory", "root"} {
			t.Run(kind+"/"+where, func(t *testing.T) {
				a, b := Store{Dir: t.TempDir()}, Store{Dir: t.TempDir()}
				id, scope := "0000000000000001", "synthetic-project"
				inputs := map[string]string{"message": "AUDIT_SYNTHETIC_B_ONLY"}
				record := Record{TraceID: id, SpecID: "intent", SpecHash: "hash", Model: "m", Inputs: inputs, InputsHash: HashInputs(inputs), At: time.Now()}
				label := Label{TraceID: id, SpecID: "intent", SpecHash: "hash", Model: "m", Inputs: inputs, InputsHash: HashInputs(inputs), Truth: "yes", Source: "manual"}
				feedback := Feedback{ID: id, Request: "synthetic request", Summary: "synthetic answer", Reply: "synthetic reply"}
				review := Review{TraceID: id, Spec: "done-as-asked", Scope: scope, Truth: "yes", Reason: "synthetic evidence", InputsHash: HashInputs(inputs), Source: feedback}
				var pathA, pathB string
				var read func() error
				var write func() error
				switch kind {
				case "records":
					if err := b.AppendRecord(record); err != nil {
						t.Fatal(err)
					}
					pathA, pathB = a.recordsPath("intent"), b.recordsPath("intent")
					read = func() error { _, err := a.Records("intent", 0); return err }
					write = func() error { return a.AppendRecord(record) }
				case "labels":
					if err := b.AppendLabel(label); err != nil {
						t.Fatal(err)
					}
					pathA, pathB = a.labelsPath("intent"), b.labelsPath("intent")
					read = func() error { _, err := a.LabeledTraces("intent"); return err }
					write = func() error { return a.AppendLabel(label) }
				case "feedback":
					if err := b.AppendFeedback(scope, feedback); err != nil {
						t.Fatal(err)
					}
					pathA, pathB = a.feedbackPath(scope), b.feedbackPath(scope)
					read = func() error { _, err := a.RecentFeedback(scope); return err }
					write = func() error { return a.AppendFeedback(scope, feedback) }
				case "originals":
					original := map[string]string{"message": inputs["message"] + strings.Repeat("長い原文。", 200)}
					if err := b.SaveRecordText(record, original); err != nil {
						t.Fatal(err)
					}
					pathA, _ = a.recordTextPath(record)
					pathB, _ = b.recordTextPath(record)
					read = func() error {
						originals, _ := a.RecordText(record)
						if len(originals) == 0 {
							return errors.New("原文を読めません")
						}
						return nil
					}
					write = func() error { return a.SaveRecordText(record, original) }
				case "reviews":
					if _, err := b.SaveReview(review); err != nil {
						t.Fatal(err)
					}
					pathA, pathB = filepath.Join(a.Dir, "records/reviews/done-as-asked.jsonl"), filepath.Join(b.Dir, "records/reviews/done-as-asked.jsonl")
					read = func() error { _, err := a.Reviews(review.Spec); return err }
					write = func() error { review.TraceID = "0000000000000002"; _, err := a.SaveReview(review); return err }
				}
				before, err := os.ReadFile(pathB)
				if err != nil {
					t.Fatal(err)
				}
				linkA, linkB := pathA, pathB
				if where == "directory" {
					linkA, linkB = filepath.Dir(pathA), filepath.Dir(pathB)
				}
				if where == "root" {
					linkA, linkB = a.Dir, b.Dir
					if err := os.RemoveAll(a.Dir); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.MkdirAll(filepath.Dir(linkA), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(linkB, linkA); err != nil {
					t.Fatal(err)
				}
				if err := read(); err == nil {
					t.Error("リンク経由で他の保存先を読まない")
				}
				if err := write(); err == nil {
					t.Error("リンク経由で他の保存先へ書かない")
				}
				after, err := os.ReadFile(pathB)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("リンク先を変更しない")
				}
			})
		}
	}
}

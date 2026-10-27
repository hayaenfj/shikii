package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRecordTextPreservesOriginalAndDecisionBinding(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	input := strings.Repeat("あ", 500)
	rec := Record{TraceID: "0000000000000001", SpecID: "done-as-asked", Inputs: map[string]string{"summary": input}, InputsHash: HashInputs(map[string]string{"summary": input})}
	if _, status := st.RecordText(rec); status["summary"] != "legacy" {
		t.Fatal("text near the old storage limit is shown as possibly missing its continuation")
	}
	full := input + "\nここまでが原文の末尾です"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.SaveRecordText(rec, map[string]string{"summary": full}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	original, status := st.RecordText(rec)
	if original["summary"] != full || len(status) != 0 {
		t.Fatal("reads the original text to its end and drops the storage-limit notice")
	}
	if err := st.SaveRecordText(rec, map[string]string{"summary": full + "別の続き"}); err == nil {
		t.Fatal("saved original text is not overwritten later")
	}
	path, _ := st.recordTextPath(rec)
	data, _ := os.ReadFile(path)
	var changed recordText
	_ = json.Unmarshal(data, &changed)
	changed.InputsHash = "別の入力"
	broken, _ := json.Marshal(changed)
	_ = os.WriteFile(path, broken, 0o600)
	if original, status := st.RecordText(rec); len(original) != 0 || status["summary"] != "unavailable" {
		t.Fatal("original text of a different decision is not shown")
	}
	_ = os.WriteFile(path, data, 0o600)
	changed.InputsHash = rec.InputsHash
	changed.Inputs["summary"] = "別の冒頭"
	broken, _ = json.Marshal(changed)
	_ = os.WriteFile(path, broken, 0o600)
	if original, _ := st.RecordText(rec); len(original) != 0 {
		t.Fatal("text whose opening differs is not used even if only the hash matches")
	}
	_ = os.WriteFile(path, data, 0o600)
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatal("the conversation's original text is readable only by its owner")
	}
	long := rec
	long.TraceID = "0000000000000002"
	if err := st.SaveRecordText(long, map[string]string{"summary": strings.Repeat("あ", 30000)}); err != nil {
		t.Fatal(err)
	}
	original, status = st.RecordText(long)
	if len(original["summary"]) > RecordTextLimit || !utf8.ValidString(original["summary"]) || status["summary"] != "limited" {
		t.Fatal("keeps the long-text storage limit and the UTF-8 boundary, and shows it was cut")
	}
	outside := t.TempDir()
	linkStore := Store{Dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(linkStore.Dir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(linkStore.Dir, "records", "text")); err != nil {
		t.Fatal(err)
	}
	if err := linkStore.SaveRecordText(rec, map[string]string{"summary": full}); err == nil {
		t.Fatal("does not follow a symlink at the original text's location")
	}
}

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
							return errors.New("cannot read the original text")
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
					t.Error("does not read another storage location through a link")
				}
				if err := write(); err == nil {
					t.Error("does not write to another storage location through a link")
				}
				after, err := os.ReadFile(pathB)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("does not change the link target")
				}
			})
		}
	}
}

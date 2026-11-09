package store

import (
	"bytes"
	"os"
	"testing"
)

func TestFeedbackReadChecksBytesAndCopiesTargets(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 1)
	if err := s.AppendFeedback(scope, Feedback{ID: "target", Request: "request", Summary: "summary", Reply: "reply", Targets: []ReviewTarget{{Spec: "spec", TraceID: "trace"}}}); err != nil {
		t.Fatal(err)
	}
	view, err := s.ReadFeedback(scope)
	if err != nil {
		t.Fatal(err)
	}
	first := view.Items()
	first[0].Targets[0].TraceID = "changed"
	if view.Items()[0].Targets[0].TraceID != "trace" {
		t.Fatal("shared feedback targets must not alias a caller")
	}
	check, err := view.CurrentHash(make([]byte, 32<<10))
	if err != nil || check != view.Hash() {
		t.Fatal(check, err)
	}
	path := s.feedbackPath(scope)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, bytes.Replace(raw, []byte("summary"), []byte("changed"), 1), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	check, err = view.CurrentHash(make([]byte, 32<<10))
	if err != nil || check == view.Hash() {
		t.Fatal("same-size/time changes must be found", check, err)
	}
}

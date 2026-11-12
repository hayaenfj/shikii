package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func reflectionHistoryFixture(t *testing.T, count int) (notebook.Book, store.Store) {
	t.Helper()
	dir := t.TempDir()
	book := notebook.Book{Root: filepath.Join(dir, ".shikii"), Scope: dir}
	st := store.Store{Dir: book.Root}
	if _, err := book.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic history", Content: "Synthetic only.", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(book.Root, "records", "feedback", store.StoreName(book.Root, book.Scope)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range count {
		raw, _ := json.Marshal(store.Feedback{ID: fmt.Sprintf("%016x", i+1), Request: "Synthetic request.", Summary: "Synthetic summary.", Reply: "Synthetic reply.", At: time.Unix(int64(i+1), 0)})
		if _, err = f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return book, st
}

func TestReflectReadRejectsChangedHistoryBeforeCommit(t *testing.T) {
	book, st := reflectionHistoryFixture(t, 5)
	view := book.ReadSnapshot()
	if _, err := reflectQueryBook(view, reflectHeadsMax); err != nil {
		t.Fatal(err)
	}
	if err := view.ValidateRead(); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendFeedback(book.Scope, store.Feedback{ID: "0000000000000100", Request: "New request.", Summary: "New summary.", Reply: "New reply."}); err != nil {
		t.Fatal(err)
	}
	if err := view.ValidateRead(); err == nil {
		t.Fatal("a prepared reflection must not save after its conversation history changes")
	}
}

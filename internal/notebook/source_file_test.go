package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOutcomeSourcesRejectNonregularFiles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/synthetic/audit"}
	captured, err := b.Capture(Observation{Kind: "test", Title: "Synthetic check", Content: "Synthetic tenant check passed.", Reference: "isolated audit", Version: "candidate", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.OutcomeSources([]string{captured.ID}); err != nil {
		t.Fatal("regular source control:", err)
	}
	dir, err := b.directory(false, "sources")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, captured.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if json.Unmarshal(data, &shape) != nil {
		t.Fatal("invalid fixture")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	// A writer is already present in this control, so refusal must come from
	// descriptor type validation rather than an empty read or a timeout.
	anchor, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f, openErr := openSourceFile(path)
	if f != nil {
		f.Close()
	}
	if err = anchor.Close(); err != nil {
		t.Fatal(err)
	}
	if openErr == nil {
		t.Error("the source descriptor boundary accepted a named pipe")
	}
	type readResult struct {
		refs []Source
		err  error
	}
	done := make(chan readResult, 1)
	go func() { refs, e := b.OutcomeSources([]string{captured.ID}); done <- readResult{refs, e} }()
	select {
	case result := <-done:
		if result.err == nil {
			t.Fatal("named pipe accepted")
		}
		return
	case <-time.After(100 * time.Millisecond):
	}
	// A nonblocking read/write anchor releases an old blocked reader without
	// risking a blocked cleanup writer if the rejecting implementation wins.
	anchor, e := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, writeErr := anchor.Write(data)
	closeErr := anchor.Close()
	result := <-done
	if writeErr != nil || closeErr != nil {
		t.Fatal(writeErr, closeErr)
	}
	t.Logf("fifo_waited_for_writer=true fifo_accepted=%t evidence_references=%d", result.err == nil, len(result.refs))
	t.Fatal("outcome source waited for a named-pipe writer instead of rejecting a nonregular source")
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// `shikii gc` over every question also sweeps the original texts left without a decision, and says so: a dry run counts them,
// --apply removes them, and a line that names no decision keeps them and is reported.
func TestGCCommandSweepsTextsLeftWithoutADecision(t *testing.T) {
	dir := t.TempDir()
	st := store.Store{Dir: dir}
	old := time.Now().Add(-48 * time.Hour)
	inputs := map[string]string{"request": "short opening"}
	rec := store.Record{TraceID: "aaaaaaaaaaaaaaaa", SpecID: "prompt-kind", SpecHash: "h", Type: "noul", Model: "m", Inputs: inputs, InputsHash: store.HashInputs(inputs), Raw: map[string]float64{"yes": .6, "no": .4}, P: .6, Band: "ask", At: old}
	if err := st.SaveRecordText(rec, map[string]string{"request": "short opening and the long original"}); err != nil {
		t.Fatal(err)
	}
	text := filepath.Join(dir, "records", "text", "aaaaaaaaaaaaaaaa.json")
	if err := os.Chtimes(text, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "records", "needs-plan.jsonl"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdGC([]string{"--store", dir, "--older-than", "1d", "--apply"}) })
	if !strings.Contains(out, "original texts: none removed") || !strings.Contains(out, "names no decision") {
		t.Fatalf("a line without a trace keeps the texts and is reported:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(dir, "records", "needs-plan.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdGC([]string{"--store", dir, "--older-than", "1d"}) })
	if !strings.Contains(out, "original texts: would remove 1 left without a decision") {
		t.Fatalf("a dry run counts the text left without a decision:\n%s", out)
	}
	out = capture(t, func() error { return cmdGC([]string{"--store", dir, "--older-than", "1d", "--apply"}) })
	if !strings.Contains(out, "original texts: removed 1 left without a decision") {
		t.Fatalf("--apply removes it:\n%s", out)
	}
	if _, err := os.Stat(text); !os.IsNotExist(err) {
		t.Errorf("the text is gone: %v", err)
	}
}

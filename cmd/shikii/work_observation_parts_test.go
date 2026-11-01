package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAudit019ObservationContinuationContract(t *testing.T) {
	dir := t.TempDir()
	b := notebook.Book{Root: filepath.Join(dir, ".shikii"), Scope: dir}
	content := strings.Repeat("<", 48<<10) + "AUDIT_END"
	observation, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic large escaped source", Content: content, Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	value, err := runWork(b.Root, b.Scope, "prepare", workRequest{Query: "large escaped source"})
	if err != nil {
		t.Fatal(err)
	}
	prepared := loopObject(t, value)
	entries := prepared["observations"].([]any)
	if len(entries) != 1 {
		t.Fatal(len(entries))
	}
	entry := entries[0].(map[string]any)
	if entry["incomplete"] != true || entry["ref"] != nil {
		t.Fatal(entry)
	}
	page := entry["source"].(map[string]any)
	joined := ""
	pages := 0
	for {
		if page["observation_part"] == nil {
			t.Fatal("\u5206\u5272\u306e\u5834\u6240\u304c\u7121\u3044")
		}
		if page["original_part"] != nil {
			t.Fatal("\u89b3\u5bdf\u8cc7\u6599\u306b\u4f1a\u8a71\u7528\u306e\u65ad\u7247\u304c\u6df7\u3056\u3063\u305f")
		}
		for _, o := range page["observations"].([]any) {
			joined += o.(map[string]any)["content"].(string)
		}
		pages++
		cursor, _ := page["next_cursor"].(string)
		if cursor == "" {
			break
		}
		if pages > 20 {
			t.Fatal("\u7d9a\u304d\u304c\u7d42\u308f\u3089\u306a\u3044")
		}
		next, err := runNotebook(b.Root, b.Scope, notebookRequest{Operation: "query", Sources: []string{observation.ID}, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		page = loopObject(t, next)
	}
	if joined != content {
		t.Fatal("\u7d9a\u304d\u304b\u3089\u672c\u6587\u3092\u5fa9\u5143\u3067\u304d\u306a\u3044")
	}
	notice := entry["next_action"].(string)
	t.Logf("\u5b89\u5168\u306a\u5408\u6210\u8cc7\u6599: %d\u30d0\u30a4\u30c8\u3001%d\u30da\u30fc\u30b8\u3001observation_part\u3068observations[].content\u304b\u3089\u5168\u6587\u5fa9\u5143\u6e08\u307f\u3002\u8fd4\u5374\u6848\u5185\u306f original_part=%t / observation_part=%t", len(content), pages, strings.Contains(notice, "original_part"), strings.Contains(notice, "observation_part"))
	if strings.Contains(notice, "original_part") && !strings.Contains(notice, "observation_part") {
		t.Fatal("\u89b3\u5bdf\u8cc7\u6599\u306e\u7d9a\u304d\u306b\u5b58\u5728\u3057\u306a\u3044original_part\u306e\u9023\u7d50\u3092\u6848\u5185\u3059\u308b")
	}
}

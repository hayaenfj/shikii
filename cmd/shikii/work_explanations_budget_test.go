package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkSharedExplanationTightBudget(t *testing.T) {
	b, source := loopBook(t)
	for i := 0; i < 4; i++ {
		loopPage(t, b, source, map[string]any{"title": fmt.Sprintf("Routing delivery %d", i), "body": strings.Repeat("Routing permits only the synthetic owner under this exact condition.\n\n", 30)})
	}
	fields := map[string]any{"request": "Check routing delivery.", "query": "routing", "max_bytes": 65536, "reasoning": map[string]any{"depth": "brief"}}
	first, err := workCall(t, b, "prepare", fields)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(first)
	fields["max_bytes"] = len(raw)
	tight, err := workCall(t, b, "prepare", fields)
	t.Logf("wide wire=%d; tight budget=%d; tight error=%v", len(raw)+1, len(raw), err)
	if err == nil {
		wire, _ := json.Marshal(tight)
		if len(wire)+1 > len(raw) {
			t.Fatal("successful response exceeds the complete budget", len(wire)+1, len(raw))
		}
		return
	}
	if !strings.Contains(err.Error(), "recovery metadata exceeds max_bytes") {
		t.Fatal(err)
	}
	names, e := os.ReadDir(filepath.Join(b.Root, "sessions"))
	if e != nil {
		t.Fatal(e)
	}
	saved := 0
	for _, name := range names {
		if !strings.HasSuffix(name.Name(), ".json") {
			continue
		}
		data, e := os.ReadFile(filepath.Join(b.Root, "sessions", name.Name()))
		if e != nil {
			t.Fatal(e)
		}
		var session hookSession
		if e = json.Unmarshal(data, &session); e != nil {
			t.Fatal(e)
		}
		if session.Planning != nil && session.Planning.Work != nil {
			quoted := 0
			for key := range session.Planning.Sources {
				if strings.HasPrefix(key, "article:") || strings.HasPrefix(key, "lesson:") {
					quoted++
				}
			}
			t.Logf("saved work quotes=%d excerpt reuse keys=%d", quoted, len(session.Planning.Work.Loop.Excerpts))
			saved++
		}
	}
	t.Logf("saved work sessions after one delivered and one rejected request=%d", saved)
	if saved != 1 {
		t.Fatalf("rejected delivery left a quote/reuse-capable context: sessions=%d", saved)
	}
}

func TestWorkSharedExplanationRejectedExtension(t *testing.T) {
	b, source := loopBook(t)
	for i := 0; i < 4; i++ {
		loopPage(t, b, source, map[string]any{"title": fmt.Sprintf("Routing delivery %d", i), "body": strings.Repeat("Routing permits only the synthetic owner under this exact condition.\n\n", 30)})
	}
	initial := map[string]any{"request": "Check routing delivery. " + strings.Repeat("Synthetic review condition. ", 50), "query": "zzzsynthnone", "max_bytes": 65536, "reasoning": map[string]any{"depth": "brief"}}
	first, e := workCall(t, b, "prepare", initial)
	if e != nil {
		t.Fatal(e)
	}
	second, e := workCall(t, b, "prepare", initial)
	if e != nil {
		t.Fatal(e)
	}
	if len(first["knowledge"].([]any)) != 0 || len(second["knowledge"].([]any)) != 0 {
		t.Fatal("control: initial receipts must not have excerpts")
	}
	wide, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "routing", "max_bytes": 65536})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(wide)
	tight, e := workCall(t, b, "prepare", map[string]any{"receipt": second["receipt"], "query": "routing", "max_bytes": len(raw)})
	t.Logf("extension wide wire=%d tight budget=%d rejected=%v", len(raw)+1, len(raw), e)
	if e == nil {
		wire, _ := json.Marshal(tight)
		if len(wire)+1 > len(raw) {
			t.Fatal("successful extension exceeds the complete budget", len(wire)+1, len(raw))
		}
		for _, raw := range tight["knowledge"].([]any) {
			entry := raw.(map[string]any)
			if entry["reused"] == true {
				t.Fatal("first delivery was treated as reused")
			}
		}
		return
	}
	if !strings.Contains(e.Error(), "recovery metadata exceeds max_bytes") {
		t.Fatal(e)
	}
	retry, e := workCall(t, b, "prepare", map[string]any{"receipt": second["receipt"], "query": "routing", "max_bytes": 65536})
	if e != nil {
		t.Fatal(e)
	}
	for _, raw := range retry["knowledge"].([]any) {
		entry := raw.(map[string]any)
		t.Logf("retry reused=%v content=%v body_excerpts=%v text=%v", entry["reused"], entry["reused_content"], entry["body_excerpts"] != nil, entry["text"] != nil)
		if entry["reused"] == true {
			t.Error("rejected excerpts were reused without ever being delivered")
		}
	}
}

package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func collectiveNoticeMap(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(reply["collective"])
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestWorkNoticeDeliveryRetainsEvidenceAndRecovery(t *testing.T) {
	first, second, third := deltaPrepare(t)
	initial := collectiveNoticeMap(t, first)
	for _, reply := range []map[string]any{second, third} {
		view := collectiveNoticeMap(t, reply)
		if view["notice_from"] != first["delivery"] {
			t.Fatalf("reuse only the notice actually delivered under this receipt: %v", view)
		}
		text, _ := view["notice"].(string)
		if !strings.Contains(text, "collective.notice") || !strings.Contains(text, "reasoning:{depth:focused}") {
			t.Fatal("the reference must name the field and an existing full-recovery path", text)
		}
		baseline := map[string]any{}
		for key, value := range view {
			baseline[key] = value
		}
		delete(baseline, "notice_from")
		baseline["notice"] = initial["notice"]
		if !reflect.DeepEqual(baseline, initial) {
			t.Fatal("notice sharing must retain every update, contrast, investigation and recovery field")
		}
		if workContextSize(view) >= workContextSize(baseline) {
			t.Fatal("the complete notice representation must shrink")
		}
		t.Logf("collective UTF-8 JSON bytes: %d -> %d", workContextSize(baseline), workContextSize(view))
		wire, _ := json.Marshal(reply)
		mcp := markdownMCPCall(t, "shikii_wiki", 200, wire)
		text = markdownMCPText(t, mcp)
		var restored any
		if strings.HasPrefix(text, "# Knowledge\n") {
			restored = restoreMarkdownMCP(t, text)
		} else {
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if err := decoder.Decode(&restored); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(restored, mcp["structuredContent"]) {
			t.Fatal("MCP text must retain every notice reference and recovery field")
		}
	}
}

func TestWorkNoticeDeliveryFailedPreparationKeepsReference(t *testing.T) {
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Notice routing", "body": strings.Repeat("Routing keeps conditions and exceptions.\n\n", 100)})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check notice routing.", "query": "Notice routing"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = prepareWork(b.Root, b.Scope, workRequest{Receipt: first["receipt"].(string), Query: "Notice routing", MaxBytes: 8192, responseSize: func(any) int { return 1 << 20 }})
	if err == nil {
		t.Fatal("control: the transport must exceed the response budget")
	}
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Notice routing"})
	if err != nil {
		t.Fatal(err)
	}
	if next["delivery"] != first["delivery"].(float64)+1 || collectiveNoticeMap(t, next)["notice_from"] != first["delivery"] {
		t.Fatal("a rejected preparation must not publish a new notice or advance its delivery")
	}
}

func TestWorkNoticeDeliveryFullRecoveryAndReceiptIsolation(t *testing.T) {
	b, source := loopBook(t)
	page := loopPage(t, b, source, map[string]any{"title": "Notice routing", "body": strings.Repeat("Routing preserves the owner condition and every exception.\n\n", 100)})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check notice routing; never publish.", "query": "Notice routing"})
	if err != nil {
		t.Fatal(err)
	}
	original := collectiveNoticeMap(t, first)["notice"]
	for _, fields := range []map[string]any{
		{"receipt": first["receipt"], "query": "Notice routing"},
		{"receipt": first["receipt"], "query": "Notice routing", "reasoning": map[string]any{"depth": "focused"}},
		{"receipt": first["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision},
		{"request": "Check another notice routing decision.", "query": "Notice routing"},
	} {
		reply, err := workCall(t, b, "prepare", fields)
		if err != nil {
			t.Fatal(err)
		}
		view := collectiveNoticeMap(t, reply)
		if fields["reasoning"] == nil && fields["receipt"] != nil && fields["query"] == "Notice routing" {
			if view["notice_from"] == nil {
				t.Fatal("control: the repeated broad read should share its fixed notice")
			}
		} else if view["notice_from"] != nil || view["notice"] != original {
			t.Fatal("explicit recovery and a new receipt must return the full original notice", fields, view)
		}
	}
}

func TestWorkNoticeDeliveryChecksExactTextAndWholeTransport(t *testing.T) {
	notice := strings.Repeat("Keep the exact condition and its exception; never treat evidence as permission. ", 5)
	for _, mode := range []string{"same", "changed", "not-reused", "transport"} {
		t.Run(mode, func(t *testing.T) {
			w := &workState{Nonce: "synthetic"}
			context := notebook.CollectiveContext{Notice: notice, Omitted: map[string]int{"contrasts": 1}, Recovery: []string{"lesson:0000000000000001@0000000000000002"}}
			first := map[string]any{"knowledge": []any{deltaMetadataFixture()}, "collective": context}
			if _, err := limitWorkPreparation(notebook.Book{}, w, first, map[string]string{}, 65536); err != nil {
				t.Fatal(err)
			}
			rememberWorkMetadata(w, first)
			entry := deltaMetadataFixture()
			if mode == "changed" {
				context.Notice += " New condition: do not reuse the earlier interpretation."
			}
			if mode == "not-reused" {
				delete(entry, "reused")
			}
			reply := map[string]any{"knowledge": []any{entry}, "collective": context}
			size := func(value any) int {
				n := workContextSize(value)
				if mode == "transport" && collectiveNoticeMap(t, value.(map[string]any))["notice_from"] != nil {
					n += 4096
				}
				return n
			}
			if _, err := limitWorkPreparation(notebook.Book{}, w, reply, map[string]string{}, 65536, size); err != nil {
				t.Fatal(err)
			}
			view := collectiveNoticeMap(t, reply)
			if (view["notice_from"] != nil) != (mode == "same") {
				t.Fatal("reuse requires an exact delivered notice, reusable content and cheaper full transport", mode, view)
			}
			if !reflect.DeepEqual(view["omitted"], collectiveNoticeMap(t, first)["omitted"]) || !reflect.DeepEqual(view["recovery"], collectiveNoticeMap(t, first)["recovery"]) {
				t.Fatal("sharing must not hide missing material or recovery")
			}
		})
	}
}

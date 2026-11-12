package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// replyMembers reads one JSON object's members in the order they were written, each with its raw value.
func replyMembers(t *testing.T, raw []byte) (keys []string, values map[string]json.RawMessage) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %v %.80s", err, raw)
	}
	values = map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		values[tok.(string)] = value
	}
	return keys, values
}

// nestedOrderProblem walks every object below the top one and reports the first whose members are not smallest first.
func nestedOrderProblem(t *testing.T, raw []byte, path string, top bool) string {
	t.Helper()
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) > 0 && raw[0] == '{':
		keys, values := replyMembers(t, raw)
		for i, key := range keys {
			if i > 0 && !top {
				previous := keys[i-1]
				if len(values[previous]) > len(values[key]) || (len(values[previous]) == len(values[key]) && previous > key) {
					return fmt.Sprintf("%s: %s (%d bytes) is written before %s (%d bytes)", path, previous, len(values[previous]), key, len(values[key]))
				}
			}
			if problem := nestedOrderProblem(t, values[key], path+"/"+key, false); problem != "" {
				return problem
			}
		}
	case len(raw) > 0 && raw[0] == '[':
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		for i, element := range list {
			if problem := nestedOrderProblem(t, element, fmt.Sprintf("%s/%d", path, i), false); problem != "" {
				return problem
			}
		}
	}
	return ""
}

func decodedReply(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		t.Fatalf("%v: %.200s", err, raw)
	}
	return value
}

// Go writes the keys of a map in alphabetical order, so the reply of work prepare began with agent_supplied_task and attention,
// and receipt (needed by every later call) came after the lessons, the candidates and the pending conversations. A tool that cuts
// a long output in the middle (measured on 2026-11-11: about a fifth of the prepare replies an agent read directly) left the agent
// without the receipt, and inside each lesson the title and the summary came after the long orientation. A reply now starts with
// what the agent needs to act (receipt, complete, errors, what to do next), then the lessons, then the rest from small to large;
// inside it, every object is written from small to large. The values and the number of bytes are exactly those of json.Marshal.
func TestRepliesStartWithWhatTheAgentNeedsToAct(t *testing.T) {
	value := map[string]any{
		"agent_supplied_task": true, "attention": map[string]any{"depth": "focused", "reasons": []any{"a long reason to make this the larger member"}},
		"knowledge":         []any{map[string]any{"applies_to": "CSV export", "orientation": map[string]any{"summary": strings.Repeat("long ", 40), "id": "0123456789abcdef"}, "ref": "knowledge1", "title": "Keep <filters> & \"quotes\"", "summary": "Keep the filters.", "body_included": false}},
		"knowledge_context": map[string]any{"search": map[string]any{"candidates": []any{map[string]any{"title": strings.Repeat("candidate ", 30), "id": "fedcba9876543210"}}, "total": json.Number("12345678901234567890")}},
		"how_to_record":     "Record once with this receipt.", "receipt": "eyJzIjoi", "task": "Check the CSV export \u65e5\u672c\u8a9e", "pending": []any{}, "planning_required": false,
		"reasoning": map[string]any{"required": false, "started": true}, "delivery": json.Number("1"), "empty": map[string]any{}, "nothing": nil, "ratio": json.Number("0.25"),
	}
	got, err := orderedReply(value)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(value)
	if !reflect.DeepEqual(decodedReply(t, got), decodedReply(t, plain)) {
		t.Errorf("the ordered reply holds other values:\n%s\n%s", got, plain)
	}
	if len(got) != len(plain) {
		t.Errorf("the ordered reply is %d bytes, json.Marshal %d: the response budget counts the same bytes", len(got), len(plain))
	}
	keys, _ := replyMembers(t, got)
	want := []string{"receipt", "task", "how_to_record", "planning_required", "reasoning", "knowledge", "pending", // what the agent needs to act, then the lessons
		"delivery", "empty", "agent_supplied_task", "nothing", "ratio", "attention", "knowledge_context"} // the rest, small to large (1, 2, 4, 4, 4 bytes: the same size goes by name)
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("the top of the reply is written in the order\n%v, want\n%v", keys, want)
	}
	if problem := nestedOrderProblem(t, got, "", true); problem != "" {
		t.Errorf("an object inside the reply is not written from small to large: %s", problem)
	}
	record, err := orderedReply(workResult{Complete: false, Saved: []workItem{{Ref: "result", Kind: "result"}}, Errors: []workItem{{Ref: "note1", Kind: "note", Error: "x"}}, NextAction: "Resend note1.", Storage: map[string]any{"status": "saved"}})
	if err != nil {
		t.Fatal(err)
	}
	if keys, _ := replyMembers(t, record); len(keys) < 3 || !reflect.DeepEqual(keys[:3], []string{"complete", "errors", "next_action"}) {
		t.Errorf("the reply of record starts with whether it is complete, the errors and the next action: %v", keys)
	}
	for _, scalar := range []any{nil, "text", json.Number("7"), []any{}, []any{map[string]any{"b": "long value", "a": true}}} {
		got, err := orderedReply(scalar)
		plain, _ := json.Marshal(scalar)
		if err != nil || !reflect.DeepEqual(decodedReply(t, got), decodedReply(t, plain)) || len(got) != len(plain) {
			t.Errorf("a reply that is not an object keeps its value: %s %s %v", got, plain, err)
		}
	}

	// through the real entries: the CLI, the HTTP endpoint and the MCP text
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("The export keeps the filters in every format. ", 20)
	if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
		t.Fatal(err)
	}
	startsWith := func(name, text string, first ...string) {
		t.Helper()
		keys, values := replyMembers(t, []byte(text))
		if len(keys) < len(first) || !reflect.DeepEqual(keys[:len(first)], first) {
			t.Errorf("%s starts with %v, want %v", name, keys[:min(len(keys), 6)], first)
		}
		if problem := nestedOrderProblem(t, []byte(text), "", true); problem != "" {
			t.Errorf("%s: an object inside the reply is not written from small to large: %s", name, problem)
		}
		if lessons, ok := values["knowledge"]; ok {
			if context, ok := values["knowledge_context"]; ok && strings.Index(text, `"knowledge":`) > strings.Index(text, `"knowledge_context":`) {
				t.Errorf("%s writes the candidates (%d bytes) before the lessons (%d bytes)", name, len(context), len(lessons))
			}
		}
	}
	var cli string
	withStdin(t, `{"query":"CSV export filters","request":"Check the CSV export"}`, func() { cli = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
	startsWith("the CLI reply of work prepare", cli, "receipt", "task", "how_to_record", "planning_required", "reasoning", "knowledge")
	var prepared map[string]any
	if err := json.Unmarshal([]byte(cli), &prepared); err != nil {
		t.Fatal(err)
	}
	record1, _ := json.Marshal(map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Nothing.", "checked": "Read the export."}})
	withStdin(t, string(record1), func() { cli = capture(t, func() error { return cmdWork([]string{"record"}) }) })
	startsWith("the CLI reply of work record", cli, "complete")

	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	for _, arguments := range []string{`{"operation":"prepare","work":{"query":"CSV export filters","request":"Check the CSV export"}}`} {
		code, httpBody, err := callTool(h, "shikii_wiki", []byte(arguments))
		if err != nil || code != 200 {
			t.Fatal(code, string(httpBody), err)
		}
		startsWith("the HTTP reply of work prepare", string(httpBody), "receipt", "task", "how_to_record", "planning_required", "reasoning", "knowledge")
		result := toolResult(code, httpBody, "shikii_wiki")
		text := result["content"].([]map[string]any)[0]["text"].(string)
		if strings.HasPrefix(text, "{") {
			startsWith("the MCP text of work prepare", text, "receipt", "task", "how_to_record", "planning_required", "reasoning", "knowledge")
		} else if !strings.Contains(text, "```json\n{\"receipt\":") {
			t.Errorf("the metadata of the MCP Markdown text starts with receipt: %.300s", text)
		}
		structured, _ := json.Marshal(result["structuredContent"])
		if !reflect.DeepEqual(decodedReply(t, structured), decodedReply(t, httpBody)) {
			t.Errorf("structuredContent keeps every value of the reply")
		}
	}
}

// The Markdown text the MCP server chooses when long strings repeat keeps the same order in its metadata.
func TestMCPMarkdownMetadataStartsWithWhatTheAgentNeedsToAct(t *testing.T) {
	long := strings.Repeat("The export keeps the filters in every format. ", 20)
	raw, _ := json.Marshal(map[string]any{"attention": map[string]any{"depth": "focused"}, "receipt": "eyJzIjoi", "task": "Check the CSV export",
		"knowledge": []any{map[string]any{"ref": "knowledge1", "body": long, "excerpt": long, "summary": long}}})
	text := toolResult(200, raw, "shikii_wiki")["content"].([]map[string]any)[0]["text"].(string)
	if !strings.HasPrefix(text, "# Knowledge") {
		t.Fatalf("repeated long strings choose the Markdown text: %.200s", text)
	}
	if !strings.Contains(text, "```json\n{\"receipt\":\"eyJzIjoi\",\"task\":\"Check the CSV export\",\"knowledge\":[{") {
		t.Errorf("the metadata of the Markdown text starts with receipt, task and the lessons: %.400s", text)
	}
}

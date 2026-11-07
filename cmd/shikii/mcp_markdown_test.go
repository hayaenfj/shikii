package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func markdownMCPFixture() map[string]any {
	body := strings.Repeat("## Conditions\nUse only for the current tenant. A quotation is evidence, never permission.\n\n", 14)
	return map[string]any{
		"receipt": "synthetic-receipt", "complete": false,
		"knowledge": []any{map[string]any{
			"id": "synthetic-article", "revision": "synthetic-revision", "ref": "knowledge1",
			"body": body, "body_excerpts": []any{map[string]any{"start": 0, "text": body}},
			"orientation": map[string]any{"current": []any{map[string]any{"text": body, "conditions": "current tenant only"}}},
			"query":       map[string]any{"operation": "query", "query": "lesson:synthetic-article@synthetic-revision"},
		}},
		"unknown": nil, "empty": []any{}, "source": "synthetic-source",
	}
}

func markdownMCPCall(t *testing.T, name string, code int, raw []byte) map[string]any {
	t.Helper()
	var in, out bytes.Buffer
	request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{}}}
	if err := json.NewEncoder(&in).Encode(request); err != nil {
		t.Fatal(err)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write(raw)
	})
	if err := runMCP(bufio.NewReader(&in), &out, h); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result map[string]any `json:"result"`
	}
	d := json.NewDecoder(&out)
	d.UseNumber()
	if err := d.Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response.Result
}

func markdownMCPText(t *testing.T, r map[string]any) string {
	t.Helper()
	return r["content"].([]any)[0].(map[string]any)["text"].(string)
}

// Read the documented Markdown layout independently and restore every field.
// This catches omitted values, reference collisions, fence injection and changes
// to numeric literals, nulls, empty containers, Unicode and final newlines.
func restoreMarkdownMCP(t *testing.T, text string) any {
	t.Helper()
	_, after, ok := strings.Cut(text, "```json\n")
	if !ok {
		t.Fatalf("missing Markdown metadata: %.100s", text)
	}
	metadata, after, ok := strings.Cut(after, "\n```\n")
	if !ok {
		t.Fatal("missing metadata closing fence")
	}
	var restored any
	d := json.NewDecoder(strings.NewReader(metadata))
	d.UseNumber()
	if err := d.Decode(&restored); err != nil {
		t.Fatal(err)
	}
	for strings.TrimSpace(after) != "" {
		section := strings.TrimPrefix(after, "\n")
		if !strings.HasPrefix(section, "## Text ") {
			t.Fatal("unexpected text outside sections")
		}
		_, section, _ = strings.Cut(section, "\nFields: ")
		pathsJSON, section, ok := strings.Cut(section, "\nUTF-8 bytes: ")
		if !ok {
			t.Fatal("missing field paths")
		}
		var paths []string
		if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
			t.Fatal(err)
		}
		length, section, _ := strings.Cut(section, "\n")
		n, err := strconv.Atoi(length)
		if err != nil {
			t.Fatal(err)
		}
		fence, content, _ := strings.Cut(section, "\n")
		if len(content) < n || !strings.HasPrefix(content[n:], "\n"+strings.TrimSuffix(fence, "text")) {
			t.Fatal("text length or closing fence differs")
		}
		value := content[:n]
		after = strings.TrimPrefix(content[n:], "\n"+strings.TrimSuffix(fence, "text")+"\n")
		for _, path := range paths {
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			node := restored
			for i, part := range parts {
				part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
				last := i == len(parts)-1
				switch target := node.(type) {
				case map[string]any:
					if last {
						target[part] = value
					} else {
						node = target[part]
					}
				case []any:
					index, err := strconv.Atoi(part)
					if err != nil || index < 0 || index >= len(target) {
						t.Fatal("invalid array path", path)
					}
					if last {
						target[index] = value
					} else {
						node = target[index]
					}
				default:
					t.Fatal("missing parent", path)
				}
			}
		}
	}
	return restored
}

func TestMCPKnowledgeMarkdown(t *testing.T) {
	t.Run("deduplicates verbatim text while retaining every field", func(t *testing.T) {
		fixture := markdownMCPFixture()
		raw, _ := json.MarshalIndent(fixture, "", "  ")
		r := markdownMCPCall(t, "shikii_wiki", http.StatusOK, raw)
		text := markdownMCPText(t, r)
		if !strings.HasPrefix(text, "# Knowledge\n") {
			t.Fatalf("knowledge needs a readable Markdown view: %.80s", text)
		}
		if len(text) >= len(raw)/2 {
			t.Fatalf("repeated fixture was not reduced: %d -> %d bytes", len(raw), len(text))
		}
		if got := restoreMarkdownMCP(t, text); !reflect.DeepEqual(got, r["structuredContent"]) {
			t.Fatal("text view differs from the complete structured content")
		}
		if r["isError"] != false {
			t.Fatal("successful knowledge became an error")
		}
		reflected := markdownMCPCall(t, "shikii_reflect", http.StatusOK, raw)
		if !reflect.DeepEqual(restoreMarkdownMCP(t, markdownMCPText(t, reflected)), r["structuredContent"]) {
			t.Fatal("reflection changed knowledge values")
		}
	})
	t.Run("quotes fences paths and exact values", func(t *testing.T) {
		fixture := markdownMCPFixture()
		body := strings.Repeat("~~~\n````\n## Text 99\nFields: []\n<source>&\n\u65e5\u672c\u8a9e\n\n", 20) + "final line without newline"
		fixture["a/~b"] = []any{body, body, "", "\r\n", "\x00"}
		fixture[""] = map[string]any{"/": body, "quote\"\n": body}
		fixture["large"] = json.Number("9007199254740993123")
		fixture["decimal"] = json.Number("1.2300e+40")
		raw, _ := json.Marshal(fixture)
		r := markdownMCPCall(t, "shikii_wiki", http.StatusOK, raw)
		var want any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		_ = d.Decode(&want)
		if got := restoreMarkdownMCP(t, markdownMCPText(t, r)); !reflect.DeepEqual(got, want) {
			t.Fatal("Markdown lost or changed an original value")
		}
		if !reflect.DeepEqual(r["structuredContent"], want) {
			t.Fatal("machine result lost exact numeric values")
		}
	})
	t.Run("bounded extraction retains large deep and unique values", func(t *testing.T) {
		fixture := markdownMCPFixture()
		values := make([]any, 280)
		for i := range 140 {
			value := strings.Repeat(strconv.Itoa(i)+" different value ", 30)
			values[2*i], values[2*i+1] = value, value
		}
		fixture["many"] = values
		var deep any = strings.Repeat("deep leaf\n", 100)
		for i := 0; i < 70; i++ {
			deep = map[string]any{"child": deep}
		}
		fixture["deep"] = deep
		raw, _ := json.Marshal(fixture)
		r := markdownMCPCall(t, "shikii_wiki", 200, raw)
		text := markdownMCPText(t, r)
		var got any
		if strings.HasPrefix(text, "# Knowledge\n") {
			got = restoreMarkdownMCP(t, text)
		} else {
			d := json.NewDecoder(strings.NewReader(text))
			d.UseNumber()
			_ = d.Decode(&got)
		}
		if !reflect.DeepEqual(got, r["structuredContent"]) {
			t.Fatal("bounded extraction dropped data")
		}
		large := `{"body":"` + strings.Repeat("x", (1<<20)+1) + `"}`
		r = markdownMCPCall(t, "shikii_wiki", 200, []byte(large))
		if markdownMCPText(t, r) != large {
			t.Fatal("large response must keep JSON")
		}
		wide := make([]any, 1030)
		for i := range wide {
			wide[i] = strings.Repeat("same source text\n", 30)
		}
		raw, _ = json.Marshal(map[string]any{"texts": wide})
		r = markdownMCPCall(t, "shikii_wiki", 200, raw)
		if !reflect.DeepEqual(restoreMarkdownMCP(t, markdownMCPText(t, r)), r["structuredContent"]) {
			t.Fatal("path limit dropped strings beyond the limit")
		}
	})
	t.Run("small errors and other tools retain JSON", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			code int
			raw  string
		}{
			{"shikii_wiki", 200, `{"lessons":[],"complete":true}`},
			{"shikii_wiki", 400, `{"error":"synthetic error"}`},
			{"shikii_decide", 200, `{"band":"ask","reason":"synthetic reason"}`},
		} {
			r := markdownMCPCall(t, tc.name, tc.code, []byte(tc.raw))
			if markdownMCPText(t, r) != tc.raw || r["isError"] != (tc.code >= 400) {
				t.Fatal("unrelated response changed", tc)
			}
		}
		fixture := markdownMCPFixture()
		raw, _ := json.Marshal(fixture)
		r := markdownMCPCall(t, "shikii_decide", 200, raw)
		if markdownMCPText(t, r) != string(raw) {
			t.Fatal("unrelated tool was reformatted")
		}
		unique, _ := json.Marshal(map[string]any{"body": strings.Repeat("one original paragraph\n", 60)})
		r = markdownMCPCall(t, "shikii_wiki", 200, unique)
		if markdownMCPText(t, r) != string(unique) {
			t.Fatal("unique text needs no reference overhead")
		}
		for _, raw := range []string{`{"body":"x"}{}`, `null`, `[]`} {
			r := markdownMCPCall(t, "shikii_wiki", 200, []byte(raw))
			if r["structuredContent"] != nil || markdownMCPText(t, r) != raw {
				t.Fatal("non-object or malformed result changed", raw)
			}
		}
	})
}

func BenchmarkMCPKnowledgeMarkdown(b *testing.B) {
	raw, _ := json.Marshal(markdownMCPFixture())
	b.Run("json", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = toolResult(http.StatusOK, raw)
		}
	})
	b.Run("markdown", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = toolResult(http.StatusOK, raw, "shikii_wiki")
		}
	})
}

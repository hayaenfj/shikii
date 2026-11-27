package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// 0.44.2 audit A-1: the refusals of a save that would wipe an article show the patch to send as one flat JSON object. The command line takes
// that object as it is, but MCP and HTTP take a patch under the field "patch", so an agent that sent the object as shown over MCP or HTTP was
// refused (unknown field "edits"). Nothing sent the object a refusal shows back through the entries, so no test saw it.
//
// These tests cut the patch out of a real refusal, fill the placeholders it shows ([...], "..." and "<...>") with real values, and send it
// the way the refusal says to: through shikii wiki patch on the command line, and through shikii_wiki operation patch over MCP and over HTTP.
// A refusal that does not say how MCP takes the object is followed literally, as an agent would, so it sends the object flat.

// refusedPatchObject returns the JSON object that follows "shikii wiki patch" in a refusal: from its first brace to the brace that closes it.
func refusedPatchObject(t *testing.T, refusal string) string {
	t.Helper()
	at := strings.Index(refusal, "shikii wiki patch")
	if at < 0 {
		t.Fatalf("the refusal does not name shikii wiki patch: %s", refusal)
	}
	start := strings.Index(refusal[at:], "{")
	if start < 0 {
		t.Fatalf("the refusal shows no patch after shikii wiki patch: %s", refusal)
	}
	start += at
	depth, inString := 0, false
	for i := start; i < len(refusal); i++ {
		switch c := refusal[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case inString:
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				return refusal[start : i+1]
			}
		}
	}
	t.Fatalf("the patch the refusal shows is never closed: %s", refusal)
	return ""
}

// patchPlaceholder is a value the refusal leaves for the agent to fill: [...] for the sources, "..." for the reason, and "<what to put>".
var patchPlaceholder = regexp.MustCompile(`"(\w+)":(\[\.\.\.\]|"\.\.\."|"<[^"]*>")`)

// patchObjectFilled fills the placeholders of the object with the values an agent that has read the article would send: the sources, a
// reason, the revision the article has now, and an edit that adds one line after the last line of the body. A placeholder under a key the
// test does not know fails, so a new one in a refusal cannot go unsent.
func patchObjectFilled(t *testing.T, object string, sources []string, revision, ending, addition string) string {
	t.Helper()
	values := map[string]any{"sources": sources, "change_reason": "add one sentence", "revision": revision, "before": ending, "after": ending + addition}
	filled := patchPlaceholder.ReplaceAllStringFunc(object, func(m string) string {
		key := patchPlaceholder.FindStringSubmatch(m)[1]
		value, ok := values[key]
		if !ok {
			t.Fatalf("the refusal leaves %s to fill, which this test does not know how to fill: %s", m, object)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return `"` + key + `":` + string(data)
	})
	if !json.Valid([]byte(filled)) || strings.Contains(filled, "...") || strings.Contains(filled, "<") {
		t.Fatalf("the object is not whole JSON once its placeholders are filled: %s", filled)
	}
	return filled
}

// mcpWayOf returns the parenthesis of a refusal that says how MCP takes the patch ("(MCP: ...)"), and whether it puts the object under patch.
func mcpWayOf(t *testing.T, refusal string) (way string, nested bool) {
	t.Helper()
	way = regexp.MustCompile(`\(MCP: [^)]*\)`).FindString(refusal)
	if way == "" {
		t.Fatalf("the refusal does not say how MCP takes the patch: %s", refusal)
	}
	return way, strings.Contains(way, "under patch")
}

// The refusals that send an agent to a patch, and the patch each one shows goes back through every entry that takes one.
func TestThePatchARefusalShowsIsSavedThroughEveryEntryThatTakesAPatch(t *testing.T) {
	f := newOverwriteFixture(t)
	refuse := overwriteEntries(t, f)["shikii wiki ingest on the command line"]
	rows := func(numbers ...int) string {
		lines := make([]string, 0, len(numbers))
		for _, n := range numbers {
			lines = append(lines, fmt.Sprintf("row %02dxxxxxxxxxxxx", n))
		}
		return strings.Join(lines, "\n")
	}
	type refusedCase struct {
		name string
		// refused makes an article, sends a save that is refused, and returns the article's id and the refusal.
		refused func(t *testing.T, name string) (id, refusal string)
	}
	cases := []refusedCase{
		{"a body that replaces the article", func(t *testing.T, name string) (string, string) {
			p := f.article(t, name)
			return p.ID, refusalText(refuse(t, f, f.draft(p, overwriteShort)))
		}},
		{"an id and no lesson", func(t *testing.T, name string) (string, string) {
			p := f.article(t, name)
			return p.ID, refusalText(refuse(t, f, map[string]any{"id": p.ID, "sources": f.sources, "change_reason": "add one sentence"}))
		}},
		{"an id and no sources", func(t *testing.T, name string) (string, string) {
			p := f.article(t, name)
			d := f.draft(p, overwriteBody+"\n- A line the agent adds.\n")
			delete(d, "sources")
			return p.ID, refusalText(refuse(t, f, d))
		}},
		{"a body that wears the article away in small saves", func(t *testing.T, name string) (string, string) {
			p := f.articleWith(t, name, rows(1, 2, 3, 4, 5, 6, 7, 8))
			for _, step := range [][]int{{1, 2, 3, 4, 5, 6, 11}, {1, 2, 3, 4, 5, 11, 12}} {
				if refusal := refuse(t, f, f.draft(p, rows(step...))); refusal != "" {
					t.Fatalf("a save that keeps more than half of the body is refused: %s", refusal)
				}
			}
			return p.ID, refusalText(refuse(t, f, f.draft(p, rows(1, 2, 3, 4, 11, 12, 13))))
		}},
	}
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, f.st.Dir)
	// Each entry sends the filled object and returns why it was refused ("" when it was saved). nested says whether the refusal told to put
	// the object under patch for MCP and HTTP.
	type patchEntry func(t *testing.T, object string, nested bool) string
	entries := map[string]patchEntry{
		"shikii wiki patch on the command line": func(t *testing.T, object string, nested bool) string {
			if _, err := commandStdin(t, json.RawMessage(object), func() error { return cmdWiki([]string{"patch"}) }); err != nil {
				return err.Error()
			}
			return ""
		},
		"shikii_wiki patch over MCP": func(t *testing.T, object string, nested bool) string {
			args := map[string]any{"operation": "patch"}
			if nested {
				args["patch"] = json.RawMessage(object)
			} else {
				var flat map[string]any
				if err := json.Unmarshal([]byte(object), &flat); err != nil {
					t.Fatal(err)
				}
				for k, v := range flat {
					args[k] = v
				}
			}
			got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}}))
			if got["isError"] == true {
				return got["content"].([]any)[0].(map[string]any)["text"].(string)
			}
			return ""
		},
		"wiki patch over HTTP": func(t *testing.T, object string, nested bool) string {
			args := `{"operation":"patch","patch":` + object + `}`
			if !nested {
				args = `{"operation":"patch",` + strings.TrimPrefix(object, "{")
			}
			code, body, err := callTool(f.h, "shikii_wiki", json.RawMessage(args))
			if err != nil {
				t.Fatal(err)
			}
			if code >= 400 {
				return string(body)
			}
			return ""
		},
	}
	if len(entries) != 3 {
		t.Fatalf("the command line, MCP and HTTP forms of patch are all tried: %d entries", len(entries))
	}
	const addition = "\n- A line the agent adds with the patch the refusal showed."
	for _, c := range cases {
		for entryName, entry := range entries {
			t.Run(c.name+" through "+entryName, func(t *testing.T) {
				id, refusal := c.refused(t, c.name+" "+entryName)
				if refusal == "" {
					t.Fatal("the save is not refused")
				}
				_, nested := mcpWayOf(t, refusal)
				revision, before := f.body(t, id)
				lines := strings.Split(before, "\n")
				object := patchObjectFilled(t, refusedPatchObject(t, refusal), f.sources, revision, lines[len(lines)-1], addition)
				if why := entry(t, object, nested); why != "" {
					t.Fatalf("the patch the refusal shows, sent as it says, is refused: %s\nthe patch: %s\nthe refusal: %s", why, object, refusal)
				}
				after, body := f.body(t, id)
				if after == revision || body != before+addition {
					t.Fatalf("the patch was not saved as the one line that it adds (revision %s, was %s): %q", after, revision, body)
				}
			})
		}
	}
}

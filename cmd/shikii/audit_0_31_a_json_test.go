package main

import (
	"strings"
	"testing"
)

// audit A: what the one reader answers for inputs at its edges (INVARIANT 1: the refusal of an unknown key or a wrong type names
// the place, written as notes[0].sources, and the fields that place takes). The test only logged what came back and failed on the
// time a refusal took (200 ms), which says nothing on a busy machine and nothing about the text; it now checks the text.
func TestAuditAStrictReaderEdges(t *testing.T) {
	// a key written with an escape for its first letter (the two halves keep the escape out of this file as a literal)
	escapedKey := "{\"\\" + "u0062ogus\":1}"
	cases := []struct {
		name, input string
		refused     bool
		extra       bool
		want        []string // what the refusal says
	}{
		// malformed JSON: refused; what the refusal says belongs to the entries (jsonShapeError), not to this reader
		{"trailing comma", `{"query":"x",}`, true, false, nil},
		{"a raw newline in a string", "{\"query\":\"a\nb\"}", true, false, nil},
		{"single quotes", `{'query':'x'}`, true, false, nil},
		// not an object
		{"top-level array", `[{"query":"x"}]`, true, false, []string{"pass the JSON as an object", "(got array)"}},
		{"top-level string", `"query"`, true, false, []string{"pass the JSON as an object", "(got string)"}},
		{"null", `null`, false, false, nil},
		{"two objects", `{"query":"x"}{"query":"y"}`, false, true, nil},
		// an unknown key: the place and the fields there
		{"unknown key twice, known place first", `{"query":"x","result":{"query":"y"}}`, true, false, []string{`unknown field "query" in result; the fields there: `, "changed", "evidence"}},
		{"unknown key under a known key of another case", `{"Query":"x","RESULT":{"bogus":1}}`, true, false, []string{`unknown field "bogus" in RESULT; the fields there: `, "changed"}},
		{"the same unknown key in two places", `{"bogus":1,"notes":[{"bogus":2}]}`, true, false, []string{`unknown field "bogus" at the top level; the fields there: `, "receipt", "notes"}},
		{"unknown key deep in a note", `{"receipt":"r","notes":[{"title":"t","learning":{"probe":{"results":[{"nope":1}]}}}]}`, true, false, []string{`unknown field "nope" in notes[0].learning.probe.results[0]; the fields there: `, "conclusion"}},
		{"empty key", `{"":1}`, true, false, []string{`unknown field "" at the top level; the fields there: `}},
		{"key with a quote", `{"a\"b":1}`, true, false, []string{`unknown field "a\"b" at the top level; the fields there: `}},
		{"key with unicode escape", escapedKey, true, false, []string{`unknown field "bogus" at the top level; the fields there: `}},
		{"project at the top", `{"project":"/x","query":"y"}`, true, false, []string{`unknown field "project" at the top level`, "project is an argument of the MCP tools", "leave project out"}},
		// a wrong type: the place in the same writing, what goes there and what came
		{"wrong type deep", `{"receipt":"r","notes":[{"title":"t","sources":"new1"}]}`, true, false, []string{"pass the JSON notes[0].sources as an array", "(got string)"}},
		{"wrong type in the second element", `{"receipt":"r","observations":[{"kind":"test"},{"kind":7}]}`, true, false, []string{"pass the JSON observations[1].kind as a string (got number)"}},
		{"number too large for int", `{"query":"x","max_bytes":99999999999999999999}`, true, false, []string{"pass the JSON max_bytes as an integer (got number 99999999999999999999)"}},
		{"float for int", `{"query":"x","max_bytes":1.5}`, true, false, []string{"pass the JSON max_bytes as an integer (got number 1.5)"}},
	}
	if !strings.Contains(escapedKey, "\\"+"u0062") {
		t.Fatalf("precondition: the key is sent with its escape: %s", escapedKey)
	}
	for _, c := range cases {
		var req workRequest
		err, extra := decodeStrictObject([]byte(c.input), &req)
		t.Logf("%-50s extra=%v err=%v", c.name, extra, err)
		if (err != nil) != c.refused || extra != c.extra {
			t.Errorf("%s: refused=%v extra=%v, want refused=%v extra=%v: %v", c.name, err != nil, extra, c.refused, c.extra, err)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal lacks %q: %v", c.name, w, err)
			}
		}
	}
	// the widest request types: a key that is unknown at the top level is named with the fields of the top level, and with at most
	// three other places that take a field of that name (the search for them is bounded, so the refusal stays short)
	for _, c := range []struct {
		target any
		want   []string
	}{
		{&workRequest{}, []string{`unknown field "sources" at the top level; the fields there: `, "receipt"}},
		{&notebookRequest{}, []string{`unknown field "zz_unknown" at the top level; the fields there: `, "operation"}},
		{&reflectRequest{}, []string{`unknown field "zz_unknown" at the top level; the fields there: operation, sources, reviews, wiki, memory, maintenance, discoveries`}},
		{&planningRequest{}, []string{`unknown field "sources" at the top level; the fields there: `, `"sources" is a field of reflection, reflection.wiki[]`}},
	} {
		err, _ := decodeStrictObject([]byte(`{"sources":["x"],"zz_unknown":1,"evidence":1}`), c.target)
		if err == nil {
			t.Errorf("%T: an unknown key is refused", c.target)
			continue
		}
		t.Logf("%T: err=%.300v", c.target, err)
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%T: the refusal lacks %q: %v", c.target, w, err)
			}
		}
		if _, homes, named := strings.Cut(err.Error(), " is a field of "); named {
			homes, _, _ = strings.Cut(homes, ";")
			if n := len(strings.Split(homes, ", ")); n > 3 {
				t.Errorf("%T: the refusal names %d other places of the field, more than three: %s", c.target, n, homes)
			}
		}
	}
	// a large input with the unknown key at its end: the key is named with its place, and the input is not sent back
	big := `{"receipt":"r","observations":[` + strings.Repeat(`{"kind":"test","title":"t","content":"`+strings.Repeat("x", 1000)+`"},`, 4000) + `{"kind":"test"}],"zz":1}`
	var req workRequest
	err, _ := decodeStrictObject([]byte(big), &req)
	if err == nil || !strings.Contains(err.Error(), `unknown field "zz" at the top level; the fields there: `) || len(err.Error()) > 2048 {
		t.Errorf("%d bytes with an unknown key at the end: the refusal names the key and its place in at most 2048 bytes: %.300v", len(big), err)
	}
}

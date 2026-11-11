package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// audit A: what the one reader answers for inputs at its edges.
func TestAuditAStrictReaderEdges(t *testing.T) {
	cases := []struct{ name, input string }{
		{"trailing comma", `{"query":"x",}`},
		{"a raw newline in a string", "{\"query\":\"a\nb\"}"},
		{"single quotes", `{'query':'x'}`},
		{"top-level array", `[{"query":"x"}]`},
		{"top-level string", `"query"`},
		{"null", `null`},
		{"two objects", `{"query":"x"}{"query":"y"}`},
		{"unknown key twice, known place first", `{"query":"x","result":{"query":"y"}}`},
		{"unknown key under a known key of another case", `{"Query":"x","RESULT":{"bogus":1}}`},
		{"the same unknown key in two places", `{"bogus":1,"notes":[{"bogus":2}]}`},
		{"unknown key deep in a note", `{"receipt":"r","notes":[{"title":"t","learning":{"probe":{"results":[{"nope":1}]}}}]}`},
		{"wrong type deep", `{"receipt":"r","notes":[{"title":"t","sources":"new1"}]}`},
		{"wrong type in the second element", `{"receipt":"r","observations":[{"kind":"test"},{"kind":7}]}`},
		{"number too large for int", `{"query":"x","max_bytes":99999999999999999999}`},
		{"float for int", `{"query":"x","max_bytes":1.5}`},
		{"empty key", `{"":1}`},
		{"key with a quote", `{"a\"b":1}`},
		{"key with unicode escape", `{"bogus":1}`},
		{"project at the top", `{"project":"/x","query":"y"}`},
	}
	for _, c := range cases {
		var req workRequest
		start := time.Now()
		err, extra := decodeStrictObject([]byte(c.input), &req)
		took := time.Since(start)
		t.Logf("%-50s extra=%v took=%v err=%v", c.name, extra, took.Round(time.Microsecond), err)
		if took > 200*time.Millisecond {
			t.Errorf("%s: the refusal took %v", c.name, took)
		}
	}
	// the widest request type: how long the search for the other homes of a field takes
	for _, target := range []any{&workRequest{}, &notebookRequest{}, &reflectRequest{}, &planningRequest{}} {
		start := time.Now()
		err, _ := decodeStrictObject([]byte(`{"sources":["x"],"zz_unknown":1,"evidence":1}`), target)
		t.Logf("%T: took=%v err=%.300v", target, time.Since(start).Round(time.Microsecond), err)
	}
	// a large input with the unknown key at its end
	big := `{"receipt":"r","observations":[` + strings.Repeat(`{"kind":"test","title":"t","content":"`+strings.Repeat("x", 1000)+`"},`, 4000) + `{"kind":"test"}],"zz":1}`
	start := time.Now()
	var req workRequest
	err, _ := decodeStrictObject([]byte(big), &req)
	t.Logf("%d bytes: took=%v err=%.200v", len(big), time.Since(start).Round(time.Millisecond), err)
	_ = fmt.Sprint()
}

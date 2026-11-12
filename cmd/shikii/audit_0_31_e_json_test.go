package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// auditEFill fills a value of any request type with something in every field: two elements in a slice, the keys "k" and "1" in a
// map, every pointer allocated. Types that read their own JSON are left as they are.
func auditEFill(v reflect.Value, depth int) {
	if depth > 7 {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.Type().Elem().Kind() == reflect.Struct || v.Type().Elem().Kind() == reflect.Bool || v.Type().Elem().Kind() == reflect.String || v.Type().Elem().Kind() == reflect.Int {
			v.Set(reflect.New(v.Type().Elem()))
			auditEFill(v.Elem(), depth+1)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Unix(1700000000, 0).UTC()))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.CanSet() {
				auditEFill(f, depth+1)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		s := reflect.MakeSlice(v.Type(), 2, 2)
		auditEFill(s.Index(0), depth+1)
		auditEFill(s.Index(1), depth+1)
		v.Set(s)
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}
		m := reflect.MakeMap(v.Type())
		for _, key := range []string{"k", "1"} {
			e := reflect.New(v.Type().Elem()).Elem()
			auditEFill(e, depth+1)
			m.SetMapIndex(reflect.ValueOf(key).Convert(v.Type().Key()), e)
		}
		v.Set(m)
	case reflect.String:
		v.SetString("text")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	}
}

type auditEStep struct {
	key   string
	index int
	list  bool
}

func auditEPath(steps []auditEStep) string {
	out := ""
	for _, s := range steps {
		if s.list {
			out += "[" + strconv.Itoa(s.index) + "]"
		} else if out == "" {
			out = s.key
		} else {
			out += "." + s.key
		}
	}
	return out
}

// auditEMutations returns every document that differs from tree in one value, replaced by a value of another JSON type, with the
// path of that value written the way the reader should write it.
func auditEMutations(tree any) (paths []string, docs [][]byte, kinds []string) {
	var walk func(node any, steps []auditEStep, set func(any))
	emit := func(steps []auditEStep, set func(any), original any, wrong any, kind string) {
		set(wrong)
		raw, _ := json.Marshal(tree)
		set(original)
		paths, docs, kinds = append(paths, auditEPath(steps)), append(docs, raw), append(kinds, kind)
	}
	walk = func(node any, steps []auditEStep, set func(any)) {
		switch n := node.(type) {
		case map[string]any:
			if len(steps) > 0 {
				emit(steps, set, node, "x", "string")
				emit(steps, set, node, []any{}, "array")
			}
			keys := make([]string, 0, len(n))
			for k := range n {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				k := k
				walk(n[k], append(append([]auditEStep{}, steps...), auditEStep{key: k}), func(v any) { n[k] = v })
			}
		case []any:
			emit(steps, set, node, "x", "string")
			emit(steps, set, node, map[string]any{}, "object")
			for i := range n {
				i := i
				walk(n[i], append(append([]auditEStep{}, steps...), auditEStep{index: i, list: true}), func(v any) { n[i] = v })
			}
		case string:
			emit(steps, set, node, 12, "number")
			emit(steps, set, node, []any{"x"}, "array")
			emit(steps, set, node, map[string]any{"x": 1}, "object")
		case float64:
			emit(steps, set, node, "x", "string")
			emit(steps, set, node, true, "bool")
		case bool:
			emit(steps, set, node, "x", "string")
			emit(steps, set, node, 3, "number")
		}
	}
	walk(tree, nil, func(any) {})
	return
}

// audit E: for every field of every request type an agent sends, a value of the wrong JSON type is answered with the place of
// that value: a key stays a key, an index is written [N], and the place is the one that was changed.
func TestAuditEWrongTypePlacesOverEveryRequestType(t *testing.T) {
	targets := map[string]func() any{
		"work":            func() any { return &workRequest{} },
		"reflect":         func() any { return &reflectRequest{} },
		"wiki (HTTP/MCP)": func() any { return &notebookRequest{} },
		"hook plan":       func() any { return &planningRequest{} },
		"guard add":       func() any { return &guard{} },
		"review":          func() any { return &reviewRequest{} },
		"wiki ingest":     func() any { return &notebook.Draft{} },
		"wiki patch":      func() any { return &notebook.Patch{} },
		"wiki capture":    func() any { return &notebook.Observation{} },
		"wiki maintain":   func() any { return &notebook.MaintenanceReview{} },
		"variants":        func() any { return &variantsRequest{} },
		"wording check":   func() any { return &wordingCheckRequest{} },
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		full := targets[name]()
		auditEFill(reflect.ValueOf(full).Elem(), 0)
		raw, err := json.Marshal(full)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var tree any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatal(err)
		}
		if err, _ := decodeStrictObject(raw, targets[name]()); err != nil {
			t.Errorf("%s: the filled sample itself is refused: %v", name, err)
			continue
		}
		paths, docs, kinds := auditEMutations(tree)
		wrong, typed, other := 0, 0, map[string]int{}
		for i, doc := range docs {
			var got error
			func() {
				defer func() {
					if r := recover(); r != nil {
						got = fmt.Errorf("PANIC: %v", r)
					}
				}()
				got, _ = decodeStrictObject(doc, targets[name]())
			}()
			if got == nil {
				continue
			}
			var typeErr *fieldTypeError
			if !errors.As(got, &typeErr) {
				other[got.Error()]++
				continue
			}
			typed++
			if typeErr.Field != paths[i] {
				wrong++
				if wrong <= 6 {
					t.Errorf("%s: a %s put at %s is answered with the place %q: %v", name, kinds[i], paths[i], typeErr.Field, got)
				}
			}
		}
		t.Logf("%-16s %4d wrong-type inputs, %4d answered with a place, %d with another place than the changed one", name, len(docs), typed, wrong)
		for text, n := range other {
			t.Logf("    other answer (%d): %.200s", n, text)
			if strings.Contains(text, "PANIC") {
				t.Errorf("%s: the reader panicked: %s", name, text)
			}
		}
	}
}

// audit E: inputs around the byte order mark and other edges, at every entry that reads an agent's JSON.
func TestAuditEJSONEdgesAtEveryEntry(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	bom := "\xef\xbb\xbf"
	deep := strings.Repeat("[", 12000) + strings.Repeat("]", 12000)
	cases := []struct{ name, input string }{
		{"a byte order mark alone", bom},
		{"a byte order mark and a line break", bom + "\n"},
		{"a mark, then JSON with a comma before the brace", bom + `{"sources":["x"],}`},
		{"two marks, then JSON", bom + bom + `{"sources":["x"]}`},
		{"null", `null`},
		{"a string", `"text"`},
		{"an array", `[{"sources":["x"]}]`},
		{"very deep nesting", `{"sources":` + deep + `}`},
		{"a bad UTF-8 byte inside a string", "{\"sources\":[\"x\xff\"],}"},
		{"two objects", `{} {}`},
		{"a number too large", `{"max_bytes":1e999}`},
	}
	for _, c := range cases {
		seen := map[string][]string{}
		for _, e := range jsonEntries {
			got := ""
			func() {
				defer func() {
					if r := recover(); r != nil {
						got = fmt.Sprintf("PANIC: %v", r)
						t.Errorf("%s, %s: the entry panicked: %v", e.name, c.name, r)
					}
				}()
				if strings.HasPrefix(e.name, "POST") {
					req := newAuditERequest(strings.TrimSuffix(strings.Fields(e.name)[1], " "), c.input)
					code, body := req(h)
					got = fmt.Sprintf("%d %s", code, body)
				} else {
					got = e.call(t, h, c.input)
				}
			}()
			for _, word := range []string{"invalid character", "unexpected EOF", "json: ", "looking for beginning", "Go struct field", "Go value"} {
				if strings.Contains(got, word) {
					got += "   <== encoding/json's words"
					break
				}
			}
			seen[got] = append(seen[got], e.name)
		}
		t.Logf("--- %s", c.name)
		answers := make([]string, 0, len(seen))
		for a := range seen {
			answers = append(answers, a)
		}
		sort.Strings(answers)
		for _, a := range answers {
			t.Logf("  %3d entries: %.260s   (e.g. %s)", len(seen[a]), a, seen[a][0])
		}
	}
}

func newAuditERequest(path, body string) func(h interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}) (int, string) {
	return func(h interface {
		ServeHTTP(http.ResponseWriter, *http.Request)
	}) (int, string) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
}

// audit E: the reader skips a byte order mark, so an input of a mark alone is an empty input. The entries answer an empty input
// with the shape to send; the mark alone gets encoding/json's "EOF".
func TestAuditEByteOrderMarkAloneIsAnEmptyInput(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	for _, e := range jsonEntries {
		if strings.HasPrefix(e.name, "POST") {
			continue
		}
		empty := e.call(t, h, "")
		mark := e.call(t, h, "\xef\xbb\xbf")
		markAndBreak := e.call(t, h, "\xef\xbb\xbf\n")
		if mark != empty || markAndBreak != empty {
			t.Errorf("%s:\n   empty input        -> %.150s\n   a mark alone       -> %.150s\n   a mark and a break -> %.150s", e.name, empty, mark, markAndBreak)
		}
	}
}

// audit E: what each CLI entry answers to an empty standard input (logged; the entries without a refusal of their own are named).
func TestAuditEEmptyInputAtEveryCLIEntry(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	for _, e := range jsonEntries {
		if strings.HasPrefix(e.name, "POST") {
			continue
		}
		empty := e.call(t, h, "")
		// the entry's own words for an empty standard input, or the reader's: either says what is missing; encoding/json's EOF does not
		if !strings.Contains(empty, "standard input was empty") && !strings.Contains(empty, "pass the JSON as an object {\u2026} (got nothing)") || strings.Contains(empty, "EOF") {
			t.Errorf("%-30s empty input -> %.160s", e.name, empty)
		}
	}
}

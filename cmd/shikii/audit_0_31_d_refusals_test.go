package main

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/handoff"
)

// The second round of the audit of 0.31.0 (D-A3, C-U5, D-I1, D-U4): the refusals of the entries an agent calls. The fix of the
// first round worded malformed JSON for work alone; reflect, wiki and the HTTP bodies kept encoding/json's words, an input that was
// cut off was "unexpected EOF" everywhere, a key of an object was written as an index, and a work record could still be replaced
// by a text with nothing in it.

// jsonEntry is one entry that reads an agent's JSON: how the test reaches it, and the function of the program that hands the
// input to the one reader (decodeStrictObject, or decodeStrict around it).
type jsonEntry struct {
	name   string
	reader string
	call   func(t *testing.T, h http.Handler, input string) string // the text of the refusal
}

func cliJSONEntry(args ...string) func(*testing.T, http.Handler, string) string {
	return func(t *testing.T, _ http.Handler, input string) string {
		t.Helper()
		var err error
		withStdin(t, input, func() {
			_, err = captureErr(func() error {
				switch args[0] {
				case "review":
					return cmdReview(args[1:])
				case "guard":
					return cmdGuard(args[1:])
				}
				return cliCall(args)
			})
		})
		if err == nil {
			return ""
		}
		return err.Error()
	}
}

func httpJSONEntry(path string) func(*testing.T, http.Handler, string) string {
	return func(t *testing.T, h http.Handler, input string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(input))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var reply struct {
			Error string `json:"error"`
		}
		if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &reply) != nil {
			t.Errorf("POST %s with JSON that cannot be read answers %d %s", path, rec.Code, rec.Body.String())
		}
		return reply.Error
	}
}

// jsonEntries is every entry that reads an agent's JSON. TestEveryEntryAnswersUnreadableJSONInTheReadersWords fails when a
// function that calls the reader has no row here.
var jsonEntries = []jsonEntry{
	{"shikii work prepare", "cmdWork", cliJSONEntry("work", "prepare")},
	{"shikii work record", "cmdWork", cliJSONEntry("work", "record")},
	{"shikii hook plan query", "cmdHookPlan", cliJSONEntry("hook", "plan", "query")},
	{"shikii hook plan submit", "cmdHookPlan", cliJSONEntry("hook", "plan", "submit")},
	{"shikii hook variants submit", "cmdHookVariants", cliJSONEntry("hook", "variants", "submit")},
	{"shikii hook variants check", "cmdHookVariantsCheck", cliJSONEntry("hook", "variants", "check")},
	{"shikii reflect place", "decodeReflect", cliJSONEntry("reflect", "place")},
	{"shikii reflect submit", "decodeReflect", cliJSONEntry("reflect", "submit")},
	{"shikii wiki ingest", "cmdNotebook", cliJSONEntry("wiki", "ingest")},
	{"shikii wiki capture", "cmdNotebook", cliJSONEntry("wiki", "capture")},
	{"shikii wiki patch", "cmdNotebook", cliJSONEntry("wiki", "patch")},
	{"shikii wiki maintain", "cmdNotebook", cliJSONEntry("wiki", "maintain")},
	{"shikii wiki consult", "cmdNotebook", cliJSONEntry("wiki", "consult")},
	{"shikii wiki reuse", "cmdNotebook", cliJSONEntry("wiki", "reuse")},
	{"shikii wiki organize", "cmdNotebook", cliJSONEntry("wiki", "organize")},
	{"shikii wiki reconcile", "cmdNotebook", cliJSONEntry("wiki", "reconcile")},
	{"shikii wiki diagnose", "cmdNotebook", cliJSONEntry("wiki", "diagnose")},
	{"shikii wiki compare", "cmdNotebook", cliJSONEntry("wiki", "compare")},
	{"shikii review submit", "decodeReview", cliJSONEntry("review", "submit")},
	{"shikii guard add", "guardAdd", cliJSONEntry("guard", "add")},
	{"POST /v1/wiki/notebook (the MCP tool shikii_wiki)", "handleNotebook", httpJSONEntry("/v1/wiki/notebook")},
	{"POST /v1/reflect (the MCP tool shikii_reflect)", "decodeReflect", httpJSONEntry("/v1/reflect")},
	{"POST /v1/review (the MCP tool shikii_review)", "decodeReview", httpJSONEntry("/v1/review")},
}

// readerCallers lists the functions of the given Go sources that call the reader of an agent's JSON.
func readerCallers(t *testing.T, sources map[string]string) map[string]bool {
	t.Helper()
	callers := map[string]bool{}
	fset := token.NewFileSet()
	for name, source := range sources {
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "decodeStrictObject" || id.Name == "decodeStrict") {
						callers[fn.Name.Name] = true
					}
				}
				return true
			})
		}
	}
	return callers
}

// withoutARow names the callers of the reader that the table does not cover, and the rows whose function does not call it.
func withoutARow(callers map[string]bool, entries []jsonEntry) (uncovered, idle []string) {
	covered := map[string]bool{"decodeStrict": true} // the reader's wrapper for one value: its callers are the entries
	for _, e := range entries {
		covered[e.reader] = true
	}
	for name := range callers {
		if !covered[name] {
			uncovered = append(uncovered, name)
		}
	}
	for name := range covered {
		if !callers[name] {
			idle = append(idle, name)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(idle)
	return uncovered, idle
}

// D-A3 and C-U5 (a): JSON that does not parse is answered alike at every entry, in the reader's words, with the place. The reader
// words it once; an entry only puts its own words in front.
func TestEveryEntryAnswersUnreadableJSONInTheReadersWords(t *testing.T) {
	sources := map[string]string{} // read before the fixture moves the working directory
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot list the sources of the package: %v", err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		sources[name] = string(mustRead(t, name))
	}
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	for _, c := range []struct{ name, input, want string }{
		{"a comma before the closing brace", `{"sources":["x"],}`, "the JSON is malformed (at byte 18)"},
		{"an input that was cut off", `{"sources":["x"]`, "the JSON is malformed (at byte 16: the input ends there, before the JSON is closed; send the whole object)"},
	} {
		var object map[string]any
		own, _ := decodeStrictObject([]byte(c.input), &object)
		if own == nil || own.Error() != c.want {
			t.Fatalf("%s: the reader answers %q, want %q", c.name, own, c.want)
		}
		for _, e := range jsonEntries {
			got := e.call(t, h, c.input)
			if !strings.Contains(got, c.want) {
				t.Errorf("%s, %s: the refusal lacks the reader's sentence %q: %q", e.name, c.name, c.want, got)
			}
			for _, word := range []string{"invalid character", "unexpected EOF", "json: ", "looking for beginning"} {
				if strings.Contains(got, word) {
					t.Errorf("%s, %s: the refusal is in encoding/json's words (%q): %q", e.name, c.name, word, got)
				}
			}
		}
	}
	// the table is every entry: a function that hands an agent's JSON to the reader has a row, and a row names such a function
	uncovered, idle := withoutARow(readerCallers(t, sources), jsonEntries)
	if len(uncovered) > 0 {
		t.Errorf("these functions read an agent's JSON and have no row in jsonEntries: %v", uncovered)
	}
	if len(idle) > 0 {
		t.Errorf("these rows of jsonEntries name a function that does not call the reader: %v", idle)
	}
	// the negative example: an entry nobody listed is found
	added := readerCallers(t, map[string]string{"new_entry.go": "package main\nfunc cmdNewEntry(data []byte) error {\n\tvar v struct{}\n\terr, _ := decodeStrictObject(data, &v)\n\treturn err\n}\n"})
	if uncovered, _ := withoutARow(added, jsonEntries); len(uncovered) != 1 || uncovered[0] != "cmdNewEntry" {
		t.Errorf("a new function that calls the reader is not reported: %v", uncovered)
	}
}

// What the reader returns is one of its own answers, never encoding/json's: a place for JSON that does not parse (C-U5 a), a
// number written as an index only where the request has an array (C-U5 b).
func TestTheReaderAnswersInItsOwnWords(t *testing.T) {
	own := func(err error) bool {
		var malformed *malformedJSONError
		var wrongType *fieldTypeError
		var unknown *unknownFieldError
		return errors.As(err, &malformed) || errors.As(err, &wrongType) || errors.As(err, &unknown)
	}
	for _, c := range []struct{ input, want string }{
		{`{"receipt":"r",}`, "the JSON is malformed (at byte 16)"},
		{`{'receipt':'r'}`, "the JSON is malformed (at byte 2)"},
		{`{"receipt":"r","notes":[{"title":"abc`, "the JSON is malformed (at byte 37: the input ends there, before the JSON is closed; send the whole object)"},
		{"{\"receipt\":\"r\"\n  \n", "the JSON is malformed (at byte 14: the input ends there, before the JSON is closed; send the whole object)"},
		{`{"receipt": tru`, "the JSON is malformed (at byte 15: the input ends there, before the JSON is closed; send the whole object)"},
		// a key of an object stays a key, also when it is a number; an element of an array is an index
		{`{"receipt":"r","trial":{"ref":"t","result":{"assessment":"supports","observed":"x","evidence":["new1"],"measurements":{"1":"fast"}}}}`, "pass the JSON trial.result.measurements.1 as a number (got string)"},
		{`{"receipt":"r","trial":{"ref":"t","result":{"assessment":"supports","observed":"x","evidence":["new1"],"measurements":{"p95":"fast"}}}}`, "pass the JSON trial.result.measurements.p95 as a number (got string)"},
		{`{"receipt":"r","notes":[{"sources":"new1"}]}`, "pass the JSON notes[0].sources as an array"},
		{`{"receipt":"r","observations":[{"kind":"test"},{"kind":7}]}`, "pass the JSON observations[1].kind as a string (got number)"},
		{`{"receipt":"r","notes":[{},{"edits":[{"field":1}]}]}`, "pass the JSON notes[1].edits[0].field as a string (got number)"},
		{`{"receipt":"r","result":"done"}`, "pass the JSON result as an object"},
		{`[1,2]`, "pass the JSON as an object"},
		{`{"receipt":"r","notes":[{"nope":1}]}`, `unknown field "nope" in notes[0]; the fields there: `},
	} {
		var req workRequest
		err, _ := decodeStrictObject([]byte(c.input), &req)
		if err == nil || !own(err) || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: the reader answers %T %q, want one of its own answers starting %q", c.input, err, err, c.want)
			continue
		}
		for _, word := range []string{"invalid character", "unexpected EOF", "json: ", "Go struct field", "Go value"} {
			if strings.Contains(err.Error(), word) {
				t.Errorf("%s: the answer is in encoding/json's words (%q): %q", c.input, word, err)
			}
		}
	}
	// what is no object at all, each with its cause: nothing, and null (encoding/json reads null into any value without a word, and
	// the entry went on with an empty request). A byte order mark before them is not part of the JSON and changes nothing.
	mark := string([]byte{0xef, 0xbb, 0xbf})
	for _, c := range []struct{ input, ends string }{
		{"", "(got nothing)"},
		{" \n", "(got nothing)"},
		{mark, "(got nothing)"},
		{"null", "(got null)"},
		{" null\n", "(got null)"},
		{mark + "null", "(got null)"},
	} {
		var req workRequest
		err, _ := decodeStrictObject([]byte(c.input), &req)
		if err == nil || !own(err) || !strings.HasPrefix(err.Error(), "pass the JSON as an object") || !strings.HasSuffix(err.Error(), c.ends) {
			t.Errorf("%q: the reader answers %T %q, want one of its own answers: pass the JSON as an object ... %s", c.input, err, err, c.ends)
		}
	}
	// the negative example: what encoding/json returns is none of the reader's answers
	for _, raw := range []error{io.EOF, io.ErrUnexpectedEOF, &json.SyntaxError{}, &json.UnmarshalTypeError{}, errors.New(`json: unknown field "x"`)} {
		if own(raw) {
			t.Errorf("%T counts as one of the reader's own answers", raw)
		}
	}
	// the reader of the entries that pass over keys they do not know (decide, label, tune) says the same sentences
	for _, input := range []string{`{"receipt":"r",}`, `{"receipt":"r","notes":[{"title":"abc`, `{"receipt":"r","notes":[{"sources":"new1"}]}`, `[1,2]`, "", "null", mark, mark + "null", mark + `{"receipt":"r",}`} {
		var strictly, loosely workRequest
		strict, _ := decodeStrictObject([]byte(input), &strictly)
		loose := decodeObject(strings.NewReader(input), &loosely)
		if strict == nil || loose == nil || loose.Error() != strict.Error() {
			t.Errorf("%q: the two readers answer %q and %q", input, strict, loose)
		}
	}
	var passedOver struct {
		Spec string `json:"spec"`
	}
	if err := decodeObject(strings.NewReader(`{"spec":"needs-plan","not_a_field":1}`), &passedOver); err != nil || passedOver.Spec != "needs-plan" {
		t.Errorf("decodeObject passes over a key it does not know: %v %+v", err, passedOver)
	}
	// and takes a byte order mark before the JSON, as decodeStrictObject does
	passedOver.Spec = ""
	if err := decodeObject(strings.NewReader(mark+`{"spec":"needs-plan"}`), &passedOver); err != nil || passedOver.Spec != "needs-plan" {
		t.Errorf("decodeObject reads the JSON behind a byte order mark: %v %+v", err, passedOver)
	}
	var marked workRequest
	if err, _ := decodeStrictObject([]byte(mark+`{"receipt":"r"}`), &marked); err != nil || marked.Receipt != "r" {
		t.Errorf("decodeStrictObject reads the JSON behind a byte order mark: %v", err)
	}
	if err, _ := decodeStrictObject([]byte(`{"spec":"needs-plan","not_a_field":1}`), &passedOver); err == nil {
		t.Errorf("decodeStrictObject refuses a key it does not know")
	}
	// the path of a value along a type: an index under an array, a key under an object and where the type says nothing
	type inner struct {
		Values map[string][]int `json:"values"`
		List   []map[string]int `json:"list"`
	}
	type outer struct {
		Items  []inner          `json:"items"`
		ByName map[string]inner `json:"by_name"`
		Free   any              `json:"free"`
		Fixed  [2]*inner        `json:"fixed"`
	}
	for path, want := range map[string]string{
		"":                    "",
		"items":               "items",
		"items.0.values.1.2":  "items[0].values.1[2]",
		"by_name.3.list.4.5":  "by_name.3.list[4].5",
		"fixed.1.list.0.name": "fixed[1].list[0].name",
		"free.0.x":            "free.0.x",
		"items.first":         "items.first",
		"nowhere.0":           "nowhere.0",
	} {
		if got := indexedJSONPath(path, reflect.TypeFor[*outer]()); got != want {
			t.Errorf("the place of %q is written %q, want %q", path, got, want)
		}
	}
}

// D-I1 and C-U5 (c): a write or an append whose text holds nothing to read is refused at every entry and the record stays: the
// MCP tool with content left out or blank replaced it with 0 bytes, and the CLI with a byte order mark alone replaced it with the
// mark. What counts is named by what a text is (a character that shows), so a blank of another kind is a blank too.
func TestAWorkRecordIsNotReplacedByATextWithNothingToRead(t *testing.T) {
	// the documents that describe a write say so (read before the fixture moves the working directory)
	for path, want := range map[string]string{
		"../../skills/shikii/references/handoff.md": "A text with nothing to read (`content` left out or empty, an empty standard input, only spaces and line breaks, or only a byte order mark) is refused",
		"../../docs/guide.md":                       "A text with nothing to read (empty, only spaces and line breaks, or only a byte order mark) is refused by `write` and `append`, on the command line and in MCP",
		"../../docs/guide.ja.md":                    "BOM",
	} {
		if !strings.Contains(string(mustRead(t, path)), want) {
			t.Errorf("%s does not say that a write of a text with nothing to read is refused (%q)", path, want)
		}
	}
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	const report = "The report, three hours of work.\n"
	cli := func(input string, args ...string) (string, error) {
		t.Helper()
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cmdHandoff(args) }) })
		return out, err
	}
	mcp := func(arguments string) (bool, string) {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(arguments)})
		reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)
		result, _ := reply["result"].(map[string]any)
		raw, _ := json.Marshal(result)
		return result["isError"] == true, string(raw)
	}
	kept := func() string {
		t.Helper()
		out, err := cli("", "read", "audit/report.md")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, err := cli(report, "write", "audit/report.md"); err != nil {
		t.Fatal(err)
	}
	mark := string([]byte{0xef, 0xbb, 0xbf}) // a byte order mark
	blanks := map[string]string{
		"nothing":                            "",
		"a line break":                       "\n",
		"spaces and a tab":                   " \t ",
		"a byte order mark":                  mark,
		"a byte order mark and a line break": mark + "\r\n",
		"a zero-width space":                 string([]byte{0xe2, 0x80, 0x8b}),
		"a full-width space":                 string([]byte{0xe3, 0x80, 0x80}),
		"a NUL":                              "\x00",
	}
	for name, blank := range blanks {
		if handoff.HasText(blank) {
			t.Errorf("%s counts as a text with something to read", name)
		}
		for _, operation := range []string{"write", "append"} {
			_, err := cli(blank, operation, "audit/report.md")
			if want := "standard input was empty: shikii handoff " + operation + " reads the text from it, and nothing was written\nusage: shikii handoff "; err == nil || !strings.HasPrefix(err.Error(), want) {
				t.Errorf("shikii handoff %s with %s on standard input is refused with the cause and the forms: %v", operation, name, err)
			}
			arguments, _ := json.Marshal(map[string]any{"operation": "handoff_" + operation, "path": "audit/report.md", "content": blank})
			refused, raw := mcp(string(arguments))
			if want := "content was empty: handoff_" + operation + " takes the text of the work record in it, and nothing was written"; !refused || !strings.Contains(raw, want) {
				t.Errorf("handoff_%s through the MCP tool with %s as content is refused with the cause: %s", operation, name, raw)
			}
			if got := kept(); got != report {
				t.Fatalf("after handoff %s with %s the record reads %q", operation, name, got)
			}
		}
	}
	// content left out altogether
	for _, operation := range []string{"handoff_write", "handoff_append"} {
		refused, raw := mcp(`{"operation":"` + operation + `","path":"audit/report.md"}`)
		if !refused || !strings.Contains(raw, "content was empty: "+operation+" takes the text of the work record in it, and nothing was written") || kept() != report {
			t.Errorf("%s without content is refused and the record stays: %s, the record reads %q", operation, raw, kept())
		}
	}
	// the place itself refuses it too, whoever calls it
	if _, err := (handoff.Book{Store: root}).Write("audit/report.md", mark, false); !errors.Is(err, handoff.ErrNoText) || kept() != report {
		t.Errorf("the place refuses a text with nothing to read: %v, the record reads %q", err, kept())
	}
	// a text with something to read is written, also behind a byte order mark, and so is one character
	for _, text := range []string{"x", mark + "Second report.\n"} {
		if !handoff.HasText(text) {
			t.Errorf("%q counts as a text with nothing to read", text)
		}
		if _, err := cli(text, "write", "audit/report.md"); err != nil || kept() != text {
			t.Errorf("a text with something to read is written: %v, the record reads %q", err, kept())
		}
	}
	if refused, raw := mcp(`{"operation":"handoff_append","path":"audit/report.md","content":"More.\n"}`); refused || kept() != mark+"Second report.\nMore.\n" {
		t.Errorf("an append through the MCP tool adds its text: %s, the record reads %q", raw, kept())
	}
}

// D-U4: an operation is told where its text goes only when it reads one. read prints a record and reads nothing, and was told
// that "the text goes on standard input".
func TestHandoffTellsOnlyAnOperationThatReadsTextWhereItGoes(t *testing.T) {
	for _, c := range []struct {
		call string
		want string
		text bool // the refusal says where the text goes
	}{
		{"read a b", "handoff read takes one path (one work record at a time)", false},
		{"read a b c", "handoff read takes one path (one work record at a time)", false},
		{"write a b", "handoff write takes one path (the text goes on standard input, not as an argument)", true},
		{"append a b", "handoff append takes one path (the text goes on standard input, not as an argument)", true},
		{"list x", "handoff list takes no arguments", false},
		{"read", "handoff read needs the path of a work record", false},
		{"write", "handoff write needs the path of a work record", false},
		{"read --all a", "handoff read takes no flags", false},
		{"delete a", `handoff has no operation "delete"`, false},
	} {
		got := handoffProblem(strings.Fields(c.call))
		if got != c.want {
			t.Errorf("shikii handoff %s is answered %q, want %q", c.call, got, c.want)
		}
		if strings.Contains(got, "standard input") != c.text {
			t.Errorf("shikii handoff %s: the refusal names standard input: %v, want %v (%q)", c.call, !c.text, c.text, got)
		}
		err := cmdHandoff(strings.Fields(c.call))
		if err == nil || !strings.HasPrefix(err.Error(), c.want+"\nusage: shikii handoff ") {
			t.Errorf("shikii handoff %s says what is wrong and then the forms: %v", c.call, err)
		}
	}
	// the operations that are told where the text goes are exactly the ones whose form in the help has a standard input
	for _, form := range agentUsages["handoff"].Forms {
		operation := strings.Fields(form.Call)[1]
		takes, known := handoffOperations[operation]
		if !known || takes.text != (form.Input != "") {
			t.Errorf("handoff %s: the help gives it a standard input: %v, handoffOperations says it reads a text: %v (known: %v)", operation, form.Input != "", takes.text, known)
		}
	}
	if len(handoffOperations) != len(agentUsages["handoff"].Forms) {
		t.Errorf("handoffOperations has %d operations and the help has %d forms", len(handoffOperations), len(agentUsages["handoff"].Forms))
	}
}

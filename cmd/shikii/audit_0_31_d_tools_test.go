package main

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Found while fixing the second round of the audit of 0.31.0: the entries the MCP tools shikii_decide, shikii_label and shikii_tune
// reach read their JSON on their own, and answered a value of the wrong type in encoding/json's words ("bad json: json: cannot
// unmarshal string into Go struct field .inputs of type map[string]string"). They keep passing over keys they do not know, and
// say what they cannot read in the reader's words, like the entries of shikii_wiki, shikii_review and shikii_reflect. The
// entries the page calls (redo, the tune actions, restoring answers, wiki claims) answered "bad json" the same way and read
// through the same reader now: no entry reads a JSON body on its own, but for the ones ownBodyReaders lists with their reason.

// toolWrongTypes is every MCP tool with arguments that hold one value of the wrong type, and the reader's sentence for it. A tool
// that sends no body has no arguments here.
var toolWrongTypes = map[string]struct{ arguments, want string }{
	"shikii_specs":   {},
	"shikii_status":  {},
	"shikii_decide":  {`{"spec":"needs-plan","inputs":"the request as text"}`, "pass the JSON inputs as an object"},
	"shikii_label":   {`{"trace_id":"0000000000000001","truth":"yes","weight":"heavy"}`, "pass the JSON weight as a number (got string)"},
	"shikii_tune":    {`{"spec":"needs-plan","propose":"three"}`, "pass the JSON propose as an integer (got string)"},
	"shikii_wiki":    {`{"operation":"prepare","work":"the ids as text"}`, "pass the JSON work as an object"},
	"shikii_review":  {`{"operation":"submit","trace_id":7}`, "pass the JSON trace_id as a string (got number)"},
	"shikii_reflect": {`{"operation":"submit","reviews":"one"}`, "pass the JSON reviews as an array"},
}

// toolsWithoutARow names the tools that have no row in the table, and the rows that name no tool.
func toolsWithoutARow(tools []map[string]any) (uncovered, idle []string) {
	named := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		named[name] = true
		if _, covered := toolWrongTypes[name]; !covered {
			uncovered = append(uncovered, name)
		}
	}
	for name := range toolWrongTypes {
		if !named[name] {
			idle = append(idle, name)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(idle)
	return uncovered, idle
}

// encodingJSONWords are words only encoding/json says ("bad json: " is these entries' own opening, so "json: " alone is not one).
var encodingJSONWords = []string{"json: cannot", "json: unknown", "json: invalid", "Go struct field", "Go value", "cannot unmarshal", "invalid character", "unexpected EOF", ": EOF"}

// ownBodyReaders are the functions that may read a JSON body with a decoder of their own, each with its reason. Every other
// function reads through decodeObject or decodeStrictObject, so what it cannot read is worded by the one reader.
var ownBodyReaders = map[string]string{
	"handleArticle":  "the page's edit of an article: read strictly, with one fixed answer",
	"handleNotebook": "reads the bytes under its own bound (a work record grows when encoded) and hands them to decodeStrictObject",
}

// bodyReaders lists the functions of the given Go sources that hand a Body to json.NewDecoder themselves.
// bodyReaders returns the functions that touch the body of a request (the Body of a net/http Request, read by its type, however the
// request is reached) in any way but the two
// the one reader allows: passing it to decodeObject, decodeReflect or decodeReview, and bounding it in place with
// r.Body = http.MaxBytesReader(w, r.Body, n). An allowlist of forms: a handler that reads its body with io.ReadAll, through a
// LimitReader or from a variable is found as much as one with json.NewDecoder (audit H of 0.31.0: only that form was found).
func bodyReaders(t *testing.T, sources map[string]string) []string {
	t.Helper()
	readers := map[string]bool{"decodeObject": true, "decodeReflect": true, "decodeReview": true}
	found := map[ast.Node]string{} // keyed by the declaration (its body), so two functions of one name are two
	for _, tf := range typedSources(t, sources, typedIsThePackage(sources)) {
		if tf.prelude {
			continue
		}
		info := tf.info
		// isBody: the Body field of a net/http Request, however the request is reached (a parameter, a variable, a field, an
		// embedded request, *r), read by its type
		isBody := func(e ast.Expr) bool {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" {
				return false
			}
			selection := info.Selections[sel]
			if selection == nil || selection.Kind() != types.FieldVal {
				return false
			}
			typ := selection.Recv()
			path := selection.Index()
			for _, i := range path[:len(path)-1] {
				st, ok := typedNamed(typ).Underlying().(*types.Struct)
				if !ok {
					return false
				}
				typ = st.Field(i).Type()
			}
			n := typedNamed(typ)
			return n != nil && n.Obj().Name() == "Request" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "net/http"
		}
		// the functions and methods that read the body of a request without its Body field
		readsTheBody := func(obj types.Object) bool {
			fn, ok := obj.(*types.Func)
			if !ok || fn.Pkg() == nil {
				return false
			}
			switch fn.Pkg().Path() {
			case "net/http/httputil":
				return fn.Name() == "DumpRequest" || fn.Name() == "DumpRequestOut"
			case "net/http":
				recv := fn.Type().(*types.Signature).Recv()
				if recv == nil || typedNamed(recv.Type()) == nil || typedNamed(recv.Type()).Obj().Name() != "Request" {
					return false
				}
				switch fn.Name() {
				case "ParseForm", "ParseMultipartForm", "FormValue", "PostFormValue", "FormFile", "MultipartReader", "Write", "WriteProxy":
					return true
				}
			}
			return false
		}
		// the bounding that is allowed: net/http's MaxBytesReader, by the object it denotes (a method of the same name is none)
		isMaxBytesReader := func(call *ast.CallExpr) bool {
			f, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			obj := info.Uses[f.Sel]
			fn, ok := obj.(*types.Func)
			return ok && fn.Name() == "MaxBytesReader" && fn.Pkg() != nil && fn.Pkg().Path() == "net/http" && fn.Type().(*types.Signature).Recv() == nil
		}
		typedDecls(tf.file, func(name string, root ast.Node) {
			ast.Inspect(root, func(n ast.Node) bool {
				if v, ok := n.(*ast.SelectorExpr); ok {
					obj := info.Uses[v.Sel]
					if sel := info.Selections[v]; sel != nil {
						obj = sel.Obj()
					}
					if obj != nil && readsTheBody(obj) {
						found[root] = name
					}
				}
				return true
			})
			parents := typedParents(root)
			for node, parent := range parents {
				sel, ok := node.(*ast.SelectorExpr)
				if !ok || !isBody(sel) {
					continue
				}
				allowed := false
				switch p := parent.(type) {
				case *ast.CallExpr:
					// handed to the one reader: the package's function of that name, by the object it denotes (a local function of the
					// same name is no reader)
					if id, ok := p.Fun.(*ast.Ident); ok && readers[id.Name] && len(p.Args) > 0 && p.Args[0] == sel {
						if fn, ok := info.Uses[id].(*types.Func); ok && fn.Pkg() != nil && fn.Parent() == fn.Pkg().Scope() {
							allowed = true
						}
					}
					if isMaxBytesReader(p) {
						if assign, ok := parents[p].(*ast.AssignStmt); ok && len(assign.Lhs) == 1 && isBody(assign.Lhs[0]) {
							allowed = true // r.Body = http.MaxBytesReader(w, r.Body, n)
						}
					}
				case *ast.AssignStmt:
					if len(p.Lhs) == 1 && p.Lhs[0] == sel && len(p.Rhs) == 1 {
						if call, ok := p.Rhs[0].(*ast.CallExpr); ok && isMaxBytesReader(call) {
							allowed = true // the left side of the same bounding
						}
					}
				}
				if !allowed {
					found[root] = name
				}
			}
		})
	}
	var names []string
	for _, name := range found {
		names = append(names, name) // a name twice: two functions of that name read a body (a method of another type)
	}
	sort.Strings(names)
	return names
}

func TestEveryToolAnswersWhatItCannotReadInTheReadersWords(t *testing.T) {
	sources := map[string]string{} // read before the fixture moves the working directory
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot list the sources of the package: %v", err)
	}
	for _, name := range files {
		if !strings.HasSuffix(name, "_test.go") {
			sources[name] = string(mustRead(t, name))
		}
	}
	// no entry reads a JSON body on its own, but for the ones listed with their reason
	readers := bodyReaders(t, sources)
	seen := map[string]bool{}
	for _, name := range readers {
		if seen[name] {
			t.Errorf("two functions named %s read a JSON body: the allowlist names one of them (a method of another type is a new reader)", name)
		}
		seen[name] = true
		if ownBodyReaders[name] == "" {
			t.Errorf("%s reads a JSON body with a decoder of its own: read it through decodeObject or decodeStrictObject, so the one reader words what it cannot read", name)
		}
	}
	if len(readers) != len(ownBodyReaders) {
		t.Errorf("the functions that read a body on their own are %v, and ownBodyReaders lists %d: a listed one no longer does", readers, len(ownBodyReaders))
	}
	// the negative example: a handler nobody listed is found
	added := bodyReaders(t, map[string]string{"new_handler.go": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n"})
	if len(added) != 1 || added[0] != "handleNew" || ownBodyReaders["handleNew"] != "" {
		t.Errorf("a new handler that reads its body on its own is not reported: %v", added)
	}
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: feedbackScope(repo)})
	// a value of the wrong type, through each tool
	for name, c := range toolWrongTypes {
		if c.arguments == "" {
			continue
		}
		params, _ := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(c.arguments)})
		result, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)["result"].(map[string]any)
		raw, _ := json.Marshal(result)
		if result["isError"] != true || !strings.Contains(string(raw), c.want) {
			t.Errorf("%s with %s is refused in the reader's words (%q): %s", name, c.arguments, c.want, raw)
		}
		for _, word := range encodingJSONWords {
			if strings.Contains(string(raw), word) {
				t.Errorf("%s with %s is answered in encoding/json's words (%q): %s", name, c.arguments, word, raw)
			}
		}
	}
	// the table is every tool
	if uncovered, idle := toolsWithoutARow(mcpTools); len(uncovered) > 0 || len(idle) > 0 {
		t.Errorf("tools without a row in toolWrongTypes: %v; rows that name no tool: %v", uncovered, idle)
	}
	// the negative example: a tool nobody listed is found
	if uncovered, _ := toolsWithoutARow(append([]map[string]any{{"name": "shikii_new"}}, mcpTools...)); len(uncovered) != 1 || uncovered[0] != "shikii_new" {
		t.Errorf("a new tool without a row is not reported: %v", uncovered)
	}
	// a body that is no JSON object, at the entries of the decision tools (an MCP client always sends an object; a caller of the
	// HTTP endpoint may not) and at the entries the page calls, which answered "bad json" in encoding/json's words as well
	for _, path := range []string{"/v1/decide", "/v1/label", "/v1/tune/sync", "/v1/tune/run", "/v1/tune/variant", "/v1/tune/adopt", "/v1/tune/review", "/v1/redo", "/v1/answers/restore", "/v1/wiki/claim"} {
		for body, want := range map[string]string{
			`{"spec":"needs-plan",}`: "the JSON is malformed (at byte 22)",
			`{"spec":"needs-plan"`:   "the JSON is malformed (at byte 20: the input ends there, before the JSON is closed; send the whole object)",
			"":                       "(got nothing)",
			"null":                   "(got null)",
			`["needs-plan"]`:         "(got array)",
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			var reply struct {
				Error string `json:"error"`
			}
			if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &reply) != nil || !strings.Contains(reply.Error, want) {
				t.Errorf("POST %s with %q answers %d %s, want 400 with %q", path, body, rec.Code, rec.Body.String(), want)
			}
			for _, word := range encodingJSONWords {
				if strings.Contains(reply.Error, word) {
					t.Errorf("POST %s with %q is answered in encoding/json's words (%q): %s", path, body, word, reply.Error)
				}
			}
		}
	}
	// these entries still pass over a key they do not know, and read the JSON behind a byte order mark: the reply is about the
	// decision, not about the key or the mark
	for _, body := range []string{`{"spec":"no-such-decision","inputs":{},"not_a_field":1}`, string([]byte{0xef, 0xbb, 0xbf}) + `{"spec":"no-such-decision","inputs":{}}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/decide", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "not_a_field") || strings.Contains(rec.Body.String(), "malformed") || !strings.Contains(rec.Body.String(), "no-such-decision") {
			t.Errorf("POST /v1/decide with %q answers about the decision: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

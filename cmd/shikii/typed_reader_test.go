package main

import (
	"os"
	"time"

	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The readers of the tests that keep a kind of code to an allowlist (who reads a request's body, who saves a source and under what
// title) read the code by its types, the way the compiler does. Readers by spelling missed a new spelling in every round of the
// audits of 0.31.0 (an element whose type is left out, a field set one by one, a copy, a pointer, an alias, a function literal, a
// field of a struct): each form is the same expression to the type checker.

type typedFile struct {
	name    string
	file    *ast.File
	info    *types.Info
	prelude bool // declarations a snippet uses without declaring them, not part of what is read
}

// The type checker finds the packages this one imports from the folder of the package (go/build's Dir), not from the working
// directory: a test that moves into a fixture folder still reads the code.
func init() {
	if wd, err := os.Getwd(); err == nil {
		build.Default.Dir = wd
	}
}

var (
	typedMu       sync.Mutex
	typedFset     = token.NewFileSet()
	typedImporter = importer.ForCompiler(typedFset, "source", nil)
)

const typedSnippetImports = "import (\n\t\"bytes\"\n\t\"encoding/json\"\n\t\"io\"\n\t\"net/http\"\n\n\t\"github.com/hayaenfj/shikii/internal/notebook\"\n)\n"

// typedPrelude declares what a snippet of cmd/shikii may use: each is added when the snippet does not declare it.
var typedPrelude = map[string]string{
	"workObservation":        "type workObservation struct {\n\tKind, Title, Content, Reference, Version string\n\treport bool\n}",
	"captureWorkObservation": "func captureWorkObservation(book notebook.Book, w any, o workObservation, save func() error, commit any) (string, string, error) {\n\treturn \"\", \"\", nil\n}",
	"captureWorkTrialSource": "func captureWorkTrialSource(book notebook.Book, w any, o workObservation, binding *string, key string, item any, save func() error, commit any) error {\n\treturn nil\n}",
	"server":                 "type server struct{}",
	"decodeObject":           "func decodeObject(r io.Reader, v any) error {\n\treturn nil\n}",
	"decodeReflect":          "func decodeReflect(r io.Reader) (any, error) {\n\treturn nil, nil\n}",
	"decodeReview":           "func decodeReview(r io.Reader) (any, error) {\n\treturn nil, nil\n}",
	"decodeStrictObject":     "func decodeStrictObject(data []byte, target any) (err error, extra bool) {\n\treturn nil, false\n}",
	"decodeStrict":           "func decodeStrict(data []byte, v any) error {\n\treturn nil\n}",
}

var typedPackageClause = regexp.MustCompile(`(?m)^package (\w+)`)

// typedSources type-checks the sources, one package per package clause. The code of the repository (strict) must check without an
// error, or a reader would be blind where the checker gave up. A snippet of a test (not strict) gets the imports and the
// declarations of typedPrelude it does not have, and its errors are let pass: what can be typed is typed.
func typedSources(t *testing.T, sources map[string]string, strict bool) []typedFile {
	t.Helper()
	typedMu.Lock()
	defer typedMu.Unlock()
	groups := map[string][]string{}
	for name, src := range sources {
		m := typedPackageClause.FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("%s has no package clause", name)
		}
		groups[m[1]] = append(groups[m[1]], name)
	}
	var out []typedFile
	for pkg, names := range groups {
		sort.Strings(names)
		var files []*ast.File
		var typed []typedFile
		joined := ""
		for _, name := range names {
			src := sources[name]
			if !strict && !strings.Contains(src, "\nimport") {
				clause := typedPackageClause.FindStringIndex(src)
				src = src[:clause[1]] + "\n\n" + typedSnippetImports + src[clause[1]:]
			}
			joined += src
			f, err := parser.ParseFile(typedFset, name, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
			typed = append(typed, typedFile{name: name, file: f})
		}
		if !strict && pkg == "main" {
			prelude := "package main\n\n" + typedSnippetImports
			for name, decl := range typedPrelude {
				if !regexp.MustCompile(`(?m)^(func|type) ` + name + `\b`).MatchString(joined) {
					prelude += "\n" + decl + "\n"
				}
			}
			f, err := parser.ParseFile(typedFset, "prelude_of_the_snippet.go", prelude, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
			typed = append(typed, typedFile{name: "prelude_of_the_snippet.go", file: f, prelude: true})
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		var errs []error
		conf := types.Config{Importer: typedImporter, Error: func(err error) { errs = append(errs, err) }}
		_, _ = conf.Check(pkg, typedFset, files, info)
		if strict && len(errs) > 0 {
			t.Fatalf("the code of package %s does not type-check, and a reader of it would be blind: %v", pkg, errs[0])
		}
		for i := range typed {
			typed[i].info = info
		}
		out = append(out, typed...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// typedNamed is the named type of t, through pointers and aliases.
func typedNamed(t types.Type) *types.Named {
	for t != nil {
		t = types.Unalias(t)
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
			continue
		}
		n, _ := t.(*types.Named)
		return n
	}
	return nil
}

// typedDecls calls visit with every function body and every package-level value of the file, named by the function or the variable
// (a handler written as a package variable holding a function literal is named by the variable).
func typedDecls(f *ast.File, visit func(name string, root ast.Node)) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body != nil {
				visit(d.Name.Name, d.Body)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for i, value := range vs.Values {
						name := "_"
						if i < len(vs.Names) {
							name = vs.Names[i].Name
						}
						visit(name, value)
					}
				}
			}
		}
	}
}

// typedParents maps every node under root to its parent.
func typedParents(root ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	stack := []ast.Node{}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// testWorkReport reports whether a source of this kind and title is a report or an archive a work wrote about itself.
func testWorkReport(kind, title string) bool {
	k, ok := notebook.WorkSourceOf(notebook.Observation{Kind: kind, Title: title})
	return ok && !k.Citable()
}

// testTitleRefusal is the refusal of an observation of the agent under this title (nil when it is taken).
func testTitleRefusal(title string) error {
	if _, ok := notebook.WorkSourceOf(notebook.Observation{Kind: "agent", Title: title}); ok {
		return notebook.Observation{Kind: "agent", Title: title, Content: "c", Reference: "r", Version: "v", ObservedAt: time.Now()}.ValidateObserved()
	}
	return nil
}

// typedUses reads the sources by their types and returns, for each use of an object that match accepts (a call, a value, a field
// or a method, however it is reached: an alias, a method value, an embedded field, a pointer), the file and the function or the
// package-level variable it is in ("file:function").
func typedUses(t *testing.T, sources map[string]string, strict bool, match func(types.Object) bool) []string {
	t.Helper()
	var found []string
	for _, tf := range typedSources(t, sources, strict) {
		if tf.prelude {
			continue
		}
		info := tf.info
		typedDecls(tf.file, func(name string, root ast.Node) {
			ast.Inspect(root, func(n ast.Node) bool {
				var obj types.Object
				switch v := n.(type) {
				case *ast.SelectorExpr:
					if sel := info.Selections[v]; sel != nil {
						obj = sel.Obj()
					} else {
						obj = info.Uses[v.Sel]
					}
					if obj != nil && match(obj) {
						found = append(found, tf.name+":"+name)
					}
					ast.Inspect(v.X, func(m ast.Node) bool { return true })
				case *ast.Ident:
					if obj = info.Uses[v]; obj != nil && match(obj) && !typedIsSelectorName(root, v) {
						found = append(found, tf.name+":"+name)
					}
				}
				return true
			})
		})
	}
	sort.Strings(found)
	return found
}

// typedIsSelectorName reports whether id is the name after the dot of a selector under root (counted with the selector).
func typedIsSelectorName(root ast.Node, id *ast.Ident) bool {
	inside := false
	ast.Inspect(root, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel == id {
			inside = true
		}
		return !inside
	})
	return inside
}

// typedFuncValues maps each variable defined from a function (join := filepath.Join) to that function, so a call through the
// variable is read as a call of the function.
func typedFuncValues(info *types.Info, root ast.Node) map[types.Object]types.Object {
	values := map[types.Object]types.Object{}
	ast.Inspect(root, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			var fn types.Object
			switch rhs := ast.Unparen(assign.Rhs[i]).(type) {
			case *ast.Ident:
				fn = info.Uses[rhs]
			case *ast.SelectorExpr:
				if sel := info.Selections[rhs]; sel != nil {
					fn = sel.Obj()
				} else {
					fn = info.Uses[rhs.Sel]
				}
			}
			if _, isFunc := fn.(*types.Func); isFunc {
				if def := info.Defs[id]; def != nil {
					values[def] = fn
				} else if use := info.Uses[id]; use != nil {
					values[use] = fn
				}
			}
		}
		return true
	})
	return values
}

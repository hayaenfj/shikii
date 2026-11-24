package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A file a test writes to be run must be made by writeStub (see exec_stub_test.go): the first run of a new file is slow on macOS, and
// the hook gives its git only hookGitTimeout. This is an allowlist, not a list of bad spellings: in the *_test.go files of this
// package, every call of os.WriteFile or os.OpenFile whose permission has an execute bit (any of 0o111), or whose permission this check
// cannot work out (a parameter, a field, a call), and every use of either function that is not a call (a method value), is refused,
// except in a function of execWriteAllowed (an entry with a third part allows only the permission written there). The permission is worked out from a number, os.FileMode(...) of one, os.ModePerm, | + & &^
// of those, a constant of the package, and a variable of the same function whose every assignment can be worked out.
//
// It does not look at os.Chmod: a file made with 0o644 and then chmod-ed is not seen, and chmod is also how the tests give a folder
// back its permissions (a dozen of them), so a ban on it would need an allowlist of folders, not of functions.
var execWriteAllowed = map[string]string{
	"exec_stub_test.go:writeStub":            "the one place that writes a script to be run, and runs it once before it is used",
	"exec_stub_test.go:writeUnrunExecutable": "a file with the execute bit that nothing runs, so there is no first run to be slow",
	// permissions that are not constants and are never an execute bit: file:function:the text of the permission (or of the use as a value)
	"audit_0_31_e_codex_test.go:TestAuditECodexPreviewAndInstallOverManyForms:mode": "the config file's mode comes from a table: 0o444 or 0o644",
	"init_settings_cases_test.go:TestUnwritableCodexConfigLetsInitGoOn:perm":        "a stand-in for the product's openPersonFile passes the perm it was given through",
	"init_settings_cases_test.go:TestUnwritableCodexConfigLetsInitGoOn:os.OpenFile": "the test puts the product's openPersonFile back to os.OpenFile",
}

// execWritesOutsideTheAllowlist parses the given files (name -> source) and returns where a file is written or opened with an execute
// bit, an unknown permission, or as a value, outside the functions of allowed; and which keys of allowed were used.
func execWritesOutsideTheAllowlist(t *testing.T, files map[string]string, allowed map[string]string) (found []string, used map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	consts := map[string]ast.Expr{}
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		parsed[name] = f
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, s := range g.Specs {
					v := s.(*ast.ValueSpec)
					for i, n := range v.Names {
						if i < len(v.Values) {
							consts[n.Name] = v.Values[i]
						}
					}
				}
			}
		}
	}
	used = map[string]bool{}
	names := make([]string, 0, len(parsed))
	for name := range parsed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := parsed[name]
		osName := ""
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "os" {
				osName = "os"
				if imp.Name != nil {
					osName = imp.Name.Name
				}
			}
		}
		if osName == "" || osName == "_" || osName == "." {
			continue
		}
		for _, d := range f.Decls {
			var fn *ast.FuncDecl
			owner := ""
			if fd, ok := d.(*ast.FuncDecl); ok {
				fn, owner = fd, fd.Name.Name
			}
			key := filepath.Base(name) + ":" + owner
			report := func(pos token.Pos, what, perm string) {
				for _, k := range []string{key, key + ":" + perm} {
					if _, ok := allowed[k]; ok {
						used[k] = true
						return
					}
				}
				found = append(found, fmt.Sprintf("%s: %s", fset.Position(pos), what))
			}
			callees := map[ast.Expr]bool{}
			ast.Inspect(d, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if osFunction(x.Fun, osName) {
						callees[x.Fun] = true
						if len(x.Args) != 3 {
							report(x.Pos(), "a call whose permission is not the third argument", "")
						} else if v, ok := evalPermission(x.Args[2], fn, consts, 0); !ok {
							report(x.Pos(), "the permission of "+types.ExprString(x.Fun)+" cannot be worked out: "+types.ExprString(x.Args[2]), types.ExprString(x.Args[2]))
						} else if v&0o111 != 0 {
							report(x.Pos(), fmt.Sprintf("%s writes a file with an execute bit (%#o): use writeStub", types.ExprString(x.Fun), v), types.ExprString(x.Args[2]))
						}
					}
				case *ast.SelectorExpr:
					if osFunction(x, osName) && !callees[x] {
						report(x.Pos(), types.ExprString(x)+" is used as a value, so its permission is not seen", types.ExprString(x))
					}
				}
				return true
			})
		}
	}
	return found, used
}

// osFunction says whether e is os.WriteFile or os.OpenFile (the package under the name the file imports it by).
func osFunction(e ast.Expr, osName string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || (s.Sel.Name != "WriteFile" && s.Sel.Name != "OpenFile") {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == osName
}

// evalPermission works out the number an expression of a permission stands for, or says that it cannot.
func evalPermission(e ast.Expr, fn *ast.FuncDecl, consts map[string]ast.Expr, depth int) (int64, bool) {
	if depth > 8 {
		return 0, false
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.INT {
			return 0, false
		}
		v, err := strconv.ParseInt(x.Value, 0, 64)
		return v, err == nil
	case *ast.ParenExpr:
		return evalPermission(x.X, fn, consts, depth+1)
	case *ast.CallExpr: // os.FileMode(0o755), fs.FileMode(...), FileMode(...)
		name := ""
		switch f := x.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		if name == "FileMode" && len(x.Args) == 1 {
			return evalPermission(x.Args[0], fn, consts, depth+1)
		}
	case *ast.SelectorExpr:
		if x.Sel.Name == "ModePerm" {
			return 0o777, true
		}
	case *ast.BinaryExpr:
		l, lok := evalPermission(x.X, fn, consts, depth+1)
		r, rok := evalPermission(x.Y, fn, consts, depth+1)
		if !lok || !rok {
			return 0, false
		}
		switch x.Op {
		case token.OR, token.ADD:
			return l | r, true
		case token.AND:
			return l & r, true
		case token.AND_NOT:
			return l &^ r, true
		}
	case *ast.Ident:
		if fn != nil && fn.Body != nil {
			if v, ok, found := evalLocalVariable(x.Name, fn, consts, depth); found {
				return v, ok
			}
		}
		if c, ok := consts[x.Name]; ok {
			return evalPermission(c, fn, consts, depth+1)
		}
	}
	return 0, false
}

// evalLocalVariable works out a variable of fn from every place it is given a value (name := v, name = v, var name = v): the bits of all of
// them together. found is false when fn never gives the name a value (a parameter, a constant of the package), and ok is false when one of the
// values cannot be worked out.
func evalLocalVariable(name string, fn *ast.FuncDecl, consts map[string]ast.Expr, depth int) (v int64, ok, found bool) {
	ok = true
	take := func(e ast.Expr) {
		found = true
		w, good := evalPermission(e, fn, consts, depth+1)
		v |= w
		ok = ok && good
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for i, l := range s.Lhs {
				if id, isID := l.(*ast.Ident); isID && id.Name == name {
					if len(s.Lhs) == len(s.Rhs) && (s.Tok == token.DEFINE || s.Tok == token.ASSIGN) {
						take(s.Rhs[i])
					} else {
						found, ok = true, false
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range s.Names {
				if id.Name == name {
					if i < len(s.Values) {
						take(s.Values[i])
					} else {
						found, ok = true, false
					}
				}
			}
		}
		return true
	})
	return v, ok, found
}

// readPackageTestSources reads the *_test.go files of this package (go test runs in its folder).
func readPackageTestSources(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("*_test.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no test files found here: %v", err)
	}
	files := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = string(b)
	}
	return files
}

func TestAnExecutableWrittenOutsideTheStubHelpersIsRefused(t *testing.T) {
	found, used := execWritesOutsideTheAllowlist(t, readPackageTestSources(t), execWriteAllowed)
	if len(found) > 0 {
		t.Errorf("%d files are written with an execute bit (or with a permission that cannot be checked) outside writeStub and writeUnrunExecutable:\n%s", len(found), strings.Join(found, "\n"))
	}
	for key := range execWriteAllowed {
		if !used[key] {
			t.Errorf("%s is in execWriteAllowed and writes no file with an execute bit or an unknown permission: take it out", key)
		}
	}
}

// The check itself: each case must be found (or not) as written, so a check that sees nothing cannot pass.
func TestWhatTheExecutableWriteAllowlistMustFind(t *testing.T) {
	const head = "package main\nimport \"os\"\n"
	for _, c := range []struct {
		name, file, src string
		want            int
	}{
		{"an executable script in a test", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"the old spelling of the number", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0755) }\n", 1},
		{"only the owner's execute bit", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o700) }\n", 1},
		{"only the group's execute bit", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o610) }\n", 1},
		{"os.FileMode of a number", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, os.FileMode(0o755)) }\n", 1},
		{"a number with another one added by or", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o600|0o100) }\n", 1},
		{"os.ModePerm", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, os.ModePerm) }\n", 1},
		{"a constant of the package", "x_test.go", head + "const runnable = 0o755\nfunc TestX() { os.WriteFile(\"a\", nil, runnable) }\n", 1},
		{"a constant built from another constant", "x_test.go", head + "const (\n\tplain = 0o644\n\trunnable = plain | 0o111\n)\nfunc TestX() { os.WriteFile(\"a\", nil, runnable) }\n", 1},
		{"a variable of the function", "x_test.go", head + "func TestX() { perm := os.FileMode(0o755); os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a variable set to a plain number and then to an executable one", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); perm = 0o755; os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a variable declared with var", "x_test.go", head + "func TestX() { var perm os.FileMode = 0o755; os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a parameter", "x_test.go", head + "func put(perm os.FileMode) { os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a field", "x_test.go", head + "type c struct{ mode os.FileMode }\nfunc put(f c) { os.WriteFile(\"a\", nil, f.mode) }\n", 1},
		{"a call", "x_test.go", head + "func mode() os.FileMode { return 0o644 }\nfunc TestX() { os.WriteFile(\"a\", nil, mode()) }\n", 1},
		{"os.OpenFile", "x_test.go", head + "func TestX() { os.OpenFile(\"a\", os.O_CREATE, 0o755) }\n", 1},
		{"os under another name", "x_test.go", "package main\nimport o \"os\"\nfunc TestX() { o.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"os.WriteFile kept as a value", "x_test.go", head + "func TestX() { write := os.WriteFile; write(\"a\", nil, 0o755) }\n", 1},
		{"os.OpenFile kept in a variable of the package", "x_test.go", head + "var open = os.OpenFile\n", 1},
		{"a file-level variable that calls it", "x_test.go", head + "var made = func() error { return os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"inside a closure of a test", "x_test.go", head + "func TestX() { func() { os.WriteFile(\"a\", nil, 0o755) }() }\n", 1},
		{"two of them", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o755); os.WriteFile(\"b\", nil, 0o700) }\n", 2},
		{"a plain file", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o644) }\n", 0},
		{"a private file", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o600) }\n", 0},
		{"a file nobody can use", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0) }\n", 0},
		{"a constant that is not executable", "x_test.go", head + "const plain = 0o644\nfunc TestX() { os.WriteFile(\"a\", nil, plain) }\n", 0},
		{"the execute bit taken out", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o755&^0o111) }\n", 0},
		{"a folder", "x_test.go", head + "func TestX() { os.MkdirAll(\"a\", 0o755); os.Chmod(\"a\", 0o755) }\n", 0},
		{"a file that does not use os", "x_test.go", "package main\nfunc TestX() { o := struct{ WriteFile func(string, []byte, int) }{}; o.WriteFile(\"a\", nil, 0o755) }\n", 0},
		{"writeStub itself", "exec_stub_test.go", head + "func writeStub() { os.WriteFile(\"a\", nil, 0o755) }\n", 0},
		{"writeUnrunExecutable itself", "exec_stub_test.go", head + "func writeUnrunExecutable() { os.WriteFile(\"a\", nil, 0o755) }\n", 0},
		{"a closure inside writeStub", "exec_stub_test.go", head + "func writeStub() { func() { os.WriteFile(\"a\", nil, 0o755) }() }\n", 0},
		{"writeStub's name in another file", "x_test.go", head + "func writeStub() { os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"another function in writeStub's file", "exec_stub_test.go", head + "func other() { os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"a file-level variable in writeStub's file", "exec_stub_test.go", head + "var made = func() error { return os.WriteFile(\"a\", nil, 0o755) }\n", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			found, _ := execWritesOutsideTheAllowlist(t, map[string]string{c.file: c.src}, execWriteAllowed)
			if len(found) != c.want {
				t.Fatalf("found %d (want %d): %v", len(found), c.want, found)
			}
		})
	}
}

// An entry of the allowlist that is no longer needed is found, so the list cannot only grow.
func TestAnAllowlistEntryThatWritesNothingExecutableIsFound(t *testing.T) {
	allowed := map[string]string{"x_test.go:TestX": "a test that writes a plain file"}
	_, used := execWritesOutsideTheAllowlist(t, map[string]string{"x_test.go": "package main\nimport \"os\"\nfunc TestX() { os.WriteFile(\"a\", nil, 0o644) }\n"}, allowed)
	if used["x_test.go:TestX"] {
		t.Fatal("an entry of the allowlist that is for a function with no such write is counted as used")
	}
	_, used = execWritesOutsideTheAllowlist(t, map[string]string{"x_test.go": "package main\nimport \"os\"\nfunc TestX() { os.WriteFile(\"a\", nil, 0o755) }\n"}, allowed)
	if !used["x_test.go:TestX"] {
		t.Fatal("an entry of the allowlist for a function that writes an executable file is not counted as used")
	}
}

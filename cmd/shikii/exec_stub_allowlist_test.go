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
// the hook gives its git only hookGitTimeout. This is an allowlist, not a list of bad spellings. In the *_test.go files of this package
// these are refused, except in a function of execWriteAllowed (an entry with a third part allows only the permission written there):
//   - a call of os.WriteFile, os.OpenFile, os.Chmod or io/ioutil.WriteFile (under every name the file imports the package by) whose
//     permission has an execute bit (any of 0o111), or whose permission this check cannot work out (a parameter, a field, a call);
//   - a call of os.CopyFS, which has no permission to look at: it writes each file with the permission of the file it copies, so it can
//     write one with an execute bit whatever the call says (an entry with the third part os.CopyFS allows it in one function);
//   - a use of one of those functions that is not a call (a method value), because its permission is not seen;
//   - an import of os or io/ioutil with a dot, because a call of its functions then looks like a call of any function (no entry allows it).
//
// The permission is worked out from a number, os.FileMode(...) of one, os.ModePerm, + | & &^ of those, a constant of the package, and a
// variable whose every assignment can be worked out. A name is looked up from the function that holds the use outwards (a closure first,
// then the functions around it): a parameter, a result or a receiver of one of them is unknown (it hides what is outside, even a constant
// of the package of the same name), a variable or a constant that one of them declares is worked out, and only a name that none of them
// declares is a constant of the package. A variable whose address is taken, that is incremented, or that is changed by op= is unknown.
// A variable that is given more than one value stands for all of them together, which is the most the permission can have (the bits of
// every value), and that is right only where more bits mean more bits in the result: + is worked out as a real sum, and the right side of
// &^ takes bits away, so both need a value that is one number: a variable given more than one value there is unknown.
// There is no type check (it is slow), so a local variable that hides the package's name os is not told from the package.
//
// os.Chmod is checked like the writes, and the tests do use it to give a folder its permissions back (a folder needs the execute bit),
// so each such function is listed with the permission it gives. The text of the permission names the permission, not the call, so an
// entry for a chmod of 0o755 also lets a write of 0o755 pass in that function: keep an entry's function small. A file that a test makes
// executable and never runs is made by writeUnrunExecutable, not by a chmod. Not seen: (*os.File).Chmod, the methods of os.Root, syscall,
// and a command that is run to chmod.
var execWriteAllowed = map[string]string{
	"exec_stub_test.go:writeStub":            "the one place that writes a script to be run, and runs it once before it is used",
	"exec_stub_test.go:writeUnrunExecutable": "a file with the execute bit that nothing runs, so there is no first run to be slow",
	// permissions that are not constants and are never an execute bit: file:function:the text of the permission (or of the use as a value)
	"audit_0_31_e_codex_test.go:TestAuditECodexPreviewAndInstallOverManyForms:mode": "the config file's mode comes from a table: 0o444 or 0o644",
	"init_settings_cases_test.go:TestUnwritableCodexConfigLetsInitGoOn:perm":        "a stand-in for the product's openPersonFile passes the perm it was given through",
	"init_settings_cases_test.go:TestUnwritableCodexConfigLetsInitGoOn:os.OpenFile": "the test puts the product's openPersonFile back to os.OpenFile",
	// os.Chmod of a folder: a folder needs the execute bit to be entered (the permission is a constant, so the entry names its text)
	"audit_0_11_confirm_test.go:TestConfirm011DecideReturnsTheRecordedDecisionWhenTheKnowledgeCannotBeRead:0o700": "gives a folder its permissions back (a folder needs the execute bit)",
	"audit_0_36_test.go:TestAFailingToolLessonAddsNothingAndRefusesNothing:0o500":                                 "makes a folder read-only but enterable, so a write into it fails",
	"audit_0_36_test.go:TestAFailingToolLessonAddsNothingAndRefusesNothing:0o755":                                 "gives the folder its permissions back",
	"codex_toml_test.go:TestRe2InitGoesOnPastUnwritableCodexFolderOnFix:0o555":                                    "makes a folder read-only but enterable, so a write into it fails",
	"codex_toml_test.go:TestRe2InitGoesOnPastUnwritableCodexFolderOnFix:0o755":                                    "gives the folder its permissions back",
	"hook_git_layout_test.go:TestAGitFolderGitWouldRefuseIsNotDerived:0o755":                                      "gives the objects folder and the refs folder their permissions back (a folder needs the execute bit)",
	"hook_setup_cases_test.go:TestHookSecondAuditRound:0o755":                                                     "other.txt is a text file that nothing runs: only the change of its mode is looked at, as a change of the tree",
	"needs_plan_copy_safety_test.go:TestReadOnlyDecisionsFolderDoesNotStopSeeding:0o555":                          "makes a folder read-only but enterable, so a write into it fails",
	"needs_plan_copy_safety_test.go:TestReadOnlyDecisionsFolderDoesNotStopSeeding:0o755":                          "gives the folder its permissions back",
	"opencode_plugin_test.go:TestHookInstallGoesOnWhenTheOpenCodePluginCannotBeWritten:0o555":                     "makes a folder read-only but enterable, so a write into it fails",
	"opencode_plugin_test.go:TestHookInstallGoesOnWhenTheOpenCodePluginCannotBeWritten:0o755":                     "gives the folder its permissions back",
	"project_scope_registered_test.go:TestStorePlacedAboveByHandGetsNoRecordOfTheSession:0o777":                   "makes two folders writable by everyone (a umask would cut them), so a store planted above the project is as open as a shared one",
	"project_test.go:TestRe1RelinkFailingAtProjectJSONRecoversNextTime:0o500":                                     "makes a folder read-only but enterable, so a write into it fails",
	"project_test.go:TestRe1RelinkFailingAtProjectJSONRecoversNextTime:0o700":                                     "gives the folder its permissions back",
	"work_missed_test.go:TestRecordStaysCompleteWhenTyingTheMissesFails:0o500":                                    "makes a folder read-only but enterable, so a write into it fails",
	"work_missed_test.go:TestRecordStaysCompleteWhenTyingTheMissesFails:0o700":                                    "gives the folder its permissions back",
}

// execScope is a function that holds a use: its receiver, its parameters and results (typ), and its body.
type execScope struct {
	recv *ast.FieldList
	typ  *ast.FuncType
	body *ast.BlockStmt
}

// declares says whether the receiver, a parameter or a result of the function has this name.
func (s execScope) declares(name string) bool {
	for _, fields := range []*ast.FieldList{s.recv, s.typ.Params, s.typ.Results} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			for _, id := range field.Names {
				if id.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// inspectWithScopes walks root like ast.Inspect and tells visit the functions around each node, the innermost first, after the ones of
// base. When visit returns false the node's children are not visited.
func inspectWithScopes(root ast.Node, base []execScope, visit func(n ast.Node, scopes []execScope) bool) {
	scopes := base
	var pushed []bool
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			if pushed[len(pushed)-1] {
				scopes = scopes[1:]
			}
			pushed = pushed[:len(pushed)-1]
			return true
		}
		push := false
		switch f := n.(type) {
		case *ast.FuncDecl:
			scopes, push = append([]execScope{{recv: f.Recv, typ: f.Type, body: f.Body}}, scopes...), true
		case *ast.FuncLit:
			scopes, push = append([]execScope{{typ: f.Type, body: f.Body}}, scopes...), true
		}
		descend := visit(n, scopes)
		if !descend && push {
			scopes = scopes[1:]
		} else {
			pushed = append(pushed, push)
		}
		return descend
	})
}

// execPermissionArg is the position of the permission among the arguments of each function the check looks at; a function with no
// permission among its arguments (os.CopyFS, which copies the permission of the files) is -1, and every call of it is reported.
var execPermissionArg = map[string]int{"WriteFile": 2, "OpenFile": 2, "Chmod": 1, "CopyFS": -1}

// execWritesOutsideTheAllowlist parses the given files (name -> source) and returns where a file is written, opened or chmod-ed with an
// execute bit, an unknown permission, or as a value, outside the functions of allowed; and which keys of allowed were used.
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
		// every name the file gives os and io/ioutil (the same package may be imported under two names)
		pkgNames := map[string]bool{}
		dotImport := false
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			local := map[string]string{"os": "os", "io/ioutil": "ioutil"}[p]
			if local == "" {
				continue
			}
			if imp.Name != nil {
				local = imp.Name.Name
			}
			switch local {
			case "_": // nothing of the package can be used through it
			case ".":
				if !dotImport {
					found = append(found, fmt.Sprintf("%s: %s is imported with a dot, so its calls are not seen", fset.Position(imp.Pos()), p))
				}
				dotImport = true
			default:
				pkgNames[local] = true
			}
		}
		if len(pkgNames) == 0 {
			continue
		}
		for _, d := range f.Decls {
			owner := ""
			if fd, ok := d.(*ast.FuncDecl); ok {
				owner = fd.Name.Name
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
			inspectWithScopes(d, nil, func(n ast.Node, scopes []execScope) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if fname, ok := permissionFunction(x.Fun, pkgNames); ok {
						callees[x.Fun] = true
						at := execPermissionArg[fname]
						if at < 0 {
							report(x.Pos(), types.ExprString(x.Fun)+" writes each file with the permission of the file it copies, which may have an execute bit: copy with writeStub, or list the function with os.CopyFS", types.ExprString(x.Fun))
						} else if len(x.Args) != at+1 {
							report(x.Pos(), fmt.Sprintf("a call of %s whose permission is not argument %d", types.ExprString(x.Fun), at+1), "")
						} else if v, ok := evalPermission(x.Args[at], scopes, consts, 0, false); !ok {
							report(x.Pos(), "the permission of "+types.ExprString(x.Fun)+" cannot be worked out: "+types.ExprString(x.Args[at]), types.ExprString(x.Args[at]))
						} else if v&0o111 != 0 && fname == "Chmod" {
							report(x.Pos(), fmt.Sprintf("%s gives an execute bit (%#o): make a file to be run with writeStub, or one never run with writeUnrunExecutable", types.ExprString(x.Fun), v), types.ExprString(x.Args[at]))
						} else if v&0o111 != 0 {
							report(x.Pos(), fmt.Sprintf("%s writes a file with an execute bit (%#o): use writeStub", types.ExprString(x.Fun), v), types.ExprString(x.Args[at]))
						}
					}
				case *ast.SelectorExpr:
					if _, ok := permissionFunction(x, pkgNames); ok && !callees[x] {
						report(x.Pos(), types.ExprString(x)+" is used as a value, so its permission is not seen", types.ExprString(x))
					}
				}
				return true
			})
		}
	}
	return found, used
}

// permissionFunction says whether e is os.WriteFile, os.OpenFile, os.Chmod or ioutil.WriteFile (the package under a name the file imports
// it by, any of them), and which function it is.
func permissionFunction(e ast.Expr, pkgNames map[string]bool) (string, bool) {
	s, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if _, known := execPermissionArg[s.Sel.Name]; !known {
		return "", false
	}
	id, ok := s.X.(*ast.Ident)
	if !ok || !pkgNames[id.Name] {
		return "", false
	}
	return s.Sel.Name, true
}

// evalPermission works out the number an expression of a permission stands for, or says that it cannot. scopes are the functions that
// hold the expression, the innermost first. A variable that is given more than one value stands for the bits of all of them together
// (at most the bits it has), unless exact is set: then the expression must be one number, and such a variable is unknown. + and the right
// side of &^ are worked out with exact set, since more bits there do not mean more bits in the result (0o644+0o044 carries into an execute
// bit; 0o755&^0o111 loses them).
func evalPermission(e ast.Expr, scopes []execScope, consts map[string]ast.Expr, depth int, exact bool) (int64, bool) {
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
		return evalPermission(x.X, scopes, consts, depth+1, exact)
	case *ast.CallExpr: // os.FileMode(0o755), fs.FileMode(...), FileMode(...)
		name := ""
		switch f := x.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		if name == "FileMode" && len(x.Args) == 1 {
			return evalPermission(x.Args[0], scopes, consts, depth+1, exact)
		}
	case *ast.SelectorExpr:
		if x.Sel.Name == "ModePerm" {
			return 0o777, true
		}
	case *ast.BinaryExpr:
		lExact, rExact := exact || x.Op == token.ADD, exact || x.Op == token.ADD || x.Op == token.AND_NOT
		l, lok := evalPermission(x.X, scopes, consts, depth+1, lExact)
		r, rok := evalPermission(x.Y, scopes, consts, depth+1, rExact)
		if !lok || !rok {
			return 0, false
		}
		switch x.Op {
		case token.OR:
			return l | r, true
		case token.ADD:
			return l + r, true
		case token.AND:
			return l & r, true
		case token.AND_NOT:
			return l &^ r, true
		}
	case *ast.Ident:
		for i, s := range scopes {
			if s.declares(x.Name) {
				return 0, false
			}
			if v, ok, found := evalLocalVariable(x.Name, scopes[i:], consts, depth, exact); found {
				return v, ok
			}
		}
		if c, ok := consts[x.Name]; ok {
			return evalPermission(c, nil, consts, depth+1, exact)
		}
	}
	return 0, false
}

// evalLocalVariable works out a variable of the function scopes[0] from every place it is given a value (name := v, name = v, var name = v,
// const name = v): the bits of all of them together. A closure inside that declares the name itself is left out, since what it assigns is
// its own. found is false when the function never gives the name a value (a constant of the package, or a name of a function around it),
// and ok is false when one of the values cannot be worked out, or the variable is changed in a way that is not an assignment of a value,
// or, with exact set, when the variable is given more than one value (it is not one number).
func evalLocalVariable(name string, scopes []execScope, consts map[string]ast.Expr, depth int, exact bool) (v int64, ok, found bool) {
	ok = true
	if scopes[0].body == nil {
		return 0, true, false
	}
	unknown := func() { found, ok = true, false }
	values := 0
	inspectWithScopes(scopes[0].body, scopes, func(n ast.Node, at []execScope) bool {
		take := func(e ast.Expr) {
			found = true
			values++
			w, good := evalPermission(e, at, consts, depth+1, exact)
			v |= w
			ok = ok && good && !(exact && values > 1)
		}
		isName := func(e ast.Expr) bool {
			id, isID := e.(*ast.Ident)
			return isID && id.Name == name
		}
		switch s := n.(type) {
		case *ast.FuncLit:
			return !at[0].declares(name)
		case *ast.AssignStmt:
			for i, l := range s.Lhs {
				if isName(l) {
					if len(s.Lhs) == len(s.Rhs) && (s.Tok == token.DEFINE || s.Tok == token.ASSIGN) {
						take(s.Rhs[i])
					} else {
						unknown()
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range s.Names {
				if id.Name == name {
					if i < len(s.Values) {
						take(s.Values[i])
					} else {
						unknown()
					}
				}
			}
		case *ast.RangeStmt:
			if (s.Key != nil && isName(s.Key)) || (s.Value != nil && isName(s.Value)) {
				unknown()
			}
		case *ast.IncDecStmt:
			if isName(s.X) {
				unknown()
			}
		case *ast.UnaryExpr:
			if s.Op == token.AND && isName(s.X) {
				unknown()
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
		{"a folder", "x_test.go", head + "func TestX() { os.MkdirAll(\"a\", 0o755); os.Mkdir(\"b\", 0o755) }\n", 0},
		{"a file that does not use os", "x_test.go", "package main\nfunc TestX() { o := struct{ WriteFile func(string, []byte, int) }{}; o.WriteFile(\"a\", nil, 0o755) }\n", 0},
		{"writeStub itself", "exec_stub_test.go", head + "func writeStub() { os.WriteFile(\"a\", nil, 0o755) }\n", 0},
		{"writeUnrunExecutable itself", "exec_stub_test.go", head + "func writeUnrunExecutable() { os.WriteFile(\"a\", nil, 0o755) }\n", 0},
		{"a closure inside writeStub", "exec_stub_test.go", head + "func writeStub() { func() { os.WriteFile(\"a\", nil, 0o755) }() }\n", 0},
		{"writeStub's name in another file", "x_test.go", head + "func writeStub() { os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"another function in writeStub's file", "exec_stub_test.go", head + "func other() { os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"a file-level variable in writeStub's file", "exec_stub_test.go", head + "var made = func() error { return os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		// the six ways round that the first version had (a name that is not looked up where it is used, a file that is skipped, a call that is not looked at)
		{"a parameter named like a constant of the package", "x_test.go", head + "const perm = 0o644\nfunc put(perm os.FileMode) { os.WriteFile(\"a\", nil, perm) }\nfunc TestX() { put(0o755) }\n", 1},
		{"a parameter of a closure that hides a variable of the function", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); func(perm os.FileMode) { os.WriteFile(\"a\", nil, perm) }(0o755) }\n", 1},
		{"a dot import of os", "x_test.go", "package main\nimport . \"os\"\nfunc TestX() { WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"io/ioutil.WriteFile", "x_test.go", "package main\nimport \"io/ioutil\"\nfunc TestX() { ioutil.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"a chmod after a plain write", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o644); os.Chmod(\"a\", 0o755) }\n", 1},
		{"os imported twice, under two names", "x_test.go", "package main\nimport (\n\t\"os\"\n\to \"os\"\n)\nfunc TestX() { os.WriteFile(\"a\", nil, 0o755) }\n", 1},
		// audit of 0.42.1: a sum that carries into an execute bit, a mask taken away that is not always the same, and a copy that keeps the permission
		{"an addition that carries into the execute bit", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o644+0o044) }\n", 1},
		{"an addition whose sum has no execute bit though an or of the two would", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o377+0o001) }\n", 0},
		{"an addition whose sum has no execute bit", "x_test.go", head + "func TestX() { os.WriteFile(\"a\", nil, 0o600+0o044) }\n", 0},
		{"an addition of a variable that is not always the same", "x_test.go", head + "func TestX(c bool) { m := os.FileMode(0o044); if c { m = 0 }; os.WriteFile(\"a\", nil, 0o644+m) }\n", 1},
		{"an addition of a variable that is set once", "x_test.go", head + "func TestX() { m := os.FileMode(0o004); os.WriteFile(\"a\", nil, 0o640+m) }\n", 0},
		{"a mask taken away that is not always set", "x_test.go", head + "func TestX(c bool) { m := os.FileMode(0o111); if c { m = 0 }; os.WriteFile(\"a\", nil, 0o755&^m) }\n", 1},
		{"a mask taken away that is set once", "x_test.go", head + "func TestX() { m := os.FileMode(0o111); os.WriteFile(\"a\", nil, 0o755&^m) }\n", 0},
		{"a mask taken away that is a constant", "x_test.go", head + "const m = 0o111\nfunc TestX() { os.WriteFile(\"a\", nil, 0o755&^m) }\n", 0},
		{"a mask taken away through a variable made from a variable that is not always set", "x_test.go", head + "func TestX(c bool) { m := os.FileMode(0o111); if c { m = 0 }; n := m; os.WriteFile(\"a\", nil, 0o755&^n) }\n", 1},
		{"a mask taken away that holds an addition of a variable that is not always set", "x_test.go", head + "func TestX(c bool) { m := os.FileMode(0o100); if c { m = 0 }; os.WriteFile(\"a\", nil, 0o755&^(m+0o011)) }\n", 1},
		{"a variable that is not always set on the left of a mask taken away", "x_test.go", head + "func TestX(c bool) { m := os.FileMode(0o755); if c { m = 0o644 }; os.WriteFile(\"a\", nil, m&^0o001) }\n", 1},
		{"a variable that is not always set, kept by and", "x_test.go", head + "func TestX(c bool) { m := os.FileMode(0o111); if c { m = 0 }; os.WriteFile(\"a\", nil, 0o755&m) }\n", 1},
		{"os.CopyFS of an executable file", "x_test.go", "package main\nimport (\n\t\"os\"\n\t\"testing/fstest\"\n)\nfunc TestX() { os.CopyFS(\"d\", fstest.MapFS{\"a\": {Mode: 0o755}}) }\n", 1},
		{"os.CopyFS of a plain file", "x_test.go", "package main\nimport (\n\t\"os\"\n\t\"testing/fstest\"\n)\nfunc TestX() { os.CopyFS(\"d\", fstest.MapFS{\"a\": {Mode: 0o644}}) }\n", 1},
		{"os.CopyFS of a folder of the machine", "x_test.go", head + "func TestX() { os.CopyFS(\"d\", os.DirFS(\"s\")) }\n", 1},
		{"os.CopyFS under another name", "x_test.go", "package main\nimport o \"os\"\nfunc TestX() { o.CopyFS(\"d\", o.DirFS(\"s\")) }\n", 1},
		{"os.CopyFS kept as a value", "x_test.go", head + "var copyFS = os.CopyFS\n", 1},
		{"os.CopyFS in a closure", "x_test.go", head + "func TestX() { func() { os.CopyFS(\"d\", os.DirFS(\"s\")) }() }\n", 1},
		{"os.DirFS alone", "x_test.go", head + "func TestX() { _ = os.DirFS(\"s\") }\n", 0},
		// the same holes in other forms, and what must stay quiet
		{"a range variable named like a constant of the package", "x_test.go", head + "const perm = 0o644\nfunc TestX() { for _, perm := range []os.FileMode{0o755} { os.WriteFile(\"a\", nil, perm) } }\n", 1},
		{"a named result named like a constant of the package", "x_test.go", head + "const perm = 0o644\nfunc put() (perm os.FileMode) { os.WriteFile(\"a\", nil, perm); return }\n", 1},
		{"a closure parameter named like a constant of the package", "x_test.go", head + "const perm = 0o644\nfunc TestX() { func(perm os.FileMode) { os.WriteFile(\"a\", nil, perm) }(0o755) }\n", 1},
		{"a file-level closure whose parameter is named like a constant", "x_test.go", head + "const perm = 0o644\nvar made = func(perm os.FileMode) error { return os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a variable given its value in a closure from the closure's parameter", "x_test.go", head + "const x = 0o644\nfunc TestX() { perm := os.FileMode(0o644); func(x os.FileMode) { perm = x }(0o755); os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a variable whose address is taken", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); p := &perm; *p = 0o755; os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a variable that is incremented", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); perm++; os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a variable changed by op=", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); perm |= 0o111; os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a closure that has its own variable of the name", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); func(perm os.FileMode) { perm = 0o755; _ = perm }(0o600); os.WriteFile(\"a\", nil, perm) }\n", 0},
		{"a constant of the function", "x_test.go", head + "func TestX() { const perm = 0o755; os.WriteFile(\"a\", nil, perm) }\n", 1},
		{"a dot import of io/ioutil", "x_test.go", "package main\nimport . \"io/ioutil\"\nfunc TestX() { WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"a dot import of os in a file that writes nothing", "x_test.go", "package main\nimport . \"os\"\nfunc TestX() { Getenv(\"a\") }\n", 1},
		{"io/ioutil under another name", "x_test.go", "package main\nimport u \"io/ioutil\"\nfunc TestX() { u.WriteFile(\"a\", nil, 0o755) }\n", 1},
		{"both names of os used", "x_test.go", "package main\nimport (\n\t\"os\"\n\to \"os\"\n)\nfunc TestX() { os.WriteFile(\"a\", nil, 0o755); o.WriteFile(\"b\", nil, 0o755) }\n", 2},
		{"a chmod with the owner's execute bit", "x_test.go", head + "func TestX() { os.Chmod(\"a\", 0o700) }\n", 1},
		{"a chmod of os.FileMode of an executable number", "x_test.go", head + "func TestX() { os.Chmod(\"a\", os.FileMode(0o755)) }\n", 1},
		{"a chmod with a parameter", "x_test.go", head + "func put(m os.FileMode) { os.Chmod(\"a\", m) }\n", 1},
		{"a chmod with one argument too few", "x_test.go", head + "func put(a ...any) { os.Chmod(a...) }\n", 1},
		{"os.Chmod kept as a value", "x_test.go", head + "var chmod = os.Chmod\n", 1},
		{"a chmod inside a closure", "x_test.go", head + "func TestX() { t := func() { os.Chmod(\"a\", 0o755) }; t() }\n", 1},
		{"a chmod to a plain mode", "x_test.go", head + "func TestX() { os.Chmod(\"a\", 0o644) }\n", 0},
		{"a chmod that takes every permission away", "x_test.go", head + "func TestX() { os.Chmod(\"a\", 0) }\n", 0},
		{"a chmod to read only", "x_test.go", head + "func TestX() { os.Chmod(\"a\", 0o444) }\n", 0},
		{"io/ioutil.WriteFile of a plain file", "x_test.go", "package main\nimport \"io/ioutil\"\nfunc TestX() { ioutil.WriteFile(\"a\", nil, 0o644) }\n", 0},
		{"a dot import of another package", "x_test.go", "package main\nimport . \"fmt\"\nfunc TestX() { Println(\"a\") }\n", 0},
		{"a variable of the function read by a closure", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); func() { os.WriteFile(\"a\", nil, perm) }() }\n", 0},
		{"a closure with a parameter that is not the permission", "x_test.go", head + "func TestX() { perm := os.FileMode(0o644); func(name string) { os.WriteFile(name, nil, perm) }(\"a\") }\n", 0},
		{"a constant of the package next to a function with a parameter of that name", "x_test.go", head + "const perm = 0o644\nfunc other(perm os.FileMode) {}\nfunc TestX() { os.WriteFile(\"a\", nil, perm) }\n", 0},
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

// An entry of the allowlist for a chmod names the file, the function and the permission as written, so another function, another file
// or another permission is still found.
func TestAChmodWithAnExecuteBitIsAllowedOnlyWhereTheAllowlistSaysSo(t *testing.T) {
	const src = "package main\nimport \"os\"\nfunc TestX() { os.Chmod(\"a\", 0o755) }\nfunc TestY() { os.Chmod(\"a\", 0o755) }\n"
	const reason = "gives a folder its permissions back"
	for _, c := range []struct {
		name     string
		allowed  map[string]string
		want     int
		wantUsed string // the keys that must be counted as used, joined by ","
	}{
		{"nothing allowed", map[string]string{}, 2, ""},
		{"TestX allowed for this permission", map[string]string{"x_test.go:TestX:0o755": reason}, 1, "x_test.go:TestX:0o755"},
		{"both allowed for this permission", map[string]string{"x_test.go:TestX:0o755": reason, "x_test.go:TestY:0o755": reason}, 0, "x_test.go:TestX:0o755,x_test.go:TestY:0o755"},
		{"TestX allowed for another permission", map[string]string{"x_test.go:TestX:0o700": reason}, 2, ""},
		{"another function allowed", map[string]string{"x_test.go:TestZ:0o755": reason}, 2, ""},
		{"another file allowed", map[string]string{"y_test.go:TestX:0o755": reason}, 2, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			found, used := execWritesOutsideTheAllowlist(t, map[string]string{"x_test.go": src}, c.allowed)
			if len(found) != c.want {
				t.Fatalf("found %d (want %d): %v", len(found), c.want, found)
			}
			var usedKeys []string
			for key := range used {
				usedKeys = append(usedKeys, key)
			}
			sort.Strings(usedKeys)
			if got := strings.Join(usedKeys, ","); got != c.wantUsed {
				t.Fatalf("used entries = %q, want %q", got, c.wantUsed)
			}
		})
	}
}

// os.CopyFS writes each file with the permission of the file it copies, so it can write one with an execute bit whatever the call says. It
// is refused everywhere except in a function the allowlist names for it (file:function:os.CopyFS), like a chmod.
func TestACopyFSIsAllowedOnlyWhereTheAllowlistSaysSo(t *testing.T) {
	const src = "package main\nimport \"os\"\nfunc TestX() { os.CopyFS(\"d\", os.DirFS(\"s\")) }\nfunc TestY() { os.CopyFS(\"d\", os.DirFS(\"s\")) }\n"
	const reason = "copies a folder of plain text files"
	for _, c := range []struct {
		name     string
		allowed  map[string]string
		want     int
		wantUsed string // the keys that must be counted as used, joined by ","
	}{
		{"nothing allowed", map[string]string{}, 2, ""},
		{"TestX allowed for os.CopyFS", map[string]string{"x_test.go:TestX:os.CopyFS": reason}, 1, "x_test.go:TestX:os.CopyFS"},
		{"the whole of TestX allowed", map[string]string{"x_test.go:TestX": reason}, 1, "x_test.go:TestX"},
		{"both allowed for os.CopyFS", map[string]string{"x_test.go:TestX:os.CopyFS": reason, "x_test.go:TestY:os.CopyFS": reason}, 0, "x_test.go:TestX:os.CopyFS,x_test.go:TestY:os.CopyFS"},
		{"TestX allowed for another call", map[string]string{"x_test.go:TestX:os.Chmod": reason}, 2, ""},
		{"another function allowed", map[string]string{"x_test.go:TestZ:os.CopyFS": reason}, 2, ""},
		{"another file allowed", map[string]string{"y_test.go:TestX:os.CopyFS": reason}, 2, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			found, used := execWritesOutsideTheAllowlist(t, map[string]string{"x_test.go": src}, c.allowed)
			if len(found) != c.want {
				t.Fatalf("found %d (want %d): %v", len(found), c.want, found)
			}
			var usedKeys []string
			for key := range used {
				usedKeys = append(usedKeys, key)
			}
			sort.Strings(usedKeys)
			if got := strings.Join(usedKeys, ","); got != c.wantUsed {
				t.Fatalf("used entries = %q, want %q", got, c.wantUsed)
			}
		})
	}
}

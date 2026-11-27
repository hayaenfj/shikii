package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A patch that is saved goes through patchWithOutcome (reflect_place.go): it reads the placement state before, calls notebook.Book.Patch and
// attaches the truths to the place decisions after, the way ingestWithOutcome does for a save. An entry that calls Book.Patch itself would
// save a patch whose draft was placed and leave its decisions without a truth, and no test would notice (0.44.1 audit W7). This is an
// allowlist, not a list of entries: every use of a method named Patch in the non-test files of this package is refused, except in the
// function listed below for that kind of use. PreviewPatch (a preview saves nothing) is another name and is not looked at.
//
// A use is one of two kinds. A call (`b.Patch(p)`, also `notebook.Book.Patch(b, p)`) saves a patch. A value (`f := b.Patch`, a method value or
// expression that is not called here) can be called anywhere later, so it is held to the same list. The check has no type information, so the
// request's own field Patch (a *notebook.Patch that a request carries) has the same shape as a method value: the functions that read or make
// that field are listed with the kind value, and a function listed for one kind may not use the other. The type notebook.Patch (the name of
// the package the file imports internal/notebook by, followed by Patch) is not a use of the method and is skipped.
const (
	patchCall  = "call"
	patchValue = "value"
)

var patchUseAllowed = map[string]string{
	"patchWithOutcome:" + patchCall: "the one place that saves a patch: reads the placement state first and attaches the truths after",
	"runNotebook:" + patchValue:     "reads the Patch field of the request (a *notebook.Patch), for reconcile, compare and the patch operation, which saves through patchWithOutcome",
	"cmdNotebook:" + patchValue:     "makes the Patch field of the request from standard input",
}

// notebookPackage is the path of the package whose type Patch is not a method.
const notebookPackage = "github.com/hayaenfj/shikii/internal/notebook"

// patchUses parses the given files (name -> source) and returns "<function>:<kind>" for every use of a selector named Patch with the places
// it sits at; a use outside any function is under "(package level)".
func patchUses(t *testing.T, files map[string]string) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	out := map[string][]string{}
	for name, src := range files {
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		notebookNames := map[string]bool{} // the names this file calls the notebook package by
		for _, spec := range file.Imports {
			if path, _ := strconv.Unquote(spec.Path.Value); path == notebookPackage {
				name := "notebook"
				if spec.Name != nil {
					name = spec.Name.Name
				}
				notebookNames[name] = true
			}
		}
		for _, decl := range file.Decls {
			caller := "(package level)"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				caller = fn.Name.Name
			}
			called := map[*ast.SelectorExpr]bool{}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr: // a call is visited before its function, so the selector is known to be called when it is reached
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Patch" {
						if pkg, ok := sel.X.(*ast.Ident); ok && notebookNames[pkg.Name] {
							return true // a conversion notebook.Patch(v) of the type
						}
						called[sel] = true
						out[caller+":"+patchCall] = append(out[caller+":"+patchCall], fset.Position(sel.Pos()).String())
					}
				case *ast.SelectorExpr:
					if pkg, ok := x.X.(*ast.Ident); ok && notebookNames[pkg.Name] {
						return true // notebook.Patch, the type
					}
					if x.Sel.Name == "Patch" && !called[x] {
						out[caller+":"+patchValue] = append(out[caller+":"+patchValue], fset.Position(x.Pos()).String())
					}
				}
				return true
			})
		}
	}
	return out
}

// patchUsesOutsideTheList returns the uses of Patch in functions that are not listed for that kind of use, sorted.
func patchUsesOutsideTheList(uses map[string][]string, allowed map[string]string) []string {
	var out []string
	for key, places := range uses {
		if _, ok := allowed[key]; !ok {
			out = append(out, fmt.Sprintf("%s at %s", key, strings.Join(places, ", ")))
		}
	}
	slices.Sort(out)
	return out
}

func TestOnlyPatchWithOutcomeSavesAPatchInTheCommandLineProgram(t *testing.T) {
	files := map[string]string{}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(data)
	}
	uses := patchUses(t, files)
	if problems := patchUsesOutsideTheList(uses, patchUseAllowed); len(problems) > 0 {
		t.Errorf("these use a method named Patch in a function that is not on the list; a patch that is saved goes through patchWithOutcome, so the truths of the decisions asked about the draft are attached (list a function in patchUseAllowed only with the reason): %v", problems)
	}
	for key := range patchUseAllowed {
		if len(uses[key]) == 0 {
			t.Errorf("%s is on the list but no longer does that: remove it from the list", key)
		}
	}

	// Negative examples: a call in a function that is not on the list, a call by method expression, a method value, a held method value, a
	// call in a function that is listed only for the value kind, and a value in one that is listed only for the call kind are found; a
	// preview and the request's field read in the function listed for it are not.
	src := "package main\n\n" +
		"import nb \"" + notebookPackage + "\"\n\n" +
		"func sneakyCall(b nb.Book, p nb.Patch) { b.Patch(p) }\n\n" +
		"func sneakyExpression(b nb.Book, p nb.Patch) { nb.Book.Patch(b, p) }\n\n" +
		"func sneakyValue(b nb.Book) { f := b.Patch; _ = f }\n\n" +
		"var sneakyHeld = nb.Book{}.Patch\n\n" +
		"func runNotebook(b nb.Book, p nb.Patch, req request) { b.Patch(p); _ = req.Patch }\n\n" +
		"func patchWithOutcome(b nb.Book, p nb.Patch) { f := b.Patch; _ = f }\n\n" +
		"func cmdNotebook(b nb.Book, p nb.Patch) { b.PreviewPatch(p); _ = nb.Patch{} }\n"
	got := patchUsesOutsideTheList(patchUses(t, map[string]string{"sneaky.go": src}), patchUseAllowed)
	var names []string
	for _, line := range got {
		names = append(names, strings.SplitN(line, " at ", 2)[0])
	}
	want := []string{
		"(package level):" + patchValue,
		"patchWithOutcome:" + patchValue,
		"runNotebook:" + patchCall,
		"sneakyCall:" + patchCall,
		"sneakyExpression:" + patchCall,
		"sneakyValue:" + patchValue,
	}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("the scan does not find every use of Patch in the functions that are not listed for it:\n got  %v\n want %v", names, want)
	}
}

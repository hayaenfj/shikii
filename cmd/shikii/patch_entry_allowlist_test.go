package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A patch that is saved goes through patchWithOutcome (reflect_place.go): it reads the placement state before, calls notebook.Book.Patch and
// attaches the truths to the place decisions after, the way ingestWithOutcome does for a save. An entry that calls Book.Patch itself would
// save a patch whose draft was placed and leave its decisions without a truth, and no test would notice (0.44.1 audit W7).
//
// This is an allowlist found by type, not by name: the non-test files of this package are type-checked, and every use of the method
// (github.com/hayaenfj/shikii/internal/notebook.Book).Patch is refused except the call inside patchWithOutcome. A use is found by what
// the type checker says the selector denotes (types.Info.Selections), so none of these depends on how the file spells it: a package imported
// under another name, a variable named like the package (`notebook := nb.Book{}; notebook.Patch(p)`), a method expression, a method held
// as a value, a method promoted through a struct that embeds a Book. What is not that method is not found, whatever it is called: the
// request's field Patch, the type notebook.Patch, and PreviewPatch (a preview saves nothing).
//
// A use is one of two kinds. A call (`b.Patch(p)`, `notebook.Book.Patch(b, p)`) saves a patch. A value (`f := b.Patch`, a method value or
// expression that is not called here) can be called anywhere later, so it is held to the same list: a function listed for the call may not
// hold the value.
//
// Out of reach (as for the root-open check): a Book reached through an interface or a type parameter is not the method of Book, and a file
// that is not built on this machine has no types, so the check refuses to run while one exists.
const (
	patchCall  = "call"
	patchValue = "value"
)

var patchUseAllowed = map[string]string{
	"patchWithOutcome:" + patchCall: "the one place that saves a patch: reads the placement state first and attaches the truths after",
}

// notebookPackage is the path of the package whose Book has the method Patch.
const notebookPackage = "github.com/hayaenfj/shikii/internal/notebook"

// commandProgram is the import path of the command-line program (package main).
const commandProgram = "github.com/hayaenfj/shikii/cmd/shikii"

// commandProgramSources returns the export data of everything the program imports and the program's own Go files that are not tests,
// named by their path from the repository's top folder.
func commandProgramSources(t *testing.T) (map[string]string, []namedSource) {
	t.Helper()
	root, pkgs, exp := listRootOpenPackages(t)
	for _, p := range pkgs {
		if p.ImportPath != commandProgram {
			continue
		}
		if len(p.CgoFiles) > 0 {
			t.Fatalf("%s uses cgo, which this check does not type-check", p.ImportPath)
		}
		for _, name := range p.IgnoredGoFiles {
			if !strings.HasSuffix(name, "_test.go") {
				t.Fatalf("%s is not built on this machine, so it has no types here and the check cannot see a use of Patch in it", name)
			}
		}
		var srcs []namedSource
		for _, name := range p.GoFiles {
			srcs = append(srcs, readSource(t, root, filepath.Join(p.Dir, name)))
		}
		return exp, srcs
	}
	t.Fatalf("package %s is not among the listed packages", commandProgram)
	return nil, nil
}

// isBookPatch reports whether a selector denotes the method Patch of notebook.Book (a value or a pointer receiver), as a method value or
// a method expression.
func isBookPatch(sel *types.Selection) bool {
	if sel == nil || sel.Kind() != types.MethodVal && sel.Kind() != types.MethodExpr {
		return false
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok || fn.Name() != "Patch" || fn.Pkg() == nil || fn.Pkg().Path() != notebookPackage {
		return false
	}
	recv := fn.Signature().Recv().Type()
	if p, ok := recv.(*types.Pointer); ok {
		recv = p.Elem()
	}
	named, ok := recv.(*types.Named)
	return ok && named.Obj().Name() == "Book"
}

// bookPatchUses type-checks the files of one package and returns "<function>:<kind>" for every use of the method notebook.Book.Patch
// with the places it sits at; a use outside any function is under "(package level)" and one in a method under "method <name>".
func bookPatchUses(t *testing.T, exp map[string]string, path string, srcs []namedSource) map[string][]string {
	t.Helper()
	fset, files, info := typeCheckSources(t, exp, path, srcs)
	out := map[string][]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			caller := "(package level)"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				caller = fn.Name.Name
				if fn.Recv != nil {
					caller = "method " + caller
				}
			}
			called := map[*ast.SelectorExpr]bool{}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr: // a call is visited before its function, so the selector is known to be called when it is reached
					if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok {
						called[sel] = true
					}
				case *ast.SelectorExpr:
					if isBookPatch(info.Selections[x]) {
						kind := patchValue
						if called[x] {
							kind = patchCall
						}
						out[caller+":"+kind] = append(out[caller+":"+kind], fset.Position(x.Sel.Pos()).String())
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
	exp, srcs := commandProgramSources(t)
	if len(srcs) == 0 {
		t.Fatal("the check must see the Go files of the program")
	}
	uses := bookPatchUses(t, exp, commandProgram, srcs)
	if problems := patchUsesOutsideTheList(uses, patchUseAllowed); len(problems) > 0 {
		t.Errorf("these use the method notebook.Book.Patch in a function that is not on the list; a patch that is saved goes through patchWithOutcome, so the truths of the decisions asked about the draft are attached (list a function in patchUseAllowed only with the reason): %v", problems)
	}
	for key := range patchUseAllowed {
		if len(uses[key]) == 0 {
			t.Errorf("%s is on the list but no longer does that: remove it from the list", key)
		}
	}
}

// The check itself, on the real package with two files added to it (so the types are the program's own): each use below must be found (or
// not) as written, so a check that sees nothing cannot pass. The names start with zz so they do not meet a name of the program.
func TestThePatchAllowlistFindsWhatItMustFind(t *testing.T) {
	exp, srcs := commandProgramSources(t)
	srcs = append(srcs,
		// the package imported under another name
		namedSource{"cmd/shikii/zz_alias.go", "package main\n\n" +
			"import nb \"" + notebookPackage + "\"\n\n" +
			"type zzHolder struct{ nb.Book }\n\n" +
			"var zzHeld = nb.Book{}.Patch\n\n" +
			"func zzCall(b nb.Book, p nb.Patch) { b.Patch(p) }\n\n" +
			"func zzPointer(b *nb.Book, p nb.Patch) { b.Patch(p) }\n\n" +
			"func zzExpression(b nb.Book, p nb.Patch) { nb.Book.Patch(b, p) }\n\n" +
			"func zzPointerExpression(b *nb.Book, p nb.Patch) { (*nb.Book).Patch(b, p) }\n\n" +
			"func zzValue(b nb.Book) { f := b.Patch; _ = f }\n\n" +
			"func zzNamedLikeThePackage(p nb.Patch) { notebook := nb.Book{}; notebook.Patch(p) }\n\n" +
			"func zzPromoted(h zzHolder, p nb.Patch) { h.Patch(p) }\n\n" +
			"func zzListedForTheCall(b nb.Book, p nb.Patch) { b.Patch(p); f := b.Patch; _ = f }\n\n" +
			// not the method: the request's field, the type, a conversion of it and the preview
			"func zzNotTheMethod(b nb.Book, p nb.Patch, req notebookRequest) { _ = req.Patch; _ = nb.Patch{}; _ = nb.Patch(p); b.PreviewPatch(p) }\n\n" +
			"func (zzHolder) zzMethodPatch(b nb.Book, p nb.Patch) { b.Patch(p) }\n"},
		// the package imported under its own name, and a variable of that name that hides it
		namedSource{"cmd/shikii/zz_own_name.go", "package main\n\n" +
			"import \"" + notebookPackage + "\"\n\n" +
			"func zzOwnName(p notebook.Patch) { notebook := notebook.Book{}; notebook.Patch(p) }\n"})
	allowed := map[string]string{"patchWithOutcome:" + patchCall: "the real one", "zzListedForTheCall:" + patchCall: "listed for the call only"}
	uses := bookPatchUses(t, exp, commandProgram, srcs)
	var found []string
	for _, line := range patchUsesOutsideTheList(uses, allowed) {
		found = append(found, strings.SplitN(line, " at ", 2)[0])
	}
	want := []string{
		"(package level):" + patchValue,
		"method zzMethodPatch:" + patchCall,
		"zzCall:" + patchCall,
		"zzExpression:" + patchCall,
		"zzListedForTheCall:" + patchValue,
		"zzNamedLikeThePackage:" + patchCall,
		"zzOwnName:" + patchCall,
		"zzPointer:" + patchCall,
		"zzPointerExpression:" + patchCall,
		"zzPromoted:" + patchCall,
		"zzValue:" + patchValue,
	}
	if !slices.Equal(found, want) {
		t.Fatalf("the check does not find every use of Patch outside the list, and nothing else:\n got  %v\n want %v", found, want)
	}
	// the program's own use stays the only one in its files
	if got := uses["patchWithOutcome:"+patchCall]; len(got) != 1 {
		t.Fatalf("the program's own call in patchWithOutcome must be seen once: %v", got)
	}
}

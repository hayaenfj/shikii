package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// os.Root.OpenFile follows a link in the last name even with O_NOFOLLOW, so every open of an *os.Root goes through
// rootfile.OpenNoFollow. This is an allowlist, not a list of bad spellings: any OpenFile that is not the os package's own function
// (a call, a method value or a method expression such as (*os.Root).OpenFile) is refused, except inside rootfile.OpenNoFollow itself
// and in a call whose flags name O_EXCL (create-exclusive already refuses any link, a dangling one too).
const rootOpenerFile, rootOpenerFunc = "internal/rootfile/rootfile.go", "OpenNoFollow"

func TestOnlyTheNoFollowOpenerOpensAFileInARoot(t *testing.T) {
	root := strings.TrimSpace(run(t, "git", "rev-parse", "--show-toplevel"))
	out := run(t, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z", "--", "cmd", "internal") // tracked and new files, as on disk
	var hits []string
	files, opener := 0, 0
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue // deleted in the working tree
		}
		files++
		found, allowed := rootOpensOutsideTheAllowlist(t, filepath.ToSlash(name), string(src))
		hits = append(hits, found...)
		opener += allowed
	}
	if files == 0 || opener != 1 {
		t.Fatalf("the check must see the Go files (%d) and exactly one open inside %s %s (%d)", files, rootOpenerFile, rootOpenerFunc, opener)
	}
	if len(hits) > 0 {
		t.Fatalf("an *os.Root is opened without rootfile.OpenNoFollow in %d places (it follows a link even with O_NOFOLLOW):\n%s", len(hits), strings.Join(hits, "\n"))
	}
}

// The check itself: each case must be found (or not) as written, so a check that sees nothing cannot pass.
func TestTheRootOpenAllowlistFindsWhatItMustFind(t *testing.T) {
	for _, c := range []struct {
		name, file, src string
		want, allowed   int
	}{
		{"a call on a root", "cmd/shikii/x.go", "package main\nimport (\"os\"; \"syscall\")\nfunc f(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY|syscall.O_NOFOLLOW, 0) }\n", 1, 0},
		{"a call split over lines", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) {\n\tr.OpenFile(\n\t\t\"a\",\n\t\tos.O_CREATE|os.O_RDWR,\n\t\t0o600)\n}\n", 1, 0},
		{"a method value", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { open := r.OpenFile; open(\"a\", os.O_RDONLY, 0) }\n", 1, 0},
		{"a method expression", "internal/store/x.go", "package store\nimport \"os\"\nvar open = (*os.Root).OpenFile\n", 1, 0},
		{"flags passed in a variable", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root, flags int) { r.OpenFile(\"a\", flags, 0) }\n", 1, 0},
		{"the opener's name in another file", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc OpenNoFollow(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY, 0) }\n", 1, 0},
		{"another function in the opener's file", rootOpenerFile, "package rootfile\nimport \"os\"\nfunc other(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY, 0) }\n", 1, 0},
		{"the opener itself", rootOpenerFile, "package rootfile\nimport \"os\"\nfunc OpenNoFollow(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY, 0) }\n", 0, 1},
		{"create-exclusive cleared", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root, flags int) { r.OpenFile(\"a\", flags&^os.O_EXCL, 0) }\n", 1, 0},
		{"create-exclusive", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.OpenFile(\"a\", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) }\n", 0, 0},
		{"the os package's function", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f() { os.OpenFile(\"a\", os.O_RDONLY, 0) }\nvar open = os.OpenFile\n", 0, 0},
		{"the os package under another name", "cmd/shikii/x.go", "package main\nimport o \"os\"\nfunc f() { o.OpenFile(\"a\", o.O_RDONLY, 0) }\n", 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			found, allowed := rootOpensOutsideTheAllowlist(t, c.file, c.src)
			if len(found) != c.want || allowed != c.allowed {
				t.Fatalf("found %d (want %d) %v, allowed opener opens %d (want %d)", len(found), c.want, found, allowed, c.allowed)
			}
		})
	}
}

// rootOpensOutsideTheAllowlist returns each OpenFile in src that is not os.OpenFile and is not allowed, and how many are inside the
// opener itself.
func rootOpensOutsideTheAllowlist(t *testing.T, name, src string) ([]string, int) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	osNames := map[string]bool{}
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == "os" {
			if imp.Name != nil {
				osNames[imp.Name.Name] = true
			} else {
				osNames["os"] = true
			}
		}
	}
	var hits []string
	allowed := 0
	for _, decl := range f.Decls {
		inOpener := false
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == rootOpenerFunc && name == rootOpenerFile {
			inOpener = true
		}
		exclusive := map[*ast.SelectorExpr]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "OpenFile" && len(call.Args) >= 2 && namesExclusive(call.Args[1]) {
					exclusive[sel] = true
				}
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "OpenFile" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && osNames[id.Name] {
				return true // os.OpenFile refuses a link with O_NOFOLLOW
			}
			switch {
			case inOpener:
				allowed++
			case exclusive[sel]:
			default:
				hits = append(hits, fset.Position(sel.Pos()).String())
			}
			return true
		})
	}
	return hits, allowed
}

// namesExclusive reports whether a flags expression names O_EXCL (os.O_EXCL, syscall.O_EXCL or a bare O_EXCL) other than as a
// flag being cleared (x &^ O_EXCL).
func namesExclusive(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op == token.AND_NOT {
				found = found || namesExclusive(v.X)
				return false
			}
		case *ast.SelectorExpr:
			found = found || v.Sel.Name == "O_EXCL"
		case *ast.Ident:
			found = found || v.Name == "O_EXCL"
		}
		return !found
	})
	return found
}

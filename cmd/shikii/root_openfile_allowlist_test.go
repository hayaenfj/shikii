package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// os.Root follows a link in the last name of a file it opens, creates, reads or writes, even with O_NOFOLLOW, so every such use goes
// through rootfile.OpenNoFollow. This is an allowlist found by type, not a list of bad spellings: every use (a call, a method value or
// a method expression, also through a struct that embeds a root) of a method of os.Root in rootFileMethods, and every use of
// os.OpenInRoot, is refused, except inside rootfile.OpenNoFollow itself and in an OpenFile call whose flags are a constant with both
// O_CREATE and O_EXCL (create-exclusive refuses any link at the name, a dangling one too; O_EXCL without O_CREATE follows it).
const rootOpenerFile, rootOpenerFunc = "internal/rootfile/rootfile.go", "OpenNoFollow"

// rootFileMethods are the methods of os.Root that open, create, read or write a file by its name. FS is one too: the fs.FS it returns
// opens a file following a link in its last name. A use of FS is allowed only as the first argument of io/fs.WalkDir from "." written
// in place (a walk from the root, which opens folders only and does not go into a link); a root kept in an interface or a type parameter is out of this check's
// reach (it sees the use of a method of os.Root, not a call through another type), so do not hand an *os.Root to one.
var rootFileMethods = map[string]bool{"OpenFile": true, "Open": true, "Create": true, "ReadFile": true, "WriteFile": true, "FS": true}

// listedPackage is the part of `go list -json` the check reads.
type listedPackage struct {
	ImportPath, Dir, Export           string
	GoFiles, CgoFiles, IgnoredGoFiles []string
	DepOnly                           bool
}

var rootOpenList struct {
	once sync.Once
	root string
	pkgs []listedPackage // the packages under cmd and internal
	exp  map[string]string
	err  error
}

// listRootOpenPackages lists the packages under cmd and internal and the compiler's export data of every package they import (one
// `go list -export -deps`; the build cache the test was just built with makes it about a second), so each package is type-checked
// from its source against the types its imports really have, without another module.
func listRootOpenPackages(t *testing.T) (string, []listedPackage, map[string]string) {
	t.Helper()
	l := &rootOpenList
	l.once.Do(func() {
		l.root = strings.TrimSpace(run(t, "git", "rev-parse", "--show-toplevel"))
		cmd := exec.Command("go", "list", "-deps", "-export", "-json=ImportPath,Dir,Export,GoFiles,CgoFiles,IgnoredGoFiles,DepOnly", "./cmd/...", "./internal/...")
		cmd.Dir = l.root
		out, err := cmd.Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				err = fmt.Errorf("%v: %s", err, exit.Stderr)
			}
			l.err = err
			return
		}
		l.exp = map[string]string{}
		dec := json.NewDecoder(strings.NewReader(string(out)))
		for {
			var p listedPackage
			if err := dec.Decode(&p); err == io.EOF {
				break
			} else if err != nil {
				l.err = err
				return
			}
			if p.Export != "" {
				l.exp[p.ImportPath] = p.Export
			}
			if !p.DepOnly {
				l.pkgs = append(l.pkgs, p)
			}
		}
	})
	if l.err != nil {
		t.Fatalf("go list: %v", l.err)
	}
	return l.root, l.pkgs, l.exp
}

func TestOnlyTheNoFollowOpenerOpensAFileInARoot(t *testing.T) {
	root, pkgs, exp := listRootOpenPackages(t)
	var hits []string
	files, opener := 0, 0
	for _, p := range pkgs {
		if len(p.CgoFiles) > 0 {
			t.Fatalf("%s uses cgo, which this check does not type-check", p.ImportPath)
		}
		var srcs []namedSource
		for _, name := range p.GoFiles {
			srcs = append(srcs, readSource(t, root, filepath.Join(p.Dir, name)))
		}
		files += len(srcs)
		found, allowed := rootOpensInPackage(t, exp, p.ImportPath, srcs)
		hits = append(hits, found...)
		opener += allowed
		for _, name := range p.IgnoredGoFiles { // not built on this machine (another system's file), so it has no types here
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			src := readSource(t, root, filepath.Join(p.Dir, name))
			files++
			hits = append(hits, rootOpenerNamesInAFileNotBuilt(t, src)...)
		}
	}
	if files == 0 || opener != 1 {
		t.Fatalf("the check must see the Go files (%d) and exactly one open inside %s %s (%d)", files, rootOpenerFile, rootOpenerFunc, opener)
	}
	if len(hits) > 0 {
		t.Fatalf("a file is opened through an *os.Root without rootfile.OpenNoFollow in %d places (it follows a link even with O_NOFOLLOW):\n%s", len(hits), strings.Join(hits, "\n"))
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
		{"a method expression called with create-exclusive", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { (*os.Root).OpenFile(r, \"a\", os.O_CREATE|os.O_EXCL, 0) }\n", 1, 0},
		{"flags passed in a variable", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root, flags int) { r.OpenFile(\"a\", flags, 0) }\n", 1, 0},
		{"the opener's name in another file", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc OpenNoFollow(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY, 0) }\n", 1, 0},
		{"another function in the opener's file", rootOpenerFile, "package rootfile\nimport \"os\"\nfunc other(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY, 0) }\n", 1, 0},
		{"the opener itself", rootOpenerFile, "package rootfile\nimport \"os\"\nfunc OpenNoFollow(r *os.Root) { r.OpenFile(\"a\", os.O_RDONLY, 0) }\n", 0, 1},
		{"create-exclusive cleared", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root, flags int) { r.OpenFile(\"a\", flags&^os.O_EXCL, 0) }\n", 1, 0},
		{"create-exclusive", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.OpenFile(\"a\", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) }\n", 0, 0},
		{"the os package's function", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f() { os.OpenFile(\"a\", os.O_RDONLY, 0) }\nvar open = os.OpenFile\n", 0, 0},
		{"the os package under another name", "cmd/shikii/x.go", "package main\nimport o \"os\"\nfunc f() { o.OpenFile(\"a\", o.O_RDONLY, 0) }\n", 0, 0},
		{"Root.Open", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.Open(\"a\") }\n", 1, 0},
		{"Root.Create", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.Create(\"a\") }\n", 1, 0},
		{"Root.ReadFile", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.ReadFile(\"a\") }\n", 1, 0},
		{"Root.WriteFile", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.WriteFile(\"a\", nil, 0o600) }\n", 1, 0},
		{"os.OpenInRoot", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f() { os.OpenInRoot(\"/d\", \"a\") }\n", 1, 0},
		{"O_EXCL without O_CREATE", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.OpenFile(\"a\", os.O_RDWR|os.O_EXCL, 0) }\n", 1, 0},
		{"a parameter named os", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(os *os.Root) { os.OpenFile(\"a\", 0, 0) }\n", 1, 0},
		{"a method value of Open", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { open := r.Open; open(\"a\") }\n", 1, 0},
		{"a root embedded in a struct", "cmd/shikii/x.go", "package main\nimport \"os\"\ntype dir struct{ *os.Root }\nfunc f(d dir) { d.ReadFile(\"a\") }\n", 1, 0},
		{"a root returned by a call", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc g() *os.Root { return nil }\nfunc f() { g().WriteFile(\"a\", nil, 0o600) }\n", 1, 0},
		{"create-exclusive in a named constant", "cmd/shikii/x.go", "package main\nimport \"os\"\nconst fl = os.O_CREATE | os.O_EXCL | os.O_WRONLY\nfunc f(r *os.Root) { r.OpenFile(\"a\", fl, 0o600) }\n", 0, 0},
		{"a type of its own named Root", "cmd/shikii/x.go", "package main\ntype Root struct{}\nfunc (Root) Open(string) {}\nfunc f(r Root) { r.Open(\"a\") }\n", 0, 0},
		{"Root.FS read with fs.ReadFile", "cmd/shikii/x.go", "package main\nimport (\"io/fs\"; \"os\")\nfunc f(r *os.Root) { fs.ReadFile(r.FS(), \"a\") }\n", 1, 0},
		{"Root.FS opened", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f(r *os.Root) { r.FS().Open(\"a\") }\n", 1, 0},
		{"Root.FS kept in a value", "cmd/shikii/x.go", "package main\nimport (\"io/fs\"; \"os\")\nfunc f(r *os.Root) { fsys := r.FS(); fs.WalkDir(fsys, \".\", nil) }\n", 1, 0},
		{"Root.FS walked from the root", "cmd/shikii/x.go", "package main\nimport (\"io/fs\"; \"os\")\nfunc f(r *os.Root) { fs.WalkDir(r.FS(), \".\", nil) }\n", 0, 0},
		{"Root.FS walked from a folder that may be a link", "cmd/shikii/x.go", "package main\nimport (\"io/fs\"; \"os\")\nfunc f(r *os.Root, d string) { fs.WalkDir(r.FS(), d, nil) }\n", 1, 0},
		{"Root.FS listed with fs.ReadDir", "cmd/shikii/x.go", "package main\nimport (\"io/fs\"; \"os\")\nfunc f(r *os.Root) { fs.ReadDir(r.FS(), \".\") }\n", 1, 0},
		{"Root.FS walked by a function of its own named WalkDir", "cmd/shikii/x.go", "package main\nimport (\"io/fs\"; \"os\")\nfunc WalkDir(fsys fs.FS, root string, fn fs.WalkDirFunc) error { _, err := fs.ReadFile(fsys, root); return err }\nfunc f(r *os.Root) { WalkDir(r.FS(), \".\", nil) }\n", 1, 0},
		{"the os package's other openers", "cmd/shikii/x.go", "package main\nimport \"os\"\nfunc f() { os.Open(\"a\"); os.Create(\"a\"); os.ReadFile(\"a\"); os.WriteFile(\"a\", nil, 0o600) }\n", 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			found, allowed := rootOpensOutsideTheAllowlist(t, c.file, c.src)
			if len(found) != c.want || allowed != c.allowed {
				t.Fatalf("found %d (want %d) %v, allowed opener opens %d (want %d)", len(found), c.want, found, allowed, c.allowed)
			}
		})
	}
}

// The real update_install.go with its open of the update target written back as root.Open(target) (as it was before it went through
// OpenNoFollow): the check, type-checking the whole real package, reports exactly that line.
func TestTheRootOpenAllowlistSeesARootOpenInTheRealPackage(t *testing.T) {
	root, pkgs, exp := listRootOpenPackages(t)
	const file, now, was = "cmd/shikii/update_install.go", "rootfile.OpenNoFollow(root, target, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)", "root.Open(target)"
	var srcs []namedSource
	line := 0
	for _, p := range pkgs {
		if p.ImportPath != "github.com/hayaenfj/shikii/cmd/shikii" {
			continue
		}
		for _, name := range p.GoFiles {
			s := readSource(t, root, filepath.Join(p.Dir, name))
			if s.name == file {
				if strings.Count(s.src, now) != 1 {
					t.Fatalf("%s must open the update target once as %s", file, now)
				}
				line = strings.Count(s.src[:strings.Index(s.src, now)], "\n") + 1
				s.src = strings.Replace(s.src, now, was, 1)
			}
			srcs = append(srcs, s)
		}
	}
	if line == 0 {
		t.Fatalf("%s is not in the package", file)
	}
	found, _ := rootOpensInPackage(t, exp, "github.com/hayaenfj/shikii/cmd/shikii", srcs)
	if len(found) != 1 || !strings.HasPrefix(found[0], fmt.Sprintf("%s:%d:", file, line)) {
		t.Fatalf("found %v, want only %s:%d", found, file, line)
	}
}

// A file not built on this machine is read without types, so it must not name a root opener at all.
func TestAFileNotBuiltHereMustNotNameARootOpener(t *testing.T) {
	src := namedSource{"internal/handoff/x_other.go", "package handoff\nimport (\"io/fs\"; \"os\")\nfunc f(r *os.Root) { r.Open(\"a\") }\nfunc g() { os.OpenInRoot(\"/d\", \"a\") }\nfunc h(r *os.Root) { fs.ReadFile(r.FS(), \"a\") }\n"}
	if found := rootOpenerNamesInAFileNotBuilt(t, src); len(found) != 4 { // r.Open, os.OpenInRoot, and fs.ReadFile and r.FS by name
		t.Fatalf("found %v, want the four names of openers", found)
	}
	src.src = "package handoff\nimport \"io/fs\"\nconst noFollow = 0\nfunc linkCount(fs.FileInfo) uint64 { return 1 }\n"
	if found := rootOpenerNamesInAFileNotBuilt(t, src); len(found) != 0 {
		t.Fatalf("found %v in a file without an opener", found)
	}
}

type namedSource struct{ name, src string }

// readSource reads a file and names it by its path from the repository's top folder (as the allowlist names the opener's file).
func readSource(t *testing.T, root, path string) namedSource {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return namedSource{filepath.ToSlash(rel), string(b)}
}

// rootOpensOutsideTheAllowlist type-checks src as a package of its own (its imports from the standard library) and returns what
// rootOpensInPackage returns.
func rootOpensOutsideTheAllowlist(t *testing.T, name, src string) ([]string, int) {
	t.Helper()
	_, _, exp := listRootOpenPackages(t)
	return rootOpensInPackage(t, exp, "example.com/x", []namedSource{{name, src}})
}

// rootOpensInPackage type-checks the files of one package (a type error fails the test, so nothing goes unseen for want of a type)
// and returns each use of a root opener that is not allowed, and how many are inside the opener itself.
func rootOpensInPackage(t *testing.T, exp map[string]string, path string, srcs []namedSource) ([]string, int) {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, s := range srcs {
		f, err := parser.ParseFile(fset, s.name, s.src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		if exp[path] == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(exp[path])
	})}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	if _, err := conf.Check(path, fset, files, info); err != nil {
		t.Fatalf("type-check %s: %v", path, err)
	}
	var hits []string
	allowed := 0
	for i, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			inOpener := ok && fn.Recv == nil && fn.Name.Name == rootOpenerFunc && srcs[i].name == rootOpenerFile
			exclusive := map[*ast.Ident]bool{}
			listing := map[*ast.Ident]bool{} // r.FS() written in place as the folder of fs.WalkDir from "."
			ast.Inspect(decl, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 3 && walksFromTheRoot(info, call) {
					if inner, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr); ok && len(inner.Args) == 0 {
						if sel, ok := ast.Unparen(inner.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "FS" {
							listing[sel.Sel] = true
						}
					}
				}
				if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 3 {
					if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "OpenFile" {
						if s := info.Selections[sel]; s != nil && s.Kind() == types.MethodVal && createExclusive(info.Types[call.Args[1]]) {
							exclusive[sel.Sel] = true
						}
					}
				}
				id, ok := n.(*ast.Ident)
				if !ok || !opensAFileInARoot(info.Uses[id]) {
					return true
				}
				switch {
				case inOpener:
					allowed++
				case exclusive[id]:
				case listing[id]:
				default:
					hits = append(hits, fset.Position(id.Pos()).String())
				}
				return true
			})
		}
	}
	return hits, allowed
}

// walksFromTheRoot reports whether call is io/fs.WalkDir(fsys, ".", fn): a walk from the root itself, which opens folders only and
// lists names, never a file's content. WalkDir does not go into a folder that was a link when it read the listing; a folder swapped for
// a link between the listing and its turn is listed (names and sizes only), and a walk from a named folder could start at a link.
func walksFromTheRoot(info *types.Info, call *ast.CallExpr) bool {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return false
	}
	fn, ok := info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "io/fs" || fn.Signature().Recv() != nil || fn.Name() != "WalkDir" {
		return false
	}
	root := info.Types[call.Args[1]]
	return root.Value != nil && root.Value.Kind() == constant.String && constant.StringVal(root.Value) == "."
}

// opensAFileInARoot reports whether obj is a method of os.Root in rootFileMethods or the function os.OpenInRoot.
func opensAFileInARoot(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "os" {
		return false
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return fn.Name() == "OpenInRoot"
	}
	t := recv.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == "Root" && rootFileMethods[fn.Name()]
}

// createExclusive reports whether flags are a constant with both O_CREATE and O_EXCL.
func createExclusive(flags types.TypeAndValue) bool {
	if flags.Value == nil || flags.Value.Kind() != constant.Int {
		return false
	}
	v, ok := constant.Int64Val(flags.Value)
	return ok && v&int64(os.O_CREATE) != 0 && v&int64(os.O_EXCL) != 0
}

// rootOpenerNamesInAFileNotBuilt returns every selector in src named like a root opener (any receiver), as a file without types
// cannot tell a root from anything else.
func rootOpenerNamesInAFileNotBuilt(t *testing.T, s namedSource) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, s.name, s.src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && (rootFileMethods[sel.Sel.Name] || sel.Sel.Name == "OpenInRoot") {
			hits = append(hits, fset.Position(sel.Sel.Pos()).String()+" (not built on this machine)")
		}
		return true
	})
	return hits
}

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
	"strings"
	"testing"
)

// The time a step may take is written as a name, not as a time, wherever git is asked (hookGitTimeout is the one limit of every question
// the hook asks git) or the hook waits for anything: a limit written as time.Second beside another written as 2*time.Second is how the
// question about where the repository is came to have one second, the question about the origin two, and the rest three, and a test
// that stretches one limit (patientGit) left the others at their real values, so a loaded machine made the hook ask git twice.
//
// This reads the Go files of the package that are not tests and finds every limit made by context.WithTimeout (its time) and by
// time.Now().Add (a deadline; a negative time is a moment in the past, not a limit). A limit is "direct" when it is written as a time
// (time.Second in it, a number alone, or a name of the same function that was given one). The files that may have direct limits are listed
// with how many each has, and a name of the package that holds a time may be a limit only where it is listed; any other is reported.
// What this does not see: a limit through a field, a result of a call, a name of another function, time.After, a timer, an http client's
// Timeout, a deadline of a connection, and context.WithDeadline given a moment made elsewhere.

// directLimitFiles are the files where a limit is written as a time, with how many. None of them starts git (every question to git is
// asked with hookGitTimeout); a file that starts git does not belong here.
var directLimitFiles = map[string]int{
	"calibrate_truths.go": 1, // the replay of the person's truths against the model (10 minutes)
	"cli_bias.go":         1, // shikii bias: the queries to the model (20 minutes)
	"cli_decide.go":       1, // shikii decide: one decision from the model (60 seconds)
	"judge.go":            1, // shikii judge: the model's answers for a spec (30 minutes)
	"serve_decision.go":   1, // a decision asked over HTTP (60 seconds)
	"serve_listener.go":   1, // serve waits for the local model to answer (15 minutes)
	"serve_queue.go":      1, // a queued decision asked over HTTP (60 seconds)
	"update_command.go":   1, // shikii update: the download and the installation (5 minutes)
	"update_install.go":   1, // the new shikii started once to print its version (10 seconds)
	"wiki.go":             1, // shikii wiki: the model's answers for the pages (30 minutes)
}

// namedLimits are the names of the package that hold a time and may be a limit, and what each is for.
var namedLimits = map[string]string{
	"hookGitTimeout":    "every question the hook asks git (the one limit; 2 times it where a step asks several)",
	"hookDecideTimeout": "the decision the model gives a hook",
	"pageLockRetry":     "how long a start of the page tries the lock of its file",
	"replayLimit":       "the replay of the truths against the model",
	"tuneRunLimit":      "one run of the wording search",
}

// limitUse is one limit found in the code.
type limitUse struct {
	file   string
	line   int
	call   string   // context.WithTimeout or time.Now().Add
	expr   string   // the time as written
	direct bool     // written as a time
	names  []string // names of the package that hold a time and are in it
}

var timeUnits = map[string]bool{"Nanosecond": true, "Microsecond": true, "Millisecond": true, "Second": true, "Minute": true, "Hour": true}

// exprIdents are the names an expression uses: not the field or method after a dot, and not the package time of time.Second.
func exprIdents(e ast.Expr) []string {
	after := map[*ast.Ident]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			after[sel.Sel] = true
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "time" {
				after[x] = true
			}
		}
		return true
	})
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !after[id] {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// mentionsTime reports whether e has time.Second (or another unit) in it, a name in held, or numbers and nothing else.
func mentionsTime(e ast.Expr, held map[string]bool) bool {
	unit, number := false, false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && x.Name == "time" && timeUnits[n.Sel.Name] {
				unit = true
			}
		case *ast.BasicLit:
			number = true
		}
		return true
	})
	idents := exprIdents(e)
	for _, id := range idents {
		if held[id] {
			unit = true
		}
	}
	return unit || number && len(idents) == 0
}

// timeNamesOf are the names of the package that hold a time: declared with a time in it, or with another such name.
func timeNamesOf(files []*ast.File) map[string]bool {
	held := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, f := range files {
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.VAR && g.Tok != token.CONST {
					continue
				}
				for _, s := range g.Specs {
					v := s.(*ast.ValueSpec)
					for i, n := range v.Names {
						if i < len(v.Values) && len(v.Values) == len(v.Names) && !held[n.Name] && mentionsTime(v.Values[i], held) {
							held[n.Name], changed = true, true
						}
					}
				}
			}
		}
	}
	return held
}

// timeLimitsIn finds the limits in Go sources (file name to text) of one package.
func timeLimitsIn(sources map[string]string) ([]limitUse, error) {
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	var all []*ast.File
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, sources[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		parsed[name] = f
		all = append(all, f)
	}
	held := timeNamesOf(all)
	var uses []limitUse
	for _, name := range names {
		for _, d := range parsed[name].Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// the names this function gives a time (limit := 2 * time.Second), found until no more come
			local := map[string]bool{}
			for changed := true; changed; {
				changed = false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					var lhs, rhs []ast.Expr
					switch n := n.(type) {
					case *ast.AssignStmt:
						lhs, rhs = n.Lhs, n.Rhs
					case *ast.ValueSpec:
						for _, id := range n.Names {
							lhs = append(lhs, id)
						}
						rhs = n.Values
					}
					for i, l := range lhs {
						if id, ok := l.(*ast.Ident); ok && len(lhs) == len(rhs) && !local[id.Name] && mentionsTime(rhs[i], local) {
							local[id.Name], changed = true, true
						}
					}
					return true
				})
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				kind := ""
				var arg ast.Expr
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					switch x := sel.X.(type) {
					case *ast.Ident:
						if x.Name == "context" && sel.Sel.Name == "WithTimeout" && len(call.Args) == 2 {
							kind, arg = "context.WithTimeout", call.Args[1]
						}
					case *ast.CallExpr:
						if now, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Add" && len(call.Args) == 1 && now.Sel.Name == "Now" {
							if pkg, ok := now.X.(*ast.Ident); ok && pkg.Name == "time" {
								kind, arg = "time.Now().Add", call.Args[0]
							}
						}
					}
				}
				if kind == "" {
					return true
				}
				if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.SUB {
					return true // a moment in the past, not a limit
				}
				use := limitUse{file: name, line: fset.Position(call.Pos()).Line, call: kind, expr: types.ExprString(arg), direct: mentionsTime(arg, local)}
				for _, id := range exprIdents(arg) {
					if held[id] && !local[id] {
						use.names = append(use.names, id)
					}
				}
				uses = append(uses, use)
				return true
			})
		}
	}
	return uses, nil
}

// limitProblems says what is wrong with the limits found, given the files that may have direct ones (with how many) and the names that
// may be a limit. A direct limit in a file that is not listed, more or fewer than a listed file's number, and a name that is not listed
// are reported; so is a listed file or name that no longer has any (so freed room is not refilled).
func limitProblems(uses []limitUse, files map[string]int, names map[string]string) []string {
	var out []string
	direct, named := map[string][]limitUse{}, map[string]bool{}
	for _, u := range uses {
		if u.direct {
			direct[u.file] = append(direct[u.file], u)
			continue
		}
		for _, n := range u.names {
			named[n] = true
			if _, ok := names[n]; !ok {
				out = append(out, fmt.Sprintf("%s:%d: %s(%s) uses %s, a name that holds a time and is not in namedLimits", u.file, u.line, u.call, u.expr, n))
			}
		}
	}
	for file, found := range direct {
		allowed, listed := files[file]
		switch {
		case !listed:
			for _, u := range found {
				out = append(out, fmt.Sprintf("%s:%d: %s(%s) is a limit written as a time in a file that is not in directLimitFiles", u.file, u.line, u.call, u.expr))
			}
		case len(found) != allowed:
			out = append(out, fmt.Sprintf("%s has %d limits written as a time, directLimitFiles says %d (first at line %d)", file, len(found), allowed, found[0].line))
		}
	}
	for file, allowed := range files {
		if allowed > 0 && len(direct[file]) == 0 {
			out = append(out, fmt.Sprintf("%s is in directLimitFiles but has no limit written as a time: remove it", file))
		}
	}
	for n := range names {
		if !named[n] {
			out = append(out, fmt.Sprintf("%s is in namedLimits but no limit uses it: remove it", n))
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryTimeLimitIsANameExceptWhereListed(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sources[p] = string(b)
	}
	if len(sources) < 50 {
		t.Fatalf("premise: the Go files of the package are read, found %d", len(sources))
	}
	uses, err := timeLimitsIn(sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range limitProblems(uses, directLimitFiles, namedLimits) {
		t.Error(p)
	}
}

// The check itself: a limit written as a time in a file that is not listed, and a name that holds a time but is not listed, are found;
// a limit that goes through hookGitTimeout, or a listed file with the right number, is not.
func TestTheTimeLimitCheckFindsWhatItShouldAndNothingElse(t *testing.T) {
	const head = "package main\nimport (\"context\"; \"time\")\nvar hookGitTimeout = 3 * time.Second\nvar anotherLimit = time.Second\nfunc f(wait time.Duration) {\n"
	cases := []struct {
		name  string
		body  string
		files map[string]int
		names map[string]string
		want  []string // pieces of the problems, each in one of them; none: no problem
	}{
		{"a time written in a file that is not listed", "ctx, _ := context.WithTimeout(context.Background(), time.Second)\n_ = ctx", nil, map[string]string{"hookGitTimeout": ""},
			[]string{"limit.go:6: context.WithTimeout(time.Second) is a limit written as a time in a file that is not in directLimitFiles", "hookGitTimeout is in namedLimits but no limit uses it"}},
		{"a product of a number and a time", "ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)\n_ = ctx", nil, nil, []string{"limit.go:6: context.WithTimeout(2 * time.Second) is a limit written"}},
		{"a time given to a name first", "d := 2 * time.Second\nctx, _ := context.WithTimeout(context.Background(), d)\n_ = ctx", nil, nil, []string{"limit.go:7: context.WithTimeout(d) is a limit written"}},
		{"a deadline made with time.Now().Add", "dl := time.Now().Add(time.Minute)\n_ = dl", nil, nil, []string{"limit.go:6: time.Now().Add(time.Minute) is a limit written"}},
		{"a name that holds a time and is not listed", "ctx, _ := context.WithTimeout(context.Background(), anotherLimit)\n_ = ctx", nil, map[string]string{"hookGitTimeout": ""},
			[]string{"limit.go:6: context.WithTimeout(anotherLimit) uses anotherLimit, a name that holds a time and is not in namedLimits", "hookGitTimeout is in namedLimits but no limit uses it"}},
		{"the one limit", "ctx, _ := context.WithTimeout(context.Background(), hookGitTimeout)\n_ = ctx", nil, map[string]string{"hookGitTimeout": ""}, nil},
		{"twice the one limit", "ctx, _ := context.WithTimeout(context.Background(), 2*hookGitTimeout)\n_ = ctx", nil, map[string]string{"hookGitTimeout": ""}, nil},
		{"a time given as a parameter", "dl := time.Now().Add(wait)\n_ = dl", nil, nil, nil},
		{"a moment in the past", "dl := time.Now().Add(-time.Hour)\n_ = dl", nil, nil, nil},
		{"a listed file with its number", "ctx, _ := context.WithTimeout(context.Background(), time.Second)\n_ = ctx", map[string]int{"limit.go": 1}, nil, nil},
		{"a listed file with one more", "ctx, _ := context.WithTimeout(context.Background(), time.Second)\nctx, _ = context.WithTimeout(ctx, time.Minute)\n_ = ctx", map[string]int{"limit.go": 1}, nil,
			[]string{"limit.go has 2 limits written as a time, directLimitFiles says 1 (first at line 6)"}},
		{"a listed file with none", "_ = wait", map[string]int{"limit.go": 1}, nil, []string{"limit.go is in directLimitFiles but has no limit written as a time: remove it"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uses, err := timeLimitsIn(map[string]string{"limit.go": head + c.body + "\n}\n"})
			if err != nil {
				t.Fatal(err)
			}
			got := limitProblems(uses, c.files, c.names)
			if len(got) != len(c.want) {
				t.Fatalf("problems:\n%s\nwant %d, with:\n%s", strings.Join(got, "\n"), len(c.want), strings.Join(c.want, "\n"))
			}
			for _, w := range c.want {
				found := false
				for _, g := range got {
					found = found || strings.Contains(g, w)
				}
				if !found {
					t.Errorf("no problem holds %q, got:\n%s", w, strings.Join(got, "\n"))
				}
			}
		})
	}
}

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sourceWrite is one place in the code that builds a source (a notebook.Observation, an Observation inside the notebook package,
// or a workObservation) whose kind is agent or not known, or that saves one (a call of captureWorkObservation or of a Capture
// method). where is "file:function" ("file:" outside a function).
type sourceWrite struct {
	where, title string
	call         bool
}

// sourceWrites reads Go sources and returns every place that builds or saves a source. A source is found however it is spelled: a
// composite literal with its type, an element of a list or a map of the type (the type left out), a declaration of a variable of
// the type, new of the type. Its title is the string or the string constant of the same files, and "<expr>" for anything else (a
// title set field by field, positional fields, an expression), so it cannot slip past. A kind that is left out, a variable or an
// expression counts as agent (prepareWorkObservation makes an empty kind agent). Audit I of 0.31.0 wrote each of these forms, and
// the earlier reader, which knew the spelled-out literal with the kind "agent" alone, missed them all.
func sourceWrites(t *testing.T, files map[string]string) []sourceWrite {
	t.Helper()
	fset := token.NewFileSet()
	consts := map[string]string{}
	parsed := map[string]*ast.File{}
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed[name] = f
		ast.Inspect(f, func(n ast.Node) bool {
			if spec, ok := n.(*ast.ValueSpec); ok {
				for i, name := range spec.Names {
					if i < len(spec.Values) {
						if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							consts[name.Name], _ = strconv.Unquote(lit.Value)
						}
					}
				}
			}
			return true
		})
	}
	text := func(e ast.Expr) string {
		switch v := e.(type) {
		case nil:
			return ""
		case *ast.BasicLit:
			s, _ := strconv.Unquote(v.Value)
			return s
		case *ast.Ident:
			if s, ok := consts[v.Name]; ok {
				return s
			}
		}
		return "<expr>"
	}
	sourceType := func(e ast.Expr) bool {
		switch typ := e.(type) {
		case *ast.Ident:
			return typ.Name == "Observation" || typ.Name == "workObservation"
		case *ast.SelectorExpr:
			return typ.Sel.Name == "Observation"
		case *ast.StarExpr:
			switch inner := typ.X.(type) {
			case *ast.Ident:
				return inner.Name == "Observation" || inner.Name == "workObservation"
			case *ast.SelectorExpr:
				return inner.Sel.Name == "Observation"
			}
		}
		return false
	}
	var writes []sourceWrite
	for name, f := range parsed {
		where := name + ":"
		vars := map[string]bool{} // the variables and parameters of the type in the function read now
		literal := func(lit *ast.CompositeLit) {
			kind, title := "", ""
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					kind, title = "<expr>", "<expr>" // positional fields
					break
				}
				switch key, _ := kv.Key.(*ast.Ident); {
				case key == nil:
				case key.Name == "Kind":
					kind = text(kv.Value)
				case key.Name == "Title":
					title = text(kv.Value)
				}
			}
			if kind == "" || kind == "agent" || kind == "<expr>" {
				writes = append(writes, sourceWrite{where: where, title: title})
			}
		}
		var visit func(n ast.Node, elem bool) bool
		visit = func(n ast.Node, elem bool) bool {
			switch v := n.(type) {
			case *ast.FuncDecl:
				where = name + ":" + v.Name.Name
				vars = map[string]bool{}
				for _, field := range v.Type.Params.List {
					if sourceType(field.Type) {
						for _, n := range field.Names {
							vars[n.Name] = true
						}
					}
				}
				if v.Body != nil {
					ast.Inspect(v.Body, func(m ast.Node) bool { return visit(m, false) })
				}
				where, vars = name+":", map[string]bool{}
				return false
			case *ast.CompositeLit:
				typed := v.Type != nil && sourceType(v.Type)
				if (typed || (v.Type == nil && elem)) && len(v.Elts) > 0 { // T{} is the zero value: no source, nothing to save
					literal(v)
				}
				// the elements of a list or a map of the type leave the type out
				inner := false
				switch c := v.Type.(type) {
				case *ast.ArrayType:
					inner = sourceType(c.Elt)
				case *ast.MapType:
					inner = sourceType(c.Value)
				}
				for _, el := range v.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						el = kv.Value
					}
					if c, ok := el.(*ast.CompositeLit); ok && c.Type == nil {
						visit(c, inner)
						continue
					}
					ast.Inspect(el, func(m ast.Node) bool { return visit(m, false) })
				}
				return false
			case *ast.ValueSpec:
				if v.Type != nil && sourceType(v.Type) {
					for _, n := range v.Names {
						vars[n.Name] = true
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range v.Lhs {
					if call, ok := v.Rhs[min(i, len(v.Rhs)-1)].(*ast.CallExpr); ok {
						if fun, ok := call.Fun.(*ast.Ident); ok && fun.Name == "new" && len(call.Args) == 1 && sourceType(call.Args[0]) {
							if id, ok := lhs.(*ast.Ident); ok {
								vars[id.Name] = true
							}
						}
					}
					// a source set field by field: its title is the one given here, or not known
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Title" {
						continue
					}
					if id, ok := sel.X.(*ast.Ident); ok && vars[id.Name] {
						title := "<expr>"
						if len(v.Rhs) == len(v.Lhs) {
							title = text(v.Rhs[i])
						}
						writes = append(writes, sourceWrite{where: where, title: title})
					}
				}
			case *ast.CallExpr:
				switch fun := v.Fun.(type) {
				case *ast.Ident:
					if fun.Name == "captureWorkObservation" {
						writes = append(writes, sourceWrite{where: where, call: true})
					}
				case *ast.SelectorExpr:
					if fun.Sel.Name == "Capture" {
						writes = append(writes, sourceWrite{where: where, call: true})
					}
				}
			}
			return true
		}
		ast.Inspect(f, func(n ast.Node) bool { return visit(n, false) })
	}
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].where != writes[j].where {
			return writes[i].where < writes[j].where
		}
		return writes[i].title < writes[j].title
	})
	return writes
}

// The places that build a source whose title is not one of shikii's: the agent's observation (or the host's, for an assignment),
// whose title reportTitleRefusal looks at before it is saved, and the conversions that copy a source built elsewhere. Only a title
// they take from what they are given passes here: a fixed title written in one of them is shikii's and needs the table.
var agentSourceBuilders = map[string]string{
	"work_assignment_record.go:recordAssignment":        "the host's observation of an assignment event; it is recorded through recordWork, which refuses a title of shikii's",
	"work_record_observation.go:prepareWorkObservation": "a copy of the workObservation it is given (an empty kind becomes agent)",
	"work_record_observation.go:captureWorkObservation": "a copy of the workObservation it is given",
}

// The functions that may save a source (call captureWorkObservation or a Capture method). A new function that saves one fails the
// test until it is listed here with what it saves: a source shikii writes takes a title of workSourceTitles, an observation of the
// agent passes reportTitleRefusal.
var sourceSavers = map[string]string{
	"notebook.go:runNotebook":                                                 "the agent's wiki capture (reportTitleRefusal)",
	"work_record.go:recordWork":                                               "the agent's observations of a work record (reportTitleRefusal)",
	"work_record_observation.go:captureWorkObservation":                       "the one way a work saves a source: what its callers give it",
	"work_completion.go:captureWorkCompletion":                                "the result (workResultTitle)",
	"work_completion.go:finishWorkCompletion":                                 "the observations a work record prepared and did not save yet, again",
	"work_reasoning.go:archiveReasoningPending":                               "the archive of the pending wiki work",
	"work_reasoning.go:recordWorkReasoning":                                   "the ending review (workReviewTitle)",
	"work_reasoning_current.go:resolveCurrentWorkSource":                      "the person's current request (kind human)",
	"work_obligations.go:reserveRequiredNote":                                 "the archive of a required wiki note",
	"work_obligations.go:archiveWorkCapacity":                                 "the archive at the session capacity boundary",
	"work_outcomes.go:recordWorkOutcome":                                      "a procedure outcome",
	"work_trials.go:captureWorkTrialSource":                                   "a trial, a comparison, a recovered result (the callers' titles)",
	"work_reading.go:recordWorkReading":                                       "a measured reading",
	"../../internal/notebook/ingest_page.go:buildIngestPage":                  "the change of a page (kind human)",
	"../../internal/notebook/procedure_evidence.go:captureProcedureExecution": "the record of a procedure (Procedure execution)",
}

// sourceWriteProblems is what sourceWrites finds outside the decisions: a source built with a title workSourceTitles does not
// classify, or saved by a function that is not listed.
func sourceWriteProblems(writes []sourceWrite) []string {
	var problems []string
	for _, w := range writes {
		switch {
		case w.call:
			if _, ok := sourceSavers[w.where]; !ok {
				problems = append(problems, w.where+" saves a source, and sourceSavers does not say what it saves")
			}
		case agentSourceBuilders[w.where] != "" && w.title == "<expr>": // a title these places take from what they are given; a fixed one is shikii's
		default:
			if _, known := workSourceTitles[w.title]; !known {
				problems = append(problems, fmt.Sprintf("%s builds a source titled %q with kind agent, and workSourceTitles does not say whether evidence may cite it", w.where, w.title))
			}
		}
	}
	return problems
}

// Every source shikii writes about a work with kind agent has its title in workSourceTitles, which says whether evidence may cite
// it (audit G of 0.31.0: the archives of the work's state were not in the list of reports, and a result citing one was complete).
// The code of cmd/shikii and internal/notebook is read: a new archive, however it is spelled, and a new function that saves a
// source fail here until a decision is written (audit I of 0.31.0: the reader knew one spelling).
func TestEveryTitleShikiiWritesIsClassified(t *testing.T) {
	files := map[string]string{}
	for _, pattern := range []string{"*.go", "../../internal/notebook/*.go"} {
		paths, _ := filepath.Glob(pattern)
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
	}
	writes := sourceWrites(t, files)
	titles, calls, places := 0, 0, map[string]bool{}
	for _, w := range writes {
		places[w.where] = true
		if w.call {
			calls++
		} else if _, known := workSourceTitles[w.title]; known {
			titles++
		}
	}
	if titles < 10 || calls < 10 {
		t.Fatalf("the reader found %d titles of the table and %d saving calls; it no longer sees the sources shikii writes: %v", titles, calls, writes)
	}
	for _, problem := range sourceWriteProblems(writes) {
		t.Error(problem)
	}
	// every decision names a place that is still there
	for where := range agentSourceBuilders {
		if !places[where] {
			t.Errorf("agentSourceBuilders names %s, which builds no source now", where)
		}
	}
	for where := range sourceSavers {
		if !places[where] {
			t.Errorf("sourceSavers names %s, which saves no source now", where)
		}
	}
	// the table itself: which of shikii's titles evidence may cite is a decision of its own. Changing one fails here, and the
	// tests beside it (a report cited as evidence is refused) say why (audit I of 0.31.0: a report made citable failed no test).
	decided := map[string]bool{
		"Work result and remaining conditions":                        false,
		"Evidence-linked work judgment and ending review":             false,
		"Recovered work result evidence":                              false,
		"Procedure execution":                                         false,
		"Required wiki note and original prepared bindings":           false,
		"Required work and bindings at the session capacity boundary": false,
		"Pending wiki work before replacing preparation":              false,
		"Work trial plan":                                             true,
		"Work trial execution":                                        true,
		"Work trial comparison":                                       true,
		"Measured reading execution":                                  true,
		"Procedure execution with cited evidence":                     true,
	}
	if fmt.Sprint(decided) != fmt.Sprint(workSourceTitles) {
		t.Errorf("workSourceTitles changed:\n got  %v\n want %v", workSourceTitles, decided)
	}
}

// Negative: each way the code can build or save a source outside the decisions is found (audit I of 0.31.0 wrote the first five,
// and the reader then found none), and the spelled-out form of a known title is not taken for one.
func TestTheSourceReaderFindsEveryForm(t *testing.T) {
	forms := map[string]string{
		"spelled out":               "package main\nconst newArchive = \"A new archive of the work\"\nfunc a() { _ = workObservation{Kind: \"agent\", Title: newArchive} }\n",
		"the element type left out": "package main\nfunc a() { _ = []workObservation{{Kind: \"agent\", Title: \"An archive in a list\"}} }\n",
		"in a map":                  "package main\nfunc a() { _ = map[string]notebook.Observation{\"k\": {Kind: \"agent\", Title: \"An archive in a map\"}} }\n",
		"kind left out":             "package main\nfunc a() { _ = workObservation{Title: \"An archive without a kind\", report: true} }\n",
		"kind in a variable":        "package main\nfunc a(kind string) { _ = notebook.Observation{Kind: kind, Title: \"An archive of a given kind\"} }\n",
		"field by field":            "package main\nfunc a() { var o notebook.Observation; o.Kind = \"agent\"; o.Title = \"An archive set field by field\"; _ = o }\n",
		"positional fields":         "package main\nfunc a() { _ = workObservation{\"agent\", \"An archive in positional fields\", \"c\", \"r\", \"v\", true} }\n",
		"new":                       "package main\nfunc a() { o := new(workObservation); o.Title = \"An archive from new\"; _ = o }\n",
		"title from a parameter":    "package main\nfunc a(title string) { _ = workObservation{Kind: \"agent\", Title: title} }\n",
		"a new saver":               "package main\nfunc a(book notebook.Book, o notebook.Observation) { book.Capture(o) }\n",
		"a new saver of a work":     "package main\nfunc a(o workObservation) { captureWorkObservation(nil, nil, o, nil, nil) }\n",
	}
	for name, src := range forms {
		if problems := sourceWriteProblems(sourceWrites(t, map[string]string{"new.go": src})); len(problems) != 1 {
			t.Errorf("%s: %d problems, want 1: %v", name, len(problems), problems)
		}
	}
	for _, src := range []string{
		"package main\nfunc a() { _ = workObservation{Kind: \"agent\", Title: \"Work trial plan\"} }\n",
		"package main\nfunc a() { _ = notebook.Observation{Kind: \"human\", Title: \"Not the work's\"} }\n",
	} {
		if problems := sourceWriteProblems(sourceWrites(t, map[string]string{"new.go": src})); len(problems) != 0 {
			t.Errorf("a known title, or a source of another kind, is taken for a problem: %v", problems)
		}
	}
}

// An observation the agent sends cannot take the title of one of shikii's reports or archives: it would be taken for one and
// never cited (audit G of 0.31.0, U-2). A record of what the agent did keeps its title.
func TestAnObservationCannotTakeTheTitleOfAReport(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	for title, citable := range workSourceTitles {
		saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "agent", "title": title, "content": "The totals match (" + title + ")."}}})
		raw := auditJSON(saved["errors"])
		if err != nil {
			t.Fatal(err)
		}
		if refused := strings.Contains(raw, "is the one shikii gives its own reports"); refused == citable {
			t.Errorf("an observation titled %q: refused=%v, want refused=%v: %s", title, refused, !citable, raw)
		}
	}
}

// An older record (no notes of its reports) prepared again hands none of its reports over: carried lists no source of the work's
// own, and the refusal of an unknown reference lists neither those nor result or procedure among what evidence can cite (audit G
// of 0.31.0, U-6: the lines that keep reports out of these lists were held only by the refusal of citing them).
func TestAnOlderRecordHandsNoReportOver(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	done, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}},
		"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"new1"}}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: %v %.300s", err, auditJSON(done))
	}
	if err := updateSession(f.root, "planning", func(s *hookSession) {
		s.Planning.Work.Reports = nil
		for key, capture := range s.Planning.Work.Captures {
			capture.Report = false
			s.Planning.Work.Captures[key] = capture
		}
	}); err != nil {
		t.Fatal(err)
	}
	second := f.prepare(ids, "CSV export filters")
	carried, _ := second["carried"].(map[string]any)
	sources := auditJSON(carried["sources"])
	w := loadSession(f.root, "planning").Planning.Work
	if !strings.Contains(sources, `"observed1"`) {
		t.Fatalf("precondition: the observation of the earlier preparation is handed over: %s", sources)
	}
	for ref, id := range w.Sources {
		if strings.HasPrefix(ref, "observed") && workReportSource(f.b, id) && strings.Contains(sources, `"`+ref+`"`) {
			t.Errorf("carried lists %s, a report of the earlier preparation: %s", ref, sources)
		}
	}
}

// The refusal that lists what evidence can cite (a receipt without hook ids citing current) lists the work's observations, and
// neither result nor procedure, the names of its own reports (audit G of 0.31.0, U-6: that line was held by no test).
func TestTheUsableListOmitsTheWorksOwnReports(t *testing.T) {
	f := newAuditFlow(t)
	prepared, err := workCall(t, f.b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := f.record(prepared, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}},
		"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"new1"}}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: %v %.300s", err, auditJSON(done))
	}
	refused, err := f.record(prepared, map[string]any{"result": map[string]any{"changed": "Renamed the button and checked it.", "evidence": []any{"current"}}})
	raw := auditJSON(refused["errors"])
	if err != nil || !strings.Contains(raw, "usable here: ") || !strings.Contains(raw, "observed1") {
		t.Fatalf("precondition: the refusal lists the references evidence can cite: %v %s", err, raw)
	}
	list := raw[strings.Index(raw, "usable here: "):]
	for _, name := range []string{"result", "procedure", "current"} {
		if strings.Contains(list, name+",") || strings.Contains(list, name+" (") || strings.Contains(list, ": "+name) {
			t.Errorf("the refusal lists %s among what evidence can cite: %s", name, raw)
		}
	}
}

// A receipt that is too long to be one is refused in the same words as any other that is not one: they say where it comes from.
func TestALongReceiptIsToldWhereReceiptsComeFrom(t *testing.T) {
	for _, receipt := range []string{strings.Repeat("A", 1001), "short"} {
		_, err := workDecode(receipt)
		if err == nil || !strings.Contains(err.Error(), "pass the receipt of the reply of work prepare") {
			t.Errorf("a receipt of %d bytes: %v", len(receipt), err)
		}
	}
}

// A Stop with the id of a subagent leaves the parent's request as it is: not judged, its last reply not replaced, its turn not
// ended (audit G of 0.31.0, U-5: the turn was no longer ended, and the request was still judged on the child's reply).
func TestAStopOfASubagentLeavesTheRequestAsItIs(t *testing.T) {
	f := newAuditFlow(t)
	f.prepare(f.ask(planningTestRequest), "CSV export filters")
	before := loadSession(f.root, "planning")
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "agent_id": "child-1", "last_assistant_message": "Subagent done."})
	after := loadSession(f.root, "planning")
	if out != "" || after.Judged != before.Judged || after.Previous != before.Previous || after.EndedTurn != before.EndedTurn {
		t.Errorf("a Stop of a subagent changed the request: out=%q judged %v -> %v, previous %q -> %q, ended %q -> %q", out, before.Judged, after.Judged, before.Previous, after.Previous, before.EndedTurn, after.EndedTurn)
	}
}

// The list of what evidence can cite leaves procedure out by name: the record of the procedure a result closed is no report the
// work noted, and it is no evidence (audit G of 0.31.0, U-6: the line was held by no test). result is noted as a report the
// moment it is saved, so its name is kept out by the note as well.
func TestTheUsableListOmitsTheProcedureRecord(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Check recovery after a lost acknowledgement.", "")
	for i := 0; i < 3; i++ {
		if _, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The requested source condition was checked.")},
		"result": map[string]any{"changed": "Checked the requested source condition.", "evidence": []string{"new1"}}})
	if err != nil || saved["complete"] != true || loopObject(t, saved["procedure"])["source"] == nil {
		t.Fatalf("precondition: the result is recorded with its procedure record: %v %.400s", err, auditJSON(saved))
	}
	refused, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the source condition again.", "evidence": []string{"current"}}})
	raw := auditJSON(refused["errors"])
	if err != nil || !strings.Contains(raw, "usable here: ") || !strings.Contains(raw, "observed1") {
		t.Fatalf("precondition: the refusal lists the references evidence can cite: %v %s", err, raw)
	}
	list := raw[strings.Index(raw, "usable here: "):]
	for _, name := range []string{"procedure", "result"} {
		if strings.Contains(list, name+",") || strings.Contains(list, name+" (") || strings.Contains(list, ": "+name) {
			t.Errorf("the refusal lists %s among what evidence can cite: %s", name, raw)
		}
	}
}

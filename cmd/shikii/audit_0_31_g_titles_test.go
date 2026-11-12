package main

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// workSources is every kind of source shikii writes about a work.
func workSources() []notebook.WorkSource {
	return notebook.WorkSources()
}

// The places of cmd/shikii that may build a workObservation with its fields, or set its title: the constructor of shikii's own
// sources, the copy a capture keeps, and the entrances of what an agent or a person observed. A source shikii writes is built by
// workArchive from a notebook.WorkSource, and the notebook saves anything else as an observation, refused under a title of a work
// source (notebook.CaptureObserved): a new archive written here as a plain observation would be saved as one, and evidence could
// cite it. The types decide the rest: outside the notebook a source is saved only through CaptureObserved or CaptureWork.
var workObservationBuilders = map[string]string{
	"work.go:workArchive":                                "the constructor of a source shikii writes, from its kind",
	"work_record_observation.go:checkWorkSource":         "the defaults of an observation (kind, reference, version) and a work source's title",
	"work_record_observation.go:captureWorkObservation":  "the copy a capture keeps until it is saved",
	"work_completion.go:finishWorkCompletion":            "a capture that failed once, finished as the kind its memo kept",
	"work_reasoning_current.go:resolveCurrentWorkSource": "the person's current request, quoted (kind human)",
	"work_assignment_record.go:recordAssignment":         "the host's observation of an assignment event, recorded through recordWork as an observation",
}

// The functions of cmd/shikii that may save an observation (call CaptureObserved): the entrance of wiki capture and the capture of
// a work, which saves a workObservation as its kind says.
var observationSavers = map[string]string{
	"notebook.go:runNotebook":                           "wiki capture (the command line, MCP shikii_wiki, HTTP)",
	"work_record_observation.go:captureWorkObservation": "an observation of a work record, or a work source by its kind",
}

// workObservationProblems reads Go sources of package main by their types and says where a workObservation is built with its fields
// or retitled, or CaptureObserved is called, outside the places above.
func workObservationProblems(t *testing.T, files map[string]string) []string {
	t.Helper()
	var problems []string
	places := map[string]bool{}
	for _, tf := range typedSources(t, files, typedIsThePackage(files)) {
		if tf.prelude {
			continue
		}
		info := tf.info
		isObservation := func(typ types.Type) bool {
			n := typedNamed(typ)
			return n != nil && n.Obj().Name() == "workObservation" && n.Obj().Pkg() != nil && n.Obj().Pkg().Name() == "main"
		}
		typedDecls(tf.file, func(name string, root ast.Node) {
			where := tf.name + ":" + name
			ast.Inspect(root, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.CallExpr:
					// a conversion to a workObservation builds one as much as a composite literal does
					if tv, ok := info.Types[v.Fun]; ok && tv.IsType() && isObservation(tv.Type) {
						places[where] = true
						if workObservationBuilders[where] == "" {
							problems = append(problems, where+" builds a workObservation by a conversion; a source shikii writes is built by workArchive")
						}
					}
				case *ast.CompositeLit:
					if isObservation(info.Types[v].Type) && len(v.Elts) > 0 {
						places[where] = true
						if workObservationBuilders[where] == "" {
							problems = append(problems, where+" builds a workObservation; a source shikii writes is built by workArchive")
						}
					}
				case *ast.AssignStmt:
					for _, lhs := range v.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok {
							if selection := info.Selections[sel]; selection != nil && isObservation(selection.Recv()) {
								places[where] = true
								if workObservationBuilders[where] == "" {
									problems = append(problems, where+" sets the "+sel.Sel.Name+" of a workObservation")
								}
							}
						}
					}
				}
				// CaptureObserved however it is reached: called, taken as a method value, through another name
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if selection := info.Selections[sel]; selection != nil && selection.Obj().Name() == "CaptureObserved" && selection.Obj().Pkg() != nil && selection.Obj().Pkg().Name() == "notebook" {
						places[where] = true
						if observationSavers[where] == "" {
							problems = append(problems, where+" saves an observation (CaptureObserved)")
						}
					}
				}
				return true
			})
		})
	}
	if typedIsThePackage(files) {
		for where := range workObservationBuilders {
			if !places[where] {
				problems = append(problems, "workObservationBuilders names "+where+", which builds no workObservation now")
			}
		}
		for where := range observationSavers {
			if !places[where] {
				problems = append(problems, "observationSavers names "+where+", which saves no observation now")
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// A source shikii writes about a work takes a title of notebook.WorkSource, and nothing else saves one under such a title: the types
// decide it (a source leaves the notebook only through CaptureObserved or CaptureWork), and this test keeps the few places of
// cmd/shikii that build an observation to a list. The readers by spelling that came before missed a new spelling in every round of
// the audits of 0.31.0; a typed reader sees every spelling as the same expression.
func TestOnlyWorkArchiveBuildsASourceShikiiWrites(t *testing.T) {
	files := map[string]string{}
	paths, _ := filepath.Glob("*.go")
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
	for _, problem := range workObservationProblems(t, files) {
		t.Error(problem)
	}
	// negative: each spelling of a new archive outside the list is found
	for name, src := range map[string]string{
		"spelled out":               "package main\nfunc newArchive() { _ = workObservation{Kind: \"agent\", Title: \"A new archive\"} }\n",
		"the element type left out": "package main\nfunc newArchive() { _ = []workObservation{{Kind: \"agent\", Title: \"An archive in a list\"}} }\n",
		"an alias":                  "package main\ntype archive = workObservation\nfunc newArchive() { _ = archive{Title: \"An aliased archive\"} }\n",
		"field by field":            "package main\nfunc newArchive() { var o workObservation; o.Title = \"An archive set field by field\"; _ = o }\n",
		"through a pointer":         "package main\nfunc newArchive(o workObservation) { p := &o; p.Title = \"An archive through a pointer\"; _ = p }\n",
		"saved as an observation":   "package main\nfunc newArchive(book notebook.Book) { _, _ = book.CaptureObserved(notebook.Observation{}) }\n",
	} {
		if problems := workObservationProblems(t, map[string]string{"new.go": src}); len(problems) == 0 {
			t.Errorf("negative: %s: a new archive outside the list is not found", name)
		}
	}
	// control: a source built by workArchive is no problem
	if problems := workObservationProblems(t, map[string]string{"new.go": "package main\nfunc newArchive() { _ = workArchive(notebook.WorkPending, \"c\", \"r\") }\nfunc workArchive(k notebook.WorkSource, content, reference string) workObservation { return workObservation{} }\n"}); len(problems) != 0 {
		t.Errorf("control: %v", problems)
	}
}

// An observation the agent sends cannot take the title of one of shikii's reports or archives: it would be taken for one and
// never cited (audit G of 0.31.0, U-2). A record of what the agent did keeps its title.
func TestAnObservationCannotTakeTheTitleOfAReport(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	// every title of a work source, a record of what the agent did too: under it, an observation would pass for shikii's record
	// (a trial, a reading is read back by its title)
	for _, k := range workSources() {
		title := k.Title()
		saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "agent", "title": title, "content": "The totals match (" + title + ")."}}})
		raw := auditJSON(saved["errors"])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(raw, "is the one shikii gives its own reports") {
			t.Errorf("an observation titled %q is not refused: %s", title, raw)
		}
	}
	// an observation of another kind keeps the title, and one of the agent's own title is saved
	for _, o := range []map[string]any{{"kind": "test", "title": notebook.WorkProcedure.Title(), "content": "make test passed."}, {"kind": "agent", "title": "make test", "content": "make test passed."}} {
		saved, err := f.record(first, map[string]any{"observations": []any{o}})
		if err != nil || strings.Contains(auditJSON(saved["errors"]), "is the one shikii gives") {
			t.Errorf("an observation %v is refused: %v %s", o, err, auditJSON(saved["errors"]))
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
			capture.Source = notebook.WorkSource{} // a capture memo written before it kept the kind
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
	wentOn := before.goesOnForTest() // read before the Stop: the helper reads the state as it is when called
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "agent_id": "child-1", "last_assistant_message": "Subagent done."})
	after := loadSession(f.root, "planning")
	if out != "" || after.Judged != before.Judged || after.Previous != before.Previous || after.goesOnForTest() != wentOn || !wentOn {
		t.Errorf("a Stop of a subagent changed the request: out=%q judged %v -> %v, previous %q -> %q, goes on %v -> %v", out, before.Judged, after.Judged, before.Previous, after.Previous, wentOn, after.goesOnForTest())
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

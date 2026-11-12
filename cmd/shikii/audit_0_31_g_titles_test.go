package main

import (
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

// agentSourceTitles reads Go sources and returns the titles of every source literal with kind agent (a notebook.Observation, an
// Observation inside the notebook package, or a workObservation), resolving string constants of the same files. A title that is
// neither a string nor a known constant is returned as "<expr>", so it cannot slip past.
func agentSourceTitles(t *testing.T, files map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	consts := map[string]string{}
	parsed := []*ast.File{}
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
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
	titles := []string{}
	for _, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := ""
			switch typ := lit.Type.(type) {
			case *ast.Ident:
				name = typ.Name
			case *ast.SelectorExpr:
				name = typ.Sel.Name
			}
			if name != "Observation" && name != "workObservation" {
				return true
			}
			kind, title := "", ""
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				switch key, _ := kv.Key.(*ast.Ident); {
				case key == nil:
				case key.Name == "Kind":
					kind = text(kv.Value)
				case key.Name == "Title":
					title = text(kv.Value)
				}
			}
			if kind == "agent" {
				titles = append(titles, title)
			}
			return true
		})
	}
	sort.Strings(titles)
	return titles
}

// Every source shikii writes about a work with kind agent has its title in workSourceTitles, which says whether evidence may cite
// it (audit G of 0.31.0: the archives of the work's state were not in the list of reports, and a result citing one was complete).
// The code of cmd/shikii and internal/notebook is read, so a new archive written without a decision fails here.
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
	titles := agentSourceTitles(t, files)
	if len(titles) < 10 {
		t.Fatalf("the reader found %d titles; it no longer sees the sources shikii writes: %v", len(titles), titles)
	}
	for _, title := range titles {
		if _, known := workSourceTitles[title]; !known {
			t.Errorf("shikii writes a source titled %q with kind agent, and workSourceTitles does not say whether evidence may cite it", title)
		}
	}
	// negative: a new archive, written the way shikii writes one, is found and is not in the list
	found := agentSourceTitles(t, map[string]string{"new.go": "package main\nconst newArchive = \"A new archive of the work\"\nvar _ = workObservation{Kind: \"agent\", Title: newArchive}\nvar _ = notebook.Observation{Kind: \"agent\", Title: \"Another archive\"}\nvar _ = notebook.Observation{Kind: \"human\", Title: \"Not the work's\"}\n"})
	if strings.Join(found, "|") != "A new archive of the work|Another archive" {
		t.Errorf("the reader does not find the titles of a new archive: %v", found)
	}
	for _, title := range found {
		if _, known := workSourceTitles[title]; known {
			t.Errorf("negative: %q is in workSourceTitles", title)
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

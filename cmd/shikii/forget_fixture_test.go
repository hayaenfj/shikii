package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// forgetWorld is a project that holds the records of two sessions after a full flow: the session to forget (A) and another (B) whose id
// starts with A's id (a substring match on A's id takes B's records too). Every record of A is reachable only the way shikii forget
// follows: a field with A's id, or an id A's own records carry (its turns, its deliveries, its decisions, its sources). Some of what
// A made must stay: a source an article cites, a source another session's work state or the search journal names, and a decision a
// person labeled.
type forgetWorld struct {
	t            *testing.T
	repo, root   string
	scope        string
	st           store.Store
	book         notebook.Book
	name         string
	A, B         string
	childA       string
	childB       string
	tA, tA0, tB  string
	src          map[string]string // the label of a source and its id
	tr           map[string]string // the label of a decision and its trace
	del          map[string]string // the label of a delivery and its id
	page         notebook.Page
	old          time.Time
	mustNotStay  []string // what names A and nothing else: not found anywhere after forget (outside the folder it keeps them in)
	mayNameAOnly []string // files that name A on purpose and stay: a cited source, a source only a text of it names A
}

const (
	fxA, fxB     = "ses_fx_a1", "ses_fx_a10"
	fxTA, fxTA0  = "a1a1a1a1a1a1a1a1", "a0a0a0a0a0a0a0a0"
	fxTB         = "b1b1b1b1b1b1b1b1"
	fxChildA     = "agent_a1"
	fxChildB     = "agent_b1"
	fxLesson     = "e1e1e1e1e1e1e1e1"
	fxRevision   = "e2e2e2e2e2e2e2e2"
	fxLessonKey  = "lesson:" + fxLesson + ":" + fxRevision
	fxObservedAt = "2026-11-20T00:00:00Z"
)

func forgetHexName(label string) string {
	sum := workHash(label)
	return sum[:16]
}

func (w *forgetWorld) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *forgetWorld) observe(label, content string) string {
	w.t.Helper()
	at, _ := time.Parse(time.RFC3339, fxObservedAt)
	o, err := w.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "fixture " + label, Content: content, Reference: "forget fixture", Version: "1", ObservedAt: at})
	w.must(err)
	w.src[label] = o.ID
	return o.ID
}

func (w *forgetWorld) write(rel string, data []byte) {
	w.t.Helper()
	path := filepath.Join(w.root, rel)
	w.must(os.MkdirAll(filepath.Dir(path), 0o700))
	w.must(os.WriteFile(path, data, 0o600))
}

func (w *forgetWorld) writeJSON(rel string, v any) {
	w.t.Helper()
	b, err := json.Marshal(v)
	w.must(err)
	w.write(rel, b)
}

func (w *forgetWorld) delivery(label, id, parent, agent, session, tool, trace string) {
	w.t.Helper()
	w.del[label] = id
	q := store.MemoryQuote{Key: fxLessonKey, ID: fxLesson, Revision: fxRevision, Lesson: "l", AppliesTo: "a", Sources: []string{w.src["b1"]}, Trace: trace}
	w.must(w.st.SaveMemoryDelivery(store.MemoryDelivery{ID: id, Scope: w.scope, Request: "r " + label, Quotes: []store.MemoryQuote{q}, Parent: parent, Agent: agent, Session: session, Tool: tool}))
}

func (w *forgetWorld) assess(deliveryID, agent string) {
	w.t.Helper()
	a := store.MemoryAssessment{SourceID: deliveryID, Key: fxLessonKey, Result: "unused", Decision: "not needed", Evidence: "did not come up", InRequest: agent == ""}
	a.Agent = agent
	w.must(w.st.SaveMemoryAssessment(w.scope, a))
}

func (w *forgetWorld) decision(spec, trace, source string, text bool) {
	w.t.Helper()
	at := time.Now().UTC()
	w.must(w.st.AppendRecord(store.Record{TraceID: trace, SpecID: spec, SpecHash: "h", Type: "noul", Band: "act", At: at}))
	if source != "" {
		w.must(w.st.AppendLabel(store.Label{TraceID: trace, SpecID: spec, SpecHash: "h", Type: "noul", Model: "m", Truth: "yes", Weight: 1, Source: source, At: at}))
	}
	if text {
		w.writeJSON(filepath.Join("records", "text", trace+".json"), map[string]any{"inputs_hash": "h", "inputs": map[string]string{"request": "full text"}})
	}
}

// newForgetWorld builds the project. Nothing in it comes from a real session.
func newForgetWorld(t *testing.T) *forgetWorld {
	t.Helper()
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	root, scope, err := projectStoreAt(repo)
	if err != nil {
		t.Fatal(err)
	}
	w := &forgetWorld{t: t, repo: repo, root: root, scope: scope, st: store.Store{Dir: root}, book: notebook.Book{Root: root, Scope: scope},
		A: fxA, B: fxB, childA: fxChildA, childB: fxChildB, tA: fxTA, tA0: fxTA0, tB: fxTB,
		src: map[string]string{}, tr: map[string]string{}, del: map[string]string{}, old: time.Now().Add(-time.Hour)}
	w.name = store.StoreName(root, scope)
	covA, covB := store.CoverageSession(w.A), store.CoverageSession(w.B)

	// the sources
	w.observe("a1", "made by A: a note that nothing cites")
	w.observe("a2", "made by A in session "+w.A+": an observation an article cites")
	w.observe("a3", "made by A: its result")
	w.observe("a4", "made by A: another session's work state names it")
	w.observe("a5", "made by A: only its reasoning history names it")
	w.observe("a6", "made by A: only a miss of another session in the search journal names it")
	w.observe("read", "made by nobody here: A's work read it")
	w.observe("b1", "made by B")
	w.observe("mention", "an observation of someone else that says "+w.A+" in its text")
	for _, label := range []string{"a1", "a3"} { // reviewed as a conversation: its marks stay beside it
		w.write(filepath.Join("records", "wiki-sources", w.name, w.src[label]+".reviewed"), []byte("reviewed\n"))
	}
	w.write(filepath.Join("records", "wiki-sources", w.name, w.src["a1"]+".text.json"), []byte(`{"long":"text"}`))
	// the copy of A's conversation, named by its turn (an ingest makes it)
	w.src["conv"] = w.tA
	w.writeJSON(filepath.Join("records", "wiki-sources", w.name, w.tA+".json"), store.Feedback{ID: w.tA, Request: "r", Summary: "s", Reply: "p", At: time.Now().UTC()})
	w.write(filepath.Join("records", "wiki-sources", w.name, w.tA+".reviewed"), []byte("reviewed\n"))
	// an article cites a2
	page, err := w.book.Ingest(notebook.Draft{Sources: []string{w.src["a2"]}, Title: "Forget fixture lesson", Lesson: "Keep the fixture's article.", AppliesTo: "forget tests", Kind: "rule", Scene: "when testing forget"})
	w.must(err)
	w.page = *page.Page

	// feedback and the decisions of the conversations
	w.tr["fb"], w.tr["pend"], w.tr["la1"], w.tr["la2"], w.tr["lb"], w.tr["rvb"] = "c4c4c4c4c4c4c4c4", "c5c5c5c5c5c5c5c5", "c1c1c1c1c1c1c1c1", "c2c2c2c2c2c2c2c2", "c3c3c3c3c3c3c3c3", "c6c6c6c6c6c6c6c6"
	w.must(w.st.AppendFeedback(scope, store.Feedback{ID: w.tA, Request: "ra", Summary: "sa", Reply: "pa", Session: w.A, Targets: []store.ReviewTarget{{Spec: "done-as-asked", TraceID: w.tr["fb"]}}}))
	w.must(w.st.AppendFeedback(scope, store.Feedback{ID: w.tA0, Request: "ra0", Summary: "sa0", Reply: "pa0"})) // an older record: no session field
	w.must(w.st.AppendFeedback(scope, store.Feedback{ID: w.tB, Request: "rb", Summary: "sb", Reply: "pb", Session: w.B, Targets: []store.ReviewTarget{{Spec: "done-as-asked", TraceID: w.tr["rvb"]}}}))
	w.decision("done-as-asked", w.tr["fb"], "", false)
	w.decision("done-as-asked", w.tr["rvb"], "", false)
	w.decision("needs-plan", w.tr["pend"], "", true)
	w.decision("lesson-applies", w.tr["la1"], "outcome", true)
	w.decision("lesson-applies", w.tr["la2"], "outcome", true)
	w.must(w.st.AppendLabel(store.Label{TraceID: w.tr["la2"], SpecID: "lesson-applies", SpecHash: "h", Type: "noul", Model: "m", Truth: "no", Weight: 1, Source: "ui", At: time.Now().UTC()}))
	w.decision("lesson-applies", w.tr["lb"], "outcome", true)
	for _, r := range []struct{ session, id, trace string }{{w.A, w.tA, w.tr["fb"]}, {w.B, w.tB, w.tr["rvb"]}} {
		_, err := w.st.SaveReview(store.Review{TraceID: r.trace, Spec: "done-as-asked", Scope: scope, InputsHash: "h", Truth: "yes", Reason: "fixture", Source: store.Feedback{ID: r.id, Request: "r", Summary: "s", Reply: "p", Session: r.session}})
		w.must(err)
	}

	// the deliveries and what was said of them
	w.delivery("A", w.tA, "", "", "", "", w.tr["la1"])
	w.delivery("Atool", "d1d1d1d1d1d1d1d1", w.tA, "", "", "Edit forget.go", w.tr["la2"])
	w.delivery("Achild", "d2d2d2d2d2d2d2d2", w.tA, w.childA, w.A, "", "")
	w.delivery("Aold", "d3d3d3d3d3d3d3d3", w.tA0, "agent_old", "", "", "") // a delivery before the session field: only its parent turn names A
	w.delivery("B", w.tB, "", "", "", "", w.tr["lb"])
	w.delivery("Bchild", "d4d4d4d4d4d4d4d4", w.tB, w.childB, w.B, "", "")
	for _, label := range []string{"A", "Achild", "B", "Bchild"} {
		agent := ""
		if strings.HasSuffix(label, "child") {
			agent = map[string]string{"Achild": w.childA, "Bchild": w.childB}[label]
		}
		w.assess(w.del[label], agent)
	}

	// the coverage ledger: B left a row open, A's result took it over, then A's own row
	rowX := store.CoverageRow{Area: "X area", Outcome: store.CoverageOpen, Origin: store.CoverageFromRemaining, Session: covB}
	_, err = w.st.RecordCoverage(scope, w.src["b1"], []store.CoverageRow{rowX}, store.CoverageHandover{})
	w.must(err)
	rowDocs := store.CoverageRow{Area: "docs area", Outcome: store.CoverageOpen, Origin: store.CoverageFromRemaining, Session: covA}
	handed, err := w.st.RecordCoverage(scope, w.src["a3"], []store.CoverageRow{rowDocs}, store.CoverageHandover{Session: covA, Shown: []string{"X area"}})
	w.must(err)
	if len(handed) != 1 {
		t.Fatalf("precondition: A's result takes B's row over: %v", handed)
	}

	// the guards, the reasoning history, the search journal, the delivery journal
	now := time.Now().UTC()
	w.must(appendGuardRecords(root, []guardRecord{{At: now, Session: w.A, Turn: w.tA0, Guard: "g", Pass: true}, {At: now, Session: w.B, Turn: w.tB, Guard: "g", Pass: true}, {At: now, Session: w.A, Turn: w.tA, Guard: "g", Pass: false, Reason: "stopped"}}))
	for _, h := range []struct{ session, turn, source, key string }{{w.A, w.tA0, w.src["a5"], "a"}, {w.B, w.tB, w.src["b1"], "b"}} {
		w.writeJSON("records/work-reasoning-"+workHash([]string{w.name, h.session, h.turn, h.key})+".json", map[string]any{"store": w.name, "session": h.session, "turn": h.turn, "entry": map[string]any{"key": h.key, "source": h.source, "at": now}})
	}
	workA, workB := workHash([]string{w.A, w.tA})[:16], workHash([]string{w.B, w.tB})[:16]
	discovery := func(work string) map[string]any {
		return map[string]any{"lookup_id": "f1f1f1f1f1f1f1f1", "page_id": w.page.ID, "revision": w.page.Revision, "context": "c", "decision": "d", "evidence": "e", "work_id": work}
	}
	miss := func(query, session, result string) map[string]any {
		m := map[string]any{"query": query, "at": now}
		if session != "" {
			m["session"] = session
		}
		if result != "" {
			m["result"] = result
		}
		return m
	}
	w.writeJSON(filepath.Join("records", "wiki-searches", w.name, ".searches.json"), map[string]any{
		"lookups":     []any{},
		"discoveries": []any{discovery(workA), discovery(workB)},
		"misses":      []any{miss("alpha beta", covA, w.src["a1"]), miss("gamma delta", covB, w.src["a6"]), miss("omega", "", "")},
	})
	for _, turn := range []string{w.tA, w.tB} {
		w.must(w.book.SaveDeliveryCase(notebook.DeliveryCase{TurnID: turn, Request: "r " + turn, Mode: "request", Complete: true,
			Entries: []notebook.DeliveryEntry{{ID: w.page.ID, Revision: w.page.Revision, Retrieval: "candidate", Reason: "candidate"}}}))
	}

	// the session records and what sits beside them
	work := func(captures, saved map[string]any, sources map[string]string, reports map[string]bool, result string) map[string]any {
		return map[string]any{"captures": captures, "saved": saved, "sources": sources, "reports": reports, "result": map[string]any{"source": result}, "coverage_left": []string{result}, "query": "q"}
	}
	w.writeJSON("sessions/"+w.A+".json", map[string]any{"request": "ra", "turn_id": w.tA, "scope": scope, "at": now,
		"planning": map[string]any{"turn_id": w.tA, "at": now, "source_material": map[string]string{}, "work": work(
			map[string]any{"h1": map[string]any{"ref": "observed1", "id": w.src["a1"]}, "h4": map[string]any{"ref": "observed4", "id": w.src["a4"]}, "h6": map[string]any{"ref": "observed6", "id": w.src["a6"]}},
			map[string]any{"s2": map[string]any{"ref": "note", "kind": "note", "id": w.src["a2"]}},
			map[string]string{"source1": w.src["read"], "observed1": w.src["a1"]}, map[string]bool{w.src["a3"]: true}, w.src["a3"])},
		"pending": []any{map[string]any{"trace": w.tr["pend"], "tree": "t", "at": now}}, "reasoning": map[string]any{"turn_id": w.tA, "session_id": w.A}})
	w.writeJSON("sessions/"+w.B+".json", map[string]any{"request": "rb", "turn_id": w.tB, "scope": scope, "at": now,
		"planning": map[string]any{"turn_id": w.tB, "at": now, "source_material": map[string]string{}, "work": work(
			map[string]any{"h1": map[string]any{"ref": "observed1", "id": w.src["b1"]}}, map[string]any{},
			map[string]string{"evidence1": w.src["a4"]}, map[string]bool{}, w.src["b1"])}})
	for _, id := range []string{w.A, w.B} {
		for _, suffix := range []string{".files", ".lines", ".json.lock", ".json.work.lock"} {
			w.write("sessions/"+id+suffix, []byte("cache of "+id+"\n"))
		}
		w.must(os.Chtimes(filepath.Join(root, "sessions", id+".json"), w.old, w.old))
	}
	noteSessionRequest(w.A, root, w.tA)
	noteSessionRequest(w.B, root, w.tB)
	noticeOnce(w.A, scope)
	noticeOnce(w.B, scope)

	told := sha256.Sum256([]byte(w.A + "\n" + scope))
	w.mustNotStay = []string{hex.EncodeToString(told[:8]), w.A, w.tA, w.tA0, w.tr["fb"], w.tr["pend"], w.tr["la1"], w.src["a1"], w.src["a3"], w.src["a5"], "d1d1d1d1d1d1d1d1", "d2d2d2d2d2d2d2d2", "d3d3d3d3d3d3d3d3", w.childA, "agent_old", covA, workA}
	w.mayNameAOnly = []string{w.src["a2"], w.src["mention"]}
	return w
}

// forget runs shikii forget with the arguments and gives what it printed and its error.
func forgetCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	defer func() { stdout = old }()
	err := cmdForget(args)
	return buf.String(), err
}

// snapshot reads every file of the project's store and of the agents' common folder: relative path -> content.
func (w *forgetWorld) snapshot() map[string][]byte {
	w.t.Helper()
	home, _ := os.UserHomeDir()
	out := map[string][]byte{}
	for prefix, dir := range map[string]string{"store/": w.root, "home/": filepath.Join(home, ".shikii")} {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				w.t.Fatal(err)
			}
			rel, _ := filepath.Rel(dir, path)
			out[prefix+filepath.ToSlash(rel)] = b
			return nil
		})
	}
	return out
}

var forgetIDWord = func(id string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(id) + `([^A-Za-z0-9_-]|$)`)
}

// namesA reports whether a name or a content names A or something only A's records name (a whole id, not a part of B's id).
func (w *forgetWorld) namesA(text string) bool {
	if forgetIDWord(w.A).MatchString(text) {
		return true
	}
	for _, token := range w.mustNotStay {
		if token != w.A && strings.Contains(text, token) {
			return true
		}
	}
	return false
}

// problems checks the project against what forget owes, and says what is wrong: a record of A left, a record of B or a file that is
// not A's changed, what was to stay gone, and a closed row that should be open. before is the snapshot taken before forget.
func (w *forgetWorld) problems(before map[string][]byte) []string {
	w.t.Helper()
	var out []string
	after := w.snapshot()
	// (a) nothing names A and only A, anywhere but the folder that keeps it
	for path, b := range after {
		if strings.HasPrefix(path, "store/records/forgotten/") {
			continue
		}
		stays := false
		for _, keep := range w.mayNameAOnly {
			stays = stays || strings.Contains(path, keep)
		}
		if !stays && (w.namesA(path) || w.namesA(string(b))) {
			out = append(out, "a: a record of the session is left in "+path)
		}
	}
	// (b) what is not A's is untouched: a file stays as it was, a file that holds lines keeps every line that is not A's, in order
	for path, b := range before {
		if strings.HasPrefix(path, "store/records/forgotten/") {
			continue
		}
		now, exists := after[path]
		switch {
		case !exists:
			if !w.namesA(path) && !w.namesA(string(b)) {
				out = append(out, "b: a file that is not the session's is gone: "+path)
			}
		case bytes.Equal(b, now):
		case strings.HasSuffix(path, ".jsonl"):
			if want := w.linesWithoutA(b); !bytes.Equal(want, now) {
				out = append(out, "b: "+path+" lost or changed a line that is not the session's")
			}
		case !w.sharedFile(path):
			out = append(out, "b: a file that is not the session's changed: "+path)
		}
	}
	// (c) what is cited, named or labeled by a person stays
	for _, label := range []string{"a2", "a4", "a6", "read", "b1", "mention"} {
		if _, ok := after["store/records/wiki-sources/"+w.name+"/"+w.src[label]+".json"]; !ok {
			out = append(out, "c: the source "+label+" was removed though something names it")
		}
	}
	if _, ok := after["store/records/text/"+w.tr["la2"]+".json"]; !ok || !bytes.Contains(joinFiles(after, "store/labels/lesson-applies.jsonl"), []byte(w.tr["la2"])) || !bytes.Contains(joinFiles(after, "store/records/lesson-applies.jsonl"), []byte(w.tr["la2"])) {
		out = append(out, "c: the decision a person labeled lost its record, its text or its label")
	}
	if report, err := w.book.Lint(); err != nil {
		out = append(out, "c: the wiki's check fails: "+err.Error())
	} else {
		for _, issue := range report.Issues {
			if issue.Kind == "missing-source" || issue.Kind == "changed-source-text" {
				out = append(out, "c: the wiki's check finds "+issue.Kind+" for "+issue.Source)
			}
		}
	}
	if _, err := w.book.PendingObservations(); err != nil {
		out = append(out, "c: the queue of unreviewed sources cannot be read: "+err.Error())
	}
	out = append(out, w.journalProblems(after)...)
	// (d) the row of B that A's result took over is open again, and A's rows are gone
	open, err := w.st.OpenCoverage(w.scope)
	if err != nil {
		out = append(out, "d: the ledger cannot be read: "+err.Error())
	}
	areas := []string{}
	for _, row := range open {
		areas = append(areas, row.Area)
	}
	sort.Strings(areas)
	if strings.Join(areas, ",") != "X area" {
		out = append(out, "d: the open rows are "+strings.Join(areas, ",")+", want only B's X area")
	}
	return out
}

func joinFiles(files map[string][]byte, paths ...string) []byte {
	var out []byte
	for _, p := range paths {
		out = append(out, files[p]...)
	}
	return out
}

// sharedFile is a file that holds the records of several sessions (it is rewritten, and its lines are compared one by one) or that forget
// is to change as a whole: the journals, the queue, the list of what a session was told.
func (w *forgetWorld) sharedFile(path string) bool {
	return strings.HasSuffix(path, ".jsonl") || strings.HasSuffix(path, "/.searches.json") || strings.HasSuffix(path, "/.deliveries.json") ||
		strings.HasSuffix(path, "/.captured.json") || path == "home/agent/told.json"
}

// linesWithoutA is the JSONL file without the lines of A, as forget leaves it.
func (w *forgetWorld) linesWithoutA(data []byte) []byte {
	var out []byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 && !w.namesA(string(line)) {
			out = append(append(out, line...), '\n')
		}
	}
	return out
}

// journalProblems checks the search journal and the delivery journal: B's elements stay, A's go.
func (w *forgetWorld) journalProblems(after map[string][]byte) []string {
	var out []string
	var j struct {
		Discoveries []struct {
			WorkID string `json:"work_id"`
		} `json:"discoveries"`
		Misses []struct {
			Query string `json:"query"`
		} `json:"misses"`
	}
	if err := json.Unmarshal(after["store/records/wiki-searches/"+w.name+"/.searches.json"], &j); err != nil {
		return []string{"the search journal cannot be read: " + err.Error()}
	}
	queries := []string{}
	for _, m := range j.Misses {
		queries = append(queries, m.Query)
	}
	sort.Strings(queries)
	if len(j.Discoveries) != 1 || j.Discoveries[0].WorkID != workHash([]string{w.B, w.tB})[:16] || strings.Join(queries, ",") != "gamma delta,omega" {
		out = append(out, "b: the search journal holds "+string(after["store/records/wiki-searches/"+w.name+"/.searches.json"]))
	}
	cases, err := w.book.DeliveryCases()
	if err != nil || len(cases) != 1 || cases[0].TurnID != w.tB {
		out = append(out, "b: the delivery journal does not hold B's case alone")
	}
	return out
}

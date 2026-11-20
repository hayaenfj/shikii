package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func coverageRow(area, outcome string, more ...string) workCoverageRow {
	row := workCoverageRow{Area: area, Outcome: outcome}
	if len(more) > 0 {
		row.Evidence = more[0]
	}
	if len(more) > 1 {
		row.Reason = more[1]
	}
	return row
}

// A result that cites new1 and says what it examined, for the tests of the ledger.
func coverageResult(rows ...workCoverageRow) workCompletion {
	return workCompletion{Changed: "Audited the export.", Checked: "Read the export code.", Evidence: []string{"new1"}, Coverage: rows}
}

// refusals of a coverage list: the cause is named and so is what to send instead. The valid cases beside them are the allow-list's
// negative side: the same checks accept exactly what the ledger can keep.
func TestWorkCoverageRefusesWhatTheLedgerCannotKeep(t *testing.T) {
	yes := true
	many := func(n int) []workCoverageRow {
		rows := []workCoverageRow{}
		for i := range n {
			rows = append(rows, coverageRow(strings.Repeat("a", i+1), "open"))
		}
		return rows
	}
	with := func(c workCompletion, f func(*workCompletion)) workCompletion {
		f(&c)
		return c
	}
	for _, c := range []struct {
		name   string
		result workCompletion
		want   string // a part of the refusal, or "" when the result is valid
	}{
		{"clear without evidence", coverageResult(coverageRow("export filters", "clear")), "result.coverage[0] is clear without evidence"},
		{"clear with evidence outside result.evidence", coverageResult(coverageRow("export filters", "clear", "new2")), `evidence "new2" is not one of result.evidence (new1)`},
		{"clear citing current", with(coverageResult(coverageRow("export filters", "clear", "current")), func(c *workCompletion) { c.Evidence = []string{"new1", "current"} }), "is current, the request's own text"},
		{"reported with evidence outside result.evidence", coverageResult(coverageRow("export filters", "reported", "new2")), "is not one of result.evidence"},
		{"open with evidence outside result.evidence", coverageResult(coverageRow("export filters", "open", "knowledge1")), "is not one of result.evidence"},
		{"excluded without reason", coverageResult(coverageRow("export filters", "excluded")), "result.coverage[0].reason must be 1-300 bytes"},
		{"excluded with a blank reason", coverageResult(coverageRow("export filters", "excluded", "", "  \n ")), "reason must be 1-300 bytes"},
		{"reason over 300 bytes on any row", coverageResult(coverageRow("export filters", "open", "", strings.Repeat("r", 301))), "at most 300 bytes for any other"},
		{"sufficient true with an open row", with(coverageResult(coverageRow("export filters", "clear", "new1"), coverageRow("export totals", "open")), func(c *workCompletion) { c.Sufficient = &yes }), "result.sufficient is true while result.coverage[1] is open"},
		{"duplicate area", coverageResult(coverageRow("export filters", "open"), coverageRow("  export   filters ", "open")), "result.coverage[1].area is the same area as result.coverage[0]"},
		{"17 rows", coverageResult(many(17)...), "takes at most 16 rows (got 17)"},
		{"unknown outcome", coverageResult(coverageRow("export filters", "done")), `outcome must be reported, clear, excluded or open (got "done")`},
		{"empty outcome", coverageResult(coverageRow("export filters", "")), "outcome must be reported, clear, excluded or open"},
		{"outcome in another case", coverageResult(coverageRow("export filters", "Open")), "outcome must be"},
		{"blank area", coverageResult(coverageRow(" \t ", "open")), "area must be 1-160 bytes"},
		{"area with a line break", coverageResult(coverageRow("export\nfilters", "open")), "area must be 1-160 bytes"},
		{"area over 160 bytes", coverageResult(coverageRow(strings.Repeat("a", 161), "open")), "area must be 1-160 bytes"},
		{"area that is not UTF-8", coverageResult(coverageRow("export \xff filters", "open")), "area must be 1-160 bytes"},
		{"recovery by ref with coverage", workCompletion{Ref: "0123456789abcdef", Coverage: []workCoverageRow{coverageRow("export filters", "open")}}, "a repair cannot add it; leave coverage out"},
		// the same checks accept what the ledger keeps
		{"16 rows", coverageResult(many(16)...), ""},
		{"area of 160 bytes", coverageResult(coverageRow(strings.Repeat("a", 160), "open")), ""},
		{"reason of 300 bytes", coverageResult(coverageRow("export filters", "excluded", "", strings.Repeat("r", 300))), ""},
		{"clear citing a listed ref", coverageResult(coverageRow("export filters", "clear", "new1")), ""},
		{"reported and open citing a listed ref", coverageResult(coverageRow("export filters", "reported", "new1"), coverageRow("export totals", "open", "new1", "ran out of time")), ""},
		{"sufficient false with an open row", with(coverageResult(coverageRow("export filters", "open")), func(c *workCompletion) { f := false; c.Sufficient = &f }), ""},
		{"sufficient true with no open row", with(coverageResult(coverageRow("export filters", "clear", "new1")), func(c *workCompletion) { c.Sufficient = &yes }), ""},
		{"recovery by ref without coverage", workCompletion{Ref: "0123456789abcdef", Evidence: []string{"new1"}}, ""},
		{"no coverage", workCompletion{Changed: "Audited the export."}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := validateWorkCompletion(&c.result)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("a valid result is refused: %v", err)
			case c.want != "" && err == nil:
				t.Fatalf("the result is accepted, want a refusal with %q", c.want)
			case c.want != "" && !strings.Contains(err.Error(), c.want):
				t.Fatalf("the refusal is %q, want a part %q", err.Error(), c.want)
			}
		})
	}
}

// The coverage of a result is read by the one strict reader of the entries: a field of a row it does not know is refused, and a
// row it knows is read as sent.
func TestWorkCoverageRowsAreReadByTheStrictReader(t *testing.T) {
	var req workRequest
	if err, extra := decodeStrictObject([]byte(`{"receipt":"r","result":{"changed":"x","evidence":["new1"],"coverage":[{"area":"export filters","outcome":"clear","evidence":"new1"},{"area":"export totals","outcome":"excluded","reason":"out of scope"}]}}`), &req); err != nil || extra {
		t.Fatalf("a valid coverage list is read: %v", err)
	}
	if got := req.Result.Coverage; len(got) != 2 || got[0] != coverageRow("export filters", "clear", "new1") || got[1] != coverageRow("export totals", "excluded", "", "out of scope") {
		t.Fatalf("the rows are not read as sent: %+v", got)
	}
	err, _ := decodeStrictObject([]byte(`{"receipt":"r","result":{"changed":"x","coverage":[{"area":"export filters","outcome":"open","status":"todo"}]}}`), &workRequest{})
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("a field of a row that coverage does not have is accepted: %v", err)
	}
}

type coverageFlow struct {
	t *testing.T
	b notebook.Book
}

func newCoverageFlow(t *testing.T) coverageFlow {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, ".shikii")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return coverageFlow{t: t, b: notebook.Book{Root: root, Scope: dir}}
}

// prepare starts a work of its own (no hook ids): every call is another session of the same project.
func (f coverageFlow) prepare() map[string]any {
	f.t.Helper()
	value, err := runWork(f.b.Root, f.b.Scope, "prepare", workRequest{Query: "export audit", Request: "Audit the export."})
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value)
}

// record sends a result that cites an observation of its call as new1.
func (f coverageFlow) record(prepared map[string]any, c workCompletion) map[string]any {
	f.t.Helper()
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &c, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters."}}})
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value)
}

// state is the work state a receipt holds.
func (f coverageFlow) state(prepared map[string]any) *workState {
	f.t.Helper()
	token, err := workDecode(prepared["receipt"].(string))
	if err != nil {
		f.t.Fatal(err)
	}
	dir, err := openWorkSessions(f.b.Root)
	if err != nil {
		f.t.Fatal(err)
	}
	defer dir.Close()
	w, _, err := loadWorkState(dir, f.b.Scope, token)
	if err != nil {
		f.t.Fatal(err)
	}
	return w
}

// ledger is the lines of the project's coverage ledger.
func (f coverageFlow) ledger() []map[string]any {
	f.t.Helper()
	files, err := filepath.Glob(filepath.Join(f.b.Root, "records", "coverage", "*.jsonl"))
	if err != nil {
		f.t.Fatal(err)
	}
	lines := []map[string]any{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				f.t.Fatal(err)
			}
			lines = append(lines, row)
		}
	}
	return lines
}

// coverageReplyOf is the coverage a record's reply carries; a reply without it fails the test where it is read.
func coverageReplyOf(t *testing.T, saved map[string]any) map[string]any {
	t.Helper()
	reply, ok := saved["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("the reply of the record has no coverage: %v", saved)
	}
	return reply
}

func openAreasOf(t *testing.T, prepared map[string]any) []string {
	t.Helper()
	coverage, ok := prepared["coverage"].(map[string]any)
	if !ok {
		return nil
	}
	areas := []string{}
	for _, raw := range coverage["rows"].([]any) {
		areas = append(areas, raw.(map[string]any)["area"].(string))
	}
	return areas
}

// An open row in a result is kept: the reply of the record names it, the result is unresolved, and the preparation of another
// session lists it with the saved result it came from (a note on what is left, not an instruction).
func TestWorkCoverageOpenRowsReachTheNextPreparation(t *testing.T) {
	f := newCoverageFlow(t)
	first := f.prepare()
	if first["coverage"] != nil {
		t.Fatalf("a project without an open row has no coverage in its preparation: %v", first["coverage"])
	}
	saved := f.record(first, coverageResult(coverageRow("export filters", "open", "", "ran out of time"), coverageRow("export totals", "clear", "new1")))
	if saved["complete"] != true {
		t.Fatalf("the record is complete: %v", saved)
	}
	reply, _ := saved["coverage"].(map[string]any)
	if reply == nil || reply["rows"] != float64(2) || len(reply["open"].([]any)) != 1 || reply["open"].([]any)[0] != "export filters" {
		t.Fatalf("the reply names the rows and the area left open: %v", saved["coverage"])
	}
	w := f.state(first)
	if w.Result == nil || !w.Result.Ready || !w.Result.Unresolved {
		t.Fatalf("a result with an open row is unresolved, as one with remaining work: %+v", w.Result)
	}
	second := f.prepare()
	coverage, _ := second["coverage"].(map[string]any)
	if coverage == nil || coverage["open"] != float64(1) || !slices.Equal(openAreasOf(t, second), []string{"export filters"}) {
		t.Fatalf("another session's preparation lists the open area: %v", second["coverage"])
	}
	row := coverage["rows"].([]any)[0].(map[string]any)
	if row["source"] != w.Result.Source || len(row) != 2 {
		t.Fatalf("a row names its area and the saved result it came from, and nothing else: %v (result %s)", row, w.Result.Source)
	}
	if next, _ := coverage["next_action"].(string); !strings.Contains(next, "not an instruction") || !strings.Contains(next, "result.coverage") {
		t.Fatalf("the note says it is not an instruction and how to close an area: %q", next)
	}
	// a result without coverage says nothing of it
	if plain := f.record(second, workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}}); plain["coverage"] != nil {
		t.Fatalf("a result without coverage has no coverage in its reply: %v", plain["coverage"])
	}
}

// A later result that records the same area as clear (with evidence) closes it; a row for another area stays open.
func TestWorkCoverageALaterResultClosesOnlyItsArea(t *testing.T) {
	f := newCoverageFlow(t)
	f.record(f.prepare(), coverageResult(coverageRow("export filters", "open"), coverageRow("export totals", "open"), coverageRow("export dates", "open")))
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"export dates", "export totals", "export filters"}) {
		t.Fatalf("three open areas, the newest first: %q", got)
	}
	closed := f.record(f.prepare(), coverageResult(coverageRow("export  filters", "clear", "new1")))
	if open, _ := coverageReplyOf(t, closed)["open"].([]any); closed["complete"] != true || open == nil || len(open) != 0 {
		t.Fatalf("a result that closes an area leaves none open of its own: %v", closed["coverage"])
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"export dates", "export totals"}) {
		t.Fatalf("the closed area is gone and the unrelated rows stay: %q", got)
	}
	f.record(f.prepare(), coverageResult(coverageRow("export totals", "excluded", "", "out of scope"), coverageRow("export dates", "reported", "new1")))
	if after := f.prepare(); after["coverage"] != nil {
		t.Fatalf("every area is closed, so the preparation carries no coverage: %v", after["coverage"])
	}
}

// The same record sent again with the same receipt adds no line to the ledger.
func TestWorkCoverageTheSameRecordSentAgainAddsNoLine(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	result := coverageResult(coverageRow("export filters", "open"))
	f.record(prepared, result)
	if n := len(f.ledger()); n != 1 {
		t.Fatalf("the first record wrote %d lines, want 1", n)
	}
	for range 2 {
		again := f.record(prepared, result)
		if again["complete"] != true {
			t.Fatalf("the record sent again is complete: %v", again)
		}
	}
	if n := len(f.ledger()); n != 1 {
		t.Fatalf("the record sent again wrote lines: %d, want 1", n)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"export filters"}) {
		t.Fatalf("%q", got)
	}
}

// A clear, reported or excluded row for an area that was never open stays in the saved result and adds nothing to the ledger.
func TestWorkCoverageAClearRowForAnAreaNeverOpenIsNotInTheLedger(t *testing.T) {
	f := newCoverageFlow(t)
	saved := f.record(f.prepare(), coverageResult(coverageRow("export filters", "clear", "new1"), coverageRow("export totals", "excluded", "", "out of scope"), coverageRow("export dates", "reported")))
	if saved["complete"] != true || coverageReplyOf(t, saved)["rows"] != float64(3) {
		t.Fatalf("the record is complete and says it held 3 rows: %v", saved)
	}
	if lines := f.ledger(); len(lines) != 0 {
		t.Fatalf("rows for areas that were never open were written: %v", lines)
	}
}

// A result that waits for its evidence writes nothing; the repair by ref that completes it writes its coverage.
func TestWorkCoverageIsWrittenWhenARepairByRefCompletesTheResult(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Changed: "Audited the export.", Evidence: []string{"new1"}, Coverage: []workCoverageRow{coverageRow("export filters", "open")}}})
	if err != nil {
		t.Fatal(err)
	}
	waiting := loopObject(t, value)
	id := f.state(prepared).Result.Source
	if waiting["complete"] == true || id == "" {
		t.Fatalf("the result waits for its evidence (new1 names no observation): %v", waiting)
	}
	if waiting["coverage"] != nil || len(f.ledger()) != 0 {
		t.Fatalf("a result that is not ready writes no coverage: %v %v", waiting["coverage"], f.ledger())
	}
	value, err = runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Ref: id, Evidence: []string{"new1"}}, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters."}}})
	if err != nil {
		t.Fatal(err)
	}
	repaired := loopObject(t, value)
	if repaired["complete"] != true {
		t.Fatalf("the repair completes the result: %v", repaired)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"export filters"}) {
		t.Fatalf("the coverage of the repaired result reaches the next preparation: %q (reply %v)", got, repaired["coverage"])
	}
	if reply, _ := repaired["coverage"].(map[string]any); reply == nil || reply["rows"] != float64(1) {
		t.Fatalf("the reply of the repair names the rows: %v", repaired["coverage"])
	}
}

// A ledger that cannot be written is an error of the record, which the same record sent again repairs.
func TestWorkCoverageALedgerThatCannotBeWrittenIsAnErrorOfTheRecord(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	blocker := filepath.Join(f.b.Root, "records")
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "coverage"), []byte("a file where the folder of the ledger goes"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := f.record(prepared, coverageResult(coverageRow("export filters", "open")))
	var failed map[string]any
	errs, _ := saved["errors"].([]any)
	for _, raw := range errs {
		if item := raw.(map[string]any); item["kind"] == "coverage" {
			failed = item
		}
	}
	if saved["complete"] == true || failed == nil || !strings.Contains(failed["error"].(string), "send the same record again") {
		t.Fatalf("the record names the ledger it could not write: %v", saved)
	}
	if err := os.Remove(filepath.Join(blocker, "coverage")); err != nil {
		t.Fatal(err)
	}
	if again := f.record(prepared, coverageResult(coverageRow("export filters", "open"))); again["complete"] != true {
		t.Fatalf("the same record sent again writes the ledger: %v", again)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"export filters"}) {
		t.Fatalf("%q", got)
	}
}

// withRemaining is a result that wrote what it left in remaining, as agents do.
func withRemaining(c workCompletion, remaining string) workCompletion {
	c.Remaining = remaining
	return c
}

// A result that wrote what was left in remaining and sent no coverage row is told, in the reply of its record, that remaining is
// not shown to later work and how an area is sent. The reply holds no row, nothing reaches the ledger, and a later preparation
// has no coverage.
func TestWorkCoverageRemainingWithoutRowsGetsTheHint(t *testing.T) {
	f := newCoverageFlow(t)
	saved := f.record(f.prepare(), withRemaining(coverageResult(), "The import path was not read."))
	if saved["complete"] != true {
		t.Fatalf("the record is complete: %v", saved)
	}
	reply := coverageReplyOf(t, saved)
	open, isList := reply["open"].([]any)
	next, _ := reply["next_action"].(string)
	if reply["rows"] != float64(0) || !isList || len(open) != 0 || len(reply) != 3 {
		t.Fatalf("the reply holds no row, an empty open list and the hint: %v", reply)
	}
	for _, part := range []string{
		"result.remaining is kept with this result but is not shown to later work",
		"send this result again with",
		`coverage:[{"area":"<short name of what was not examined>","outcome":"open"}]`,
		"later preparations of this project then list the area until a result closes it",
		"Not needed when nothing was left unexamined.",
	} {
		if !strings.Contains(next, part) {
			t.Fatalf("the hint says %q: %q", part, next)
		}
	}
	if lines := f.ledger(); len(lines) != 0 {
		t.Fatalf("the hint writes nothing to the ledger: %v", lines)
	}
	if after := f.prepare(); after["coverage"] != nil {
		t.Fatalf("a later preparation has no coverage: %v", after["coverage"])
	}
}

// The hint is for a remaining without coverage rows only: a result with rows keeps the reply it had, and a result with no remaining
// (or a blank one) has no coverage key at all.
func TestWorkCoverageHintIsGivenOnlyToARemainingWithoutRows(t *testing.T) {
	for _, c := range []struct {
		name     string
		result   workCompletion
		wantRows any // the rows the reply names, or nil when it has no coverage key
	}{
		{"remaining and an open row", withRemaining(coverageResult(coverageRow("import path", "open")), "The import path was not read."), float64(1)},
		{"remaining and a clear row", withRemaining(coverageResult(coverageRow("export filters", "clear", "new1")), "Nothing else was read."), float64(1)},
		{"no remaining and no row", coverageResult(), nil},
		{"blank remaining and no row", withRemaining(coverageResult(), " \n\t "), nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCoverageFlow(t)
			got := f.record(f.prepare(), c.result)
			if got["complete"] != true {
				t.Fatalf("the record is complete: %v", got)
			}
			reply, has := got["coverage"].(map[string]any)
			if c.wantRows == nil {
				if has || got["coverage"] != nil {
					t.Fatalf("no coverage key: %v", got["coverage"])
				}
				return
			}
			if _, hint := reply["next_action"]; !has || reply["rows"] != c.wantRows || hint {
				t.Fatalf("the reply names its rows and has no hint: %v", got["coverage"])
			}
		})
	}
}

// A repair by ref is not told the hint (it cannot add coverage), and neither is a result that is not ready yet: the hint comes with
// the record that made the result ready, and only a full result.
func TestWorkCoverageNoHintOnARepairByRefOrAResultThatIsNotReady(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Changed: "Audited the export.", Remaining: "The import path was not read.", Evidence: []string{"new1"}}})
	if err != nil {
		t.Fatal(err)
	}
	waiting := loopObject(t, value)
	id := f.state(prepared).Result.Source
	if waiting["complete"] == true || id == "" {
		t.Fatalf("the result waits for its evidence (new1 names no observation): %v", waiting)
	}
	if waiting["coverage"] != nil {
		t.Fatalf("a result that is not ready has no coverage in its reply: %v", waiting["coverage"])
	}
	value, err = runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Ref: id, Evidence: []string{"new1"}}, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters."}}})
	if err != nil {
		t.Fatal(err)
	}
	repaired := loopObject(t, value)
	if repaired["complete"] != true || repaired["coverage"] != nil {
		t.Fatalf("the repair completes the result and its reply has no coverage, so no hint: %v", repaired)
	}
}

// The way the hint asks for works: the result is sent again with one open row added, with the same receipt. The record is complete,
// the ledger holds the open row, the reply names it in open and carries no hint, and the next preparation lists the area.
func TestWorkCoverageResendingTheResultWithAnOpenRowDoesWhatTheHintAsks(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	result := withRemaining(coverageResult(), "The import path was not read.")
	if first := f.record(prepared, result); coverageReplyOf(t, first)["next_action"] == nil {
		t.Fatalf("the first record is told the hint: %v", first)
	}
	result.Coverage = []workCoverageRow{coverageRow("import path", "open")}
	second := f.record(prepared, result)
	if second["complete"] != true {
		t.Fatalf("the result sent again with a row is complete: %v", second)
	}
	reply := coverageReplyOf(t, second)
	open, _ := reply["open"].([]any)
	if _, hint := reply["next_action"]; reply["rows"] != float64(1) || len(open) != 1 || open[0] != "import path" || hint {
		t.Fatalf("the reply names the open area and has no hint: %v", reply)
	}
	lines := f.ledger()
	if len(lines) != 1 || lines[0]["area"] != "import path" || lines[0]["outcome"] != "open" || lines[0]["source"] != f.state(prepared).Result.Source {
		t.Fatalf("the ledger holds the open row, from the result that was sent again: %v", lines)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"import path"}) {
		t.Fatalf("the next preparation lists the area: %q", got)
	}
}

// A refusal of the coverage happens before anything is saved.
func TestWorkCoverageRefusedBeforeAnythingIsSaved(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	_, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Changed: "Audited the export.", Evidence: []string{"new1"}, Coverage: []workCoverageRow{coverageRow("export filters", "clear")}}, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters."}}})
	if err == nil || !strings.Contains(err.Error(), "is clear without evidence") {
		t.Fatalf("the record is refused: %v", err)
	}
	if w := f.state(prepared); w.Result != nil || len(w.Saved) != 0 || len(w.Captures) != 0 || len(f.ledger()) != 0 {
		t.Fatalf("a refused record saved something: %+v", w)
	}
}

// With an assignment, a preparation and a record behave as they do without one: the record writes the ledger, and a later
// preparation of another assignment carries the open rows (beside its teamwork context, which has its own coverage of peers).
func TestWorkCoverageWorksWithAnAssignment(t *testing.T) {
	b, _ := loopBook(t)
	first := assignmentInput("audit-1", "case-1", "auditor", "audit")
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "export audit", "assignment": first})
	if err != nil {
		t.Fatal(err)
	}
	if prepared["coverage"] != nil {
		t.Fatalf("no row is open yet: %v", prepared["coverage"])
	}
	saved, err := workCall(t, b, "record", map[string]any{"assignment": first, "receipt": prepared["receipt"],
		"observations": []any{map[string]any{"kind": "test", "title": "Export check", "content": "The synthetic export kept its filters."}},
		"result":       map[string]any{"changed": "Audited the export.", "evidence": []string{"new1"}, "coverage": []any{map[string]any{"area": "export filters", "outcome": "open", "reason": "not reached"}}}})
	if err != nil || saved["complete"] != true {
		t.Fatalf("the record is complete: %v %v", saved, err)
	}
	if reply, _ := saved["coverage"].(map[string]any); reply == nil || reply["rows"] != float64(1) {
		t.Fatalf("the reply of an assignment's record names the rows: %v", saved["coverage"])
	}
	second := assignmentInput("audit-2", "case-1", "auditor", "audit")
	next, err := workCall(t, b, "prepare", map[string]any{"query": "export audit", "assignment": second})
	if err != nil {
		t.Fatal(err)
	}
	coverage, _ := next["coverage"].(map[string]any)
	if coverage == nil || coverage["open"] != float64(1) || !slices.Equal(openAreasOf(t, next), []string{"export filters"}) {
		t.Fatalf("the preparation of another assignment lists the open area: %v", next["coverage"])
	}
	if next["teamwork"] == nil {
		t.Fatalf("the teamwork context is still there: %v", slices.Collect(func(yield func(string) bool) {
			for k := range next {
				if !yield(k) {
					return
				}
			}
		}))
	}
}

// A preparation whose bound is tight drops coverage after reasoning, reading and attention and before pending and observations,
// and names it in output.omitted. Each of the six is large enough that the order, not the size, decides what goes first.
func TestLimitWorkPreparationDropsCoverageInItsPlace(t *testing.T) {
	pad := func(n int) string { return strings.Repeat("x", n) }
	reply := map[string]any{
		"receipt": "r", "how_to_record": pad(2000),
		"reasoning": map[string]any{"p": pad(30)}, "reading": map[string]any{"p": pad(30)}, "attention": map[string]any{"p": pad(30)},
		"coverage": map[string]any{"open": 3, "rows": []any{map[string]any{"area": pad(1500), "source": "s"}}},
		"pending":  []any{map[string]any{"p": pad(2500)}}, "observations": []any{map[string]any{"p": pad(2500)}},
	}
	if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 8192); err != nil {
		t.Fatal(err)
	}
	output, _ := reply["output"].(map[string]any)
	omitted, _ := output["omitted"].([]string)
	if !slices.Equal(omitted, []string{"reasoning", "reading", "attention", "coverage"}) {
		t.Fatalf("omitted is %v: coverage goes after attention and before pending and observations, and only as far as the bound needs", omitted)
	}
	if reply["pending"] == nil || reply["observations"] == nil || reply["coverage"] != nil {
		t.Fatalf("pending and observations stay and coverage goes: %v", slices.Sorted(func(yield func(string) bool) {
			for k := range reply {
				if !yield(k) {
					return
				}
			}
		}))
	}
}

// The schema an agent reads names exactly the rows the reader keeps: the four outcomes of the ledger, a row's four fields (area
// and outcome required), at most as many rows as the validation takes, and no other field.
func TestWorkSchemaCoverageIsTheLedgersAllowList(t *testing.T) {
	result := workSchema["properties"].(map[string]any)["result"].(map[string]any)["properties"].(map[string]any)
	coverage, _ := result["coverage"].(map[string]any)
	if coverage == nil {
		t.Fatal("result has no coverage in the schema an agent reads")
	}
	items := coverage["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	names := []string{}
	for name := range properties {
		names = append(names, name)
	}
	slices.Sort(names)
	outcomes := properties["outcome"].(map[string]any)["enum"].([]string)
	if coverage["maxItems"] != workCoverageMax || items["additionalProperties"] != false || !slices.Equal(names, []string{"area", "evidence", "outcome", "reason"}) || !slices.Equal(items["required"].([]string), []string{"area", "outcome"}) {
		t.Fatalf("the shape of a row is not the reader's: %v", items)
	}
	want := []string{store.CoverageReported, store.CoverageClear, store.CoverageExcluded, store.CoverageOpen}
	slices.Sort(outcomes)
	slices.Sort(want)
	if !slices.Equal(outcomes, want) {
		t.Fatalf("the outcomes of the schema are %v, the ledger's are %v", outcomes, want)
	}
	for _, outcome := range outcomes {
		if !store.CoverageOutcome(outcome) {
			t.Fatalf("%s is offered to agents and the ledger drops it", outcome)
		}
	}
}

// A later request may repair the result an earlier request left waiting for its evidence; the coverage of the repaired result
// reaches the ledger although the receipt of the later request holds no result of its own.
func TestWorkCoverageIsWrittenWhenALaterRequestRepairsAnEarlierResult(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	waiting, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Audited the export.", "evidence": []any{"new1"}, "coverage": []any{map[string]any{"area": "export filters", "outcome": "open"}}}})
	if err != nil || waiting["complete"] == true {
		t.Fatalf("the result waits for its evidence: %v %v", waiting, err)
	}
	id := ""
	for _, raw := range waiting["errors"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "result" {
			id, _ = item["id"].(string)
		}
	}
	if id == "" {
		t.Fatalf("the waiting result names its id: %v", waiting["errors"])
	}
	read := func() []store.CoverageRow {
		rows, err := (store.Store{Dir: f.root}).OpenCoverage(f.b.Scope)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := read(); len(rows) != 0 {
		t.Fatalf("a result that waits for its evidence writes nothing: %v", rows)
	}
	second := f.prepare(f.ask("Now check the dates of the CSV export."), "CSV export dates")
	repaired, err := f.record(second, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Export check", "content": "The synthetic export kept its filters."}}, "result": map[string]any{"ref": id, "evidence": []any{"new1"}}})
	if err != nil || repaired["complete"] != true {
		t.Fatalf("the repair of the earlier result is complete: %v %v", repaired, err)
	}
	rows := read()
	if len(rows) != 1 || rows[0].Area != "export filters" || rows[0].Source != id {
		t.Fatalf("the coverage of the repaired result is in the ledger under its source: %+v (want source %s)", rows, id)
	}
	if reply := coverageReplyOf(t, repaired); reply["rows"] != float64(1) {
		t.Fatalf("%v", reply)
	}
}

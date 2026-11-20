package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

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
		{"area over 160 bytes as sent, short once its white space is normalized", coverageResult(coverageRow("a"+strings.Repeat(" ", 200)+"b", "open")), "area must be 1-160 bytes"},
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

// session is the session id a receipt names. Every standalone preparation of a coverageFlow is a session of its own.
func (f coverageFlow) session(prepared map[string]any) string {
	f.t.Helper()
	token, err := workDecode(prepared["receipt"].(string))
	if err != nil {
		f.t.Fatal(err)
	}
	return token.Session
}

// prepareTight starts a work whose request is so large for its bound that the preparation leaves coverage out of its reply.
func (f coverageFlow) prepareTight() map[string]any {
	f.t.Helper()
	request := strings.Repeat("Audit the export carefully. ", 143)[:4001]
	value, err := runWork(f.b.Root, f.b.Scope, "prepare", workRequest{Query: "export audit", Request: request, MaxBytes: 8192})
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value)
}

// listedRow is the row of a preparation's coverage that names an area, or nil.
func listedRow(prepared map[string]any, area string) map[string]any {
	coverage, _ := prepared["coverage"].(map[string]any)
	rows, _ := coverage["rows"].([]any)
	for _, raw := range rows {
		if row := raw.(map[string]any); row["area"] == area {
			return row
		}
	}
	return nil
}

func handedOverOf(saved map[string]any) []any {
	reply, _ := saved["coverage"].(map[string]any)
	handed, _ := reply["handed_over"].([]any)
	return handed
}

// A result that left something in remaining and sent no coverage row opens an area from it, by itself: the ledger holds an open row
// of origin remaining with the session of the work (the first 16 hex digits of the sha256 of its id), the reply of the record names
// the area in open, and the preparation of another session lists it as from:remaining, with a note that says how it closes.
func TestWorkCoverageRemainingBecomesAnOpenRowThatTheNextWorkIsHanded(t *testing.T) {
	f := newCoverageFlow(t)
	first := f.prepare()
	const area = "The import path was not read."
	saved := f.record(first, withRemaining(coverageResult(), area))
	reply := coverageReplyOf(t, saved)
	open, _ := reply["open"].([]any)
	if saved["complete"] != true || reply["rows"] != float64(0) || len(open) != 1 || open[0] != area || len(reply) != 2 {
		t.Fatalf("the reply holds no coverage row and names the area that was opened, and nothing else: %v", reply)
	}
	sum := sha256.Sum256([]byte(f.session(first)))
	lines := f.ledger()
	if len(lines) != 1 || lines[0]["area"] != area || lines[0]["outcome"] != "open" || lines[0]["origin"] != "remaining" || lines[0]["session"] != hex.EncodeToString(sum[:])[:16] || lines[0]["source"] != f.state(first).Result.Source || len(lines[0]) != 6 {
		t.Fatalf("the ledger holds one open row of origin remaining with the session and the source of the result: %v", lines)
	}
	second := f.prepare()
	row := listedRow(second, area)
	if row == nil || row["from"] != "remaining" || row["source"] != f.state(first).Result.Source || len(row) != 3 {
		t.Fatalf("another session's preparation lists the area as from remaining, with its source: %v", second["coverage"])
	}
	next, _ := second["coverage"].(map[string]any)["next_action"].(string)
	if !strings.HasPrefix(next, "These areas were left unexamined by earlier work.") || len(next) > 460 {
		t.Fatalf("the note begins with the words the measurement looks for and is about 400 bytes (%d): %q", len(next), next)
	}
	for _, part := range []string{"not an instruction", "from:remaining", "result.remaining", "when this work records a result", "result.coverage", "clear (with evidence)", "excluded (with a reason)"} {
		if !strings.Contains(next, part) {
			t.Fatalf("the note says %q: %q", part, next)
		}
	}
}

// What says that nothing was left opens no area: the fixed list (compared without case, without the space and the punctuation
// around it), a text of punctuation only and a blank one. A phrase that is not on the list opens one (the ledger takes it, and it
// costs one handover). The reply of such a record has no coverage at all.
func TestWorkCoverageRemainingThatSaysNothingOpensNoArea(t *testing.T) {
	for _, c := range []struct {
		remaining string
		opens     bool
	}{
		{"none", false}, {"None", false}, {"NONE.", false}, {"nothing", false}, {"Nothing!", false}, {"n/a", false}, {"N/A", false},
		{"na", false}, {"NA", false}, {"no", false}, {"No.", false}, {"  none \n", false}, {"(none)", false}, {"- n/a -", false},
		{"\u306a\u3057", false}, {"\u306a\u3057\u3002", false}, {"\u7121\u3057", false}, {"\u7279\u306b\u306a\u3057", false},
		{"\u3042\u308a\u307e\u305b\u3093", false}, {"\u3042\u308a\u307e\u305b\u3093\u3002", false}, {"\u306a\u3044", false}, {"\u7121\u3044", false},
		{"\u3000\u306a\u3057\u3000", false},
		{"", false}, {"   ", false}, {"\n\t", false}, {"...", false}, {"\u2014", false}, {"\u3002\u3002", false}, {"+", false}, {"$", false},
		{"not done", true}, {"none of the dates were checked", true}, {"N/A for filters; import path not read", true}, {"no tests ran", true},
		{"\u30c6\u30b9\u30c8\u672a\u5b9f\u65bd", true},
	} {
		t.Run(strings.TrimSpace(c.remaining), func(t *testing.T) {
			f := newCoverageFlow(t)
			saved := f.record(f.prepare(), withRemaining(coverageResult(), c.remaining))
			if saved["complete"] != true {
				t.Fatalf("the record is complete: %v", saved)
			}
			lines := f.ledger()
			if c.opens {
				open, _ := coverageReplyOf(t, saved)["open"].([]any)
				if len(lines) != 1 || len(open) != 1 || lines[0]["origin"] != "remaining" {
					t.Fatalf("a phrase that is not on the list opens one area: %v %v", lines, saved["coverage"])
				}
				return
			}
			if len(lines) != 0 || saved["coverage"] != nil {
				t.Fatalf("a text that says nothing was left opens no area and the reply has no coverage: %v %v", lines, saved["coverage"])
			}
		})
	}
}

// The area is the text with each run of white space (a control character too) as one space; a text over 160 bytes is cut before the
// 160th, never inside a character, and ends in an ellipsis (3 bytes). The expected areas are written out here.
func TestWorkCoverageRemainingAreaIsNormalizedAndCutWithinTheLimit(t *testing.T) {
	ascii := func(n int) string { return strings.Repeat("x", n) }
	for _, c := range []struct {
		name      string
		remaining string
		want      string
	}{
		{"white space runs and line breaks", " The import path\n\nwas   not\tread. \r\n", "The import path was not read."},
		{"control characters", "alpha\x01beta\u0085gamma\x00delta", "alpha beta gamma delta"},
		{"exactly 160 bytes", ascii(160), ascii(160)},
		{"161 bytes", ascii(161), ascii(157) + "\u2026"},
		{"normalized before it is measured", ascii(100) + "\n\n\n\n" + ascii(100), ascii(100) + " " + ascii(56) + "\u2026"},
		{"Japanese text is cut between characters", strings.Repeat("\u3042", 100), strings.Repeat("\u3042", 52) + "\u2026"},
		{"a character that starts at the cut stays out", ascii(154) + strings.Repeat("\u3042", 3), ascii(154) + "\u3042" + "\u2026"},
		{"a character that ends at the limit", ascii(157) + "\u3042", ascii(157) + "\u3042"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCoverageFlow(t)
			f.record(f.prepare(), withRemaining(coverageResult(), c.remaining))
			lines := f.ledger()
			if len(lines) != 1 {
				t.Fatalf("the ledger holds one row: %v", lines)
			}
			if area := lines[0]["area"].(string); area != c.want || len(area) > 160 || !utf8.ValidString(area) {
				t.Fatalf("the area is %q (%d bytes), want %q of at most 160 bytes of UTF-8", area, len(area), c.want)
			}
		})
	}
}

// What a result with coverage rows does with its remaining: a result that wrote an open row itself is using the ledger for what it
// left, so its remaining is not made a second row; a result whose rows are reported, clear or excluded (and open nothing) leaves
// its remaining in the ledger as an open row of origin remaining, like a result with no row at all. The clear row of such a result
// closes the area it names as before.
func TestWorkCoverageRemainingOpensAnAreaUnlessTheResultWroteAnOpenRow(t *testing.T) {
	const left = "The import path was not read."
	for _, c := range []struct {
		name string
		rows []workCoverageRow
	}{
		{"clear row", []workCoverageRow{coverageRow("export filters", "clear", "new1")}},
		{"reported row", []workCoverageRow{coverageRow("export filters", "reported")}},
		{"excluded row", []workCoverageRow{coverageRow("export filters", "excluded", "", "out of scope")}},
		{"all three kinds", []workCoverageRow{coverageRow("export filters", "clear", "new1"), coverageRow("export totals", "reported"), coverageRow("export dates", "excluded", "", "out of scope")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCoverageFlow(t)
			first := f.prepare()
			saved := f.record(first, withRemaining(coverageResult(c.rows...), left))
			reply := coverageReplyOf(t, saved)
			open, _ := reply["open"].([]any)
			lines := f.ledger()
			if saved["complete"] != true || reply["rows"] != float64(len(c.rows)) || len(open) != 1 || open[0] != left || handedOverOf(saved) != nil {
				t.Fatalf("the reply names the rows it sent and the area made from its remaining: %v", saved["coverage"])
			}
			if len(lines) != 1 || lines[0]["area"] != left || lines[0]["outcome"] != "open" || lines[0]["origin"] != "remaining" || lines[0]["source"] != f.state(first).Result.Source {
				t.Fatalf("the ledger holds one open row of origin remaining and none for the rows that closed nothing: %v", lines)
			}
			next := f.prepare()
			if row := listedRow(next, left); row == nil || row["from"] != "remaining" || row["source"] != f.state(first).Result.Source || !slices.Equal(openAreasOf(t, next), []string{left}) {
				t.Fatalf("the next preparation lists the area as from remaining, and no other: %v", next["coverage"])
			}
		})
	}
	// the clear row of such a result closes the area it names, as it does without a remaining
	f := newCoverageFlow(t)
	f.record(f.prepare(), coverageResult(coverageRow("export filters", "open")))
	saved := f.record(f.prepare(), withRemaining(coverageResult(coverageRow("export filters", "clear", "new1")), left))
	if reply := coverageReplyOf(t, saved); reply["rows"] != float64(1) || !slices.Equal(reply["open"].([]any), []any{left}) {
		t.Fatalf("the reply names the clear row and the area of the remaining: %v", saved["coverage"])
	}
	if lines := f.ledger(); len(lines) != 3 || lines[1]["area"] != "export filters" || lines[1]["outcome"] != "clear" || lines[2]["area"] != left || lines[2]["origin"] != "remaining" {
		t.Fatalf("the clear row closes its area (once) and the remaining opens its own: %v", lines)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{left}) {
		t.Fatalf("only the area of the remaining is open: %q", got)
	}
	// a result that wrote an open row itself does not make its remaining a row: only the row of the agent is in the ledger
	g := newCoverageFlow(t)
	saved = g.record(g.prepare(), withRemaining(coverageResult(coverageRow("export totals", "open")), left))
	reply := coverageReplyOf(t, saved)
	lines := g.ledger()
	if open, _ := reply["open"].([]any); reply["rows"] != float64(1) || len(open) != 1 || open[0] != "export totals" || len(lines) != 1 || lines[0]["area"] != "export totals" || lines[0]["origin"] != nil {
		t.Fatalf("only the row of the agent is in the ledger, with no origin: %v %v", saved["coverage"], lines)
	}
	// a result with an open row and a clear row is the same: it wrote an open row
	h := newCoverageFlow(t)
	saved = h.record(h.prepare(), withRemaining(coverageResult(coverageRow("export filters", "clear", "new1"), coverageRow("export totals", "open")), left))
	if lines := h.ledger(); len(lines) != 1 || lines[0]["area"] != "export totals" || len(coverageReplyOf(t, saved)["open"].([]any)) != 1 {
		t.Fatalf("a result with an open row among others does not make its remaining a row: %v %v", saved["coverage"], lines)
	}
}

// A work that was handed an area made from remaining closes it by recording a result, with no row about it: the closing line is an
// excluded row that names the result, the reply says handed_over (with nothing of its own open), and the next preparation does not
// list the area. The work remembered the area it was handed in its saved state.
func TestWorkCoverageTheWorkThatWasHandedARemainingClosesItByRecording(t *testing.T) {
	f := newCoverageFlow(t)
	const area = "The import path was not read."
	f.record(f.prepare(), withRemaining(coverageResult(), area))
	second := f.prepare()
	if shown := f.state(second).CoverageShown; !slices.Equal(shown, []string{area}) {
		t.Fatalf("the work remembers the areas its preparation listed: %q", shown)
	}
	saved := f.record(second, workCompletion{Changed: "Read the import path.", Evidence: []string{"new1"}})
	reply := coverageReplyOf(t, saved)
	open, isList := reply["open"].([]any)
	if saved["complete"] != true || reply["rows"] != float64(0) || !isList || len(open) != 0 || !slices.Equal(handedOverOf(saved), []any{area}) || len(reply) != 3 {
		t.Fatalf("the reply says what was handed over and that nothing of its own is open: %v", reply)
	}
	lines, source := f.ledger(), f.state(second).Result.Source
	if len(lines) != 2 || lines[1]["area"] != area || lines[1]["outcome"] != "excluded" || lines[1]["source"] != source || lines[1]["reason"] != "handed over to the work that recorded "+source || lines[1]["origin"] != nil || lines[1]["session"] != nil {
		t.Fatalf("the closing line is an excluded row that names the result that took it over: %v", lines)
	}
	if after := f.prepare(); after["coverage"] != nil {
		t.Fatalf("the area is closed: %v", after["coverage"])
	}
}

// A row is closed only for a work that was handed it: a work prepared before the row was written, and a work whose preparation
// left coverage out for its bound, record results and the area stays open for the next preparation.
func TestWorkCoverageARemainingIsNotHandedToAWorkThatWasNotGivenIt(t *testing.T) {
	f := newCoverageFlow(t)
	const area = "The import path was not read."
	before := f.prepare() // prepared before the row exists
	f.record(f.prepare(), withRemaining(coverageResult(), area))
	tight := f.prepareTight()
	if output, _ := tight["output"].(map[string]any); tight["coverage"] != nil || !slices.Contains(output["omitted"].([]any), any("coverage")) {
		t.Fatalf("the bound left coverage out of this preparation: %v", tight["output"])
	}
	if f.state(tight).CoverageShown != nil || f.state(before).CoverageShown != nil {
		t.Fatalf("a work remembers no area when its reply listed none: %q %q", f.state(tight).CoverageShown, f.state(before).CoverageShown)
	}
	for name, prepared := range map[string]map[string]any{"before the row": before, "bound": tight} {
		saved := f.record(prepared, workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}})
		if saved["complete"] != true || saved["coverage"] != nil {
			t.Fatalf("%s: the record takes nothing over and says nothing of coverage: %v", name, saved["coverage"])
		}
		if len(f.ledger()) != 1 || !slices.Equal(openAreasOf(t, f.prepare()), []string{area}) {
			t.Fatalf("%s: the area is still open: %v", name, f.ledger())
		}
	}
}

// A preparation lists the five newest open areas and the work is handed those: the sixth stays open when the work records.
func TestWorkCoverageOnlyTheAreasTheReplyListedAreHandedOver(t *testing.T) {
	f := newCoverageFlow(t)
	areas := []string{"area one", "area two", "area three", "area four", "area five", "area six"}
	prepared := []map[string]any{}
	for range areas {
		prepared = append(prepared, f.prepare()) // all prepared before any of them records: none is handed an area
	}
	for i, area := range areas {
		f.record(prepared[i], withRemaining(coverageResult(), area))
	}
	second := f.prepare()
	listed := openAreasOf(t, second)
	if !slices.Equal(listed, []string{"area six", "area five", "area four", "area three", "area two"}) || second["coverage"].(map[string]any)["open"] != float64(6) {
		t.Fatalf("the preparation lists the five newest of six open areas: %q", listed)
	}
	saved := f.record(second, workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}})
	if got := handedOverOf(saved); len(got) != 5 || slices.Contains(got, any("area one")) {
		t.Fatalf("the five listed areas are handed over, the sixth is not: %v", got)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"area one"}) {
		t.Fatalf("the area that was not listed stays open: %q", got)
	}
}

// Within one session (one receipt) the latest remaining is what is left: a later result with no remaining closes the earlier one, a
// result with another remaining closes it and opens its own, and the same remaining said again stays one open area, now from the
// newer result and with no closing line.
func TestWorkCoverageTheNextResultOfTheSameSessionReplacesItsRemaining(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	open := func() []string { return openAreasOf(t, f.prepare()) }
	f.record(prepared, withRemaining(coverageResult(), "alpha left"))
	if got := open(); !slices.Equal(got, []string{"alpha left"}) {
		t.Fatalf("the first result opens its remaining: %q", got)
	}
	saved := f.record(prepared, workCompletion{Changed: "Looked again.", Evidence: []string{"new1"}})
	if got := open(); len(got) != 0 || !slices.Equal(handedOverOf(saved), []any{"alpha left"}) {
		t.Fatalf("a later result with no remaining closes it: %q %v", got, saved["coverage"])
	}
	f.record(prepared, withRemaining(coverageResult(), "beta left"))
	saved = f.record(prepared, withRemaining(coverageResult(), "gamma left"))
	if got := open(); !slices.Equal(got, []string{"gamma left"}) || !slices.Equal(handedOverOf(saved), []any{"beta left"}) {
		t.Fatalf("another remaining closes the earlier one and opens its own: %q %v", got, saved["coverage"])
	}
	again := withRemaining(coverageResult(), "gamma left")
	again.Changed = "Audited the export once more."
	saved = f.record(prepared, again)
	row := listedRow(f.prepare(), "gamma left")
	if len(open()) != 1 || row == nil || row["source"] != f.state(prepared).Result.Source || handedOverOf(saved) != nil {
		t.Fatalf("the same remaining said again stays one open area from the newer result: %v %v", row, saved["coverage"])
	}
	if lines := f.ledger(); len(lines) != 6 || lines[5]["area"] != "gamma left" || lines[5]["outcome"] != "open" {
		t.Fatalf("the ledger has the six lines of the five results and no closing line for the area said again: %v", lines)
	}
}

// A result that closes nothing on its own is sent again after a later result of the session opened another area: it takes nothing
// over, because the ledger already has its lines (or it would close the later result's area).
func TestWorkCoverageAnOldResultSentAgainDoesNotCloseALaterResultsArea(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	first := withRemaining(coverageResult(), "alpha left")
	f.record(prepared, first)
	f.record(prepared, withRemaining(coverageResult(), "beta left"))
	before := len(f.ledger())
	saved := f.record(prepared, first) // the first result again, after the second opened its area and closed the first
	if saved["complete"] != true || len(f.ledger()) != before || !slices.Equal(openAreasOf(t, f.prepare()), []string{"beta left"}) {
		t.Fatalf("the first result sent again changes nothing: %v %v", saved["coverage"], f.ledger())
	}
}

// An old result that wrote no row to the ledger (it left nothing, or sent only rows for areas that were never open) is sent again
// after a later result of the session opened an area: it takes nothing over either. The ledger holds no line of it, so the work
// state is what remembers that its coverage was handled.
func TestWorkCoverageAnOldResultThatWroteNothingSentAgainDoesNotCloseALaterResultsArea(t *testing.T) {
	for _, c := range []struct {
		name  string
		first workCompletion
	}{
		{"no remaining and no coverage", coverageResult()},
		{"a clear row for an area that was never open", coverageResult(coverageRow("export totals", "clear", "new1"))},
		{"a remaining that says nothing was left", withRemaining(coverageResult(), "none")},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCoverageFlow(t)
			prepared := f.prepare()
			f.record(prepared, c.first)
			firstSource := f.state(prepared).Result.Source
			if len(f.ledger()) != 0 {
				t.Fatalf("the first result wrote no line: %v", f.ledger())
			}
			f.record(prepared, withRemaining(coverageResult(), "beta left"))
			if got, want := f.state(prepared).CoverageHandled, []string{f.state(prepared).Result.Source, firstSource}; !slices.Equal(got, want) {
				t.Fatalf("the work remembers both results whose coverage it handled, the newest first: %q, want %q", got, want)
			}
			before := len(f.ledger())
			saved := f.record(prepared, c.first)
			if open := openAreasOf(t, f.prepare()); saved["complete"] != true || len(f.ledger()) != before || !slices.Equal(open, []string{"beta left"}) || handedOverOf(saved) != nil {
				t.Fatalf("the first result sent again changed the ledger: %d -> %d lines, open %q, reply %v", before, len(f.ledger()), open, saved["coverage"])
			}
		})
	}
}

// A work state that never handled the coverage of a result has no field for it, so a state saved before the field existed and a
// state of a work that handled nothing read and write alike; once a result is recorded the state names its source.
func TestWorkStateCoverageHandledIsOmittedWhenEmpty(t *testing.T) {
	if data, err := json.Marshal(workState{}); err != nil || strings.Contains(string(data), "coverage_handled") {
		t.Fatalf("an empty work state has no coverage_handled: %s %v", data, err)
	}
	f := newCoverageFlow(t)
	prepared := f.prepare()
	if f.state(prepared).CoverageHandled != nil {
		t.Fatalf("a prepared work has handled nothing: %q", f.state(prepared).CoverageHandled)
	}
	f.record(prepared, workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}})
	if got := f.state(prepared).CoverageHandled; !slices.Equal(got, []string{f.state(prepared).Result.Source}) {
		t.Fatalf("a recorded result is remembered once saved: %q", got)
	}
}

// A work remembers the sources of at most 16 results, the newest first, and a source it remembers already is not added again.
func TestWorkCoverageHandledKeepsTheSixteenNewestSources(t *testing.T) {
	w := &workState{}
	sources := []string{}
	for i := range 20 {
		source := strings.Repeat(string(rune('a'+i)), 16)
		if !w.rememberCoverageHandled(source) || w.rememberCoverageHandled(source) {
			t.Fatalf("a new source is added once: %q", w.CoverageHandled)
		}
		sources = append([]string{source}, sources...)
	}
	if !slices.Equal(w.CoverageHandled, sources[:16]) {
		t.Fatalf("the work remembers the 16 newest sources: %q, want %q", w.CoverageHandled, sources[:16])
	}
	if w.rememberCoverageHandled(sources[15]) || w.rememberCoverageHandled(sources[0]) || !slices.Equal(w.CoverageHandled, sources[:16]) {
		t.Fatalf("a source that is remembered is not added again: %q", w.CoverageHandled)
	}
}

// recordWorkCoverage tells the record that the state holds an unsaved source once it remembers the source of a result, and only
// then: a source that is remembered already is not told again.
func TestWorkCoverageMarksTheStateUnsavedOnceTheSourceIsRemembered(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	f.record(prepared, withRemaining(coverageResult(), "alpha left"))
	w := f.state(prepared)
	source, session := w.Result.Source, f.session(prepared)
	w.CoverageHandled = nil
	marks, reply := 0, workResult{}
	add := func(item workItem) { t.Fatalf("nothing is refused: %v", item) }
	unsaved := func() { marks++ }
	recordWorkCoverage(f.b.Root, f.b, w, &workCompletion{Ref: source}, session, &reply, add, unsaved)
	if marks != 1 || !slices.Equal(w.CoverageHandled, []string{source}) {
		t.Fatalf("the source is remembered and the record is told once: %d, %q", marks, w.CoverageHandled)
	}
	recordWorkCoverage(f.b.Root, f.b, w, &workCompletion{Ref: source}, session, &reply, add, unsaved)
	if marks != 1 {
		t.Fatalf("a source that is remembered is not told again: %d", marks)
	}
}

// What the record has not saved when it ends is saved at its end (flush): the later saves of a record are not on every path. The
// repair by ref of an older result, while the receipt holds a later one, has no save after its coverage. The work still remembers the
// older result, so that the older result recorded in full afterwards (it becomes the result of the receipt again) takes nothing over:
// the area the later result left stays open, and the older remaining opens beside it.
func TestWorkCoverageHandledIsSavedWhenNoLaterSaveFollows(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	older := workCompletion{Changed: "Older audit.", Remaining: "alpha left", Evidence: []string{"new1"}}
	olderSource := f.waits(prepared, older)
	f.record(prepared, withRemaining(workCompletion{Changed: "Newer audit.", Evidence: []string{"new1"}}, "beta left"))
	newerSource := f.state(prepared).Result.Source
	f.repairs(prepared, olderSource)
	if got := f.state(prepared).CoverageHandled; !slices.Equal(got, []string{olderSource, newerSource}) {
		t.Fatalf("the saved state remembers the repaired older result and the later one: %q, want [%s %s]", got, olderSource, newerSource)
	}
	again := f.record(prepared, older)
	if open := openAreasOf(t, f.prepare()); again["complete"] != true || !slices.Equal(open, []string{"alpha left", "beta left"}) || handedOverOf(again) != nil {
		t.Fatalf("the older result recorded in full takes nothing over: open %q, reply %v, ledger %v", open, again["coverage"], f.ledger())
	}
}

// flush saves the state only when something of it is unsaved, and says so when that save fails; a record that was interrupted said
// so already. A save that succeeds clears what was unsaved.
func TestWorkRecordPersistenceFlushSavesOnlyWhatIsUnsaved(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	token, err := workDecode(prepared["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(f.b.Root)
	if err != nil {
		t.Fatal(err)
	}
	w, name, err := loadWorkState(dir, f.b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	p := &workRecordPersistence{dir: dir, name: name, scope: f.b.Scope, token: token, w: w, checkpoint: &workSessionCheckpoint{}}
	refused := []workItem{}
	add := func(item workItem) { refused = append(refused, item) }
	p.markUnsaved()
	if err := p.save(); err != nil || p.unsaved {
		t.Fatalf("a save that succeeds clears what was unsaved: %v %v", err, p.unsaved)
	}
	dir.Close() // from here on a save fails
	p.flush(add)
	if len(refused) != 0 {
		t.Fatalf("nothing is unsaved, so nothing is saved or reported: %v", refused)
	}
	p.markUnsaved()
	p.interrupted = errors.New("synthetic interruption")
	p.flush(add)
	if len(refused) != 0 {
		t.Fatalf("an interrupted record said so already: %v", refused)
	}
	p.interrupted = nil
	p.flush(add)
	if len(refused) != 1 || refused[0].Kind != "coverage" || refused[0].Ref != "coverage" || !strings.Contains(refused[0].Error, "the work state could not be saved") || !strings.Contains(refused[0].Error, "send the same record again") {
		t.Fatalf("a state that cannot be saved is an error of the record: %v", refused)
	}
}

// waits sends a result that waits for its evidence (new1 names no observation) and returns the id it was saved under.
func (f coverageFlow) waits(prepared map[string]any, c workCompletion) string {
	f.t.Helper()
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &c})
	if err != nil {
		f.t.Fatal(err)
	}
	if waiting := loopObject(f.t, value); waiting["complete"] == true || waiting["coverage"] != nil {
		f.t.Fatalf("the result waits for its evidence and says nothing of coverage: %v", waiting)
	}
	return f.state(prepared).Result.Source
}

// repairs sends the repair by ref of a result that waits, with an observation for its evidence.
func (f coverageFlow) repairs(prepared map[string]any, id string) map[string]any {
	f.t.Helper()
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Ref: id, Evidence: []string{"new1"}}, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its totals."}}})
	if err != nil {
		f.t.Fatal(err)
	}
	repaired := loopObject(f.t, value)
	if repaired["complete"] != true {
		f.t.Fatalf("the repair completes the result: %v", repaired)
	}
	return repaired
}

// A result that waited for its evidence is repaired by ref after a later result of the same receipt was recorded: what the later
// result left is what is left now, so the repaired older result writes its coverage rows and nothing else. Its remaining does not
// become the open area, it does not close the later result's area, and it takes nothing over.
func TestWorkCoverageTheRepairOfAnOlderResultDoesNotReplaceWhatALaterOneLeft(t *testing.T) {
	for _, c := range []struct {
		name  string
		older workCompletion
		want  []string // the open areas afterwards, newest first
		lines int
	}{
		{"remaining only", workCompletion{Changed: "Older audit.", Remaining: "alpha left", Evidence: []string{"new1"}}, []string{"beta left"}, 1},
		{"a clear row for an area that was never open and a remaining", workCompletion{Changed: "Older audit.", Remaining: "alpha left", Evidence: []string{"new1"}, Coverage: []workCoverageRow{coverageRow("export totals", "clear", "new1")}}, []string{"beta left"}, 1},
		{"an open row and a remaining: the row is written, the remaining is not", workCompletion{Changed: "Older audit.", Remaining: "alpha left", Evidence: []string{"new1"}, Coverage: []workCoverageRow{coverageRow("export filters", "open")}}, []string{"export filters", "beta left"}, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCoverageFlow(t)
			prepared := f.prepare()
			older := f.waits(prepared, c.older)
			newer := f.record(prepared, withRemaining(workCompletion{Changed: "Newer audit.", Evidence: []string{"new1"}}, "beta left"))
			if !slices.Equal(openAreasOf(t, f.prepare()), []string{"beta left"}) {
				t.Fatalf("the later result is complete and its remaining is open: %v", newer)
			}
			repaired := f.repairs(prepared, older)
			if open := openAreasOf(t, f.prepare()); !slices.Equal(open, c.want) || len(f.ledger()) != c.lines || handedOverOf(repaired) != nil {
				t.Fatalf("the repair of the older result changed what the later one left: open %q (want %q), %d lines (want %d), reply %v, ledger %v", open, c.want, len(f.ledger()), c.lines, repaired["coverage"], f.ledger())
			}
			for _, line := range f.ledger() {
				if line["area"] == "alpha left" || line["source"] == older && line["origin"] != nil {
					t.Fatalf("the remaining of the repaired older result is in the ledger: %v", line)
				}
			}
		})
	}
}

// A ledger that could not be written leaves the coverage of the result unhandled: the same record sent again writes its remaining as
// an open row and takes over the area its work was handed, once.
func TestWorkCoverageALedgerThatCouldNotBeWrittenIsHandledByTheSameRecordSentAgain(t *testing.T) {
	f := newCoverageFlow(t)
	const handed, left = "The import path was not read.", "The totals were not read."
	f.record(f.prepare(), withRemaining(coverageResult(), handed))
	second := f.prepare()
	folder, aside := filepath.Join(f.b.Root, "records", "coverage"), filepath.Join(f.b.Root, "records", "coverage-aside")
	if err := os.Rename(folder, aside); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(folder, []byte("a file where the folder of the ledger goes"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := withRemaining(workCompletion{Changed: "Read the import path.", Evidence: []string{"new1"}}, left)
	failed := f.record(second, result)
	errs, _ := failed["errors"].([]any)
	if failed["complete"] == true || len(errs) != 1 || errs[0].(map[string]any)["kind"] != "coverage" {
		t.Fatalf("the record names the ledger it could not write: %v", failed)
	}
	if got := f.state(second).CoverageHandled; len(got) != 0 {
		t.Fatalf("a result whose ledger could not be written is not remembered as handled: %q", got)
	}
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(aside, folder); err != nil {
		t.Fatal(err)
	}
	again := f.record(second, result)
	reply := coverageReplyOf(t, again)
	if again["complete"] != true || !slices.Equal(handedOverOf(again), []any{handed}) || !slices.Equal(reply["open"].([]any), []any{left}) {
		t.Fatalf("the same record sent again opens its remaining and takes the area over: %v", again)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{left}) {
		t.Fatalf("the handed area is closed and the remaining is open: %q (%v)", got, f.ledger())
	}
	if got := f.state(second).CoverageHandled; !slices.Equal(got, []string{f.state(second).Result.Source}) {
		t.Fatalf("the result is remembered once its ledger was written: %q", got)
	}
}

// The agent's own open row is not turned into a row that closes by itself: a later work that repeats the area in its remaining
// (the white space of it is not counted) leaves the row as it is, and a work that is handed it and records a result closes nothing.
func TestWorkCoverageARemainingThatRepeatsAnAgentsOpenAreaDoesNotTurnItIntoARemainingRow(t *testing.T) {
	for _, remaining := range []string{"export filters", " export   filters\n"} {
		t.Run(strings.TrimSpace(remaining), func(t *testing.T) {
			f := newCoverageFlow(t)
			f.record(f.prepare(), coverageResult(coverageRow("export filters", "open")))
			second := f.prepare()
			if row := listedRow(second, "export filters"); row == nil || row["from"] != nil {
				t.Fatalf("the agent's row is listed without from: %v", second["coverage"])
			}
			saved := f.record(second, withRemaining(workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}}, remaining))
			if open, _ := coverageReplyOf(t, saved)["open"].([]any); len(open) != 1 || open[0] != "export filters" || len(f.ledger()) != 1 {
				t.Fatalf("the reply names the area and the ledger has no second row for it: %v %v", saved["coverage"], f.ledger())
			}
			third := f.prepare()
			if row := listedRow(third, "export filters"); row == nil || row["from"] != nil || row["source"] != f.ledger()[0]["source"] {
				t.Fatalf("the preparation lists the row of the agent, with no from: %v", third["coverage"])
			}
			f.record(third, workCompletion{Changed: "Read something else.", Evidence: []string{"new1"}})
			if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"export filters"}) || len(f.ledger()) != 1 {
				t.Fatalf("the agent's row is still open and nothing was written: %q %v", got, f.ledger())
			}
		})
	}
}

// A row the agent wrote itself (no origin) is not closed by the work that is handed it, nor by the next result of its own session:
// only a later result that records the same area closes it. An open row made from remaining sits beside it, so that the ledger is
// asked to take rows over and the agent's row is the one it must leave.
func TestWorkCoverageAnAgentsOwnOpenRowIsNotClosedByHandover(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	f.record(prepared, coverageResult(coverageRow("export filters", "open")))
	f.record(f.prepare(), withRemaining(coverageResult(), "other left"))
	second := f.prepare()
	if row := listedRow(second, "export filters"); row == nil || row["from"] != nil || len(row) != 2 {
		t.Fatalf("a row the agent wrote has no from: %v", second["coverage"])
	}
	if listedRow(second, "other left") == nil {
		t.Fatalf("the row made from remaining is listed beside it: %v", second["coverage"])
	}
	own := f.record(prepared, workCompletion{Changed: "Read the export again.", Evidence: []string{"new1"}})
	if own["complete"] != true || own["coverage"] != nil || !slices.Equal(openAreasOf(t, f.prepare()), []string{"other left", "export filters"}) {
		t.Fatalf("the next result of the session that wrote the row leaves it open: %v", own["coverage"])
	}
	handed := f.record(second, workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}})
	if !slices.Equal(handedOverOf(handed), []any{"other left"}) || !slices.Equal(openAreasOf(t, f.prepare()), []string{"export filters"}) {
		t.Fatalf("the work that was handed both closes the row made from remaining only: %v", handed["coverage"])
	}
}

// When the work that was handed a remaining records the same area itself, the row of the result decides: a clear row with evidence
// closes it as clear, not as excluded, and the record does not call that a handover.
func TestWorkCoverageTheReceivingWorksClearRowClosesItAsClear(t *testing.T) {
	f := newCoverageFlow(t)
	f.record(f.prepare(), withRemaining(coverageResult(), "export filters"))
	second := f.prepare()
	saved := f.record(second, coverageResult(coverageRow("export  filters", "clear", "new1")))
	reply := coverageReplyOf(t, saved)
	lines := f.ledger()
	if reply["rows"] != float64(1) || len(reply["open"].([]any)) != 0 || reply["handed_over"] != nil || len(lines) != 2 || lines[1]["outcome"] != "clear" || lines[1]["source"] != f.state(second).Result.Source {
		t.Fatalf("the area is closed once, as clear: %v %v", saved["coverage"], lines)
	}
	if after := f.prepare(); after["coverage"] != nil {
		t.Fatalf("the area is closed: %v", after["coverage"])
	}
}

// A result that is repaired by ref is handled like the full record that made it ready: its remaining opens an area, and a repaired
// result of a work that was handed an area closes it.
func TestWorkCoverageARepairByRefOpensAndHandsOverLikeAFullRecord(t *testing.T) {
	f := newCoverageFlow(t)
	waits := func(prepared map[string]any, remaining string) string { // a result that waits for its evidence
		value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Changed: "Audited the export.", Remaining: remaining, Evidence: []string{"new1"}}})
		if err != nil {
			t.Fatal(err)
		}
		if waiting := loopObject(t, value); waiting["complete"] == true || waiting["coverage"] != nil {
			t.Fatalf("the result waits for its evidence and says nothing of coverage: %v", waiting)
		}
		return f.state(prepared).Result.Source
	}
	repairs := func(prepared map[string]any, id string) map[string]any {
		value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: &workCompletion{Ref: id, Evidence: []string{"new1"}}, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters."}}})
		if err != nil {
			t.Fatal(err)
		}
		repaired := loopObject(t, value)
		if repaired["complete"] != true {
			t.Fatalf("the repair completes the result: %v", repaired)
		}
		return repaired
	}
	first := f.prepare()
	id := waits(first, "The import path was not read.")
	if len(f.ledger()) != 0 {
		t.Fatalf("a result that is not ready writes nothing: %v", f.ledger())
	}
	repaired := repairs(first, id)
	if open, _ := coverageReplyOf(t, repaired)["open"].([]any); len(open) != 1 || open[0] != "The import path was not read." || len(f.ledger()) != 1 || f.ledger()[0]["source"] != id {
		t.Fatalf("the remaining of the repaired result opens an area: %v %v", repaired["coverage"], f.ledger())
	}
	second := f.prepare()
	id = waits(second, "")
	if len(f.ledger()) != 1 {
		t.Fatalf("a result that is not ready takes nothing over: %v", f.ledger())
	}
	repaired = repairs(second, id)
	if lines := f.ledger(); !slices.Equal(handedOverOf(repaired), []any{"The import path was not read."}) || len(lines) != 2 || lines[1]["source"] != id || lines[1]["outcome"] != "excluded" {
		t.Fatalf("the repaired result of the work that was handed the area closes it: %v %v", repaired["coverage"], lines)
	}
}

// The same record sent again adds no line, the closing line included, and a result sent again after the area it handed over was
// closed does not reopen it.
func TestWorkCoverageSendingTheSameRecordsAgainAddsNoLineAndKeepsTheHandover(t *testing.T) {
	f := newCoverageFlow(t)
	first := f.prepare()
	opening := withRemaining(coverageResult(), "The import path was not read.")
	f.record(first, opening)
	second := f.prepare()
	closing := workCompletion{Changed: "Read the import path.", Evidence: []string{"new1"}}
	f.record(second, closing)
	if len(f.ledger()) != 2 {
		t.Fatalf("the open row and the closing line: %v", f.ledger())
	}
	for range 2 {
		if again := f.record(second, closing); again["complete"] != true {
			t.Fatalf("the record sent again is complete: %v", again)
		}
		if again := f.record(first, opening); again["complete"] != true {
			t.Fatalf("the opening record sent again is complete: %v", again)
		}
	}
	if lines := f.ledger(); len(lines) != 2 || f.prepare()["coverage"] != nil {
		t.Fatalf("the records sent again added lines or reopened the area: %v", lines)
	}
}

// A result that has no coverage and no remaining, in a project whose ledger does not exist, leaves no ledger behind (not even its
// lock); and one whose ledger cannot be read is not failed for it: it has nothing to write.
func TestWorkCoverageAPlainResultNeitherCreatesNorNeedsTheLedger(t *testing.T) {
	f := newCoverageFlow(t)
	plain := workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}}
	if saved := f.record(f.prepare(), plain); saved["complete"] != true || saved["coverage"] != nil {
		t.Fatalf("the record is complete: %v", saved)
	}
	if _, err := os.Stat(filepath.Join(f.b.Root, "records", "coverage")); !os.IsNotExist(err) {
		t.Fatalf("a plain result created the ledger folder: %v", err)
	}
	records := filepath.Join(f.b.Root, "records")
	if err := os.MkdirAll(records, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(records, "coverage"), []byte("a file where the folder of the ledger goes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if saved := f.record(f.prepare(), plain); saved["complete"] != true || saved["errors"] != nil || saved["coverage"] != nil {
		t.Fatalf("a plain result is not failed by a ledger that cannot be read: %v", saved)
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

// A preparation whose bound is tight drops coverage after reasoning, reading and attention, then earlier_search, and before pending
// and observations, and names each in output.omitted. Each of the seven is large enough that the order, not the size, decides what
// goes first, and it goes only as far as the bound needs: a small earlier_search stays when dropping coverage was enough.
func TestLimitWorkPreparationDropsCoverageAndEarlierSearchInTheirPlace(t *testing.T) {
	pad := func(n int) string { return strings.Repeat("x", n) }
	for _, c := range []struct {
		name    string
		earlier int // the size of the note in earlier_search; 0 has no earlier_search at all
		omitted []string
	}{
		{"coverage alone", 0, []string{"reasoning", "reading", "attention", "coverage"}},
		{"a small earlier_search stays when coverage was enough", 200, []string{"reasoning", "reading", "attention", "coverage"}},
		{"a large earlier_search goes right after coverage", 1400, []string{"reasoning", "reading", "attention", "coverage", "earlier_search"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			reply := map[string]any{
				"receipt": "r", "how_to_record": pad(2000),
				"reasoning": map[string]any{"p": pad(30)}, "reading": map[string]any{"p": pad(30)}, "attention": map[string]any{"p": pad(30)},
				"coverage": map[string]any{"open": 3, "rows": []any{map[string]any{"area": pad(1500), "source": "s"}}},
				"pending":  []any{map[string]any{"p": pad(2500)}}, "observations": []any{map[string]any{"p": pad(2500)}},
			}
			if c.earlier > 0 {
				reply["earlier_search"] = map[string]any{"query": "q", "result": "0123456789abcdef", "next_action": pad(c.earlier)}
			}
			if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 8192); err != nil {
				t.Fatal(err)
			}
			output, _ := reply["output"].(map[string]any)
			omitted, _ := output["omitted"].([]string)
			if !slices.Equal(omitted, c.omitted) {
				t.Fatalf("omitted is %v, want %v: coverage goes after attention, then earlier_search, before pending and observations, and only as far as the bound needs", omitted, c.omitted)
			}
			if reply["pending"] == nil || reply["observations"] == nil || reply["coverage"] != nil || (reply["earlier_search"] != nil) != (c.earlier > 0 && !slices.Contains(omitted, "earlier_search")) {
				t.Fatalf("pending and observations stay, coverage goes, earlier_search goes only when named: %v", slices.Sorted(func(yield func(string) bool) {
					for k := range reply {
						if !yield(k) {
							return
						}
					}
				}))
			}
		})
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

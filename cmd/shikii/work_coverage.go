package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The coverage ledger of a result: what the work examined, and what it did not. A result carries up to workCoverageMax rows; the
// rows whose outcome is open are kept in the project's ledger (internal/store, coverage_ledger.go) and handed over by later preparations
// until a later result closes the same area. A row is the agent's report of what it looked at: it never says that the area is
// free of problems, and it is no permission.
//
// Agents write what they left in result.remaining and rarely send coverage rows. A result with no open coverage row (none at all, or
// only reported, clear and excluded rows) therefore leaves its remaining (unless it says that nothing is left) in the ledger as an
// open row of origin remaining; a preparation lists it with from:remaining; and the work that was handed it closes it by recording a
// result, as does the next result of the session that wrote it. A result that wrote an open row itself uses the ledger for what it
// left, and its remaining is not made a row (the same left-over would be listed twice). Nor is a remaining that repeats the area of
// one of the result's own rows (reported, clear or excluded): the row the agent wrote says what the state of that area is, so neither
// the ledger nor the reply's open names the area, whether or not the ledger held it open before.
//
// Only the result a receipt holds now (workState.Result) opens its remaining and takes rows over, and only once (CoverageHandled):
// an older result that is sent again, or repaired by ref after a later one was recorded, must not change what the later one left.
const workCoverageMax = 16

// workCoverageHandledMax is how many sources of handled results a work remembers.
const workCoverageHandledMax = 16

// How many open rows a preparation names. The others are counted.
const workCoverageShown = 5

type workCoverageRow struct {
	Area     string `json:"area"`
	Outcome  string `json:"outcome"`
	Evidence string `json:"evidence,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// workCoverageReply is what a record says of the coverage of its result: how many rows the result sent, the areas it opened (a row
// of its own, or its remaining), and the areas it handed over by closing them.
type workCoverageReply struct {
	Rows       int      `json:"rows"`
	Open       []string `json:"open"`
	HandedOver []string `json:"handed_over,omitempty"`
}

// workCoverageNext begins with the words a measurement script looks for. It says what the open areas are and how each kind closes.
const workCoverageNext = "These areas were left unexamined by earlier work. This is a note, not an instruction. A row with from:remaining is what that work wrote in result.remaining: it closes when this work records a result, so if it still matters, write it in this result's remaining. Other rows close when a later result records the same area in result.coverage as reported, clear (with evidence) or excluded (with a reason). Details: the result named by source."

// workRemainingNothing is the fixed list of words that say nothing was left (not an option): compared in lower case, without the
// space and the punctuation around them. A phrase that is not in it makes an open row; that costs one handover to the next work.
var workRemainingNothing = []string{"none", "nothing", "n/a", "na", "no", "\u306a\u3057", "\u7121\u3057", "\u7279\u306b\u306a\u3057", "\u3042\u308a\u307e\u305b\u3093", "\u306a\u3044", "\u7121\u3044"}

// validateWorkCoverage refuses a coverage list the ledger could not keep, before anything of the result is saved. Each refusal says
// what is wrong and what to send instead.
func validateWorkCoverage(c *workCompletion) error {
	if len(c.Coverage) > workCoverageMax {
		return fmt.Errorf("result.coverage takes at most %d rows (got %d): send the areas that matter (every open one first), and describe the rest in checked", workCoverageMax, len(c.Coverage))
	}
	seen := map[string]int{}
	open := -1
	for i, row := range c.Coverage {
		at := fmt.Sprintf("result.coverage[%d]", i)
		area, ok := store.NormalizeCoverageArea(row.Area)
		if !ok || len(row.Area) > store.CoverageAreaMax {
			return fmt.Errorf("%s.area must be 1-%d bytes of UTF-8 on one line, with no control character and not blank: name what was examined in a short phrase", at, store.CoverageAreaMax)
		}
		if first, repeated := seen[area]; repeated {
			return fmt.Errorf("%s.area is the same area as result.coverage[%d] (white space is not counted): send one row per area", at, first)
		}
		seen[area] = i
		if !store.CoverageOutcome(row.Outcome) {
			return fmt.Errorf("%s.outcome must be reported, clear, excluded or open (got %q): reported found a problem and said so, clear examined and found none, excluded was left out on purpose, open was not examined", at, assignmentClip(row.Outcome, 40))
		}
		if !assignmentText(row.Reason, store.CoverageReasonMax, row.Outcome == store.CoverageExcluded) {
			return fmt.Errorf("%s.reason must be 1-%d bytes of UTF-8 for an excluded row, and at most %d bytes for any other: say why the area was left out", at, store.CoverageReasonMax, store.CoverageReasonMax)
		}
		if row.Evidence != "" && !slices.Contains(c.Evidence, row.Evidence) {
			return fmt.Errorf("%s.evidence %q is not one of result.evidence (%s): cite one of those refs, or leave it out", at, assignmentClip(row.Evidence, 80), assignmentClip(strings.Join(c.Evidence, ", "), 200))
		}
		if row.Outcome == store.CoverageClear {
			switch {
			case row.Evidence == "":
				return fmt.Errorf("%s is clear without evidence: name one of result.evidence (what you checked), or send open or excluded instead", at)
			case row.Evidence == "current":
				return fmt.Errorf("%s.evidence is current, the request's own text, which checks nothing: cite an observation of this call or a page you read from result.evidence, or send open instead", at)
			}
		}
		if row.Outcome == store.CoverageOpen && open < 0 {
			open = i
		}
	}
	if open >= 0 && c.Sufficient != nil && *c.Sufficient {
		return fmt.Errorf("result.sufficient is true while result.coverage[%d] is open: an area left unexamined is not a sufficient check; send sufficient:false or leave it out, or close the row", open)
	}
	return nil
}

// workCoverageOpen reports whether a result leaves an area open.
func workCoverageOpen(rows []workCoverageRow) bool {
	for _, row := range rows {
		if row.Outcome == store.CoverageOpen {
			return true
		}
	}
	return false
}

// workCoverageNames reports whether one of the rows is about the area: the areas are compared as normalized areas (area is already
// one: it comes from workRemainingArea).
func workCoverageNames(rows []workCoverageRow, area string) bool {
	return slices.ContainsFunc(rows, func(row workCoverageRow) bool {
		named, _ := store.NormalizeCoverageArea(row.Area)
		return named == area
	})
}

// readyCoverageResult names the saved result that this record made ready, and the signature it was saved under. A full record names
// the result of the receipt; a repair by ref names the result it repaired, which a repair of an earlier request may complete
// while the receipt holds another result.
func readyCoverageResult(w *workState, c *workCompletion) (source, signature string, ok bool) {
	if c.Ref == "" {
		if w.Result != nil && w.Result.Ready && w.Result.Signature == workHash(*c) {
			return w.Result.Source, w.Result.Signature, true
		}
		return "", "", false
	}
	if w.Result != nil && w.Result.Ready && w.Result.Source == c.Ref {
		return c.Ref, w.Result.Signature, true
	}
	if w.Reasoning != nil {
		if p := w.Reasoning.PendingResults[c.Ref]; p != nil && p.Complete && p.Result != nil && p.Result.Ready && p.Result.Source == c.Ref {
			return c.Ref, p.Result.Signature, true
		}
	}
	return "", "", false
}

// savedCompletion reads the saved source of a result, which is what the ledger is written from, and checks that the source is the
// result it was saved as.
func savedCompletion(book notebook.Book, source, signature string) (workCompletion, error) {
	var saved workCompletion
	material, err := book.ReadSources([]string{source})
	if err != nil {
		return saved, err
	}
	originals, _ := material["observations"].([]notebook.Observation)
	if len(originals) != 1 || !isWorkSource(originals[0], notebook.WorkResult()) || json.Unmarshal([]byte(originals[0].Content), &saved) != nil || workHash(saved) != signature {
		return saved, errors.New("the saved result is unavailable or changed")
	}
	return saved, nil
}

// workRemainingArea is the area an open row takes from the text of remaining: the text with each run of white space (a control
// character is white space) as one space, and cut to the longest area the ledger keeps (the cut is never inside a character and
// ends in an ellipsis). It reports false when the text says that nothing was left: it is empty or only punctuation, or it is one of
// workRemainingNothing.
func workRemainingArea(remaining string) (string, bool) {
	area := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, remaining)), " ")
	bare := strings.ToLower(strings.TrimFunc(area, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) }))
	if bare == "" || slices.Contains(workRemainingNothing, bare) {
		return "", false
	}
	if len(area) > store.CoverageAreaMax {
		const ellipsis = "\u2026"
		cut := store.CoverageAreaMax - len(ellipsis)
		for !utf8.RuneStart(area[cut]) {
			cut--
		}
		area = area[:cut] + ellipsis
	}
	return area, true
}

// recordWorkCoverage writes what the project's ledger keeps of a result that is ready, and tells the reply what the result opened
// and handed over. It runs for a full record and for the repair of a result by ref, from the saved result: the coverage rows are
// written; and a result that has no open coverage row writes its remaining as an open row of origin remaining (workRemainingArea),
// unless one of its own rows names the same area (workCoverageNames): that row says what the state of the area is, so the remaining
// is not made a row, and the reply's open does not name the area, whatever the ledger held before.
// The result the receipt holds now also closes the open rows of remaining that it takes over (store.CoverageHandover): the rows of
// its own session and the rows its preparation listed. Both are done for that result only, and once: a result that was repaired by
// ref while the receipt holds another one writes its coverage rows and nothing else. The work remembers the results it handled
// (CoverageHandled, newest first): the coverage rows of a result whose source it remembers are not given to the ledger again when it
// is sent again, and neither is a handover. The ledger's own check of a record sent again sees only the lines the store reads, the
// newest 4MB; once the lines of the result are further back than that, only this memory keeps an open row from coming back for an area
// a later result closed. The row made from the remaining of a result is not covered by it: a result that was repaired by ref while
// the receipt held another one is remembered although its remaining was not written, and written when it is recorded in full later,
// so the store's own check is what keeps that row from being written twice (inside its window only). The source is saved with the
// work state by the next save of the record, or at its end when none follows (unsaved), and is remembered only after the ledger was
// written without error. A record sent again writes nothing again. A ledger that cannot be written is an error of the record (kind coverage): the same record sent again
// writes it, and the source is not remembered. A result with nothing to write, whose ledger cannot be read, is not failed for it.
func recordWorkCoverage(root string, book notebook.Book, w *workState, c *workCompletion, session string, result *workResult, add func(workItem), unsaved func()) {
	if c == nil {
		return
	}
	source, signature, ready := readyCoverageResult(w, c)
	if !ready {
		return
	}
	fail := func(err error) {
		add(workItem{Kind: "coverage", Ref: "coverage", Error: "the coverage of the result could not be written to the project's ledger: " + err.Error() + "; send the same record again"})
	}
	var saved workCompletion
	if c.Ref != "" || len(c.Coverage) > 0 || strings.TrimSpace(c.Remaining) != "" {
		var err error
		if saved, err = savedCompletion(book, source, signature); err != nil {
			fail(err)
			return
		}
	}
	current := w.Result != nil && w.Result.Source == source
	rows := saved.Coverage
	handled := slices.Contains(w.CoverageHandled, source)
	var ledger []store.CoverageRow
	open := []string{}
	for _, row := range rows {
		if !handled { // the coverage rows of a result the work handled were written then, and are not sent to the ledger again
			ledger = append(ledger, store.CoverageRow{Area: row.Area, Outcome: row.Outcome, Reason: row.Reason})
		}
		if row.Outcome == store.CoverageOpen {
			area, _ := store.NormalizeCoverageArea(row.Area)
			open = append(open, area)
		}
	}
	var handover store.CoverageHandover
	takesOver := current && !handled
	if takesOver {
		handover = store.CoverageHandover{Session: store.CoverageSession(session), Shown: w.CoverageShown}
	}
	if area, left := workRemainingArea(saved.Remaining); current && left && !workCoverageOpen(rows) && !workCoverageNames(rows, area) {
		ledger = append(ledger, store.CoverageRow{Area: area, Outcome: store.CoverageOpen, Origin: store.CoverageFromRemaining, Session: store.CoverageSession(session)})
		open = append(open, area)
	}
	st := store.Store{Dir: root}
	var handed []string
	// a result with no row to write needs the ledger only to take rows of remaining over
	if len(ledger) > 0 || takesOver && ledgerHoldsRemaining(st, book.Scope) {
		var err error
		if handed, err = st.RecordCoverage(book.Scope, source, ledger, handover); err != nil {
			fail(err)
			return
		}
	}
	if len(rows) > 0 || len(open) > 0 || len(handed) > 0 {
		result.Coverage = &workCoverageReply{Rows: len(rows), Open: open, HandedOver: handed}
	}
	if w.rememberCoverageHandled(source) {
		unsaved() // saved by the next save of the record, or by its end (workRecordPersistence.flush)
	}
}

// rememberCoverageHandled adds the source of a result whose coverage was handled (newest first, at most workCoverageHandledMax of
// them). It reports false when the work remembers the source already.
func (w *workState) rememberCoverageHandled(source string) bool {
	if slices.Contains(w.CoverageHandled, source) {
		return false
	}
	w.CoverageHandled = append([]string{source}, w.CoverageHandled...)
	w.CoverageHandled = w.CoverageHandled[:min(len(w.CoverageHandled), workCoverageHandledMax)]
	return true
}

// ledgerHoldsRemaining reports whether the project's ledger holds an open row made from remaining, which a result could take over.
// A ledger that cannot be read holds none for this purpose: a result with nothing to write is not failed for it.
func ledgerHoldsRemaining(st store.Store, scope string) bool {
	held, err := st.OpenCoverage(scope)
	return err == nil && slices.ContainsFunc(held, func(r store.CoverageRow) bool { return r.Origin == store.CoverageFromRemaining })
}

// preparedCoverage is the open rows of the project's ledger for a preparation, or nil when none is open: the newest workCoverageShown
// of them are named and the others are counted. A row from remaining says so. A ledger that cannot be read says so in place of the
// rows: a preparation is not refused for it. The bound of the reply may name fewer rows than this (limitWorkPreparation), which is
// why the work remembers what the reply names (coverageAreasNamed), not what this lists.
func preparedCoverage(root, scope string) map[string]any {
	open, err := (store.Store{Dir: root}).OpenCoverage(scope)
	if err != nil {
		return map[string]any{"error": "the coverage ledger could not be read: " + err.Error()}
	}
	if len(open) == 0 {
		return nil
	}
	rows := []any{}
	for _, row := range open[:min(workCoverageShown, len(open))] {
		entry := map[string]any{"area": row.Area, "source": row.Source}
		if row.Origin == store.CoverageFromRemaining {
			entry["from"] = store.CoverageFromRemaining
		}
		rows = append(rows, entry)
	}
	return map[string]any{"open": len(open), "rows": rows, "next_action": workCoverageNext}
}

// coverageAreasNamed is the areas of the rows a reply's coverage names after the bound has been applied. The work remembers them
// (workState.CoverageShown): it is handed those rows, and only those, so a row the bound left out stays open for a later
// preparation. It is nil when the reply has no coverage, or names no row (a ledger that could not be read).
func coverageAreasNamed(coverage any) []string {
	named, _ := coverage.(map[string]any)
	rows, _ := named["rows"].([]any)
	var areas []string
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok {
			if area, ok := row["area"].(string); ok {
				areas = append(areas, area)
			}
		}
	}
	return areas
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The coverage ledger of a result: what the work examined, and what it did not. A result carries up to workCoverageMax rows; the
// rows whose outcome is open are kept in the project's ledger (internal/store, coverage_ledger.go) and handed over by later preparations
// until a later result closes the same area. A row is the agent's report of what it looked at: it never says that the area is
// free of problems, and it is no permission.
const workCoverageMax = 16

// How many open rows a preparation names. The others are counted.
const workCoverageShown = 5

type workCoverageRow struct {
	Area     string `json:"area"`
	Outcome  string `json:"outcome"`
	Evidence string `json:"evidence,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// workCoverageReply is what a record says of the coverage rows of its result. A result that names what it left (remaining) but has no
// coverage rows gets next_action alone: the project's ledger takes only coverage rows, and remaining is never shown to later work.
type workCoverageReply struct {
	Rows       int      `json:"rows"`
	Open       []string `json:"open"`
	NextAction string   `json:"next_action,omitempty"`
}

const workCoverageNext = "These areas were left unexamined by earlier work. This is a note on what is still open, not an instruction. If this work examines one, record the same area in result.coverage as reported, as clear with evidence (one of result.evidence), or as excluded with a reason; that closes it. Leave unrelated rows alone. The saved result named by source holds the reason an area was left open."

// workCoverageRemainingNext is told to a result that wrote what was left in remaining and sent no coverage row. Agents write the
// left-over there, and no later preparation reads it; this is the moment the way to a row can be taught.
const workCoverageRemainingNext = "result.remaining is kept with this result but is not shown to later work. If an area was left unexamined or needs follow-up, send this result again with coverage:[{\"area\":\"<short name of what was not examined>\",\"outcome\":\"open\"}] added; later preparations of this project then list the area until a result closes it. Not needed when nothing was left unexamined."

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

// savedCoverage reads the coverage rows from the saved source of a result, which is what the ledger is written from, and checks that
// the source is the result it was saved as.
func savedCoverage(book notebook.Book, source, signature string) ([]workCoverageRow, error) {
	material, err := book.ReadSources([]string{source})
	if err != nil {
		return nil, err
	}
	originals, _ := material["observations"].([]notebook.Observation)
	var saved workCompletion
	if len(originals) != 1 || !isWorkSource(originals[0], notebook.WorkResult()) || json.Unmarshal([]byte(originals[0].Content), &saved) != nil || workHash(saved) != signature {
		return nil, errors.New("the saved result is unavailable or changed")
	}
	return saved.Coverage, nil
}

// recordWorkCoverage writes the coverage rows of a result that is ready into the project's ledger, and tells the reply what the
// result holds. It runs for a full record and for the repair of a result by ref, and writes nothing again for a record sent again. A
// ledger that cannot be written is an error of the record (kind coverage): the same record sent again writes it. A full result that
// is ready, has a remaining that is not blank and no coverage row writes nothing and gets the reply that says how a row is sent.
func recordWorkCoverage(root string, book notebook.Book, w *workState, c *workCompletion, result *workResult, add func(workItem)) {
	if c == nil || c.Ref == "" && len(c.Coverage) == 0 && strings.TrimSpace(c.Remaining) == "" {
		return
	}
	source, signature, ready := readyCoverageResult(w, c)
	if !ready {
		return
	}
	if c.Ref == "" && len(c.Coverage) == 0 {
		result.Coverage = &workCoverageReply{Open: []string{}, NextAction: workCoverageRemainingNext}
		return
	}
	rows, err := savedCoverage(book, source, signature)
	if err == nil && len(rows) > 0 {
		ledger := make([]store.CoverageRow, len(rows))
		open := []string{}
		for i, row := range rows {
			ledger[i] = store.CoverageRow{Area: row.Area, Outcome: row.Outcome, Reason: row.Reason}
			if row.Outcome == store.CoverageOpen {
				area, _ := store.NormalizeCoverageArea(row.Area)
				open = append(open, area)
			}
		}
		if err = (store.Store{Dir: root}).RecordCoverage(book.Scope, source, ledger); err == nil {
			result.Coverage = &workCoverageReply{Rows: len(rows), Open: open}
		}
	}
	if err != nil {
		add(workItem{Kind: "coverage", Ref: "coverage", Error: "the coverage of the result could not be written to the project's ledger: " + err.Error() + "; send the same record again"})
	}
}

// preparedCoverage is the open rows of the project's ledger for a preparation, or nil when none is open. A ledger that cannot be
// read says so in place of the rows: a preparation is not refused for it.
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
		rows = append(rows, map[string]any{"area": row.Area, "source": row.Source})
	}
	return map[string]any{"open": len(open), "rows": rows, "next_action": workCoverageNext}
}

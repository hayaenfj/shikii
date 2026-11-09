package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PairKey is the key of PairEffects: the quote's key and the door it came through.
func PairKey(quoteKey, phrase string) string { return quoteKey + "\t" + phrase }

// PairEffects tallies the assessments of lesson deliveries per (quote key, door), read from the assessment files themselves (each
// carries the door it was saved with), so the count does not depend on the window of recent deliveries (audit A of 0.12: 260 later
// deliveries lifted a silence). Only lesson quotes count; assessments without a door (always rules, records from before 0.12) and
// "unknown" ones teach nothing; "harmful" counts as unused. A file that cannot be read is skipped: a broken assessment never silences or
// unsilences anything by itself.
func (s Store) PairEffects(scope string) map[string]PairEffect {
	out := map[string]PairEffect{}
	dir := s.recordPath("memory-reviews", "", scope)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(data) > 32<<10 {
			continue
		}
		var a MemoryAssessment
		if json.Unmarshal(data, &a) != nil || a.Phrase == "" || !strings.HasPrefix(a.Key, "lesson:") {
			continue
		}
		k := PairKey(a.Key, a.Phrase)
		pe := out[k]
		switch a.Result {
		case "helpful":
			pe.Helpful++
		case "unused", "harmful":
			pe.Unused++
		default:
			continue
		}
		out[k] = pe
	}
	return out
}

// PersonAnswer is the person's own latest answer to one decision of a question, with the decision's request text.
type PersonAnswer struct {
	TraceID, SpecID, Request, Truth string
	At                              time.Time
}

// PersonAnswers returns, for the questions specIDs, the person's own answers (the page, a spot check, a hand label: personChecks), the
// latest per decision; a truth a result or the machine wrote later does not hide it (0.11). Truths from results, the agent (mcp), re-asking
// (replay), the judge or the added context are never returned: they are not what the person said. A decision from a session that read
// production data (tainted) is left out, since the answer goes into the agent's context, which may be sent out. Request is the decision's
// original text when it was kept (SaveRecordText), else the request input.
func (s Store) PersonAnswers(specIDs []string) ([]PersonAnswer, error) {
	out := []PersonAnswer{}
	for _, id := range specIDs {
		latest := map[string]Label{}
		hashes := map[string][]string{}
		tainted := map[string]bool{}
		order := []string{}
		err := s.scanJSONL(s.labelsPath(id), func(line []byte) error {
			var l Label
			if json.Unmarshal(line, &l) != nil || l.TraceID == "" {
				return nil
			}
			tainted[l.TraceID] = tainted[l.TraceID] || l.Tainted
			if !personChecks[l.Source] || Partial(l.Truth) || strings.TrimSpace(l.Inputs["request"]) == "" {
				return nil
			}
			cur, ok := latest[l.TraceID]
			if !ok {
				order = append(order, l.TraceID)
			}
			if !ok || !l.At.Before(cur.At) {
				latest[l.TraceID] = l
			}
			hashes[l.TraceID] = append(hashes[l.TraceID], l.InputsHash)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, trace := range order {
			l := latest[trace]
			if tainted[trace] {
				continue
			}
			request := l.Inputs["request"]
			for _, hash := range hashes[trace] { // a decision that added context has a label per step; the kept text is under the record's hash
				originals, _ := s.RecordText(Record{TraceID: trace, SpecID: l.SpecID, InputsHash: hash, Inputs: l.Inputs})
				if text := originals["request"]; text != "" {
					request = text
					break
				}
			}
			out = append(out, PersonAnswer{TraceID: trace, SpecID: l.SpecID, Request: strings.TrimSpace(request), Truth: l.Truth, At: l.At})
		}
	}
	return out, nil
}

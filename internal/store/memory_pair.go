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
	s.eachAssessment(scope, func(a MemoryAssessment) {
		if a.Phrase == "" || !strings.HasPrefix(a.Key, "lesson:") {
			return
		}
		k := PairKey(a.Key, a.Phrase)
		pe := out[k]
		if pe.add(a.Result) {
			out[k] = pe
		}
	})
	return out
}

// add counts one assessment: helpful, or unused (harmful counts as unused). Unknown and anything else is not counted.
func (e *PairEffect) add(result string) bool {
	switch result {
	case "helpful":
		e.Helpful++
	case "unused", "harmful":
		e.Unused++
	default:
		return false
	}
	return true
}

// eachAssessment calls fn for every assessment of this project that can be read. It never fails: a file that cannot be read or
// is too large is skipped, so a broken assessment never changes what is delivered.
func (s Store) eachAssessment(scope string, fn func(MemoryAssessment)) {
	dir := s.recordPath("memory-reviews", "", scope)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
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
		if json.Unmarshal(data, &a) != nil {
			continue
		}
		fn(a)
	}
}

// AnswerEffects are what the reflection said about the person's past answers quoted before requests (quote keys
// decision:<trace id>): per answered decision, the time of its last counted assessment, and all of them together. Counted from the
// assessments alone, like PairEffects.
type AnswerEffects struct {
	ByTrace map[string]PairEffect
	LastAt  map[string]time.Time
	Total   PairEffect
}

// AnswerEffects reads the assessments of quoted past answers of this project.
func (s Store) AnswerEffects(scope string) AnswerEffects {
	out := AnswerEffects{ByTrace: map[string]PairEffect{}, LastAt: map[string]time.Time{}}
	s.eachAssessment(scope, func(a MemoryAssessment) {
		trace, ok := strings.CutPrefix(a.Key, "decision:")
		if !ok || trace == "" {
			return
		}
		pe := out.ByTrace[trace]
		if !pe.add(a.Result) {
			return
		}
		out.ByTrace[trace] = pe
		out.Total.add(a.Result)
		if a.At.After(out.LastAt[trace]) {
			out.LastAt[trace] = a.At
		}
	})
	return out
}

// PersonAnswer is the person's own latest answer to one decision of a question, with the decision's request text. Hashes are the
// hashes of the inputs the answer was given about (one per step of a decision that added context): two decisions of one question
// that share one were asked about the same inputs (a decision asked again copies the person's answer to a new record).
type PersonAnswer struct {
	TraceID, SpecID, Request, Truth string
	Hashes                          []string
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
			out = append(out, PersonAnswer{TraceID: trace, SpecID: l.SpecID, Request: strings.TrimSpace(request), Truth: l.Truth, Hashes: hashes[trace], At: l.At})
		}
	}
	return out, nil
}

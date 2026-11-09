package store

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"os"
)

// HumanPerThousand is person decisions per 1000 decisions. 0 when there are no records.
func (f Funnel) HumanPerThousand() float64 {
	if f.Records == 0 {
		return 0
	}
	return math.Round(1000 * float64(f.Human) / float64(f.Records))
}

// Funnel counts the spec's records by step and band. Unreadable lines are not counted.
// discardUnsure is the same rule as decide's "unsure": for types whose band comes from the highest option (choice, score without level), discard also goes to a person.
func (s Store) Funnel(specID string, discardUnsure bool) (Funnel, error) {
	var f Funnel
	err := s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) != nil {
			return nil
		}
		f.Records++
		switch {
		case r.Band == "ask" || (discardUnsure && r.Band == "discard"):
			f.Human++
		case r.RungOf() == "context":
			f.Context++
		default:
			f.Minimal++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return f, err
	}
	return f, nil
}

// AuditSample decides "show this decision to a person as a spot check". Decisions that acted automatically are not seen by people, so act precision cannot be measured with people's truths.
// So 1 in 20 is picked by trace_id (the same every time) and put in the answer-check queue. Empty traces are never picked.
func AuditSample(traceID string) bool {
	if traceID == "" {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte(traceID))
	return h.Sum32()%20 == 0
}

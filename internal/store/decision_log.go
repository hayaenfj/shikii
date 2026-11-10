package store

import (
	"context"
	"encoding/json"
	"os"
	"sort"
)

// DecisionLog is one request's decoded records and labels. RawRecords retains
// Counts' nonempty-line count, including malformed records. Nothing is cached
// across requests or shared with a writer.
type DecisionLog struct {
	RawRecords int
	Records    []Record
	Labels     []Label
	Labeled    map[string]Label
}

func (s Store) ReadDecisionLog(ctx context.Context, specID string) (DecisionLog, error) {
	v := DecisionLog{Labeled: map[string]Label{}}
	scan := func(path string, fn func([]byte)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := s.scanJSONL(path, func(line []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			fn(line)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return ctx.Err()
	}
	if err := scan(s.recordsPath(specID), func(line []byte) {
		v.RawRecords++
		var r Record
		if json.Unmarshal(line, &r) == nil {
			v.Records = append(v.Records, r)
		}
	}); err != nil {
		return DecisionLog{}, err
	}
	if err := scan(s.labelsPath(specID), func(line []byte) {
		var l Label
		if json.Unmarshal(line, &l) != nil {
			return
		}
		v.Labels = append(v.Labels, l)
		if l.TraceID == "" {
			return
		}
		if cur, ok := v.Labeled[l.TraceID]; !ok || (HumanSource(l.Source) && !HumanSource(cur.Source)) || (HumanSource(l.Source) == HumanSource(cur.Source) && l.At.After(cur.At)) {
			v.Labeled[l.TraceID] = l
		}
	}); err != nil {
		return DecisionLog{}, err
	}
	return v, nil
}

// NewestFirst is needed only for displayed lists; status aggregates in append
// order and avoids sorting the entire history.
func (v *DecisionLog) NewestFirst() {
	sort.SliceStable(v.Records, func(i, j int) bool { return v.Records[i].At.After(v.Records[j].At) })
}

func (v DecisionLog) Funnel(discardUnsure bool) Funnel {
	var f Funnel
	for _, r := range v.Records {
		f.addRecord(r, discardUnsure)
	}
	return f
}

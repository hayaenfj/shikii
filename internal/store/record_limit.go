package store

import (
	"container/heap"
	"encoding/json"
	"os"
	"sort"
)

type orderedRecord struct {
	value Record
	order int
}
type newestRecordHeap []orderedRecord

func (h newestRecordHeap) Len() int { return len(h) }

// The least wanted record is first: older time, then later input order.
func (h newestRecordHeap) Less(i, j int) bool {
	if h[i].value.At.Equal(h[j].value.At) {
		return h[i].order > h[j].order
	}
	return h[i].value.At.Before(h[j].value.At)
}
func (h newestRecordHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *newestRecordHeap) Push(v any)   { *h = append(*h, v.(orderedRecord)) }
func (h *newestRecordHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	old[n-1] = orderedRecord{}
	*h = old[:n-1]
	return v
}

func (s Store) newestRecords(specID string, limit int) ([]Record, error) {
	// Do not allocate from an untrusted requested limit before reading any rows.
	var h newestRecordHeap
	order := 0
	err := s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) != nil {
			return nil
		}
		v := orderedRecord{r, order}
		order++
		if len(h) < limit {
			heap.Push(&h, v)
		} else if r.At.After(h[0].value.At) {
			h[0] = v
			heap.Fix(&h, 0)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(h, func(i, j int) bool {
		if h[i].value.At.Equal(h[j].value.At) {
			return h[i].order < h[j].order
		}
		return h[i].value.At.After(h[j].value.At)
	})
	var out []Record
	if len(h) > 0 {
		out = make([]Record, len(h))
		for i := range h {
			out[i] = h[i].value
		}
	}
	return out, nil
}

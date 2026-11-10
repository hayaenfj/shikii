package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

func (s Store) appendJSONL(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxLineBytes {
		return fmt.Errorf("record too large: %d bytes > %d (inputs must stay under the line limit)", len(b), MaxLineBytes)
	}
	return s.appendJSONLBytes(path, append(b, '\n'))
}

// jsonlScans counts the scans of a records or labels file in this process, whether or not the file exists.
var jsonlScans atomic.Uint64

// JSONLScans returns how many times this process scanned a records or labels file (tests hold checks that run back to back to one
// scan per file).
func JSONLScans() uint64 { return jsonlScans.Load() }

func (s Store) scanJSONL(path string, fn func(line []byte) error) error {
	jsonlScans.Add(1)
	f, err := s.openFile(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 2*MaxLineBytes) // larger than the writer's limit (so old records and hand-edited lines can still be read)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := fn([]byte(sc.Text())); err != nil {
			return err
		}
	}
	return sc.Err()
}

// AppendLabelsOnce appends labels that are not already there with the same trace, spec hash, model, truth and source, under a lock on
// the spec's labels (0.9.3). Outcome truths are decided by hooks that can race (the original session's hook gives up its lock after
// 2 seconds) or retry after a failed save; this makes each one land once. A different truth for the same trace is still written.
func (s Store) AppendLabelsOnce(ls []Label) error {
	if len(ls) == 0 {
		return nil
	}
	path := s.labelsPath(ls[0].SpecID)
	return s.withJSONLAppendLock(path, func() error {
		key := func(l Label) string {
			return l.TraceID + "\x00" + l.SpecHash + "\x00" + l.Model + "\x00" + l.Truth + "\x00" + l.Source
		}
		have := map[string]bool{}
		if err := s.scanJSONL(path, func(line []byte) error {
			var l Label
			if json.Unmarshal(line, &l) == nil {
				have[key(l)] = true
			}
			return nil
		}); err != nil && !os.IsNotExist(err) {
			return err
		}
		var fresh []Label
		for _, l := range ls {
			if !have[key(l)] {
				have[key(l)] = true
				fresh = append(fresh, l)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		buf, err := marshalLabels(fresh)
		if err != nil {
			return err
		}
		return s.appendJSONLBytesLocked(path, buf)
	})
}

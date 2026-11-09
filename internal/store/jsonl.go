package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
)

func (s Store) appendJSONL(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxLineBytes {
		return fmt.Errorf("record too large: %d bytes > %d (inputs must stay under the line limit)", len(b), MaxLineBytes)
	}
	f, err := s.openFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s Store) scanJSONL(path string, fn func(line []byte) error) error {
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
	lock, err := s.openFile(s.labelsPath(ls[0].SpecID)+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	key := func(l Label) string {
		return l.TraceID + "\x00" + l.SpecHash + "\x00" + l.Model + "\x00" + l.Truth + "\x00" + l.Source
	}
	have := map[string]bool{}
	if err := s.scanJSONL(s.labelsPath(ls[0].SpecID), func(line []byte) error {
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
	return s.AppendLabels(fresh)
}

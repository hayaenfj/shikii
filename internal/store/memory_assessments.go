package store

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// MemoryAssessments reads saved evidence independently of the bounded delivery log.
// Broken records are skipped; links and unreadable records are reported, as in MemoryReport.
func (s Store) MemoryAssessments(scope string) ([]MemoryAssessment, error) {
	out := []MemoryAssessment{}
	dir := s.recordPath("memory-reviews", "", scope)
	root, _, err := s.parentRoot(filepath.Join(dir, "entry"), false)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	folder, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := folder.ReadDir(-1)
	folder.Close()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		f, err := root.OpenFile(entry.Name(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			return nil, errors.New("make the memory evaluation an ordinary file")
		}
		data, err := io.ReadAll(io.LimitReader(f, (32<<10)+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		var a MemoryAssessment
		if len(data) > 32<<10 || json.Unmarshal(data, &a) != nil || a.At.IsZero() || !validMemoryAssessment(a) || len(a.Phrase) > toolMax {
			continue
		}
		if entry.Name() != filepath.Base(s.memoryAssessmentPath(scope, a.SourceID, a.Key)) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

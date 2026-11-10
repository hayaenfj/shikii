package store

import (
	"io"
	"os"
)

// openRecentHistory positions a safely opened history at its bounded tail.
// Callers own Close, byte limits, hashing and the treatment of partial lines.
func (s Store) openRecentHistory(path string) (*os.File, int64, error) {
	f, err := s.openFile(path, os.O_RDONLY)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	start := max(int64(0), info.Size()-feedbackReadMax)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, start, nil
}

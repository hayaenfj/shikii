package store

import (
	"errors"
	"io"
	"os"
	"syscall"
)

func (s Store) appendJSONLBytes(path string, b []byte) error {
	return s.withJSONLAppendLock(path, func() error {
		return s.appendJSONLBytesLocked(path, b)
	})
}

// Keep the same blocking lock through the caller's read, deduplication and append.
func (s Store) withJSONLAppendLock(path string, appendLocked func() error) error {
	lock, err := s.openFile(path+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return appendLocked()
}

// The caller holds path + ".lock", including through deduplication when needed.
func (s Store) appendJSONLBytesLocked(path string, b []byte) error {
	f, err := s.openFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	start := info.Size()
	if start > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], start-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			// Keep an interrupted old tail separate from the next complete line.
			// A complete JSON value without a newline is preserved as well.
			b = append([]byte{'\n'}, b...)
		}
	}
	n, err := f.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err != nil {
		// All JSONL writers hold the same lock, so this cannot remove a peer's append.
		return errors.Join(err, f.Truncate(start))
	}
	return f.Close()
}

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/rootfile"
	"github.com/hayaenfj/shikii/internal/store"
)

// Retained only by one record call, after a successful durable publication.
// Reuse still occurs under the ordinary session lock and retains its sync.
type workSessionCheckpoint struct{ last []byte }

var workCheckpointNow = time.Now
var workCheckpointTouch = func(f *os.File, at time.Time) error {
	t := syscall.NsecToTimeval(at.UnixNano())
	return syscall.Futimes(int(f.Fd()), []syscall.Timeval{t, t})
}

func writeWorkCheckpoint(dir *os.Root, name string, s *hookSession, checkpoint *workSessionCheckpoint) error {
	data, err := encodeHookSession(s)
	if err != nil {
		return err
	}
	reusable := false
	if checkpoint != nil && bytes.Equal(checkpoint.last, data) {
		info, e := dir.Lstat(name)
		if e == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600 {
			if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink == 1 {
				reusable = true
			}
		}
	}
	if reusable {
		err = reuseWorkCheckpoint(dir, name, data)
	} else {
		err = writeWorkSessionBytes(dir, name, data)
	}
	if checkpoint != nil {
		if err == nil {
			checkpoint.last = data
		} else {
			checkpoint.last = nil
		}
	}
	return err
}

func reuseWorkCheckpoint(dir *os.Root, name string, data []byte) error {
	f, err := rootfile.OpenNoFollow(dir, name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	changed := errors.New("the session changed while saving; retry")
	if !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
		return changed
	}
	scratch := make([]byte, 32<<10)
	for at := 0; at < len(data); {
		n := min(len(scratch), len(data)-at)
		if _, err = io.ReadFull(f, scratch[:n]); err != nil {
			return err
		}
		if !bytes.Equal(scratch[:n], data[at:at+n]) {
			return changed
		}
		at += n
	}
	if n, e := f.Read(scratch[:1]); n != 0 || e != io.EOF {
		if e != nil && e != io.EOF {
			return e
		}
		return changed
	}
	current, err := dir.Lstat(name)
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) {
		return changed
	}
	// Replacing an unchanged file used to refresh its cleanup TTL. Keep that
	// effect on the descriptor whose complete contents were checked above.
	if err = workCheckpointTouch(f, workCheckpointNow()); err != nil {
		return err
	}
	if err = store.SyncTemp(f); err != nil {
		if restore := workCheckpointTouch(f, info.ModTime()); restore != nil {
			return errors.Join(err, restore)
		}
		return err
	}
	err = f.Close()
	closed = true
	return err
}

// Package rootfile opens a file inside an os.Root without following a link in its last name.
//
// os.Root.OpenFile follows a link in the last name even when O_NOFOLLOW is given (checked on go1.27.3 darwin/arm64): the link is
// resolved inside the root, so it cannot reach outside, but a file under another name in the same root can be read, locked or written
// through it. os.OpenFile with O_NOFOLLOW refuses the same link with ELOOP. Every open of an *os.Root without O_EXCL goes through
// OpenNoFollow (TestOnlyTheNoFollowOpenerOpensAFileInARoot in cmd/shikii keeps it so); O_CREATE|O_EXCL already refuses any link.
package rootfile

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// betweenCheckAndOpen runs after the name is checked and before it is opened; a test swaps the name there.
var betweenCheckAndOpen func()

// betweenOpenAndCheck runs after the open and before the check after it (a test only).
var betweenOpenAndCheck func()

// OpenNoFollow opens name inside r with flag and perm, refusing a link in the last name: before opening, a name that is a link is
// refused; after opening, the opened file must be the regular file checked before, or the file now at the name (not a link), or it
// was reached through a link swapped in between (also when O_CREATE made it) and is closed and refused. A refusal wraps syscall.ELOOP, as os.OpenFile with O_NOFOLLOW reports it, so a
// caller's os.IsNotExist still means only that nothing is there. Writing, locking and reading happen after the check, so they never
// reach the file behind a link. Do not pass O_TRUNC: it acts at the open, before the check after opening.
func OpenNoFollow(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
	before, err := r.Lstat(name)
	if err == nil {
		if before.Mode()&os.ModeSymlink != 0 {
			return nil, refused(name)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else {
		before = nil
	}
	if betweenCheckAndOpen != nil {
		betweenCheckAndOpen()
	}
	f, err := r.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if betweenOpenAndCheck != nil {
		betweenOpenAndCheck()
	}
	// the file opened is the one checked before the open (a writer may have replaced the name by rename since: what was opened is
	// still the file that was there, not one behind a link), or the one at the name now (a link at the name is never the same file as
	// the one opened; it was made by this open, or by another writer between the check and the open)
	if before != nil && os.SameFile(opened, before) {
		return f, nil
	}
	there, err := r.Lstat(name)
	if err != nil || !os.SameFile(opened, there) {
		f.Close()
		return nil, refused(name)
	}
	return f, nil
}

func refused(name string) error {
	return &fs.PathError{Op: "open", Path: name, Err: syscall.ELOOP}
}

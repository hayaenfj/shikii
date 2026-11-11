// Package rootfile opens a file inside an os.Root without following a link in its last name.
//
// os.Root.OpenFile follows a link in the last name even when O_NOFOLLOW is given (checked on go1.27.3 darwin/arm64): the link is
// resolved inside the root, so it cannot reach outside, but a file under another name in the same root can be read, locked or written
// through it. os.OpenFile with O_NOFOLLOW refuses the same link with ELOOP. Every open, create, read or write of a file through an
// *os.Root goes through OpenNoFollow (TestOnlyTheNoFollowOpenerOpensAFileInARoot in cmd/shikii keeps it so), except an OpenFile with
// both O_CREATE and O_EXCL, which already refuses any link at the name (O_EXCL without O_CREATE does not).
package rootfile

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// attempts is how many times OpenNoFollow checks and opens a name whose file changed between the check and the open before it
// refuses the name. It is a fixed number of tries again for a file that did not match, not a wait for a race to end.
const attempts = 8

// betweenCheckAndOpen runs after the name is checked and before it is opened; a test swaps the name there.
var betweenCheckAndOpen func()

// betweenOpenAndCheck runs after the open and before the check after it (a test only).
var betweenOpenAndCheck func()

// OpenNoFollow opens name inside r with flag and perm, refusing a link in the last name.
//
// Before opening, a name that is a link is refused. After opening, the opened file must be the regular file checked before, or the
// file now at the name (a link at the name is never the same file as the one opened); when it is neither (the name was rewritten by
// rename on both sides of the open, or a link was swapped in and out), the file is closed and the name is checked and opened again,
// up to a fixed number of times, after which it is refused.
//
// With O_CREATE (without O_EXCL), a name missing at the check is opened with O_EXCL added, so a link swapped in before the open is
// refused by the open itself and the file it points at is never made; the open made the file, so nothing is checked after it. A name
// that exists at the check is opened without O_CREATE, so the file behind a dangling link is not made either. If the name appeared
// or vanished in between, it is checked and opened again. With O_CREATE|O_EXCL the open fails on any name that exists, a link too.
//
// A refusal wraps syscall.ELOOP, as os.OpenFile with O_NOFOLLOW reports it, so a caller's os.IsNotExist still means only that
// nothing is there. Writing, locking and reading happen after the check, so they never reach the file behind a link. Do not pass
// O_TRUNC: it acts at the open, before the check after opening.
func OpenNoFollow(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
	create := flag&os.O_CREATE != 0
	exclusive := create && flag&os.O_EXCL != 0
	for range attempts {
		before, err := r.Lstat(name)
		switch {
		case err == nil && before.Mode()&os.ModeSymlink != 0:
			return nil, refused(name)
		case err == nil:
		case errors.Is(err, fs.ErrNotExist):
			before = nil
		default:
			return nil, err
		}
		if betweenCheckAndOpen != nil {
			betweenCheckAndOpen()
		}
		open := flag
		switch {
		case !create || exclusive:
		case before == nil:
			open |= os.O_EXCL
		default:
			open &^= os.O_CREATE
		}
		f, err := r.OpenFile(name, open, perm)
		if err != nil {
			if create && !exclusive && (before == nil && errors.Is(err, fs.ErrExist) || before != nil && errors.Is(err, fs.ErrNotExist)) {
				continue // the name was made, swapped for a link or removed after the check: check it again
			}
			return nil, err
		}
		if open&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL {
			return f, nil // made by this create-exclusive open, which refuses a link at the name, a dangling one too
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
		// still the file that was there, not one behind a link), or the one at the name now
		if before != nil && os.SameFile(opened, before) {
			return f, nil
		}
		if there, err := r.Lstat(name); err == nil && os.SameFile(opened, there) {
			return f, nil
		}
		f.Close()
	}
	return nil, refused(name)
}

func refused(name string) error {
	return &fs.PathError{Op: "open", Path: name, Err: syscall.ELOOP}
}

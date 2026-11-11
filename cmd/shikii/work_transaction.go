package main

import (
	"errors"
	"os"
	"syscall"

	"github.com/hayaenfj/shikii/internal/rootfile"
)

type workCommitter func(func() error) error

// Only a short storage operation runs inside this boundary. Planning and
// placement model calls stay outside; their publication checks run separately.
func commitWorkTurn(dir *os.Root, name, scope string, token workToken, action func() error) error {
	f, err := rootfile.OpenNoFollow(dir, name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("invalid session commit lock")
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("the session is being updated; retry the same work")
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if _, _, err = loadWorkState(dir, scope, token); err != nil {
		return err
	}
	if action == nil {
		return nil
	}
	return action()
}

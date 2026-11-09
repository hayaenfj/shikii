package main

import (
	"errors"
	"os"
	"syscall"
)

func lockPlanningSession(dir *os.Root, name string) (func(), error) {
	lock, err := dir.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}

	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() {
		lock.Close()
		return nil, errors.New("cannot check the session lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("the request record is being updated; retry")
	}
	return func() { syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); lock.Close() }, nil
}

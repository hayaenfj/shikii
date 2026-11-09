package main

import (
	"os"
	"syscall"

	"github.com/hayaenfj/shikii/internal/store"
)

func writeWorkTemp(dir *os.Root, temp string, data []byte) (err error) {
	f, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			dir.Remove(temp)
		}
	}()
	_, err = f.Write(data)
	if err == nil {
		err = store.SyncTemp(f)
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return nil
}

package main

import (
	"errors"
	"os"
	"path/filepath"
)

// A new standalone preparation is not delivered until its assignment binding
// is published. Retire only that unreturned nonce, under both session locks.
func retireAssignmentPreparation(root, scope, receipt string) error {
	token, err := workDecode(receipt)
	if err != nil {
		return err
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, token.Session))
	release, err := workLock(dir, name)
	if err != nil {
		return err
	}
	defer release()
	if _, err = dir.Lstat(name); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	err = commitWorkTurn(dir, name, scope, token, func() error {
		w, _, err := loadWorkState(dir, scope, token)
		if err != nil {
			return err
		}
		if !w.Standalone || len(w.Saved) > 0 || len(w.Captures) > 0 || w.Result != nil {
			return errors.New("the unreturned preparation changed; cannot retire another execution")
		}
		return dir.Remove(name)
	})
	if err != nil {
		return err
	}
	// Persist removal without depending on the failed checkpoint's sync hook.
	f, err := dir.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

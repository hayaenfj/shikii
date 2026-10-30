package main

import (
	"encoding/json"
	"errors"
	"os"
	"syscall"

	"github.com/hayaenfj/shikii/internal/store"
)

// Save only into the same ongoing frame under the hook's session lock. Query does not judge or regroup it.
func savePlanningQuery(dir *os.Root, name, scope string, req planningRequest, sources map[string]string, knowledge *store.KnowledgeContext) error {
	lock, err := dir.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("cannot check the session lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("the request record is being updated; retry")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	s.Planning.Sources, s.Planning.Knowledge, s.Planning.KnowledgePages = sources, knowledge, req.Pages
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("the prepared context exceeds the session record limit")
	}
	nameTemp := ".planning-" + store.NewTraceID() + ".tmp"
	f, err := dir.OpenFile(nameTemp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(nameTemp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return dir.Rename(nameTemp, name)
}

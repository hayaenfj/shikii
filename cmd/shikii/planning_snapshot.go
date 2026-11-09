package main

import (
	"os"

	"github.com/hayaenfj/shikii/internal/store"
)

// Save only into the same ongoing frame under the hook's session lock. Query does not judge or regroup it.
func savePlanningQuery(dir *os.Root, name, scope string, req planningRequest, sources map[string]string, knowledge *store.KnowledgeContext) error {
	unlock, err := lockPlanningSession(dir, name)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return err
	}
	if err := currentPlanningFrame(s, req.TurnID, scope); err != nil {
		return err
	}
	// Replacing the planning snapshot also replaces its prepared references.
	// Never leave a live receipt pointing at a different quotation context.
	s.Planning.Work = nil
	s.Planning.Sources, s.Planning.Knowledge, s.Planning.KnowledgePages = sources, knowledge, req.Pages
	data, err := encodeHookSession(s)
	if err != nil {
		return err
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

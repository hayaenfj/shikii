package main

import (
	"os"
	"sync"
)

type workRecordPersistence struct {
	dir         *os.Root
	name, scope string
	token       workToken
	w           *workState
	checkpoint  *workSessionCheckpoint
	commitMu    sync.Mutex
	interrupted error
	unsaved     bool // the state holds what no save has written yet: a later save writes it, and flush saves it when none follows
}

func (p *workRecordPersistence) save() error {
	p.commitMu.Lock()
	defer p.commitMu.Unlock()
	if p.interrupted != nil {
		return p.interrupted
	}
	e := saveWorkState(p.dir, p.name, p.scope, p.token.Turn, p.w, false, p.checkpoint)
	if e != nil {
		p.interrupted = e
	} else {
		p.unsaved = false
	}
	return e
}

// markUnsaved says that the state holds something that has to reach the saved state before the record ends.
func (p *workRecordPersistence) markUnsaved() {
	p.commitMu.Lock()
	defer p.commitMu.Unlock()
	p.unsaved = true
}

// flush saves the state when something of it is still unsaved (the record had no later save). A record that was interrupted already
// said so, and the state it could not save is not reported twice.
func (p *workRecordPersistence) flush(add func(workItem)) {
	p.commitMu.Lock()
	pending, interrupted := p.unsaved, p.interrupted != nil
	p.commitMu.Unlock()
	if !pending || interrupted {
		return
	}
	if e := p.save(); e != nil {
		add(workItem{Kind: "coverage", Ref: "coverage", Error: "the work state could not be saved after the coverage of the result was handled: " + e.Error() + "; send the same record again"})
	}
}
func (p *workRecordPersistence) commit(action func() error) error {
	p.commitMu.Lock()
	defer p.commitMu.Unlock()
	if p.interrupted != nil {
		return p.interrupted
	}
	ran := false
	e := commitWorkTurn(p.dir, p.name, p.scope, p.token, func() error {
		ran = true
		if action == nil {
			return nil
		}
		return action()
	})
	if !ran && e != nil {
		p.interrupted = e
	}
	return e
}
func (p *workRecordPersistence) halt(add func(workItem)) bool {
	_ = p.commit(nil)
	p.commitMu.Lock()
	e := p.interrupted
	p.commitMu.Unlock()
	if e == nil {
		return false
	}
	add(workItem{Ref: "remaining", Kind: "context", Error: "remaining operations stopped: " + e.Error()})
	return true
}

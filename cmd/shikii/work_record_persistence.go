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
	}
	return e
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
